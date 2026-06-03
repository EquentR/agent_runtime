package openai_chat

import (
	"encoding/json"
	"strings"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

func TestBuildOpenAIChatMessages_WithImageAttachmentReference(t *testing.T) {
	msgs, promptMessages, err := buildOpenAIChatMessages([]model.Message{{
		Role:    model.RoleUser,
		Content: "please edit this",
		Attachments: []model.Attachment{{
			ID:       "att_image_1",
			FileName: "source.png",
			MimeType: "image/png",
			Data:     []byte{1, 2, 3},
		}},
	}})
	if err != nil {
		t.Fatalf("buildOpenAIChatMessages() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}

	raw, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatalf("json.Marshal(msgs[0]) error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}
	content, ok := payload["content"].([]any)
	if !ok || len(content) != 3 {
		t.Fatalf("content = %#v, want 3 items", payload["content"])
	}
	imagePart, ok := content[1].(map[string]any)
	if !ok || imagePart["type"] != "image_url" {
		t.Fatalf("image part = %#v, want image_url", content[1])
	}
	referencePart, ok := content[2].(map[string]any)
	if !ok || referencePart["type"] != "text" {
		t.Fatalf("reference part = %#v, want text", content[2])
	}
	referenceText, _ := referencePart["text"].(string)
	if !strings.Contains(referenceText, "attachment_id: att_image_1") {
		t.Fatalf("reference text = %q, want attachment id", referenceText)
	}
	if !strings.Contains(referenceText, "edit_image.source_attachment_ids") {
		t.Fatalf("reference text = %q, want edit_image hint", referenceText)
	}
	if len(promptMessages) != 1 || !strings.Contains(promptMessages[0], "attachment_id: att_image_1") {
		t.Fatalf("promptMessages = %#v, want attachment id hint", promptMessages)
	}
}
