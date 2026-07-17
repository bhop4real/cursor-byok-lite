package forwarder

import (
	"encoding/json"
	"strings"
	"testing"

	"cursor/gen/agentv1"
	runtimecore "cursor/internal/backend/agent/core"
	modeladapter "cursor/internal/backend/agent/model"
)

func TestAppendEntriesDeduplicatesPromptContextAcrossRequests(t *testing.T) {
	store := NewConversationFileStore(t.TempDir())
	conversationID := "11111111-1111-1111-1111-111111111111"
	if _, err := store.CreateConversation(conversationID, agentv1.AgentMode_AGENT_MODE_AGENT, "", "", conversationID); err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	context := newPromptContextMessage("mode_contract", modelMessage("system", "<system_reminder>stable</system_reminder>"), true)
	for _, requestID := range []string{"request-1", "request-2", "request-3"} {
		if _, _, err := store.AppendEntries(conversationID, []HistoryEntry{
			newPromptContextEntry(1, requestID, context),
		}); err != nil {
			t.Fatalf("append prompt context for %s: %v", requestID, err)
		}
	}

	conversation, err := store.LoadConversation(conversationID)
	if err != nil {
		t.Fatalf("load conversation: %v", err)
	}
	if got := countHistoryEntries(conversation, 1, "prompt_context"); got != 1 {
		t.Fatalf("prompt_context count = %d, want 1", got)
	}

	replay, err := NewHistoryProjector().ProjectPromptReplay(conversation)
	if err != nil {
		t.Fatalf("project prompt replay: %v", err)
	}
	count := 0
	for _, message := range replay {
		if strings.Contains(message.Content, "<system_reminder>stable</system_reminder>") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("reminder replay count = %d, want 1", count)
	}
}

