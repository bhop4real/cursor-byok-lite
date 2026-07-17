package forwarder

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"cursor/gen/agentv1"
	runtimecore "cursor/internal/backend/agent/core"
	modeladapter "cursor/internal/backend/agent/model"
)

const (
	mcpPairSeparator                 = "\x00"
	mcpCapabilityReminderPairLimit   = 64
	mcpCapabilityReminderServerLimit = 32
)

// MCPDirectTool preserves a direct MCP definition and its executable target.
type MCPDirectTool struct {
	InvocationName string
	Server         string
	ToolName       string
	Description    string
	InputSchema    any
}

// MCPToolCapabilities is the immutable current-request tool capability snapshot.
type MCPToolCapabilities struct {
	ReadLintsEnabled     bool
	MetaEnabled          bool
	FileSystemEnabled    bool
	CallMcpEnabled       bool
	FetchResourceEnabled bool
	Pairs                map[string]struct{}
	Servers              map[string]struct{}
	ResourceServers      map[string]struct{}
	ToolsByName          map[string][]string
	DirectTools          map[string]MCPDirectTool
	DirectToolConflicts  map[string]struct{}
}

func emptyMCPToolCapabilities(readLintsEnabled bool) MCPToolCapabilities {
	return MCPToolCapabilities{
		ReadLintsEnabled:    readLintsEnabled,
		Pairs:               make(map[string]struct{}),
		Servers:             make(map[string]struct{}),
		ResourceServers:     make(map[string]struct{}),
		ToolsByName:         make(map[string][]string),
		DirectTools:         make(map[string]MCPDirectTool),
		DirectToolConflicts: make(map[string]struct{}),
	}
}

func readLintsEnabledFromRequestContext(requestContext *agentv1.RequestContext) bool {
	return requestContext == nil || requestContext.ReadLintsEnabled == nil || requestContext.GetReadLintsEnabled()
}

// deriveMCPToolCapabilities combines all current request capability sources.
func deriveMCPToolCapabilities(
	directDefinitions []*agentv1.McpToolDefinition,
	topLevelFileSystemOptions *agentv1.McpFileSystemOptions,
	requestContext *agentv1.RequestContext,
) MCPToolCapabilities {
	capabilities := emptyMCPToolCapabilities(readLintsEnabledFromRequestContext(requestContext))
	addDirectDefinitions(&capabilities, directDefinitions)
	if requestContext != nil {
		addDirectDefinitions(&capabilities, requestContext.GetTools())
	}

	metaHasTools := false
	if meta := requestContext.GetMcpMetaToolOptions(); meta != nil && meta.GetEnabled() {
		_, metaHasTools = addDescriptorCapabilities(&capabilities, meta.GetMcpDescriptors(), false)
	}
	capabilities.MetaEnabled = metaHasTools

	fileSystemHasServers := false
	fileSystemHasTools := false
	for _, options := range []*agentv1.McpFileSystemOptions{
		topLevelFileSystemOptions,
		requestContext.GetMcpFileSystemOptions(),
	} {
		if !mcpFileSystemOptionsAvailable(options) {
			continue
		}
		hasServers, hasTools := addDescriptorCapabilities(&capabilities, options.GetMcpDescriptors(), true)
		fileSystemHasServers = fileSystemHasServers || hasServers
		fileSystemHasTools = fileSystemHasTools || hasTools
	}
	capabilities.FileSystemEnabled = fileSystemHasServers
	capabilities.CallMcpEnabled = metaHasTools || fileSystemHasTools
	capabilities.FetchResourceEnabled = fileSystemHasServers
	finalizeMCPToolCapabilities(&capabilities)
	return capabilities
}

func mcpFileSystemOptionsAvailable(options *agentv1.McpFileSystemOptions) bool {
	return options != nil && options.GetEnabled() && len(options.GetMcpDescriptors()) > 0
}

