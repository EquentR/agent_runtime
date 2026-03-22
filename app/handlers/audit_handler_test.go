package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuditHandlerGetRun(t *testing.T) {
	server, store := newAuditHandlerTestServer(t)
	seedAuditReplayRun(t, store, "run_1")

	response, err := http.Get(server.URL + "/api/v1/audit/runs/run_1")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer response.Body.Close()

	run := decodeAuditRunResponse(t, response.Body)
	if run.ID != "run_1" {
		t.Fatalf("run.ID = %q, want run_1", run.ID)
	}
	if run.TaskID != "task_1" {
		t.Fatalf("run.TaskID = %q, want task_1", run.TaskID)
	}
	if !run.Replayable {
		t.Fatal("run.Replayable = false, want true")
	}
	if run.CreatedBy != "tester" {
		t.Fatalf("run.CreatedBy = %q, want tester", run.CreatedBy)
	}
}

func TestAuditHandlerGetRunEvents(t *testing.T) {
	server, store := newAuditHandlerTestServer(t)
	seedAuditReplayRun(t, store, "run_1")

	response, err := http.Get(server.URL + "/api/v1/audit/runs/run_1/events")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer response.Body.Close()

	events := decodeAuditEventsResponse(t, response.Body)
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 || events[2].Seq != 3 {
		t.Fatalf("event seqs = [%d %d %d], want [1 2 3]", events[0].Seq, events[1].Seq, events[2].Seq)
	}
	if events[1].RefArtifactID != "art_request" {
		t.Fatalf("events[1].RefArtifactID = %q, want art_request", events[1].RefArtifactID)
	}
	if events[2].EventType != "tool.finished" {
		t.Fatalf("events[2].EventType = %q, want tool.finished", events[2].EventType)
	}
}

func TestAuditHandlerGetReplayBundle(t *testing.T) {
	server, store := newAuditHandlerTestServer(t)
	seedAuditReplayRun(t, store, "run_1")

	response, err := http.Get(server.URL + "/api/v1/audit/runs/run_1/replay")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer response.Body.Close()

	bundle := decodeAuditReplayResponse(t, response.Body)
	if bundle.Run.ID != "run_1" {
		t.Fatalf("bundle.Run.ID = %q, want run_1", bundle.Run.ID)
	}
	if len(bundle.Timeline) != 3 {
		t.Fatalf("len(bundle.Timeline) = %d, want 3", len(bundle.Timeline))
	}
	if bundle.Timeline[1].Artifact == nil || bundle.Timeline[1].Artifact.ID != "art_request" {
		t.Fatalf("bundle.Timeline[1].Artifact = %#v, want art_request summary", bundle.Timeline[1].Artifact)
	}
	if len(bundle.Artifacts) != 2 {
		t.Fatalf("len(bundle.Artifacts) = %d, want 2", len(bundle.Artifacts))
	}
	if string(bundle.Artifacts[0].Body) == "" {
		t.Fatal("bundle.Artifacts[0].Body is empty, want inlined JSON body")
	}
}

func TestAuditHandlerRequiresSession(t *testing.T) {
	deps, server := newAuthenticatedAuditHandlerTestServer(t)
	seedAuditReplayRunWithOwner(t, deps.store, "run_1", "owner")

	response, err := http.Get(server.URL + "/api/v1/audit/runs/run_1")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer response.Body.Close()

	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("response ok = true, want false for anonymous audit access")
	}
	if envelope.Code != http.StatusUnauthorized {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusUnauthorized)
	}
}

func TestAuditHandlerRejectsCrossUserAccess(t *testing.T) {
	deps, server := newAuthenticatedAuditHandlerTestServer(t)
	seedAuditReplayRunWithOwner(t, deps.store, "run_1", "owner")
	registerAuditHandlerUser(t, deps.authLogic, "admin")
	registerAuditHandlerUser(t, deps.authLogic, "guest")
	cookie := loginAuditHandlerSessionCookie(t, deps.authLogic, "guest")

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/audit/runs/run_1/replay", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.AddCookie(cookie)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("response ok = true, want false for cross-user audit access")
	}
	if envelope.Code != http.StatusUnauthorized {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(envelope.Message, "无权") {
		t.Fatalf("envelope.Message = %q, want ownership denial", envelope.Message)
	}
}

