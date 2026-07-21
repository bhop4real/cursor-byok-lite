package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"cursor/internal/modelchannel"
)

const (
	DefaultBackendListenAddr                 = "127.0.0.1:18090"
	DefaultProxyListenAddr                   = "127.0.0.1:18080"
	DefaultFrontendBaseURL                   = "http://127.0.0.1"
	DefaultRoutingMode                       = "local"
	DefaultResponseLanguage                  = "auto"
	DefaultProviderStreamIdleTimeoutSeconds  = 240
	DefaultHomeMetricsRefreshIntervalSeconds = 60
	MinProviderStreamIdleTimeoutSeconds      = 30
)

func normalizeResponseLanguage(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return "auto"
	case "en-us", "en":
		return "en-US"
	case "zh-cn", "zh":
		return "zh-CN"
	case "ja-jp", "ja":
		return "ja-JP"
	default:
		return DefaultResponseLanguage
	}
}

type ModelAdapterConfig struct {
	ID                          string `json:"id,omitempty" yaml:"-"`
	DisplayName                 string `json:"displayName" yaml:"displayName"`
	Type                        string `json:"type" yaml:"type"`
	BaseURL                     string `json:"baseURL" yaml:"baseURL"`
	APIKey                      string `json:"apiKey" yaml:"apiKey"`
	TooltipData                 string `json:"tooltipData" yaml:"tooltipData"`
	ModelID                     string `json:"modelID" yaml:"modelID"`
	ReasoningEffort             string `json:"reasoningEffort" yaml:"reasoningEffort"`
	OpenAIEndpoint              string `json:"openAIEndpoint" yaml:"openAIEndpoint"`
	OpenAIExtraParamsEnabled    bool   `json:"openAIExtraParamsEnabled" yaml:"openAIExtraParamsEnabled"`
	OpenAIExtraParamsJSON       string `json:"openAIExtraParamsJSON" yaml:"openAIExtraParamsJSON"`
	CustomHeadersEnabled        bool   `json:"customHeadersEnabled" yaml:"customHeadersEnabled"`
	CustomHeadersJSON           string `json:"customHeadersJSON" yaml:"customHeadersJSON"`
	AnthropicExtraParamsEnabled bool   `json:"anthropicExtraParamsEnabled" yaml:"anthropicExtraParamsEnabled"`
	AnthropicExtraParamsJSON    string `json:"anthropicExtraParamsJSON" yaml:"anthropicExtraParamsJSON"`
	ContextWindowTokens         int    `json:"contextWindowTokens" yaml:"contextWindowTokens"`
	MaxCompletionTokens         int    `json:"maxCompletionTokens" yaml:"maxCompletionTokens"`
	AnthropicMaxTokens          int    `json:"anthropicMaxTokens" yaml:"anthropicMaxTokens"`
	AnthropicThinkingEffort     string `json:"anthropicThinkingEffort,omitempty" yaml:"anthropicThinkingEffort,omitempty"`
	ThinkingBudgetTokens        int    `json:"thinkingBudgetTokens" yaml:"thinkingBudgetTokens"`
}

type RoutingConfig struct {
	Mode string `json:"mode" yaml:"mode"`
}

type HomeMetricsConfig struct {
	IncludeCacheWriteInHitRate bool `json:"includeCacheWriteInHitRate" yaml:"includeCacheWriteInHitRate"`
	RefreshIntervalSeconds     int  `json:"refreshIntervalSeconds" yaml:"refreshIntervalSeconds"`
}