func addDirectDefinitions(capabilities *MCPToolCapabilities, definitions []*agentv1.McpToolDefinition) {
	if capabilities == nil {
		return
	}
	for _, definition := range definitions {
		if definition == nil {
			continue
		}
		invocationName := strings.TrimSpace(definition.GetName())
		server := strings.TrimSpace(definition.GetProviderIdentifier())
		toolName := strings.TrimSpace(definition.GetToolName())
		if invocationName == "" || server == "" || toolName == "" {
			continue
		}
		addMCPPair(capabilities, server, toolName)
		if runtimecore.IsCurrentlySupportedTool(invocationName) {
			continue
		}
		if _, conflicted := capabilities.DirectToolConflicts[invocationName]; conflicted {
			continue
		}
		if existing, exists := capabilities.DirectTools[invocationName]; exists {
			if existing.Server != server || existing.ToolName != toolName {
				delete(capabilities.DirectTools, invocationName)
				capabilities.DirectToolConflicts[invocationName] = struct{}{}
			}
			continue
		}
		capabilities.DirectTools[invocationName] = MCPDirectTool{
			InvocationName: invocationName,
			Server:         server,
			ToolName:       toolName,
			Description:    strings.TrimSpace(definition.GetDescription()),
			InputSchema:    structValueInterface(definition.GetInputSchema()),
		}
	}
}

func addDescriptorCapabilities(capabilities *MCPToolCapabilities, descriptors []*agentv1.McpDescriptor, resourceCapable bool) (bool, bool) {
	if capabilities == nil {
		return false, false
	}
	hasServers := false
	hasTools := false
	for _, descriptor := range descriptors {
		if descriptor == nil {
			continue
		}
		server := firstNonEmpty(descriptor.GetServerIdentifier(), descriptor.GetServerName())
		if server == "" {
			continue
		}
		hasServers = true
		capabilities.Servers[server] = struct{}{}
		if resourceCapable {
			capabilities.ResourceServers[server] = struct{}{}
		}
		for _, tool := range descriptor.GetTools() {
			if tool == nil {
				continue
			}
			toolName := strings.TrimSpace(tool.GetToolName())
			if toolName == "" {
				continue
			}
			hasTools = true
			addMCPPair(capabilities, server, toolName)
		}
	}
	return hasServers, hasTools
}

func addMCPPair(capabilities *MCPToolCapabilities, server string, toolName string) {
	server = strings.TrimSpace(server)
	toolName = strings.TrimSpace(toolName)
	if capabilities == nil || server == "" || toolName == "" {
		return
	}
	capabilities.Servers[server] = struct{}{}
	capabilities.Pairs[mcpPairKey(server, toolName)] = struct{}{}
	capabilities.ToolsByName[toolName] = appendUniqueString(capabilities.ToolsByName[toolName], server)
}

func finalizeMCPToolCapabilities(capabilities *MCPToolCapabilities) {
	if capabilities == nil {
		return
	}
	for toolName, servers := range capabilities.ToolsByName {
		sort.Strings(servers)
		capabilities.ToolsByName[toolName] = servers
	}
}

