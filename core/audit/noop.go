package audit

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type NoopRecorder struct {
	seq atomic.Int64
}

func NewNoopRecorder() Recorder {
	return &NoopRecorder{}
}

func (n *NoopRecorder) StartRun(_ context.Context, input StartRunInput) (*Run, error) {
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = newRunID()
	}
	taskID := strings.TrimSpace(input.TaskID)
	if taskID == "" {
		taskID = newNoopTaskID()
	}
	taskType := strings.TrimSpace(input.TaskType)
	if taskType == "" {
		taskType = "noop"
	}
	status := input.Status
	if status == "" {
		status = StatusQueued
	}
	schemaVersion := input.SchemaVersion
	if schemaVersion == "" {
		schemaVersion = SchemaVersionV1
	}
	var startedAt *time.Time
	if !input.StartedAt.IsZero() {
		started := input.StartedAt.UTC()
		startedAt = &started
	}
	return &Run{
		ID:             runID,
		TaskID:         taskID,
		ConversationID: input.ConversationID,
		TaskType:       taskType,
		ProviderID:     input.ProviderID,
		ModelID:        input.ModelID,
		RunnerID:       input.RunnerID,
		Status:         status,
		CreatedBy:      input.CreatedBy,
		Replayable:     input.Replayable,
		SchemaVersion:  schemaVersion,
		StartedAt:      startedAt,
	}, nil
}

func (n *NoopRecorder) AppendEvent(_ context.Context, runID string, input AppendEventInput) (*Event, error) {
	resolvedRunID := strings.TrimSpace(runID)
	if resolvedRunID == "" {
		resolvedRunID = newRunID()
	}
	eventType := strings.TrimSpace(input.EventType)
	if eventType == "" {
		eventType = "noop.event"
	}
	payloadJSON, err := marshalJSON(input.Payload, true)
	if err != nil {
		return nil, err
	}
	createdAt := input.CreatedAt.UTC()
	if input.CreatedAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return &Event{
		RunID:         resolvedRunID,
		Seq:           n.seq.Add(1),
		Phase:         normalizePhase(input.Phase, eventType),
		EventType:     eventType,
		Level:         normalizeLevel(input.Level),
		StepIndex:     input.StepIndex,
		ParentSeq:     input.ParentSeq,
		RefArtifactID: input.RefArtifactID,
		PayloadJSON:   payloadJSON,
		CreatedAt:     createdAt,
	}, nil
}

func (n *NoopRecorder) AttachArtifact(_ context.Context, runID string, input CreateArtifactInput) (*Artifact, error) {
	resolvedRunID := strings.TrimSpace(runID)
	if resolvedRunID == "" {
		resolvedRunID = newRunID()
	}
	artifactID := strings.TrimSpace(input.ArtifactID)
	if artifactID == "" {
		artifactID = newArtifactID()
	}
	bodyJSON, err := marshalJSON(input.Body, false)
	if err != nil {
		return nil, err
	}
	createdAt := input.CreatedAt.UTC()
	if input.CreatedAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return &Artifact{
		ID:             artifactID,
		RunID:          resolvedRunID,
		Kind:           input.Kind,
		MimeType:       input.MimeType,
		Encoding:       normalizeEncoding(input.Encoding),
		SizeBytes:      int64(len(bodyJSON)),
		SHA256:         input.SHA256,
		RedactionState: normalizeRedactionState(input.RedactionState),
		BodyJSON:       bodyJSON,
		CreatedAt:      createdAt,
	}, nil
}

func (*NoopRecorder) FinishRun(context.Context, string, FinishRunInput) error {
	return nil
}

func newNoopTaskID() string {
	return "tsk_" + uuid.NewString()
}
