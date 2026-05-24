package workspaces

import "time"

const (
	StateFileName = ".workspace-state.json"

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
