package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestConversationHandlerGetConversation(t *testing.T) {
	store, auditStore, server := newConversationHandlerTestServer(t)
	conversation, err := store.CreateConversation(context.Background(), coreagent.CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		Title:      "Hello",
		CreatedBy:  "tester",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if _, err := auditStore.CreateRun(context.Background(), coreaudit.StartRunInput{
		RunID:          "run_1",
		TaskID:         "task_1",
		ConversationID: conversation.ID,
		TaskType:       "agent.run",
		CreatedBy:      "tester",
		Status:         coreaudit.StatusSucceeded,
		SchemaVersion:  coreaudit.SchemaVersionV1,
	}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	resp, err := http.Get(server.URL + "/api/v1/conversations/" + conversation.ID)
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	got := decodeConversationResponse(t, resp.Body)
	if got.ID != "conv_1" || got.ProviderID != "openai" || got.ModelID != "gpt-5.4" {
		t.Fatalf("conversation = %#v, want persisted conversation", got)
	}
	if got.AuditRunID != "run_1" {
		t.Fatalf("conversation.AuditRunID = %q, want run_1", got.AuditRunID)
	}
}

func TestConversationHandlerGetConversationMessages(t *testing.T) {
	store, _, server := newConversationHandlerTestServer(t)
	_, err := store.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if err := store.AppendMessages(context.Background(), "conv_1", "task_1", []model.Message{{Role: model.RoleUser, Content: "hello"}, {Role: model.RoleAssistant, Content: "hi"}}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	resp, err := http.Get(server.URL + "/api/v1/conversations/conv_1/messages")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	got := decodeConversationMessagesResponse(t, resp.Body)
	if len(got) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(got))
	}
	if got[0].Role != model.RoleUser || got[1].Role != model.RoleAssistant {
		t.Fatalf("messages = %#v, want ordered user/assistant messages", got)
	}
}

func TestConversationHandlerGetConversationMessagesIncludesPersistedUsage(t *testing.T) {
	_, _, db, server := newConversationHandlerTestServerWithDB(t)
	if err := db.Create(&coreagent.Conversation{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4"}).Error; err != nil {
		t.Fatalf("create conversation error = %v", err)
	}
	envelopeRaw := []byte(`{"version":"v1","message":{"Role":"assistant","Content":"hello","Usage":{"PromptTokens":123,"CompletionTokens":45,"TotalTokens":168}}}`)
	if err := db.Create(&coreagent.ConversationMessage{ConversationID: "conv_1", Seq: 1, Role: model.RoleAssistant, Content: "hello", MessageJSON: envelopeRaw, TaskID: "task_1"}).Error; err != nil {
		t.Fatalf("insert message error = %v", err)
	}

	resp, err := http.Get(server.URL + "/api/v1/conversations/conv_1/messages")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	got := decodeConversationMessagesResponseAsMaps(t, resp.Body)
	if len(got) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(got))
	}
	usage, ok := got[0]["Usage"].(map[string]any)
	if !ok {
		t.Fatalf("message = %#v, want Usage map", got[0])
	}
	if usage["PromptTokens"] != float64(123) || usage["CompletionTokens"] != float64(45) || usage["TotalTokens"] != float64(168) {
		t.Fatalf("usage = %#v, want persisted token usage", usage)
	}
}

func TestConversationHandlerGetConversationMessagesBackfillsUsageFromTaskResult(t *testing.T) {
	_, _, db, server := newConversationHandlerTestServerWithDB(t)
	if err := db.AutoMigrate(&coretasks.Task{}); err != nil {
		t.Fatalf("AutoMigrate(tasks) error = %v", err)
	}
	if err := db.Create(&coreagent.Conversation{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4"}).Error; err != nil {
		t.Fatalf("create conversation error = %v", err)
	}
	envelopeRaw := []byte(`{"version":"v1","message":{"Role":"assistant","Content":"hello"}}`)
	if err := db.Create(&coreagent.ConversationMessage{ConversationID: "conv_1", Seq: 1, Role: model.RoleAssistant, Content: "hello", MessageJSON: envelopeRaw, TaskID: "task_1"}).Error; err != nil {
		t.Fatalf("insert message error = %v", err)
	}
	resultJSON := []byte(`{"conversation_id":"conv_1","final_message":{"Role":"assistant","Content":"hello"},"usage":{"PromptTokens":222,"CompletionTokens":33,"TotalTokens":255}}`)
	if err := db.Create(&coretasks.Task{
		ID:            "task_1",
		TaskType:      "agent.run",
		Status:        coretasks.StatusSucceeded,
		InputJSON:     []byte(`{}`),
		ConfigJSON:    []byte(`{}`),
		MetadataJSON:  []byte(`{}`),
		ResultJSON:    resultJSON,
		ExecutionMode: coretasks.ExecutionModeSerial,
		RootTaskID:    "task_1",
	}).Error; err != nil {
		t.Fatalf("insert task error = %v", err)
	}

	resp, err := http.Get(server.URL + "/api/v1/conversations/conv_1/messages")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	got := decodeConversationMessagesResponseAsMaps(t, resp.Body)
	if len(got) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(got))
	}
	usage, ok := got[0]["Usage"].(map[string]any)
	if !ok {
		t.Fatalf("message = %#v, want backfilled Usage map", got[0])
	}
	if usage["PromptTokens"] != float64(222) || usage["CompletionTokens"] != float64(33) || usage["TotalTokens"] != float64(255) {
		t.Fatalf("usage = %#v, want task result usage", usage)
	}
}

