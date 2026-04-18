package audit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildReplayBundleReturnsOrderedTimeline(t *testing.T) {
	store := seededReplayStore(t)

	bundle, err := BuildReplayBundle(context.Background(), store, "run_1")
	if err != nil {
		t.Fatalf("BuildReplayBundle() error = %v", err)
	}
	if bundle.Run.ID != "run_1" {
		t.Fatalf("bundle.Run.ID = %q, want run_1", bundle.Run.ID)
	}
	if bundle.Run.TaskID != "task_1" {
		t.Fatalf("bundle.Run.TaskID = %q, want task_1", bundle.Run.TaskID)
	}
	if len(bundle.Timeline) != 3 {
		t.Fatalf("len(bundle.Timeline) = %d, want 3", len(bundle.Timeline))
	}
	if bundle.Timeline[0].Seq != 1 || bundle.Timeline[1].Seq != 2 || bundle.Timeline[2].Seq != 3 {
		t.Fatalf("timeline seqs = [%d %d %d], want [1 2 3]", bundle.Timeline[0].Seq, bundle.Timeline[1].Seq, bundle.Timeline[2].Seq)
	}
	if bundle.Timeline[0].DisplayName != "运行开始" {
		t.Fatalf("timeline[0].DisplayName = %q, want 运行开始", bundle.Timeline[0].DisplayName)
	}
	if bundle.Timeline[1].DisplayName != "构建 LLM 请求" {
		t.Fatalf("timeline[1].DisplayName = %q, want 构建 LLM 请求", bundle.Timeline[1].DisplayName)
	}
	if bundle.Timeline[2].DisplayName != "工具调用完成" {
		t.Fatalf("timeline[2].DisplayName = %q, want 工具调用完成", bundle.Timeline[2].DisplayName)
	}
	if bundle.Timeline[1].Artifact == nil || bundle.Timeline[1].Artifact.ID != "art_request" {
		t.Fatalf("timeline[1].artifact = %#v, want art_request summary", bundle.Timeline[1].Artifact)
	}
	if bundle.Timeline[2].Artifact == nil || bundle.Timeline[2].Artifact.ID != "art_tool" {
		t.Fatalf("timeline[2].artifact = %#v, want art_tool summary", bundle.Timeline[2].Artifact)
	}
	if got := string(bundle.Timeline[1].Payload); got != `{"message_count":2}` {
		t.Fatalf("timeline[1].payload = %s, want compact request payload", got)
	}
	if len(bundle.Artifacts) != 3 {
		t.Fatalf("len(bundle.Artifacts) = %d, want 3 artifacts including retained diagnostics", len(bundle.Artifacts))
	}
	if bundle.Artifacts[0].ID != "art_request" || bundle.Artifacts[1].ID != "art_tool" || bundle.Artifacts[2].ID != "art_unused" {
		t.Fatalf("artifact ids = [%q %q %q], want [art_request art_tool art_unused]", bundle.Artifacts[0].ID, bundle.Artifacts[1].ID, bundle.Artifacts[2].ID)
	}
	if got := string(bundle.Artifacts[0].Body); got != `{"messages":[{"role":"user","content":"hello"}]}` {
		t.Fatalf("art_request body = %s, want inlined request messages", got)
	}
	if got := string(bundle.Artifacts[1].Body); got != `{"tool_call_id":"call_1","tool_name":"lookup_weather","output":"sunny"}` {
		t.Fatalf("art_tool body = %s, want inlined tool output", got)
	}
	if got := string(bundle.Artifacts[2].Body); got != `{"error":"unused"}` {
		t.Fatalf("art_unused body = %s, want retained error snapshot", got)
	}
}

