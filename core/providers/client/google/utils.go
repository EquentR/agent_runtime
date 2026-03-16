package google

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	genai "google.golang.org/genai"
)

func buildGenerateContentRequest(req model.ChatRequest) ([]*genai.Content, *genai.GenerateContentConfig, []string, error) {
	// 将统一消息链转换为 GenAI contents，并提取可选 systemInstruction。
	contents, systemInstruction, promptMessages, err := buildGenAIMessages(req.Messages)
	if err != nil {
		return nil, nil, nil, err
	}

	// 映射语义尽量与 OpenAI 适配层保持一致，降低跨 provider 行为差异。
	cfg := &genai.GenerateContentConfig{
		SystemInstruction: systemInstruction,
		Tools:             modelToolsToGenAI(req.Tools),
	}

	// GenAI 的 MaxOutputTokens 是 int32，而 llm_core 使用 int64。
	// 这里做上限截断，避免溢出。
	if req.MaxTokens > 0 {
		if req.MaxTokens > math.MaxInt32 {
			cfg.MaxOutputTokens = math.MaxInt32
		} else {
			cfg.MaxOutputTokens = int32(req.MaxTokens)
		}
	}

	// 采样参数保持指针语义，区分“未设置”和“显式设置为 0”。
	if req.Sampling.Temperature != nil {
		cfg.Temperature = req.Sampling.Temperature
	}
	if req.Sampling.TopP != nil {
		cfg.TopP = req.Sampling.TopP
	}
	if req.Sampling.TopK != nil {
		topK := float32(*req.Sampling.TopK)
		cfg.TopK = &topK
	}

	toolConfig, err := modelToolChoiceToGenAI(req.ToolChoice)
	if err != nil {
		return nil, nil, nil, err
	}
	if toolConfig != nil {
		cfg.ToolConfig = toolConfig
	}

	return contents, cfg, promptMessages, nil
}

func buildGenAIMessages(messages []model.Message) ([]*genai.Content, *genai.Content, []string, error) {
	contents := make([]*genai.Content, 0, len(messages))
	promptMessages := make([]string, 0, len(messages))
	systemTexts := make([]string, 0)
	toolCallNames := make(map[string]string)

	for _, m := range messages {
		switch m.Role {
		case model.RoleSystem:
			// GenAI 更推荐使用独立 SystemInstruction 字段，而不是普通对话轮次。
			// 因此这里将所有 system 消息聚合。
			systemText, err := renderMessageText(m)
			if err != nil {
				return nil, nil, nil, err
			}
			if systemText != "" {
				systemTexts = append(systemTexts, systemText)
			}
			promptMessages = append(promptMessages, systemText)
		case model.RoleUser:
			parts, promptText, err := buildUserMessageParts(m)
			if err != nil {
				return nil, nil, nil, err
			}
			contents = append(contents, &genai.Content{Role: genai.RoleUser, Parts: parts})
			promptMessages = append(promptMessages, promptText)
		case model.RoleAssistant:
			if replayed, ok, err := contentFromProviderState(m.ProviderState); err != nil {
				return nil, nil, nil, err
			} else if ok {
				promptParts := make([]string, 0, len(replayed.Parts))
				for _, part := range replayed.Parts {
					if part == nil {
						continue
					}
					if part.FunctionCall != nil {
						if part.FunctionCall.ID != "" {
							toolCallNames[part.FunctionCall.ID] = part.FunctionCall.Name
						}
						args, err := json.Marshal(part.FunctionCall.Args)
						if err != nil {
							return nil, nil, nil, err
						}
						promptParts = append(promptParts, part.FunctionCall.Name+"("+string(args)+")")
						continue
					}
					if part.Text != "" && !part.Thought {
						promptParts = append(promptParts, part.Text)
					}
				}
				contents = append(contents, replayed)
				promptMessages = append(promptMessages, strings.Join(promptParts, "\n"))
				continue
			}
			parts, promptText, err := buildAssistantMessageParts(m)
			if err != nil {
				return nil, nil, nil, err
			}
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					toolCallNames[tc.ID] = tc.Name
				}
			}
			contents = append(contents, &genai.Content{Role: genai.RoleModel, Parts: parts})
			promptMessages = append(promptMessages, promptText)
		case model.RoleTool:
			// 在 GenAI 对话协议中，tool 输出以 user 角色的 FunctionResponse 形式回传。
			responsePart, promptText, err := buildToolResponsePart(m, toolCallNames)
			if err != nil {
				return nil, nil, nil, err
			}
			contents = append(contents, &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{responsePart}})
			promptMessages = append(promptMessages, promptText)
		default:
			return nil, nil, nil, fmt.Errorf("unsupported message role: %s", m.Role)
		}
	}

	var systemInstruction *genai.Content
	if len(systemTexts) > 0 {
		systemInstruction = genai.NewContentFromText(strings.Join(systemTexts, "\n"), genai.RoleUser)
	}

	return contents, systemInstruction, promptMessages, nil
}

