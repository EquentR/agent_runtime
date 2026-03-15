package tasks

import (
	"encoding/json"
	"time"
)

// Task 是任务快照表对应的 GORM 模型。
//
// 这里保存的是任务的当前聚合状态，便于 REST 直接查询；
// 更细粒度的执行轨迹则落在 task_events 中。
type Task struct {
	ID                string          `json:"id" gorm:"type:varchar(64);primaryKey"`
	TaskType          string          `json:"task_type" gorm:"type:varchar(128);not null;index"`
	Status            Status          `json:"status" gorm:"type:varchar(32);not null;index"`
	InputJSON         json.RawMessage `json:"input" gorm:"column:input_json;type:blob;not null"`
	ConfigJSON        json.RawMessage `json:"config" gorm:"column:config_json;type:blob;not null"`
	MetadataJSON      json.RawMessage `json:"metadata" gorm:"column:metadata_json;type:blob;not null"`
	ResultJSON        json.RawMessage `json:"result,omitempty" gorm:"column:result_json;type:blob"`
	ErrorJSON         json.RawMessage `json:"error,omitempty" gorm:"column:error_json;type:blob"`
	CurrentStepKey    string          `json:"current_step_key" gorm:"type:varchar(128)"`
	CurrentStepTitle  string          `json:"current_step_title" gorm:"type:varchar(255)"`
	StepSeq           int64           `json:"step_seq" gorm:"not null;default:0"`
	ExecutionMode     ExecutionMode   `json:"execution_mode" gorm:"type:varchar(32);not null"`
	RootTaskID        string          `json:"root_task_id" gorm:"type:varchar(64);not null;index"`
	ParentTaskID      string          `json:"parent_task_id" gorm:"type:varchar(64);index"`
	ChildIndex        int             `json:"child_index" gorm:"not null;default:0"`
	RetryOfTaskID     string          `json:"retry_of_task_id" gorm:"type:varchar(64);index"`
	WaitingOnTaskID   string          `json:"waiting_on_task_id" gorm:"type:varchar(64);index"`
	SuspendReason     string          `json:"suspend_reason" gorm:"type:varchar(255)"`
	RunnerID          string          `json:"runner_id" gorm:"type:varchar(128);index"`
	HeartbeatAt       *time.Time      `json:"heartbeat_at,omitempty"`
	LeaseExpiresAt    *time.Time      `json:"lease_expires_at,omitempty" gorm:"index"`
	CancelRequestedAt *time.Time      `json:"cancel_requested_at,omitempty"`
	StartedAt         *time.Time      `json:"started_at,omitempty"`
	FinishedAt        *time.Time      `json:"finished_at,omitempty"`
	CreatedBy         string          `json:"created_by" gorm:"type:varchar(128)"`
	IdempotencyKey    string          `json:"idempotency_key" gorm:"type:varchar(128);index"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// TableName 返回任务快照表名。
func (Task) TableName() string {
	return "tasks"
}

// TaskEvent 是任务事件流表对应的 GORM 模型。
//
// 该表采用 append-only 方式记录每个 task 的事件序列，
// 既可供 SSE 订阅，也可供后续审计与回放使用。
type TaskEvent struct {
	ID          uint64          `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID      string          `json:"task_id" gorm:"type:varchar(64);not null;uniqueIndex:idx_task_event_seq,priority:1;index"`
	Seq         int64           `json:"seq" gorm:"not null;uniqueIndex:idx_task_event_seq,priority:2"`
	EventType   string          `json:"event_type" gorm:"type:varchar(64);not null;index"`
	Level       string          `json:"level" gorm:"type:varchar(16);not null"`
	PayloadJSON json.RawMessage `json:"payload" gorm:"column:payload_json;type:blob;not null"`
	CreatedAt   time.Time       `json:"created_at" gorm:"not null;index"`
}

// TableName 返回任务事件表名。
func (TaskEvent) TableName() string {
	return "task_events"
}
