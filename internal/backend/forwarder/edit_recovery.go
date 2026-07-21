package forwarder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"cursor/gen/agentv1"
	runtimecore "cursor/internal/backend/agent/core"
)

const (
	hiddenEditStageTimeout = 90 * time.Second
	hiddenEditCloseGrace   = 1500 * time.Millisecond
)

func (service *Service) scheduleHiddenEditStageDeadline(stream *ActiveStream, pending runtimecore.PendingExec, delay time.Duration, reason string) {
	if service == nil || stream == nil || !isHiddenEditExecKind(pending.ExecKind) || strings.TrimSpace(pending.ExecID) == "" {
		return
	}
	if delay <= 0 {
		delay = hiddenEditStageTimeout
	}
	service.scheduleStreamTimer(
		stream,
		providerTimerKey(streamTimerHiddenEditStage, pending.ExecID),
		delay,
		streamTimerHiddenEditStage,
		pending.ExecID,
		pending.MessageID,
		reason,
	)
	service.logHiddenEditStage(stream, pending, "stage_wait_started", map[string]any{
		"deadline_ms": delay.Milliseconds(),
		"reason":      strings.TrimSpace(reason),
	})
}

func isHiddenEditExecKind(kind string) bool {
	return isHiddenWriteExecKind(kind) || isHiddenPatchEditExecKind(kind)
}

func (service *Service) observeHiddenEditControl(stream *ActiveStream, pending runtimecore.PendingExec, message *agentv1.ExecClientControlMessage) bool {
	if service == nil || stream == nil || message == nil || !isHiddenEditExecKind(pending.ExecKind) {
		return false
	}
	switch message.GetMessage().(type) {
	case *agentv1.ExecClientControlMessage_Heartbeat:
		service.scheduleHiddenEditStageDeadline(stream, pending, hiddenEditStageTimeout, "heartbeat")
		return true
	case *agentv1.ExecClientControlMessage_StreamClose:
		markExecTransportClosed(stream, pending)
		service.scheduleHiddenEditStageDeadline(stream, pending, hiddenEditCloseGrace, "stream_close")
		return true
	default:
		return false
	}
}

func (service *Service) completeHiddenEditStageObservation(stream *ActiveStream, pending runtimecore.PendingExec, resultKind string) {
	if stream == nil || !isHiddenEditExecKind(pending.ExecKind) {
		return
	}
	clearStreamTimer(stream, providerTimerKey(streamTimerHiddenEditStage, pending.ExecID))
	service.logHiddenEditStage(stream, pending, "stage_result_received", map[string]any{
		"result_kind": strings.TrimSpace(resultKind),
		"elapsed_ms":  elapsedSinceMillis(pending.OpenedAt),
	})
}

