package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrTaskNotFound = errors.New("task not found")

var terminalTaskStatuses = []Status{StatusCancelled, StatusSucceeded, StatusFailed}

var claimBlockingTaskStatuses = []Status{StatusRunning, StatusWaiting, StatusCancelRequested}

const claimCandidateBatchSize = 32

const sqliteWriteRetryAttempts = 3

const sqliteWriteRetryBaseDelay = 2 * time.Millisecond

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

	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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

// FindLatestActiveTaskByConversation 根据 conversation id 查询最近的非终态任务。
func (s *Store) FindLatestActiveTaskByConversation(ctx context.Context, conversationID string) (*Task, error) {
	trimmedConversationID := strings.TrimSpace(conversationID)
	if trimmedConversationID == "" {
		return nil, nil
	}

	var task Task
	err := s.db.WithContext(ctx).
		Where("status IN ?", []Status{StatusQueued, StatusRunning, StatusWaiting, StatusCancelRequested}).
		Where("json_extract(input_json, '$.conversation_id') = ? OR json_extract(result_json, '$.conversation_id') = ?", trimmedConversationID, trimmedConversationID).
		Order("created_at desc").
		Order("id desc").
		Take(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// ClaimNextTask 领取一个排队任务并推进到 running。
func (s *Store) ClaimNextTask(ctx context.Context, runnerID string, lease time.Duration) (*Task, []TaskEvent, error) {
	for range 3 {
		task, events, err := s.claimNextTaskOnce(ctx, runnerID, lease)
		if err != nil || task != nil {
			return task, events, err
		}
	}
	return nil, nil, nil
}

func (s *Store) claimNextTaskOnce(ctx context.Context, runnerID string, lease time.Duration) (*Task, []TaskEvent, error) {
	var claimed *Task
	var startedEvent TaskEvent

	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for offset := 0; ; offset += claimCandidateBatchSize {
				var candidates []Task
				// 1. 按创建顺序分批扫描 queued 候选任务，直到成功领取任务或耗尽队列。
				if err := tx.Where("status = ?", StatusQueued).
					Order("created_at asc").
					Order("id asc").
					Limit(claimCandidateBatchSize).
					Offset(offset).
					Find(&candidates).Error; err != nil {
					return err
				}
				if len(candidates) == 0 {
					return nil
				}

				for i := range candidates {
					candidate := &candidates[i]
					blocked, err := hasActiveTaskWithSameConcurrencyKey(tx, candidate)
					if err != nil {
						return err
					}
					if blocked {
						continue
					}

					now := time.Now().UTC()
					leaseExpiry := now.Add(lease)
					// 2. 仅在仍为 queued 时抢占该候选任务，避免并发领取覆盖。
					result := tx.Model(&Task{}).
						Where("id = ?", candidate.ID).
						Where("status = ?", StatusQueued).
						Updates(map[string]any{
							"status":           StatusRunning,
							"runner_id":        runnerID,
							"started_at":       now,
							"heartbeat_at":     now,
							"lease_expires_at": leaseExpiry,
							"updated_at":       now,
						})
					if result.Error != nil {
						return result.Error
					}
					if result.RowsAffected == 0 {
						continue
					}

					candidate.Status = StatusRunning
					candidate.RunnerID = runnerID
					candidate.StartedAt = &now
					candidate.HeartbeatAt = &now
					candidate.LeaseExpiresAt = &leaseExpiry

					// 3. 追加 task.started 事件，供观测层与审计层消费。
					startedEvent, err = appendEventTx(tx, candidate.ID, EventTaskStarted, "info", map[string]any{
						"status":    candidate.Status,
						"runner_id": runnerID,
					})
					if err != nil {
						return err
					}
					claimed = candidate
					return nil
				}

				if len(candidates) < claimCandidateBatchSize {
					return nil
				}
			}

		})
	})
	if err != nil {
		return nil, nil, err
	}
	if claimed == nil {
		return nil, nil, nil
	}
	return claimed, []TaskEvent{startedEvent}, nil
}

