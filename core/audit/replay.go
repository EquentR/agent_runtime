package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ReplayBundle struct {
	Run       RunSummary         `json:"run"`
	Timeline  []ReplayEventEntry `json:"timeline"`
	Artifacts []ReplayArtifact   `json:"artifacts"`
}

type RunSummary struct {
	ID             string        `json:"id"`
	TaskID         string        `json:"task_id"`
	ConversationID string        `json:"conversation_id,omitempty"`
	TaskType       string        `json:"task_type"`
	ProviderID     string        `json:"provider_id,omitempty"`
	ModelID        string        `json:"model_id,omitempty"`
	RunnerID       string        `json:"runner_id,omitempty"`
	Status         Status        `json:"status"`
	CreatedBy      string        `json:"created_by,omitempty"`
	Replayable     bool          `json:"replayable"`
	SchemaVersion  SchemaVersion `json:"schema_version"`
	StartedAt      *time.Time    `json:"started_at,omitempty"`
	FinishedAt     *time.Time    `json:"finished_at,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type ReplayEventEntry struct {
	Seq         int64            `json:"seq"`
	Phase       Phase            `json:"phase"`
	EventType   string           `json:"event_type"`
	DisplayName string           `json:"display_name"`
	Level       string           `json:"level"`
	StepIndex   int              `json:"step_index,omitempty"`
	ParentSeq   int64            `json:"parent_seq,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	Payload     json.RawMessage  `json:"payload,omitempty"`
	Artifact    *ArtifactSummary `json:"artifact,omitempty"`
}

type ArtifactSummary struct {
	ID             string       `json:"id"`
	Kind           ArtifactKind `json:"kind"`
	MimeType       string       `json:"mime_type"`
	Encoding       string       `json:"encoding"`
	SizeBytes      int64        `json:"size_bytes"`
	SHA256         string       `json:"sha256,omitempty"`
	RedactionState string       `json:"redaction_state"`
	CreatedAt      time.Time    `json:"created_at"`
}

type ReplayArtifact struct {
	ArtifactSummary
	Body json.RawMessage `json:"body,omitempty"`
}

var ErrReplayNotReplayable = errors.New("audit run is not replayable")
var ErrReplayUnsupportedSchemaVersion = errors.New("audit run has unsupported schema version")
var ErrReplayRunNotFinished = errors.New("audit run is not finished")

var replayRetainedArtifactKinds = map[ArtifactKind]struct{}{
	ArtifactKindRequestMessages:       {},
	ArtifactKindErrorSnapshot:         {},
	ArtifactKindResolvedPrompt:        {},
	ArtifactKindRuntimePromptEnvelope: {},
	ArtifactKindModelRequest:          {},
	ArtifactKindModelResponse:         {},
	ArtifactKindToolArguments:         {},
	ArtifactKindToolOutput:            {},
}

var replayEventDisplayNames = map[string]string{
	"run.created":           "运行已创建",
	"run.started":           "运行开始",
	"run.waiting":           "运行等待中",
	"run.finished":          "运行完成",
	"run.succeeded":         "运行成功",
	"run.failed":            "运行失败",
	"conversation.loaded":   "会话已加载",
	"user_message.appended": "用户消息追加",
	"step.started":          "步骤开始",
	"step.finished":         "步骤完成",
	"prompt.resolved":       "提示词解析",
	"request.built":         "构建 LLM 请求",
	"model.completed":       "模型生成",
	"tool.started":          "工具调用开始",
	"tool.called":           "工具调用",
	"tool.finished":         "工具调用完成",
	"approval.requested":    "审批请求",
	"approval.resolved":     "审批已处理",
	"interaction.requested": "用户交互请求",
	"interaction.responded": "用户交互已响应",
	"messages.persisted":    "消息已持久化",
}