func TestConversationHandlerReturnsNotFoundForMissingConversation(t *testing.T) {
	_, _, server := newConversationHandlerTestServer(t)
	resp, err := http.Get(server.URL + "/api/v1/conversations/missing")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	var envelope taskTestResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if envelope.OK {
		t.Fatal("response ok = true, want false for missing conversation")
	}
}

func TestConversationHandlerListConversations(t *testing.T) {
	store, auditStore, server := newConversationHandlerTestServer(t)
	_, err := store.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: "conv_old", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation(old) error = %v", err)
	}
	_, err = store.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: "conv_new", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation(new) error = %v", err)
	}
	if err := store.AppendMessages(context.Background(), "conv_old", "task_old", []model.Message{{Role: model.RoleUser, Content: "Old topic"}}); err != nil {
		t.Fatalf("AppendMessages(old) error = %v", err)
	}
	if _, err := auditStore.CreateRun(context.Background(), coreaudit.StartRunInput{
		RunID:          "run_new",
		TaskID:         "task_new",
		ConversationID: "conv_new",
		TaskType:       "agent.run",
		CreatedBy:      "tester",
		Status:         coreaudit.StatusSucceeded,
		SchemaVersion:  coreaudit.SchemaVersionV1,
	}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := store.AppendMessages(context.Background(), "conv_new", "task_new", []model.Message{{Role: model.RoleUser, Content: "New topic"}, {Role: model.RoleAssistant, Content: "Latest answer"}}); err != nil {
		t.Fatalf("AppendMessages(new) error = %v", err)
	}

	resp, err := http.Get(server.URL + "/api/v1/conversations")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	got := decodeConversationListResponse(t, resp.Body)
	if len(got) < 2 {
		t.Fatalf("len(conversations) = %d, want at least 2", len(got))
	}
	if got[0].ID != "conv_new" {
		t.Fatalf("first conversation = %q, want conv_new", got[0].ID)
	}
	if got[0].Title == "" || got[0].LastMessage != "Latest answer" || got[0].MessageCount != 2 {
		t.Fatalf("conversation summary = %#v, want title/last_message/message_count", got[0])
	}
	if got[0].AuditRunID != "run_new" {
		t.Fatalf("conversation audit_run_id = %q, want run_new", got[0].AuditRunID)
	}
}

