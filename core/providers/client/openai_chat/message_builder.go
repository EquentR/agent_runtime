package openai_chat

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
)

func buildOpenAIChatMessages(messages []model.Message) ([]openai.ChatCompletionMessageParamUnion, []string, error) {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	promptMessages := make([]string, 0, len(messages))

	for _, message := range messages {
		if message.Role == model.RoleAssistant {
			replayed, replayState, ok, err := messageParamFromProviderState(message.ProviderState)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				result = append(result, replayed)
				promptMessages = append(promptMessages, replayState.Content)
				continue
			}
		}

		payload, promptText, err := chatMessagePayload(message)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, err
		}
		result = append(result, param.Override[openai.ChatCompletionMessageParamUnion](json.RawMessage(raw)))
		promptMessages = append(promptMessages, promptText)
	}

	return result, promptMessages, nil
}

func chatMessagePayload(message model.Message) (map[string]any, string, error) {
	payload := map[string]any{"role": message.Role}
	promptParts := make([]string, 0, len(message.Attachments)+1)

	if len(message.Attachments) == 0 {
		payload["content"] = message.Content
		if message.Content != "" {
			promptParts = append(promptParts, message.Content)
		}
	} else {
		parts := make([]map[string]any, 0, len(message.Attachments)*2+1)
		if message.Content != "" {
			parts = append(parts, map[string]any{"type": "text", "text": message.Content})
			promptParts = append(promptParts, message.Content)
		}
		for _, attachment := range message.Attachments {
			attachmentParts, promptText, err := chatContentPartsFromAttachment(attachment)
			if err != nil {
				return nil, "", err
			}
			parts = append(parts, attachmentParts...)
			if promptText != "" {
				promptParts = append(promptParts, promptText)
			}
		}
		payload["content"] = parts
	}

	if message.ToolCallId != "" {
		payload["tool_call_id"] = message.ToolCallId
	}
	if toolCalls := chatToolCallsPayload(message.ToolCalls); len(toolCalls) > 0 {
		payload["tool_calls"] = toolCalls
	}

	return payload, strings.Join(promptParts, "\n"), nil
}

func chatToolCallsPayload(toolCalls []types.ToolCall) []map[string]any {
	if len(toolCalls) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(toolCalls))
	for _, call := range toolCalls {
		result = append(result, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		})
	}
	return result
}

func chatContentPartsFromAttachment(attachment model.Attachment) ([]map[string]any, string, error) {
	mimeType := strings.TrimSpace(attachment.MimeType)
	if mimeType == "" {
		mimeType = http.DetectContentType(attachment.Data)
	}

	if strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		if len(attachment.Data) == 0 {
			return nil, "", fmt.Errorf("image attachment %q data is empty", attachment.FileName)
		}
		dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(attachment.Data)
		parts := []map[string]any{{
			"type":      "image_url",
			"image_url": map[string]any{"url": dataURL},
		}}
		promptText := model.ImageAttachmentReferenceText(attachment)
		if promptText != "" {
			parts = append(parts, map[string]any{"type": "text", "text": promptText})
		}
		return parts, promptText, nil
	}

	if isTextMimeType(mimeType) || utf8.Valid(attachment.Data) {
		fileName := attachment.FileName
		if strings.TrimSpace(fileName) == "" {
			fileName = "attachment.txt"
		}
		text := "[attachment:" + fileName + "]\n" + string(attachment.Data)
		return []map[string]any{{"type": "text", "text": text}}, text, nil
	}

	return nil, "", fmt.Errorf("unsupported attachment type: %s", mimeType)
}

func isTextMimeType(mimeType string) bool {
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	return mimeType == "application/json" || strings.HasSuffix(mimeType, "+json") ||
		mimeType == "application/xml" || strings.HasSuffix(mimeType, "+xml")
}
