package openai

import (
	"reflect"
	"strings"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	goopenai "github.com/sashabaranov/go-openai"
)

func TestBuildOpenAIMessages_TextOnly(t *testing.T) {
	msgs, promptMessages, err := buildOpenAIMessages([]model.Message{{
		Role:    model.RoleUser,
		Content: "hello",
	}})
	if err != nil {
		t.Fatalf("buildOpenAIMessages() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Fatalf("msgs[0].Content = %q, want %q", msgs[0].Content, "hello")
	}
	if msgs[0].MultiContent != nil {
		t.Fatalf("msgs[0].MultiContent should be nil")
	}
	if len(promptMessages) != 1 || promptMessages[0] != "hello" {
		t.Fatalf("promptMessages = %#v, want [hello]", promptMessages)
	}
}

func TestBuildOpenAIMessages_WithAttachments(t *testing.T) {
	msgs, promptMessages, err := buildOpenAIMessages([]model.Message{{
		Role:    model.RoleUser,
		Content: "请分析附件",
		Attachments: []model.Attachment{
			{
				FileName: "img.png",
				MimeType: "image/png",
				Data:     []byte{1, 2, 3},
			},
			{
				FileName: "note.txt",
				MimeType: "text/plain",
				Data:     []byte("line1"),
			},
		},
	}})
	if err != nil {
		t.Fatalf("buildOpenAIMessages() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if len(msgs[0].MultiContent) != 3 {
		t.Fatalf("len(msgs[0].MultiContent) = %d, want 3", len(msgs[0].MultiContent))
	}
	if msgs[0].MultiContent[0].Type != goopenai.ChatMessagePartTypeText {
		t.Fatalf("first part type = %q, want %q", msgs[0].MultiContent[0].Type, goopenai.ChatMessagePartTypeText)
	}
	if msgs[0].MultiContent[1].Type != goopenai.ChatMessagePartTypeImageURL {
		t.Fatalf("second part type = %q, want %q", msgs[0].MultiContent[1].Type, goopenai.ChatMessagePartTypeImageURL)
	}
	if !strings.HasPrefix(msgs[0].MultiContent[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("image url = %q, want data url", msgs[0].MultiContent[1].ImageURL.URL)
	}
	if msgs[0].MultiContent[2].Type != goopenai.ChatMessagePartTypeText {
		t.Fatalf("third part type = %q, want %q", msgs[0].MultiContent[2].Type, goopenai.ChatMessagePartTypeText)
	}
	if !strings.Contains(msgs[0].MultiContent[2].Text, "[附件:note.txt]") {
		t.Fatalf("third part text = %q, want file marker", msgs[0].MultiContent[2].Text)
	}
	if len(promptMessages) != 1 || !strings.Contains(promptMessages[0], "请分析附件") || !strings.Contains(promptMessages[0], "[附件:note.txt]") {
		t.Fatalf("promptMessages = %#v, want combined prompt text", promptMessages)
	}
}

func TestBuildOpenAIMessages_UnsupportedAttachment(t *testing.T) {
	_, _, err := buildOpenAIMessages([]model.Message{{
		Role: model.RoleUser,
		Attachments: []model.Attachment{{
			FileName: "doc.pdf",
			MimeType: "application/pdf",
			Data:     []byte{0xff, 0x00, 0x10},
		}},
	}})
	if err == nil {
		t.Fatal("expected error for unsupported attachment type")
	}
}

func TestBuildOpenAIMessages_WithToolCalls(t *testing.T) {
	msgs, promptMessages, err := buildOpenAIMessages([]model.Message{
		{
			Role:    model.RoleAssistant,
			Content: "",
			ToolCalls: []types.ToolCall{{
				ID:        "call_1",
				Name:      "lookup_weather",
				Arguments: `{"city":"Shanghai"}`,
			}},
		},
		{
			Role:       model.RoleTool,
			Content:    `{"temp":23}`,
			ToolCallId: "call_1",
		},
	})
	if err != nil {
		t.Fatalf("buildOpenAIMessages() error = %v", err)
	}

	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}

	if len(msgs[0].ToolCalls) != 1 {
		t.Fatalf("len(msgs[0].ToolCalls) = %d, want 1", len(msgs[0].ToolCalls))
	}
	if msgs[0].ToolCalls[0].Type != goopenai.ToolTypeFunction {
		t.Fatalf("tool call type = %q, want %q", msgs[0].ToolCalls[0].Type, goopenai.ToolTypeFunction)
	}
	if msgs[0].ToolCalls[0].Function.Name != "lookup_weather" {
		t.Fatalf("tool call function name = %q, want %q", msgs[0].ToolCalls[0].Function.Name, "lookup_weather")
	}
	if msgs[0].ToolCalls[0].Function.Arguments != `{"city":"Shanghai"}` {
		t.Fatalf("tool call arguments = %q, want %q", msgs[0].ToolCalls[0].Function.Arguments, `{"city":"Shanghai"}`)
	}

	if msgs[1].ToolCallID != "call_1" {
		t.Fatalf("msgs[1].ToolCallID = %q, want %q", msgs[1].ToolCallID, "call_1")
	}

	if len(promptMessages) != 2 {
		t.Fatalf("len(promptMessages) = %d, want 2", len(promptMessages))
	}
}

func TestBuildOpenAIMessages_PreservesAssistantReasoningContent(t *testing.T) {
	msgs, _, err := buildOpenAIMessages([]model.Message{withMessageReasoning(model.Message{
		Role: model.RoleAssistant,
		ToolCalls: []types.ToolCall{{
			ID:        "call_1",
			Name:      "lookup_weather",
			Arguments: `{"city":"Shanghai"}`,
		}},
	}, "Need the weather tool.")})
	if err != nil {
		t.Fatalf("buildOpenAIMessages() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].ReasoningContent != "Need the weather tool." {
		t.Fatalf("msgs[0].ReasoningContent = %q, want %q", msgs[0].ReasoningContent, "Need the weather tool.")
	}
}

func withMessageReasoning(message model.Message, reasoning string) model.Message {
	field := reflect.ValueOf(&message).Elem().FieldByName("Reasoning")
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.String {
		return message
	}
	field.SetString(reasoning)
	return message
}