func TestConversationHandlerDeleteConversation(t *testing.T) {
	store, _, server := newConversationHandlerTestServer(t)
	_, err := store.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if err := store.AppendMessages(context.Background(), "conv_1", "task_1", []model.Message{{Role: model.RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/conversations/conv_1", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	listResp, err := http.Get(server.URL + "/api/v1/conversations")
	if err != nil {
		t.Fatalf("http.Get(list) error = %v", err)
	}
	defer listResp.Body.Close()
	got := decodeConversationListResponse(t, listResp.Body)
	if len(got) != 0 {
		t.Fatalf("len(conversations) = %d, want 0 after delete", len(got))
	}
}

func TestConversationHandlerAdminCanListOtherUsersConversations(t *testing.T) {
	deps, server := newAuthenticatedConversationHandlerTestServer(t)
	if _, err := deps.authLogic.Register(context.Background(), "admin", "secret-123", "secret-123"); err != nil {
		t.Fatalf("Register(admin) error = %v", err)
	}
	owner, err := deps.authLogic.Register(context.Background(), "owner", "secret-123", "secret-123")
	if err != nil {
		t.Fatalf("Register(owner) error = %v", err)
	}
	if _, err := deps.store.CreateConversation(context.Background(), coreagent.CreateConversationInput{
		ID:         "conv_owner",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		CreatedBy:  owner.Username,
	}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/conversations", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.AddCookie(newConversationHandlerSessionCookie(t, deps.authLogic, "admin"))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	got := decodeConversationListResponse(t, resp.Body)
	if len(got) != 1 {
		t.Fatalf("len(conversations) = %d, want 1", len(got))
	}
	if got[0].ID != "conv_owner" {
		t.Fatalf("conversation.ID = %q, want conv_owner", got[0].ID)
	}
}

func TestConversationHandlerAdminCanViewAndDeleteOtherUsersConversation(t *testing.T) {
	deps, server := newAuthenticatedConversationHandlerTestServer(t)
	if _, err := deps.authLogic.Register(context.Background(), "admin", "secret-123", "secret-123"); err != nil {
		t.Fatalf("Register(admin) error = %v", err)
	}
	owner, err := deps.authLogic.Register(context.Background(), "owner", "secret-123", "secret-123")
	if err != nil {
		t.Fatalf("Register(owner) error = %v", err)
	}
	if _, err := deps.store.CreateConversation(context.Background(), coreagent.CreateConversationInput{
		ID:         "conv_owner",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		CreatedBy:  owner.Username,
	}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	getRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/conversations/conv_owner", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(get) error = %v", err)
	}
	adminCookie := newConversationHandlerSessionCookie(t, deps.authLogic, "admin")
	getRequest.AddCookie(adminCookie)

	getResponse, err := http.DefaultClient.Do(getRequest)
	if err != nil {
		t.Fatalf("Do(get) error = %v", err)
	}
	defer getResponse.Body.Close()
	conversation := decodeConversationResponse(t, getResponse.Body)
	if conversation.ID != "conv_owner" {
		t.Fatalf("conversation.ID = %q, want conv_owner", conversation.ID)
	}

	messagesRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/conversations/conv_owner/messages", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(messages) error = %v", err)
	}
	messagesRequest.AddCookie(adminCookie)

	if err := deps.store.AppendMessages(context.Background(), "conv_owner", "task_1", []model.Message{{Role: model.RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}
	messagesResponse, err := http.DefaultClient.Do(messagesRequest)
	if err != nil {
		t.Fatalf("Do(messages) error = %v", err)
	}
	defer messagesResponse.Body.Close()
	messages := decodeConversationMessagesResponse(t, messagesResponse.Body)
	if len(messages) != 1 || messages[0].Content != "hello" {
		t.Fatalf("messages = %#v, want owner messages readable by admin", messages)
	}

	deleteRequest, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/conversations/conv_owner", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(delete) error = %v", err)
	}
	deleteRequest.AddCookie(adminCookie)

	deleteResponse, err := http.DefaultClient.Do(deleteRequest)
	if err != nil {
		t.Fatalf("Do(delete) error = %v", err)
	}
	defer deleteResponse.Body.Close()

	deleteEnvelope := decodeEnvelope(t, deleteResponse.Body)
	if deleteEnvelope.OK {
		t.Fatal("delete response ok = true, want false for admin cross-user delete")
	}
	if deleteEnvelope.Code != http.StatusUnauthorized {
		t.Fatalf("delete envelope.Code = %d, want %d", deleteEnvelope.Code, http.StatusUnauthorized)
	}

	listRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/conversations", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(list) error = %v", err)
	}
	listRequest.AddCookie(adminCookie)

	listResponse, err := http.DefaultClient.Do(listRequest)
	if err != nil {
		t.Fatalf("Do(list) error = %v", err)
	}
	defer listResponse.Body.Close()
	got := decodeConversationListResponse(t, listResponse.Body)
	if len(got) != 1 {
		t.Fatalf("len(conversations) = %d, want 1 because admin delete should be denied", len(got))
	}
}

