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

func TestSwaggerUIRoutesExposeModelManagementPathsInGeneratedDocs(t *testing.T) {
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
	document := string(body)
	for _, path := range []string{
		"/admin/models",
		"/admin/models/yaml/{provider_id}/{model_id}",
		"/admin/models/custom",
		"/admin/models/custom/{id}",
		"/admin/models/custom/{id}/test",
		"/users/me/models",
		"/users/me/models/{id}",
		"/users/me/models/{id}/test",
	} {
		if !strings.Contains(document, path) {
			t.Fatalf("swagger json missing %s", path)
		}
	}
}

func TestSwaggerUIRoutesExposePublicSettingsAndProfilePathsInGeneratedDocs(t *testing.T) {
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
	document := string(body)
	for _, path := range []string{
		"/settings/registration",
		"/settings/turnstile",
		"/users/me",
	} {
		if !strings.Contains(document, path) {
			t.Fatalf("swagger json missing %s", path)
		}
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
	auditRun, ok := definitions["app_handlers.AuditRunSwaggerDoc"].(map[string]any)
	if !ok {
		t.Fatalf("app_handlers.AuditRunSwaggerDoc = %#v, want object", definitions["app_handlers.AuditRunSwaggerDoc"])
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
		string(coretasks.StatusWaiting),
		string(coretasks.StatusCancelRequested),
		string(coretasks.StatusCancelled),
		string(coretasks.StatusSucceeded),
		string(coretasks.StatusFailed),
	}
	if !equalSwaggerStringSlices(got, want) {
		t.Fatalf("audit status enum = %v, want %v", got, want)
	}
}

func TestSwaggerJSONIncludesWaitingTaskStatus(t *testing.T) {
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

	assertSwaggerStatusEnumContainsWaiting(t, definitions, "app_handlers.AuditRunSwaggerDoc")
	assertSwaggerStatusEnumContainsWaiting(t, definitions, "app_handlers.TaskSwaggerDoc")
}

func TestSwaggerJSONIncludesApprovalPathsAndDefinitions(t *testing.T) {
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
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths = %#v, want object", document["paths"])
	}
	for _, path := range []string{"/tasks/{id}/approvals", "/tasks/{id}/approvals/{approvalID}/decision"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("swagger paths missing %q", path)
		}
	}
	definitions, ok := document["definitions"].(map[string]any)
	if !ok {
		t.Fatalf("definitions = %#v, want object", document["definitions"])
	}
	for _, definition := range []string{"app_handlers.ApprovalSwaggerDoc", "app_handlers.ApprovalDecisionSwaggerRequest", "app_handlers.ApprovalListSwaggerResponse", "app_handlers.ApprovalSwaggerResponse"} {
		if _, ok := definitions[definition]; !ok {
			t.Fatalf("swagger definitions missing %q", definition)
		}
	}
}

func TestSwaggerJSONIncludesApprovalFailureCodes(t *testing.T) {
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
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths = %#v, want object", document["paths"])
	}
	assertSwaggerPathHasResponses(t, paths, "/tasks/{id}/approvals", "get", "200", "401", "404")
	assertSwaggerPathHasResponses(t, paths, "/tasks/{id}/approvals/{approvalID}/decision", "post", "200", "400", "401", "404")
}

func TestSwaggerJSONIncludesAuditConversationListDefinitions(t *testing.T) {
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
	for _, definition := range []string{"app_handlers.AuditRunListSwaggerResponse", "app_handlers.AuditEventListSwaggerResponse"} {
		if _, ok := definitions[definition]; !ok {
			t.Fatalf("swagger definitions missing %q", definition)
		}
	}
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths = %#v, want object", document["paths"])
	}
	for _, path := range []string{"/audit/conversations/{conversation_id}/runs", "/audit/conversations/{conversation_id}/events"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("swagger paths missing %q", path)
		}
	}
}

