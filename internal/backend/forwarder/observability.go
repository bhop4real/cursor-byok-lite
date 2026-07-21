package forwarder

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func (recorder *artifactRecorder) observeProviderRequest(requestID string, modelCallID string, conversationID string, payload map[string]any) {
	if recorder == nil {
		return
	}
	key := artifactSessionKey(requestID, modelCallID)
	now := time.Now().UTC()
	recorder.mu.Lock()
	session := recorder.sessions[key]
	session.conversationID = firstNonEmpty(session.conversationID, conversationID)
	session.requestObservedAt = now
	recorder.sessions[key] = session
	recorder.mu.Unlock()

	body, _ := payload["body"].(map[string]any)
	metrics := map[string]any{
		"model_call_id":         strings.TrimSpace(modelCallID),
		"provider":              strings.TrimSpace(readStringValue(payload["provider"])),
		"model":                 strings.TrimSpace(readStringValue(payload["model"])),
		"replay_message_count":  replayMessageCountFromRequestPayload(payload),
		"stable_message_count":  readInt64Value(payload["stable_message_count"]),
		"tool_count":            collectionLength(payload["tools_summary"]),
		"body_bytes":            encodedJSONSize(payload["body"]),
		"tool_schema_bytes":     encodedJSONSize(body["tools"]),
		"message_bytes":         encodedJSONSize(firstNonNil(body["messages"], body["input"])),
		"system_bytes":          encodedJSONSize(firstNonNil(body["system"], body["instructions"])),
		"compile_summary_bytes": len([]byte(readStringValue(payload["compile_summary"]))),
	}
	if knobs, ok := payload["request_knobs"].(map[string]any); ok {
		metrics["prompt_tokens_estimate"] = readInt64Value(knobs["compiled_prompt_tokens_estimate"])
		metrics["context_window_tokens"] = readInt64Value(knobs["context_window_tokens"])
		metrics["cache_frontier"] = knobs["cache_frontier"]
	}
	recorder.debug.LogProvider(context.Background(), requestID, conversationID, "provider_request_metrics", metrics)
}

func (recorder *artifactRecorder) observeProviderChunk(requestID string, modelCallID string, conversationID string, byteLen int) {
	if recorder == nil {
		return
	}
	key := artifactSessionKey(requestID, modelCallID)
	now := time.Now().UTC()
	recorder.mu.Lock()
	session := recorder.sessions[key]
	first := session.firstResponseObservedAt.IsZero()
	if first {
		session.firstResponseObservedAt = now
	}
	session.lastResponseObservedAt = now
	session.responseChunkCount++
	chunkCount := session.responseChunkCount
	requestObservedAt := session.requestObservedAt
	recorder.sessions[key] = session
	recorder.mu.Unlock()
	if !first {
		return
	}
	recorder.debug.LogProvider(context.Background(), requestID, conversationID, "provider_first_response", map[string]any{
		"model_call_id": strings.TrimSpace(modelCallID),
		"ttfb_ms":       durationMillis(requestObservedAt, now),
		"chunk_bytes":   byteLen,
		"chunk_count":   chunkCount,
	})
}

func (recorder *artifactRecorder) observeProviderSummary(requestID string, modelCallID string, conversationID string, payload map[string]any) {
	if recorder == nil {
		return
	}
	key := artifactSessionKey(requestID, modelCallID)
	recorder.mu.Lock()
	session := recorder.sessions[key]
	recorder.mu.Unlock()
	fields := map[string]any{
		"model_call_id":      strings.TrimSpace(modelCallID),
		"provider":           strings.TrimSpace(readStringValue(payload["provider"])),
		"model":              strings.TrimSpace(readStringValue(payload["model"])),
		"finish_reason":      strings.TrimSpace(readStringValue(payload["finish_reason"])),
		"error":              strings.TrimSpace(readStringValue(payload["error"])),
		"ttft_ms":            readInt64Value(payload["ttft_ms"]),
		"duration_ms":        readInt64Value(payload["duration_ms"]),
		"input_tokens":       readInt64Value(payload["input_tokens"]),
		"output_tokens":      readInt64Value(payload["output_tokens"]),
		"cache_read_tokens":  readInt64Value(payload["cache_read_tokens"]),
		"cache_write_tokens": readInt64Value(payload["cache_write_tokens"]),
		"response_chunks":    session.responseChunkCount,
		"last_event_at":      session.lastResponseObservedAt,
	}
	recorder.debug.LogProvider(context.Background(), requestID, conversationID, "provider_call_summary", fields)
}

func encodedJSONSize(value any) int {
	if value == nil {
		return 0
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return len(encoded)
}

func collectionLength(value any) int {
	switch items := value.(type) {
	case []string:
		return len(items)
	case []any:
		return len(items)
	case []map[string]any:
		return len(items)
	default:
		return 0
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func durationMillis(start time.Time, end time.Time) int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}