type Config struct {
	Log                       bool                 `json:"log" yaml:"log"`
	DisableUpdates            bool                 `json:"disableUpdates" yaml:"disableUpdates"`
	CompactContextTools       bool                 `json:"compactContextTools" yaml:"compactContextTools"`
	ResponseLanguage          string               `json:"responseLanguage" yaml:"responseLanguage"`
	ProviderStreamIdleTimeout int                  `json:"providerStreamIdleTimeout" yaml:"providerStreamIdleTimeout"`
	BackendListenAddr         string               `json:"backendListenAddr" yaml:"backendListenAddr"`
	ProxyListenAddr           string               `json:"proxyListenAddr" yaml:"proxyListenAddr"`
	ModelAdapters             []ModelAdapterConfig `json:"modelAdapters" yaml:"modelAdapters"`
	Routing                   RoutingConfig        `json:"routing" yaml:"routing"`
	HomeMetrics               HomeMetricsConfig    `json:"homeMetrics" yaml:"homeMetrics"`
	LastAgentModelHash        string               `json:"lastAgentModelHash" yaml:"lastAgentModelHash"`
}

type RoutingConfigPatch struct {
	Mode *string `json:"mode,omitempty"`
}

type HomeMetricsConfigPatch struct {
	IncludeCacheWriteInHitRate *bool `json:"includeCacheWriteInHitRate,omitempty"`
	RefreshIntervalSeconds     *int  `json:"refreshIntervalSeconds,omitempty"`
}

// ConfigPatch contains only user-owned fields. Pointer values distinguish an
// omitted field from an explicit false, zero, or empty value.
type ConfigPatch struct {
	Log                       *bool                   `json:"log,omitempty"`
	DisableUpdates            *bool                   `json:"disableUpdates,omitempty"`
	CompactContextTools       *bool                   `json:"compactContextTools,omitempty"`
	ResponseLanguage          *string                 `json:"responseLanguage,omitempty"`
	ProviderStreamIdleTimeout *int                    `json:"providerStreamIdleTimeout,omitempty"`
	BackendListenAddr         *string                 `json:"backendListenAddr,omitempty"`
	ProxyListenAddr           *string                 `json:"proxyListenAddr,omitempty"`
	Routing                   *RoutingConfigPatch     `json:"routing,omitempty"`
	HomeMetrics               *HomeMetricsConfigPatch `json:"homeMetrics,omitempty"`
}

type EditableFieldMetadata struct {
	Path       string   `json:"path"`
	Type       string   `json:"type"`
	Default    any      `json:"default"`
	Minimum    *int     `json:"minimum,omitempty"`
	Enum       []string `json:"enum,omitempty"`
	Trim       bool     `json:"trim"`
	Secret     bool     `json:"secret"`
	ApplyScope string   `json:"applyScope"`
}

type EditableConfigMetadata struct {
	Fields []EditableFieldMetadata `json:"fields"`
}

func EditableConfigContract() EditableConfigMetadata {
	providerMinimum := MinProviderStreamIdleTimeoutSeconds
	refreshMinimum := 1
	return EditableConfigMetadata{Fields: []EditableFieldMetadata{
		{Path: "log", Type: "boolean", Default: false, ApplyScope: "immediate"},
		{Path: "disableUpdates", Type: "boolean", Default: false, ApplyScope: "app_restart"},
		{Path: "compactContextTools", Type: "boolean", Default: false, ApplyScope: "new_conversations_only"},
		{Path: "responseLanguage", Type: "enum", Default: DefaultResponseLanguage, Enum: []string{"auto", "en-US", "zh-CN", "ja-JP"}, Trim: true, ApplyScope: "next_request"},
		{Path: "providerStreamIdleTimeout", Type: "integer", Default: DefaultProviderStreamIdleTimeoutSeconds, Minimum: &providerMinimum, ApplyScope: "next_request"},
		{Path: "backendListenAddr", Type: "string", Default: DefaultBackendListenAddr, Trim: true, ApplyScope: "service_restart"},
		{Path: "proxyListenAddr", Type: "string", Default: DefaultProxyListenAddr, Trim: true, ApplyScope: "service_restart"},
		{Path: "routing.mode", Type: "enum", Default: DefaultRoutingMode, Enum: []string{"local", "upstream"}, Trim: true, ApplyScope: "next_request"},
		{Path: "homeMetrics.includeCacheWriteInHitRate", Type: "boolean", Default: false, ApplyScope: "immediate"},
		{Path: "homeMetrics.refreshIntervalSeconds", Type: "integer", Default: DefaultHomeMetricsRefreshIntervalSeconds, Minimum: &refreshMinimum, ApplyScope: "immediate"},
	}}
}

