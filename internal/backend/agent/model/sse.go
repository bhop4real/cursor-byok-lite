package modeladapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

const (
	providerStreamEvidenceLimit = 512
	providerStreamErrorMarker   = "cursor-byok-provider-stream-error:"
)

// ProviderStreamError classifies stream termination without losing provider evidence.
type ProviderStreamError struct {
	Provider      string
	RequestID     string
	ModelCallID   string
	EventType     string
	TerminalSeen  bool
	OutputEscaped bool
	PayloadPrefix string
	Retryable     bool
	Cause         error
}

func (err *ProviderStreamError) Error() string {
	if err == nil {
		return "provider stream error"
	}
	parts := []string{"provider stream failed"}
	if value := strings.TrimSpace(err.Provider); value != "" {
		parts = append(parts, "provider="+value)
	}
	if value := strings.TrimSpace(err.RequestID); value != "" {
		parts = append(parts, "request_id="+value)
	}
	if value := strings.TrimSpace(err.ModelCallID); value != "" {
		parts = append(parts, "model_call_id="+value)
	}
	if value := strings.TrimSpace(err.EventType); value != "" {
		parts = append(parts, "event="+value)
	}
	parts = append(parts,
		fmt.Sprintf("terminal_seen=%t", err.TerminalSeen),
		fmt.Sprintf("output_escaped=%t", err.OutputEscaped),
	)
	message := strings.Join(parts, " ")
	if err.Cause != nil {
		message += ": " + err.Cause.Error()
	}
	if evidence := strings.TrimSpace(err.PayloadPrefix); evidence != "" {
		message += fmt.Sprintf(" (payload_prefix=%q)", evidence)
	}
	return message
}

func (err *ProviderStreamError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

type providerStreamErrorEnvelope struct {
	Provider      string `json:"provider,omitempty"`
	EventType     string `json:"event_type,omitempty"`
	TerminalSeen  bool   `json:"terminal_seen"`
	PayloadPrefix string `json:"payload_prefix,omitempty"`
	Retryable     bool   `json:"retryable"`
	Cause         string `json:"cause,omitempty"`
}

// canonicalSSEReader validates and normalizes complete provider events before
// adapters see them. A truncated or malformed event is converted into a
// provider-native error event so existing adapter loops observe the failure
// before they can finalize partially accumulated tool calls.
func canonicalSSEReader(body io.Reader, provider string) io.Reader {
	reader, writer := io.Pipe()
	go func() {
		err := copyCanonicalSSE(writer, body, strings.TrimSpace(provider))
		_ = writer.CloseWithError(err)
	}()
	return reader
}

func copyCanonicalSSE(dst io.Writer, src io.Reader, provider string) error {
	if src == nil {
		return writeProviderStreamErrorEvent(dst, &ProviderStreamError{
			Provider:  provider,
			Retryable: true,
			Cause:     io.ErrUnexpectedEOF,
		})
	}

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), openAIStreamMaxTokenSize)
	eventType := ""
	dataLines := make([]string, 0, 4)
	terminalSeen := false
	semanticOutputSeen := false
	lastEventType := ""
	lastPayload := ""

	dispatch := func() (bool, error) {
		defer func() {
			eventType = ""
			dataLines = dataLines[:0]
		}()
		if len(dataLines) == 0 {
			return false, nil
		}

		payload := strings.Join(dataLines, "\n")
		if isProviderSSEKeepalive(eventType, payload) {
			return false, nil
		}
		resolvedEventType := firstNonEmptyString(providerSSEEventType(eventType, payload), "data")
		lastEventType = resolvedEventType
		lastPayload = boundedProviderStreamEvidence(payload)

		normalizedPayload, err := normalizeProviderSSEPayload(payload)
		if err != nil {
			streamErr := &ProviderStreamError{
				Provider:      provider,
				EventType:     resolvedEventType,
				TerminalSeen:  terminalSeen,
				PayloadPrefix: lastPayload,
				Retryable:     false,
				Cause:         fmt.Errorf("malformed provider SSE JSON: %w", err),
			}
			if writeErr := writeProviderStreamErrorEvent(dst, streamErr); writeErr != nil {
				return false, writeErr
			}
			return true, nil
		}
		if providerSSEEventCarriesSemanticOutput(provider, eventType, normalizedPayload) {
			semanticOutputSeen = true
		}
		terminal := isProviderTerminalSSEEvent(provider, eventType, normalizedPayload)
		stop := shouldStopAfterProviderSSEEvent(provider, eventType, normalizedPayload)
		if terminal || stop {
			terminalSeen = true
		}
		if err := writeCanonicalSSEEvent(dst, eventType, normalizedPayload); err != nil {
			return false, err
		}
		return stop, nil
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			stop, err := dispatch()
			if err != nil {
				return err
			}
			if stop {
				return nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			field = line
			value = ""
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			eventType = strings.TrimSpace(value)
		case "data":
			dataLines = append(dataLines, value)
		}
	}

	pendingPayload := strings.Join(dataLines, "\n")
	if len(dataLines) == 0 && terminalSeen {
		return nil
	}
	if recoveredPayload, ok := recoverTruncatedOpenAICompletedEvent(provider, eventType, pendingPayload, semanticOutputSeen); ok {
		return writeCanonicalSSEEvent(dst, "response.completed", recoveredPayload)
	}
	failure := &ProviderStreamError{
		Provider:      provider,
		EventType:     firstNonEmptyString(providerSSEEventType(eventType, pendingPayload), lastEventType),
		TerminalSeen:  terminalSeen,
		PayloadPrefix: firstNonEmptyString(boundedProviderStreamEvidence(pendingPayload), lastPayload),
		Retryable:     true,
		Cause:         io.ErrUnexpectedEOF,
	}
	if err := scanner.Err(); err != nil {
		failure.Retryable = isRetryableProviderStreamCause(err)
		failure.Cause = err
	}
	return writeProviderStreamErrorEvent(dst, failure)
}