func renderMessageText(m model.Message) (string, error) {
	// 复用附件渲染逻辑，确保 system/user 的 token 统计口径一致。
	parts := make([]string, 0, len(m.Attachments)+1)
	if m.Content != "" {
		parts = append(parts, m.Content)
	}
	for _, attachment := range m.Attachments {
		_, promptPart, err := toPartFromAttachment(attachment)
		if err != nil {
			return "", err
		}
		if promptPart != "" {
			parts = append(parts, promptPart)
		}
	}
	return strings.Join(parts, "\n"), nil
}

func buildUserMessageParts(m model.Message) ([]*genai.Part, string, error) {
	parts := make([]*genai.Part, 0, len(m.Attachments)+1)
	promptParts := make([]string, 0, len(m.Attachments)+1)

	if m.Content != "" {
		parts = append(parts, genai.NewPartFromText(m.Content))
		promptParts = append(promptParts, m.Content)
	}

	for _, attachment := range m.Attachments {
		part, promptPart, err := toPartFromAttachment(attachment)
		if err != nil {
			return nil, "", err
		}
		parts = append(parts, part)
		if promptPart != "" {
			promptParts = append(promptParts, promptPart)
		}
	}

	if len(parts) == 0 {
		parts = append(parts, genai.NewPartFromText(""))
	}

	return parts, strings.Join(promptParts, "\n"), nil
}

func buildAssistantMessageParts(m model.Message) ([]*genai.Part, string, error) {
	parts := make([]*genai.Part, 0, len(m.ToolCalls)+1)
	promptParts := make([]string, 0, len(m.ToolCalls)+1)

	if m.Content != "" {
		parts = append(parts, genai.NewPartFromText(m.Content))
		promptParts = append(promptParts, m.Content)
	}

	for _, tc := range m.ToolCalls {
		// GenAI function call part 需要泛型 JSON 对象参数。
		args, err := parseJSONArgs(tc.Arguments)
		if err != nil {
			return nil, "", fmt.Errorf("invalid tool call args for %s: %w", tc.Name, err)
		}
		parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{
			ID:   tc.ID,
			Name: tc.Name,
			Args: args,
		}, ThoughtSignature: append([]byte(nil), tc.ThoughtSignature...)})
		promptParts = append(promptParts, tc.Name+"("+tc.Arguments+")")
	}

	if len(parts) == 0 {
		parts = append(parts, genai.NewPartFromText(""))
	}

	return parts, strings.Join(promptParts, "\n"), nil
}

func buildToolResponsePart(m model.Message, toolCallNames map[string]string) (*genai.Part, string, error) {
	if strings.TrimSpace(m.ToolCallId) == "" {
		return nil, "", errors.New("tool message missing ToolCallId")
	}

	// FunctionResponse 必须带函数名；优先根据 ToolCallId 从上一次 assistant
	// tool 调用中恢复。兜底名保证消息结构合法。
	name := toolCallNames[m.ToolCallId]
	if name == "" {
		name = "tool_response"
	}

	response, err := parseToolResponseContent(m.Content)
	if err != nil {
		return nil, "", err
	}

	part := &genai.Part{FunctionResponse: &genai.FunctionResponse{
		ID:       m.ToolCallId,
		Name:     name,
		Response: response,
	}}

	return part, m.Content, nil
}

func toPartFromAttachment(attachment model.Attachment) (*genai.Part, string, error) {
	mimeType := strings.TrimSpace(attachment.MimeType)
	if mimeType == "" {
		mimeType = http.DetectContentType(attachment.Data)
	}

	if strings.HasPrefix(mimeType, "image/") {
		// 图片按 inline bytes part 发送，符合 GenAI 多模态输入格式。
		if len(attachment.Data) == 0 {
			return nil, "", fmt.Errorf("image attachment %q data is empty", attachment.FileName)
		}
		return genai.NewPartFromBytes(attachment.Data, mimeType), "[image attachment]", nil
	}

	if isTextMimeType(mimeType) || utf8.Valid(attachment.Data) {
		// 文本类附件转成纯文本上下文，便于跨 provider 保持一致行为。
		fileName := attachment.FileName
		if fileName == "" {
			fileName = "attachment.txt"
		}
		text := "[附件:" + fileName + "]\n" + string(attachment.Data)
		return genai.NewPartFromText(text), text, nil
	}

	return nil, "", fmt.Errorf("unsupported attachment type: %s", mimeType)
}

func isTextMimeType(mimeType string) bool {
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	if mimeType == "application/json" || strings.HasSuffix(mimeType, "+json") {
		return true
	}
	if mimeType == "application/xml" || strings.HasSuffix(mimeType, "+xml") {
		return true
	}
	return false
}

