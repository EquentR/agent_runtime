package openai_responses

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

type responsesModelConfig struct {
	isReasoningModel       bool
	systemMessageMode      string
	requiredAutoTruncation bool
}

func getResponsesModelConfig(modelID string) responsesModelConfig {
	defaults := responsesModelConfig{
		systemMessageMode:      "system",
		requiredAutoTruncation: false,
	}

	if strings.Contains(modelID, "gpt-5-chat") {
		return defaults
	}

	if strings.HasPrefix(modelID, "o1") || strings.Contains(modelID, "-o1") ||
		strings.HasPrefix(modelID, "o3") || strings.Contains(modelID, "-o3") ||
		strings.HasPrefix(modelID, "o4") || strings.Contains(modelID, "-o4") ||
		strings.HasPrefix(modelID, "oss") || strings.Contains(modelID, "-oss") ||
		strings.Contains(modelID, "gpt-5") || strings.Contains(modelID, "codex-") ||
		strings.Contains(modelID, "computer-use") {
		if strings.Contains(modelID, "o1-mini") || strings.Contains(modelID, "o1-preview") {
			return responsesModelConfig{
				isReasoningModel:       true,
				systemMessageMode:      "remove",
				requiredAutoTruncation: defaults.requiredAutoTruncation,
			}
		}
		return responsesModelConfig{
			isReasoningModel:       true,
			systemMessageMode:      "developer",
			requiredAutoTruncation: defaults.requiredAutoTruncation,
		}
	}

	return defaults
}

func buildResponseRequestParams(req model.ChatRequest) (responses.ResponseNewParams, error) {
	modelConfig := getResponsesModelConfig(req.Model)
	input, _, err := buildResponseInput(req.Messages, modelConfig.systemMessageMode)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}

	params := responses.ResponseNewParams{
		Model: req.Model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Store: openai.Bool(false),
		Tools: modelToolsToResponse(req.Tools),
	}
	if modelConfig.isReasoningModel {
		params.Reasoning = shared.ReasoningParam{
			Summary: shared.ReasoningSummaryAuto,
			Effort:  shared.ReasoningEffortMedium,
		}
	}
	if strings.TrimSpace(req.PromptCacheKey) != "" {
		params.PromptCacheKey = openai.String(strings.TrimSpace(req.PromptCacheKey))
	}

	if req.MaxTokens > 0 {
		params.MaxOutputTokens = openai.Int(req.MaxTokens)
	}
	if req.Sampling.Temperature != nil && !modelConfig.isReasoningModel {
		params.Temperature = openai.Float(float64(*req.Sampling.Temperature))
	}
	if req.Sampling.TopP != nil && !modelConfig.isReasoningModel {
		params.TopP = openai.Float(float64(*req.Sampling.TopP))
	}

	toolChoice, err := modelToolChoiceToResponse(req.ToolChoice)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	if toolChoice != nil {
		params.ToolChoice = *toolChoice
	}

	if requestEndsWithToolContinuation(req.Messages) {
		params.Tools = nil
	}
	if modelConfig.requiredAutoTruncation {
		params.Truncation = responses.ResponseNewParamsTruncationAuto
	}

	return params, nil
}

func responseInputHasToolOutput(input responses.ResponseInputParam) bool {
	for _, item := range input {
		if raw, err := json.Marshal(item); err == nil {
			var obj map[string]any
			if err := json.Unmarshal(raw, &obj); err == nil {
				if obj["type"] == "function_call_output" {
					return true
				}
			}
		}
	}
	return false
}

func requestEndsWithToolContinuation(messages []model.Message) bool {
	if len(messages) == 0 {
		return false
	}
	hasTrailingTool := false
	for i := len(messages) - 1; i >= 0; i-- {
		switch messages[i].Role {
		case model.RoleTool:
			hasTrailingTool = true
		case model.RoleAssistant:
			return hasTrailingTool
		case model.RoleSystem, model.RoleUser:
			if hasTrailingTool {
				return false
			}
		default:
			if hasTrailingTool {
				return false
			}
		}
	}
	return false
}

