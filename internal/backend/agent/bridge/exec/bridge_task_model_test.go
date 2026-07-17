package execbridge

import (
	"encoding/json"
	"strings"
	"testing"

	runtimecore "cursor/internal/backend/agent/core"
)

func TestOpenTaskModelResolution(t *testing.T) {
	tests := []struct {
		name            string
		subagentType    string
		taskModelID     string
		parentModelID   string
		resolvedModelID string
		overrides       map[string]runtimecore.SubagentModelOverrideSelection
		wantModelID     string
		wantError       string
	}{
		{
			name:            "explicit model wins over model override",
			subagentType:    "explore",
			taskModelID:     "explicit-model",
			parentModelID:   "default",
			resolvedModelID: "resolved-parent",
			overrides: map[string]runtimecore.SubagentModelOverrideSelection{
				"explore": {Selection: "model", ModelID: "override-model"},
			},
			wantModelID: "explicit-model",
		},
		{
			name:            "model override wins without explicit model",
			subagentType:    "browser-use",
			parentModelID:   "auto",
			resolvedModelID: "resolved-parent",
			overrides: map[string]runtimecore.SubagentModelOverrideSelection{
				"browser-use": {Selection: "model", ModelID: "override-model"},
			},
			wantModelID: "override-model",
		},
		{
			name:            "inherit resolves parent meta alias",
			subagentType:    "shell",
			parentModelID:   "fast",
			resolvedModelID: "resolved-parent",
			overrides: map[string]runtimecore.SubagentModelOverrideSelection{
				"shell": {Selection: "inherit"},
			},
			wantModelID: "resolved-parent",
		},
		{
			name:            "unconfigured child inherits resolved parent",
			subagentType:    "custom-reviewer",
			parentModelID:   "default",
			resolvedModelID: "resolved-parent",
			wantModelID:     "resolved-parent",
		},
		{
			name:          "concrete parent is fallback without resolver",
			subagentType:  "explore",
			parentModelID: "concrete-parent",
			wantModelID:   "concrete-parent",
		},
		{
			name:         "explicit meta alias remains explicit",
			subagentType: "explore",
			taskModelID:  "fast",
			wantModelID:  "fast",
		},
		{
			name:            "disabled override rejects explicit model",
			subagentType:    "explore",
			taskModelID:     "explicit-model",
			parentModelID:   "default",
			resolvedModelID: "resolved-parent",
			overrides: map[string]runtimecore.SubagentModelOverrideSelection{
				"explore": {Selection: "disabled"},
			},
			wantError: "disabled by model override",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := map[string]any{
				"subagent_type": test.subagentType,
				"prompt":        "inspect the repository",
			}
			if test.taskModelID != "" {
				args["model"] = test.taskModelID
			}
			argsJSON, err := json.Marshal(args)
			if err != nil {
				t.Fatalf("marshal task args: %v", err)
			}

			message, _, err := NewBridge().OpenExec(OpenExecContext{
				ConversationID:         "parent-conversation",
				ModelID:                test.parentModelID,
				ResolvedModelID:        test.resolvedModelID,
				SubagentModelOverrides: test.overrides,
			}, runtimecore.ToolInvocation{
				CallID:   "tool-call-1",
				ToolName: "Task",
				ArgsJSON: argsJSON,
			})
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("OpenExec error = %v, want substring %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("OpenExec: %v", err)
			}
			if message == nil || message.GetExecServerMessage() == nil {
				t.Fatalf("OpenExec returned no exec server message")
			}
			subagentArgs := message.GetExecServerMessage().GetSubagentArgs()
			if subagentArgs == nil {
				t.Fatalf("OpenExec returned no subagent args: %T", message.GetMessage())
			}
			if got := subagentArgs.GetModelId(); got != test.wantModelID {
				t.Fatalf("SubagentArgs.model_id = %q, want %q", got, test.wantModelID)
			}
		})
	}
}
