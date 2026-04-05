package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/EquentR/agent_runtime/core/approvals"
	"github.com/EquentR/agent_runtime/core/interactions"
	"gorm.io/gorm"
)

// Executor 定义单个 task_type 的执行器签名。
type Executor func(ctx context.Context, task *Task, runtime *Runtime) (any, error)

// ManagerOptions 定义任务管理器的后台轮询与租约参数。
type ManagerOptions struct {
	RunnerID          string
	WorkerCount       int
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	AuditRecorder     AuditRecorder
	ApprovalStore     *approvals.Store
	InteractionStore  *interactions.Store
}

type AuditRun struct {
	ID     string
	TaskID string
}

type AuditEvent struct {
	RunID     string
	EventType string
}

type AuditStartRunInput struct {
	TaskID    string
	TaskType  string
	RunnerID  string
	CreatedBy string
	Status    Status
	StartedAt time.Time
}

type AuditAppendEventInput struct {
	EventType string
	Payload   any
}

type AuditFinishRunInput struct {
	Status     Status
	FinishedAt time.Time
}

type AuditRecorder interface {
	StartRun(ctx context.Context, input AuditStartRunInput) (*AuditRun, error)
	AppendEvent(ctx context.Context, runID string, input AuditAppendEventInput) (*AuditEvent, error)
	FinishRun(ctx context.Context, runID string, input AuditFinishRunInput) error
}

var ErrTaskSuspended = errors.New("task suspended")

// Manager 负责任务的创建、领取、串行执行、取消与事件发布。
type Manager struct {
	store        *Store
	hub          *EventHub
	audit        AuditRecorder
	approvals    *approvals.Store
	interactions *interactions.Store

	runnerID          string
	workerCount       int
	pollInterval      time.Duration
	leaseDuration     time.Duration
	heartbeatInterval time.Duration

	mu           sync.RWMutex
	executors    map[string]Executor
	activeCancel map[string]context.CancelFunc
	startOnce    sync.Once
}

// NewManager 创建一个串行任务管理器实例。
func NewManager(store *Store, options ManagerOptions) *Manager {
	runnerID := options.RunnerID
	if runnerID == "" {
		runnerID = "local-runner"
	}
	workerCount := options.WorkerCount
	if workerCount <= 0 {
		workerCount = 1
	}
	pollInterval := options.PollInterval
	if pollInterval <= 0 {
		pollInterval = 50 * time.Millisecond
	}
	leaseDuration := options.LeaseDuration
	if leaseDuration <= 0 {
		leaseDuration = 5 * time.Second
	}
	heartbeatInterval := options.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = leaseDuration / 2
		if heartbeatInterval <= 0 {
			heartbeatInterval = 2 * time.Second
		}
	}

	return &Manager{
		store:             store,
		hub:               NewEventHub(),
		audit:             options.AuditRecorder,
		approvals:         options.ApprovalStore,
		interactions:      options.InteractionStore,
		runnerID:          runnerID,
		workerCount:       workerCount,
		pollInterval:      pollInterval,
		leaseDuration:     leaseDuration,
		heartbeatInterval: heartbeatInterval,
		executors:         make(map[string]Executor),
		activeCancel:      make(map[string]context.CancelFunc),
	}
}

// RegisterExecutor 为指定 task_type 注册执行器。
func (m *Manager) RegisterExecutor(taskType string, executor Executor) error {
	if taskType == "" {
		return fmt.Errorf("task type cannot be empty")
	}
	if executor == nil {
		return fmt.Errorf("executor cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.executors[taskType]; exists {
		return fmt.Errorf("executor already registered for %s", taskType)
	}
	m.executors[taskType] = executor
	return nil
}

// Start 启动后台 worker 池；重复调用只会生效一次。
func (m *Manager) Start(ctx context.Context) {
	m.startOnce.Do(func() {
		for workerIndex := 0; workerIndex < m.workerCount; workerIndex++ {
			go m.runWorker(ctx, workerIndex)
		}
	})
}

// CreateTask 创建并发布一个新的任务。
func (m *Manager) CreateTask(ctx context.Context, input CreateTaskInput) (*Task, error) {
	task, events, err := m.store.CreateTask(ctx, input)
	if err != nil {
		return nil, err
	}
	m.recordTaskCreated(task)
	m.publish(events...)
	return task, nil
}

// GetTask 查询任务当前快照。
func (m *Manager) GetTask(ctx context.Context, id string) (*Task, error) {
	return m.store.GetTask(ctx, id)
}

// FindLatestActiveTaskByConversation 查询最近的非终态 conversation 任务。
func (m *Manager) FindLatestActiveTaskByConversation(ctx context.Context, conversationID string) (*Task, error) {
	return m.store.FindLatestActiveTaskByConversation(ctx, conversationID)
}

// ListEvents 查询任务事件流。
func (m *Manager) ListEvents(ctx context.Context, taskID string, afterSeq int64, limit int) ([]TaskEvent, error) {
	return m.store.ListEvents(ctx, taskID, afterSeq, limit)
}

// ListTaskApprovals 返回任务下的审批记录。
func (m *Manager) ListTaskApprovals(ctx context.Context, taskID string) ([]approvals.ToolApproval, error) {
	if m == nil || m.approvals == nil {
		return nil, fmt.Errorf("approval store is not configured")
	}
	return m.approvals.ListTaskApprovals(ctx, taskID)
}

// CreateApproval 创建审批记录并追加 approval.requested 事件。
func (m *Manager) CreateApproval(ctx context.Context, input approvals.CreateApprovalInput) (*approvals.ToolApproval, error) {
	if m == nil || m.approvals == nil || m.store == nil {
		return nil, fmt.Errorf("approval store is not configured")
	}
	approval, err := m.approvals.FindApprovalByToolCall(ctx, input.TaskID, input.ToolCallID)
	if err != nil {
		return nil, err
	}
	if approval == nil {
		approval, err = m.approvals.CreateApproval(ctx, input)
		if err != nil {
			return nil, err
		}
	}
	approval, events, err := m.finalizeCreatedApproval(ctx, approval)
	if err != nil {
		return nil, err
	}
	if _, err := m.ensureApprovalInteraction(ctx, approval); err != nil {
		return nil, err
	}
	if len(events) > 0 {
		m.publish(events...)
	}
	return approval, nil
}

// CreateInteraction 创建可恢复的人工交互记录并发布 interaction.requested 事件。
func (m *Manager) CreateInteraction(ctx context.Context, input interactions.CreateInteractionInput) (*interactions.Interaction, error) {
	if m == nil || m.interactions == nil || m.store == nil {
		return nil, fmt.Errorf("interaction store is not configured")
	}
	interaction, events, err := m.createInteraction(ctx, input)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		m.publish(events...)
	}
	return interaction, nil
}

