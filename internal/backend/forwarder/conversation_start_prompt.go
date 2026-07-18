package forwarder

import (
	"encoding/json"
	"fmt"
	"strings"

	"cursor/gen/agentv1"
	promptassets "cursor/prompt"
)

const conversationStartPromptSummaryTag = "conversation_start_prompt_md"

type conversationStartPromptSnapshot struct {
	Mode    string
	Path    string
	Model   string
	Content string
}

func ensureConversationStartPrompt(conversation *ConversationFile, fallbackMode agentv1.AgentMode, fallbackModelName string) error {
	if conversation == nil || strings.TrimSpace(conversation.ConversationStartPromptContent) != "" {
		return nil
	}
	startMode := initialConversationMode(conversation.Entries, fallbackMode)
	assetMode, err := promptAssetModeForConversation(startMode, conversation.SubagentTypeName)
	if err != nil {
		return err
	}
	promptText, err := promptassets.ReadPrompt(assetMode)
	if err != nil {
		return err
	}
	modelName := initialConversationModelName(conversation.Entries, fallbackModelName)
	content := strings.TrimSpace(sanitizePromptAsset(promptText, modelName))
	if content == "" {
		return fmt.Errorf("conversation-start prompt asset is empty")
	}
	path, err := promptassets.PromptPath(assetMode)
	if err != nil {
		return err
	}
	conversation.ConversationStartPromptMode = string(assetMode)
	conversation.ConversationStartPromptPath = path
	conversation.ConversationStartPromptModel = strings.TrimSpace(modelName)
	conversation.ConversationStartPromptContent = content
	return nil
}

func initialConversationMode(entries []HistoryEntry, fallback agentv1.AgentMode) agentv1.AgentMode {
	for _, entry := range entries {
		if strings.TrimSpace(entry.Kind) != "metadata" {
			continue
		}
		var payload metadataPayload
		if json.Unmarshal(entry.Payload, &payload) != nil || strings.TrimSpace(payload.Type) != "mode" {
			continue
		}
		mode, err := parseModeAlias(readStringValue(payload.Value["mode"]))
		if err == nil {
			return mode
		}
	}
	return fallback
}

func initialConversationModelName(entries []HistoryEntry, fallback string) string {
	for _, entry := range entries {
		if strings.TrimSpace(entry.Kind) != "metadata" {
			continue
		}
		var payload metadataPayload
		if json.Unmarshal(entry.Payload, &payload) != nil || strings.TrimSpace(payload.Type) != "run_request" {
			continue
		}
		if modelName := strings.TrimSpace(readStringValue(payload.Value["model_name"])); modelName != "" {
			return modelName
		}
	}
	return strings.TrimSpace(fallback)
}

func conversationStartPromptFromStream(stream *ActiveStream) conversationStartPromptSnapshot {
	if stream == nil {
		return conversationStartPromptSnapshot{}
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.CheckpointConversation == nil {
		return conversationStartPromptSnapshot{}
	}
	conversation := stream.CheckpointConversation
	return conversationStartPromptSnapshot{
		Mode:    strings.TrimSpace(conversation.ConversationStartPromptMode),
		Path:    strings.TrimSpace(conversation.ConversationStartPromptPath),
		Model:   strings.TrimSpace(conversation.ConversationStartPromptModel),
		Content: conversation.ConversationStartPromptContent,
	}
}

func composeConversationStartPromptSummary(prompt conversationStartPromptSnapshot, summary string) string {
	trimmedSummary := strings.TrimSpace(summary)
	content := strings.TrimSpace(prompt.Content)
	if content == "" || strings.Contains(trimmedSummary, "<"+conversationStartPromptSummaryTag+">") {
		return trimmedSummary
	}
	metadata := make([]string, 0, 3)
	if prompt.Path != "" {
		metadata = append(metadata, "path: "+prompt.Path)
	}
	if prompt.Mode != "" {
		metadata = append(metadata, "mode: "+prompt.Mode)
	}
	if prompt.Model != "" {
		metadata = append(metadata, "model: "+prompt.Model)
	}
	parts := []string{"<" + conversationStartPromptSummaryTag + ">"}
	parts = append(parts, metadata...)
	parts = append(parts, "content:", content, "</"+conversationStartPromptSummaryTag+">")
	if trimmedSummary != "" {
		parts = append(parts, "", trimmedSummary)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
