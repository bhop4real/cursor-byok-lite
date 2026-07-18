// compiler.go 负责把固定 prompt、自然历史和 tool catalog 编译成 provider 请求。
package forwarder

import (
	"fmt"
	"strings"
	"unicode"

	"cursor/gen/agentv1"
	modeladapter "cursor/internal/backend/agent/model"
	promptassets "cursor/prompt"
)

type PromptCompiler interface {
	Compile(conversation *ConversationFile, mode agentv1.AgentMode, latestUserText string, modelName string) (CompiledConversation, error)
	DerivePromptContexts(conversation *ConversationFile, mode agentv1.AgentMode, latestUserText string) ([]PromptContextMessage, error)
}

type replayPromptCompiler interface {
	CompileWithReplay(conversation *ConversationFile, mode agentv1.AgentMode, latestUserText string, modelName string, replayMessages []modeladapter.Message) (CompiledConversation, error)
}

type ResponseLanguageSource interface {
	ResponseLanguage() string
}

type DefaultPromptCompiler struct {
	projector        *HistoryProjector
	catalog          ToolCatalog
	reminders        ReminderInjector
	rules            *UserRuleStore
	responseLanguage ResponseLanguageSource
}

// NewPromptCompiler 创建默认 prompt 编译器。
func NewPromptCompiler(projector *HistoryProjector, catalog ToolCatalog, reminders ReminderInjector, rules *UserRuleStore, responseLanguage ...ResponseLanguageSource) *DefaultPromptCompiler {
	var source ResponseLanguageSource
	if len(responseLanguage) > 0 {
		source = responseLanguage[0]
	}
	return &DefaultPromptCompiler{
		projector:        projector,
		catalog:          catalog,
		reminders:        reminders,
		rules:            rules,
		responseLanguage: source,
	}
}

// Compile 生成当前 turn 应发送给 provider 的消息和工具集合。
func (compiler *DefaultPromptCompiler) Compile(conversation *ConversationFile, mode agentv1.AgentMode, latestUserText string, modelName string) (CompiledConversation, error) {
	if compiler == nil || compiler.projector == nil {
		return CompiledConversation{}, fmt.Errorf("prompt compiler dependencies are not initialized")
	}
	replayMessages, err := compiler.projector.ProjectPromptReplay(conversation)
	if err != nil {
		return CompiledConversation{}, err
	}
	return compiler.CompileWithReplay(conversation, mode, latestUserText, modelName, replayMessages)
}

// CompileWithReplay 复用已投影的 replay 生成 provider 请求。
func (compiler *DefaultPromptCompiler) CompileWithReplay(conversation *ConversationFile, mode agentv1.AgentMode, latestUserText string, modelName string, replayMessages []modeladapter.Message) (CompiledConversation, error) {
	if compiler == nil || compiler.catalog == nil {
		return CompiledConversation{}, fmt.Errorf("prompt compiler dependencies are not initialized")
	}
	normalizedMode, err := validateSupportedActiveMode(mode)
	if err != nil {
		return CompiledConversation{}, err
	}
	subagentTypeName := ""
	if conversation != nil {
		subagentTypeName = conversation.SubagentTypeName
	}
	assetMode, err := promptAssetModeForConversation(normalizedMode, subagentTypeName)
	if err != nil {
		return CompiledConversation{}, err
	}
	systemPrompt, err := promptassets.ReadPrompt(assetMode)
	if err != nil {
		return CompiledConversation{}, err
	}
	tools, _, err := compiler.catalog.Load(normalizedMode, subagentTypeName)
	if err != nil {
		return CompiledConversation{}, err
	}
	sharedRulesPrompt := ""
	sharedRuleCount := 0
	sharedRuleTotal := 0
	if compiler.rules != nil && normalizedMode != agentv1.AgentMode_AGENT_MODE_DEBUG {
		sharedRulesPrompt, sharedRuleTotal, sharedRuleCount, err = compiler.rules.BuildSystemPromptSection()
		if err != nil {
			return CompiledConversation{}, err
		}
	}
	responseLanguage := resolveResponseLanguage(compiler.configuredResponseLanguage(), latestUserText)
	messages := make([]modeladapter.Message, 0, len(replayMessages)+2)
	systemParts := []string{sanitizePromptAsset(systemPrompt, modelName)}
	if strings.TrimSpace(sharedRulesPrompt) != "" {
		systemParts = append(systemParts, sharedRulesPrompt)
	}
	systemText := strings.TrimSpace(strings.Join(filterNonEmpty(systemParts), "\n\n"))
	if systemText != "" {
		messages = append(messages, modeladapter.Message{
			Role:    "system",
			Content: systemText,
		})
	}
	stableReplayCount, err := compiler.stableReplayMessageCount(conversation, replayMessages)
	if err != nil {
		return CompiledConversation{}, err
	}
	messages = append(messages, replayMessages...)
	messages = append(messages, modeladapter.Message{
		Role:    "user",
		Content: responseLanguageInstruction(responseLanguage),
	})
	messages = append(messages, modeladapter.Message{
		Role:    "user",
		Content: currentPassExecutionContract(conversation, replayMessages),
	})
	return CompiledConversation{
		Mode:               normalizedMode,
		Messages:           messages,
		StableMessageCount: stableReplayCount,
		Tools:              tools,
		CompileSummary:     fmt.Sprintf("mode=%s asset_mode=%s child=%t messages=%d tools=%d shared_rules_total=%d shared_rules_deduped=%d response_language=%s", normalizedMode.String(), string(assetMode), isChildConversationSubagentTypeName(subagentTypeName), len(messages), len(tools), sharedRuleTotal, sharedRuleCount, responseLanguage),
	}, nil
}