func mcpPairKey(server string, toolName string) string {
	return strings.TrimSpace(server) + mcpPairSeparator + strings.TrimSpace(toolName)
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func structValueInterface(value *structpb.Value) any {
	if value == nil {
		return nil
	}
	return value.AsInterface()
}

func (capabilities MCPToolCapabilities) hasMCPPair(server string, toolName string) bool {
	_, ok := capabilities.Pairs[mcpPairKey(server, toolName)]
	return ok
}

func (capabilities MCPToolCapabilities) hasResourceServer(server string) bool {
	_, ok := capabilities.ResourceServers[strings.TrimSpace(server)]
	return ok
}

func (capabilities MCPToolCapabilities) resolveMCPServer(toolName string) (string, int) {
	servers := capabilities.ToolsByName[strings.TrimSpace(toolName)]
	if len(servers) != 1 {
		return "", len(servers)
	}
	return servers[0], 1
}

func (capabilities MCPToolCapabilities) directTool(name string) (MCPDirectTool, bool) {
	tool, ok := capabilities.DirectTools[strings.TrimSpace(name)]
	return tool, ok
}

func (capabilities MCPToolCapabilities) directToolSchema(tool MCPDirectTool) (json.RawMessage, bool) {
	if strings.TrimSpace(tool.InvocationName) == "" {
		return nil, false
	}
	parameters, ok := tool.InputSchema.(map[string]any)
	if !ok || parameters == nil {
		parameters = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	item := map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        tool.InvocationName,
			"description": tool.Description,
			"parameters":  parameters,
		},
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

func replaceStreamToolCapabilities(stream *ActiveStream, capabilities MCPToolCapabilities) {
	if stream == nil {
		return
	}
	stream.mu.Lock()
	stream.ToolCapabilities = capabilities
	stream.UpdatedAt = time.Now().UTC()
	stream.mu.Unlock()
}

func snapshotStreamToolCapabilities(stream *ActiveStream) MCPToolCapabilities {
	if stream == nil {
		return emptyMCPToolCapabilities(true)
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.ToolCapabilities
}

// applyProviderToolCapabilities keeps volatile client capabilities out of persisted history.
func applyProviderToolCapabilities(compiled CompiledConversation, capabilities MCPToolCapabilities) CompiledConversation {
	filtered := make([]json.RawMessage, 0, len(compiled.Tools)+len(capabilities.DirectTools))
	seenNames := make(map[string]struct{}, len(compiled.Tools)+len(capabilities.DirectTools))
	hadMCPTool := false
	mcpModeAllowed := false
	for _, item := range compiled.Tools {
		name, err := extractToolName(item)
		if err != nil {
			continue
		}
		keep := true
		switch name {
		case "ReadLints":
			keep = capabilities.ReadLintsEnabled
		case "CallMcpTool":
			hadMCPTool = true
			mcpModeAllowed = true
			keep = capabilities.CallMcpEnabled
		case "FetchMcpResource":
			hadMCPTool = true
			keep = capabilities.FetchResourceEnabled
		}
		if !keep {
			continue
		}
		filtered = append(filtered, item)
		seenNames[name] = struct{}{}
	}

	directToolCount := 0
	if mcpModeAllowed {
		names := make([]string, 0, len(capabilities.DirectTools))
		for name := range capabilities.DirectTools {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, exists := seenNames[name]; exists {
				continue
			}
			schema, ok := capabilities.directToolSchema(capabilities.DirectTools[name])
			if !ok {
				continue
			}
			filtered = append(filtered, schema)
			seenNames[name] = struct{}{}
			directToolCount++
		}
	}
	compiled.Tools = filtered
	if hadMCPTool {
		if reminder := buildMCPProviderCapabilityReminder(capabilities, directToolCount); reminder != "" {
			compiled.Messages = append(compiled.Messages, modeladapter.Message{
				Role:    "user",
				Content: wrapSystemReminder(reminder),
			})
		}
	}
	compiled.CompileSummary = strings.TrimSpace(fmt.Sprintf(
		"%s capabilities_read_lints=%t capabilities_mcp_call=%t capabilities_mcp_resource=%t capabilities_mcp_direct=%d",
		compiled.CompileSummary,
		capabilities.ReadLintsEnabled,
		capabilities.CallMcpEnabled,
		capabilities.FetchResourceEnabled,
		directToolCount,
	))
	return compiled
}

func buildMCPProviderCapabilityReminder(capabilities MCPToolCapabilities, directToolCount int) string {
	if !capabilities.CallMcpEnabled && !capabilities.FetchResourceEnabled && directToolCount == 0 {
		return "MCP tools and resources are not available for this request. Do not call MCP capabilities mentioned only in earlier conversation context."
	}
	parts := make([]string, 0, 3)
	if directToolCount > 0 && !capabilities.CallMcpEnabled {
		parts = append(parts, "Use only the direct MCP tool schemas provided for this request; CallMcpTool is unavailable.")
	}
	if capabilities.CallMcpEnabled {
		pairs := sortedMCPPairs(capabilities.Pairs, mcpCapabilityReminderPairLimit)
		if len(pairs) > 0 {
			parts = append(parts, "CallMcpTool is limited to these current server/tool pairs: "+strings.Join(pairs, ", ")+".")
		}
	}
	if capabilities.FetchResourceEnabled {
		servers := sortedStringSet(capabilities.ResourceServers, mcpCapabilityReminderServerLimit)
		if len(servers) > 0 {
			parts = append(parts, "FetchMcpResource is limited to these current servers: "+strings.Join(servers, ", ")+".")
		}
	}
	return strings.Join(parts, " ")
}

func sortedMCPPairs(pairs map[string]struct{}, limit int) []string {
	values := make([]string, 0, len(pairs))
	for pair := range pairs {
		parts := strings.SplitN(pair, mcpPairSeparator, 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			continue
		}
		values = append(values, parts[0]+"/"+parts[1])
	}
	sort.Strings(values)
	if limit > 0 && len(values) > limit {
		values = append(values[:limit], fmt.Sprintf("and %d more", len(values)-limit))
	}
	return values
}

func sortedStringSet(values map[string]struct{}, limit int) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	sort.Strings(result)
	if limit > 0 && len(result) > limit {
		result = append(result[:limit], fmt.Sprintf("and %d more", len(result)-limit))
	}
	return result
}