func TestSwaggerJSONIncludesWorkspacePathsAndDefinitions(t *testing.T) {
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
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths = %#v, want object", document["paths"])
	}
	for _, path := range []string{
		"/tasks/{id}/workspace/confirm",
		"/tasks/{id}/workspace/discard",
		"/conversations/{id}/workspace",
		"/conversations/{id}/workspace/files",
		"/conversations/{id}/workspace/file",
		"/conversations/{id}/workspace/diff",
		"/conversations/{id}/workspace/download",
		"/conversations/{id}/workspace/confirm",
		"/conversations/{id}/workspace/discard",
		"/admin/workspaces/users/{user_id}",
	} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("swagger paths missing %q", path)
		}
	}
	assertSwaggerPathHasResponses(t, paths, "/tasks/{id}/workspace/confirm", "post", "200", "400", "401", "404", "409")
	assertSwaggerPathHasResponses(t, paths, "/conversations/{id}/workspace", "get", "200", "401", "404")
	assertSwaggerPathHasResponses(t, paths, "/conversations/{id}/workspace/files", "get", "200", "400", "401", "404")
	assertSwaggerPathHasResponses(t, paths, "/conversations/{id}/workspace/file", "get", "200", "400", "401", "404")
	assertSwaggerPathHasResponses(t, paths, "/conversations/{id}/workspace/diff", "get", "200", "400", "401", "404")
	assertSwaggerPathHasResponses(t, paths, "/conversations/{id}/workspace/download", "get", "200", "400", "401", "404")
	assertSwaggerPathHasResponses(t, paths, "/conversations/{id}/workspace/confirm", "post", "200", "400", "401", "404", "409")
	assertSwaggerPathHasResponses(t, paths, "/conversations/{id}/workspace/discard", "post", "200", "400", "401", "404")
	definitions, ok := document["definitions"].(map[string]any)
	if !ok {
		t.Fatalf("definitions = %#v, want object", document["definitions"])
	}
	for _, definition := range []string{
		"app_handlers.WorkspaceStateSwaggerDoc",
		"app_handlers.WorkspaceStateSwaggerResponse",
		"app_handlers.ConversationWorkspaceStateSwaggerResponse",
		"app_handlers.WorkspaceBrowserSnapshotSwaggerDoc",
		"app_handlers.WorkspaceBrowserSnapshotSwaggerResponse",
		"app_handlers.WorkspaceBrowserTreeNodeSwaggerDoc",
		"app_handlers.WorkspaceBrowserFileSwaggerDoc",
		"app_handlers.WorkspaceBrowserFileSwaggerResponse",
		"app_handlers.WorkspaceBrowserDiffSwaggerDoc",
		"app_handlers.WorkspaceBrowserDiffSwaggerResponse",
		"app_handlers.UserWorkspaceSummarySwaggerDoc",
		"app_handlers.TaskWorkspaceSummarySwaggerDoc",
	} {
		if _, ok := definitions[definition]; !ok {
			t.Fatalf("swagger definitions missing %q", definition)
		}
	}
	resultDefinition, ok := definitions["app_handlers.RunTaskResultSwaggerDoc"].(map[string]any)
	if !ok {
		t.Fatalf("app_handlers.RunTaskResultSwaggerDoc = %#v, want object", definitions["app_handlers.RunTaskResultSwaggerDoc"])
	}
	properties, ok := resultDefinition["properties"].(map[string]any)
	if !ok {
		t.Fatalf("RunTaskResultSwaggerDoc.properties = %#v, want object", resultDefinition["properties"])
	}
	for _, property := range []string{"workspace_mode", "workspace_state"} {
		if _, ok := properties[property]; !ok {
			t.Fatalf("RunTaskResultSwaggerDoc properties missing %q", property)
		}
	}

	downloadPath, ok := paths["/conversations/{id}/workspace/download"].(map[string]any)
	if !ok {
		t.Fatalf("workspace download path = %#v, want object", paths["/conversations/{id}/workspace/download"])
	}
	downloadGet, ok := downloadPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("workspace download get = %#v, want object", downloadPath["get"])
	}
	gotProduces := swaggerStringValues(t, downloadGet["produces"])
	if !equalSwaggerStringSlices(gotProduces, []string{"application/octet-stream", "application/zip"}) {
		t.Fatalf("workspace download produces = %v, want octet-stream and zip", gotProduces)
	}

	browserSnapshotDefinition, ok := definitions["app_handlers.WorkspaceBrowserSnapshotSwaggerDoc"].(map[string]any)
	if !ok {
		t.Fatalf("WorkspaceBrowserSnapshotSwaggerDoc = %#v, want object", definitions["app_handlers.WorkspaceBrowserSnapshotSwaggerDoc"])
	}
	browserSnapshotProperties, ok := browserSnapshotDefinition["properties"].(map[string]any)
	if !ok {
		t.Fatalf("WorkspaceBrowserSnapshotSwaggerDoc.properties = %#v, want object", browserSnapshotDefinition["properties"])
	}
	for _, property := range []string{"home_root", "task_root"} {
		if _, ok := browserSnapshotProperties[property]; ok {
			t.Fatalf("WorkspaceBrowserSnapshotSwaggerDoc properties include %q, want hidden", property)
		}
	}
	if _, ok := browserSnapshotProperties["path"]; !ok {
		t.Fatalf("WorkspaceBrowserSnapshotSwaggerDoc properties missing path")
	}

	workspaceStateDefinition, ok := definitions["app_handlers.WorkspaceStateSwaggerDoc"].(map[string]any)
	if !ok {
		t.Fatalf("WorkspaceStateSwaggerDoc = %#v, want object", definitions["app_handlers.WorkspaceStateSwaggerDoc"])
	}
	workspaceStateProperties, ok := workspaceStateDefinition["properties"].(map[string]any)
	if !ok {
		t.Fatalf("WorkspaceStateSwaggerDoc.properties = %#v, want object", workspaceStateDefinition["properties"])
	}
	for _, property := range []string{"home_root", "task_root"} {
		if _, ok := workspaceStateProperties[property]; !ok {
			t.Fatalf("WorkspaceStateSwaggerDoc properties missing %q", property)
		}
	}
}