func providerSSEEventCarriesSemanticOutput(provider string, eventType string, payload string) bool {
	if strings.TrimSpace(provider) != "openai" {
		return false
	}
	resolvedType := providerSSEEventType(eventType, payload)
	switch resolvedType {
	case "response.output_text.delta",
		"response.reasoning_summary_text.delta",
		"response.reasoning_text.delta",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.image_generation_call.partial_image":
		var value map[string]any
		if json.Unmarshal([]byte(payload), &value) != nil {
			return false
		}
		for _, field := range []string{"delta", "arguments", "partial_image_b64"} {
			if text, ok := value[field].(string); ok && text != "" {
				return true
			}
		}
		return false
	case "response.output_item.added", "response.output_item.done":
		var value struct {
			Item struct {
				Type             string `json:"type"`
				EncryptedContent string `json:"encrypted_content"`
			} `json:"item"`
		}
		if json.Unmarshal([]byte(payload), &value) != nil {
			return false
		}
		switch strings.TrimSpace(value.Item.Type) {
		case "function_call", "image_generation_call":
			return true
		case "reasoning":
			return strings.TrimSpace(value.Item.EncryptedContent) != ""
		}
	}
	return false
}

// The Responses API can repeat the complete prompt and output in its terminal
// envelope. Some compatible providers close that redundant, very large frame
// after the explicit completed status but before its final JSON delimiter. The
// decoder only certifies the visible terminal fields here; the OpenAI adapter
// decides whether prior semantic output makes the synthetic acknowledgement
// safe or whether the request must follow the pre-output retry path.
func recoverTruncatedOpenAICompletedEvent(provider string, eventType string, payload string, _ bool) (string, bool) {
	if strings.TrimSpace(provider) != "openai" {
		return "", false
	}
	payloadType, responseStatus := inspectPartialOpenAIResponseEnvelope(payload)
	if payloadType != "response.completed" || responseStatus != "completed" {
		return "", false
	}
	resolvedEventType := strings.TrimSpace(eventType)
	if resolvedEventType != "" && resolvedEventType != "response.completed" {
		return "", false
	}
	recovered, err := json.Marshal(map[string]any{
		"type": "response.completed",
		"response": map[string]string{
			"status": "completed_cursor_byok_recovery",
		},
		"cursor_byok_recovery": map[string]string{
			"reason":         "truncated_redundant_terminal",
			"payload_prefix": boundedProviderStreamEvidence(payload),
		},
	})
	if err != nil {
		return "", false
	}
	return string(recovered), true
}

type partialJSONContainer struct {
	kind           json.Delim
	topLevel       bool
	responseObject bool
	expectingKey   bool
	pendingKey     string
}

