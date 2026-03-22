package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coretasks "github.com/EquentR/agent_runtime/core/tasks"
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

func TestSwaggerUIRoutesExposeAuditPathsInGeneratedDocs(t *testing.T) {
	engine := rest.Init()
	NewSwaggerHandler().Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL + "/api/v1/swagger/swagger.json")
	if err != nil {
		t.Fatalf("GET /swagger/swagger.json error = %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(json) error = %v", err)
	}
	if !strings.Contains(string(body), "/audit/runs/{id}") {
		t.Fatalf("swagger json = %q, want /audit/runs/{id}", string(body))
	}
	if !strings.Contains(string(body), "/audit/runs/{id}/events") {
		t.Fatalf("swagger json = %q, want /audit/runs/{id}/events", string(body))
	}
	if !strings.Contains(string(body), "/audit/runs/{id}/replay") {
		t.Fatalf("swagger json = %q, want /audit/runs/{id}/replay", string(body))
	}
}

func TestSwaggerUIRoutesExposeAuditStatusEnumInGeneratedDocs(t *testing.T) {
	engine := rest.Init()
	NewSwaggerHandler().Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL + "/api/v1/swagger/swagger.json")
	if err != nil {
		t.Fatalf("GET /swagger/swagger.json error = %v", err)
	}
	defer response.Body.Close()

	var document map[string]any
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatalf("Decode(swagger.json) error = %v", err)
	}
	definitions, ok := document["definitions"].(map[string]any)
	if !ok {
		t.Fatalf("definitions = %#v, want object", document["definitions"])
	}
	auditRun, ok := definitions["handlers.AuditRunSwaggerDoc"].(map[string]any)
	if !ok {
		t.Fatalf("handlers.AuditRunSwaggerDoc = %#v, want object", definitions["handlers.AuditRunSwaggerDoc"])
	}
	properties, ok := auditRun["properties"].(map[string]any)
	if !ok {
		t.Fatalf("AuditRunSwaggerDoc.properties = %#v, want object", auditRun["properties"])
	}
	statusSchema, ok := properties["status"].(map[string]any)
	if !ok {
		t.Fatalf("AuditRunSwaggerDoc.status = %#v, want object", properties["status"])
	}

	got := swaggerEnumValues(t, statusSchema["enum"])
	want := []string{
		string(coretasks.StatusQueued),
		string(coretasks.StatusRunning),
		string(coretasks.StatusCancelRequested),
		string(coretasks.StatusCancelled),
		string(coretasks.StatusSucceeded),
		string(coretasks.StatusFailed),
	}
	if !equalSwaggerStringSlices(got, want) {
		t.Fatalf("audit status enum = %v, want %v", got, want)
	}
}

func swaggerEnumValues(t *testing.T, raw any) []string {
	t.Helper()
	values, ok := raw.([]any)
	if !ok {
		t.Fatalf("enum = %#v, want array", raw)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		item, ok := value.(string)
		if !ok {
			t.Fatalf("enum entry = %#v, want string", value)
		}
		result = append(result, item)
	}
	return result
}

func equalSwaggerStringSlices(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