func (compiler *DefaultPromptCompiler) configuredResponseLanguage() string {
	if compiler == nil || compiler.responseLanguage == nil {
		return "auto"
	}
	return strings.TrimSpace(compiler.responseLanguage.ResponseLanguage())
}

func resolveResponseLanguage(configured string, latestUserText string) string {
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "en", "en-us":
		return "en-US"
	case "zh", "zh-cn":
		return "zh-CN"
	case "ja", "ja-jp":
		return "ja-JP"
	}

	latinCount := 0
	hanCount := 0
	kanaCount := 0
	for _, value := range latestUserText {
		switch {
		case unicode.In(value, unicode.Hiragana, unicode.Katakana):
			kanaCount++
		case unicode.In(value, unicode.Han):
			hanCount++
		case unicode.Is(unicode.Latin, value) && unicode.IsLetter(value):
			latinCount++
		}
	}
	switch {
	case kanaCount > 0:
		return "ja-JP"
	case hanCount > latinCount:
		return "zh-CN"
	default:
		return "en-US"
	}
}

func responseLanguageInstruction(language string) string {
	var requirement string
	switch language {
	case "zh-CN":
		requirement = "Use Simplified Chinese for all natural-language responses."
	case "ja-JP":
		requirement = "Use Japanese for all natural-language responses."
	default:
		requirement = "Use English for all natural-language responses."
	}
	return wrapSystemReminder(requirement + " This current-turn requirement overrides older response-language patterns in the conversation. Keep code identifiers, commands, paths, logs, and error text in their original language.")
}

func (compiler *DefaultPromptCompiler) DerivePromptContexts(conversation *ConversationFile, mode agentv1.AgentMode, latestUserText string) ([]PromptContextMessage, error) {
	if compiler == nil || compiler.projector == nil || compiler.catalog == nil || compiler.reminders == nil {
		return nil, fmt.Errorf("prompt compiler dependencies are not initialized")
	}
	normalizedMode, err := validateSupportedActiveMode(mode)
	if err != nil {
		return nil, err
	}
	subagentTypeName := ""
	if conversation != nil {
		subagentTypeName = conversation.SubagentTypeName
	}
	_, toolNames, err := compiler.catalog.Load(normalizedMode, subagentTypeName)
	if err != nil {
		return nil, err
	}
	replayMessages, err := compiler.projector.ProjectPromptReplay(conversation)
	if err != nil {
		return nil, err
	}
	structuredStatePromptContexts, structuredStateTailMessages, err := buildStructuredStatePromptContexts(conversation)
	if err != nil {
		return nil, err
	}
	promptReminders := compiler.reminders.Inject(normalizedMode, conversation, replayMessages, latestUserText, toolNames)
	candidates := make([]PromptContextMessage, 0, len(structuredStatePromptContexts)+len(structuredStateTailMessages)+len(promptReminders.PromptContexts)+len(promptReminders.TailMessages))
	candidates = append(candidates, structuredStatePromptContexts...)
	for _, message := range structuredStateTailMessages {
		candidates = append(candidates, newPromptContextMessage(promptContextSourceStructuredTodoReminder, message, true))
	}
	candidates = append(candidates, promptReminders.PromptContexts...)
	for index, message := range promptReminders.TailMessages {
		candidates = append(candidates, newPromptContextMessage(fmt.Sprintf("tail_reminder/%d", index), message, true))
	}
	for index := range candidates {
		candidates[index].Persist = true
	}
	return filterCurrentTurnPromptContexts(conversation, candidates), nil
}