// FinalizeQuestionInteraction 在 question interaction 响应后发布 interaction.responded 并恢复任务。
func (m *Manager) FinalizeQuestionInteraction(ctx context.Context, taskID string, interactionID string) (*Task, error) {
	task, events, err := m.finalizeQuestionInteraction(ctx, taskID, interactionID)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		m.publish(events...)
	}
	return task, nil
}

// RespondQuestionInteraction 以原子路径完成 question interaction 响应落库、事件发布与任务恢复。
func (m *Manager) RespondQuestionInteraction(ctx context.Context, taskID string, interactionID string, input interactions.ResponseInput) (*Task, *interactions.Interaction, []TaskEvent, error) {
	if m == nil || m.interactions == nil || m.store == nil {
		return nil, nil, nil, fmt.Errorf("interaction store is not configured")
	}
	if input.Status != interactions.StatusResponded {
		return nil, nil, nil, fmt.Errorf("question interaction status must be responded")
	}
	if responsePayloadIsEmpty(input.Response) {
		return nil, nil, nil, fmt.Errorf("question interaction response cannot be empty")
	}
	var (
		resumed     *Task
		interaction *interactions.Interaction
		events      []TaskEvent
	)
	err := m.store.withWriteRetry(ctx, func() error {
		return m.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			existing := lookupInteractionTx(tx, taskID, interactionID)
			if existing == nil {
				return fmt.Errorf("%w: %s", interactions.ErrInteractionNotFound, interactionID)
			}
			if existing.Kind != interactions.KindQuestion {
				return fmt.Errorf("interaction kind must be question")
			}
			store := interactions.NewStore(tx)
			responded, changed, err := store.RespondInteraction(ctx, taskID, interactionID, input)
			if err != nil {
				return err
			}
			if responded == nil {
				return nil
			}
			interaction = responded
			if !changed {
				return nil
			}
			if responded.Kind != interactions.KindQuestion || responded.Status != interactions.StatusResponded {
				return nil
			}
			task, err := loadTaskTx(tx, taskID)
			if err != nil {
				return err
			}
			payload, err := buildInteractionEventPayload(responded)
			if err != nil {
				return err
			}
			respondedEvent, err := appendEventTx(tx, task.ID, EventInteractionResponded, "info", payload)
			if err != nil {
				return err
			}
			events = append(events, respondedEvent)
			if task.Status != StatusWaiting || task.SuspendReason != "waiting_for_interaction" {
				return nil
			}
			checkpointID, ok, err := taskApprovalCheckpointID(task.MetadataJSON)
			if err != nil {
				return err
			}
			if ok && checkpointID != responded.ID {
				return nil
			}
			resumedEvent, err := resumeQuestionTaskTx(tx, task)
			if err != nil {
				return err
			}
			if resumedEvent != nil {
				events = append(events, *resumedEvent)
				copy := *task
				resumed = &copy
			}
			return nil
		})
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(events) > 0 {
		m.publish(events...)
	}
	return resumed, interaction, events, nil
}

func responsePayloadIsEmpty(value any) bool {
	payload, ok := value.(map[string]any)
	if !ok || len(payload) == 0 {
		return true
	}
	for key, field := range payload {
		switch key {
		case "selected_option_id", "custom_text":
			if strings.TrimSpace(fmt.Sprint(field)) != "" {
				return false
			}
		case "selected_option_ids":
			switch typed := field.(type) {
			case []string:
				for _, item := range typed {
					if strings.TrimSpace(item) != "" {
						return false
					}
				}
			case []any:
				for _, item := range typed {
					if strings.TrimSpace(fmt.Sprint(item)) != "" {
						return false
					}
				}
			}
		}
	}
	return true
}