func ApplyConfigPatch(current Config, patch ConfigPatch) (Config, error) {
	next := current
	if patch.Log != nil {
		next.Log = *patch.Log
	}
	if patch.DisableUpdates != nil {
		next.DisableUpdates = *patch.DisableUpdates
	}
	if patch.CompactContextTools != nil {
		next.CompactContextTools = *patch.CompactContextTools
	}
	if patch.ResponseLanguage != nil {
		next.ResponseLanguage = strings.TrimSpace(*patch.ResponseLanguage)
	}
	if patch.ProviderStreamIdleTimeout != nil {
		next.ProviderStreamIdleTimeout = *patch.ProviderStreamIdleTimeout
	}
	if patch.BackendListenAddr != nil {
		next.BackendListenAddr = strings.TrimSpace(*patch.BackendListenAddr)
	}
	if patch.ProxyListenAddr != nil {
		next.ProxyListenAddr = strings.TrimSpace(*patch.ProxyListenAddr)
	}
	if patch.Routing != nil && patch.Routing.Mode != nil {
		next.Routing.Mode = strings.TrimSpace(*patch.Routing.Mode)
	}
	if patch.HomeMetrics != nil {
		if patch.HomeMetrics.IncludeCacheWriteInHitRate != nil {
			next.HomeMetrics.IncludeCacheWriteInHitRate = *patch.HomeMetrics.IncludeCacheWriteInHitRate
		}
		if patch.HomeMetrics.RefreshIntervalSeconds != nil {
			next.HomeMetrics.RefreshIntervalSeconds = *patch.HomeMetrics.RefreshIntervalSeconds
		}
	}
	return NormalizeConfig(next)
}

func DefaultConfig() Config {
	return Config{
		Log:                       false,
		DisableUpdates:            false,
		CompactContextTools:       false,
		ResponseLanguage:          DefaultResponseLanguage,
		ProviderStreamIdleTimeout: DefaultProviderStreamIdleTimeoutSeconds,
		BackendListenAddr:         DefaultBackendListenAddr,
		ProxyListenAddr:           DefaultProxyListenAddr,
		ModelAdapters:             []ModelAdapterConfig{},
		Routing: RoutingConfig{
			Mode: DefaultRoutingMode,
		},
		HomeMetrics: HomeMetricsConfig{
			RefreshIntervalSeconds: DefaultHomeMetricsRefreshIntervalSeconds,
		},
	}
}

func NormalizeConfig(input Config) (Config, error) {
	output := DefaultConfig()
	output.Log = input.Log
	output.DisableUpdates = input.DisableUpdates
	output.CompactContextTools = input.CompactContextTools
	output.ResponseLanguage = normalizeResponseLanguage(input.ResponseLanguage)
	output.ProviderStreamIdleTimeout = normalizeProviderStreamIdleTimeout(input.ProviderStreamIdleTimeout)
	backendListenAddr, err := normalizeListenAddr(input.BackendListenAddr, DefaultBackendListenAddr, "backendListenAddr")
	if err != nil {
		return Config{}, err
	}
	proxyListenAddr, err := normalizeListenAddr(input.ProxyListenAddr, DefaultProxyListenAddr, "proxyListenAddr")
	if err != nil {
		return Config{}, err
	}
	output.BackendListenAddr = backendListenAddr
	output.ProxyListenAddr = proxyListenAddr
	output.HomeMetrics.IncludeCacheWriteInHitRate = input.HomeMetrics.IncludeCacheWriteInHitRate
	output.HomeMetrics.RefreshIntervalSeconds = normalizeHomeMetricsRefreshInterval(input.HomeMetrics.RefreshIntervalSeconds)
	output.LastAgentModelHash = strings.TrimSpace(input.LastAgentModelHash)
	output.Routing.Mode = normalizeRoutingMode(input.Routing.Mode)
	if output.Routing.Mode == "" {
		output.Routing.Mode = DefaultRoutingMode
	}
	adapters, err := NormalizeModelAdapterConfigs(input.ModelAdapters)
	if err != nil {
		return Config{}, err
	}
	output.ModelAdapters = adapters
	return output, nil
}

