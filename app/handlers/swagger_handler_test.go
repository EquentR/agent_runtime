package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EquentR/agent_runtime/pkg/rest"
)

// TestSwaggerUIRoutesExposeHTMLAndGeneratedDocs 验证 Swagger UI 路由会暴露 HTML 页面与生成后的文档文件。
func TestSwaggerUIRoutesExposeHTMLAndGeneratedDocs(t *testing.T) {
	engine := rest.Init()
	NewSwaggerHandler().Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL + "/api/v1/swagger")
	if err != nil {
		t.Fatalf("GET /swagger error = %v", err)
	}
	defer response.Body.Close()
	if response.Request.URL.Path != "/api/v1/swagger/index.html" {
		t.Fatalf("redirect path = %q, want %q", response.Request.URL.Path, "/api/v1/swagger/index.html")
	}

	htmlResponse, err := http.Get(server.URL + "/api/v1/swagger/index.html")
	if err != nil {
		t.Fatalf("GET /swagger/index.html error = %v", err)
	}
	defer htmlResponse.Body.Close()
	htmlBody, err := io.ReadAll(htmlResponse.Body)
	if err != nil {
		t.Fatalf("ReadAll(html) error = %v", err)
	}
	if !strings.Contains(string(htmlBody), "SwaggerUIBundle") {
		t.Fatalf("html body = %q, want SwaggerUIBundle", string(htmlBody))
	}
	if !strings.Contains(string(htmlBody), "swagger.json") {
		t.Fatalf("html body = %q, want swagger.json reference", string(htmlBody))
	}

	jsonResponse, err := http.Get(server.URL + "/api/v1/swagger/swagger.json")
	if err != nil {
		t.Fatalf("GET /swagger/swagger.json error = %v", err)
	}
	defer jsonResponse.Body.Close()
	jsonBody, err := io.ReadAll(jsonResponse.Body)
	if err != nil {
		t.Fatalf("ReadAll(json) error = %v", err)
	}
	if !strings.Contains(string(jsonBody), "Agent Runtime API") {
		t.Fatalf("swagger json = %q, want Agent Runtime API title", string(jsonBody))
	}
}
