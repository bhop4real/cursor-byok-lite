package modeladapter

import (
	"crypto/sha256"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"cursor/gen/agentv1"
)

func emitCreatePlanToolProgress(
	sink func(ModelEvent) error,
	provider string,
	model string,
	callID string,
	args *agentv1.CreatePlanArgs,
	argsTextDelta string,
	lastSnapshot *string,
) error {
	if sink == nil || lastSnapshot == nil {
		return nil
	}
	trimmedCallID := strings.TrimSpace(callID)
	if trimmedCallID == "" {
		return nil
	}
	if !hasCreatePlanArgsProgress(args) {
		return nil
	}
	signatureBytes, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(args)
	if err != nil {
		return err
	}
	signatureSum := sha256.Sum256(signatureBytes)
	signature := string(signatureSum[:])
	if signature == "" || signature == *lastSnapshot {
		return nil
	}
	*lastSnapshot = signature
	if err := sink(ModelEvent{
		Kind:          ModelEventKindPartialToolCall,
		OccurredAt:    time.Now().UTC(),
		Provider:      provider,
		Model:         model,
		ToolCallID:    trimmedCallID,
		ArgsTextDelta: argsTextDelta,
		ToolCall: &agentv1.ToolCall{
			Tool: &agentv1.ToolCall_CreatePlanToolCall{
				CreatePlanToolCall: &agentv1.CreatePlanToolCall{
					Args: args,
				},
			},
		},
	}); err != nil {
		return err
	}
	return nil
}

func hasCreatePlanArgsProgress(args *agentv1.CreatePlanArgs) bool {
	if args == nil {
		return false
	}
	return args.GetPlan() != "" ||
		args.GetOverview() != "" ||
		args.GetName() != "" ||
		args.GetIsProject() ||
		len(args.GetTodos()) > 0 ||
		len(args.GetPhases()) > 0
}
