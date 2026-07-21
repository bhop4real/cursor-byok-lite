package forwarder

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"cursor/gen/agentv1"
	runtimecore "cursor/internal/backend/agent/core"
	modeladapter "cursor/internal/backend/agent/model"
)

const (
	PromptProfileBaseline              = "baseline-v1"
	PromptProfileCompactContextToolsV1 = "compact-context-tools-v1"

	compactContextSoftBudgetTokens = int64(100000)
	compactContextHardBudgetTokens = int64(125000)
)

// CompactContextToolsEnrollmentSource is read only when a conversation is
// created. The resulting profile is persisted and never derived again.
type CompactContextToolsEnrollmentSource interface {
	CompactContextToolsEnabled() bool
}

type providerProjectionDiagnostics struct {
	Profile                      string
	OmittedMCPContextBytes       int
	OmittedMCPDescriptorCount    int
	OmittedTransientContextCount int
	FallbackReason               string
}

type providerReplayProjection struct {
	Messages    []modeladapter.Message
	Diagnostics providerProjectionDiagnostics
}

type canonicalMCPProviderTool struct {
	Name        string
	Server      string
	ToolName    string
	Description string
	InputSchema any
	Schema      json.RawMessage
}

type canonicalMCPProviderTools struct {
	ByName map[string]canonicalMCPProviderTool
	ByPair map[string][]canonicalMCPProviderTool
}

func normalizedPromptProfile(profile string) string {
	if strings.TrimSpace(profile) == PromptProfileCompactContextToolsV1 {
		return PromptProfileCompactContextToolsV1
	}
	return PromptProfileBaseline
}

func conversationUsesCompactContextTools(conversation *ConversationFile) bool {
	return conversation != nil && normalizedPromptProfile(conversation.PromptProfile) == PromptProfileCompactContextToolsV1
}

func enrollmentPromptProfile(source CompactContextToolsEnrollmentSource) string {
	if source != nil && source.CompactContextToolsEnabled() {
		return PromptProfileCompactContextToolsV1
	}
	return PromptProfileBaseline
}

func (service *Service) compactContextToolsEnrollmentSource() CompactContextToolsEnrollmentSource {
	if service == nil {
		return nil
	}
	if service.compactContextEnrollment != nil {
		return service.compactContextEnrollment
	}
	if source, ok := service.resolver.(CompactContextToolsEnrollmentSource); ok {
		return source
	}
	return nil
}

// conversationStateHasPriorHistory is deliberately conservative. A new state
// may carry mode, an unused token limit, and creation time; every other payload
// is treated as legacy history and therefore remains on the baseline profile.
func conversationStateHasPriorHistory(state *agentv1.ConversationStateStructure) bool {
	if state == nil {
		return false
	}
	if state.GetTokenDetails().GetUsedTokens() > 0 {
		return true
	}
	cloned, ok := proto.Clone(state).(*agentv1.ConversationStateStructure)
	if !ok || cloned == nil {
		return true
	}
	cloned.Mode = nil
	cloned.TokenDetails = nil
	cloned.ConversationStartedTimestampMs = nil
	cloned.ConversationStartedTimeZone = nil
	return proto.Size(cloned) > 0
}

func compactProviderProjectionConversation(conversation *ConversationFile, providerTools []json.RawMessage) (*ConversationFile, providerProjectionDiagnostics, error) {
	diagnostics := providerProjectionDiagnostics{Profile: PromptProfileBaseline}
	if conversation == nil || !conversationUsesCompactContextTools(conversation) {
		return conversation, diagnostics, nil
	}
	diagnostics.Profile = PromptProfileCompactContextToolsV1
	canonicalTools := currentTurnCanonicalMCPProviderTools(conversation, providerTools)
	projected := cloneConversationFile(conversation)
	// Provider projections bypass the canonical replay cache because their
	// entries intentionally differ from the persisted conversation.
	projected.ContextVersion = 0
	projected.Entries = make([]HistoryEntry, 0, len(conversation.Entries))
	for _, entry := range conversation.Entries {
		next := entry
		switch strings.TrimSpace(entry.Kind) {
		case "request_context":
			requestContext := &agentv1.RequestContext{}
			if err := protojson.Unmarshal(entry.Payload, requestContext); err != nil {
				return nil, providerProjectionDiagnostics{}, err
			}
			omitted := removeEquivalentPersistedMCPTools(requestContext, canonicalTools)
			if omitted > 0 {
				beforeBytes := len(entry.Payload)
				payload, err := protojson.Marshal(requestContext)
				if err != nil {
					return nil, providerProjectionDiagnostics{}, err
				}
				next.Payload = payload
				diagnostics.OmittedMCPDescriptorCount += omitted
				if beforeBytes > len(payload) {
					diagnostics.OmittedMCPContextBytes += beforeBytes - len(payload)
				}
			}
		case "prompt_context":
			var payload promptContextEntryPayload
			if err := json.Unmarshal(entry.Payload, &payload); err != nil {
				return nil, providerProjectionDiagnostics{}, err
			}
			if isCompactTransientPromptContextSource(payload.Source) {
				diagnostics.OmittedTransientContextCount++
				continue
			}
		}
		projected.Entries = append(projected.Entries, next)
	}
	return projected, diagnostics, nil
}

