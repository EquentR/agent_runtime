package model

import (
	"strings"
	"testing"
)

func TestImageAttachmentReferenceTextIncludesAttachmentID(t *testing.T) {
	text := ImageAttachmentReferenceText(Attachment{
		ID:       "att_image_1",
		FileName: "source.png",
		MimeType: "image/png",
	})

	if text == "" {
		t.Fatal("ImageAttachmentReferenceText() = empty, want reference text")
	}
	if !strings.Contains(text, "attachment_id: att_image_1") {
		t.Fatalf("reference text = %q, want attachment id", text)
	}
	if !strings.Contains(text, "edit_image.source_attachment_ids") {
		t.Fatalf("reference text = %q, want edit_image hint", text)
	}
}