func inspectPartialOpenAIResponseEnvelope(payload string) (string, string) {
	decoder := json.NewDecoder(strings.NewReader(payload))
	containers := make([]partialJSONContainer, 0, 4)
	payloadType := ""
	responseStatus := ""

	consumeParentValue := func() (bool, string) {
		if len(containers) == 0 {
			return false, ""
		}
		parent := &containers[len(containers)-1]
		if parent.kind != '{' || parent.expectingKey {
			return false, ""
		}
		key := parent.pendingKey
		isTopLevel := parent.topLevel
		parent.pendingKey = ""
		parent.expectingKey = true
		return isTopLevel, key
	}

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch value := token.(type) {
		case json.Delim:
			switch value {
			case '{':
				parentTopLevel, parentKey := consumeParentValue()
				containers = append(containers, partialJSONContainer{
					kind:           value,
					topLevel:       len(containers) == 0,
					responseObject: parentTopLevel && parentKey == "response",
					expectingKey:   true,
				})
			case '[':
				consumeParentValue()
				containers = append(containers, partialJSONContainer{kind: value})
			case '}', ']':
				if len(containers) > 0 {
					containers = containers[:len(containers)-1]
				}
			}
		case string:
			if len(containers) == 0 {
				continue
			}
			current := &containers[len(containers)-1]
			if current.kind != '{' {
				continue
			}
			if current.expectingKey {
				current.pendingKey = value
				current.expectingKey = false
				continue
			}
			switch {
			case current.topLevel && current.pendingKey == "type":
				payloadType = strings.TrimSpace(value)
			case current.responseObject && current.pendingKey == "status":
				responseStatus = strings.TrimSpace(value)
			}
			current.pendingKey = ""
			current.expectingKey = true
		default:
			consumeParentValue()
		}
		if payloadType == "response.completed" && responseStatus == "completed" {
			return payloadType, responseStatus
		}
	}
	return payloadType, responseStatus
}

func shouldStopAfterProviderSSEEvent(provider string, eventType string, payload string) bool {
	if strings.TrimSpace(payload) == "[DONE]" {
		return true
	}
	resolvedType := providerSSEEventType(eventType, payload)
	switch strings.TrimSpace(provider) {
	case "anthropic":
		return resolvedType == "message_stop" || resolvedType == "error"
	case "openai":
		switch resolvedType {
		case "response.completed", "response.incomplete", "response.failed", "error":
			return true
		}
	}
	return false
}

func normalizeProviderSSEPayload(payload string) (string, error) {
	trimmed := strings.TrimSpace(payload)
	if trimmed == "[DONE]" {
		return trimmed, nil
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, []byte(payload)); err != nil {
		return "", err
	}
	return compacted.String(), nil
}

func writeCanonicalSSEEvent(dst io.Writer, eventType string, payload string) error {
	if value := strings.TrimSpace(eventType); value != "" {
		if _, err := fmt.Fprintf(dst, "event: %s\n", value); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(dst, "data: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}

func writeProviderStreamErrorEvent(dst io.Writer, streamErr *ProviderStreamError) error {
	if streamErr == nil {
		streamErr = &ProviderStreamError{Cause: io.ErrUnexpectedEOF, Retryable: true}
	}
	marker := encodeProviderStreamErrorMarker(streamErr)
	payload, err := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    "transport_error",
			"code":    "provider_stream_error",
			"message": marker,
		},
	})
	if err != nil {
		return err
	}
	eventType := ""
	if strings.TrimSpace(streamErr.Provider) == "anthropic" {
		eventType = "error"
	}
	return writeCanonicalSSEEvent(dst, eventType, string(payload))
}

func encodeProviderStreamErrorMarker(streamErr *ProviderStreamError) string {
	envelope := providerStreamErrorEnvelope{
		Provider:      strings.TrimSpace(streamErr.Provider),
		EventType:     strings.TrimSpace(streamErr.EventType),
		TerminalSeen:  streamErr.TerminalSeen,
		PayloadPrefix: boundedProviderStreamEvidence(streamErr.PayloadPrefix),
		Retryable:     streamErr.Retryable,
	}
	if streamErr.Cause != nil {
		envelope.Cause = strings.TrimSpace(streamErr.Cause.Error())
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return providerStreamErrorMarker
	}
	return providerStreamErrorMarker + base64.RawURLEncoding.EncodeToString(payload)
}