func currentTurnCanonicalMCPProviderTools(conversation *ConversationFile, providerTools []json.RawMessage) canonicalMCPProviderTools {
	result := canonicalMCPProviderTools{
		ByName: make(map[string]canonicalMCPProviderTool),
		ByPair: make(map[string][]canonicalMCPProviderTool),
	}
	if conversation == nil {
		return result
	}
	providerSchemas := concreteProviderToolSchemas(providerTools)
	if len(providerSchemas) == 0 {
		return result
	}
	turnSeq := currentConversationTurnSeq(conversation)
	requestID := strings.TrimSpace(conversation.CurrentRequestID)
	conflicts := make(map[string]struct{})
	for _, entry := range conversation.Entries {
		if entry.TurnSeq != turnSeq || !currentTurnEntryMatchesRequest(entry, requestID) || strings.TrimSpace(entry.Kind) != "request_context" {
			continue
		}
		requestContext := &agentv1.RequestContext{}
		if protojson.Unmarshal(entry.Payload, requestContext) != nil {
			continue
		}
		for _, definition := range requestContext.GetTools() {
			tool, ok := canonicalMCPProviderToolFromDefinition(definition)
			if !ok {
				continue
			}
			providerSchema, exists := providerSchemas[tool.Name]
			if !exists || !equivalentJSONRaw(tool.Schema, providerSchema) {
				continue
			}
			if _, conflicted := conflicts[tool.Name]; conflicted {
				continue
			}
			if existing, exists := result.ByName[tool.Name]; exists {
				if existing.Server != tool.Server || existing.ToolName != tool.ToolName || !equivalentJSONRaw(existing.Schema, tool.Schema) {
					delete(result.ByName, tool.Name)
					conflicts[tool.Name] = struct{}{}
				}
				continue
			}
			result.ByName[tool.Name] = tool
		}
	}
	for _, tool := range result.ByName {
		key := mcpPairKey(tool.Server, tool.ToolName)
		result.ByPair[key] = append(result.ByPair[key], tool)
	}
	return result
}

func concreteProviderToolSchemas(providerTools []json.RawMessage) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage)
	conflicts := make(map[string]struct{})
	for _, schema := range providerTools {
		name, err := extractToolName(schema)
		if err != nil || name == "" {
			continue
		}
		if _, conflicted := conflicts[name]; conflicted {
			continue
		}
		if existing, exists := result[name]; exists {
			if !equivalentJSONRaw(existing, schema) {
				delete(result, name)
				conflicts[name] = struct{}{}
			}
			continue
		}
		result[name] = append(json.RawMessage(nil), schema...)
	}
	return result
}

func canonicalMCPProviderToolFromDefinition(definition *agentv1.McpToolDefinition) (canonicalMCPProviderTool, bool) {
	if definition == nil {
		return canonicalMCPProviderTool{}, false
	}
	name := strings.TrimSpace(definition.GetName())
	server := strings.TrimSpace(definition.GetProviderIdentifier())
	toolName := strings.TrimSpace(definition.GetToolName())
	if name == "" || server == "" || toolName == "" || runtimecore.IsCurrentlySupportedTool(name) {
		return canonicalMCPProviderTool{}, false
	}
	inputSchema := structValueInterface(definition.GetInputSchema())
	if inputSchema != nil {
		if _, ok := inputSchema.(map[string]any); !ok {
			return canonicalMCPProviderTool{}, false
		}
	}
	tool := canonicalMCPProviderTool{
		Name:        name,
		Server:      server,
		ToolName:    toolName,
		Description: strings.TrimSpace(definition.GetDescription()),
		InputSchema: inputSchema,
	}
	schema, ok := (MCPToolCapabilities{}).directToolSchema(MCPDirectTool{
		InvocationName: tool.Name,
		Server:         tool.Server,
		ToolName:       tool.ToolName,
		Description:    tool.Description,
		InputSchema:    tool.InputSchema,
	})
	if !ok {
		return canonicalMCPProviderTool{}, false
	}
	tool.Schema = schema
	return tool, true
}

