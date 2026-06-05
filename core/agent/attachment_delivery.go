package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/core/attachments"
	model "github.com/EquentR/agent_runtime/core/providers/types"
)

const runtimeAttachmentsDir = ".attachments"

type plannedAttachments struct {
	display      []model.Attachment
	direct       []model.Attachment
	manifestText string
}

type attachmentManifestItem struct {
	FileName     string `json:"file_name"`
	AttachmentID string `json:"attachment_id"`
	Path         string `json:"path,omitempty"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	Kind         string `json:"kind"`
	Delivery     string `json:"delivery"`
	Reason       string `json:"reason"`
}

func planCurrentAttachments(ctx context.Context, store *attachments.Store, storage attachments.Storage, attachmentIDs []string, owner string, conversationID string, workspaceRoot string, directImageInput bool) (plannedAttachments, error) {
	if len(attachmentIDs) == 0 {
		return plannedAttachments{}, nil
	}
	planned := plannedAttachments{}
	manifestItems := make([]attachmentManifestItem, 0, len(attachmentIDs))
	for _, attachmentID := range attachmentIDs {
		trimmedID := strings.TrimSpace(attachmentID)
		if trimmedID == "" {
			continue
		}
		item, err := planAttachmentForRuntime(ctx, store, storage, model.Attachment{ID: trimmedID}, owner, conversationID, workspaceRoot, true, directImageInput)
		if err != nil {
			return plannedAttachments{}, err
		}
		planned.display = append(planned.display, item.display)
		if item.direct != nil {
			planned.direct = append(planned.direct, *item.direct)
		}
		manifestItems = append(manifestItems, item.manifest)
	}
	planned.manifestText = buildAttachmentManifestText(manifestItems)
	return planned, nil
}

func planReplayMessageAttachments(ctx context.Context, store *attachments.Store, storage attachments.Storage, message model.Message, workspaceRoot string, conversationID string, directImageInput bool) (model.Message, error) {
	if len(message.Attachments) == 0 {
		return message, nil
	}
	direct := make([]model.Attachment, 0, len(message.Attachments))
	manifestItems := make([]attachmentManifestItem, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		item, err := planAttachmentForRuntime(ctx, store, storage, attachment, "", conversationID, workspaceRoot, false, directImageInput)
		if err != nil {
			return model.Message{}, err
		}
		if item.direct != nil {
			direct = append(direct, *item.direct)
			continue
		}
		if attachmentManifestAlreadyPresent(message.Content, item.manifest) {
			continue
		}
		manifestItems = append(manifestItems, item.manifest)
	}
	message.Attachments = direct
	message.Content = appendAttachmentManifest(message.Content, buildAttachmentManifestText(manifestItems))
	return message, nil
}

type plannedAttachmentItem struct {
	display  model.Attachment
	direct   *model.Attachment
	manifest attachmentManifestItem
}

func planAttachmentForRuntime(ctx context.Context, store *attachments.Store, storage attachments.Storage, attachment model.Attachment, owner string, conversationID string, workspaceRoot string, promoteDrafts bool, directImageInput bool) (plannedAttachmentItem, error) {
	if store == nil || storage == nil {
		return plannedAttachmentItem{}, fmt.Errorf("attachment runtime is not configured")
	}
	if strings.TrimSpace(attachment.ID) == "" {
		return plannedAttachmentItem{}, fmt.Errorf("attachment id cannot be empty")
	}
	record, err := store.GetAttachment(ctx, attachment.ID)
	if err != nil {
		return plannedAttachmentItem{}, err
	}
	if trimmedOwner := strings.TrimSpace(owner); trimmedOwner != "" && record.CreatedBy != trimmedOwner {
		return plannedAttachmentItem{}, fmt.Errorf("attachment %q does not belong to %s", record.ID, trimmedOwner)
	}
	if record.ExpiresAt != nil && !record.ExpiresAt.After(time.Now().UTC()) {
		return plannedAttachmentItem{}, fmt.Errorf("%w: %s", attachments.ErrAttachmentExpired, record.ID)
	}
	if promoteDrafts && record.Status == attachments.StatusDraft {
		record, err = store.PromoteDraftToSent(ctx, record.ID, attachments.PromoteInput{ConversationID: conversationID})
		if err != nil {
			return plannedAttachmentItem{}, err
		}
	}
	reader, meta, err := storage.Open(ctx, record.StorageKey)
	if err != nil {
		return plannedAttachmentItem{}, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return plannedAttachmentItem{}, err
	}

	fileName := firstNonEmpty(record.FileName, meta.FileName)
	mimeType := firstNonEmpty(record.MimeType, meta.MimeType)
	classification := attachments.ClassifyFile(fileName, mimeType)
	display := model.Attachment{
		ID:          record.ID,
		FileName:    fileName,
		MimeType:    mimeType,
		SizeBytes:   firstNonZero(record.SizeBytes, meta.SizeBytes, int64(len(data))),
		Kind:        firstNonEmpty(record.Kind, classification.Kind),
		Status:      string(record.Status),
		PreviewText: record.PreviewText,
		ContextText: record.ContextText,
		Width:       record.Width,
		Height:      record.Height,
		ExpiresAt:   record.ExpiresAt,
	}

	manifest := attachmentManifestItem{
		FileName:     display.FileName,
		AttachmentID: display.ID,
		MimeType:     display.MimeType,
		SizeBytes:    display.SizeBytes,
		Kind:         display.Kind,
	}
	if directImageInput && classification.RasterImage {
		direct := display
		direct.Data = data
		manifest.Delivery = "direct_to_llm"
		manifest.Reason = "raster image sent directly because the model is configured for image input"
		return plannedAttachmentItem{display: display, direct: &direct, manifest: manifest}, nil
	}

	workspacePath, err := materializeWorkspaceAttachment(workspaceRoot, display, data)
	if err != nil {
		return plannedAttachmentItem{}, err
	}
	manifest.Path = filepath.ToSlash(workspacePath)
	manifest.Delivery = "workspace_only"
	if classification.RasterImage {
		manifest.Reason = "raster image kept in workspace because the model is not configured for image input"
	} else {
		manifest.Reason = "file kept in workspace because this type is not sent directly to the model"
	}
	return plannedAttachmentItem{display: display, manifest: manifest}, nil
}

func materializeWorkspaceAttachment(workspaceRoot string, attachment model.Attachment, data []byte) (string, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return "", fmt.Errorf("workspace root is required for workspace-only attachment %s", attachment.ID)
	}
	attachmentID, err := safeAttachmentIDPathSegment(attachment.ID)
	if err != nil {
		return "", err
	}
	fileName := safeAttachmentFileName(attachment.FileName)
	relativePath := filepath.Join(runtimeAttachmentsDir, attachmentID, fileName)
	absolutePath := filepath.Clean(filepath.Join(root, relativePath))
	rel, err := filepath.Rel(root, absolutePath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("attachment path escapes workspace: %s", attachment.ID)
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(absolutePath, data, 0o644); err != nil {
		return "", err
	}
	metadataPath := filepath.Join(filepath.Dir(absolutePath), "metadata.json")
	metadata, err := json.MarshalIndent(map[string]any{
		"attachment_id": attachment.ID,
		"file_name":     attachment.FileName,
		"mime_type":     attachment.MimeType,
		"size_bytes":    attachment.SizeBytes,
		"kind":          attachment.Kind,
		"delivery":      "workspace_only",
	}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(metadataPath, metadata, 0o644); err != nil {
		return "", err
	}
	return relativePath, nil
}

func safeAttachmentIDPathSegment(attachmentID string) (string, error) {
	segment := strings.TrimSpace(attachmentID)
	if segment == "" || segment == "." || segment == ".." || filepath.IsAbs(segment) || filepath.VolumeName(segment) != "" || strings.ContainsAny(segment, `/\`) {
		return "", fmt.Errorf("unsafe attachment id path segment: %s", attachmentID)
	}
	return segment, nil
}

func buildAttachmentManifestText(items []attachmentManifestItem) string {
	if len(items) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("Uploaded files are available in the workspace:\n\n")
	for _, item := range items {
		builder.WriteString("- ")
		builder.WriteString(firstNonEmpty(item.FileName, "attachment"))
		builder.WriteString("\n")
		builder.WriteString("  attachment_id: ")
		builder.WriteString(item.AttachmentID)
		builder.WriteString("\n")
		if strings.TrimSpace(item.Path) != "" {
			builder.WriteString("  path: ")
			builder.WriteString(item.Path)
			builder.WriteString("\n")
		}
		builder.WriteString("  mime_type: ")
		builder.WriteString(item.MimeType)
		builder.WriteString("\n")
		builder.WriteString("  size_bytes: ")
		builder.WriteString(fmt.Sprintf("%d", item.SizeBytes))
		builder.WriteString("\n")
		builder.WriteString("  delivery: ")
		builder.WriteString(item.Delivery)
		builder.WriteString("\n")
		builder.WriteString("  note: ")
		builder.WriteString(item.Reason)
		builder.WriteString("\n")
	}
	return builder.String()
}

func appendAttachmentManifest(content string, manifest string) string {
	if strings.TrimSpace(manifest) == "" {
		return content
	}
	if strings.TrimSpace(content) == "" {
		return manifest
	}
	return content + "\n\n" + manifest
}

func attachmentManifestAlreadyPresent(content string, item attachmentManifestItem) bool {
	path := strings.TrimSpace(item.Path)
	if path == "" {
		return false
	}
	return strings.Contains(content, "path: "+path)
}

func safeAttachmentFileName(fileName string) string {
	base := filepath.Base(strings.TrimSpace(fileName))
	base = strings.ReplaceAll(base, string(filepath.Separator), "_")
	base = strings.ReplaceAll(base, "/", "_")
	if base == "" || base == "." {
		return "attachment.bin"
	}
	return base
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
