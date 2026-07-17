package forwarder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	"cursor/gen/agentv1"
	runtimecore "cursor/internal/backend/agent/core"
)

var errProviderLoopInterrupted = errors.New("provider loop interrupted")

func isTerminalStreamStatus(status StreamStatus) bool {
	switch status {
	case StreamStatusCanceled, StreamStatusCompleted, StreamStatusFailed:
		return true
	default:
		return false
	}
}

func (service *Service) finalizeInterruptedTurn(stream *ActiveStream) error {
	if service == nil || stream == nil {
		return nil
	}

	stream.mu.Lock()
	if stream.InterruptedTurnFinalized || stream.CheckpointConversation == nil {
		stream.mu.Unlock()
		return nil
	}
	stream.InterruptedTurnFinalized = true
	conversationID := strings.TrimSpace(stream.ConversationID)
	requestID := strings.TrimSpace(stream.RequestID)
	turnSeq := stream.TurnSeq
	modelCallID := strings.TrimSpace(stream.CurrentModelCallID)
	text := string(stream.ProviderAccumulatedText)
	reasoning := string(stream.ProviderAccumulatedReasoning)
	reasoningSignature := strings.TrimSpace(stream.ProviderAccumulatedReasoningSignature)
	reasoningSignatureSource := strings.TrimSpace(stream.ProviderAccumulatedReasoningSignatureSource)
	reasoningItemID := strings.TrimSpace(stream.ProviderAccumulatedReasoningItemID)
	reasoningStatus := strings.TrimSpace(stream.ProviderAccumulatedReasoningStatus)
	reasoningSummary := append([]byte(nil), stream.ProviderAccumulatedReasoningSummary...)
	partialCalls := make([]interruptedToolCall, 0, len(stream.PartialToolCalls))
	for _, partial := range stream.PartialToolCalls {
		partialCalls = append(partialCalls, partial)
	}
	pendingExecs := make([]runtimecore.PendingExec, 0, len(stream.PendingExecs))
	for _, pending := range stream.PendingExecs {
		pendingExecs = append(pendingExecs, pending)
	}
	pendingInteractions := make([]runtimecore.PendingInteraction, 0, len(stream.PendingInteractions))
	for _, pending := range stream.PendingInteractions {
		pendingInteractions = append(pendingInteractions, pending)
	}
	stream.mu.Unlock()

	var latest *ConversationFile
	if service.store != nil {
		var err error
		latest, err = service.store.LoadConversation(conversationID)
		if err != nil {
			stream.mu.Lock()
			stream.InterruptedTurnFinalized = false
			stream.mu.Unlock()
			return err
		}
		if interruptedTurnMarkerExists(latest, turnSeq, modelCallID) {
			return nil
		}
	} else {
		stream.mu.Lock()
		latest = cloneConversationFile(stream.CheckpointConversation)
		stream.mu.Unlock()
	}
	existingToolCalls, existingToolResults := interruptedTurnToolIDs(latest, turnSeq)

	entries := make([]HistoryEntry, 0, 2+len(partialCalls)+len(pendingExecs)+len(pendingInteractions))
	if !assistantTextEntryExists(latest, turnSeq, text, reasoning, reasoningSignature) &&
		(strings.TrimSpace(text) != "" || hasReplayableReasoningPayload(reasoning, reasoningSignature, reasoningSignatureSource)) {
		entries = append(entries, newAssistantTextEntryWithProviderMetadata(
			turnSeq,
			requestID,
			text,
			reasoning,
			reasoningSignature,
			reasoningSignatureSource,
			reasoningItemID,
			reasoningStatus,
			reasoningSummary,
		))
	}

	sort.SliceStable(partialCalls, func(i, j int) bool {
		return strings.TrimSpace(partialCalls[i].ToolCallID) < strings.TrimSpace(partialCalls[j].ToolCallID)
	})
	seenToolCalls := make(map[string]struct{}, len(partialCalls)+len(pendingExecs)+len(pendingInteractions))
	for _, partial := range partialCalls {
		toolCallID := strings.TrimSpace(partial.ToolCallID)
		if toolCallID == "" {
			continue
		}
		toolName := strings.TrimSpace(partial.ToolName)
		if toolName == "" {
			toolName = interruptedToolName(partial.ToolCall)
		}
		toolName = firstNonEmpty(toolName, "interrupted_tool")
		arguments := firstNonEmpty(strings.TrimSpace(partial.Arguments), "{}")
		seenToolCalls[toolCallID] = struct{}{}
		if _, exists := existingToolCalls[toolCallID]; !exists {
			entries = append(entries, newToolCallEntryWithProviderMetadata(
				turnSeq,
				requestID,
				toolCallID,
				toolName,
				[]byte(arguments),
				partial.ReasoningContent,
				partial.ReasoningSignature,
				partial.ReasoningSignatureSource,
				"",
				"",
				nil,
				partial.ProviderItemID,
				partial.ProviderCallID,
				partial.ProviderStatus,
				partial.ToolCall,
			))
		}
		if _, exists := existingToolResults[toolCallID]; !exists {
			entries = append(entries, newToolResultEntry(
				turnSeq,
				requestID,
				toolCallID,
				toolName,
				arguments,
				interruptedToolResultText(toolName),
				partial.ReasoningContent,
				nil,
			))
		}
	}

	sort.SliceStable(pendingExecs, func(i, j int) bool {
		return strings.TrimSpace(pendingExecs[i].ToolCallID) < strings.TrimSpace(pendingExecs[j].ToolCallID)
	})
	for _, pending := range pendingExecs {
		toolCallID := strings.TrimSpace(pending.ToolCallID)
		toolName := strings.TrimSpace(visiblePendingExecToolName(pending))
		if toolCallID == "" || toolName == "" {
			continue
		}
		if _, exists := seenToolCalls[toolCallID]; exists {
			continue
		}
		if _, exists := existingToolResults[toolCallID]; exists {
			seenToolCalls[toolCallID] = struct{}{}
			continue
		}
		seenToolCalls[toolCallID] = struct{}{}
		arguments := string(visiblePendingExecArgsJSON(pending))
		if _, exists := existingToolCalls[toolCallID]; !exists {
			entries = append(entries, newToolCallEntryWithProviderMetadata(
				turnSeq,
				requestID,
				toolCallID,
				toolName,
				[]byte(arguments),
				pending.ReasoningContent,
				"",
				"",
				"",
				"",
				nil,
				"",
				"",
				"",
				nil,
			))
		}
		entries = append(entries, newToolResultEntry(
			turnSeq,
			requestID,
			toolCallID,
			toolName,
			arguments,
			interruptedToolResultText(toolName),
			pending.ReasoningContent,
			nil,
		))
	}

	sort.SliceStable(pendingInteractions, func(i, j int) bool {
		return strings.TrimSpace(pendingInteractions[i].ToolCallID) < strings.TrimSpace(pendingInteractions[j].ToolCallID)
	})
	for _, pending := range pendingInteractions {
		toolCallID := strings.TrimSpace(pending.ToolCallID)
		toolName := strings.TrimSpace(deriveToolNameFromPendingInteraction(pending))
		if toolCallID == "" || toolName == "" {
			continue
		}
		if _, exists := existingToolResults[toolCallID]; exists {
			seenToolCalls[toolCallID] = struct{}{}
			continue
		}
		seenToolCalls[toolCallID] = struct{}{}
		arguments := string(pending.ArgsJSON)
		if _, exists := existingToolCalls[toolCallID]; !exists {
			entries = append(entries, newToolCallEntryWithProviderMetadata(
				turnSeq,
				requestID,
				toolCallID,
				toolName,
				[]byte(arguments),
				pending.ReasoningContent,
				"",
				"",
				"",
				"",
				nil,
				"",
				"",
				"",
				nil,
			))
		}
		entries = append(entries, newToolResultEntry(
			turnSeq,
			requestID,
			toolCallID,
			toolName,
			string(pending.ArgsJSON),
			interruptedToolResultText(toolName),
			pending.ReasoningContent,
			nil,
		))
	}

	if len(entries) == 0 {
		return nil
	}
	entries = append([]HistoryEntry{
		newInterruptedTurnMarkerEntry(turnSeq, requestID, modelCallID),
	}, entries...)
	_, err := service.appendConversationEntries(stream, conversationID, entries)
	if err != nil {
		stream.mu.Lock()
		stream.InterruptedTurnFinalized = false
		stream.mu.Unlock()
	}
	return err
}