func decodeProviderStreamErrorMarker(message string) *ProviderStreamError {
	start := strings.Index(message, providerStreamErrorMarker)
	if start < 0 {
		return nil
	}
	encoded := message[start+len(providerStreamErrorMarker):]
	if end := strings.IndexFunc(encoded, func(value rune) bool {
		return !((value >= 'a' && value <= 'z') ||
			(value >= 'A' && value <= 'Z') ||
			(value >= '0' && value <= '9') || value == '-' || value == '_')
	}); end >= 0 {
		encoded = encoded[:end]
	}
	if encoded == "" {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	var envelope providerStreamErrorEnvelope
	if json.Unmarshal(payload, &envelope) != nil {
		return nil
	}
	var cause error
	if strings.TrimSpace(envelope.Cause) != "" {
		cause = errors.New(strings.TrimSpace(envelope.Cause))
	}
	return &ProviderStreamError{
		Provider:      strings.TrimSpace(envelope.Provider),
		EventType:     strings.TrimSpace(envelope.EventType),
		TerminalSeen:  envelope.TerminalSeen,
		PayloadPrefix: boundedProviderStreamEvidence(envelope.PayloadPrefix),
		Retryable:     envelope.Retryable,
		Cause:         cause,
	}
}

func isProviderSSEKeepalive(eventType string, payload string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "ping", "keepalive", "heartbeat":
		return true
	}
	return strings.TrimSpace(payload) == ""
}

func providerSSEEventType(eventType string, payload string) string {
	resolvedType := strings.TrimSpace(eventType)
	var value map[string]any
	if json.Unmarshal([]byte(payload), &value) == nil {
		if payloadType, ok := value["type"].(string); ok && strings.TrimSpace(payloadType) != "" {
			resolvedType = strings.TrimSpace(payloadType)
		}
	}
	return resolvedType
}

func isProviderTerminalSSEEvent(provider string, eventType string, payload string) bool {
	if strings.TrimSpace(payload) == "[DONE]" {
		return true
	}
	resolvedType := providerSSEEventType(eventType, payload)
	switch strings.TrimSpace(provider) {
	case "anthropic":
		return resolvedType == "message_stop"
	case "openai":
		switch resolvedType {
		case "response.completed", "response.incomplete", "response.failed", "error":
			return true
		}
		var value map[string]any
		if json.Unmarshal([]byte(payload), &value) != nil {
			return false
		}
		choices, _ := value["choices"].([]any)
		for _, rawChoice := range choices {
			choice, _ := rawChoice.(map[string]any)
			if choice != nil && choice["finish_reason"] != nil {
				return true
			}
		}
	}
	return false
}

func classifyProviderStreamFailure(err error, provider string, requestID string, modelCallID string, outputEscaped bool) error {
	if err == nil {
		return nil
	}
	var streamErr *ProviderStreamError
	if errors.As(err, &streamErr) {
		copy := *streamErr
		copy.Provider = firstNonEmptyString(copy.Provider, provider)
		copy.RequestID = strings.TrimSpace(requestID)
		copy.ModelCallID = strings.TrimSpace(modelCallID)
		copy.OutputEscaped = outputEscaped
		copy.Retryable = copy.Retryable || isRetryableProviderStreamCause(copy.Cause)
		return &copy
	}
	return &ProviderStreamError{
		Provider:      strings.TrimSpace(provider),
		RequestID:     strings.TrimSpace(requestID),
		ModelCallID:   strings.TrimSpace(modelCallID),
		OutputEscaped: outputEscaped,
		Retryable:     isRetryableProviderStreamCause(err),
		Cause:         err,
	}
}

func shouldRetryProviderStream(err error) bool {
	var streamErr *ProviderStreamError
	return errors.As(err, &streamErr) && streamErr.Retryable && !streamErr.OutputEscaped && !streamErr.TerminalSeen
}

func isRetryableProviderStreamCause(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "unexpected eof") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "server closed idle connection") ||
		strings.Contains(message, "stream idle timeout")
}

func isSemanticModelEvent(event ModelEvent) bool {
	switch event.Kind {
	case ModelEventKindTextDelta,
		ModelEventKindThinkingDelta,
		ModelEventKindThinkingCompleted,
		ModelEventKindPartialToolCall,
		ModelEventKindToolCallDelta,
		ModelEventKindToolLikeCompleted:
		return true
	default:
		return false
	}
}

func boundedProviderStreamEvidence(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= providerStreamEvidenceLimit {
		return value
	}
	return value[:providerStreamEvidenceLimit] + "..."
}