func TestFinalizeInterruptedTurnPersistsAssistantAndPartialToolOnce(t *testing.T) {
	store := NewConversationFileStore(t.TempDir())
	conversationID := "22222222-2222-2222-2222-222222222222"
	conversation, err := store.CreateConversation(conversationID, agentv1.AgentMode_AGENT_MODE_AGENT, "", "", conversationID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	service := &Service{store: store}
	stream := &ActiveStream{
		RequestID:                    "request-interrupted",
		ConversationID:               conversationID,
		TurnSeq:                      1,
		CurrentModelCallID:           "model-call-1",
		CheckpointConversation:       conversation,
		ProviderAccumulatedText:      []byte("visible partial answer"),
		ProviderAccumulatedReasoning: []byte("partial reasoning"),
		PendingExecs:                 map[string]runtimecore.PendingExec{},
		PendingInteractions:          map[string]runtimecore.PendingInteraction{},
		PartialToolCalls: map[string]interruptedToolCall{
			"tool-call-1": {
				ToolCallID:  "tool-call-1",
				ToolName:    "Shell",
				Arguments:   `{"command":"printf partial"}`,
				ModelCallID: "model-call-1",
			},
		},
	}

	if err := service.finalizeInterruptedTurn(stream); err != nil {
		t.Fatalf("finalize interrupted turn: %v", err)
	}
	stream.mu.Lock()
	stream.InterruptedTurnFinalized = false
	stream.mu.Unlock()
	if err := service.finalizeInterruptedTurn(stream); err != nil {
		t.Fatalf("repeat interrupted finalization: %v", err)
	}

	conversation, err = store.LoadConversation(conversationID)
	if err != nil {
		t.Fatalf("load finalized conversation: %v", err)
	}
	if got := countMetadataType(conversation, 1, "interrupted_turn"); got != 1 {
		t.Fatalf("interrupted marker count = %d, want 1", got)
	}
	if got := countHistoryEntries(conversation, 1, "assistant_text"); got != 1 {
		t.Fatalf("assistant_text count = %d, want 1", got)
	}
	if got := countHistoryEntries(conversation, 1, "tool_call"); got != 1 {
		t.Fatalf("tool_call count = %d, want 1", got)
	}
	if got := countHistoryEntries(conversation, 1, "tool_result"); got != 1 {
		t.Fatalf("tool_result count = %d, want 1", got)
	}

	if _, _, err := store.AppendEntries(conversationID, []HistoryEntry{
		newMetadataEntry(1, "request-interrupted", "control", map[string]any{
			"status":        "canceled",
			"reason":        "user aborted",
			"replay_policy": cancelReplayPolicyKeepStableInput,
		}),
	}); err != nil {
		t.Fatalf("append canceled marker: %v", err)
	}
	conversation, err = store.LoadConversation(conversationID)
	if err != nil {
		t.Fatalf("reload canceled conversation: %v", err)
	}
	replay, err := NewHistoryProjector().ProjectPromptReplay(conversation)
	if err != nil {
		t.Fatalf("project canceled replay: %v", err)
	}
	assertInterruptedReplay(t, replay)
}

func TestCanceledTurnWithoutActivityKeepsOnlyStableInput(t *testing.T) {
	modelEntry, ok, err := newModelMessageEntry(1, "request-1", modelMessage("user", "stable input"))
	if err != nil || !ok {
		t.Fatalf("build model message entry: ok=%v err=%v", ok, err)
	}
	conversation := &ConversationFile{
		ConversationID: "33333333-3333-3333-3333-333333333333",
		Mode:           "agent",
		NextTurnSeq:    2,
		NextEntrySeq:   4,
		Entries: []HistoryEntry{
			modelEntry,
			newMetadataEntry(1, "request-1", "control", map[string]any{
				"status":        "canceled",
				"reason":        "user aborted",
				"replay_policy": cancelReplayPolicyKeepStableInput,
			}),
		},
	}

	filtered := sanitizeCanceledReplayEntries(conversation.Entries)
	if len(filtered) != 0 {
		t.Fatalf("non-canonical model_message input should not survive stable-input filtering: %d entries", len(filtered))
	}
}

func modelMessage(role string, content string) modeladapter.Message {
	return modeladapter.Message{Role: role, Content: content}
}

func countHistoryEntries(conversation *ConversationFile, turnSeq int64, kind string) int {
	count := 0
	if conversation == nil {
		return count
	}
	for _, entry := range conversation.Entries {
		if entry.TurnSeq == turnSeq && strings.TrimSpace(entry.Kind) == kind {
			count++
		}
	}
	return count
}

func countMetadataType(conversation *ConversationFile, turnSeq int64, metadataType string) int {
	count := 0
	if conversation == nil {
		return count
	}
	for _, entry := range conversation.Entries {
		if entry.TurnSeq != turnSeq || strings.TrimSpace(entry.Kind) != "metadata" {
			continue
		}
		var payload metadataPayload
		if json.Unmarshal(entry.Payload, &payload) == nil && strings.TrimSpace(payload.Type) == metadataType {
			count++
		}
	}
	return count
}

func TestFinalizeInterruptedTurnCompletesExistingToolCallWithoutDuplication(t *testing.T) {
	store := NewConversationFileStore(t.TempDir())
	conversationID := "44444444-4444-4444-4444-444444444444"
	conversation, err := store.CreateConversation(conversationID, agentv1.AgentMode_AGENT_MODE_AGENT, "", "", conversationID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	toolCall := newToolCallEntryWithProviderMetadata(
		1, "request-complete-tool", "tool-call-complete", "Shell", []byte(`{"command":"date"}`),
		"", "", "", "", "", nil, "", "", "", nil,
	)
	conversation, _, err = store.AppendEntries(conversationID, []HistoryEntry{toolCall})
	if err != nil {
		t.Fatalf("append existing tool call: %v", err)
	}

	service := &Service{store: store}
	stream := &ActiveStream{
		RequestID:              "request-complete-tool",
		ConversationID:         conversationID,
		TurnSeq:                1,
		CurrentModelCallID:     "model-call-complete",
		CheckpointConversation: conversation,
		PendingExecs: map[string]runtimecore.PendingExec{
			"exec-1": {
				ExecID:     "exec-1",
				ExecKind:   "shell",
				ToolCallID: "tool-call-complete",
				ArgsJSON:   []byte(`{"command":"date"}`),
			},
		},
		PendingInteractions: map[string]runtimecore.PendingInteraction{},
		PartialToolCalls:    map[string]interruptedToolCall{},
	}
	if err := service.finalizeInterruptedTurn(stream); err != nil {
		t.Fatalf("finalize interrupted complete tool call: %v", err)
	}
	conversation, err = store.LoadConversation(conversationID)
	if err != nil {
		t.Fatalf("load finalized conversation: %v", err)
	}
	if got := countHistoryEntries(conversation, 1, "tool_call"); got != 1 {
		t.Fatalf("tool_call count = %d, want 1", got)
	}
	if got := countHistoryEntries(conversation, 1, "tool_result"); got != 1 {
		t.Fatalf("tool_result count = %d, want 1", got)
	}
	assertToolReplay(t, mustProjectReplay(t, conversation), "tool-call-complete", "Shell", "date")
}

func TestFinalizeInterruptedTurnDoesNotCreateEmptyAssistant(t *testing.T) {
	store := NewConversationFileStore(t.TempDir())
	conversationID := "55555555-5555-5555-5555-555555555555"
	conversation, err := store.CreateConversation(conversationID, agentv1.AgentMode_AGENT_MODE_AGENT, "", "", conversationID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	service := &Service{store: store}
	stream := &ActiveStream{
		RequestID:              "request-empty",
		ConversationID:         conversationID,
		TurnSeq:                1,
		CurrentModelCallID:     "model-call-empty",
		CheckpointConversation: conversation,
		PendingExecs:           map[string]runtimecore.PendingExec{},
		PendingInteractions:    map[string]runtimecore.PendingInteraction{},
		PartialToolCalls:       map[string]interruptedToolCall{},
	}
	if err := service.finalizeInterruptedTurn(stream); err != nil {
		t.Fatalf("finalize empty interrupted turn: %v", err)
	}
	conversation, err = store.LoadConversation(conversationID)
	if err != nil {
		t.Fatalf("load empty conversation: %v", err)
	}
	if got := countHistoryEntries(conversation, 1, "assistant_text"); got != 0 {
		t.Fatalf("assistant_text count = %d, want 0", got)
	}
	if got := countMetadataType(conversation, 1, "interrupted_turn"); got != 0 {
		t.Fatalf("interrupted marker count = %d, want 0 without recoverable output", got)
	}
}

func TestBrokerReconnectStartsAtLatestCheckpoint(t *testing.T) {
	broker := NewStreamBroker()
	requestID := "request-reconnect"
	if _, err := broker.OpenStream(requestID, "conversation-reconnect", 1, "model", "model", agentv1.AgentMode_AGENT_MODE_AGENT, ""); err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := broker.Publish(requestID, StreamEvent{Message: buildCheckpointMessage(&agentv1.ConversationStateStructure{})}); err != nil {
		t.Fatalf("publish first checkpoint: %v", err)
	}
	if err := broker.Publish(requestID, StreamEvent{Message: buildTextDeltaMessage("obsolete delta")}); err != nil {
		t.Fatalf("publish obsolete delta: %v", err)
	}
	if err := broker.Publish(requestID, StreamEvent{Message: buildCheckpointMessage(&agentv1.ConversationStateStructure{})}); err != nil {
		t.Fatalf("publish latest checkpoint: %v", err)
	}

	subscriberID, _, cursor, err := broker.Subscribe(requestID)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer broker.Unsubscribe(requestID, subscriberID)
	events, err := broker.ReadFromCursor(requestID, cursor)
	if err != nil {
		t.Fatalf("read latest checkpoint: %v", err)
	}
	if len(events) != 1 || !isCheckpointStreamEvent(events[0]) {
		t.Fatalf("reconnect events = %#v, want only latest checkpoint", events)
	}
	broker.AdvanceSubscriber(requestID, subscriberID, events[0].Sequence+1)
	if replayed, err := broker.ReadFromCursor(requestID, events[0].Sequence+1); err != nil || len(replayed) != 0 {
		t.Fatalf("consumed events replayed: events=%#v err=%v", replayed, err)
	}
}

func mustProjectReplay(t *testing.T, conversation *ConversationFile) []modeladapter.Message {
	t.Helper()
	replay, err := NewHistoryProjector().ProjectPromptReplay(conversation)
	if err != nil {
		t.Fatalf("project prompt replay: %v", err)
	}
	return replay
}

func assertInterruptedReplay(t *testing.T, replay []modeladapter.Message) {
	t.Helper()
	var assistantSeen bool
	for _, message := range replay {
		if strings.TrimSpace(message.Role) == "assistant" && strings.Contains(message.Content, "visible partial answer") {
			assistantSeen = true
		}
	}
	if !assistantSeen {
		t.Fatalf("interrupted assistant text missing from replay: %#v", replay)
	}
	assertToolReplay(t, replay, "tool-call-1", "Shell", "printf partial")
}

func assertToolReplay(t *testing.T, replay []modeladapter.Message, toolCallID string, toolName string, argumentFragment string) {
	t.Helper()
	var toolCallSeen bool
	var toolResultSeen bool
	for _, message := range replay {
		switch strings.TrimSpace(message.Role) {
		case "assistant":
			for _, toolCall := range message.ToolCalls {
				if toolCall.ID == toolCallID && toolCall.Function.Name == toolName && strings.Contains(toolCall.Function.Arguments, argumentFragment) {
					toolCallSeen = true
				}
			}
		case "tool":
			if message.ToolCallID == toolCallID && strings.Contains(message.Content, "interrupted") {
				toolResultSeen = true
			}
		}
	}
	if !toolCallSeen || !toolResultSeen {
		t.Fatalf("interrupted tool replay missing content: tool_call=%v tool_result=%v replay=%#v", toolCallSeen, toolResultSeen, replay)
	}
}