func parseJSONArgs(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func parseToolResponseContent(content string) (map[string]any, error) {
	// tool 输出可能是对象、标量 JSON 或纯文本；这里统一归一化为
	// GenAI 期望的 response 对象结构。
	content = strings.TrimSpace(content)
	if content == "" {
		return map[string]any{"output": ""}, nil
	}

	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err == nil {
		if obj, ok := parsed.(map[string]any); ok {
			return obj, nil
		}
		return map[string]any{"output": parsed}, nil
	}

	return map[string]any{"output": content}, nil
}

func modelToolsToGenAI(tools []types.Tool) []*genai.Tool {
	if len(tools) == 0 {
		return nil
	}

	result := make([]*genai.Tool, 0, len(tools))
	for _, tool := range tools {
		// 直接使用 JSON schema map，避免结构转换损失并保持与上游 Tool 定义一致。
		jsonSchema := map[string]any{
			"type":       tool.Parameters.Type,
			"properties": tool.Parameters.Properties,
			"required":   tool.Parameters.Required,
		}
		result = append(result, &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:                 tool.Name,
			Description:          tool.Description,
			ParametersJsonSchema: jsonSchema,
		}}})
	}

	return result
}

func modelToolChoiceToGenAI(choice types.ToolChoice) (*genai.ToolConfig, error) {
	// ToolChoice 映射：
	// - auto  -> 模型自行决定是否调用工具
	// - none  -> 禁用工具调用，仅走文本回复
	// - force -> 强制函数调用，可选指定函数名
	switch choice.Type {
	case "":
		return nil, nil
	case types.ToolAuto:
		return &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAuto}}, nil
	case types.ToolNone:
		return &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeNone}}, nil
	case types.ToolForce:
		cfg := &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny}
		if choice.Name != "" {
			cfg.AllowedFunctionNames = []string{choice.Name}
		}
		return &genai.ToolConfig{FunctionCallingConfig: cfg}, nil
	default:
		return nil, errors.New("unsupported tool choice type")
	}
}

func extractChatResponse(resp *genai.GenerateContentResponse) (model.ChatResponse, error) {
	if resp == nil || len(resp.Candidates) == 0 {
		return model.ChatResponse{}, errors.New("google genai returned no candidates")
	}

	first := resp.Candidates[0]
	if first.Content == nil {
		return model.ChatResponse{}, errors.New("google genai candidate has no content")
	}

	// 与 OpenAI 适配层一致：仅使用第一个 candidate 作为最终回复。
	content, reasoning, toolCalls, err := extractContentAndToolCalls(first.Content)
	if err != nil {
		return model.ChatResponse{}, err
	}

	usage := toModelUsage(resp.UsageMetadata)
	state, err := providerStateFromContent(first.Content)
	if err != nil {
		return model.ChatResponse{}, err
	}
	message := finalAssistantMessageFromContent(content, reasoning, toolCalls, state)

	chatResp := model.ChatResponse{
		Message: message,
		Usage:   usage,
	}
	chatResp.SyncFieldsFromMessage()
	return chatResp, nil
}

func extractContentAndToolCalls(content *genai.Content) (string, string, []types.ToolCall, error) {
	if content == nil {
		return "", "", nil, nil
	}

	var textBuilder strings.Builder
	var reasoningBuilder strings.Builder
	toolCalls := make([]types.ToolCall, 0)

	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		if part.Text != "" {
			if part.Thought {
				reasoningBuilder.WriteString(part.Text)
			} else {
				textBuilder.WriteString(part.Text)
			}
		}
		if part.FunctionCall != nil {
			// 参数持久化为 JSON 字符串，保持 model.ToolCall 结构契约不变。
			args, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				return "", "", nil, err
			}
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               part.FunctionCall.ID,
				Name:             part.FunctionCall.Name,
				Arguments:        string(args),
				ThoughtSignature: append([]byte(nil), part.ThoughtSignature...),
			})
		}
	}

	if len(toolCalls) == 0 {
		toolCalls = nil
	}

	extractedReasoning, answer := model.SplitLeadingThinkBlock(textBuilder.String())
	reasoning := strings.TrimSpace(reasoningBuilder.String())
	if reasoning == "" {
		reasoning = extractedReasoning
	}
	return answer, reasoning, toolCalls, nil
}

func toModelUsage(usage *genai.GenerateContentResponseUsageMetadata) model.TokenUsage {
	if usage == nil {
		return model.TokenUsage{}
	}
	// Gemini 会把 tool-use、cached content、thoughts 等 token 分散在不同字段里，
	// 这里统一折算成项目内部的输入/缓存输入/输出统计口径。
	return model.TokenUsage{
		PromptTokens:       int64(usage.PromptTokenCount) + int64(usage.ToolUsePromptTokenCount),
		CachedPromptTokens: int64(usage.CachedContentTokenCount),
		CompletionTokens:   int64(usage.CandidatesTokenCount) + int64(usage.ThoughtsTokenCount),
		TotalTokens:        int64(usage.TotalTokenCount),
	}
}