// ResolveTaskApproval 解析审批并在 waiting 任务上安全恢复一次。
func (m *Manager) ResolveTaskApproval(ctx context.Context, taskID string, approvalID string, input approvals.ResolveApprovalInput) (*approvals.ToolApproval, error) {
	if m == nil || m.approvals == nil || m.store == nil {
		return nil, fmt.Errorf("approval store is not configured")
	}
	approval, _, err := m.approvals.ResolveApproval(ctx, taskID, approvalID, input)
	if err != nil {
		return nil, err
	}
	approval, events, err := m.finalizeResolvedApproval(ctx, approval)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		m.publish(events...)
	}
	return approval, nil
}

// ExpireTaskApproval 将 pending 审批标记为 expired，并按 resolved 路径安全恢复一次 waiting 任务。
func (m *Manager) ExpireTaskApproval(ctx context.Context, taskID string, approvalID string, reason string) (*approvals.ToolApproval, error) {
	if m == nil || m.approvals == nil || m.store == nil {
		return nil, fmt.Errorf("approval store is not configured")
	}
	approval, _, err := m.approvals.ExpireApproval(ctx, taskID, approvalID, reason)
	if err != nil {
		return nil, err
	}
	approval, events, err := m.finalizeResolvedApproval(ctx, approval)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		m.publish(events...)
	}
	return approval, nil
}

// Subscribe 订阅某个任务的实时事件。
func (m *Manager) Subscribe(taskID string) (<-chan TaskEvent, func()) {
	return m.hub.Subscribe(taskID)
}

// CancelTask 发起任务取消。
//
// 对 queued 任务会直接收敛到 cancelled；对 running 任务会先写入
// cancel_requested，再通过取消函数向执行上下文传播信号。
func (m *Manager) CancelTask(ctx context.Context, id string) (*Task, error) {
	current, err := m.store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status.IsTerminal() {
		return current, nil
	}
	if current.Status == StatusCancelRequested {
		if cancel := m.lookupCancel(id); cancel != nil {
			cancel()
			return current, nil
		}
		return m.forceFinalizeCancelledTask(ctx, current)
	}

	updated, events, err := m.store.RequestCancel(ctx, id)
	if err != nil {
		return nil, err
	}
	m.publish(events...)
	if updated.Status.IsTerminal() {
		return updated, nil
	}
	if updated.Status == StatusCancelRequested {
		if cancel := m.lookupCancel(id); cancel != nil {
			cancel()
			return updated, nil
		}
	}

	// 没有活动执行器的任务可直接在管理器层完成终态转换。
	if updated.Status == StatusCancelRequested && taskHasNoActiveExecutor(updated) {
		return m.finalizeCancelledTask(ctx, updated)
	}

	// 已在执行中的任务依赖协作式取消，由执行上下文感知并退出。
	if cancel := m.lookupCancel(id); cancel != nil {
		cancel()
	}
	return updated, nil
}

// RetryTask 基于原任务创建新的排队任务。
func (m *Manager) RetryTask(ctx context.Context, id string) (*Task, error) {
	task, events, err := m.store.RetryTask(ctx, id)
	if err != nil {
		return nil, err
	}
	m.recordTaskCreated(task)
	m.publish(events...)
	return task, nil
}

// runWorker 持续轮询并执行单个 worker 领取到的任务。
func (m *Manager) runWorker(ctx context.Context, workerIndex int) {
	_ = workerIndex
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// 1. 从持久层领取下一个待执行任务。
		task, events, err := m.store.ClaimNextTask(ctx, m.runnerID, m.leaseDuration)
		if err != nil {
			time.Sleep(m.pollInterval)
			continue
		}
		if task == nil {
			if m.reconcileResolvedWaitingToolApprovalTasks(ctx) {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(m.pollInterval):
			}
			continue
		}

		// 2. 先发布 task.started，再执行实际任务逻辑。
		m.recordTaskStarted(task)
		m.publish(events...)
		m.executeTask(ctx, task)
	}
}

func (m *Manager) reconcileResolvedWaitingToolApprovalTasks(ctx context.Context) bool {
	if m == nil || m.store == nil || m.store.db == nil {
		return false
	}

	var waitingTasks []Task
	if err := m.store.db.WithContext(ctx).
		Where("status = ?", StatusWaiting).
		Where("suspend_reason IN ?", []string{"waiting_for_interaction", "waiting_for_tool_approval"}).
		Order("updated_at asc").
		Limit(max(1, m.workerCount)).
		Find(&waitingTasks).Error; err != nil {
		return false
	}

	recoveredAny := false
	for i := range waitingTasks {
		task := waitingTasks[i]
		interactionID, ok, err := taskApprovalCheckpointID(task.MetadataJSON)
		if err != nil || !ok {
			continue
		}
		resolvedApproval, err := m.loadResolvedApprovalForInteraction(ctx, task.ID, interactionID)
		if err != nil || resolvedApproval == nil || resolvedApproval.Status == approvals.StatusPending {
			continue
		}
		finalized, events, err := m.finalizeResolvedApproval(ctx, resolvedApproval)
		if err != nil {
			continue
		}
		if finalized != nil && task.Status == StatusWaiting && finalized.Status != approvals.StatusPending {
			recoveredAny = true
		}
		if len(events) > 0 {
			m.publish(events...)
		}
	}

	return recoveredAny
}

