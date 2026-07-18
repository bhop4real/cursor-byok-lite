package forwarder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	modeladapter "cursor/internal/backend/agent/model"
)

const currentPassLedgerLimit = 12

type currentTurnToolCall struct {
	CallID      string
	ToolName    string
	Arguments   string
	Fingerprint string
	Path        string
	ResultText  string
	Outcome     string
}

type currentTurnMutationState struct {
	Path        string
	Successes   int
	Fingerprint string
}

func currentPassExecutionContract(conversation *ConversationFile, replayMessages []modeladapter.Message) string {
	calls := collectCurrentTurnToolCalls(conversation, replayMessages)
	mutations, failures, pending, repetitions := summarizeCurrentTurnToolCalls(calls)

	var builder strings.Builder
	builder.WriteString("<system_reminder>\n")
	builder.WriteString("CURRENT-PASS EXECUTION CONTRACT — apply this contract now. It is regenerated after replay and tool results on every provider pass; it overrides weaker or older execution habits.\n\n")
	builder.WriteString("Deterministic execution:\n")
	builder.WriteString("- Before any mutation, inspect enough current context to decide the complete final state. Apply all known changes to a file in one coherent edit; do not construct it incrementally, toggle alternatives through edits, or use edits as experiments.\n")
	builder.WriteString("- A successful mutation settles that path for this turn. Do not mutate it again unless a later compiler, test, linter, or runtime result identifies a concrete defect, or the user changes the required final state. Mere uncertainty, preference, or rereading is not new evidence.\n")
	builder.WriteString("- After any failed, rejected, or permission-denied tool call, inspect its result and change the arguments or approach. Never repeat an identical rejected invocation. Never retry a failed tool call unchanged.\n")
	builder.WriteString("- Use actual tool calls for tool actions; never print tool-call JSON or tool-call syntax as assistant prose. A progress update is not completion: continue while required work remains, then report the result.\n\n")
	fmt.Fprintf(&builder, "Authoritative current-turn tool ledger: calls=%d, settled_paths=%d, failed_or_rejected=%d, pending=%d, repeated_invocations=%d.\n", len(calls), len(mutations), len(failures), len(pending), len(repetitions))
	appendExecutionLedgerSection(&builder, "Settled paths (do not edit again without new defect evidence):", mutations, currentPassLedgerLimit)
	appendExecutionLedgerSection(&builder, "Failed or rejected calls (do not retry unchanged):", failures, currentPassLedgerLimit)
	appendExecutionLedgerSection(&builder, "Pending calls (do not duplicate):", pending, currentPassLedgerLimit)
	appendExecutionLedgerSection(&builder, "Repeated identical invocations already observed (stop repeating them):", repetitions, currentPassLedgerLimit)
	builder.WriteString("</system_reminder>")
	return builder.String()
}

func collectCurrentTurnToolCalls(conversation *ConversationFile, replayMessages []modeladapter.Message) []currentTurnToolCall {
	turnSeq := currentConversationTurnSeq(conversation)
	requestID := ""
	if conversation != nil {
		requestID = strings.TrimSpace(conversation.CurrentRequestID)
	}
	calls := make([]currentTurnToolCall, 0)
	indexes := make(map[string]int)
	if conversation != nil && turnSeq > 0 {
		for _, entry := range conversation.Entries {
			if entry.TurnSeq != turnSeq || !currentTurnEntryMatchesRequest(entry, requestID) {
				continue
			}
			switch strings.TrimSpace(entry.Kind) {
			case "tool_call":
				var payload toolCallEntryPayload
				if json.Unmarshal(entry.Payload, &payload) != nil {
					continue
				}
				upsertCurrentTurnToolCall(&calls, indexes, payload.ToolCallID, payload.ToolName, payload.Arguments, "")
			case "tool_result":
				var payload toolResultEntryPayload
				if json.Unmarshal(entry.Payload, &payload) != nil {
					continue
				}
				upsertCurrentTurnToolCall(&calls, indexes, payload.ToolCallID, payload.ToolName, payload.Arguments, payload.ResultText)
			case "model_message":
				var payload modelMessageEntryPayload
				if json.Unmarshal(entry.Payload, &payload) != nil {
					continue
				}
				collectToolCallsFromReplayMessage(&calls, indexes, payload.Message)
			}
		}
	}
	if len(calls) == 0 && turnSeq <= 0 {
		for _, message := range replayMessages {
			collectToolCallsFromReplayMessage(&calls, indexes, message)
		}
	}
	for index := range calls {
		calls[index].Outcome = classifyCurrentTurnToolOutcome(calls[index].ResultText)
		if calls[index].Path == "" {
			calls[index].Path = extractPathFromToolArguments(calls[index].Arguments)
		}
	}
	return calls
}

