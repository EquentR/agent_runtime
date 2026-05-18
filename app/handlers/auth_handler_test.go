package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/mail"
	"github.com/EquentR/agent_runtime/pkg/rest"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type authTestUser struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func TestAuthHandlerRegisterLoginLogoutFlow(t *testing.T) {
	server := newAuthHandlerTestServer(t)

	register := postAuthJSON(t, server.URL+"/api/v1/auth/register", map[string]any{
		"username":         "alice",
		"email":            "alice@example.com",
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
	if registered.Role != "admin" {
		t.Fatalf("registered role = %q, want admin", registered.Role)
	}

	duplicate := postAuthJSON(t, server.URL+"/api/v1/auth/register", map[string]any{
		"username":         "alice",
		"email":            "alice@example.com",
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
	if loggedIn.Role != "admin" {
		t.Fatalf("login role = %q, want admin", loggedIn.Role)
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
	if current.Role != "admin" {
		t.Fatalf("current role = %q, want admin", current.Role)
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
	user, err := authLogic.RegisterWithInput(context.Background(), logics.RegisterInput{
		Username:        "alice",
		Email:           "alice@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if err != nil {
		t.Fatalf("RegisterWithInput() error = %v", err)
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

	owner := registerVerifiedAuthHandlerTestUser(t, deps, "owner")
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
	registerVerifiedAuthHandlerTestUser(t, deps, "guest")
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

func TestAuthMiddlewareRejectsAdminTaskCreationAgainstAnotherUsersConversation(t *testing.T) {
	deps, server := newAuthHandlerTestServerWithDeps(t)

	admin := registerVerifiedAuthHandlerTestUser(t, deps, "admin")
	if admin.Role != "admin" {
		t.Fatalf("admin.Role = %q, want admin", admin.Role)
	}
	owner := registerVerifiedAuthHandlerTestUser(t, deps, "owner")
	conversation, err := deps.conversationStore.CreateConversation(context.Background(), coreagent.CreateConversationInput{
		ID:         "conv_owner",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		CreatedBy:  owner.Username,
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	_, adminSession, err := deps.authLogic.Login(context.Background(), admin.Username, "secret-123")
	if err != nil {
		t.Fatalf("Login(admin) error = %v", err)
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
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: adminSession.ID})

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("admin cross-user task create ok = true, want false")
	}
	if envelope.Code != http.StatusUnauthorized {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(envelope.Message, "无权") {
		t.Fatalf("message = %q, want ownership denial", envelope.Message)
	}
}

func TestAuthMiddlewareCanonicalizesNestedCreatedByOnTaskCreate(t *testing.T) {
	deps, server := newAuthHandlerTestServerWithDeps(t)

	owner := registerVerifiedAuthHandlerTestUser(t, deps, "owner")
	_, ownerSession, err := deps.authLogic.Login(context.Background(), owner.Username, "secret-123")
	if err != nil {
		t.Fatalf("Login(owner) error = %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/tasks", bytes.NewReader(mustJSON(t, map[string]any{
		"task_type":  "agent.run",
		"created_by": "spoofed-top",
		"input": map[string]any{
			"provider_id": "openai",
			"model_id":    "gpt-5.4",
			"message":     "hello",
			"created_by":  "spoofed-nested",
		},
	})))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: ownerSession.ID})

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	created := decodeTaskResponse(t, response.Body)
	task, err := deps.taskManager.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.CreatedBy != owner.Username {
		t.Fatalf("task.CreatedBy = %q, want %q", task.CreatedBy, owner.Username)
	}
	decodedInput := decodeJSONRaw(t, task.InputJSON)
	if decodedInput["created_by"] != owner.Username {
		t.Fatalf("input.created_by = %#v, want %q", decodedInput["created_by"], owner.Username)
	}
}

func TestAuthHandlerTurnstileProtectsLoginRegisterAndVerificationSend(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	authLogic := newAuthLogicForTest(t, db)
	verifier := &fakeTurnstileVerifier{err: errors.New("turnstile rejected")}
	settings := &fakeAuthHandlerSettings{turnstile: logics.TurnstileSettings{
		Enabled:             true,
		ProtectLogin:        true,
		ProtectRegistration: true,
		ProtectVerification: true,
	}}

	engine := rest.Init()
	group := engine.Group("/api/v1")
	NewAuthHandler(
		authLogic,
		WithAuthHandlerSettings(settings),
		WithAuthHandlerTurnstileVerifier(verifier),
	).Register(group)

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	requests := []struct {
		name    string
		path    string
		payload map[string]any
		token   string
	}{
		{
			name: "register",
			path: "/api/v1/auth/register",
			payload: map[string]any{
				"username":         "alice",
				"email":            "alice@example.com",
				"password":         "secret-123",
				"confirm_password": "secret-123",
				"turnstile_token":  "register-token",
			},
			token: "register-token",
		},
		{
			name: "login",
			path: "/api/v1/auth/login",
			payload: map[string]any{
				"username":        "alice",
				"password":        "secret-123",
				"turnstile_token": "login-token",
			},
			token: "login-token",
		},
		{
			name: "verification send",
			path: "/api/v1/auth/email-verification/send",
			payload: map[string]any{
				"email":           "alice@example.com",
				"purpose":         logics.EmailVerificationPurposeRegistration,
				"turnstile_token": "verification-token",
			},
			token: "verification-token",
		},
	}

	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			response := postAuthJSON(t, server.URL+request.path, request.payload)
			defer response.Body.Close()
			if decodeEnvelope(t, response.Body).OK {
				t.Fatalf("%s ok = true, want false", request.name)
			}
		})
	}

	if len(verifier.calls) != len(requests) {
		t.Fatalf("turnstile calls = %d, want %d", len(verifier.calls), len(requests))
	}
	for idx, request := range requests {
		if verifier.calls[idx].token != request.token {
			t.Fatalf("turnstile call %d token = %q, want %q", idx, verifier.calls[idx].token, request.token)
		}
		if verifier.calls[idx].remoteIP == "" {
			t.Fatalf("turnstile call %d remote IP is empty", idx)
		}
	}
}

type authHandlerTestDeps struct {
	authLogic         *logics.AuthLogic
	emailVerification *logics.EmailVerificationLogic
	conversationStore *coreagent.ConversationStore
	taskManager       *coretasks.Manager
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
	mailer := &fakeHandlerMailSender{}
	emailVerification, err := logics.NewEmailVerificationLogic(db, logics.EmailVerificationConfig{
		Sender: mailer,
		CodeGenerator: func() (string, error) {
			return "123456", nil
		},
	})
	if err != nil {
		t.Fatalf("NewEmailVerificationLogic() error = %v", err)
	}
	authLogic, err := logics.NewAuthLogic(db, logics.AuthConfig{CookieName: authSessionCookieName}, logics.WithAuthEmailVerification(emailVerification))
	if err != nil {
		t.Fatalf("NewAuthLogic() error = %v", err)
	}
	if err := authLogic.AutoMigrate(); err != nil {
		t.Fatalf("AuthLogic.AutoMigrate() error = %v", err)
	}
	if err := db.AutoMigrate(&models.EmailVerification{}); err != nil {
		t.Fatalf("EmailVerification AutoMigrate() error = %v", err)
	}
	authMiddleware := NewAuthMiddleware(authLogic)

	engine := rest.Init()
	group := engine.Group("/api/v1")
	NewAuthHandler(authLogic, WithAuthHandlerEmailVerification(emailVerification)).Register(group)
	NewConversationHandler(conversationStore, nil, authMiddleware.RequireSession()).Register(group)
	NewTaskHandler(taskManager, conversationStore, authMiddleware.RequireSession()).Register(group)

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return &authHandlerTestDeps{authLogic: authLogic, emailVerification: emailVerification, conversationStore: conversationStore, taskManager: taskManager}, server
}

func newAuthLogicForTest(t *testing.T, db *gorm.DB) *logics.AuthLogic {
	t.Helper()
	mailer := &fakeHandlerMailSender{}
	emailVerification, err := logics.NewEmailVerificationLogic(db, logics.EmailVerificationConfig{
		Sender: mailer,
		CodeGenerator: func() (string, error) {
			return "123456", nil
		},
	})
	if err != nil {
		t.Fatalf("NewEmailVerificationLogic() error = %v", err)
	}
	logic, err := logics.NewAuthLogic(db, logics.AuthConfig{CookieName: authSessionCookieName}, logics.WithAuthEmailVerification(emailVerification))
	if err != nil {
		t.Fatalf("NewAuthLogic() error = %v", err)
	}
	if err := logic.AutoMigrate(); err != nil {
		t.Fatalf("AuthLogic.AutoMigrate() error = %v", err)
	}
	if err := db.AutoMigrate(&models.EmailVerification{}); err != nil {
		t.Fatalf("EmailVerification AutoMigrate() error = %v", err)
	}
	return logic
}

func registerActiveAuthUserForTest(t *testing.T, logic *logics.AuthLogic, username, password string) *models.User {
	t.Helper()

	ctx := context.Background()
	user, err := logic.RegisterWithInput(ctx, logics.RegisterInput{
		Username:        username,
		Email:           username + "@example.com",
		Password:        password,
		ConfirmPassword: password,
	})
	if err != nil {
		t.Fatalf("RegisterWithInput(%q) error = %v", username, err)
	}
	if user.Status == models.UserStatusPendingEmailVerification {
		verification := logic.EmailVerification()
		if verification == nil {
			t.Fatalf("EmailVerification() = nil for pending user %q", username)
		}
		user, err = verification.Verify(ctx, logics.VerifyEmailInput{
			UserID:  user.ID,
			Email:   user.Email,
			Purpose: logics.EmailVerificationPurposeRegistration,
			Code:    "123456",
		})
		if err != nil {
			t.Fatalf("Verify(%q) error = %v", username, err)
		}
	}
	if user.Status != models.UserStatusActive {
		t.Fatalf("registered user %q status = %q, want %q", username, user.Status, models.UserStatusActive)
	}
	if user.EmailVerifiedAt == nil {
		t.Fatalf("registered user %q EmailVerifiedAt = nil, want verified timestamp", username)
	}
	return user
}

type fakeHandlerMailSender struct {
	messages []mail.Message
}

func (s *fakeHandlerMailSender) Send(ctx context.Context, message mail.Message) error {
	s.messages = append(s.messages, message)
	return nil
}

type fakeTurnstileCall struct {
	token    string
	remoteIP string
}

type fakeTurnstileVerifier struct {
	calls []fakeTurnstileCall
	err   error
}

func (v *fakeTurnstileVerifier) Verify(ctx context.Context, token string, remoteIP string) error {
	v.calls = append(v.calls, fakeTurnstileCall{token: token, remoteIP: remoteIP})
	return v.err
}

type fakeAuthHandlerSettings struct {
	turnstile logics.TurnstileSettings
}

func (s *fakeAuthHandlerSettings) GetTurnstile(ctx context.Context) (logics.TurnstileSettings, error) {
	return s.turnstile, nil
}

func registerVerifiedAuthHandlerTestUser(t *testing.T, deps *authHandlerTestDeps, username string) *models.User {
	t.Helper()

	email := username + "@example.com"
	user, err := deps.authLogic.RegisterWithInput(context.Background(), logics.RegisterInput{
		Username:        username,
		Email:           email,
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if err != nil {
		t.Fatalf("RegisterWithInput(%s) error = %v", username, err)
	}
	if user.Status == models.UserStatusPendingEmailVerification {
		user, err = deps.emailVerification.Verify(context.Background(), logics.VerifyEmailInput{
			UserID:  user.ID,
			Email:   email,
			Purpose: logics.EmailVerificationPurposeRegistration,
			Code:    "123456",
		})
		if err != nil {
			t.Fatalf("VerifyEmail(%s) error = %v", username, err)
		}
	}
	return user
}

func postAuthJSON(t *testing.T, url string, payload map[string]any) *http.Response {
	t.Helper()
	autoVerifyEmail := false
	if strings.Contains(url, "/auth/register") {
		if _, ok := payload["email"]; !ok {
			if username, ok := payload["username"].(string); ok && strings.TrimSpace(username) != "" {
				payload["email"] = strings.TrimSpace(username) + "@example.com"
				autoVerifyEmail = true
			}
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	response, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("http.Post() error = %v", err)
	}
	if autoVerifyEmail {
		autoVerifyAuthRegistration(t, url, response)
	}
	return response
}

func autoVerifyAuthRegistration(t *testing.T, registerURL string, response *http.Response) {
	t.Helper()
	if response == nil || response.Body == nil {
		return
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read register response body: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close register response body: %v", err)
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	if response.StatusCode != http.StatusOK {
		return
	}

	var envelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || !envelope.OK {
		return
	}
	var user struct {
		ID     uint64 `json:"id"`
		Email  string `json:"email"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(envelope.Data, &user); err != nil {
		return
	}
	if user.Status != models.UserStatusPendingEmailVerification {
		return
	}
	verifyURL := strings.Replace(registerURL, "/auth/register", "/auth/email-verification/verify", 1)
	verifyRaw, err := json.Marshal(map[string]any{
		"user_id": user.ID,
		"email":   user.Email,
		"purpose": logics.EmailVerificationPurposeRegistration,
		"code":    "123456",
	})
	if err != nil {
		t.Fatalf("marshal verification payload: %v", err)
	}
	verifyResponse, err := http.Post(verifyURL, "application/json", bytes.NewReader(verifyRaw))
	if err != nil {
		t.Fatalf("auto verify registration: %v", err)
	}
	defer verifyResponse.Body.Close()
	if verifyResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(verifyResponse.Body)
		t.Fatalf("auto verify status = %d, body = %s", verifyResponse.StatusCode, string(body))
	}
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