func BuildReplayBundle(ctx context.Context, store *Store, runID string) (*ReplayBundle, error) {
	if store == nil {
		return nil, fmt.Errorf("store cannot be nil")
	}

	run, err := store.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if err := validateReplayBundleRun(run); err != nil {
		return nil, err
	}
	events, err := store.ListEvents(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	artifactIDs := referencedArtifactIDs(events)
	artifacts, err := store.ListArtifacts(ctx, run.ID)
	if err != nil {
		return nil, err
	}

	artifactsByID := make(map[string]ReplayArtifact, len(artifacts))
	for _, artifact := range artifacts {
		artifactsByID[artifact.ID] = newReplayArtifact(artifact)
	}

	bundle := &ReplayBundle{
		Run:      summarizeRun(*run),
		Timeline: make([]ReplayEventEntry, 0, len(events)),
	}
	if len(artifactIDs) > 0 {
		bundle.Artifacts = make([]ReplayArtifact, 0, len(artifactIDs))
	}

	for _, event := range events {
		entry := ReplayEventEntry{
			Seq:         event.Seq,
			Phase:       event.Phase,
			EventType:   event.EventType,
			DisplayName: replayEventDisplayName(event.EventType),
			Level:       event.Level,
			StepIndex:   event.StepIndex,
			ParentSeq:   event.ParentSeq,
			CreatedAt:   event.CreatedAt,
			Payload:     cloneRawJSON(event.PayloadJSON),
		}
		if event.RefArtifactID != "" {
			artifact, ok := artifactsByID[event.RefArtifactID]
			if !ok {
				return nil, fmt.Errorf("referenced artifact %q not found for run %q", event.RefArtifactID, run.ID)
			}
			summary := artifact.ArtifactSummary
			entry.Artifact = &summary
		}
		bundle.Timeline = append(bundle.Timeline, entry)
	}

	appendedArtifactIDs := make(map[string]struct{}, len(artifactIDs))
	for _, artifactID := range artifactIDs {
		artifact, ok := artifactsByID[artifactID]
		if !ok {
			return nil, fmt.Errorf("referenced artifact %q not found for run %q", artifactID, run.ID)
		}
		bundle.Artifacts = append(bundle.Artifacts, artifact)
		appendedArtifactIDs[artifactID] = struct{}{}
	}
	for _, artifact := range artifacts {
		if !shouldRetainReplayArtifact(artifact) {
			continue
		}
		if _, ok := appendedArtifactIDs[artifact.ID]; ok {
			continue
		}
		bundle.Artifacts = append(bundle.Artifacts, artifactsByID[artifact.ID])
		appendedArtifactIDs[artifact.ID] = struct{}{}
	}

	if bundle.Artifacts == nil {
		bundle.Artifacts = []ReplayArtifact{}
	}
	if bundle.Timeline == nil {
		bundle.Timeline = []ReplayEventEntry{}
	}

	return bundle, nil
}

func summarizeRun(run Run) RunSummary {
	return RunSummary{
		ID:             run.ID,
		TaskID:         run.TaskID,
		ConversationID: run.ConversationID,
		TaskType:       run.TaskType,
		ProviderID:     run.ProviderID,
		ModelID:        run.ModelID,
		RunnerID:       run.RunnerID,
		Status:         run.Status,
		CreatedBy:      run.CreatedBy,
		Replayable:     run.Replayable,
		SchemaVersion:  run.SchemaVersion,
		StartedAt:      cloneTimePointer(run.StartedAt),
		FinishedAt:     cloneTimePointer(run.FinishedAt),
		CreatedAt:      run.CreatedAt,
		UpdatedAt:      run.UpdatedAt,
	}
}

func referencedArtifactIDs(events []Event) []string {
	if len(events) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(events))
	ids := make([]string, 0, len(events))
	for _, event := range events {
		artifactID := strings.TrimSpace(event.RefArtifactID)
		if artifactID == "" {
			continue
		}
		if _, ok := seen[artifactID]; ok {
			continue
		}
		seen[artifactID] = struct{}{}
		ids = append(ids, artifactID)
	}
	return ids
}

func validateReplayBundleRun(run *Run) error {
	if run == nil {
		return fmt.Errorf("run cannot be nil")
	}
	if !run.Replayable {
		return fmt.Errorf("%w: run %q", ErrReplayNotReplayable, run.ID)
	}
	if run.SchemaVersion != SchemaVersionV1 {
		return fmt.Errorf("%w: %q", ErrReplayUnsupportedSchemaVersion, run.SchemaVersion)
	}
	if !run.Status.IsTerminal() {
		return fmt.Errorf("%w: status %q", ErrReplayRunNotFinished, run.Status)
	}
	return nil
}

func newReplayArtifact(artifact Artifact) ReplayArtifact {
	replayArtifact := ReplayArtifact{ArtifactSummary: newArtifactSummary(artifact)}
	if shouldInlineReplayArtifactBody(artifact) {
		replayArtifact.Body = cloneRawJSON(artifact.BodyJSON)
	}
	return replayArtifact
}

func newArtifactSummary(artifact Artifact) ArtifactSummary {
	return ArtifactSummary{
		ID:             artifact.ID,
		Kind:           artifact.Kind,
		MimeType:       artifact.MimeType,
		Encoding:       artifact.Encoding,
		SizeBytes:      artifact.SizeBytes,
		SHA256:         artifact.SHA256,
		RedactionState: artifact.RedactionState,
		CreatedAt:      artifact.CreatedAt,
	}
}

func shouldInlineReplayArtifactBody(artifact Artifact) bool {
	if !shouldRetainReplayArtifact(artifact) {
		return false
	}
	mimeType := strings.ToLower(strings.TrimSpace(artifact.MimeType))
	if !strings.HasPrefix(mimeType, "application/json") {
		return false
	}
	return true
}

func shouldRetainReplayArtifact(artifact Artifact) bool {
	_, ok := replayRetainedArtifactKinds[artifact.Kind]
	return ok
}

func replayEventDisplayName(eventType string) string {
	trimmed := strings.TrimSpace(eventType)
	if trimmed == "" {
		return "审计事件"
	}

	if displayName, ok := replayEventDisplayNames[trimmed]; ok {
		return displayName
	}
	return trimmed
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return json.RawMessage(cloned)
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