// executeTask 执行单个已领取任务，并根据结果写入终态。
func (m *Manager) executeTask(ctx context.Context, task *Task) {
	executor, ok := m.executor(task.TaskType)
	if !ok {
		failed, events, err := m.store.MarkFailed(context.Background(), task.ID, map[string]any{"message": "executor not found"})
		if err == nil {
			if len(events) > 0 {
				m.recordTaskFinished(failed, map[string]any{"message": "executor not found"})
			}
			m.publish(events...)
			m.tryResumeParentAfterChild(failed)
		}
		return
	}

	// 1. 为当前任务创建可取消上下文，并登记取消函数。
	taskCtx, cancel := context.WithCancel(ctx)
	m.setActiveCancel(task.ID, cancel)
	defer func() {
		cancel()
		m.clearActiveCancel(task.ID)
	}()

	// 2. 后台刷新租约心跳，避免长任务被误判为失联。
	go m.heartbeatLoop(taskCtx, task.ID)

	// 3. 调用注册执行器，并把任务运行时交给上层逻辑使用。
	runtime := newRuntime(m, task.ID)
	result, execErr := executor(taskCtx, task, runtime)

	var finished *Task
	var events []TaskEvent
	var reason any
	// 4. 根据执行结果写入最终状态。
	switch {
	case errors.Is(execErr, context.Canceled) || errors.Is(taskCtx.Err(), context.Canceled):
		reason = map[string]any{"message": "task cancelled"}
		finished, events, execErr = m.store.MarkCancelled(context.Background(), task.ID, reason)
	case runtime.isSuspended() && m.taskStatusIs(context.Background(), task.ID, StatusWaiting):
		return
	case errors.Is(execErr, ErrTaskSuspended):
		return
	case execErr != nil:
		reason = map[string]any{"message": execErr.Error()}
		finished, events, execErr = m.store.MarkFailed(context.Background(), task.ID, reason)
	default:
		finished, events, execErr = m.store.MarkSucceeded(context.Background(), task.ID, result)
	}
	if execErr == nil {
		if len(events) > 0 {
			m.recordTaskFinished(finished, reason)
		}
		m.publish(events...)
		m.tryResumeParentAfterChild(finished)
	}
}

func (m *Manager) taskStatusIs(ctx context.Context, taskID string, want Status) bool {
	if m == nil || m.store == nil {
		return false
	}
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil || task == nil {
		return false
	}
	return task.Status == want
}

// heartbeatLoop 在任务运行期间定期刷新租约信息。
func (m *Manager) heartbeatLoop(ctx context.Context, taskID string) {
	ticker := time.NewTicker(m.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = m.store.UpdateHeartbeat(context.Background(), taskID, m.runnerID, m.leaseDuration)
		}
	}
}

// publish 将事件推送给所有实时订阅者。
func (m *Manager) publish(events ...TaskEvent) {
	m.hub.Publish(events...)
}

func (m *Manager) recordTaskCreated(task *Task) {
	if m == nil || m.audit == nil || task == nil {
		return
	}
	run, err := m.audit.StartRun(context.Background(), AuditStartRunInput{
		TaskID:    task.ID,
		TaskType:  task.TaskType,
		CreatedBy: task.CreatedBy,
		Status:    StatusQueued,
	})
	if err != nil || run == nil {
		return
	}
	_, _ = m.audit.AppendEvent(context.Background(), run.ID, AuditAppendEventInput{
		EventType: "run.created",
		Payload: map[string]any{
			"status":    task.Status,
			"task_type": task.TaskType,
		},
	})
}

func (m *Manager) recordTaskStarted(task *Task) {
	if m == nil || m.audit == nil || task == nil {
		return
	}
	run, err := m.audit.StartRun(context.Background(), AuditStartRunInput{
		TaskID:    task.ID,
		TaskType:  task.TaskType,
		RunnerID:  task.RunnerID,
		CreatedBy: task.CreatedBy,
		Status:    StatusRunning,
		StartedAt: derefTime(task.StartedAt),
	})
	if err != nil || run == nil {
		return
	}
	_, _ = m.audit.AppendEvent(context.Background(), run.ID, AuditAppendEventInput{
		EventType: "run.started",
		Payload: map[string]any{
			"status":    task.Status,
			"runner_id": task.RunnerID,
		},
	})
}

func (m *Manager) recordTaskWaiting(task *Task) {
	if m == nil || m.audit == nil || task == nil {
		return
	}
	run, err := m.audit.StartRun(context.Background(), AuditStartRunInput{
		TaskID:    task.ID,
		TaskType:  task.TaskType,
		RunnerID:  task.RunnerID,
		CreatedBy: task.CreatedBy,
		Status:    StatusWaiting,
		StartedAt: derefTime(task.StartedAt),
	})
	if err != nil || run == nil {
		return
	}
	_, _ = m.audit.AppendEvent(context.Background(), run.ID, AuditAppendEventInput{
		EventType: "run.waiting",
		Payload: map[string]any{
			"status":         task.Status,
			"suspend_reason": task.SuspendReason,
		},
	})
}

