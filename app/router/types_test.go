package router

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

func TestInitRouterServesEmbeddedFrontend(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	frontend := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>ice art</title>")},
		"assets/app.js": {Data: []byte("console.log('ice_art')")},
	}
	initRouterWithEmbeddedFrontend(engine, nil, "/api/v1", nil, frontend)

	tests := []struct {
		name       string
		path       string
		statusCode int
		wantBody   string
	}{
		{name: "root serves index", path: "/", statusCode: http.StatusOK, wantBody: "ice art"},
		{name: "asset serves asset", path: "/assets/app.js", statusCode: http.StatusOK, wantBody: "ice_art"},
		{name: "spa path serves index", path: "/chat/123", statusCode: http.StatusOK, wantBody: "ice art"},
		{name: "api miss stays json 404", path: "/api/v1/missing", statusCode: http.StatusNotFound, wantBody: "Not Found"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)

			engine.ServeHTTP(recorder, request)

			if recorder.Code != test.statusCode {
				t.Fatalf("status code = %d, want %d", recorder.Code, test.statusCode)
			}
			if !strings.Contains(recorder.Body.String(), test.wantBody) {
				t.Fatalf("body = %q, want substring %q", recorder.Body.String(), test.wantBody)
			}
		})
	}
}

func TestEmbeddedFrontendTakesPrecedenceOverConfiguredStaticPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	frontend := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><title>embedded ice art</title>")},
	}
	staticDir := t.TempDir()
	initRouterWithEmbeddedFrontend(engine, nil, "/api/v1", []rest.Static{{
		Path: "/",
		Dir:  filepath.Join(staticDir, "missing"),
	}}, frontend)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), "embedded ice art") {
		t.Fatalf("body = %q, want embedded frontend", recorder.Body.String())
	}
}

func TestEmbeddedFrontendFSRequiresIndex(t *testing.T) {
	if got := validatedEmbeddedFrontendFS(fstest.MapFS{
		"assets/app.js": {Data: []byte("console.log('ice_art')")},
	}); got != nil {
		t.Fatal("validatedEmbeddedFrontendFS without index.html returned non-nil FS")
	}

	if got := validatedEmbeddedFrontendFS(fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html>")},
	}); got == nil {
		t.Fatal("validatedEmbeddedFrontendFS with index.html returned nil FS")
	}
}