func (compiler *DefaultPromptCompiler) stableReplayMessageCount(conversation *ConversationFile, replayMessages []modeladapter.Message) (int, error) {
	if compiler == nil || compiler.projector == nil || conversation == nil || len(replayMessages) == 0 {
		return 0, nil
	}
	currentTurnSeq := conversation.CurrentTurnSeq
	if currentTurnSeq <= 0 {
		currentTurnSeq = conversation.NextTurnSeq - 1
	}
	stableCount := 0
	if currentTurnSeq > 0 {
		count, err := compiler.projector.StablePromptReplayMessageCount(conversation, currentTurnSeq)
		if err != nil {
			return 0, err
		}
		stableCount = count
	}
	if requestPrefixReplayCount := replayMessageCountFromRequestPrefix(conversation); requestPrefixReplayCount > stableCount {
		stableCount = requestPrefixReplayCount
	}
	if stableCount > len(replayMessages) {
		return len(replayMessages), nil
	}
	return stableCount, nil
}

func replayMessageCountFromRequestPrefix(conversation *ConversationFile) int {
	if conversation == nil || conversation.LatestRequestPrefix == nil {
		return 0
	}
	requestID := strings.TrimSpace(conversation.CurrentRequestID)
	if requestID == "" || strings.TrimSpace(conversation.LatestRequestPrefix.RequestID) != requestID {
		return 0
	}
	return conversation.LatestRequestPrefix.ReplayMessageCount
}

func stableReplayEntriesBeforeTurn(entries []HistoryEntry, currentTurnSeq int64) []HistoryEntry {
	if len(entries) == 0 || currentTurnSeq <= 0 {
		return nil
	}
	filtered := make([]HistoryEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.TurnSeq > 0 && entry.TurnSeq >= currentTurnSeq {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// filterNonEmpty 过滤掉空白字符串，便于安全拼接 system prompt 片段。
func filterNonEmpty(items []string) []string {
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			filtered = append(filtered, strings.TrimSpace(item))
		}
	}
	return filtered
}

func mergeAdjacentPlainUserMessages(messages []modeladapter.Message) []modeladapter.Message {
	if len(messages) == 0 {
		return nil
	}
	merged := make([]modeladapter.Message, 0, len(messages))
	for _, message := range messages {
		text, ok := plainUserMessageText(message)
		if !ok {
			merged = append(merged, message)
			continue
		}
		if len(merged) == 0 {
			message.Role = "user"
			message.Content = text
			merged = append(merged, message)
			continue
		}
		last := &merged[len(merged)-1]
		if lastText, ok := plainUserMessageText(*last); ok {
			last.Role = "user"
			last.Content = lastText + "\n\n" + text
			continue
		}
		message.Role = "user"
		message.Content = text
		merged = append(merged, message)
	}
	return merged
}

func plainUserMessageText(message modeladapter.Message) (string, bool) {
	if strings.TrimSpace(message.Role) != "user" {
		return "", false
	}
	if len(message.ContentParts) > 0 || len(message.ToolCalls) > 0 {
		return "", false
	}
	if strings.TrimSpace(message.ToolCallID) != "" || strings.TrimSpace(message.Name) != "" {
		return "", false
	}
	if strings.TrimSpace(message.ReasoningContent) != "" ||
		strings.TrimSpace(message.ReasoningSignature) != "" ||
		strings.TrimSpace(message.ReasoningSignatureSource) != "" ||
		strings.TrimSpace(message.OpenAIResponsesReasoningID) != "" ||
		strings.TrimSpace(message.OpenAIResponsesReasoningStatus) != "" ||
		len(message.OpenAIResponsesReasoningSummary) > 0 {
		return "", false
	}
	text := strings.TrimSpace(message.Content)
	if text == "" {
		return "", false
	}
	return text, true
}
