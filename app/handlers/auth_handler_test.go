package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EquentR/agent_runtime/app/logics"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/rest"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type authTestUser struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}

func TestAuthHandlerRegisterLoginLogoutFlow(t *testing.T) {
	server := newAuthHandlerTestServer(t)

	register := postAuthJSON(t, server.URL+"/api/v1/auth/register", map[string]any{
		"username":         "alice",
		"password":         "secret-123",
		"confirm_password": "secret-123",
	})
	defer register.Body.Close()
	if register.StatusCode != http.StatusOK {
		t.Fatalf("register status = %d, want 200", register.StatusCode)
	}
	registered := decodeAuthUserResponse(t, register.Body)
	if registered.Username != "alice" {
		t.Fatalf("registered username = %q, want alice", registered.Username)
	}

	duplicate := postAuthJSON(t, server.URL+"/api/v1/auth/register", map[string]any{
		"username":         "alice",
		"password":         "secret-123",
		"confirm_password": "secret-123",
	})
	defer duplicate.Body.Close()
	if decodeEnvelope(t, duplicate.Body).OK {
		t.Fatal("duplicate register ok = true, want false")
	}

	login := postAuthJSON(t, server.URL+"/api/v1/auth/login", map[string]any{
		"username": "alice",
		"password": "secret-123",
	})
	defer login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", login.StatusCode)
	}
	sessionCookie := mustFindCookie(t, login.Cookies(), authSessionCookieName)
	if sessionCookie.Value == "" {
		t.Fatal("session cookie value is empty")
	}
	loggedIn := decodeAuthUserResponse(t, login.Body)
	if loggedIn.Username != "alice" {
		t.Fatalf("login username = %q, want alice", loggedIn.Username)
	}

	meRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/auth/me", nil)
	if err != nil {
		t.Fatalf("NewRequest(me) error = %v", err)
	}
	meRequest.AddCookie(sessionCookie)
	meResponse, err := http.DefaultClient.Do(meRequest)
	if err != nil {
		t.Fatalf("Do(me) error = %v", err)
	}
	defer meResponse.Body.Close()
	current := decodeAuthUserResponse(t, meResponse.Body)
	if current.Username != "alice" {
		t.Fatalf("current username = %q, want alice", current.Username)
	}

	logoutRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/logout", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("NewRequest(logout) error = %v", err)
	}
	logoutRequest.Header.Set("Content-Type", "application/json")
	logoutRequest.AddCookie(sessionCookie)
	logoutResponse, err := http.DefaultClient.Do(logoutRequest)
	if err != nil {
		t.Fatalf("Do(logout) error = %v", err)
	}
	defer logoutResponse.Body.Close()
	clearedCookie := mustFindCookie(t, logoutResponse.Cookies(), authSessionCookieName)
	if clearedCookie.MaxAge >= 0 && clearedCookie.Value != "" {
		t.Fatalf("logout cookie = %#v, want cleared cookie", clearedCookie)
	}

	meAfterLogout, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/auth/me", nil)
	if err != nil {
		t.Fatalf("NewRequest(meAfterLogout) error = %v", err)
	}
	meAfterLogout.AddCookie(clearedCookie)
	meAfterLogoutResponse, err := http.DefaultClient.Do(meAfterLogout)
	if err != nil {
		t.Fatalf("Do(meAfterLogout) error = %v", err)
	}
	defer meAfterLogoutResponse.Body.Close()
	if decodeEnvelope(t, meAfterLogoutResponse.Body).OK {
		t.Fatal("me after logout ok = true, want false")
	}

	wrongPassword := postAuthJSON(t, server.URL+"/api/v1/auth/login", map[string]any{
		"username": "alice",
		"password": "bad-password",
	})
	defer wrongPassword.Body.Close()
	if decodeEnvelope(t, wrongPassword.Body).OK {
		t.Fatal("wrong password login ok = true, want false")
	}
}

func TestAuthMiddlewareRejectsAnonymousTaskAndConversationRequests(t *testing.T) {
	server := newAuthHandlerTestServer(t)

	taskResponse := postAuthJSON(t, server.URL+"/api/v1/tasks", map[string]any{
		"task_type": "agent.run",
		"input":     map[string]any{"prompt": "hello"},
	})
	defer taskResponse.Body.Close()
	if decodeEnvelope(t, taskResponse.Body).OK {
		t.Fatal("anonymous task create ok = true, want false")
	}

	conversationResponse, err := http.Get(server.URL + "/api/v1/conversations")
	if err != nil {
		t.Fatalf("http.Get(conversations) error = %v", err)
	}
	defer conversationResponse.Body.Close()
	if decodeEnvelope(t, conversationResponse.Body).OK {
		t.Fatal("anonymous conversation list ok = true, want false")
	}
}

