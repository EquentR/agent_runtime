package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/EquentR/agent_runtime/app/models"
	coreskills "github.com/EquentR/agent_runtime/core/skills"
	"github.com/EquentR/agent_runtime/core/workspaces"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type skillTestEnvelope struct {
	Code    int             `json:"code"`
	OK      bool            `json:"ok"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestSkillHandlerListReturnsVisibleWorkspaceSkills(t *testing.T) {
	server := newSkillHandlerTestServer(t, map[string]string{
		"debugging": "---\ndescription: Debug skill\n---\n\n# Debugging\n\nDebug carefully.\n",
		"review":    "---\ndescription: Review skill\n---\n\n# Review\n\nReview carefully.\n",
	})

	response, body := skillHandlerRequest(t, server, http.MethodGet, "/api/v1/skills")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", response.StatusCode, body)
	}
	env := decodeSkillEnvelope(t, body)
	var items []map[string]any
	if err := json.Unmarshal(env.Data, &items); err != nil {
		t.Fatalf("Unmarshal(list) error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0]["name"] != "debugging" || items[1]["name"] != "review" {
		t.Fatalf("items = %#v, want sorted [debugging review]", items)
	}
	if _, ok := items[0]["title"]; ok {
		t.Fatalf("list item contains title: %#v", items[0])
	}
	if _, ok := items[0]["content"]; ok {
		t.Fatalf("list item contains content: %#v", items[0])
	}
}

func TestSkillHandlerGetReturnsWorkspaceSkillDetail(t *testing.T) {
	server := newSkillHandlerTestServer(t, map[string]string{
		"debugging": "---\ndescription: Debug skill\n---\n\n# Debugging\n\nDebug carefully.\n",
	})

	response, body := skillHandlerRequest(t, server, http.MethodGet, "/api/v1/skills/debugging")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", response.StatusCode, body)
	}
	env := decodeSkillEnvelope(t, body)
	var item map[string]any
	if err := json.Unmarshal(env.Data, &item); err != nil {
		t.Fatalf("Unmarshal(detail) error = %v", err)
	}
	if item["name"] != "debugging" {
		t.Fatalf("item.name = %#v, want debugging", item["name"])
	}
	if _, ok := item["title"]; ok {
		t.Fatalf("detail contains title: %#v", item)
	}
	if item["content"] != "# Debugging\n\nDebug carefully.\n" {
		t.Fatalf("item.content = %#v, want markdown body", item["content"])
	}
}

func TestSkillHandlerGetReturnsNotFoundForUnknownSkill(t *testing.T) {
	server := newSkillHandlerTestServer(t, map[string]string{})

	response, body := skillHandlerRequest(t, server, http.MethodGet, "/api/v1/skills/missing")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("http status = %d, want 200, body = %s", response.StatusCode, body)
	}
	env := decodeSkillEnvelope(t, body)
	if env.Code != http.StatusNotFound {
		t.Fatalf("envelope.Code = %d, want %d", env.Code, http.StatusNotFound)
	}
}

func TestSkillHandlerListFiltersHiddenSkills(t *testing.T) {
	server := newSkillHandlerTestServer(t, map[string]string{
		"debugging":       "# Debugging\n\nDebug carefully.\n",
		"internal-review": "---\nhidden: true\n---\n\n# Internal Review\n\nReview carefully.\n",
	})

	response, body := skillHandlerRequest(t, server, http.MethodGet, "/api/v1/skills")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", response.StatusCode, body)
	}
	env := decodeSkillEnvelope(t, body)
	var items []map[string]any
	if err := json.Unmarshal(env.Data, &items); err != nil {
		t.Fatalf("Unmarshal(list) error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0]["name"] != "debugging" {
		t.Fatalf("items[0].name = %#v, want debugging", items[0]["name"])
	}
}

func TestSkillHandlerGetAllowsHiddenSkillByName(t *testing.T) {
	server := newSkillHandlerTestServer(t, map[string]string{
		"internal-review": "---\nhidden: true\n---\n\n# Internal Review\n\nReview carefully.\n",
	})

	response, body := skillHandlerRequest(t, server, http.MethodGet, "/api/v1/skills/internal-review")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", response.StatusCode, body)
	}
	env := decodeSkillEnvelope(t, body)
	var item map[string]any
	if err := json.Unmarshal(env.Data, &item); err != nil {
		t.Fatalf("Unmarshal(detail) error = %v", err)
	}
	if item["name"] != "internal-review" {
		t.Fatalf("item.name = %#v, want internal-review", item["name"])
	}
}

func TestSkillHandlerListUsesAuthenticatedUserHomeWorkspace(t *testing.T) {
	templateRoot := t.TempDir()
	writeTemplateFileForHandlerTest(t, templateRoot, "AGENTS.md", "# Template rules\n")
	writeSkillDocForHandlerTest(t, templateRoot, "template-only", "# Template Only\n")
	manager := newSkillHandlerWorkspaceManager(t, templateRoot, t.TempDir())

	aliceHome, err := manager.EnsureHomeWorkspace(t.Context(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace(alice) error = %v", err)
	}
	bobHome, err := manager.EnsureHomeWorkspace(t.Context(), "7")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace(bob) error = %v", err)
	}
	writeSkillDocForHandlerTest(t, aliceHome.Root, "alice-only", "---\ndescription: Alice skill\n---\n\n# Alice Skill\n")
	writeSkillDocForHandlerTest(t, bobHome.Root, "bob-only", "---\ndescription: Bob skill\n---\n\n# Bob Skill\n")

	server := newWorkspaceSkillHandlerTestServer(t, manager, 42, "alice")
	response, body := skillHandlerRequest(t, server, http.MethodGet, "/api/v1/skills")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", response.StatusCode, body)
	}
	env := decodeSkillEnvelope(t, body)
	var items []map[string]any
	if err := json.Unmarshal(env.Data, &items); err != nil {
		t.Fatalf("Unmarshal(list) error = %v", err)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item["name"].(string))
	}
	if containsString(names, "bob-only") {
		t.Fatalf("skills = %v, want no bob-only skill", names)
	}
	if !containsString(names, "alice-only") {
		t.Fatalf("skills = %v, want alice-only skill from authenticated user's home workspace", names)
	}
}

func newSkillHandlerTestServer(t *testing.T, docs map[string]string) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	workspaceRoot := t.TempDir()
	for name, content := range docs {
		writeSkillDocForHandlerTest(t, workspaceRoot, name, content)
	}
	loader := coreskills.NewLoader(workspaceRoot)
	router := rest.Init()
	api := router.Group("/api/v1")
	NewSkillHandler(loader).Register(api)
	return httptest.NewServer(router)
}

func newWorkspaceSkillHandlerTestServer(t *testing.T, manager *workspaces.Manager, userID uint64, username string) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := rest.Init()
	api := router.Group("/api/v1")
	middleware := func(c *gin.Context) {
		c.Set(authUserContextKey, &models.User{ID: userID, Username: username, Status: models.UserStatusActive})
		c.Next()
	}
	NewSkillHandler(nil, middleware).WithWorkspaceManager(manager).Register(api)
	return httptest.NewServer(router)
}

func newSkillHandlerWorkspaceManager(t *testing.T, templateRoot string, workspacesRoot string) *workspaces.Manager {
	t.Helper()
	manager, err := workspaces.NewManager(workspaces.Config{
		TemplateRoot: templateRoot,
		Root:         workspacesRoot,
	})
	if err != nil {
		t.Fatalf("workspaces.NewManager() error = %v", err)
	}
	return manager
}

func writeTemplateFileForHandlerTest(t *testing.T, root string, name string, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func writeSkillDocForHandlerTest(t *testing.T, workspaceRoot, name, content string) {
	t.Helper()
	dir := filepath.Join(workspaceRoot, "skills", name)
	path := filepath.Join(dir, "SKILL.md")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func skillHandlerRequest(t *testing.T, server *httptest.Server, method, path string) (*http.Response, string) {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	return response, string(body)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func decodeSkillEnvelope(t *testing.T, body string) skillTestEnvelope {
	t.Helper()
	var env skillTestEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("Unmarshal(envelope) error = %v, body = %s", err, body)
	}
	return env
}