func (m *Manager) recordTaskFinished(task *Task, reason any) {
	if m == nil || m.audit == nil || task == nil || !task.Status.IsTerminal() {
		return
	}
	run, err := m.audit.StartRun(context.Background(), AuditStartRunInput{
		TaskID:    task.ID,
		TaskType:  task.TaskType,
		RunnerID:  task.RunnerID,
		CreatedBy: task.CreatedBy,
		Status:    task.Status,
		StartedAt: derefTime(task.StartedAt),
	})
	if err != nil || run == nil {
		return
	}
	input := AuditAppendEventInput{
		EventType: terminalAuditEventType(task.Status),
		Payload: map[string]any{
			"status": task.Status,
		},
	}
	if reason != nil {
		input.Payload = map[string]any{
			"status": task.Status,
			"error":  reason,
		}
	}
	if input.EventType == "" {
		return
	}
	if _, err := m.audit.AppendEvent(context.Background(), run.ID, input); err != nil {
		return
	}
	_ = m.audit.FinishRun(context.Background(), run.ID, AuditFinishRunInput{
		Status:     task.Status,
		FinishedAt: derefTime(task.FinishedAt),
	})
}

// executor 读取指定任务类型对应的执行器。
func (m *Manager) executor(taskType string) (Executor, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	executor, ok := m.executors[taskType]
	return executor, ok
}

// setActiveCancel 记录当前正在执行任务的取消函数。
func (m *Manager) setActiveCancel(taskID string, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeCancel[taskID] = cancel
}

// lookupCancel 查询已登记的任务取消函数。
func (m *Manager) lookupCancel(taskID string) context.CancelFunc {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeCancel[taskID]
}

// clearActiveCancel 清理已完成任务的取消函数引用。
func (m *Manager) clearActiveCancel(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.activeCancel, taskID)
}

