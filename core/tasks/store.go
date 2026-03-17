package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrTaskNotFound = errors.New("task not found")

// Store 封装任务快照与事件流的数据库访问逻辑。
type Store struct {
	db *gorm.DB
}

// NewStore 使用给定数据库句柄创建任务存储层。
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// AutoMigrate 初始化任务相关表结构。
func (s *Store) AutoMigrate() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store db cannot be nil")
	}
	return s.db.AutoMigrate(&Task{}, &TaskEvent{})
}

// CreateTask 创建一个新的排队任务，并写入初始 created 事件。
func (s *Store) CreateTask(ctx context.Context, input CreateTaskInput) (*Task, []TaskEvent, error) {
	var task Task
	var createdEvent TaskEvent

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		// 1. 先根据外部输入构造任务快照。
		task, err = newTask(input)
		if err != nil {
			return err
		}
		// 2. 保存任务快照。
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		// 3. 追加 task.created 事件，保证快照与事件流同步初始化。
		createdEvent, err = appendEventTx(tx, task.ID, EventTaskCreated, "info", map[string]any{
			"status":    task.Status,
			"task_type": task.TaskType,
		})
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	return &task, []TaskEvent{createdEvent}, nil
}

// GetTask 根据任务 id 读取当前快照。
func (s *Store) GetTask(ctx context.Context, id string) (*Task, error) {
	var task Task
	err := s.db.WithContext(ctx).First(&task, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// ClaimNextTask 领取一个排队任务并推进到 running。
func (s *Store) ClaimNextTask(ctx context.Context, runnerID string, lease time.Duration) (*Task, []TaskEvent, error) {
	var claimed *Task
	var startedEvent TaskEvent

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task Task
		// 1. 按创建顺序领取最早的 queued 任务。
		err := tx.Where("status = ?", StatusQueued).Order("created_at asc").Take(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		leaseExpiry := now.Add(lease)
		// 2. 写入 runner、租约与启动时间，切换到 running。
		task.Status = StatusRunning
		task.RunnerID = runnerID
		task.StartedAt = &now
		task.HeartbeatAt = &now
		task.LeaseExpiresAt = &leaseExpiry
		if err := tx.Save(&task).Error; err != nil {
			return err
		}

		// 3. 追加 task.started 事件，供观测层与审计层消费。
		startedEvent, err = appendEventTx(tx, task.ID, EventTaskStarted, "info", map[string]any{
			"status":    task.Status,
			"runner_id": runnerID,
		})
		if err != nil {
			return err
		}
		claimed = &task
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if claimed == nil {
		return nil, nil, nil
	}
	return claimed, []TaskEvent{startedEvent}, nil
}

// RequestCancel 为任务写入取消请求，并追加 cancel_requested 事件。
func (s *Store) RequestCancel(ctx context.Context, id string) (*Task, []TaskEvent, error) {
	var task *Task
	var event TaskEvent

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 读取并锁定当前任务快照。
		loaded, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		// 2. 更新状态与取消时间。
		loaded.Status = StatusCancelRequested
		loaded.CancelRequestedAt = &now
		if err := tx.Save(loaded).Error; err != nil {
			return err
		}

		// 3. 追加 task.cancel_requested 事件。
		event, err = appendEventTx(tx, loaded.ID, EventTaskCancelRequested, "info", map[string]any{
			"status": loaded.Status,
		})
		if err != nil {
			return err
		}
		task = loaded
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return task, []TaskEvent{event}, nil
}

// MarkSucceeded 将任务推进到 succeeded 终态。
func (s *Store) MarkSucceeded(ctx context.Context, id string, result any) (*Task, []TaskEvent, error) {
	return s.finishTask(ctx, id, StatusSucceeded, result, nil)
}

// RetryTask 基于既有任务生成一个新的 queued 任务。
func (s *Store) RetryTask(ctx context.Context, id string) (*Task, []TaskEvent, error) {
	var task Task
	var createdEvent TaskEvent

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 读取原任务，复制其输入、配置与元数据。
		original, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}

		task = Task{
			ID:             newTaskID(),
			TaskType:       original.TaskType,
			Status:         StatusQueued,
			InputJSON:      cloneRawMessage(original.InputJSON),
			ConfigJSON:     cloneRawMessage(original.ConfigJSON),
			MetadataJSON:   cloneRawMessage(original.MetadataJSON),
			ExecutionMode:  original.ExecutionMode,
			CreatedBy:      original.CreatedBy,
			RetryOfTaskID:  original.ID,
			IdempotencyKey: "",
		}
		task.RootTaskID = task.ID
		// 2. 生成全新任务记录，而不是覆盖旧任务状态。
		if err := tx.Create(&task).Error; err != nil {
			return err
		}

		// 3. 以新的事件序列重新写入 created 事件。
		createdEvent, err = appendEventTx(tx, task.ID, EventTaskCreated, "info", map[string]any{
			"status":      task.Status,
			"task_type":   task.TaskType,
			"retry_of_id": original.ID,
		})
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return &task, []TaskEvent{createdEvent}, nil
}

// ListEvents 返回指定任务在某个序号之后的事件列表。
func (s *Store) ListEvents(ctx context.Context, taskID string, afterSeq int64, limit int) ([]TaskEvent, error) {
	query := s.db.WithContext(ctx).Where("task_id = ? AND seq > ?", taskID, afterSeq).Order("seq asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var events []TaskEvent
	if err := query.Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// MarkCancelled 将任务推进到 cancelled 终态。
func (s *Store) MarkCancelled(ctx context.Context, id string, reason any) (*Task, []TaskEvent, error) {
	return s.finishTask(ctx, id, StatusCancelled, nil, reason)
}

// MarkFailed 将任务推进到 failed 终态。
func (s *Store) MarkFailed(ctx context.Context, id string, reason any) (*Task, []TaskEvent, error) {
	return s.finishTask(ctx, id, StatusFailed, nil, reason)
}

// UpdateHeartbeat 刷新任务的心跳时间与租约到期时间。
func (s *Store) UpdateHeartbeat(ctx context.Context, id string, runnerID string, lease time.Duration) (*Task, error) {
	var task *Task
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaded, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		leaseExpiry := now.Add(lease)
		loaded.RunnerID = runnerID
		loaded.HeartbeatAt = &now
		loaded.LeaseExpiresAt = &leaseExpiry
		if err := tx.Save(loaded).Error; err != nil {
			return err
		}
		task = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

// StartStep 更新任务当前步骤，并追加 step.started 事件。
func (s *Store) StartStep(ctx context.Context, id string, key string, title string) (*Task, []TaskEvent, error) {
	var task *Task
	var event TaskEvent
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaded, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		loaded.StepSeq++
		loaded.CurrentStepKey = key
		loaded.CurrentStepTitle = title
		if err := tx.Save(loaded).Error; err != nil {
			return err
		}
		event, err = appendEventTx(tx, loaded.ID, EventStepStarted, "info", map[string]any{
			"step_seq": loaded.StepSeq,
			"step_key": key,
			"title":    title,
		})
		if err != nil {
			return err
		}
		task = loaded
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return task, []TaskEvent{event}, nil
}

// FinishStep 为当前步骤写入 step.finished 事件。
func (s *Store) FinishStep(ctx context.Context, id string, payload any) (*Task, []TaskEvent, error) {
	var task *Task
	var event TaskEvent
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaded, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		event, err = appendEventTx(tx, loaded.ID, EventStepFinished, "info", map[string]any{
			"step_seq": loaded.StepSeq,
			"step_key": loaded.CurrentStepKey,
			"title":    loaded.CurrentStepTitle,
			"payload":  payload,
		})
		if err != nil {
			return err
		}
		task = loaded
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return task, []TaskEvent{event}, nil
}

// AppendEvent 为指定任务追加一条自定义事件。
func (s *Store) AppendEvent(ctx context.Context, taskID string, eventType string, level string, payload any) (TaskEvent, error) {
	var event TaskEvent
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := loadTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		event, err = appendEventTx(tx, taskID, eventType, level, payload)
		return err
	})
	if err != nil {
		return TaskEvent{}, err
	}
	return event, nil
}

// finishTask 统一处理任务进入终态时的快照更新与事件写入。
func (s *Store) finishTask(ctx context.Context, id string, status Status, result any, reason any) (*Task, []TaskEvent, error) {
	var task *Task
	var event TaskEvent
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 读取当前任务快照。
		loaded, err := loadTaskTx(tx, id)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		resultJSON, err := marshalJSON(result, false)
		if err != nil {
			return err
		}
		errorJSON, err := marshalJSON(reason, false)
		if err != nil {
			return err
		}
		// 2. 写入终态结果、错误信息与收尾时间。
		loaded.Status = status
		loaded.ResultJSON = resultJSON
		loaded.ErrorJSON = errorJSON
		loaded.FinishedAt = &now
		loaded.LeaseExpiresAt = nil
		loaded.HeartbeatAt = &now
		if err := tx.Save(loaded).Error; err != nil {
			return err
		}
		// 3. 追加 task.finished 事件，形成快照与事件流的一致提交。
		event, err = appendEventTx(tx, loaded.ID, EventTaskFinished, "info", map[string]any{
			"status": status,
			"error":  reason,
		})
		if err != nil {
			return err
		}
		task = loaded
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return task, []TaskEvent{event}, nil
}

// newTask 根据创建输入构造标准化的任务快照。
func newTask(input CreateTaskInput) (Task, error) {
	if input.TaskType == "" {
		return Task{}, fmt.Errorf("task type cannot be empty")
	}

	// 1. 先将输入、配置与元数据统一序列化为 JSON，便于落库持久化。
	inputJSON, err := marshalJSON(input.Input, true)
	if err != nil {
		return Task{}, fmt.Errorf("marshal input: %w", err)
	}
	configJSON, err := marshalJSON(input.Config, true)
	if err != nil {
		return Task{}, fmt.Errorf("marshal config: %w", err)
	}
	metadataJSON, err := marshalJSON(input.Metadata, true)
	if err != nil {
		return Task{}, fmt.Errorf("marshal metadata: %w", err)
	}

	id := newTaskID()
	mode := input.ExecutionMode
	if mode == "" {
		mode = ExecutionModeSerial
	}
	rootTaskID := input.RootTaskID
	if rootTaskID == "" {
		rootTaskID = id
	}

	// 2. 组装任务快照，补齐默认执行模式与树形关联字段。
	return Task{
		ID:              id,
		TaskType:        input.TaskType,
		Status:          StatusQueued,
		InputJSON:       inputJSON,
		ConfigJSON:      configJSON,
		MetadataJSON:    metadataJSON,
		ExecutionMode:   mode,
		RootTaskID:      rootTaskID,
		ParentTaskID:    input.ParentTaskID,
		ChildIndex:      input.ChildIndex,
		RetryOfTaskID:   input.RetryOfTaskID,
		WaitingOnTaskID: input.WaitingOnTaskID,
		SuspendReason:   input.SuspendReason,
		CreatedBy:       input.CreatedBy,
		IdempotencyKey:  input.IdempotencyKey,
	}, nil
}

// loadTaskTx 在事务内按 id 读取任务快照。
func loadTaskTx(tx *gorm.DB, id string) (*Task, error) {
	var task Task
	err := tx.First(&task, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// appendEventTx 在事务内为任务追加下一条事件。
func appendEventTx(tx *gorm.DB, taskID string, eventType string, level string, payload any) (TaskEvent, error) {
	var maxSeq int64
	// 事件序号按任务维度递增，便于 SSE 续传与顺序消费。
	if err := tx.Model(&TaskEvent{}).Where("task_id = ?", taskID).Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error; err != nil {
		return TaskEvent{}, err
	}

	payloadJSON, err := marshalJSON(payload, true)
	if err != nil {
		return TaskEvent{}, err
	}

	event := TaskEvent{
		TaskID:      taskID,
		Seq:         maxSeq + 1,
		EventType:   eventType,
		Level:       normalizeLevel(level),
		PayloadJSON: payloadJSON,
		CreatedAt:   time.Now().UTC(),
	}
	if err := tx.Create(&event).Error; err != nil {
		return TaskEvent{}, err
	}
	return event, nil
}

// marshalJSON 将任意值规范化为 JSON 原始字节。
func marshalJSON(value any, objectDefault bool) (json.RawMessage, error) {
	if value == nil {
		if objectDefault {
			return json.RawMessage("{}"), nil
		}
		return json.RawMessage("null"), nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

// cloneRawMessage 复制 JSON 原始字节，避免共享底层切片。
func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("{}")
	}
	return append(json.RawMessage(nil), value...)
}

// normalizeLevel 为事件级别补齐默认值。
func normalizeLevel(level string) string {
	if level == "" {
		return "info"
	}
	return level
}

// newTaskID 生成任务主键。
func newTaskID() string {
	return "tsk_" + uuid.NewString()
}