func currentConversationTurnSeq(conversation *ConversationFile) int64 {
	if conversation == nil {
		return 0
	}
	if conversation.CurrentTurnSeq > 0 {
		return conversation.CurrentTurnSeq
	}
	if conversation.NextTurnSeq > 1 {
		return conversation.NextTurnSeq - 1
	}
	return 0
}

func currentTurnEntryMatchesRequest(entry HistoryEntry, requestID string) bool {
	return requestID == "" || strings.TrimSpace(entry.RequestID) == "" || strings.TrimSpace(entry.RequestID) == requestID
}

func collectToolCallsFromReplayMessage(calls *[]currentTurnToolCall, indexes map[string]int, message modeladapter.Message) {
	if strings.TrimSpace(message.Role) == "assistant" {
		for _, toolCall := range message.ToolCalls {
			upsertCurrentTurnToolCall(calls, indexes, toolCall.ID, toolCall.Function.Name, toolCall.Function.Arguments, "")
		}
		return
	}
	if strings.TrimSpace(message.Role) == "tool" {
		upsertCurrentTurnToolCall(calls, indexes, message.ToolCallID, message.Name, "", message.Content)
	}
}

func upsertCurrentTurnToolCall(calls *[]currentTurnToolCall, indexes map[string]int, callID string, toolName string, arguments string, resultText string) {
	callID = strings.TrimSpace(callID)
	toolName = strings.TrimSpace(toolName)
	if callID == "" || toolName == "" {
		return
	}
	index, exists := indexes[callID]
	if !exists {
		index = len(*calls)
		indexes[callID] = index
		*calls = append(*calls, currentTurnToolCall{CallID: callID})
	}
	call := &(*calls)[index]
	call.ToolName = toolName
	if strings.TrimSpace(arguments) != "" {
		call.Arguments = strings.TrimSpace(arguments)
		call.Fingerprint = currentTurnToolFingerprint(toolName, call.Arguments)
		call.Path = extractPathFromToolArguments(call.Arguments)
	}
	if strings.TrimSpace(resultText) != "" {
		call.ResultText = strings.TrimSpace(resultText)
		if path := extractSuccessfulResultPath(call.ResultText); path != "" {
			call.Path = path
		}
	}
}

func currentTurnToolFingerprint(toolName string, arguments string) string {
	normalizedArguments := strings.TrimSpace(arguments)
	if normalizedArguments == "" {
		normalizedArguments = "{}"
	} else {
		var value any
		if json.Unmarshal([]byte(normalizedArguments), &value) == nil {
			if encoded, err := json.Marshal(value); err == nil {
				normalizedArguments = string(encoded)
			}
		}
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(toolName) + "\x00" + normalizedArguments))
	return hex.EncodeToString(sum[:6])
}