func assertSwaggerStatusEnumContainsWaiting(t *testing.T, definitions map[string]any, name string) {
	t.Helper()

	definition, ok := definitions[name].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", name, definitions[name])
	}
	properties, ok := definition["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s.properties = %#v, want object", name, definition["properties"])
	}
	statusSchema, ok := properties["status"].(map[string]any)
	if !ok {
		t.Fatalf("%s.status = %#v, want object", name, properties["status"])
	}

	got := swaggerEnumValues(t, statusSchema["enum"])
	for _, value := range got {
		if value == string(coretasks.StatusWaiting) {
			return
		}
	}
	t.Fatalf("%s status enum = %v, want to include %q", name, got, coretasks.StatusWaiting)
}

func swaggerEnumValues(t *testing.T, raw any) []string {
	t.Helper()
	return swaggerStringValues(t, raw)
}

func swaggerStringValues(t *testing.T, raw any) []string {
	t.Helper()
	values, ok := raw.([]any)
	if !ok {
		t.Fatalf("values = %#v, want array", raw)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		item, ok := value.(string)
		if !ok {
			t.Fatalf("value entry = %#v, want string", value)
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

func assertSwaggerPathHasResponses(t *testing.T, paths map[string]any, path string, method string, wantCodes ...string) {
	t.Helper()
	rawPath, ok := paths[path].(map[string]any)
	if !ok {
		t.Fatalf("paths[%q] = %#v, want object", path, paths[path])
	}
	rawMethod, ok := rawPath[method].(map[string]any)
	if !ok {
		t.Fatalf("paths[%q][%q] = %#v, want object", path, method, rawPath[method])
	}
	responses, ok := rawMethod["responses"].(map[string]any)
	if !ok {
		t.Fatalf("responses for %s %s = %#v, want object", method, path, rawMethod["responses"])
	}
	for _, code := range wantCodes {
		if _, ok := responses[code]; !ok {
			t.Fatalf("responses for %s %s missing %s", method, path, code)
		}
	}
}