func TestConversationHandlerRejectsCrossUserAccessForNormalUsers(t *testing.T) {
	deps, server := newAuthenticatedConversationHandlerTestServer(t)
	if _, err := deps.authLogic.Register(context.Background(), "admin", "secret-123", "secret-123"); err != nil {
		t.Fatalf("Register(admin) error = %v", err)
	}
	owner, err := deps.authLogic.Register(context.Background(), "owner", "secret-123", "secret-123")
	if err != nil {
		t.Fatalf("Register(owner) error = %v", err)
	}
	if _, err := deps.authLogic.Register(context.Background(), "guest", "secret-123", "secret-123"); err != nil {
		t.Fatalf("Register(guest) error = %v", err)
	}
	if _, err := deps.store.CreateConversation(context.Background(), coreagent.CreateConversationInput{
		ID:         "conv_owner",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		CreatedBy:  owner.Username,
	}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	guestCookie := newConversationHandlerSessionCookie(t, deps.authLogic, "guest")
	listRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/conversations", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(list) error = %v", err)
	}
	listRequest.AddCookie(guestCookie)

	listResponse, err := http.DefaultClient.Do(listRequest)
	if err != nil {
		t.Fatalf("Do(list) error = %v", err)
	}
	defer listResponse.Body.Close()
	got := decodeConversationListResponse(t, listResponse.Body)
	if len(got) != 0 {
		t.Fatalf("len(conversations) = %d, want 0 for cross-user list", len(got))
	}

	getRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/conversations/conv_owner", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(get) error = %v", err)
	}
	getRequest.AddCookie(guestCookie)

	getResponse, err := http.DefaultClient.Do(getRequest)
	if err != nil {
		t.Fatalf("Do(get) error = %v", err)
	}
	defer getResponse.Body.Close()
	getEnvelope := decodeEnvelope(t, getResponse.Body)
	if getEnvelope.OK {
		t.Fatal("get response ok = true, want false for cross-user access")
	}
	if getEnvelope.Code != http.StatusUnauthorized {
		t.Fatalf("get envelope.Code = %d, want %d", getEnvelope.Code, http.StatusUnauthorized)
	}

	deleteRequest, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/conversations/conv_owner", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(delete) error = %v", err)
	}
	deleteRequest.AddCookie(guestCookie)

	deleteResponse, err := http.DefaultClient.Do(deleteRequest)
	if err != nil {
		t.Fatalf("Do(delete) error = %v", err)
	}
	defer deleteResponse.Body.Close()
	deleteEnvelope := decodeEnvelope(t, deleteResponse.Body)
	if deleteEnvelope.OK {
		t.Fatal("delete response ok = true, want false for cross-user access")
	}
	if deleteEnvelope.Code != http.StatusUnauthorized {
		t.Fatalf("delete envelope.Code = %d, want %d", deleteEnvelope.Code, http.StatusUnauthorized)
	}
}

func newConversationHandlerTestServer(t *testing.T) (*coreagent.ConversationStore, *coreaudit.Store, *httptest.Server) {
	store, auditStore, _, server := newConversationHandlerTestServerWithDB(t)
	return store, auditStore, server
}

type authenticatedConversationHandlerTestDeps struct {
	store     *coreagent.ConversationStore
	authLogic *logics.AuthLogic
}

func newConversationHandlerTestServerWithDB(t *testing.T) (*coreagent.ConversationStore, *coreaudit.Store, *gorm.DB, *httptest.Server) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	store := coreagent.NewConversationStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	auditStore := coreaudit.NewStore(db)
	if err := auditStore.AutoMigrate(); err != nil {
		t.Fatalf("audit AutoMigrate() error = %v", err)
	}
	engine := rest.Init()
	NewConversationHandler(store, auditStore).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return store, auditStore, db, server
}

func newAuthenticatedConversationHandlerTestServer(t *testing.T) (*authenticatedConversationHandlerTestDeps, *httptest.Server) {
	t.Helper()

	_, auditStore, db, _ := newConversationHandlerTestServerWithDB(t)
	authLogic := newAuthLogicForTest(t, db)
	authMiddleware := NewAuthMiddleware(authLogic)
	store := coreagent.NewConversationStore(db)

	engine := rest.Init()
	NewConversationHandler(store, auditStore, authMiddleware.RequireSession()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	return &authenticatedConversationHandlerTestDeps{store: store, authLogic: authLogic}, server
}

func newConversationHandlerSessionCookie(t *testing.T, logic *logics.AuthLogic, username string) *http.Cookie {
	t.Helper()

	_, session, err := logic.Login(context.Background(), username, "secret-123")
	if err != nil {
		t.Fatalf("Login(%q) error = %v", username, err)
	}
	return &http.Cookie{Name: logic.CookieName(), Value: session.ID}
}

func decodeConversationResponse(t *testing.T, body io.Reader) coreagent.Conversation {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var got coreagent.Conversation
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return got
}

func decodeConversationMessagesResponse(t *testing.T, body io.Reader) []model.Message {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var got []model.Message
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return got
}

func decodeConversationMessagesResponseAsMaps(t *testing.T, body io.Reader) []map[string]any {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var got []map[string]any
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return got
}

func decodeConversationListResponse(t *testing.T, body io.Reader) []coreagent.Conversation {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var got []coreagent.Conversation
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return got
}
