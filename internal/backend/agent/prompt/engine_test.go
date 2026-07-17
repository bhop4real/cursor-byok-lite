package promptengine

import (
	"strings"
	"testing"
	"unicode"

	"cursor/gen/agentv1"
)

func TestCompileDefaultsUnspecifiedModeToEnglishAgentPrompt(t *testing.T) {
	t.Parallel()

	compiled, err := NewEngine().Compile(CompileInput{
		Mode:               agentv1.AgentMode_AGENT_MODE_UNSPECIFIED,
		RequestedModelName: "test-model",
	})
	if err != nil {
		t.Fatalf("compile prompt: %v", err)
	}
	if compiled.Mode != agentv1.AgentMode_AGENT_MODE_AGENT {
		t.Fatalf("compiled mode = %s, want %s", compiled.Mode.String(), agentv1.AgentMode_AGENT_MODE_AGENT.String())
	}
	if len(compiled.Messages) == 0 || compiled.Messages[0].Role != "system" {
		t.Fatalf("compiled prompt is missing the system message: %#v", compiled.Messages)
	}

	systemPrompt := compiled.Messages[0].Content
	for _, required := range []string{
		"You are a programming agent in Cursor IDE, powered by test-model.",
		"<execution_discipline>",
		"You are currently in Agent mode.",
	} {
		if !strings.Contains(systemPrompt, required) {
			t.Fatalf("default system prompt is missing %q", required)
		}
	}
	for _, value := range systemPrompt {
		if unicode.Is(unicode.Han, value) {
			t.Fatalf("default system prompt contains non-English Han character %q", value)
		}
	}
}
