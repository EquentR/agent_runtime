package workspaces

import (
	"errors"
	"strings"
	"time"
)

const (
	StateFileName    = ".workspace-state.json"
	BaselineFileName = ".workspace-baseline.json"

	ModeMutable  Mode = "mutable"
	ModeReadonly Mode = "readonly"

	StateActive       State = "active"
	StatePendingMerge State = "pending_merge"
	StateMerged       State = "merged"
	StateDiscarded    State = "discarded"
	StateCompleted    State = "completed"
)

type Mode string

type State string

type ActionErrorCode string

var (
	ErrWorkspaceHomeChanged  = errors.New("workspace home changed since baseline")
	ErrWorkspacePendingMerge = errors.New("pending workspace merge")
)

const (
	ActionErrorCodeHomeChanged  ActionErrorCode = "workspace_home_changed"
	ActionErrorCodePendingMerge ActionErrorCode = "workspace_pending_merge"
)

type ActionError struct {
	Code           ActionErrorCode `json:"code"`
	Message        string          `json:"message"`
	ConversationID string          `json:"conversation_id,omitempty"`
	WorkspaceID    string          `json:"workspace_id,omitempty"`
}

func (e *ActionError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ActionError) Unwrap() error {
	if e == nil {
		return nil
	}
	switch e.Code {
	case ActionErrorCodeHomeChanged:
		return ErrWorkspaceHomeChanged
	case ActionErrorCodePendingMerge:
		return ErrWorkspacePendingMerge
	default:
		return nil
	}
}

func (e *ActionError) TaskErrorData() map[string]any {
	if e == nil {
		return nil
	}
	data := map[string]any{
		"code":    string(e.Code),
		"message": e.Message,
	}
	if strings.TrimSpace(e.ConversationID) != "" {
		data["conversation_id"] = strings.TrimSpace(e.ConversationID)
	}
	if strings.TrimSpace(e.WorkspaceID) != "" {
		data["workspace_id"] = strings.TrimSpace(e.WorkspaceID)
	}
	return data
}

func NewHomeChangedError(workspaceID string) *ActionError {
	trimmed := strings.TrimSpace(workspaceID)
	return &ActionError{
		Code:           ActionErrorCodeHomeChanged,
		Message:        "工作区基线已过期，当前 home 工作区已经发生变化。请回到该会话丢弃本次变更，或在最新 home 上重新运行。",
		ConversationID: trimmed,
		WorkspaceID:    trimmed,
	}
}

func NewPendingMergeError(workspaceID string) *ActionError {
	trimmed := strings.TrimSpace(workspaceID)
	return &ActionError{
		Code:           ActionErrorCodePendingMerge,
		Message:        "另一个会话还有未处理的工作区变更。请先前往该会话确认或丢弃后，再继续当前操作。",
		ConversationID: trimmed,
		WorkspaceID:    trimmed,
	}
}

type Config struct {
	TemplateRoot string
	Root         string
	Now          func() time.Time
}

type Workspace struct {
	UserID string
	TaskID string
	Root   string
	State  State
}

type UserWorkspaceSummary struct {
	UserID   string                 `json:"user_id"`
	HomeRoot string                 `json:"home_root"`
	Tasks    []TaskWorkspaceSummary `json:"tasks"`
}

type TaskWorkspaceSummary struct {
	TaskID      string     `json:"task_id"`
	Mode        Mode       `json:"mode"`
	State       State      `json:"state"`
	TaskRoot    string     `json:"task_root"`
	BackupRoot  string     `json:"backup_root,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	MergedAt    *time.Time `json:"merged_at,omitempty"`
	DiscardedAt *time.Time `json:"discarded_at,omitempty"`
}

type WorkspaceManifest struct {
	Version int                      `json:"version"`
	Entries []WorkspaceManifestEntry `json:"entries"`
}

type WorkspaceManifestEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type WorkspaceStateFile struct {
	TaskID       string     `json:"task_id"`
	UserID       string     `json:"user_id"`
	Mode         Mode       `json:"mode"`
	State        State      `json:"state"`
	HomeRoot     string     `json:"home_root"`
	TaskRoot     string     `json:"task_root"`
	BackupRoot   string     `json:"backup_root,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	MergedAt     *time.Time `json:"merged_at,omitempty"`
	DiscardedAt  *time.Time `json:"discarded_at"`
	ErrorMessage string     `json:"error_message,omitempty"`
}
