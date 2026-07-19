package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	coreupdater "github.com/EquentR/agent_runtime/core/updater"
	"github.com/gin-gonic/gin"
)

type fakeAdminUpdateService struct {
	checkCalls  int
	installErr  error
	rollbackErr error
}

func (f *fakeAdminUpdateService) Status(context.Context) (logics.UpdateStatus, error) {
	return logics.UpdateStatus{Current: coreupdater.BuildInfo{Version: "v1.2.3"}, State: coreupdater.OperationState{Phase: coreupdater.PhaseIdle}}, nil
}
func (f *fakeAdminUpdateService) Check(context.Context) (logics.UpdateStatus, error) {
	f.checkCalls++
	return logics.UpdateStatus{Current: coreupdater.BuildInfo{Version: "v1.2.3"}, UpdateAvailable: true}, nil
}
func (f *fakeAdminUpdateService) Authorize(context.Context, *models.User, *models.UserSession, string, string, string) (string, time.Time, error) {
	return "one-time-token", time.Now().Add(time.Minute), nil
}
func (f *fakeAdminUpdateService) Install(context.Context, *models.User, *models.UserSession, string, string, string, coreupdater.BackupMode) (coreupdater.OperationState, error) {
	return coreupdater.OperationState{OperationID: "op-1", Phase: coreupdater.PhaseDraining}, f.installErr
}
func (f *fakeAdminUpdateService) ForceInstall(context.Context, *models.User, *models.UserSession, string, string, string, coreupdater.BackupMode) (coreupdater.OperationState, error) {
	return coreupdater.OperationState{OperationID: "op-force", Phase: coreupdater.PhaseDraining}, f.installErr
}
func (f *fakeAdminUpdateService) Rollback(context.Context, *models.User, *models.UserSession, string, string, string) (coreupdater.OperationState, error) {
	return coreupdater.OperationState{OperationID: "op-2", Phase: coreupdater.PhaseRollingBack}, f.rollbackErr
}

func TestAdminUpdateHandlerRequiresSameOriginCSRFForCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeAdminUpdateService{}
	engine := gin.New()
	auth := NewAuthMiddleware(nil)
	actor := func(c *gin.Context) {
		c.Set(authUserContextKey, &models.User{ID: 1, Username: "admin"})
		c.Set(authSessionContextKey, &models.UserSession{ID: "sess-1", UserID: 1})
		c.Next()
	}
	NewAdminUpdateHandler(service, auth, nil, actor).Register(engine.Group("/api/v1"))

	statusRecorder := httptest.NewRecorder()
	engine.ServeHTTP(statusRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/updates/status", nil))
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status HTTP = %d", statusRecorder.Code)
	}
	var envelope struct {
		Data struct {
			CSRF string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &envelope); err != nil || envelope.Data.CSRF == "" {
		t.Fatalf("status response = %s, err=%v", statusRecorder.Body.String(), err)
	}
	cookies := statusRecorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("status response did not set CSRF cookie")
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/updates/check", bytes.NewReader([]byte(`{}`)))
	request.Host = "example.test"
	request.Header.Set("Origin", "http://example.test")
	request.Header.Set(updateCSRFHeader, envelope.Data.CSRF)
	request.AddCookie(cookies[0])
	checkRecorder := httptest.NewRecorder()
	engine.ServeHTTP(checkRecorder, request)
	if service.checkCalls != 1 {
		t.Fatalf("check calls = %d, response=%s", service.checkCalls, checkRecorder.Body.String())
	}

	badRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/updates/check", bytes.NewReader([]byte(`{}`)))
	badRequest.Host = "example.test"
	badRequest.Header.Set("Origin", "https://evil.test")
	badRequest.Header.Set(updateCSRFHeader, envelope.Data.CSRF)
	badRequest.AddCookie(cookies[0])
	badRecorder := httptest.NewRecorder()
	engine.ServeHTTP(badRecorder, badRequest)
	if service.checkCalls != 1 {
		t.Fatalf("cross-origin check was executed, calls=%d", service.checkCalls)
	}
}

func TestAdminUpdateHandlerDoesNotReportFailedInstallOrRollbackAsAccepted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeAdminUpdateService{}
	engine := gin.New()
	auth := NewAuthMiddleware(nil)
	actor := func(c *gin.Context) {
		c.Set(authUserContextKey, &models.User{ID: 1, Username: "admin"})
		c.Set(authSessionContextKey, &models.UserSession{ID: "sess-1", UserID: 1})
		c.Next()
	}
	NewAdminUpdateHandler(service, auth, nil, actor).Register(engine.Group("/api/v1"))

	statusRecorder := httptest.NewRecorder()
	engine.ServeHTTP(statusRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/updates/status", nil))
	var statusEnvelope struct {
		Data struct {
			CSRF string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &statusEnvelope); err != nil {
		t.Fatal(err)
	}
	cookie := statusRecorder.Result().Cookies()[0]

	for _, test := range []struct {
		path        string
		body        string
		code        int
		installErr  error
		rollbackErr error
	}{
		{path: "/api/v1/admin/updates/install", body: `{"authorization_token":"bad","target":"v2.0.0"}`, code: http.StatusUnauthorized, installErr: fmt.Errorf("%w: token is invalid", coreupdater.ErrOperationAuthorization)},
		{path: "/api/v1/admin/updates/rollback", body: `{"authorization_token":"token","target":"backup-1"}`, code: http.StatusConflict, rollbackErr: logics.ErrUpdateOperationConflict},
		{path: "/api/v1/admin/updates/install", body: `{"authorization_token":"token","target":"v2.0.0"}`, code: http.StatusConflict, installErr: fmt.Errorf("%w: deadline exceeded", logics.ErrUpdateDrainTimeout)},
		{path: "/api/v1/admin/updates/install", body: `{"authorization_token":"token","target":"stale"}`, code: http.StatusBadRequest, installErr: fmt.Errorf("%w: stale target", logics.ErrInvalidUpdateRequest)},
	} {
		service.installErr = test.installErr
		service.rollbackErr = test.rollbackErr
		request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
		request.Host = "example.test"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://example.test")
		request.Header.Set(updateCSRFHeader, statusEnvelope.Data.CSRF)
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		var envelope struct {
			Code int  `json:"code"`
			OK   bool `json:"ok"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("%s response = %s: %v", test.path, recorder.Body.String(), err)
		}
		if envelope.Code != test.code || envelope.OK {
			t.Fatalf("%s envelope = %#v, want code=%d ok=false", test.path, envelope, test.code)
		}
		if recorder.Code != test.code {
			t.Fatalf("%s HTTP status = %d, want %d", test.path, recorder.Code, test.code)
		}
	}
}

func TestAdminUpdateHTTPAuthMiddlewareReturnsRealUnauthorizedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/v1/admin/updates/status", NewAuthMiddleware(nil).RequireAdminHTTP(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/updates/status", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("HTTP status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}
