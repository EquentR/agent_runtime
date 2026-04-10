package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	coreskills "github.com/EquentR/agent_runtime/core/skills"
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

func decodeSkillEnvelope(t *testing.T, body string) skillTestEnvelope {
	t.Helper()
	var env skillTestEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("Unmarshal(envelope) error = %v, body = %s", err, body)
	}
	return env
}