func terminalAuditEventType(status Status) string {
	switch status {
	case StatusSucceeded:
		return "run.succeeded"
	case StatusFailed:
		return "run.failed"
	case StatusCancelled:
		return "run.cancelled"
	default:
		return ""
	}
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

func taskIsQueuedBeforeExecution(task *Task) bool {
	if task == nil {
		return false
	}
	return task.RunnerID == "" && task.StartedAt == nil
}

func taskHasNoActiveExecutor(task *Task) bool {
	if task == nil {
		return false
	}
	return task.RunnerID == ""
}

func (m *Manager) tryResumeParentAfterChild(task *Task) {
	if m == nil || m.store == nil || task == nil || task.ParentTaskID == "" {
		return
	}

	_, events, err := m.store.TryResumeParentTask(context.Background(), task.ParentTaskID)
	if err != nil {
		return
	}
	m.publish(events...)
}

func (m *Manager) cancelPendingApprovals(ctx context.Context, taskID string) error {
	if m == nil || m.approvals == nil {
		return nil
	}
	listed, err := m.approvals.ListTaskApprovals(ctx, taskID)
	if err != nil {
		return err
	}
	pendingIDs := make([]string, 0, len(listed))
	for _, approval := range listed {
		if approval.Status == approvals.StatusPending {
			pendingIDs = append(pendingIDs, approval.ID)
		}
	}
	if len(pendingIDs) == 0 {
		return nil
	}
	if _, err := m.approvals.CancelPendingApprovalsByTask(ctx, taskID); err != nil {
		return err
	}
	for _, approvalID := range pendingIDs {
		approval, err := m.approvals.GetApproval(ctx, taskID, approvalID)
		if err != nil {
			return err
		}
		_, events, err := m.finalizeResolvedApproval(ctx, approval)
		if err != nil {
			return err
		}
		if len(events) > 0 {
			m.publish(events...)
		}
	}
	return nil
}

func (m *Manager) finalizeCreatedApproval(ctx context.Context, approval *approvals.ToolApproval) (*approvals.ToolApproval, []TaskEvent, error) {
	if m == nil || m.store == nil || approval == nil {
		return approval, nil, nil
	}

	var events []TaskEvent
	var finalized approvals.ToolApproval
	err := m.store.withWriteRetry(ctx, func() error {
		return m.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			loaded, err := loadApprovalTx(tx, approval.TaskID, approval.ID)
			if err != nil {
				return err
			}
			if loaded.RequestedEventPublishedAt == nil {
				event, err := appendEventTx(tx, loaded.TaskID, EventApprovalRequested, "info", map[string]any{
					"approval_id":       loaded.ID,
					"task_id":           loaded.TaskID,
					"conversation_id":   loaded.ConversationID,
					"step":              loaded.StepIndex,
					"tool_call_id":      loaded.ToolCallID,
					"tool_name":         loaded.ToolName,
					"arguments_summary": loaded.ArgumentsSummary,
					"risk_level":        loaded.RiskLevel,
					"reason":            loaded.Reason,
					"status":            loaded.Status,
				})
				if err != nil {
					return err
				}
				now := time.Now().UTC()
				if err := tx.Model(&approvals.ToolApproval{}).
					Where("id = ? AND task_id = ?", loaded.ID, loaded.TaskID).
					Update("requested_event_published_at", now).Error; err != nil {
					return err
				}
				loaded.RequestedEventPublishedAt = &now
				events = append(events, event)
			}
			finalized = *loaded
			return nil
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return &finalized, events, nil
}

func (m *Manager) finalizeCancelledTask(ctx context.Context, task *Task) (*Task, error) {
	if m == nil || m.store == nil || task == nil {
		return task, nil
	}
	if task.Status != StatusCancelRequested || !taskHasNoActiveExecutor(task) {
		return task, nil
	}
	if err := m.cancelPendingApprovals(ctx, task.ID); err != nil {
		return nil, err
	}
	reason := map[string]any{"message": "task cancelled without active executor"}
	if taskIsQueuedBeforeExecution(task) {
		reason = map[string]any{"message": "task cancelled before execution"}
	}
	cancelled, finishEvents, err := m.store.MarkCancelled(ctx, task.ID, reason)
	if err != nil {
		return nil, err
	}
	if len(finishEvents) > 0 {
		m.recordTaskFinished(cancelled, reason)
	}
	m.publish(finishEvents...)
	m.tryResumeParentAfterChild(cancelled)
	return cancelled, nil
}

func (m *Manager) forceFinalizeCancelledTask(ctx context.Context, task *Task) (*Task, error) {
	if m == nil || m.store == nil || task == nil {
		return task, nil
	}
	if task.Status != StatusCancelRequested {
		return task, nil
	}
	if taskHasNoActiveExecutor(task) {
		return m.finalizeCancelledTask(ctx, task)
	}
	if err := m.cancelPendingApprovals(ctx, task.ID); err != nil {
		return nil, err
	}
	reason := map[string]any{"message": "task cancelled after executor became unavailable"}
	cancelled, finishEvents, err := m.store.MarkCancelled(ctx, task.ID, reason)
	if err != nil {
		return nil, err
	}
	if len(finishEvents) > 0 {
		m.recordTaskFinished(cancelled, reason)
	}
	m.publish(finishEvents...)
	m.tryResumeParentAfterChild(cancelled)
	return cancelled, nil
}

func (m *Manager) createInteraction(ctx context.Context, input interactions.CreateInteractionInput) (*interactions.Interaction, []TaskEvent, error) {
	if m == nil || m.interactions == nil || m.store == nil {
		return nil, nil, fmt.Errorf("interaction store is not configured")
	}
	var (
		created interactions.Interaction
		events  []TaskEvent
	)
	err := m.store.withWriteRetry(ctx, func() error {
		return m.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if existing := lookupInteractionTx(tx, input.TaskID, input.ID); existing != nil {
				created = *existing
				return nil
			}
			interaction, err := interactions.NewStore(tx).CreateInteraction(ctx, input)
			if err != nil {
				return err
			}
			payload, err := buildInteractionEventPayload(interaction)
			if err != nil {
				return err
			}
			event, err := appendEventTx(tx, interaction.TaskID, EventInteractionRequested, "info", payload)
			if err != nil {
				return err
			}
			created = *interaction
			events = append(events, event)
			return nil
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return &created, events, nil
}

func (m *Manager) finalizeQuestionInteraction(ctx context.Context, taskID string, interactionID string) (*Task, []TaskEvent, error) {
	if m == nil || m.interactions == nil || m.store == nil {
		return nil, nil, fmt.Errorf("interaction store is not configured")
	}
	interaction, err := m.interactions.GetInteraction(ctx, taskID, interactionID)
	if err != nil {
		return nil, nil, err
	}
	if interaction.Kind != interactions.KindQuestion || interaction.Status != interactions.StatusResponded {
		return nil, nil, nil
	}
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	if task.Status != StatusWaiting || task.SuspendReason != "waiting_for_interaction" {
		return nil, nil, nil
	}
	checkpointID, ok, err := taskApprovalCheckpointID(task.MetadataJSON)
	if err != nil {
		return nil, nil, err
	}
	if ok && checkpointID != interaction.ID {
		return nil, nil, nil
	}
	payload, err := buildInteractionEventPayload(interaction)
	if err != nil {
		return nil, nil, err
	}
	respondedEvent, err := m.store.AppendEvent(ctx, task.ID, EventInteractionResponded, "info", payload)
	if err != nil {
		return nil, nil, err
	}
	queued, resumeEvents, err := m.store.ResumeWaitingTask(ctx, task.ID, "interaction_resolved")
	if err != nil {
		return nil, nil, err
	}
	events := []TaskEvent{respondedEvent}
	events = append(events, resumeEvents...)
	return queued, events, nil
}

func resumeQuestionTaskTx(tx *gorm.DB, task *Task) (*TaskEvent, error) {
	if tx == nil || task == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	result := tx.Model(&Task{}).
		Where("id = ?", task.ID).
		Where("status = ?", StatusWaiting).
		Where("suspend_reason = ?", "waiting_for_interaction").
		Updates(map[string]any{
			"status":         StatusQueued,
			"suspend_reason": "",
			"updated_at":     now,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	task.Status = StatusQueued
	task.SuspendReason = ""
	task.UpdatedAt = now
	event, err := appendEventTx(tx, task.ID, EventTaskResumed, "info", map[string]any{
		"status":        task.Status,
		"resume_reason": "interaction_resolved",
	})
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (m *Manager) finalizeResolvedApproval(ctx context.Context, approval *approvals.ToolApproval) (*approvals.ToolApproval, []TaskEvent, error) {
	if m == nil || m.store == nil || approval == nil {
		return approval, nil, nil
	}

	var events []TaskEvent
	var finalized approvals.ToolApproval
	err := m.store.withWriteRetry(ctx, func() error {
		return m.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			loaded, err := loadApprovalTx(tx, approval.TaskID, approval.ID)
			if err != nil {
				return err
			}

			if loaded.ResolvedEventPublishedAt == nil {
				event, err := appendEventTx(tx, loaded.TaskID, EventApprovalResolved, "info", map[string]any{
					"approval_id":       loaded.ID,
					"task_id":           loaded.TaskID,
					"conversation_id":   loaded.ConversationID,
					"step":              loaded.StepIndex,
					"tool_call_id":      loaded.ToolCallID,
					"tool_name":         loaded.ToolName,
					"arguments_summary": loaded.ArgumentsSummary,
					"risk_level":        loaded.RiskLevel,
					"reason":            loaded.Reason,
					"decision":          approvalDecisionForStatus(loaded.Status),
					"decision_reason":   loaded.DecisionReason,
					"decision_by":       loaded.DecisionBy,
					"status":            loaded.Status,
				})
				if err != nil {
					return err
				}
				now := time.Now().UTC()
				if err := tx.Model(&approvals.ToolApproval{}).
					Where("id = ? AND task_id = ?", loaded.ID, loaded.TaskID).
					Update("resolved_event_published_at", now).Error; err != nil {
					return err
				}
				loaded.ResolvedEventPublishedAt = &now
				events = append(events, event)
			}

			if err := m.syncApprovalInteractionTx(tx, loaded); err != nil {
				return err
			}

			if err := finalizeApprovalTaskStateTx(tx, loaded, &events); err != nil {
				return err
			}
			if loaded.FinalizedAt == nil {
				now := time.Now().UTC()
				if err := tx.Model(&approvals.ToolApproval{}).
					Where("id = ? AND task_id = ?", loaded.ID, loaded.TaskID).
					Update("finalized_at", now).Error; err != nil {
					return err
				}
				loaded.FinalizedAt = &now
			}

			finalized = *loaded
			return nil
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return &finalized, events, nil
}

func finalizeApprovalTaskStateTx(tx *gorm.DB, approval *approvals.ToolApproval, events *[]TaskEvent) error {
	if tx == nil || approval == nil {
		return nil
	}
	task, err := loadTaskTx(tx, approval.TaskID)
	if err != nil {
		return err
	}
	if task.Status != StatusWaiting || !isInteractionWaitReason(task.SuspendReason) {
		return nil
	}
	if task.SuspendReason == "waiting_for_interaction" {
		checkpointID, ok, err := taskApprovalCheckpointID(task.MetadataJSON)
		if err != nil {
			return err
		}
		if !ok || checkpointID != approval.ID {
			return nil
		}
		interaction := lookupInteractionTx(tx, task.ID, checkpointID)
		if interaction == nil || interaction.Kind != interactions.KindApproval {
			return nil
		}
	}

	now := time.Now().UTC()
	result := tx.Model(&Task{}).
		Where("id = ?", task.ID).
		Where("status = ?", StatusWaiting).
		Where("suspend_reason IN ?", []string{"waiting_for_interaction", "waiting_for_tool_approval"}).
		Updates(map[string]any{
			"status":         StatusQueued,
			"suspend_reason": "",
			"updated_at":     now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}

	task.Status = StatusQueued
	task.SuspendReason = ""
	task.UpdatedAt = now
	event, err := appendEventTx(tx, task.ID, EventTaskResumed, "info", map[string]any{
		"status":        task.Status,
		"resume_reason": "interaction_resolved",
	})
	if err != nil {
		return err
	}
	*events = append(*events, event)
	return nil
}

func loadApprovalTx(tx *gorm.DB, taskID string, approvalID string) (*approvals.ToolApproval, error) {
	var approval approvals.ToolApproval
	err := tx.Where("id = ? AND task_id = ?", approvalID, taskID).Take(&approval).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", approvals.ErrApprovalNotFound, approvalID)
	}
	if err != nil {
		return nil, err
	}
	return &approval, nil
}

func approvalDecisionForStatus(status approvals.Status) approvals.Decision {
	switch status {
	case approvals.StatusApproved:
		return approvals.DecisionApprove
	case approvals.StatusRejected, approvals.StatusExpired:
		return approvals.DecisionReject
	default:
		return ""
	}
}

func (m *Manager) ensureApprovalInteraction(ctx context.Context, approval *approvals.ToolApproval) (*interactions.Interaction, error) {
	if m == nil || m.interactions == nil || approval == nil {
		return nil, nil
	}
	existing, err := m.interactions.GetInteraction(ctx, approval.TaskID, approval.ID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, interactions.ErrInteractionNotFound) {
		return nil, err
	}
	request := map[string]any{
		"tool_name":         approval.ToolName,
		"arguments_summary": approval.ArgumentsSummary,
		"risk_level":        approval.RiskLevel,
		"reason":            approval.Reason,
	}
	status := interactions.Status(approval.Status)
	if status == "" {
		status = interactions.StatusPending
	}
	var (
		response    any
		respondedBy string
		respondedAt *time.Time
	)
	if decision := approvalDecisionForStatus(approval.Status); decision != "" || approval.DecisionReason != "" {
		response = map[string]any{
			"decision": decision,
			"reason":   approval.DecisionReason,
		}
		respondedBy = approval.DecisionBy
		respondedAt = approval.DecisionAt
	}
	_, err = m.interactions.CreateInteraction(ctx, interactions.CreateInteractionInput{
		ID:             approval.ID,
		TaskID:         approval.TaskID,
		ConversationID: approval.ConversationID,
		StepIndex:      approval.StepIndex,
		ToolCallID:     approval.ToolCallID,
		Kind:           interactions.KindApproval,
		Status:         status,
		Request:        request,
		Response:       response,
		RespondedBy:    respondedBy,
		RespondedAt:    respondedAt,
	})
	if err != nil {
		return nil, err
	}
	return m.interactions.GetInteraction(ctx, approval.TaskID, approval.ID)
}

func (m *Manager) syncApprovalInteractionTx(tx *gorm.DB, approval *approvals.ToolApproval) error {
	if m == nil || m.interactions == nil || tx == nil || approval == nil {
		return nil
	}
	updates := map[string]any{
		"status":       interactions.Status(approval.Status),
		"responded_by": approval.DecisionBy,
		"responded_at": approval.DecisionAt,
		"updated_at":   time.Now().UTC(),
	}
	if decision := approvalDecisionForStatus(approval.Status); decision != "" || approval.DecisionReason != "" {
		payload, err := marshalApprovalInteractionResponse(decision, approval.DecisionReason)
		if err != nil {
			return err
		}
		updates["response_json"] = payload
	} else {
		updates["response_json"] = nil
	}
	result := tx.Model(&interactions.Interaction{}).
		Where("id = ? AND task_id = ?", approval.ID, approval.TaskID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func marshalApprovalInteractionResponse(decision approvals.Decision, reason string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"decision": decision,
		"reason":   reason,
	})
}

func buildInteractionEventPayload(interaction *interactions.Interaction) (map[string]any, error) {
	if interaction == nil {
		return nil, fmt.Errorf("interaction is required")
	}
	request, err := decodeInteractionJSON(interaction.RequestJSON)
	if err != nil {
		return nil, fmt.Errorf("decode interaction request: %w", err)
	}
	response, err := decodeInteractionJSON(interaction.ResponseJSON)
	if err != nil {
		return nil, fmt.Errorf("decode interaction response: %w", err)
	}
	payload := map[string]any{
		"id":              interaction.ID,
		"interaction_id":  interaction.ID,
		"task_id":         interaction.TaskID,
		"conversation_id": interaction.ConversationID,
		"step":            interaction.StepIndex,
		"tool_call_id":    interaction.ToolCallID,
		"kind":            interaction.Kind,
		"status":          interaction.Status,
		"request_json":    request,
		"created_at":      interaction.CreatedAt,
		"updated_at":      interaction.UpdatedAt,
	}
	if response != nil {
		payload["response_json"] = response
	}
	if interaction.RespondedBy != "" {
		payload["responded_by"] = interaction.RespondedBy
	}
	if interaction.RespondedAt != nil {
		payload["responded_at"] = interaction.RespondedAt
	}
	return payload, nil
}

func decodeInteractionJSON(raw []byte) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func lookupInteractionTx(tx *gorm.DB, taskID string, interactionID string) *interactions.Interaction {
	if tx == nil || taskID == "" || interactionID == "" {
		return nil
	}
	var interaction interactions.Interaction
	if err := tx.Where("id = ? AND task_id = ?", interactionID, taskID).Take(&interaction).Error; err != nil {
		return nil
	}
	return &interaction
}

func (m *Manager) loadResolvedApprovalForInteraction(ctx context.Context, taskID string, interactionID string) (*approvals.ToolApproval, error) {
	if m == nil || m.approvals == nil {
		return nil, fmt.Errorf("approval store is not configured")
	}
	if m.interactions != nil {
		interaction, err := m.interactions.GetInteraction(ctx, taskID, interactionID)
		if err == nil {
			if interaction.Kind != interactions.KindApproval {
				return nil, nil
			}
			return m.approvals.GetApproval(ctx, taskID, interaction.ID)
		}
		if err != nil && !errors.Is(err, interactions.ErrInteractionNotFound) {
			return nil, err
		}
	}
	return m.approvals.GetApproval(ctx, taskID, interactionID)
}
