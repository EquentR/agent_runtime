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
