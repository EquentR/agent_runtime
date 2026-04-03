package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrRunNotFound = errors.New("audit run not found")
var ErrRunAlreadyFinished = errors.New("audit run already finished")

const appendEventRetryLimit = 3

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) AutoMigrate() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store db cannot be nil")
	}
	return s.db.AutoMigrate(&Run{}, &Event{}, &Artifact{})
}

func (s *Store) GetRun(ctx context.Context, runID string) (*Run, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}

	var run Run
	err := s.db.WithContext(ctx).First(&run, "id = ?", strings.TrimSpace(runID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrRunNotFound, strings.TrimSpace(runID))
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) GetRunByTaskID(ctx context.Context, taskID string) (*Run, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}

	var run Run
	err := s.db.WithContext(ctx).First(&run, "task_id = ?", strings.TrimSpace(taskID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) GetLatestRunByConversationID(ctx context.Context, conversationID string) (*Run, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}

	var run Run
	err := s.db.WithContext(ctx).
		Where("conversation_id = ?", strings.TrimSpace(conversationID)).
		Order("created_at desc").
		Order("id desc").
		Take(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRunsByConversationID 返回指定 conversation 下的所有审计运行记录，按创建时间升序排列。
func (s *Store) ListRunsByConversationID(ctx context.Context, conversationID string) ([]Run, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil, nil
	}

	var runs []Run
	err := s.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("created_at asc").
		Order("id asc").
		Find(&runs).Error
	if err != nil {
		return nil, err
	}
	return runs, nil
}

// ListEventsByConversationID 返回指定 conversation 下所有审计运行的事件，跨 run 聚合并按时间和序列升序排列。
func (s *Store) ListEventsByConversationID(ctx context.Context, conversationID string) ([]Event, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil, nil
	}

	var runIDs []string
	if err := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("conversation_id = ?", conversationID).
		Order("created_at asc, id asc").
		Pluck("id", &runIDs).Error; err != nil {
		return nil, err
	}
	if len(runIDs) == 0 {
		return nil, nil
	}

	var events []Event
	err := s.db.WithContext(ctx).
		Where("run_id IN ?", runIDs).
		Order("created_at asc").
		Order("seq asc").
		Order("id asc").
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (s *Store) ListEvents(ctx context.Context, runID string) ([]Event, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("run id cannot be empty")
	}

	var events []Event
	err := s.db.WithContext(ctx).Where("run_id = ?", runID).Order("seq asc").Order("id asc").Find(&events).Error
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (s *Store) ListArtifactsByIDs(ctx context.Context, runID string, artifactIDs []string) ([]Artifact, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("run id cannot be empty")
	}

	normalizedIDs := normalizeArtifactIDs(artifactIDs)
	if len(normalizedIDs) == 0 {
		return nil, nil
	}

	var artifacts []Artifact
	err := s.db.WithContext(ctx).
		Where("run_id = ? AND id IN ?", runID, normalizedIDs).
		Order("created_at asc").
		Order("id asc").
		Find(&artifacts).Error
	if err != nil {
		return nil, err
	}
	return artifacts, nil
}

func (s *Store) ListArtifacts(ctx context.Context, runID string) ([]Artifact, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("run id cannot be empty")
	}

	var artifacts []Artifact
	err := s.db.WithContext(ctx).
		Where("run_id = ?", runID).
		Order("created_at asc").
		Order("id asc").
		Find(&artifacts).Error
	if err != nil {
		return nil, err
	}
	return artifacts, nil
}

