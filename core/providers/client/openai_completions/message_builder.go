package openai

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/sashabaranov/go-openai"
)

func buildOpenAIMessages(messages []model.Message) ([]openai.ChatCompletionMessage, []string, error) {
	msgs := make([]openai.ChatCompletionMessage, 0, len(messages))
	promptMessages := make([]string, 0, len(messages))

	for _, m := range messages {
		if m.Role == model.RoleAssistant {
			replayed, ok, err := messageFromProviderState(m.ProviderState)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				msgs = append(msgs, replayed)
				promptMessages = append(promptMessages, promptMessageFromOpenAIMessage(replayed))
				continue
			}
		}

		toolCalls := modelToolCallsToOpenAI(m.ToolCalls)

		if len(m.Attachments) == 0 {
			msgs = append(msgs, openai.ChatCompletionMessage{
				Role:             m.Role,
				Content:          m.Content,
				ReasoningContent: m.Reasoning,
				ToolCalls:        toolCalls,
				ToolCallID:       m.ToolCallId,
			})
			promptMessages = append(promptMessages, m.Content)
			continue
		}

		parts := make([]openai.ChatMessagePart, 0, len(m.Attachments)*2+1)
		promptParts := make([]string, 0, len(m.Attachments)+1)
		if m.Content != "" {
			parts = append(parts, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeText,
				Text: m.Content,
			})
			promptParts = append(promptParts, m.Content)
		}

		for _, attachment := range m.Attachments {
			attachmentParts, promptPart, err := toChatMessageParts(attachment)
			if err != nil {
				return nil, nil, err
			}
			parts = append(parts, attachmentParts...)
			if promptPart != "" {
				promptParts = append(promptParts, promptPart)
			}
		}

		msg := openai.ChatCompletionMessage{
			Role:             m.Role,
			ReasoningContent: m.Reasoning,
			ToolCalls:        toolCalls,
			ToolCallID:       m.ToolCallId,
		}
		if len(parts) > 0 {
			msg.MultiContent = parts
		} else {
			msg.Content = m.Content
		}
		msgs = append(msgs, msg)
		promptMessages = append(promptMessages, strings.Join(promptParts, "\n"))
	}

	return msgs, promptMessages, nil
}

func modelToolCallsToOpenAI(toolCalls []types.ToolCall) []openai.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}

	result := make([]openai.ToolCall, 0, len(toolCalls))
	for _, tc := range toolCalls {
		result = append(result, openai.ToolCall{
			ID:   tc.ID,
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      tc.Name,
				Arguments: tc.Arguments,
			},
		})
	}

	return result
}

func toChatMessageParts(attachment model.Attachment) ([]openai.ChatMessagePart, string, error) {
	mimeType := strings.TrimSpace(attachment.MimeType)
	if mimeType == "" {
		mimeType = http.DetectContentType(attachment.Data)
	}

	if model.IsRasterImageMimeType(mimeType) {
		if len(attachment.Data) == 0 {
			return nil, "", fmt.Errorf("image attachment %q data is empty", attachment.FileName)
		}
		encoded := base64.StdEncoding.EncodeToString(attachment.Data)
		dataURL := "data:" + mimeType + ";base64," + encoded
		parts := []openai.ChatMessagePart{{
			Type: openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{
				URL: dataURL,
			},
		}}
		promptText := model.ImageAttachmentReferenceText(attachment)
		if promptText != "" {
			parts = append(parts, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeText,
				Text: promptText,
			})
		}
		return parts, promptText, nil
	}

	return nil, "", fmt.Errorf("unsupported attachment type: %s", mimeType)
}
