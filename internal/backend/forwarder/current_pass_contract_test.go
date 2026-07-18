package forwarder

import (
	"encoding/json"
	"strings"
	"testing"

	"cursor/gen/agentv1"
	modeladapter "cursor/internal/backend/agent/model"
)

type currentPassContractTestCatalog struct{}

func (currentPassContractTestCatalog) Load(agentv1.AgentMode, string) ([]json.RawMessage, []string, error) {
	return nil, nil, nil
}

func TestCurrentPassExecutionContractIsLateOnInitialAndResumedPasses(t *testing.T) {
	compiler := NewPromptCompiler(NewHistoryProjector(), currentPassContractTestCatalog{}, NewReminderInjector(), nil)
	conversation := &ConversationFile{
		ConversationID:   "contract-initial",
		Mode:             "agent",
		CurrentRequestID: "request-1",
		CurrentTurnSeq:   1,
		NextTurnSeq:      2,
		NextEntrySeq:     1,
	}

	initial, err := compiler.CompileWithReplay(conversation, agentv1.AgentMode_AGENT_MODE_AGENT, "finish the task", "test-model", []modeladapter.Message{{Role: "user", Content: "finish the task"}})
	if err != nil {
		t.Fatalf("compile initial pass: %v", err)
	}
	assertSingleLateCurrentPassContract(t, initial.Messages, "calls=0")

	conversation.Entries = append(conversation.Entries, currentPassContractToolEntries(
		1,
		"request-1",
		"edit-1",
		"PatchEdit",
		`{"path":"/workspace/final.go","old_string":"old","new_string":"new"}`,
		`{"success":{"path":"/workspace/final.go"}}`,
	)...)
	replay, err := NewHistoryProjector().ProjectPromptReplay(conversation)
	if err != nil {
		t.Fatalf("project resumed replay: %v", err)
	}
	resumed, err := compiler.CompileWithReplay(conversation, agentv1.AgentMode_AGENT_MODE_AGENT, "finish the task", "test-model", replay)
	if err != nil {
		t.Fatalf("compile resumed pass: %v", err)
	}
	assertSingleLateCurrentPassContract(t, resumed.Messages, "settled_paths=1")
	if !strings.Contains(resumed.Messages[len(resumed.Messages)-1].Content, "/workspace/final.go") ||
		!strings.Contains(resumed.Messages[len(resumed.Messages)-1].Content, "do not edit again without new defect evidence") {
		t.Fatalf("resumed contract lacks successful-edit finality: %s", resumed.Messages[len(resumed.Messages)-1].Content)
	}
}

func TestCurrentPassExecutionContractDetectsRepeatedRejectedCall(t *testing.T) {
	arguments := `{"path":"/workspace/final.go","old_string":"missing","new_string":"new"}`
	conversation := &ConversationFile{
		ConversationID:   "contract-rejected",
		Mode:             "agent",
		CurrentRequestID: "request-2",
		CurrentTurnSeq:   1,
		NextTurnSeq:      2,
		NextEntrySeq:     5,
		Entries: append(
			currentPassContractToolEntries(1, "request-2", "edit-1", "PatchEdit", arguments, `{"error":{"error":"old_string not found"}}`),
			currentPassContractToolEntries(1, "request-2", "edit-2", "PatchEdit", arguments, `{"rejected":{"reason":"unchanged retry"}}`)...,
		),
	}

	contract := currentPassExecutionContract(conversation, nil)
	for _, expected := range []string{
		"failed_or_rejected=2",
		"repeated_invocations=1",
		"PatchEdit args#",
		"repeated=2",
		"Never repeat an identical rejected invocation",
	} {
		if !strings.Contains(contract, expected) {
			t.Fatalf("contract missing %q: %s", expected, contract)
		}
	}
}