func NormalizeModelAdapterConfigs(input []ModelAdapterConfig) ([]ModelAdapterConfig, error) {
	if len(input) == 0 {
		return []ModelAdapterConfig{}, nil
	}

	normalized := make([]ModelAdapterConfig, 0, len(input))
	seenChannelIDs := make(map[string]struct{}, len(input))
	for _, item := range input {
		baseURL, err := modelchannel.NormalizeBaseURL(item.BaseURL)
		if err != nil {
			return nil, err
		}
		nextType := normalizeModelAdapterType(item.Type)
		next := ModelAdapterConfig{
			DisplayName:          strings.TrimSpace(item.DisplayName),
			Type:                 nextType,
			BaseURL:              baseURL,
			APIKey:               strings.TrimSpace(item.APIKey),
			TooltipData:          strings.TrimSpace(item.TooltipData),
			ModelID:              strings.TrimSpace(item.ModelID),
			ReasoningEffort:      normalizeReasoningEffort(item.ReasoningEffort),
			OpenAIEndpoint:       modelchannel.NormalizeOpenAIEndpoint(item.Type, item.OpenAIEndpoint),
			ContextWindowTokens:  normalizeMaxCompletionTokens(item.ContextWindowTokens),
			MaxCompletionTokens:  normalizeMaxCompletionTokens(item.MaxCompletionTokens),
			AnthropicMaxTokens:   normalizeMaxCompletionTokens(item.AnthropicMaxTokens),
			ThinkingBudgetTokens: normalizeMaxCompletionTokens(item.ThinkingBudgetTokens),
		}
		if next.Type == "openai" {
			next.OpenAIExtraParamsEnabled = item.OpenAIExtraParamsEnabled
			next.OpenAIExtraParamsJSON = strings.TrimSpace(item.OpenAIExtraParamsJSON)
		} else if next.Type == "anthropic" {
			next.AnthropicThinkingEffort = normalizeAnthropicThinkingEffort(item.AnthropicThinkingEffort)
			next.AnthropicExtraParamsEnabled = item.AnthropicExtraParamsEnabled
			next.AnthropicExtraParamsJSON = strings.TrimSpace(item.AnthropicExtraParamsJSON)
		}
		next.CustomHeadersEnabled = item.CustomHeadersEnabled
		next.CustomHeadersJSON = strings.TrimSpace(item.CustomHeadersJSON)
		switch {
		case next.DisplayName == "":
			return nil, errors.New("模型适配器 displayName 不能为空")
		case next.Type == "":
			return nil, errors.New("模型适配器 type 仅支持 openai 或 anthropic")
		case next.APIKey == "":
			return nil, errors.New("模型适配器 apiKey 不能为空")
		case next.TooltipData == "":
			return nil, errors.New("模型适配器 tooltipData 不能为空")
		case next.ModelID == "":
			return nil, errors.New("模型适配器 modelID 不能为空")
		case next.Type == "openai" && next.ReasoningEffort == "":
			return nil, errors.New("模型适配器 reasoningEffort 仅支持 low、medium、high、xhigh、max")
		case next.Type == "openai" && next.OpenAIEndpoint == "":
			return nil, errors.New("模型适配器 openAIEndpoint 仅支持 /v1/responses、/v1/chat/completions 或 /custom（自定义路径）")
		case next.Type == "openai" && next.OpenAIExtraParamsEnabled:
			if err := validateJSONMap(next.OpenAIExtraParamsJSON, "openAIExtraParamsJSON"); err != nil {
				return nil, err
			}
		case next.CustomHeadersEnabled:
			if err := validateHeadersJSON(next.CustomHeadersJSON); err != nil {
				return nil, err
			}
		case next.Type == "anthropic" && next.AnthropicExtraParamsEnabled:
			if err := validateJSONMap(next.AnthropicExtraParamsJSON, "anthropicExtraParamsJSON"); err != nil {
				return nil, err
			}
		case next.Type == "anthropic" && next.AnthropicThinkingEffort == "":
			return nil, errors.New("模型适配器 anthropicThinkingEffort 仅支持 low、medium、high、xhigh、max")
		}
		next.ID = modelchannel.BuildChannelID(next.BaseURL, next.ModelID, next.APIKey, next.DisplayName, next.OpenAIEndpoint)
		if _, exists := seenChannelIDs[next.ID]; exists {
			return nil, errors.New("模型适配器渠道不能重复，请检查 url、modelID、apiKey、displayName、endpoint 组合")
		}
		seenChannelIDs[next.ID] = struct{}{}
		normalized = append(normalized, next)
	}
	return normalized, nil
}