func removeEquivalentPersistedMCPTools(requestContext *agentv1.RequestContext, canonical canonicalMCPProviderTools) int {
	if requestContext == nil || len(canonical.ByName) == 0 {
		return 0
	}
	omitted := 0
	definitions := make([]*agentv1.McpToolDefinition, 0, len(requestContext.GetTools()))
	for _, definition := range requestContext.GetTools() {
		candidate, ok := canonicalMCPProviderToolFromDefinition(definition)
		canonicalTool, exists := canonical.ByName[candidate.Name]
		if !ok || !exists || candidate.Server != canonicalTool.Server || candidate.ToolName != canonicalTool.ToolName || !equivalentJSONRaw(candidate.Schema, canonicalTool.Schema) {
			definitions = append(definitions, definition)
			continue
		}
		cloned, clonedOK := proto.Clone(definition).(*agentv1.McpToolDefinition)
		if !clonedOK || cloned == nil {
			definitions = append(definitions, definition)
			continue
		}
		if strings.TrimSpace(cloned.GetDescription()) != "" || cloned.GetInputSchema() != nil {
			omitted++
		}
		cloned.Description = ""
		cloned.InputSchema = nil
		definitions = append(definitions, cloned)
	}
	requestContext.Tools = definitions
	if options := requestContext.GetMcpFileSystemOptions(); options != nil {
		filtered, count := stripEquivalentMCPDescriptorSchemas(options.GetMcpDescriptors(), canonical)
		options.McpDescriptors = filtered
		omitted += count
	}
	if options := requestContext.GetMcpMetaToolOptions(); options != nil {
		filtered, count := stripEquivalentMCPDescriptorSchemas(options.GetMcpDescriptors(), canonical)
		options.McpDescriptors = filtered
		omitted += count
	}
	return omitted
}

func stripEquivalentMCPDescriptorSchemas(descriptors []*agentv1.McpDescriptor, canonical canonicalMCPProviderTools) ([]*agentv1.McpDescriptor, int) {
	projected := make([]*agentv1.McpDescriptor, 0, len(descriptors))
	omitted := 0
	for _, descriptor := range descriptors {
		if descriptor == nil {
			continue
		}
		cloned, ok := proto.Clone(descriptor).(*agentv1.McpDescriptor)
		if !ok || cloned == nil {
			projected = append(projected, descriptor)
			continue
		}
		server := firstNonEmpty(cloned.GetServerIdentifier(), cloned.GetServerName())
		tools := make([]*agentv1.McpToolDescriptor, 0, len(cloned.GetTools()))
		for _, descriptorTool := range cloned.GetTools() {
			if descriptorTool == nil || !descriptorToolMatchesCanonicalProviderTool(server, descriptorTool, canonical) {
				tools = append(tools, descriptorTool)
				continue
			}
			projectedTool, projectedOK := proto.Clone(descriptorTool).(*agentv1.McpToolDescriptor)
			if !projectedOK || projectedTool == nil {
				tools = append(tools, descriptorTool)
				continue
			}
			if projectedTool.Description != nil || projectedTool.InputSchema != nil {
				omitted++
			}
			projectedTool.Description = nil
			projectedTool.InputSchema = nil
			tools = append(tools, projectedTool)
		}
		cloned.Tools = tools
		projected = append(projected, cloned)
	}
	return projected, omitted
}

func descriptorToolMatchesCanonicalProviderTool(server string, descriptor *agentv1.McpToolDescriptor, canonical canonicalMCPProviderTools) bool {
	if descriptor == nil {
		return false
	}
	candidates := canonical.ByPair[mcpPairKey(server, descriptor.GetToolName())]
	for _, candidate := range candidates {
		if strings.TrimSpace(descriptor.GetDescription()) != candidate.Description {
			continue
		}
		if equivalentJSONValue(structValueInterface(descriptor.GetInputSchema()), candidate.InputSchema) {
			return true
		}
	}
	return false
}

func equivalentJSONRaw(left json.RawMessage, right json.RawMessage) bool {
	var leftValue any
	var rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	return equivalentJSONValue(leftValue, rightValue)
}

