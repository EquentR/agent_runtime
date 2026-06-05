package model

import (
	"net/http"
	"strings"
)

// IsRasterImageMimeType reports whether mimeType is supported as direct image
// input by provider builders.
func IsRasterImageMimeType(mimeType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(mimeType))
	if beforeParams, _, ok := strings.Cut(normalized, ";"); ok {
		normalized = strings.TrimSpace(beforeParams)
	}
	switch normalized {
	case "image/png", "image/jpeg", "image/jpg", "image/webp", "image/gif", "image/bmp", "image/tiff":
		return true
	default:
		return false
	}
}

// ImageAttachmentReferenceText returns a short text snippet that keeps an image
// attachment's stable ID available to multimodal models for follow-up edits.
func ImageAttachmentReferenceText(attachment Attachment) string {
	mimeType := strings.TrimSpace(attachment.MimeType)
	if mimeType == "" && len(attachment.Data) > 0 {
		mimeType = http.DetectContentType(attachment.Data)
	}
	if !IsRasterImageMimeType(mimeType) {
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
