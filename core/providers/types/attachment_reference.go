package model

import (
	"net/http"
	"strings"
)

// ImageAttachmentReferenceText returns a short text snippet that keeps an image
// attachment's stable ID available to multimodal models for follow-up edits.
func ImageAttachmentReferenceText(attachment Attachment) string {
	mimeType := strings.TrimSpace(attachment.MimeType)
	if mimeType == "" && len(attachment.Data) > 0 {
		mimeType = http.DetectContentType(attachment.Data)
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return ""
	}

	fileName := strings.TrimSpace(attachment.FileName)
	if fileName == "" {
		fileName = "image"
	}
	attachmentID := strings.TrimSpace(attachment.ID)
	if attachmentID == "" {
		return "[image attachment: " + fileName + "]"
	}

	return "[image attachment: " + fileName + "; attachment_id: " + attachmentID + "] Use this attachment_id when calling edit_image.source_attachment_ids."
}