func TestAuthMiddlewareWrapperOptionProtectsSingleRoute(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	authLogic := newAuthLogicForTest(t, db)
	user, err := authLogic.Register(context.Background(), "alice", "secret-123", "secret-123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	_, session, err := authLogic.Login(context.Background(), user.Username, "secret-123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	authMiddleware := NewAuthMiddleware(authLogic)

	engine := rest.Init()
	group := engine.Group("/api/v1/test")
	resp.HandlerWrapper(group, "", []*resp.Handler{
		resp.NewJsonOptionsHandler(func() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
			return http.MethodGet, "/public", func(c *gin.Context) (any, []resp.ResOpt, error) {
				return gin.H{"public": true}, nil, nil
			}, nil
		}),
		resp.NewJsonOptionsHandler(func() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
			return http.MethodGet, "/private", func(c *gin.Context) (any, []resp.ResOpt, error) {
				current, ok := authMiddleware.CurrentUser(c)
				if !ok || current == nil {
					return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, fmt.Errorf("missing auth user in context")
				}
				return authUserResponse(current), nil, nil
			}, []resp.WrapperOption{authMiddleware.RequireSessionOption()}
		}),
	})

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	publicResponse, err := http.Get(server.URL + "/api/v1/test/public")
	if err != nil {
		t.Fatalf("http.Get(public) error = %v", err)
	}
	defer publicResponse.Body.Close()
	if !decodeEnvelope(t, publicResponse.Body).OK {
		t.Fatal("public route ok = false, want true")
	}

	privateResponse, err := http.Get(server.URL + "/api/v1/test/private")
	if err != nil {
		t.Fatalf("http.Get(private) error = %v", err)
	}
	defer privateResponse.Body.Close()
	if decodeEnvelope(t, privateResponse.Body).OK {
		t.Fatal("anonymous private route ok = true, want false")
	}

	authedRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/test/private", nil)
	if err != nil {
		t.Fatalf("NewRequest(private) error = %v", err)
	}
	authedRequest.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: session.ID})
	authedResponse, err := http.DefaultClient.Do(authedRequest)
	if err != nil {
		t.Fatalf("Do(private) error = %v", err)
	}
	defer authedResponse.Body.Close()
	authedUser := decodeAuthUserResponse(t, authedResponse.Body)
	if authedUser.Username != "alice" {
		t.Fatalf("private username = %q, want alice", authedUser.Username)
	}
}

func TestAuthMiddlewareRejectsTaskCreationAgainstAnotherUsersConversation(t *testing.T) {
	deps, server := newAuthHandlerTestServerWithDeps(t)

	owner, err := deps.authLogic.Register(context.Background(), "owner", "secret-123", "secret-123")
	if err != nil {
		t.Fatalf("Register(owner) error = %v", err)
	}
	conversation, err := deps.conversationStore.CreateConversation(context.Background(), coreagent.CreateConversationInput{
		ID:         "conv_owner",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		CreatedBy:  owner.Username,
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	_, guestSession, err := deps.authLogic.Login(context.Background(), "guest", "secret-123")
	if err == nil {
		t.Fatal("guest login unexpectedly succeeded before registration")
	}
	if _, regErr := deps.authLogic.Register(context.Background(), "guest", "secret-123", "secret-123"); regErr != nil {
		t.Fatalf("Register(guest) error = %v", regErr)
	}
	_, guestSession, err = deps.authLogic.Login(context.Background(), "guest", "secret-123")
	if err != nil {
		t.Fatalf("Login(guest) error = %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/tasks", bytes.NewReader(mustJSON(t, map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"conversation_id": conversation.ID,
			"provider_id":     "openai",
			"model_id":        "gpt-5.4",
			"message":         "hello",
		},
	})))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: guestSession.ID})

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("cross-user task create ok = true, want false")
	}
	if !strings.Contains(envelope.Message, "无权") {
		t.Fatalf("message = %q, want ownership denial", envelope.Message)
	}
}

type authHandlerTestDeps struct {
	authLogic         *logics.AuthLogic
	conversationStore *coreagent.ConversationStore
}

func newAuthHandlerTestServer(t *testing.T) *httptest.Server {
	_, server := newAuthHandlerTestServerWithDeps(t)
	return server
}

func newAuthHandlerTestServerWithDeps(t *testing.T) (*authHandlerTestDeps, *httptest.Server) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	conversationStore := coreagent.NewConversationStore(db)
	if err := conversationStore.AutoMigrate(); err != nil {
		t.Fatalf("conversation AutoMigrate() error = %v", err)
	}
	taskStore := coretasks.NewStore(db)
	if err := taskStore.AutoMigrate(); err != nil {
		t.Fatalf("task AutoMigrate() error = %v", err)
	}
	taskManager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{RunnerID: "auth-test"})
	authLogic := newAuthLogicForTest(t, db)
	authMiddleware := NewAuthMiddleware(authLogic)

	engine := rest.Init()
	group := engine.Group("/api/v1")
	NewAuthHandler(authLogic).Register(group)
	NewConversationHandler(conversationStore, authMiddleware.RequireSession()).Register(group)
	NewTaskHandler(taskManager, conversationStore, authMiddleware.RequireSession()).Register(group)

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return &authHandlerTestDeps{authLogic: authLogic, conversationStore: conversationStore}, server
}

func newAuthLogicForTest(t *testing.T, db *gorm.DB) *logics.AuthLogic {
	t.Helper()
	logic, err := logics.NewAuthLogic(db, logics.AuthConfig{CookieName: authSessionCookieName})
	if err != nil {
		t.Fatalf("NewAuthLogic() error = %v", err)
	}
	if err := logic.AutoMigrate(); err != nil {
		t.Fatalf("AuthLogic.AutoMigrate() error = %v", err)
	}
	return logic
}

func postAuthJSON(t *testing.T, url string, payload map[string]any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	response, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("http.Post() error = %v", err)
	}
	return response
}

func decodeAuthUserResponse(t *testing.T, body io.Reader) authTestUser {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var user authTestUser
	if err := json.Unmarshal(envelope.Data, &user); err != nil {
		t.Fatalf("json.Unmarshal(user) error = %v", err)
	}
	return user
}

func decodeEnvelope(t *testing.T, body io.Reader) taskTestResponse {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("json.NewDecoder() error = %v", err)
	}
	return envelope
}

func mustFindCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if strings.TrimSpace(cookie.Name) == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q not found", name)
	return nil
}

func mustJSON(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return raw
}