func validateJSONMap(value string, fieldName string) error {
	text := strings.TrimSpace(value)
	if text == "" {
		return fmt.Errorf("模型适配器 %s 不能为空", fieldName)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return fmt.Errorf("模型适配器 %s 必须是合法 JSON 对象", fieldName)
	}
	if parsed == nil {
		return fmt.Errorf("模型适配器 %s 必须是 JSON 对象", fieldName)
	}
	return nil
}

func validateHeadersJSON(value string) error {
	text := strings.TrimSpace(value)
	if err := validateJSONMap(text, "customHeadersJSON"); err != nil {
		return err
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return errors.New("模型适配器 customHeadersJSON 的值必须是字符串")
	}
	for key := range parsed {
		if strings.TrimSpace(key) == "" {
			return errors.New("模型适配器 customHeadersJSON 的请求头名称不能为空")
		}
	}
	return nil
}

func normalizeReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "medium":
		return "medium"
	case "low", "high", "xhigh", "max":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeAnthropicThinkingEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "xhigh":
		return "xhigh"
	case "low", "medium", "high", "max":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeListenAddr(value string, defaultValue string, fieldName string) (string, error) {
	addr := strings.TrimSpace(value)
	if addr == "" {
		addr = defaultValue
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("%s 必须是 host:port 格式", fieldName)
	}
	if strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("%s host 不能为空", fieldName)
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort < 1 || parsedPort > 65535 {
		return "", fmt.Errorf("%s port 必须在 1-65535 之间", fieldName)
	}
	return net.JoinHostPort(host, strconv.Itoa(parsedPort)), nil
}

func normalizeHomeMetricsRefreshInterval(value int) int {
	if value <= 0 {
		return DefaultHomeMetricsRefreshIntervalSeconds
	}
	return value
}

func normalizeProviderStreamIdleTimeout(value int) int {
	if value <= 0 {
		return DefaultProviderStreamIdleTimeoutSeconds
	}
	if value < MinProviderStreamIdleTimeoutSeconds {
		return MinProviderStreamIdleTimeoutSeconds
	}
	return value
}

func normalizeMaxCompletionTokens(value int) int {
	if value <= 0 {
		return 0
	}
	return value
}

func normalizeModelAdapterType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "openai":
		return "openai"
	case "anthropic":
		return "anthropic"
	default:
		return ""
	}
}

func normalizeRoutingMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "local":
		return "local"
	case "upstream":
		return "upstream"
	default:
		return ""
	}
}