func (s *Store) CreateRun(ctx context.Context, input StartRunInput) (*Run, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}

	requestedRunID := strings.TrimSpace(input.RunID)
	taskID := strings.TrimSpace(input.TaskID)
	taskType := strings.TrimSpace(input.TaskType)
	if taskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}
	if taskType == "" {
		return nil, fmt.Errorf("task type cannot be empty")
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

	run := &Run{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existingByTask, err := getRunByTaskIDTx(tx, taskID)
		if err != nil {
			return err
		}
		if existingByTask != nil {
			if requestedRunID != "" && existingByTask.ID != requestedRunID {
				return fmt.Errorf("task %q already linked to audit run %q", taskID, existingByTask.ID)
			}
			if existingByTask.TaskType != taskType {
				return fmt.Errorf("task %q already linked to task type %q", taskID, existingByTask.TaskType)
			}
			if applyStartRunMetadata(existingByTask, input) {
				if err := tx.Save(existingByTask).Error; err != nil {
					return err
				}
			}
			*run = *existingByTask
			return nil
		}

		runID := requestedRunID
		if runID == "" {
			runID = newRunID()
		} else {
			existingByID, err := loadRunTx(tx, runID)
			if err != nil && !errors.Is(err, ErrRunNotFound) {
				return err
			}
			if existingByID != nil {
				if existingByID.TaskID != taskID {
					return fmt.Errorf("audit run %q already linked to task %q", runID, existingByID.TaskID)
				}
				if existingByID.TaskType != taskType {
					return fmt.Errorf("audit run %q already linked to task type %q", runID, existingByID.TaskType)
				}
				if applyStartRunMetadata(existingByID, input) {
					if err := tx.Save(existingByID).Error; err != nil {
						return err
					}
				}
				*run = *existingByID
				return nil
			}
		}

		created := Run{
			ID:             runID,
			TaskID:         taskID,
			ConversationID: strings.TrimSpace(input.ConversationID),
			TaskType:       taskType,
			ProviderID:     strings.TrimSpace(input.ProviderID),
			ModelID:        strings.TrimSpace(input.ModelID),
			RunnerID:       strings.TrimSpace(input.RunnerID),
			Status:         status,
			CreatedBy:      strings.TrimSpace(input.CreatedBy),
			Replayable:     input.Replayable,
			SchemaVersion:  schemaVersion,
			StartedAt:      startedAt,
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		*run = created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Store) AppendEvent(ctx context.Context, runID string, input AppendEventInput) (*Event, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}
	if strings.TrimSpace(input.EventType) == "" {
		return nil, fmt.Errorf("event type cannot be empty")
	}

	var lastErr error
	for attempt := 0; attempt < appendEventRetryLimit; attempt++ {
		event := &Event{}
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			run, err := loadRunTx(tx, strings.TrimSpace(runID))
			if err != nil {
				return err
			}
			if err := validateArtifactLinkTx(tx, run.ID, strings.TrimSpace(input.RefArtifactID)); err != nil {
				return err
			}

			var maxSeq int64
			// Event sequences still assume a mostly serialized task manager write path.
			// MAX(seq)+1 keeps ordering simple, and a small retry loop handles transient
			// unique(run_id, seq) conflicts if another writer wins the same sequence first.
			if err := tx.Model(&Event{}).Where("run_id = ?", run.ID).Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error; err != nil {
				return err
			}

			payloadJSON, err := marshalJSON(input.Payload, true)
			if err != nil {
				return err
			}

			createdAt := input.CreatedAt.UTC()
			if input.CreatedAt.IsZero() {
				createdAt = time.Now().UTC()
			}

			created := Event{
				RunID:         run.ID,
				TaskID:        run.TaskID,
				Seq:           maxSeq + 1,
				Phase:         normalizePhase(input.Phase, input.EventType),
				EventType:     strings.TrimSpace(input.EventType),
				Level:         normalizeLevel(input.Level),
				StepIndex:     input.StepIndex,
				ParentSeq:     input.ParentSeq,
				RefArtifactID: strings.TrimSpace(input.RefArtifactID),
				PayloadJSON:   payloadJSON,
				CreatedAt:     createdAt,
			}
			if err := tx.Create(&created).Error; err != nil {
				return err
			}
			*event = created
			return nil
		})
		if err == nil {
			return event, nil
		}
		if !isUniqueConstraintError(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func (s *Store) CreateArtifact(ctx context.Context, runID string, input CreateArtifactInput) (*Artifact, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}
	if input.Kind == "" {
		return nil, fmt.Errorf("artifact kind cannot be empty")
	}
	if strings.TrimSpace(input.MimeType) == "" {
		return nil, fmt.Errorf("artifact mime type cannot be empty")
	}

	artifactID := strings.TrimSpace(input.ArtifactID)
	if artifactID == "" {
		artifactID = newArtifactID()
	}

	artifact := &Artifact{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := loadRunTx(tx, strings.TrimSpace(runID))
		if err != nil {
			return err
		}

		bodyJSON, err := marshalJSON(input.Body, false)
		if err != nil {
			return err
		}

		createdAt := input.CreatedAt.UTC()
		if input.CreatedAt.IsZero() {
			createdAt = time.Now().UTC()
		}

		created := Artifact{
			ID:             artifactID,
			RunID:          run.ID,
			Kind:           input.Kind,
			MimeType:       strings.TrimSpace(input.MimeType),
			Encoding:       normalizeEncoding(input.Encoding),
			SizeBytes:      int64(len(bodyJSON)),
			SHA256:         strings.TrimSpace(input.SHA256),
			RedactionState: normalizeRedactionState(input.RedactionState),
			BodyJSON:       bodyJSON,
			CreatedAt:      createdAt,
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		*artifact = created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return artifact, nil
}

func (s *Store) FinishRun(ctx context.Context, runID string, status Status, finishedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store db cannot be nil")
	}
	if !status.IsTerminal() {
		return fmt.Errorf("status %q is not terminal", status)
	}
	finished := finishedAt.UTC()
	if finishedAt.IsZero() {
		finished = time.Now().UTC()
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := loadRunTx(tx, strings.TrimSpace(runID))
		if err != nil {
			return err
		}
		if run.Status.IsTerminal() {
			if run.Status == status {
				return nil
			}
			return fmt.Errorf("%w: run %q already %q", ErrRunAlreadyFinished, run.ID, run.Status)
		}
		run.Status = status
		run.FinishedAt = &finished
		return tx.Save(run).Error
	})
}

func getRunByTaskIDTx(tx *gorm.DB, taskID string) (*Run, error) {
	var run Run
	err := tx.First(&run, "task_id = ?", taskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func loadRunTx(tx *gorm.DB, runID string) (*Run, error) {
	var run Run
	err := tx.First(&run, "id = ?", runID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrRunNotFound, runID)
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func validateArtifactLinkTx(tx *gorm.DB, runID string, artifactID string) error {
	if artifactID == "" {
		return nil
	}

	var artifact Artifact
	err := tx.First(&artifact, "id = ?", artifactID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("artifact %q not found", artifactID)
	}
	if err != nil {
		return err
	}
	if artifact.RunID != runID {
		return fmt.Errorf("artifact %q belongs to run %q, want %q", artifactID, artifact.RunID, runID)
	}
	return nil
}

func applyStartRunMetadata(run *Run, input StartRunInput) bool {
	changed := false
	if shouldPromoteRunStatus(run.Status, input.Status) {
		run.Status = input.Status
		changed = true
	}
	if run.ConversationID == "" && strings.TrimSpace(input.ConversationID) != "" {
		run.ConversationID = strings.TrimSpace(input.ConversationID)
		changed = true
	}
	if run.ProviderID == "" && strings.TrimSpace(input.ProviderID) != "" {
		run.ProviderID = strings.TrimSpace(input.ProviderID)
		changed = true
	}
	if run.ModelID == "" && strings.TrimSpace(input.ModelID) != "" {
		run.ModelID = strings.TrimSpace(input.ModelID)
		changed = true
	}
	if run.RunnerID == "" && strings.TrimSpace(input.RunnerID) != "" {
		run.RunnerID = strings.TrimSpace(input.RunnerID)
		changed = true
	}
	if run.CreatedBy == "" && strings.TrimSpace(input.CreatedBy) != "" {
		run.CreatedBy = strings.TrimSpace(input.CreatedBy)
		changed = true
	}
	if !run.Replayable && input.Replayable {
		run.Replayable = true
		changed = true
	}
	if run.StartedAt == nil && !input.StartedAt.IsZero() {
		started := input.StartedAt.UTC()
		run.StartedAt = &started
		changed = true
	}
	return changed
}

func shouldPromoteRunStatus(current Status, next Status) bool {
	switch current {
	case StatusQueued:
		return next == StatusRunning || next == StatusWaiting
	case StatusWaiting:
		return next == StatusRunning
	case StatusRunning:
		return next == StatusWaiting
	default:
		return false
	}
}

func marshalJSON(value any, objectDefault bool) (json.RawMessage, error) {
	switch v := value.(type) {
	case nil:
		if objectDefault {
			return json.RawMessage("{}"), nil
		}
		return json.RawMessage("null"), nil
	case json.RawMessage:
		return normalizeRawJSON(v, objectDefault)
	case []byte:
		return normalizeRawJSON(v, objectDefault)
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(raw), nil
	}
}

func normalizeRawJSON(raw []byte, objectDefault bool) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		if objectDefault {
			return json.RawMessage("{}"), nil
		}
		return json.RawMessage("null"), nil
	}
	if !json.Valid(trimmed) {
		return nil, fmt.Errorf("invalid json")
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, trimmed); err != nil {
		return nil, err
	}
	return json.RawMessage(compacted.Bytes()), nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") || strings.Contains(message, "duplicate key") || strings.Contains(message, "duplicated key")
}

func normalizePhase(phase Phase, eventType string) Phase {
	if phase != "" {
		return phase
	}
	switch strings.TrimSpace(strings.SplitN(eventType, ".", 2)[0]) {
	case "conversation":
		return PhaseConversation
	case "step":
		return PhaseStep
	case "prompt":
		return PhasePrompt
	case "request":
		return PhaseRequest
	case "model":
		return PhaseModel
	case "tool":
		return PhaseTool
	case "replay":
		return PhaseReplay
	default:
		return PhaseRun
	}
}

func normalizeLevel(level string) string {
	if strings.TrimSpace(level) == "" {
		return "info"
	}
	return strings.TrimSpace(level)
}

func normalizeEncoding(encoding string) string {
	if strings.TrimSpace(encoding) == "" {
		return "identity"
	}
	return strings.TrimSpace(encoding)
}

func normalizeRedactionState(state string) string {
	if strings.TrimSpace(state) == "" {
		return "raw"
	}
	return strings.TrimSpace(state)
}

func newRunID() string {
	return "run_" + uuid.NewString()
}

func newArtifactID() string {
	return "art_" + uuid.NewString()
}

func normalizeArtifactIDs(artifactIDs []string) []string {
	if len(artifactIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(artifactIDs))
	normalized := make([]string, 0, len(artifactIDs))
	for _, artifactID := range artifactIDs {
		artifactID = strings.TrimSpace(artifactID)
		if artifactID == "" {
			continue
		}
		if _, ok := seen[artifactID]; ok {
			continue
		}
		seen[artifactID] = struct{}{}
		normalized = append(normalized, artifactID)
	}
	return normalized
}
