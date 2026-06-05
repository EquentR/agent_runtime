package agent

import (
	"strings"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

func TestMaterializeWorkspaceAttachmentRejectsUnsafeAttachmentIDPathSegment(t *testing.T) {
	_, err := materializeWorkspaceAttachment(t.TempDir(), model.Attachment{
		ID:       "../escape",
		FileName: "notes.txt",
		MimeType: "text/plain",
	}, []byte("notes"))
	if err == nil {
		t.Fatal("materializeWorkspaceAttachment() error = nil, want unsafe attachment id rejection")
	}
	if !strings.Contains(err.Error(), "unsafe attachment id") {
		t.Fatalf("materializeWorkspaceAttachment() error = %v, want unsafe attachment id", err)
	}
}

func TestAppendAttachmentManifestWrapsManifestInHiddenXMLBlock(t *testing.T) {
	manifest := buildAttachmentManifestText([]attachmentManifestItem{{
		FileName:     "notes.txt",
		AttachmentID: "att_1",
		Path:         ".attachments/att_1/notes.txt",
		MimeType:     "text/plain",
		SizeBytes:    12,
		Kind:         "text",
		Delivery:     "workspace_only",
		Reason:       "file kept in workspace because this type is not sent directly to the model",
	}})

	got := appendAttachmentManifest("please inspect this file", manifest)

	if !strings.Contains(got, "<agent_runtime_attachments>") || !strings.Contains(got, "</agent_runtime_attachments>") {
		t.Fatalf("appendAttachmentManifest() = %q, want XML wrapped attachment block", got)
	}
	if !strings.Contains(got, "please inspect this file\n\n<agent_runtime_attachments>") {
		t.Fatalf("appendAttachmentManifest() = %q, want user content before XML block", got)
	}
	if !strings.Contains(got, "path: .attachments/att_1/notes.txt") {
		t.Fatalf("appendAttachmentManifest() = %q, want manifest content preserved", got)
	}
}
