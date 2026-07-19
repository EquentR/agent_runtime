package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreupdater "github.com/EquentR/agent_runtime/core/updater"
	"github.com/gin-gonic/gin"
)

func TestHealthHandlerReturnsRawHandshakeResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := coreupdater.NewHealthHandshakeStore(filepath.Join(t.TempDir(), "updates"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Issue("token", "v1.2.3", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	NewHealthHandler(coreupdater.BuildInfo{Version: "v1.2.3"}, store).Register(engine.Group("/api/v1"))

	bad := httptest.NewRecorder()
	engine.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if bad.Code != http.StatusOK {
		t.Fatalf("ordinary health status = %d", bad.Code)
	}
	invalidRequest := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	invalidRequest.Header.Set(coreupdater.UpdateHealthTokenHeader, "wrong")
	invalid := httptest.NewRecorder()
	engine.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusServiceUnavailable {
		t.Fatalf("invalid handshake status = %d", invalid.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set(coreupdater.UpdateHealthTokenHeader, "token")
	good := httptest.NewRecorder()
	engine.ServeHTTP(good, request)
	if good.Code != http.StatusOK || !strings.Contains(good.Body.String(), `"ready":true`) || strings.Contains(good.Body.String(), `"data"`) {
		t.Fatalf("health response = %d %s", good.Code, good.Body.String())
	}
}
