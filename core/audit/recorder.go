package audit

import (
	"context"
	"time"
)

type StartRunInput struct {
	RunID          string
	TaskID         string
	ConversationID string
	TaskType       string
	ProviderID     string
	ModelID        string
	RunnerID       string
	CreatedBy      string
	Replayable     bool
	SchemaVersion  SchemaVersion
	Status         Status
	StartedAt      time.Time
}

type AppendEventInput struct {
	Phase         Phase
	EventType     string
	Level         string
	StepIndex     int
	ParentSeq     int64
	RefArtifactID string
	Payload       any
	CreatedAt     time.Time
}

type CreateArtifactInput struct {
	ArtifactID     string
	Kind           ArtifactKind
	MimeType       string
	Encoding       string
	SHA256         string
	RedactionState string
	Body           any
	CreatedAt      time.Time
}

type FinishRunInput struct {
	Status     Status
	FinishedAt time.Time
}

// Recorder captures audit evidence for a task run without exposing storage details.
// StartRun ensures a single audit run record exists for the task and returns it; later
// calls may fill previously blank descriptive metadata, but existing non-empty fields
// stay stable and lifecycle events are still recorded separately via AppendEvent.
type Recorder interface {
	StartRun(ctx context.Context, input StartRunInput) (*Run, error)
	AppendEvent(ctx context.Context, runID string, input AppendEventInput) (*Event, error)
	AttachArtifact(ctx context.Context, runID string, input CreateArtifactInput) (*Artifact, error)
	FinishRun(ctx context.Context, runID string, input FinishRunInput) error
}

type storeRecorder struct {
	store *Store
}

func NewRecorder(store *Store) Recorder {
	return &storeRecorder{store: store}
}

func (r *storeRecorder) StartRun(ctx context.Context, input StartRunInput) (*Run, error) {
	return r.store.CreateRun(ctx, input)
}

func (r *storeRecorder) AppendEvent(ctx context.Context, runID string, input AppendEventInput) (*Event, error) {
	return r.store.AppendEvent(ctx, runID, input)
}

func (r *storeRecorder) AttachArtifact(ctx context.Context, runID string, input CreateArtifactInput) (*Artifact, error) {
	return r.store.CreateArtifact(ctx, runID, input)
}

func (r *storeRecorder) FinishRun(ctx context.Context, runID string, input FinishRunInput) error {
	return r.store.FinishRun(ctx, runID, input.Status, input.FinishedAt)
}