func buildResponseInput(messages []model.Message, systemMessageMode string) (responses.ResponseInputParam, string, error) {
	input := make(responses.ResponseInputParam, 0, len(messages))

	for i, m := range messages {
		toolContinuation := hasFollowingToolOutput(messages, i)
		switch m.Role {
		case model.RoleSystem:
			if strings.TrimSpace(m.Content) == "" {
				continue
			}
			switch systemMessageMode {
			case "developer":
				input = append(input, responses.ResponseInputItemParamOfMessage(m.Content, responses.EasyInputMessageRoleDeveloper))
			case "remove":
				continue
			default:
				input = append(input, responses.ResponseInputItemParamOfMessage(m.Content, responses.EasyInputMessageRoleSystem))
			}
		case model.RoleUser:
			content, err := buildUserMessageContent(m)
			if err != nil {
				return nil, "", err
			}
			input = append(input, responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser))
		case model.RoleAssistant:
			if rawItems, _, ok, err := rawOutputItemsFromProviderData(m.ProviderData); err != nil {
				return nil, "", err
			} else if ok {
				for _, item := range filterReplayOutputItems(rawItems, toolContinuation) {
					param, convErr := responseOutputItemToInputParam(item)
					if convErr != nil {
						return nil, "", convErr
					}
					input = append(input, param)
				}
				continue
			}
			replayed, ok, err := outputItemsFromProviderState(m.ProviderState)
			if err != nil {
				return nil, "", err
			}
			if ok {
				input = append(input, filterReplayInputItems(replayed, toolContinuation)...)
				continue
			}
			if itemRefs, ok, err := providerStateItemReferences(m.ProviderState); err != nil {
				return nil, "", err
			} else if ok {
				input = append(input, filterReplayInputItems(itemRefs, toolContinuation)...)
				continue
			}
			for _, item := range m.ReasoningItems {
				input = append(input, modelReasoningItemToResponse(item))
			}
			if strings.TrimSpace(m.Content) != "" || len(m.ToolCalls) == 0 {
				input = append(input, responses.ResponseInputItemParamOfMessage(m.Content, responses.EasyInputMessageRoleAssistant))
			}
			for _, tc := range m.ToolCalls {
				input = append(input, responses.ResponseInputItemParamOfFunctionCall(tc.Arguments, tc.ID, tc.Name))
			}
		case model.RoleTool:
			if strings.TrimSpace(m.ToolCallId) == "" {
				return nil, "", errors.New("tool message missing ToolCallId")
			}
			input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(m.ToolCallId, m.Content))
		default:
			return nil, "", fmt.Errorf("unsupported message role: %s", m.Role)
		}
	}

	return input, "", nil
}

func buildUserMessageContent(message model.Message) (responses.ResponseInputMessageContentListParam, error) {
	content := make(responses.ResponseInputMessageContentListParam, 0, len(message.Attachments)+1)
	if strings.TrimSpace(message.Content) != "" {
		content = append(content, responses.ResponseInputContentParamOfInputText(message.Content))
	}
	for _, attachment := range message.Attachments {
		item, err := responseContentItemFromAttachment(attachment)
		if err != nil {
			return nil, err
		}
		content = append(content, item)
	}
	if len(content) == 0 {
		content = append(content, responses.ResponseInputContentParamOfInputText(""))
	}
	return content, nil
}