func hasActiveTaskWithSameConcurrencyKey(tx *gorm.DB, task *Task) (bool, error) {
	if task == nil || strings.TrimSpace(task.ConcurrencyKey) == "" {
		return false, nil
	}

	query := tx.Model(&Task{}).
		Where("id <> ?", task.ID).
		Where("concurrency_key = ?", task.ConcurrencyKey).
		Where("status IN ?", claimBlockingTaskStatuses)
	if trimmedParentTaskID := strings.TrimSpace(task.ParentTaskID); trimmedParentTaskID != "" {
		query = query.Where("NOT (id = ? AND status = ?)", trimmedParentTaskID, StatusWaiting)
	}

	var count int64
	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// MarkWaiting 将 running 任务暂停为 waiting，并释放当前租约。
func (s *Store) MarkWaiting(ctx context.Context, id string, reason string) (*Task, []TaskEvent, error) {
	var task *Task
	var event TaskEvent
	var events []TaskEvent
	trimmedReason := strings.TrimSpace(reason)

	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			loaded, err := loadTaskTx(tx, id)
			if err != nil {
				return err
			}
			if loaded.Status != StatusRunning {
				task = loaded
				return nil
			}

			now := time.Now().UTC()
			result := tx.Model(&Task{}).
				Where("id = ?", loaded.ID).
				Where("status = ?", StatusRunning).
				Updates(map[string]any{
					"status":           StatusWaiting,
					"suspend_reason":   trimmedReason,
					"runner_id":        "",
					"lease_expires_at": nil,
					"updated_at":       now,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				task, err = loadTaskTx(tx, id)
				return err
			}

			loaded.Status = StatusWaiting
			loaded.SuspendReason = trimmedReason
			loaded.RunnerID = ""
			loaded.LeaseExpiresAt = nil

			event, err = appendEventTx(tx, loaded.ID, EventTaskWaiting, "info", map[string]any{
				"status":         loaded.Status,
				"suspend_reason": trimmedReason,
			})
			if err != nil {
				return err
			}

			task = loaded
			events = []TaskEvent{event}
			return nil
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return task, events, nil
}

// ResumeWaitingTask 将 waiting 任务恢复为 queued。
func (s *Store) ResumeWaitingTask(ctx context.Context, id string, reason string) (*Task, []TaskEvent, error) {
	var task *Task
	var event TaskEvent
	var events []TaskEvent
	trimmedReason := strings.TrimSpace(reason)

	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			loaded, err := loadTaskTx(tx, id)
			if err != nil {
				return err
			}
			if loaded.Status != StatusWaiting {
				task = loaded
				return nil
			}

			now := time.Now().UTC()
			result := tx.Model(&Task{}).
				Where("id = ?", loaded.ID).
				Where("status = ?", StatusWaiting).
				Updates(map[string]any{
					"status":         StatusQueued,
					"suspend_reason": "",
					"updated_at":     now,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				task, err = loadTaskTx(tx, id)
				return err
			}

			loaded.Status = StatusQueued
			loaded.SuspendReason = ""

			event, err = appendEventTx(tx, loaded.ID, EventTaskResumed, "info", map[string]any{
				"status":        loaded.Status,
				"resume_reason": trimmedReason,
			})
			if err != nil {
				return err
			}

			task = loaded
			events = []TaskEvent{event}
			return nil
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return task, events, nil
}

// UpdateTaskMetadata 用新的元数据快照替换任务的 MetadataJSON。
func (s *Store) UpdateTaskMetadata(ctx context.Context, id string, metadata any) (*Task, error) {
	metadataJSON, err := marshalJSON(metadata, true)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	var task *Task
	err = s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			loaded, err := loadTaskTx(tx, id)
			if err != nil {
				return err
			}

			now := time.Now().UTC()
			result := tx.Model(&Task{}).
				Where("id = ?", loaded.ID).
				Updates(map[string]any{
					"metadata_json": metadataJSON,
					"updated_at":    now,
				})
			if result.Error != nil {
				return result.Error
			}

			loaded.MetadataJSON = cloneRawMessage(metadataJSON)
			loaded.UpdatedAt = now
			task = loaded
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

// ListChildTasks 按 child_index 和创建顺序返回父任务下的子任务。
func (s *Store) ListChildTasks(ctx context.Context, parentTaskID string) ([]Task, error) {
	trimmedParentTaskID := strings.TrimSpace(parentTaskID)
	if trimmedParentTaskID == "" {
		return nil, nil
	}

	var tasks []Task
	err := s.db.WithContext(ctx).
		Where("parent_task_id = ?", trimmedParentTaskID).
		Order("child_index asc").
		Order("created_at asc").
		Order("id asc").
		Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

// CountActiveChildTasks 统计父任务下尚未进入终态的子任务数量。
func (s *Store) CountActiveChildTasks(ctx context.Context, parentTaskID string) (int64, error) {
	trimmedParentTaskID := strings.TrimSpace(parentTaskID)
	if trimmedParentTaskID == "" {
		return 0, nil
	}

	var count int64
	err := s.db.WithContext(ctx).
		Model(&Task{}).
		Where("parent_task_id = ?", trimmedParentTaskID).
		Where("status NOT IN ?", terminalTaskStatuses).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

// TryResumeParentTask 在父任务等待且所有子任务终态时将其重新排队。
func (s *Store) TryResumeParentTask(ctx context.Context, parentTaskID string) (*Task, []TaskEvent, error) {
	trimmedParentTaskID := strings.TrimSpace(parentTaskID)
	if trimmedParentTaskID == "" {
		return nil, nil, nil
	}

	var task *Task
	var event TaskEvent
	var events []TaskEvent

	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			loaded, err := loadTaskTx(tx, trimmedParentTaskID)
			if err != nil {
				return err
			}
			task = loaded
			if loaded.Status != StatusWaiting {
				return nil
			}

			activeCount, err := countActiveChildTasksTx(tx, loaded.ID)
			if err != nil {
				return err
			}
			if activeCount > 0 {
				return nil
			}

			now := time.Now().UTC()
			result := tx.Model(&Task{}).
				Where("id = ?", loaded.ID).
				Where("status = ?", StatusWaiting).
				Updates(map[string]any{
					"status":         StatusQueued,
					"suspend_reason": "",
					"updated_at":     now,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				task, err = loadTaskTx(tx, loaded.ID)
				return err
			}

			loaded.Status = StatusQueued
			loaded.SuspendReason = ""

			event, err = appendEventTx(tx, loaded.ID, EventTaskResumed, "info", map[string]any{
				"status":        loaded.Status,
				"resume_reason": "children_complete",
			})
			if err != nil {
				return err
			}

			task = loaded
			events = []TaskEvent{event}
			return nil
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return task, events, nil
}

// RequestCancel 为任务写入取消请求，并追加 cancel_requested 事件。
func (s *Store) RequestCancel(ctx context.Context, id string) (*Task, []TaskEvent, error) {
	var task *Task
	var event TaskEvent
	var events []TaskEvent

	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// 1. 读取并锁定当前任务快照。
			loaded, err := loadTaskTx(tx, id)
			if err != nil {
				return err
			}
			if loaded.Status.IsTerminal() || loaded.Status == StatusCancelRequested {
				task = loaded
				return nil
			}

			now := time.Now().UTC()
			// 2. 更新状态与取消时间。
			result := tx.Model(&Task{}).
				Where("id = ?", loaded.ID).
				Where("status NOT IN ?", terminalTaskStatuses).
				Where("status <> ?", StatusCancelRequested).
				Updates(map[string]any{
					"status":              StatusCancelRequested,
					"cancel_requested_at": now,
					"updated_at":          now,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				task, err = loadTaskTx(tx, id)
				return err
			}
			loaded.Status = StatusCancelRequested
			loaded.CancelRequestedAt = &now

			// 3. 追加 task.cancel_requested 事件。
			event, err = appendEventTx(tx, loaded.ID, EventTaskCancelRequested, "info", map[string]any{
				"status": loaded.Status,
			})
			if err != nil {
				return err
			}
			task = loaded
			events = []TaskEvent{event}
			return nil
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return task, events, nil
}

// MarkSucceeded 将任务推进到 succeeded 终态。
func (s *Store) MarkSucceeded(ctx context.Context, id string, result any) (*Task, []TaskEvent, error) {
	return s.finishTask(ctx, id, StatusSucceeded, result, nil)
}

// RetryTask 基于既有任务生成一个新的 queued 任务。
func (s *Store) RetryTask(ctx context.Context, id string) (*Task, []TaskEvent, error) {
	var task Task
	var createdEvent TaskEvent

	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// 1. 读取原任务，复制其输入、配置与元数据。
			original, err := loadTaskTx(tx, id)
			if err != nil {
				return err
			}

			task = Task{
				ID:             newTaskID(),
				TaskType:       original.TaskType,
				Status:         StatusQueued,
				ConcurrencyKey: original.ConcurrencyKey,
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
	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			loaded, err := loadTaskTx(tx, id)
			if err != nil {
				return err
			}
			if loaded.Status != StatusRunning && loaded.Status != StatusCancelRequested {
				task = loaded
				return nil
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
	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	})
	if err != nil {
		return nil, nil, err
	}
	return task, []TaskEvent{event}, nil
}

// AppendEvent 为指定任务追加一条自定义事件。
func (s *Store) AppendEvent(ctx context.Context, taskID string, eventType string, level string, payload any) (TaskEvent, error) {
	var event TaskEvent
	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			_, err := loadTaskTx(tx, taskID)
			if err != nil {
				return err
			}
			event, err = appendEventTx(tx, taskID, eventType, level, payload)
			return err
		})
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
	var events []TaskEvent
	err := s.withWriteRetry(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// 1. 读取当前任务快照。
			loaded, err := loadTaskTx(tx, id)
			if err != nil {
				return err
			}
			if loaded.Status.IsTerminal() {
				task = loaded
				return nil
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
			resultTx := tx.Model(&Task{}).
				Where("id = ?", loaded.ID).
				Where("status NOT IN ?", terminalTaskStatuses).
				Updates(map[string]any{
					"status":           status,
					"result_json":      resultJSON,
					"error_json":       errorJSON,
					"finished_at":      now,
					"lease_expires_at": nil,
					"heartbeat_at":     now,
					"updated_at":       now,
				})
			if resultTx.Error != nil {
				return resultTx.Error
			}
			if resultTx.RowsAffected == 0 {
				task, err = loadTaskTx(tx, id)
				return err
			}
			loaded.Status = status
			loaded.ResultJSON = resultJSON
			loaded.ErrorJSON = errorJSON
			loaded.FinishedAt = &now
			loaded.LeaseExpiresAt = nil
			loaded.HeartbeatAt = &now
			// 3. 追加 task.finished 事件，形成快照与事件流的一致提交。
			event, err = appendEventTx(tx, loaded.ID, EventTaskFinished, "info", map[string]any{
				"status": status,
				"error":  reason,
			})
			if err != nil {
				return err
			}
			task = loaded
			events = []TaskEvent{event}
			return nil
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return task, events, nil
}

func (s *Store) withWriteRetry(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < sqliteWriteRetryAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := fn()
		if err == nil {
			return nil
		}
		if !isTransientSQLiteWriteError(err) {
			return err
		}
		lastErr = err
		if attempt == sqliteWriteRetryAttempts-1 {
			continue
		}
		delay := time.Duration(attempt+1) * sqliteWriteRetryBaseDelay
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}

func isTransientSQLiteWriteError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "locked") ||
		strings.Contains(message, "deadlocked") ||
		strings.Contains(message, "interrupted") ||
		strings.Contains(message, "busy")
}

func countActiveChildTasksTx(tx *gorm.DB, parentTaskID string) (int64, error) {
	trimmedParentTaskID := strings.TrimSpace(parentTaskID)
	if trimmedParentTaskID == "" {
		return 0, nil
	}

	var count int64
	err := tx.Model(&Task{}).
		Where("parent_task_id = ?", trimmedParentTaskID).
		Where("status NOT IN ?", terminalTaskStatuses).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
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
		ConcurrencyKey:  input.ConcurrencyKey,
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