func extractSuccessfulResultPath(resultText string) string {
	var payload map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(resultText)), &payload) != nil {
		return ""
	}
	success, ok := payload["success"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"path", "file_path"} {
		if value, ok := success[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func classifyCurrentTurnToolOutcome(resultText string) string {
	trimmed := strings.TrimSpace(resultText)
	if trimmed == "" {
		return "pending"
	}
	var payload map[string]any
	if json.Unmarshal([]byte(trimmed), &payload) == nil {
		if _, ok := payload["success"]; ok {
			return "success"
		}
		for _, key := range []string{"error", "rejected", "permission_denied", "permissionDenied", "read_permission_denied", "readPermissionDenied", "write_permission_denied", "writePermissionDenied", "file_not_found", "fileNotFound", "no_space", "noSpace"} {
			if _, ok := payload[key]; ok {
				return "failed"
			}
		}
	}
	normalized := strings.ToLower(trimmed)
	for _, marker := range []string{" rejected", "rejected:", "permission denied", " error:", "exec throw:", "transport closed before terminal result", "file not found", "no space left", "timed out", "timeout"} {
		if strings.Contains(normalized, marker) {
			return "failed"
		}
	}
	if strings.HasPrefix(normalized, "error:") || strings.HasSuffix(normalized, " result missing") {
		return "failed"
	}
	return "completed"
}

func summarizeCurrentTurnToolCalls(calls []currentTurnToolCall) (mutations []string, failures []string, pending []string, repetitions []string) {
	mutationByPath := make(map[string]*currentTurnMutationState)
	mutationOrder := make([]string, 0)
	invocationCounts := make(map[string]int)
	invocationLabels := make(map[string]string)
	for _, call := range calls {
		fingerprint := firstNonEmpty(call.Fingerprint, currentTurnToolFingerprint(call.ToolName, call.Arguments))
		key := call.ToolName + "\x00" + fingerprint
		invocationCounts[key]++
		invocationLabels[key] = fmt.Sprintf("%s args#%s", call.ToolName, fingerprint)
		label := currentTurnToolCallLabel(call, fingerprint)
		switch call.Outcome {
		case "failed":
			failures = append(failures, label)
		case "pending":
			pending = append(pending, label)
		}
		if call.Outcome != "success" || !isCurrentTurnMutationTool(call.ToolName) || strings.TrimSpace(call.Path) == "" {
			continue
		}
		path := strings.TrimSpace(call.Path)
		state := mutationByPath[path]
		if state == nil {
			state = &currentTurnMutationState{Path: path}
			mutationByPath[path] = state
			mutationOrder = append(mutationOrder, path)
		}
		state.Successes++
		state.Fingerprint = fingerprint
	}
	for _, path := range mutationOrder {
		state := mutationByPath[path]
		mutations = append(mutations, fmt.Sprintf("%s (successful mutations=%d, latest args#%s)", state.Path, state.Successes, state.Fingerprint))
	}
	repetitionKeys := make([]string, 0)
	for key, count := range invocationCounts {
		if count > 1 {
			repetitionKeys = append(repetitionKeys, key)
		}
	}
	sort.Strings(repetitionKeys)
	for _, key := range repetitionKeys {
		repetitions = append(repetitions, fmt.Sprintf("%s repeated=%d", invocationLabels[key], invocationCounts[key]))
	}
	return mutations, failures, pending, repetitions
}

func currentTurnToolCallLabel(call currentTurnToolCall, fingerprint string) string {
	label := fmt.Sprintf("%s call=%s args#%s", call.ToolName, call.CallID, fingerprint)
	if strings.TrimSpace(call.Path) != "" {
		label += " path=" + strings.TrimSpace(call.Path)
	}
	return label
}

func isCurrentTurnMutationTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "Write", "PatchEdit", "PatchEditLines", "PatchEditSpan", "Delete":
		return true
	default:
		return false
	}
}

func appendExecutionLedgerSection(builder *strings.Builder, heading string, items []string, limit int) {
	if builder == nil || len(items) == 0 {
		return
	}
	builder.WriteString("\n")
	builder.WriteString(heading)
	builder.WriteString("\n")
	shown := len(items)
	if shown > limit {
		shown = limit
	}
	for _, item := range items[len(items)-shown:] {
		builder.WriteString("- ")
		builder.WriteString(item)
		builder.WriteString("\n")
	}
	if omitted := len(items) - shown; omitted > 0 {
		fmt.Fprintf(builder, "- ... %d earlier ledger entries omitted; they remain visible in replay history.\n", omitted)
	}
}