func responseContentItemFromAttachment(attachment model.Attachment) (responses.ResponseInputContentUnionParam, error) {
	mimeType := strings.TrimSpace(attachment.MimeType)
	if mimeType == "" {
		mimeType = http.DetectContentType(attachment.Data)
	}
	switch {
	case strings.HasPrefix(strings.ToLower(mimeType), "image/"):
		if len(attachment.Data) == 0 {
			return responses.ResponseInputContentUnionParam{}, fmt.Errorf("image attachment %q data is empty", attachment.FileName)
		}
		item := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
		item.OfInputImage.ImageURL = openai.String("data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(attachment.Data))
		return item, nil
	case isTextMimeType(mimeType) || (strings.TrimSpace(attachment.MimeType) == "" && utf8.Valid(attachment.Data)):
		if len(attachment.Data) == 0 {
			return responses.ResponseInputContentUnionParam{}, fmt.Errorf("text attachment %q data is empty", attachment.FileName)
		}
		fileItem := responses.ResponseInputContentUnionParam{
			OfInputFile: &responses.ResponseInputFileParam{},
		}
		fileItem.OfInputFile.FileData = openai.String(base64.StdEncoding.EncodeToString(attachment.Data))
		fileItem.OfInputFile.Filename = openai.String(firstNonEmpty(strings.TrimSpace(attachment.FileName), "attachment.txt"))
		return fileItem, nil
	default:
		return responses.ResponseInputContentUnionParam{}, fmt.Errorf("unsupported attachment type %q", mimeType)
	}
}

func isTextMimeType(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	return strings.HasPrefix(mimeType, "text/") || mimeType == "application/json" || strings.HasSuffix(mimeType, "+json")
}

func hasFollowingToolOutput(messages []model.Message, index int) bool {
	if index < 0 || index >= len(messages) {
		return false
	}
	for _, message := range messages[index+1:] {
		if message.Role == model.RoleTool {
			return true
		}
	}
	return false
}

func filterReplayOutputItems(items []responses.ResponseOutputItemUnion, toolContinuation bool) []responses.ResponseOutputItemUnion {
	if !toolContinuation {
		return items
	}
	filtered := make([]responses.ResponseOutputItemUnion, 0, len(items))
	for _, item := range items {
		if item.Type == "function_call" {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterReplayInputItems(items []responses.ResponseInputItemUnionParam, toolContinuation bool) []responses.ResponseInputItemUnionParam {
	if !toolContinuation {
		return items
	}
	filtered := make([]responses.ResponseInputItemUnionParam, 0, len(items))
	for _, item := range items {
		raw, err := json.Marshal(item)
		if err != nil {
			filtered = append(filtered, item)
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			filtered = append(filtered, item)
			continue
		}
		typeName, _ := obj["type"].(string)
		if typeName == "function_call" || typeName == "item_reference" {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func modelToolsToResponse(tools []types.Tool) []responses.ToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	out := make([]responses.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		params := responseToolSchemaParameters(tool.Parameters)
		strict := shouldUseStrictToolSchema(tool.Parameters)
		out = append(out, responses.ToolUnionParam{OfFunction: &responses.FunctionToolParam{
			Name:        tool.Name,
			Description: openai.String(tool.Description),
			Parameters:  params,
			Strict:      openai.Bool(strict),
		}})
	}

	return out
}

func responseToolSchemaParameters(schema types.JSONSchema) map[string]any {
	properties := schema.Properties
	if properties == nil {
		properties = map[string]types.SchemaProperty{}
	}

	required := schema.Required
	if required == nil {
		required = []string{}
	}

	return map[string]any{
		"type":                 schema.Type,
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func shouldUseStrictToolSchema(schema types.JSONSchema) bool {
	if len(schema.Properties) != len(schema.Required) {
		return false
	}

	required := make(map[string]struct{}, len(schema.Required))
	for _, name := range schema.Required {
		required[name] = struct{}{}
	}

	for name := range schema.Properties {
		if _, ok := required[name]; !ok {
			return false
		}
	}

	return true
}

func modelToolChoiceToResponse(choice types.ToolChoice) (*responses.ResponseNewParamsToolChoiceUnion, error) {
	switch choice.Type {
	case "":
		return nil, nil
	case types.ToolAuto:
		u := responses.ResponseNewParamsToolChoiceUnion{OfToolChoiceMode: openai.Opt(responses.ToolChoiceOptionsAuto)}
		return &u, nil
	case types.ToolNone:
		u := responses.ResponseNewParamsToolChoiceUnion{OfToolChoiceMode: openai.Opt(responses.ToolChoiceOptionsNone)}
		return &u, nil
	case types.ToolForce:
		if strings.TrimSpace(choice.Name) == "" {
			u := responses.ResponseNewParamsToolChoiceUnion{OfToolChoiceMode: openai.Opt(responses.ToolChoiceOptionsRequired)}
			return &u, nil
		}
		u := responses.ResponseNewParamsToolChoiceUnion{OfFunctionTool: &responses.ToolChoiceFunctionParam{Name: choice.Name}}
		return &u, nil
	default:
		return nil, errors.New("unsupported tool choice type")
	}
}

func extractChatResponse(resp *responses.Response) (model.ChatResponse, error) {
	if resp == nil {
		return model.ChatResponse{}, errors.New("openai responses returned nil response")
	}

	toolCalls := make([]types.ToolCall, 0)
	reasoningParts := make([]string, 0)
	reasoningItems := make([]model.ReasoningItem, 0)
	for _, item := range resp.Output {
		if item.Type == "reasoning" {
			reasoningItems = append(reasoningItems, responseReasoningItemToModel(item))
			for _, summary := range item.Summary {
				reasoningParts = append(reasoningParts, summary.Text)
			}
		}
		if item.Type != "function_call" {
			continue
		}
		toolCalls = append(toolCalls, types.ToolCall{
			ID:        item.CallID,
			Name:      item.Name,
			Arguments: item.Arguments.OfString,
		})
	}
	if len(toolCalls) == 0 {
		toolCalls = nil
	}
	if len(reasoningItems) == 0 {
		reasoningItems = nil
	}

	extractedReasoning, answer := model.SplitLeadingThinkBlock(resp.OutputText())
	reasoning := strings.TrimSpace(strings.Join(reasoningParts, "\n"))
	if reasoning == "" {
		reasoning = extractedReasoning
	}
	state, err := providerStateFromOutputItems(resp.ID, resp.Output)
	if err != nil {
		return model.ChatResponse{}, err
	}
	out := model.ChatResponse{
		Message: finalAssistantMessageFromResponse(answer, reasoning, reasoningItems, toolCalls, state),
		Usage:   toModelUsage(resp.Usage),
	}
	out.SyncFieldsFromMessage()
	return out, nil
}

func modelReasoningItemToResponse(item model.ReasoningItem) responses.ResponseInputItemUnionParam {
	summary := make([]responses.ResponseReasoningItemSummaryParam, 0, len(item.Summary))
	for _, part := range item.Summary {
		summary = append(summary, responses.ResponseReasoningItemSummaryParam{Text: part.Text})
	}
	param := responses.ResponseInputItemParamOfReasoning(item.ID, summary)
	if item.EncryptedContent != "" && param.OfReasoning != nil {
		param.OfReasoning.EncryptedContent = openai.Opt(item.EncryptedContent)
	}
	return param
}

func responseReasoningItemToModel(item responses.ResponseOutputItemUnion) model.ReasoningItem {
	out := model.ReasoningItem{
		ID:               item.ID,
		EncryptedContent: item.EncryptedContent,
	}
	if len(item.Summary) > 0 {
		out.Summary = make([]model.ReasoningSummary, 0, len(item.Summary))
		for _, part := range item.Summary {
			out.Summary = append(out.Summary, model.ReasoningSummary{Text: part.Text})
		}
	}
	return out
}

func toModelUsage(usage responses.ResponseUsage) model.TokenUsage {
	return model.TokenUsage{
		PromptTokens:       usage.InputTokens,
		CachedPromptTokens: usage.InputTokensDetails.CachedTokens,
		CompletionTokens:   usage.OutputTokens,
		TotalTokens:        usage.TotalTokens,
	}
}
