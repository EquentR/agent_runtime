package agent

import (
	"context"
	"strings"

	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
)

type executorAuditor struct {
	recorder       coreaudit.Recorder
	task           *coretasks.Task
	conversationID string
	providerID     string
	modelID        string
	createdBy      string
}

type requestMessagesArtifact struct {
	ConversationID string          `json:"conversation_id,omitempty"`
	Messages       []model.Message `json:"messages"`
}

type errorSnapshotArtifact struct {
	TaskID          string          `json:"task_id,omitempty"`
	ConversationID  string          `json:"conversation_id,omitempty"`
	ProviderID      string          `json:"provider_id,omitempty"`
	ModelID         string          `json:"model_id,omitempty"`
	Error           string          `json:"error"`
	PartialMessages []model.Message `json:"partial_messages,omitempty"`
}

func newExecutorAuditor(recorder coreaudit.Recorder, task *coretasks.Task, input RunTaskInput) *executorAuditor {
	auditor := &executorAuditor{recorder: recorder, task: task}
	auditor.setInput(input)
	if task != nil {
		auditor.createdBy = firstNonEmpty(auditor.createdBy, task.CreatedBy)
	}
	return auditor
}

func (a *executorAuditor) setInput(input RunTaskInput) {
	if a == nil {
		return
	}
	a.conversationID = firstNonEmpty(a.conversationID, input.ConversationID)
	a.providerID = firstNonEmpty(strings.TrimSpace(input.ProviderID), a.providerID)
	a.modelID = firstNonEmpty(strings.TrimSpace(input.ModelID), a.modelID)
	a.createdBy = firstNonEmpty(strings.TrimSpace(input.CreatedBy), a.createdBy)
}

func (a *executorAuditor) setConversation(conversation *Conversation) {
	if a == nil || conversation == nil {
		return
	}
	a.conversationID = firstNonEmpty(conversation.ID, a.conversationID)
	a.providerID = firstNonEmpty(strings.TrimSpace(conversation.ProviderID), a.providerID)
	a.modelID = firstNonEmpty(strings.TrimSpace(conversation.ModelID), a.modelID)
}

func (a *executorAuditor) recordConversationLoaded(ctx context.Context, history []model.Message) {
	a.appendEvent(ctx, "conversation.loaded", map[string]any{
		"conversation_id": a.conversationID,
		"message_count":   len(history),
	}, "")
}

func (a *executorAuditor) recordUserMessageAppended(ctx context.Context, userMessage model.Message, requestMessages []model.Message) {
	artifactID := a.attachArtifact(ctx, coreaudit.ArtifactKindRequestMessages, requestMessagesArtifact{
		ConversationID: a.conversationID,
		Messages:       cloneMessages(requestMessages),
	})
	a.appendEvent(ctx, "user_message.appended", map[string]any{
		"conversation_id":       a.conversationID,
		"message_role":          userMessage.Role,
		"content_length":        len(userMessage.Content),
		"request_message_count": len(requestMessages),
	}, artifactID)
}

func (a *executorAuditor) recordMessagesPersisted(ctx context.Context, messages []model.Message) {
	if len(messages) == 0 {
		return
	}
	a.appendEvent(ctx, "messages.persisted", map[string]any{
		"conversation_id": a.conversationID,
		"message_count":   len(messages),
		"roles":           messageRoles(messages),
	}, "")
}

func (a *executorAuditor) recordErrorSnapshot(ctx context.Context, err error, partialMessages []model.Message) {
	if a == nil || err == nil {
		return
	}
	_ = a.attachArtifact(ctx, coreaudit.ArtifactKindErrorSnapshot, errorSnapshotArtifact{
		TaskID:          taskID(a.task),
		ConversationID:  a.conversationID,
		ProviderID:      a.providerID,
		ModelID:         a.modelID,
		Error:           err.Error(),
		PartialMessages: cloneMessages(partialMessages),
	})
}

func (a *executorAuditor) appendEvent(ctx context.Context, eventType string, payload any, refArtifactID string) {
	runID := a.ensureRun(ctx)
	if runID == "" || a.recorder == nil {
		return
	}
	_, _ = a.recorder.AppendEvent(ctx, runID, coreaudit.AppendEventInput{
		Phase:         coreaudit.PhaseConversation,
		EventType:     eventType,
		RefArtifactID: refArtifactID,
		Payload:       payload,
	})
}

func (a *executorAuditor) attachArtifact(ctx context.Context, kind coreaudit.ArtifactKind, body any) string {
	runID := a.ensureRun(ctx)
	if runID == "" || a.recorder == nil {
		return ""
	}
	artifact, err := a.recorder.AttachArtifact(ctx, runID, coreaudit.CreateArtifactInput{
		Kind:     kind,
		MimeType: "application/json",
		Encoding: "utf-8",
		Body:     body,
	})
	if err != nil || artifact == nil {
		return ""
	}
	return artifact.ID
}

func (a *executorAuditor) ensureRun(ctx context.Context) string {
	if a == nil || a.recorder == nil || a.task == nil {
		return ""
	}
	run, err := a.recorder.StartRun(ctx, coreaudit.StartRunInput{
		TaskID:         a.task.ID,
		ConversationID: a.conversationID,
		TaskType:       firstNonEmpty(a.task.TaskType, "agent.run"),
		ProviderID:     a.providerID,
		ModelID:        a.modelID,
		CreatedBy:      firstNonEmpty(a.createdBy, a.task.CreatedBy),
		Replayable:     true,
		SchemaVersion:  coreaudit.SchemaVersionV1,
		Status:         coreaudit.StatusRunning,
	})
	if err != nil || run == nil {
		return ""
	}
	return run.ID
}

func messageRoles(messages []model.Message) []string {
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		role := strings.TrimSpace(string(message.Role))
		if role == "" {
			role = "unknown"
		}
		roles = append(roles, role)
	}
	return roles
}

func taskID(task *coretasks.Task) string {
	if task == nil {
		return ""
	}
	return task.ID
}
