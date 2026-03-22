package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuditStoreAutoMigrateAndPersistRunGraph(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)

	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	if !db.Migrator().HasTable(&Run{}) {
		t.Fatal("Run table missing after auto-migrate")
	}
	if !db.Migrator().HasTable(&Event{}) {
		t.Fatal("Event table missing after auto-migrate")
	}
	if !db.Migrator().HasTable(&Artifact{}) {
		t.Fatal("Artifact table missing after auto-migrate")
	}

	run := &Run{
		ID:            "run_1",
		TaskID:        "task_1",
		TaskType:      "agent.run",
		Status:        StatusRunning,
		SchemaVersion: SchemaVersionV1,
	}
	if err := db.Create(run).Error; err != nil {
		t.Fatalf("create run error = %v", err)
	}

	artifact := &Artifact{
		ID:       "art_1",
		RunID:    run.ID,
		Kind:     ArtifactKindRequestMessages,
		MimeType: "application/json",
		Encoding: "identity",
		BodyJSON: json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`),
	}
	if err := db.Create(artifact).Error; err != nil {
		t.Fatalf("create artifact error = %v", err)
	}

	events := []*Event{
		{
			RunID:         run.ID,
			TaskID:        run.TaskID,
			Seq:           1,
			Phase:         PhaseRun,
			EventType:     "run.started",
			Level:         "info",
			RefArtifactID: artifact.ID,
			PayloadJSON:   json.RawMessage(`{"status":"running"}`),
		},
		{
			RunID:       run.ID,
			TaskID:      run.TaskID,
			Seq:         2,
			Phase:       PhaseStep,
			EventType:   "step.started",
			Level:       "info",
			StepIndex:   1,
			PayloadJSON: json.RawMessage(`{"step_index":1}`),
		},
	}
	for _, event := range events {
		if err := db.Create(event).Error; err != nil {
			t.Fatalf("create event error = %v", err)
		}
	}

	var gotRun Run
	if err := db.First(&gotRun, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("query run error = %v", err)
	}
	if gotRun.TaskID != run.TaskID {
		t.Fatalf("run task_id = %q, want %q", gotRun.TaskID, run.TaskID)
	}

	var gotArtifact Artifact
	if err := db.First(&gotArtifact, "id = ?", artifact.ID).Error; err != nil {
		t.Fatalf("query artifact error = %v", err)
	}
	if gotArtifact.Kind != ArtifactKindRequestMessages {
		t.Fatalf("artifact kind = %q, want %q", gotArtifact.Kind, ArtifactKindRequestMessages)
	}
	var body map[string]any
	if err := json.Unmarshal(gotArtifact.BodyJSON, &body); err != nil {
		t.Fatalf("unmarshal artifact body error = %v", err)
	}
	if len(body) == 0 {
		t.Fatal("artifact body = empty, want persisted JSON")
	}

	var gotEvents []Event
	if err := db.Where("run_id = ?", run.ID).Order("seq asc").Find(&gotEvents).Error; err != nil {
		t.Fatalf("query events error = %v", err)
	}
	if len(gotEvents) != 2 {
		t.Fatalf("event count = %d, want 2", len(gotEvents))
	}
	if gotEvents[0].Seq != 1 || gotEvents[1].Seq != 2 {
		t.Fatalf("event seqs = [%d %d], want [1 2]", gotEvents[0].Seq, gotEvents[1].Seq)
	}
	if gotEvents[0].RefArtifactID != artifact.ID {
		t.Fatalf("first event ref_artifact_id = %q, want %q", gotEvents[0].RefArtifactID, artifact.ID)
	}
}

func TestAuditRunSchemaDefaultsReplayableToFalse(t *testing.T) {
	db := newTestDB(t)

	if err := db.AutoMigrate(&Run{}, &Event{}, &Artifact{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	defaultValue, ok := tableColumnDefault(t, db, "audit_runs", "replayable")
	if !ok {
		t.Fatal("replayable column default missing")
	}
	normalized := normalizeDefaultValue(defaultValue)
	if normalized != "false" && normalized != "0" {
		t.Fatalf("replayable default = %q, want false/0", defaultValue)
	}
}

func TestAuditEventRejectsDuplicateSequenceWithinRun(t *testing.T) {
	db := newTestDB(t)

	if err := db.AutoMigrate(&Run{}, &Event{}, &Artifact{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	run := &Run{
		ID:            "run_1",
		TaskID:        "task_1",
		TaskType:      "agent.run",
		Status:        StatusQueued,
		SchemaVersion: SchemaVersionV1,
	}
	if err := db.Create(run).Error; err != nil {
		t.Fatalf("create run error = %v", err)
	}
	first := &Event{
		RunID:       run.ID,
		TaskID:      run.TaskID,
		Seq:         1,
		Phase:       PhaseRun,
		EventType:   "run.created",
		Level:       "info",
		PayloadJSON: json.RawMessage(`{"status":"queued"}`),
	}
	if err := db.Create(first).Error; err != nil {
		t.Fatalf("create first event error = %v", err)
	}
	duplicate := &Event{
		RunID:       run.ID,
		TaskID:      run.TaskID,
		Seq:         1,
		Phase:       PhaseRun,
		EventType:   "run.started",
		Level:       "info",
		PayloadJSON: json.RawMessage(`{"status":"running"}`),
	}
	if err := db.Create(duplicate).Error; err == nil {
		t.Fatal("create duplicate event error = nil, want unique constraint failure")
	}
}

func TestAuditRunRejectsDuplicateTaskID(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	first := &Run{
		ID:            "run_1",
		TaskID:        "task_1",
		TaskType:      "agent.run",
		Status:        StatusQueued,
		SchemaVersion: SchemaVersionV1,
	}
	if err := db.Create(first).Error; err != nil {
		t.Fatalf("create first run error = %v", err)
	}

	duplicate := &Run{
		ID:            "run_2",
		TaskID:        "task_1",
		TaskType:      "agent.run",
		Status:        StatusQueued,
		SchemaVersion: SchemaVersionV1,
	}
	if err := db.Create(duplicate).Error; err == nil {
		t.Fatal("create duplicate run error = nil, want unique task_id constraint failure")
	}
	if got, err := store.GetRunByTaskID(context.Background(), "task_1"); err != nil {
		t.Fatalf("GetRunByTaskID() error = %v", err)
	} else if got == nil || got.ID != first.ID {
		t.Fatalf("persisted run = %#v, want first run only", got)
	}
}

func TestRecorderStartRunCreatesRunOncePerTask(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	recorder := NewRecorder(store)
	ctx := context.Background()

	first, err := recorder.StartRun(ctx, StartRunInput{
		RunID:    "run_1",
		TaskID:   "task_1",
		TaskType: "agent.run",
	})
	if err != nil {
		t.Fatalf("StartRun(first) error = %v", err)
	}
	second, err := recorder.StartRun(ctx, StartRunInput{
		RunID:    "run_1",
		TaskID:   "task_1",
		TaskType: "agent.run",
	})
	if err != nil {
		t.Fatalf("StartRun(second) error = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("run ids = [%q %q], want same run", first.ID, second.ID)
	}

	persisted, err := store.GetRunByTaskID(ctx, "task_1")
	if err != nil {
		t.Fatalf("GetRunByTaskID() error = %v", err)
	}
	if persisted == nil {
		t.Fatal("persisted run = nil, want run")
	}
	if persisted.ID != first.ID {
		t.Fatalf("persisted run id = %q, want %q", persisted.ID, first.ID)
	}

	var count int64
	if err := db.Model(&Run{}).Where("task_id = ?", "task_1").Count(&count).Error; err != nil {
		t.Fatalf("count runs error = %v", err)
	}
	if count != 1 {
		t.Fatalf("run count = %d, want 1", count)
	}
}

func TestRecorderStartRunEnrichesExistingRunMetadata(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	recorder := NewRecorder(store)
	ctx := context.Background()

	if _, err := recorder.StartRun(ctx, StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("StartRun(initial) error = %v", err)
	}
	startedAt := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	enriched, err := recorder.StartRun(ctx, StartRunInput{
		RunID:          "run_1",
		TaskID:         "task_1",
		TaskType:       "agent.run",
		ConversationID: "conv_1",
		ProviderID:     "provider_1",
		ModelID:        "model_1",
		RunnerID:       "runner_1",
		CreatedBy:      "user_1",
		Replayable:     true,
		StartedAt:      startedAt,
	})
	if err != nil {
		t.Fatalf("StartRun(enriched) error = %v", err)
	}
	if enriched.ConversationID != "conv_1" || enriched.ProviderID != "provider_1" || enriched.ModelID != "model_1" {
		t.Fatalf("enriched metadata = %#v, want later non-empty fields applied", enriched)
	}
	if enriched.RunnerID != "runner_1" || enriched.CreatedBy != "user_1" || !enriched.Replayable {
		t.Fatalf("enriched runtime metadata = %#v, want later non-empty fields applied", enriched)
	}
	if enriched.StartedAt == nil || !enriched.StartedAt.Equal(startedAt) {
		t.Fatalf("started_at = %v, want %v", enriched.StartedAt, startedAt)
	}

	preserved, err := recorder.StartRun(ctx, StartRunInput{
		RunID:          "run_1",
		TaskID:         "task_1",
		TaskType:       "agent.run",
		ConversationID: "conv_2",
		ModelID:        "model_2",
		RunnerID:       "runner_2",
	})
	if err != nil {
		t.Fatalf("StartRun(preserved) error = %v", err)
	}
	if preserved.ConversationID != "conv_1" || preserved.ModelID != "model_1" || preserved.RunnerID != "runner_1" {
		t.Fatalf("preserved metadata = %#v, want first non-empty values kept", preserved)
	}
}

func TestRecorderStartRunRejectsRunIDDriftForExistingTask(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	recorder := NewRecorder(store)
	ctx := context.Background()

	if _, err := recorder.StartRun(ctx, StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("StartRun(initial) error = %v", err)
	}
	if _, err := recorder.StartRun(ctx, StartRunInput{RunID: "run_2", TaskID: "task_1", TaskType: "agent.run"}); err == nil {
		t.Fatal("StartRun(drift) error = nil, want task/run mismatch error")
	}
}

func TestRecorderAppendsEventsInSequenceForSerializedWrites(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	recorder := NewRecorder(store)
	if _, err := recorder.StartRun(context.Background(), StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	first, err := recorder.AppendEvent(context.Background(), "run_1", AppendEventInput{EventType: "run.started"})
	if err != nil {
		t.Fatalf("AppendEvent(first) error = %v", err)
	}
	second, err := recorder.AppendEvent(context.Background(), "run_1", AppendEventInput{EventType: "step.started"})
	if err != nil {
		t.Fatalf("AppendEvent(second) error = %v", err)
	}
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("seqs = [%d %d], want [1 2]", first.Seq, second.Seq)
	}
	if first.TaskID != "task_1" || second.TaskID != "task_1" {
		t.Fatalf("task ids = [%q %q], want both task_1", first.TaskID, second.TaskID)
	}
}

func TestStoreAppendEventRetriesTransientUniqueConflict(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	ctx := context.Background()
	if _, err := store.CreateRun(ctx, StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	callbackName := "test:audit:event_unique_conflict"
	injected := false
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if injected || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "audit_events" {
			return
		}
		injected = true
		tx.AddError(errors.New("UNIQUE constraint failed: audit_events.run_id, audit_events.seq"))
	}); err != nil {
		t.Fatalf("register callback error = %v", err)
	}
	defer func() {
		if err := db.Callback().Create().Remove(callbackName); err != nil {
			t.Fatalf("remove callback error = %v", err)
		}
	}()

	event, err := store.AppendEvent(ctx, "run_1", AppendEventInput{EventType: "run.started"})
	if err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if !injected {
		t.Fatal("unique conflict was not injected")
	}
	if event.Seq != 1 {
		t.Fatalf("event seq = %d, want 1 after retry", event.Seq)
	}
}

func TestStoreAppendEventRejectsInvalidRawJSONPayload(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	ctx := context.Background()
	if _, err := store.CreateRun(ctx, StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if _, err := store.AppendEvent(ctx, "run_1", AppendEventInput{EventType: "run.started", Payload: json.RawMessage(`{"status":`)}); err == nil {
		t.Fatal("AppendEvent() error = nil, want invalid raw JSON rejection")
	}
}

func TestStoreAppendEventNormalizesRawJSONPayload(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	ctx := context.Background()
	if _, err := store.CreateRun(ctx, StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	event, err := store.AppendEvent(ctx, "run_1", AppendEventInput{EventType: "run.started", Payload: json.RawMessage("{\n  \"status\" : \"running\"\n}")})
	if err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if string(event.PayloadJSON) != `{"status":"running"}` {
		t.Fatalf("payload = %s, want compact JSON", string(event.PayloadJSON))
	}
}

func TestStoreFinishRunIsIdempotentForSameTerminalStatus(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	ctx := context.Background()
	if _, err := store.CreateRun(ctx, StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	firstFinishedAt := time.Date(2026, time.March, 21, 15, 0, 0, 0, time.UTC)
	if err := store.FinishRun(ctx, "run_1", StatusSucceeded, firstFinishedAt); err != nil {
		t.Fatalf("FinishRun(first) error = %v", err)
	}

	secondFinishedAt := firstFinishedAt.Add(5 * time.Minute)
	if err := store.FinishRun(ctx, "run_1", StatusSucceeded, secondFinishedAt); err != nil {
		t.Fatalf("FinishRun(second) error = %v", err)
	}

	run, err := store.GetRun(ctx, "run_1")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.Status != StatusSucceeded {
		t.Fatalf("run status = %q, want %q", run.Status, StatusSucceeded)
	}
	if run.FinishedAt == nil || !run.FinishedAt.Equal(firstFinishedAt) {
		t.Fatalf("finished_at = %v, want original %v", run.FinishedAt, firstFinishedAt)
	}
}

func TestStoreFinishRunRejectsDifferentTerminalOutcome(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	ctx := context.Background()
	if _, err := store.CreateRun(ctx, StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	finishedAt := time.Date(2026, time.March, 21, 16, 0, 0, 0, time.UTC)
	if err := store.FinishRun(ctx, "run_1", StatusSucceeded, finishedAt); err != nil {
		t.Fatalf("FinishRun(first) error = %v", err)
	}

	err := store.FinishRun(ctx, "run_1", StatusFailed, finishedAt.Add(time.Minute))
	if err == nil {
		t.Fatal("FinishRun(conflict) error = nil, want terminal outcome conflict")
	}

	run, getErr := store.GetRun(ctx, "run_1")
	if getErr != nil {
		t.Fatalf("GetRun() error = %v", getErr)
	}
	if run.Status != StatusSucceeded {
		t.Fatalf("run status = %q, want preserved %q", run.Status, StatusSucceeded)
	}
	if run.FinishedAt == nil || !run.FinishedAt.Equal(finishedAt) {
		t.Fatalf("finished_at = %v, want preserved %v", run.FinishedAt, finishedAt)
	}
}

func TestRecorderAttachArtifactPersistsJSONBody(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	recorder := NewRecorder(store)
	if _, err := recorder.StartRun(context.Background(), StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	artifact, err := recorder.AttachArtifact(context.Background(), "run_1", CreateArtifactInput{
		Kind:     ArtifactKindRequestMessages,
		MimeType: "application/json",
		Body: map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "hello"}},
		},
	})
	if err != nil {
		t.Fatalf("AttachArtifact() error = %v", err)
	}
	if artifact.ID == "" {
		t.Fatal("artifact id = empty, want generated id")
	}

	var persisted Artifact
	if err := db.First(&persisted, "id = ?", artifact.ID).Error; err != nil {
		t.Fatalf("query artifact error = %v", err)
	}
	if persisted.RunID != "run_1" {
		t.Fatalf("artifact run id = %q, want run_1", persisted.RunID)
	}
	var body map[string]any
	if err := json.Unmarshal(persisted.BodyJSON, &body); err != nil {
		t.Fatalf("unmarshal artifact body error = %v", err)
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("artifact messages = %#v, want one persisted message", body["messages"])
	}
}

func TestStoreCreateArtifactRejectsInvalidRawJSONBody(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	ctx := context.Background()
	if _, err := store.CreateRun(ctx, StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if _, err := store.CreateArtifact(ctx, "run_1", CreateArtifactInput{Kind: ArtifactKindRequestMessages, MimeType: "application/json", Body: json.RawMessage(`{"messages":`)}); err == nil {
		t.Fatal("CreateArtifact() error = nil, want invalid raw JSON rejection")
	}
}

func TestRecorderFinishRunSetsTerminalStatusAndTimestamp(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	recorder := NewRecorder(store)
	ctx := context.Background()
	if _, err := recorder.StartRun(ctx, StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	finishedAt := time.Date(2026, time.March, 21, 14, 30, 0, 0, time.UTC)
	if err := recorder.FinishRun(ctx, "run_1", FinishRunInput{Status: StatusSucceeded, FinishedAt: finishedAt}); err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}

	run, err := store.GetRun(ctx, "run_1")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.Status != StatusSucceeded {
		t.Fatalf("run status = %q, want %q", run.Status, StatusSucceeded)
	}
	if run.FinishedAt == nil || !run.FinishedAt.Equal(finishedAt) {
		t.Fatalf("finished_at = %v, want %v", run.FinishedAt, finishedAt)
	}
}

func TestNoopRecorderSynthesizesPlaceholderIDsAndSeq(t *testing.T) {
	recorder := NewNoopRecorder()
	ctx := context.Background()

	run, err := recorder.StartRun(ctx, StartRunInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if run == nil {
		t.Fatal("StartRun() run = nil, want non-nil")
	}
	if run.ID == "" || run.TaskID == "" {
		t.Fatalf("StartRun() placeholders = %#v, want synthesized ids", run)
	}

	firstEvent, err := recorder.AppendEvent(ctx, "", AppendEventInput{EventType: "run.started"})
	if err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if firstEvent == nil {
		t.Fatal("AppendEvent() event = nil, want non-nil")
	}
	secondEvent, err := recorder.AppendEvent(ctx, "", AppendEventInput{EventType: "step.started"})
	if err != nil {
		t.Fatalf("AppendEvent(second) error = %v", err)
	}
	if firstEvent.RunID == "" || secondEvent.RunID == "" {
		t.Fatalf("AppendEvent() run ids = [%q %q], want synthesized placeholders", firstEvent.RunID, secondEvent.RunID)
	}
	if firstEvent.Seq != 1 || secondEvent.Seq != 2 {
		t.Fatalf("AppendEvent() seqs = [%d %d], want [1 2]", firstEvent.Seq, secondEvent.Seq)
	}

	artifact, err := recorder.AttachArtifact(ctx, "", CreateArtifactInput{Kind: ArtifactKindRequestMessages, MimeType: "application/json"})
	if err != nil {
		t.Fatalf("AttachArtifact() error = %v", err)
	}
	if artifact == nil {
		t.Fatal("AttachArtifact() artifact = nil, want non-nil")
	}
	if artifact.ID == "" || artifact.RunID == "" {
		t.Fatalf("AttachArtifact() placeholders = %#v, want synthesized ids", artifact)
	}

	if err := recorder.FinishRun(ctx, "", FinishRunInput{Status: StatusSucceeded}); err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}

type pragmaColumnInfo struct {
	Name         string         `gorm:"column:name"`
	DefaultValue sql.NullString `gorm:"column:dflt_value"`
}

func tableColumnDefault(t *testing.T, db *gorm.DB, tableName string, columnName string) (string, bool) {
	t.Helper()

	var columns []pragmaColumnInfo
	if err := db.Raw("PRAGMA table_info(" + tableName + ")").Scan(&columns).Error; err != nil {
		t.Fatalf("PRAGMA table_info(%s) error = %v", tableName, err)
	}
	for _, column := range columns {
		if column.Name == columnName && column.DefaultValue.Valid {
			return column.DefaultValue.String, true
		}
	}
	return "", false
}

func normalizeDefaultValue(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, "'\"")
	return strings.ToLower(trimmed)
}