func TestAuditHandlerAdminCanReadAnotherUsersRunAndReplay(t *testing.T) {
	deps, server := newAuthenticatedAuditHandlerTestServer(t)
	admin := registerAuditHandlerUser(t, deps.authLogic, "admin")
	if admin.Role != models.UserRoleAdmin {
		t.Fatalf("admin.Role = %q, want %q", admin.Role, models.UserRoleAdmin)
	}
	owner := registerAuditHandlerUser(t, deps.authLogic, "owner")
	seedAuditReplayRunWithOwner(t, deps.store, "run_1", owner.Username)
	cookie := loginAuditHandlerSessionCookie(t, deps.authLogic, admin.Username)

	runRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/audit/runs/run_1", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(run) error = %v", err)
	}
	runRequest.AddCookie(cookie)

	runResponse, err := http.DefaultClient.Do(runRequest)
	if err != nil {
		t.Fatalf("Do(run) error = %v", err)
	}
	defer runResponse.Body.Close()
	run := decodeAuditRunResponse(t, runResponse.Body)
	if run.ID != "run_1" {
		t.Fatalf("run.ID = %q, want run_1", run.ID)
	}
	if run.CreatedBy != owner.Username {
		t.Fatalf("run.CreatedBy = %q, want %q", run.CreatedBy, owner.Username)
	}

	eventsRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/audit/runs/run_1/events", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(events) error = %v", err)
	}
	eventsRequest.AddCookie(cookie)

	eventsResponse, err := http.DefaultClient.Do(eventsRequest)
	if err != nil {
		t.Fatalf("Do(events) error = %v", err)
	}
	defer eventsResponse.Body.Close()
	events := decodeAuditEventsResponse(t, eventsResponse.Body)
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}

	replayRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/audit/runs/run_1/replay", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(replay) error = %v", err)
	}
	replayRequest.AddCookie(cookie)

	replayResponse, err := http.DefaultClient.Do(replayRequest)
	if err != nil {
		t.Fatalf("Do(replay) error = %v", err)
	}
	defer replayResponse.Body.Close()
	bundle := decodeAuditReplayResponse(t, replayResponse.Body)
	if bundle.Run.ID != "run_1" {
		t.Fatalf("bundle.Run.ID = %q, want run_1", bundle.Run.ID)
	}
	if bundle.Run.CreatedBy != owner.Username {
		t.Fatalf("bundle.Run.CreatedBy = %q, want %q", bundle.Run.CreatedBy, owner.Username)
	}
}

func TestAuditHandlerGetRunReturnsNotFound(t *testing.T) {
	server, _ := newAuditHandlerTestServer(t)

	response, err := http.Get(server.URL + "/api/v1/audit/runs/missing")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer response.Body.Close()

	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("response ok = true, want false for missing audit run")
	}
	if envelope.Code != http.StatusNotFound {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusNotFound)
	}
}

func TestAuditHandlerGetRunEventsReturnsNotFound(t *testing.T) {
	server, _ := newAuditHandlerTestServer(t)

	response, err := http.Get(server.URL + "/api/v1/audit/runs/missing/events")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer response.Body.Close()

	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("response ok = true, want false for missing audit event stream")
	}
	if envelope.Code != http.StatusNotFound {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusNotFound)
	}
}

func TestAuditHandlerGetReplayBundleReturnsConflictForRunningRun(t *testing.T) {
	server, store := newAuditHandlerTestServer(t)
	seedAuditRunningRun(t, store, "run_1")

	response, err := http.Get(server.URL + "/api/v1/audit/runs/run_1/replay")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer response.Body.Close()

	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("response ok = true, want false for non-terminal replay bundle")
	}
	if envelope.Code != http.StatusConflict {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusConflict)
	}
}

type authenticatedAuditHandlerTestDeps struct {
	store     *coreaudit.Store
	authLogic *logics.AuthLogic
}

func newAuditHandlerTestServer(t *testing.T) (*httptest.Server, *coreaudit.Store) {
	t.Helper()

	db := newAuditHandlerTestDB(t)
	store := coreaudit.NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	engine := rest.Init()
	NewAuditHandler(store).Register(engine.Group("/api/v1"))

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return server, store
}

func newAuthenticatedAuditHandlerTestServer(t *testing.T) (*authenticatedAuditHandlerTestDeps, *httptest.Server) {
	t.Helper()

	db := newAuditHandlerTestDB(t)
	store := coreaudit.NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	authLogic := newAuthLogicForTest(t, db)
	authMiddleware := NewAuthMiddleware(authLogic)

	engine := rest.Init()
	NewAuditHandler(store, authMiddleware.RequireSession()).Register(engine.Group("/api/v1"))

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return &authenticatedAuditHandlerTestDeps{store: store, authLogic: authLogic}, server
}

func newAuditHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}

func seedAuditReplayRun(t *testing.T, store *coreaudit.Store, runID string) {
	t.Helper()
	seedAuditReplayRunWithOwner(t, store, runID, "tester")
}

func seedAuditReplayRunWithOwner(t *testing.T, store *coreaudit.Store, runID string, createdBy string) {
	t.Helper()
	seedAuditRun(t, store, runID, createdBy, true)
}

func seedAuditRunningRun(t *testing.T, store *coreaudit.Store, runID string) {
	t.Helper()
	seedAuditRun(t, store, runID, "tester", false)
}