func TestBuildReplayBundleIncludesMemoryEventDisplayNames(t *testing.T) {
	store := newReplayStore(t)
	now := time.Date(2026, time.March, 21, 18, 0, 0, 0, time.UTC)

	run := &Run{
		ID:            "run_memory_events",
		TaskID:        "task_memory_events",
		TaskType:      "agent.run",
		Status:        StatusSucceeded,
		Replayable:    true,
		SchemaVersion: SchemaVersionV1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.db.Create(run).Error; err != nil {
		t.Fatalf("create run error = %v", err)
	}

	events := []*Event{
		{
			RunID:       run.ID,
			TaskID:      run.TaskID,
			Seq:         1,
			Phase:       PhaseRequest,
			EventType:   "memory.compressed",
			Level:       "info",
			PayloadJSON: json.RawMessage(`{"tokens_before":120,"tokens_after":40}`),
			CreatedAt:   now.Add(time.Second),
		},
		{
			RunID:       run.ID,
			TaskID:      run.TaskID,
			Seq:         2,
			Phase:       PhaseRequest,
			EventType:   "memory.context_state",
			Level:       "info",
			PayloadJSON: json.RawMessage(`{"has_summary":true,"total_tokens":40}`),
			CreatedAt:   now.Add(2 * time.Second),
		},
	}
	for _, event := range events {
		if err := store.db.Create(event).Error; err != nil {
			t.Fatalf("create event seq %d error = %v", event.Seq, err)
		}
	}

	bundle, err := BuildReplayBundle(context.Background(), store, run.ID)
	if err != nil {
		t.Fatalf("BuildReplayBundle() error = %v", err)
	}
	if len(bundle.Timeline) != 2 {
		t.Fatalf("len(bundle.Timeline) = %d, want 2", len(bundle.Timeline))
	}
	if bundle.Timeline[0].DisplayName != "内存已压缩" {
		t.Fatalf("timeline[0].DisplayName = %q, want 内存已压缩", bundle.Timeline[0].DisplayName)
	}
	if bundle.Timeline[1].DisplayName != "内存上下文状态" {
		t.Fatalf("timeline[1].DisplayName = %q, want 内存上下文状态", bundle.Timeline[1].DisplayName)
	}
}

func TestBuildReplayBundleOmitsUnsupportedArtifactBodies(t *testing.T) {
	store := newReplayStore(t)
	now := time.Date(2026, time.March, 21, 14, 0, 0, 0, time.UTC)

	run := &Run{
		ID:            "run_opaque",
		TaskID:        "task_opaque",
		TaskType:      "agent.run",
		Status:        StatusSucceeded,
		Replayable:    true,
		SchemaVersion: SchemaVersionV1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.db.Create(run).Error; err != nil {
		t.Fatalf("create run error = %v", err)
	}
	artifact := &Artifact{
		ID:             "art_blob",
		RunID:          run.ID,
		Kind:           ArtifactKind("opaque_blob"),
		MimeType:       "application/octet-stream",
		Encoding:       "base64",
		SizeBytes:      int64(len(`{"raw":"AA=="}`)),
		RedactionState: "raw",
		BodyJSON:       json.RawMessage(`{"raw":"AA=="}`),
		CreatedAt:      now.Add(time.Second),
	}
	if err := store.db.Create(artifact).Error; err != nil {
		t.Fatalf("create artifact error = %v", err)
	}
	event := &Event{
		RunID:         run.ID,
		TaskID:        run.TaskID,
		Seq:           1,
		Phase:         PhaseTool,
		EventType:     "tool.finished",
		Level:         "info",
		RefArtifactID: artifact.ID,
		PayloadJSON:   json.RawMessage(`{"tool_name":"blob_writer"}`),
		CreatedAt:     now.Add(2 * time.Second),
	}
	if err := store.db.Create(event).Error; err != nil {
		t.Fatalf("create event error = %v", err)
	}

	bundle, err := BuildReplayBundle(context.Background(), store, run.ID)
	if err != nil {
		t.Fatalf("BuildReplayBundle() error = %v", err)
	}
	if len(bundle.Artifacts) != 1 {
		t.Fatalf("len(bundle.Artifacts) = %d, want 1", len(bundle.Artifacts))
	}
	if bundle.Timeline[0].Artifact == nil || bundle.Timeline[0].Artifact.ID != artifact.ID {
		t.Fatalf("timeline[0].artifact = %#v, want resolved artifact summary", bundle.Timeline[0].Artifact)
	}
	if bundle.Artifacts[0].Body != nil {
		t.Fatalf("bundle.Artifacts[0].Body = %s, want omitted body for unsupported artifact", string(bundle.Artifacts[0].Body))
	}
}

func TestBuildReplayBundlePreservesUnreferencedErrorSnapshot(t *testing.T) {
	store := newReplayStore(t)
	now := time.Date(2026, time.March, 21, 15, 0, 0, 0, time.UTC)

	run := &Run{
		ID:            "run_failed",
		TaskID:        "task_failed",
		TaskType:      "agent.run",
		Status:        StatusFailed,
		Replayable:    true,
		SchemaVersion: SchemaVersionV1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.db.Create(run).Error; err != nil {
		t.Fatalf("create run error = %v", err)
	}
	requestArtifact := &Artifact{
		ID:             "art_request_failed",
		RunID:          run.ID,
		Kind:           ArtifactKindRequestMessages,
		MimeType:       "application/json",
		Encoding:       "utf-8",
		SizeBytes:      int64(len(`{"messages":[{"role":"user","content":"hello"}]}`)),
		RedactionState: "raw",
		BodyJSON:       json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`),
		CreatedAt:      now.Add(time.Second),
	}
	errorArtifact := &Artifact{
		ID:             "art_error_failed",
		RunID:          run.ID,
		Kind:           ArtifactKindErrorSnapshot,
		MimeType:       "application/json",
		Encoding:       "utf-8",
		SizeBytes:      int64(len(`{"error":"tool crashed"}`)),
		RedactionState: "raw",
		BodyJSON:       json.RawMessage(`{"error":"tool crashed"}`),
		CreatedAt:      now.Add(2 * time.Second),
	}
	for _, artifact := range []*Artifact{requestArtifact, errorArtifact} {
		if err := store.db.Create(artifact).Error; err != nil {
			t.Fatalf("create artifact %s error = %v", artifact.ID, err)
		}
	}
	event := &Event{
		RunID:         run.ID,
		TaskID:        run.TaskID,
		Seq:           1,
		Phase:         PhaseRequest,
		EventType:     "request.built",
		Level:         "info",
		RefArtifactID: requestArtifact.ID,
		PayloadJSON:   json.RawMessage(`{"message_count":1}`),
		CreatedAt:     now.Add(3 * time.Second),
	}
	if err := store.db.Create(event).Error; err != nil {
		t.Fatalf("create event error = %v", err)
	}

	bundle, err := BuildReplayBundle(context.Background(), store, run.ID)
	if err != nil {
		t.Fatalf("BuildReplayBundle() error = %v", err)
	}
	if len(bundle.Artifacts) != 2 {
		t.Fatalf("len(bundle.Artifacts) = %d, want 2 with unreferenced error snapshot retained", len(bundle.Artifacts))
	}
	if bundle.Artifacts[0].ID != requestArtifact.ID || bundle.Artifacts[1].ID != errorArtifact.ID {
		t.Fatalf("artifact ids = [%q %q], want [%q %q]", bundle.Artifacts[0].ID, bundle.Artifacts[1].ID, requestArtifact.ID, errorArtifact.ID)
	}
	if got := string(bundle.Artifacts[1].Body); got != `{"error":"tool crashed"}` {
		t.Fatalf("error snapshot body = %s, want preserved JSON body", got)
	}
}

func TestBuildReplayBundleRejectsUnsupportedRunState(t *testing.T) {
	tests := []struct {
		name          string
		run           Run
		wantSubstring string
	}{
		{
			name: "non replayable run",
			run: Run{
				ID:            "run_non_replayable",
				TaskID:        "task_non_replayable",
				TaskType:      "agent.run",
				Status:        StatusSucceeded,
				Replayable:    false,
				SchemaVersion: SchemaVersionV1,
			},
			wantSubstring: "not replayable",
		},
		{
			name: "unsupported schema version",
			run: Run{
				ID:            "run_schema_v2",
				TaskID:        "task_schema_v2",
				TaskType:      "agent.run",
				Status:        StatusSucceeded,
				Replayable:    true,
				SchemaVersion: SchemaVersion("v2"),
			},
			wantSubstring: "unsupported schema version",
		},
		{
			name: "nonterminal run",
			run: Run{
				ID:            "run_running",
				TaskID:        "task_running",
				TaskType:      "agent.run",
				Status:        StatusRunning,
				Replayable:    true,
				SchemaVersion: SchemaVersionV1,
			},
			wantSubstring: "not finished",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newReplayStore(t)
			now := time.Date(2026, time.March, 21, 16, 0, 0, 0, time.UTC)
			run := tt.run
			run.CreatedAt = now
			run.UpdatedAt = now
			if err := store.db.Create(&run).Error; err != nil {
				t.Fatalf("create run error = %v", err)
			}

			_, err := BuildReplayBundle(context.Background(), store, run.ID)
			if err == nil {
				t.Fatalf("BuildReplayBundle() error = nil, want %q", tt.wantSubstring)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.wantSubstring) {
				t.Fatalf("BuildReplayBundle() error = %v, want substring %q", err, tt.wantSubstring)
			}
		})
	}
}

func TestBuildReplayBundleSetsDisplayNames(t *testing.T) {
	store := newReplayStore(t)
	now := time.Date(2026, time.March, 21, 17, 0, 0, 0, time.UTC)

	run := &Run{
		ID:            "run_display_names",
		TaskID:        "task_display_names",
		TaskType:      "agent.run",
		Status:        StatusSucceeded,
		Replayable:    true,
		SchemaVersion: SchemaVersionV1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.db.Create(run).Error; err != nil {
		t.Fatalf("create run error = %v", err)
	}

	events := []*Event{
		{
			RunID:       run.ID,
			TaskID:      run.TaskID,
			Seq:         1,
			Phase:       PhaseInteraction,
			EventType:   "interaction.requested",
			Level:       "info",
			PayloadJSON: json.RawMessage(`{"id":"interaction_1"}`),
			CreatedAt:   now.Add(time.Second),
		},
		{
			RunID:       run.ID,
			TaskID:      run.TaskID,
			Seq:         2,
			Phase:       PhaseRun,
			EventType:   "run.waiting",
			Level:       "info",
			PayloadJSON: json.RawMessage(`{"reason":"approval"}`),
			CreatedAt:   now.Add(2 * time.Second),
		},
		{
			RunID:       run.ID,
			TaskID:      run.TaskID,
			Seq:         3,
			Phase:       PhaseRun,
			EventType:   "  custom.event  ",
			Level:       "info",
			PayloadJSON: json.RawMessage(`{"state":"custom"}`),
			CreatedAt:   now.Add(3 * time.Second),
		},
		{
			RunID:       run.ID,
			TaskID:      run.TaskID,
			Seq:         4,
			Phase:       PhaseRun,
			EventType:   "   ",
			Level:       "info",
			PayloadJSON: json.RawMessage(`{"state":"empty"}`),
			CreatedAt:   now.Add(4 * time.Second),
		},
	}
	for _, event := range events {
		if err := store.db.Create(event).Error; err != nil {
			t.Fatalf("create event seq %d error = %v", event.Seq, err)
		}
	}

	bundle, err := BuildReplayBundle(context.Background(), store, run.ID)
	if err != nil {
		t.Fatalf("BuildReplayBundle() error = %v", err)
	}
	if len(bundle.Timeline) != 4 {
		t.Fatalf("len(bundle.Timeline) = %d, want 4", len(bundle.Timeline))
	}

	if got := bundle.Timeline[0].DisplayName; got != "用户交互请求" {
		t.Fatalf("timeline[0].display_name = %q, want 用户交互请求", got)
	}
	if got := bundle.Timeline[1].DisplayName; got != "运行等待中" {
		t.Fatalf("timeline[1].display_name = %q, want 运行等待中", got)
	}
	if got := bundle.Timeline[2].DisplayName; got != "custom.event" {
		t.Fatalf("timeline[2].display_name = %q, want custom.event", got)
	}
	if got := bundle.Timeline[3].DisplayName; got != "审计事件" {
		t.Fatalf("timeline[3].display_name = %q, want 审计事件", got)
	}
}

func TestBuildReplayBundleRetainsRuntimePromptEnvelopeArtifacts(t *testing.T) {
	store := newReplayStore(t)
	now := time.Date(2026, time.April, 4, 10, 0, 0, 0, time.UTC)

	run := &Run{
		ID:            "run_runtime_prompt",
		TaskID:        "task_runtime_prompt",
		TaskType:      "agent.run",
		Status:        StatusSucceeded,
		Replayable:    true,
		SchemaVersion: SchemaVersionV1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.db.Create(run).Error; err != nil {
		t.Fatalf("create run error = %v", err)
	}

	artifact := &Artifact{
		ID:             "art_runtime_prompt",
		RunID:          run.ID,
		Kind:           ArtifactKindRuntimePromptEnvelope,
		MimeType:       "application/json",
		Encoding:       "utf-8",
		SizeBytes:      int64(len(`{"source_counts":{"forced_block":3}}`)),
		RedactionState: "raw",
		BodyJSON:       json.RawMessage(`{"source_counts":{"forced_block":3}}`),
		CreatedAt:      now.Add(time.Second),
	}
	if err := store.db.Create(artifact).Error; err != nil {
		t.Fatalf("create artifact error = %v", err)
	}

	event := &Event{
		RunID:         run.ID,
		TaskID:        run.TaskID,
		Seq:           1,
		Phase:         PhasePrompt,
		EventType:     "prompt.resolved",
		Level:         "info",
		RefArtifactID: artifact.ID,
		PayloadJSON:   json.RawMessage(`{"segment_count":3}`),
		CreatedAt:     now.Add(2 * time.Second),
	}
	if err := store.db.Create(event).Error; err != nil {
		t.Fatalf("create event error = %v", err)
	}

	bundle, err := BuildReplayBundle(context.Background(), store, run.ID)
	if err != nil {
		t.Fatalf("BuildReplayBundle() error = %v", err)
	}
	if len(bundle.Artifacts) != 1 {
		t.Fatalf("len(bundle.Artifacts) = %d, want 1", len(bundle.Artifacts))
	}
	if bundle.Artifacts[0].Kind != ArtifactKindRuntimePromptEnvelope {
		t.Fatalf("artifact kind = %q, want runtime_prompt_envelope", bundle.Artifacts[0].Kind)
	}
}

func seededReplayStore(t *testing.T) *Store {
	t.Helper()

	store := newReplayStore(t)
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)

	run := &Run{
		ID:             "run_1",
		TaskID:         "task_1",
		ConversationID: "conv_1",
		TaskType:       "agent.run",
		ProviderID:     "provider_1",
		ModelID:        "model_1",
		RunnerID:       "runner_1",
		Status:         StatusSucceeded,
		Replayable:     true,
		SchemaVersion:  SchemaVersionV1,
		StartedAt:      timePointer(now),
		FinishedAt:     timePointer(now.Add(5 * time.Second)),
		CreatedAt:      now,
		UpdatedAt:      now.Add(5 * time.Second),
	}
	if err := store.db.Create(run).Error; err != nil {
		t.Fatalf("create run error = %v", err)
	}

	artifacts := []*Artifact{
		{
			ID:             "art_tool",
			RunID:          run.ID,
			Kind:           ArtifactKindToolOutput,
			MimeType:       "application/json",
			Encoding:       "utf-8",
			SizeBytes:      int64(len(`{"tool_call_id":"call_1","tool_name":"lookup_weather","output":"sunny"}`)),
			RedactionState: "raw",
			BodyJSON:       json.RawMessage(`{"tool_call_id":"call_1","tool_name":"lookup_weather","output":"sunny"}`),
			CreatedAt:      now.Add(3 * time.Second),
		},
		{
			ID:             "art_request",
			RunID:          run.ID,
			Kind:           ArtifactKindRequestMessages,
			MimeType:       "application/json",
			Encoding:       "utf-8",
			SizeBytes:      int64(len(`{"messages":[{"role":"user","content":"hello"}]}`)),
			RedactionState: "raw",
			BodyJSON:       json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`),
			CreatedAt:      now.Add(2 * time.Second),
		},
		{
			ID:             "art_unused",
			RunID:          run.ID,
			Kind:           ArtifactKindErrorSnapshot,
			MimeType:       "application/json",
			Encoding:       "utf-8",
			SizeBytes:      int64(len(`{"error":"unused"}`)),
			RedactionState: "raw",
			BodyJSON:       json.RawMessage(`{"error":"unused"}`),
			CreatedAt:      now.Add(4 * time.Second),
		},
	}
	for _, artifact := range artifacts {
		if err := store.db.Create(artifact).Error; err != nil {
			t.Fatalf("create artifact %s error = %v", artifact.ID, err)
		}
	}

	events := []*Event{
		{
			RunID:         run.ID,
			TaskID:        run.TaskID,
			Seq:           3,
			Phase:         PhaseTool,
			EventType:     "tool.finished",
			Level:         "info",
			StepIndex:     1,
			RefArtifactID: "art_tool",
			PayloadJSON:   json.RawMessage(`{"tool_name":"lookup_weather","output_length":5}`),
			CreatedAt:     now.Add(3 * time.Second),
		},
		{
			RunID:       run.ID,
			TaskID:      run.TaskID,
			Seq:         1,
			Phase:       PhaseRun,
			EventType:   "run.started",
			Level:       "info",
			PayloadJSON: json.RawMessage(`{"status":"running"}`),
			CreatedAt:   now.Add(time.Second),
		},
		{
			RunID:         run.ID,
			TaskID:        run.TaskID,
			Seq:           2,
			Phase:         PhaseRequest,
			EventType:     "request.built",
			Level:         "info",
			StepIndex:     1,
			RefArtifactID: "art_request",
			PayloadJSON:   json.RawMessage(`{"message_count":2}`),
			CreatedAt:     now.Add(2 * time.Second),
		},
	}
	for _, event := range events {
		if err := store.db.Create(event).Error; err != nil {
			t.Fatalf("create event seq %d error = %v", event.Seq, err)
		}
	}

	return store
}

func newReplayStore(t *testing.T) *Store {
	t.Helper()

	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return store
}

func timePointer(value time.Time) *time.Time {
	return &value
}
