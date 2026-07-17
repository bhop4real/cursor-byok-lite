// provider.go 把 forwarder 的 canonical 请求转交给现有的 provider adapter 层。
package forwarder

import (
	"context"
	"encoding/json"
	"strings"

	modeladapter "cursor/internal/backend/agent/model"
)

type activeArtifactCleaner interface {
	ClearActiveArtifacts(requestID string, modelCallID string)
}

type DefaultProviderGateway struct {
	router modeladapter.ModelAdapterRouter
}

// NewProviderGateway 创建默认 provider 网关。
func NewProviderGateway(resolver modeladapter.ChannelResolver) *DefaultProviderGateway {
	return &DefaultProviderGateway{
		router: modeladapter.NewRouter(resolver),
	}
}

// Close 释放 provider 路由器持有的 transport。
func (gateway *DefaultProviderGateway) Close() {
	if gateway == nil {
		return
	}
	if closer, ok := gateway.router.(interface{ Close() }); ok {
		closer.Close()
	}
	gateway.router = nil
}

func (service *Service) rawProviderObserver(ctx context.Context) modeladapter.LLMArtifactObserver {
	if service == nil || service.recorder == nil || service.debug == nil || !service.debug.enabled(ctx) {
		return nil
	}
	return service.recorder
}

// StartStream 把 forwarder 的 provider 请求翻译成 modeladapter.StreamRequest 并发起流式调用。
func (gateway *DefaultProviderGateway) StartStream(ctx context.Context, req ProviderRequest, sink func(modeladapter.ModelEvent) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if cleaner, ok := req.Observer.(activeArtifactCleaner); ok {
		defer cleaner.ClearActiveArtifacts(req.RequestID, req.ModelCallID)
	}
	requestKnobs := make(map[string]any, len(req.RequestKnobs)+2)
	for key, value := range req.RequestKnobs {
		requestKnobs[key] = value
	}
	requestKnobs["stream"] = true
	if req.MaxTokens > 0 {
		requestKnobs["max_tokens"] = req.MaxTokens
	}
	if strings.TrimSpace(req.ThinkingEffort) != "" {
		requestKnobs["runtime_thinking_effort"] = strings.TrimSpace(req.ThinkingEffort)
	}
	err := gateway.router.Stream(ctx, modeladapter.StreamRequest{
		RequestID:           req.RequestID,
		RunID:               req.RunID,
		ModelCallID:         req.ModelCallID,
		ConversationID:      req.ConversationID,
		Mode:                req.Mode,
		ModelID:             req.ModelID,
		ThinkingEffort:      req.ThinkingEffort,
		Messages:            req.Messages,
		StableMessageCount:  req.StableMessageCount,
		Tools:               append([]json.RawMessage(nil), req.Tools...),
		MaxTokens:           req.MaxTokens,
		Stream:              true,
		RequestKnobs:        requestKnobs,
		CompileSummary:      req.CompileSummary,
		Observer:            req.Observer,
		RawResponseObserver: req.RawResponseObserver,
		ArtifactPaths:       req.ArtifactPaths,
		RequestBodyOverride: req.RequestBodyOverride,
	}, sink)
	if err != nil {
		return providerTerminalError{cause: err}
	}
	return nil
}