func seedAuditRun(t *testing.T, store *coreaudit.Store, runID string, createdBy string, finish bool) {
	t.Helper()

	ctx := context.Background()
	now := time.Date(2026, time.March, 21, 18, 0, 0, 0, time.UTC)
	run, err := store.CreateRun(ctx, coreaudit.StartRunInput{
		RunID:          runID,
		TaskID:         "task_1",
		ConversationID: "conv_1",
		TaskType:       "agent.run",
		ProviderID:     "openai",
		ModelID:        "gpt-5.4",
		RunnerID:       "runner_1",
		CreatedBy:      createdBy,
		Replayable:     true,
		SchemaVersion:  coreaudit.SchemaVersionV1,
		Status:         coreaudit.StatusRunning,
		StartedAt:      now,
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	requestArtifact, err := store.CreateArtifact(ctx, run.ID, coreaudit.CreateArtifactInput{
		ArtifactID:     "art_request",
		Kind:           coreaudit.ArtifactKindRequestMessages,
		MimeType:       "application/json",
		Encoding:       "utf-8",
		RedactionState: "raw",
		Body:           map[string]any{"messages": []map[string]any{{"role": "user", "content": "hello"}}},
		CreatedAt:      now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("CreateArtifact(request) error = %v", err)
	}
	toolArtifact, err := store.CreateArtifact(ctx, run.ID, coreaudit.CreateArtifactInput{
		ArtifactID:     "art_tool",
		Kind:           coreaudit.ArtifactKindToolOutput,
		MimeType:       "application/json",
		Encoding:       "utf-8",
		RedactionState: "raw",
		Body:           map[string]any{"tool_call_id": "call_1", "tool_name": "lookup_weather", "output": "sunny"},
		CreatedAt:      now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("CreateArtifact(tool) error = %v", err)
	}

	for _, input := range []coreaudit.AppendEventInput{
		{
			Phase:     coreaudit.PhaseRun,
			EventType: "run.started",
			Payload:   map[string]any{"status": "running"},
			CreatedAt: now.Add(3 * time.Second),
		},
		{
			Phase:         coreaudit.PhaseRequest,
			EventType:     "request.built",
			RefArtifactID: requestArtifact.ID,
			Payload:       map[string]any{"message_count": 1},
			CreatedAt:     now.Add(4 * time.Second),
		},
		{
			Phase:         coreaudit.PhaseTool,
			EventType:     "tool.finished",
			RefArtifactID: toolArtifact.ID,
			Payload:       map[string]any{"tool_name": "lookup_weather"},
			CreatedAt:     now.Add(5 * time.Second),
		},
	} {
		if _, err := store.AppendEvent(ctx, run.ID, input); err != nil {
			t.Fatalf("AppendEvent(%s) error = %v", input.EventType, err)
		}
	}

	if !finish {
		return
	}

	if err := store.FinishRun(ctx, run.ID, coreaudit.StatusSucceeded, now.Add(6*time.Second)); err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}
}

func newAuditHandlerSessionCookie(t *testing.T, logic *logics.AuthLogic, username string) *http.Cookie {
	t.Helper()

	const password = "secret1"
	if _, err := logic.Register(context.Background(), username, password, password); err != nil {
		t.Fatalf("Register(%q) error = %v", username, err)
	}
	return loginAuditHandlerSessionCookie(t, logic, username)
}

func loginAuditHandlerSessionCookie(t *testing.T, logic *logics.AuthLogic, username string) *http.Cookie {
	t.Helper()

	const password = "secret1"
	_, session, err := logic.Login(context.Background(), username, password)
	if err != nil {
		t.Fatalf("Login(%q) error = %v", username, err)
	}
	return &http.Cookie{Name: logic.CookieName(), Value: session.ID}
}

func registerAuditHandlerUser(t *testing.T, logic *logics.AuthLogic, username string) *models.User {
	t.Helper()

	const password = "secret1"
	user, err := logic.Register(context.Background(), username, password, password)
	if err != nil {
		t.Fatalf("Register(%q) error = %v", username, err)
	}
	return user
}

func decodeAuditRunResponse(t *testing.T, body io.Reader) coreaudit.Run {
	t.Helper()

	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var run coreaudit.Run
	if err := json.Unmarshal(envelope.Data, &run); err != nil {
		t.Fatalf("Unmarshal() run error = %v", err)
	}
	return run
}

func decodeAuditEventsResponse(t *testing.T, body io.Reader) []coreaudit.Event {
	t.Helper()

	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var events []coreaudit.Event
	if err := json.Unmarshal(envelope.Data, &events); err != nil {
		t.Fatalf("Unmarshal() events error = %v", err)
	}
	return events
}

func decodeAuditReplayResponse(t *testing.T, body io.Reader) coreaudit.ReplayBundle {
	t.Helper()

	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var bundle coreaudit.ReplayBundle
	if err := json.Unmarshal(envelope.Data, &bundle); err != nil {
		t.Fatalf("Unmarshal() replay bundle error = %v", err)
	}
	return bundle
}