func interruptedTurnToolIDs(conversation *ConversationFile, turnSeq int64) (map[string]struct{}, map[string]struct{}) {
	toolCalls := make(map[string]struct{})
	toolResults := make(map[string]struct{})
	if conversation == nil || turnSeq <= 0 {
		return toolCalls, toolResults
	}
	for _, entry := range conversation.Entries {
		if entry.TurnSeq != turnSeq {
			continue
		}
		var toolCallID string
		switch strings.TrimSpace(entry.Kind) {
		case "tool_call":
			var payload toolCallEntryPayload
			if json.Unmarshal(entry.Payload, &payload) == nil {
				toolCallID = strings.TrimSpace(payload.ToolCallID)
			}
			if toolCallID != "" {
				toolCalls[toolCallID] = struct{}{}
			}
		case "tool_result":
			var payload toolResultEntryPayload
			if json.Unmarshal(entry.Payload, &payload) == nil {
				toolCallID = strings.TrimSpace(payload.ToolCallID)
			}
			if toolCallID != "" {
				toolResults[toolCallID] = struct{}{}
			}
		}
	}
	return toolCalls, toolResults
}

func assistantTextEntryExists(conversation *ConversationFile, turnSeq int64, text string, reasoning string, signature string) bool {
	if conversation == nil || turnSeq <= 0 {
		return false
	}
	for _, entry := range conversation.Entries {
		if entry.TurnSeq != turnSeq || strings.TrimSpace(entry.Kind) != "assistant_text" {
			continue
		}
		var payload assistantTextPayload
		if json.Unmarshal(entry.Payload, &payload) != nil {
			continue
		}
		if strings.TrimSpace(payload.Text) == strings.TrimSpace(text) &&
			strings.TrimSpace(payload.ReasoningContent) == strings.TrimSpace(reasoning) &&
			strings.TrimSpace(payload.ReasoningSignature) == strings.TrimSpace(signature) {
			return true
		}
	}
	return false
}