func equivalentJSONValue(left any, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func isCompactTransientPromptContextSource(source string) bool {
	switch strings.TrimSpace(source) {
	case "mode_change",
		promptContextSourceStructuredCurrentPlan,
		promptContextSourceStructuredTodoList,
		promptContextSourceStructuredTodoReminder,
		promptContextSourcePlanTurnContract,
		promptContextSourceActiveModeContract,
		promptContextSourceLatestUserIntent,
		promptContextSourceCurrentUserRequest,
		promptContextSourceSubagentContract,
		promptContextSourceSubagentEmptyStopRecovery,
		promptContextSourceDebugModeReminder:
		return true
	default:
		return strings.HasPrefix(strings.TrimSpace(source), "tail_reminder/")
	}
}

func compactCurrentTurnPersistedTailMessages(conversation *ConversationFile) []modeladapter.Message {
	if conversation == nil {
		return nil
	}
	turnSeq := currentConversationTurnSeq(conversation)
	if turnSeq <= 0 {
		return nil
	}
	type indexedMessage struct {
		Index   int
		Message modeladapter.Message
	}
	latestBySource := make(map[string]indexedMessage)
	for index, entry := range conversation.Entries {
		if entry.TurnSeq != turnSeq || strings.TrimSpace(entry.Kind) != "prompt_context" {
			continue
		}
		var payload promptContextEntryPayload
		if json.Unmarshal(entry.Payload, &payload) != nil || !isCompactTransientPromptContextSource(payload.Source) {
			continue
		}
		source := strings.TrimSpace(payload.Source)
		latestBySource[source] = indexedMessage{
			Index: index,
			Message: modeladapter.Message{
				Role:    firstNonEmpty(strings.TrimSpace(payload.Role), "user"),
				Content: strings.TrimSpace(payload.Content),
			},
		}
	}
	ordered := make([]indexedMessage, 0, len(latestBySource))
	for _, item := range latestBySource {
		ordered = append(ordered, item)
	}
	sort.Slice(ordered, func(left int, right int) bool { return ordered[left].Index < ordered[right].Index })
	messages := make([]modeladapter.Message, 0, len(ordered))
	for _, item := range ordered {
		messages = append(messages, item.Message)
	}
	return messages
}

func appendUniqueProviderTailMessage(messages []modeladapter.Message, seen map[string]struct{}, message modeladapter.Message) []modeladapter.Message {
	message.Role = firstNonEmpty(strings.TrimSpace(message.Role), "user")
	message.Content = strings.TrimSpace(message.Content)
	if message.Content == "" {
		return messages
	}
	key := promptContextContentHash(message)
	if _, exists := seen[key]; exists {
		return messages
	}
	seen[key] = struct{}{}
	return append(messages, message)
}

func applyCompactProjectionDiagnostics(compiled CompiledConversation, diagnostics providerProjectionDiagnostics) CompiledConversation {
	compiled.PromptProfile = normalizedPromptProfile(diagnostics.Profile)
	if compiled.PromptProfile != PromptProfileCompactContextToolsV1 {
		return compiled
	}
	fallbackReason := strings.TrimSpace(diagnostics.FallbackReason)
	compiled.ProviderProjectionApplied = fallbackReason == ""
	compiled.ProviderProjectionFallback = fallbackReason != ""
	estimatedTokens := estimateCompiledPromptTokens(compiled)
	budgetStatus := "within_soft_budget"
	switch {
	case estimatedTokens > compactContextHardBudgetTokens:
		budgetStatus = "hard_budget_exceeded"
	case estimatedTokens > compactContextSoftBudgetTokens:
		budgetStatus = "soft_budget_exceeded"
	}
	fields := []string{
		"prompt_profile=" + PromptProfileCompactContextToolsV1,
		"tool_contract=canonical-compact-v1",
		"stable_prefix=canonical_history",
		"recent_tail=current_turn_dynamic",
		"omitted_mcp_context_bytes=" + strconv.Itoa(diagnostics.OmittedMCPContextBytes),
		"omitted_mcp_descriptors=" + strconv.Itoa(diagnostics.OmittedMCPDescriptorCount),
		"omitted_transient_contexts=" + strconv.Itoa(diagnostics.OmittedTransientContextCount),
		"prompt_tokens_estimate=" + strconv.FormatInt(estimatedTokens, 10),
		"soft_budget=" + strconv.FormatInt(compactContextSoftBudgetTokens, 10),
		"hard_budget=" + strconv.FormatInt(compactContextHardBudgetTokens, 10),
		"budget_status=" + budgetStatus,
		"tool_results=retained",
	}
	if reason := strings.TrimSpace(diagnostics.FallbackReason); reason != "" {
		fields = append(fields, "projection_fallback="+reason)
	}
	compiled.CompileSummary = strings.TrimSpace(compiled.CompileSummary + " " + strings.Join(fields, " "))
	return compiled
}