func (service *Service) recoverHiddenEditStage(stream *ActiveStream, pending runtimecore.PendingExec, reason string) error {
	if service == nil || stream == nil || !isHiddenEditExecKind(pending.ExecKind) {
		return nil
	}
	service.logHiddenEditStage(stream, pending, "stage_deadline_reached", map[string]any{
		"reason":     strings.TrimSpace(reason),
		"elapsed_ms": elapsedSinceMillis(pending.OpenedAt),
	})

	switch strings.TrimSpace(pending.ExecKind) {
	case writeReadExecKind:
		payload, err := decodePendingWritePayload(pending.ArgsJSON)
		if err != nil {
			return err
		}
		markExecCompleted(stream, pending)
		return service.finishWriteOperation(stream, pending.ToolCallID, pending.ModelCallID, pending.ProviderPass, pending.ReasoningContent, payload.VisibleArgs, buildEditErrorResult(payload.ResolvedPath, "write pre-read timed out before any mutation was dispatched"))
	case writeWriteExecKind:
		payload, err := decodePendingWritePayload(pending.ArgsJSON)
		if err != nil {
			return err
		}
		markExecCompleted(stream, pending)
		return service.startHiddenWritePostRead(stream, pending.ToolCallID, pending.ModelCallID, pending.ProviderPass, pending.ReasoningContent, pending.ReasoningSignature, pending.ReasoningSignatureSource, payload)
	case writePostReadExecKind:
		payload, err := decodePendingWritePayload(pending.ArgsJSON)
		if err != nil {
			return err
		}
		markExecCompleted(stream, pending)
		args := payload.VisibleArgs
		args.Path = firstNonEmpty(strings.TrimSpace(payload.ResolvedPath), strings.TrimSpace(args.Path))
		return service.finishWriteOperation(stream, pending.ToolCallID, pending.ModelCallID, pending.ProviderPass, pending.ReasoningContent, args, buildEditErrorResult(args.Path, "write result was lost and bounded read-back verification timed out; commit state is unknown"))
	case patchEditReadExecKindName:
		payload, err := decodePendingPatchEditPayload(pending.ArgsJSON)
		if err != nil {
			return err
		}
		markExecCompleted(stream, pending)
		return service.finishPatchEditOperation(stream, pending.ToolCallID, pending.ModelCallID, pending.ProviderPass, pending.ReasoningContent, payload, buildEditErrorResult(payload.ResolvedPath, "patch edit pre-read timed out before any mutation was dispatched"))
	case patchEditWriteExecKindName:
		payload, err := decodePendingPatchEditPayload(pending.ArgsJSON)
		if err != nil {
			return err
		}
		markExecCompleted(stream, pending)
		return service.startHiddenPatchEditPostRead(stream, pending.ToolCallID, pending.ModelCallID, pending.ProviderPass, pending.ReasoningContent, pending.ReasoningSignature, pending.ReasoningSignatureSource, payload)
	case patchEditPostReadExecKindName:
		payload, err := decodePendingPatchEditPayload(pending.ArgsJSON)
		if err != nil {
			return err
		}
		markExecCompleted(stream, pending)
		return service.finishPatchEditOperation(stream, pending.ToolCallID, pending.ModelCallID, pending.ProviderPass, pending.ReasoningContent, payload, buildEditErrorResult(payload.ResolvedPath, "patch edit write result was lost and bounded read-back verification timed out; commit state is unknown"))
	default:
		return fmt.Errorf("unsupported hidden edit recovery kind: %s", pending.ExecKind)
	}
}

func successfulWriteResultWithWarning(path string, beforeContent string, afterContent string, warning string) *agentv1.EditResult {
	result := buildSuccessfulWriteResult(path, beforeContent, afterContent)
	appendEditResultWarning(result, warning)
	return result
}

func appendEditResultWarning(result *agentv1.EditResult, warning string) {
	if result == nil || result.GetSuccess() == nil || strings.TrimSpace(warning) == "" {
		return
	}
	success := result.GetSuccess()
	message := strings.TrimSpace(warning)
	if current := strings.TrimSpace(success.GetMessage()); current != "" {
		message = current + "; " + message
	}
	success.Message = proto.String(message)
}

func (service *Service) logHiddenEditStage(stream *ActiveStream, pending runtimecore.PendingExec, eventName string, fields map[string]any) {
	if service == nil || service.debug == nil || stream == nil {
		return
	}
	payload := map[string]any{
		"provider_pass": pending.ProviderPass,
		"model_call_id": strings.TrimSpace(pending.ModelCallID),
		"tool_call_id":  strings.TrimSpace(pending.ToolCallID),
		"exec_id":       strings.TrimSpace(pending.ExecID),
		"message_id":    pending.MessageID,
		"exec_kind":     strings.TrimSpace(pending.ExecKind),
		"opened_at":     pending.OpenedAt,
	}
	for key, value := range fields {
		payload[key] = value
	}
	service.debug.LogRuntime(context.Background(), stream.RequestID, stream.ConversationID, eventName, payload)
}

func elapsedSinceMillis(start time.Time) int64 {
	if start.IsZero() {
		return 0
	}
	value := time.Since(start).Milliseconds()
	if value < 0 {
		return 0
	}
	return value
}