func interruptedToolName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	toolCall := &agentv1.ToolCall{}
	if err := protojson.Unmarshal(raw, toolCall); err != nil {
		return ""
	}
	return strings.TrimSpace(inferToolName(toolCall))
}

func interruptedTurnMarkerExists(conversation *ConversationFile, turnSeq int64, modelCallID string) bool {
	if conversation == nil || turnSeq <= 0 {
		return false
	}
	for _, entry := range conversation.Entries {
		if entry.TurnSeq != turnSeq || strings.TrimSpace(entry.Kind) != "metadata" {
			continue
		}
		var payload metadataPayload
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			continue
		}
		if strings.TrimSpace(payload.Type) != "interrupted_turn" {
			continue
		}
		if modelCallID == "" || strings.TrimSpace(readStringValue(payload.Value["model_call_id"])) == modelCallID {
			return true
		}
	}
	return false
}

func newInterruptedTurnMarkerEntry(turnSeq int64, requestID string, modelCallID string) HistoryEntry {
	return newMetadataEntry(turnSeq, requestID, "interrupted_turn", map[string]any{
		"model_call_id": strings.TrimSpace(modelCallID),
		"status":        "interrupted",
	})
}

func providerLoopInterruptErr(ctx context.Context, stream *ActiveStream, modelCallID string) error {
	if ctx != nil && ctx.Err() != nil {
		return errProviderLoopInterrupted
	}
	if stream == nil {
		return nil
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if isTerminalStreamStatus(stream.Status) {
		return errProviderLoopInterrupted
	}
	switch stream.Phase {
	case TurnPhaseCanceled, TurnPhaseCompleted, TurnPhaseFailed:
		return errProviderLoopInterrupted
	}
	expectedModelCallID := strings.TrimSpace(modelCallID)
	currentModelCallID := strings.TrimSpace(stream.CurrentModelCallID)
	if expectedModelCallID != "" && currentModelCallID != "" && currentModelCallID != expectedModelCallID {
		return errProviderLoopInterrupted
	}
	return nil
}

func interruptedToolResultText(toolName string) string {
	return fmt.Sprintf("%s was interrupted before a terminal result was received; do not replay this external call automatically.", firstNonEmpty(toolName, "tool"))
}
