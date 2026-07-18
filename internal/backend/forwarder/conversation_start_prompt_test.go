package forwarder

import (
	"strings"
	"testing"

	"cursor/gen/agentv1"
	promptassets "cursor/prompt"
)

func TestEnsureConversationStartPromptUsesEarliestModeAndModel(t *testing.T) {
	modeEntry, err := newModeMetadataEntry(1, "request-1", agentv1.AgentMode_AGENT_MODE_PLAN, true, ModeSourceUserMessage)
	if err != nil {
		t.Fatalf("build initial mode entry: %v", err)
	}
	conversation := &ConversationFile{
		ConversationID: "start-prompt",
		Mode:           "agent",
		Entries: []HistoryEntry{
			modeEntry,
			newMetadataEntry(1, "request-1", "run_request", map[string]any{"model_name": "initial-model"}),
		},
	}
	if err := ensureConversationStartPrompt(conversation, agentv1.AgentMode_AGENT_MODE_AGENT, "current-model"); err != nil {
		t.Fatalf("capture conversation-start prompt: %v", err)
	}
	rawPrompt, err := promptassets.ReadPrompt(promptassets.ModePlan)
	if err != nil {
		t.Fatalf("read expected plan prompt: %v", err)
	}
	expectedContent := strings.TrimSpace(sanitizePromptAsset(rawPrompt, "initial-model"))
	if conversation.ConversationStartPromptMode != "plan" {
		t.Fatalf("captured mode = %q, want plan", conversation.ConversationStartPromptMode)
	}
	if conversation.ConversationStartPromptPath != "plan/prompt.md" {
		t.Fatalf("captured path = %q, want plan/prompt.md", conversation.ConversationStartPromptPath)
	}
	if conversation.ConversationStartPromptModel != "initial-model" {
		t.Fatalf("captured model = %q, want initial-model", conversation.ConversationStartPromptModel)
	}
	if conversation.ConversationStartPromptContent != expectedContent {
		t.Fatal("captured content does not equal the rendered conversation-start prompt")
	}

	original := conversation.ConversationStartPromptContent
	if err := ensureConversationStartPrompt(conversation, agentv1.AgentMode_AGENT_MODE_DEBUG, "later-model"); err != nil {
		t.Fatalf("repeat capture: %v", err)
	}
	if conversation.ConversationStartPromptContent != original || conversation.ConversationStartPromptMode != "plan" {
		t.Fatal("conversation-start prompt changed after a later mode/model change")
	}
}

func TestComposeConversationStartPromptSummaryContainsPromptExactlyOnce(t *testing.T) {
	prompt := conversationStartPromptSnapshot{
		Mode:    "agent",
		Path:    "agent/prompt.md",
		Model:   "initial-model",
		Content: "original prompt body",
	}
	composed := composeConversationStartPromptSummary(prompt, "durable conversation facts")
	for _, required := range []string{
		"<conversation_start_prompt_md>",
		"path: agent/prompt.md",
		"mode: agent",
		"model: initial-model",
		"original prompt body",
		"durable conversation facts",
	} {
		if !strings.Contains(composed, required) {
			t.Fatalf("composed summary is missing %q: %s", required, composed)
		}
	}
	if repeated := composeConversationStartPromptSummary(prompt, composed); repeated != composed {
		t.Fatal("conversation-start prompt was duplicated on repeated summary composition")
	}
}

func TestMergeConversationMetadataKeepsCapturedStartPromptImmutable(t *testing.T) {
	target := &ConversationFile{
		ConversationStartPromptMode:    "agent",
		ConversationStartPromptPath:    "agent/prompt.md",
		ConversationStartPromptModel:   "first-model",
		ConversationStartPromptContent: "first prompt",
	}
	source := &ConversationFile{
		ConversationStartPromptMode:    "debug",
		ConversationStartPromptPath:    "debug/prompt.md",
		ConversationStartPromptModel:   "later-model",
		ConversationStartPromptContent: "later prompt",
	}
	mergeConversationMetadata(target, source)
	if target.ConversationStartPromptContent != "first prompt" || target.ConversationStartPromptMode != "agent" {
		t.Fatal("metadata merge overwrote the immutable conversation-start prompt")
	}

	emptyTarget := &ConversationFile{}
	mergeConversationMetadata(emptyTarget, source)
	if emptyTarget.ConversationStartPromptContent != "later prompt" || emptyTarget.ConversationStartPromptPath != "debug/prompt.md" {
		t.Fatal("metadata merge did not propagate a missing conversation-start prompt")
	}
}