func TestCurrentPassExecutionContractExcludesHistoricalReplayWithoutCurrentTurnCalls(t *testing.T) {
	conversation := &ConversationFile{
		ConversationID:   "contract-history",
		Mode:             "agent",
		CurrentRequestID: "request-current",
		CurrentTurnSeq:   2,
		NextTurnSeq:      3,
		NextEntrySeq:     1,
	}
	replay := []modeladapter.Message{
		{
			Role: "assistant",
			ToolCalls: []modeladapter.ToolCallDescriptor{{
				ID:   "historical-edit",
				Type: "function",
				Function: modeladapter.ToolCallFunctionShape{
					Name:      "PatchEdit",
					Arguments: `{"path":"/workspace/historical.go","old_string":"old","new_string":"new"}`,
				},
			}},
		},
		{Role: "tool", Name: "PatchEdit", ToolCallID: "historical-edit", Content: `{"success":{"path":"/workspace/historical.go"}}`},
	}

	contract := currentPassExecutionContract(conversation, replay)
	if !strings.Contains(contract, "calls=0") {
		t.Fatalf("historical replay leaked into current-turn call count: %s", contract)
	}
	if strings.Contains(contract, "/workspace/historical.go") {
		t.Fatalf("historical path leaked into current-turn ledger: %s", contract)
	}
}

func TestCurrentPassExecutionContractIsNotPersistedAsPromptContext(t *testing.T) {
	compiler := NewPromptCompiler(NewHistoryProjector(), currentPassContractTestCatalog{}, NewReminderInjector(), nil)
	conversation := &ConversationFile{
		ConversationID:   "contract-persistence",
		Mode:             "agent",
		CurrentRequestID: "request-3",
		CurrentTurnSeq:   1,
		NextTurnSeq:      2,
		NextEntrySeq:     1,
	}
	entryCount := len(conversation.Entries)

	contexts, err := compiler.DerivePromptContexts(conversation, agentv1.AgentMode_AGENT_MODE_AGENT, "finish")
	if err != nil {
		t.Fatalf("derive prompt contexts: %v", err)
	}
	for _, context := range contexts {
		if strings.Contains(context.Message.Content, "CURRENT-PASS EXECUTION CONTRACT") {
			t.Fatalf("execution contract must not be persisted as prompt context: %#v", context)
		}
	}
	for pass := 0; pass < 2; pass++ {
		compiled, err := compiler.CompileWithReplay(conversation, agentv1.AgentMode_AGENT_MODE_AGENT, "finish", "test-model", nil)
		if err != nil {
			t.Fatalf("compile pass %d: %v", pass+1, err)
		}
		assertSingleLateCurrentPassContract(t, compiled.Messages, "calls=0")
	}
	if len(conversation.Entries) != entryCount {
		t.Fatalf("compile mutated conversation entries: got %d, want %d", len(conversation.Entries), entryCount)
	}
}

func currentPassContractToolEntries(turnSeq int64, requestID string, callID string, toolName string, arguments string, resultText string) []HistoryEntry {
	toolCallPayload, _ := json.Marshal(toolCallEntryPayload{
		ToolCallID: callID,
		ToolName:   toolName,
		Arguments:  arguments,
	})
	toolResultPayload, _ := json.Marshal(toolResultEntryPayload{
		ToolCallID: callID,
		ToolName:   toolName,
		Arguments:  arguments,
		ResultText: resultText,
	})
	return []HistoryEntry{
		{TurnSeq: turnSeq, RequestID: requestID, Role: "assistant", Kind: "tool_call", ToolCallID: callID, Payload: toolCallPayload},
		{TurnSeq: turnSeq, RequestID: requestID, Role: "tool", Kind: "tool_result", ToolCallID: callID, Payload: toolResultPayload},
	}
}

func assertSingleLateCurrentPassContract(t *testing.T, messages []modeladapter.Message, expected string) {
	t.Helper()
	if len(messages) == 0 {
		t.Fatal("compiled messages are empty")
	}
	count := 0
	for _, message := range messages {
		if strings.Contains(message.Content, "CURRENT-PASS EXECUTION CONTRACT") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("execution contract count = %d, want 1", count)
	}
	last := messages[len(messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, expected) {
		t.Fatalf("execution contract is not the late current-pass message containing %q: %#v", expected, last)
	}
}