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
	"os"
	"strings"
	"testing"

	"github.com/EquentR/agent_runtime/app/logics"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestPromptHandlerAdminCanListCreateAndUpdateDocuments(t *testing.T) {
	deps, server := newPromptHandlerTestServer(t)
	ctx := context.Background()

	if _, err := deps.store.CreateDocument(ctx, coreprompt.CreateDocumentInput{
		ID:        "doc-seeded",
		Name:      "Seeded",
		Content:   "Seed content",
		Scope:     "admin",
		Status:    "active",
		CreatedBy: "seed",
		UpdatedBy: "seed",
	}); err != nil {
		t.Fatalf("CreateDocument(seed) error = %v", err)
	}

	adminCookie := newPromptHandlerSessionCookie(t, deps.authLogic, deps.adminUsername)

	listResponse := doPromptRequest(t, http.MethodGet, server.URL+"/api/v1/prompts/documents", nil, adminCookie)
	defer listResponse.Body.Close()
	listed := decodePromptDocumentListResponse(t, listResponse.Body)
	if len(listed) != 1 {
		t.Fatalf("len(documents) = %d, want 1", len(listed))
	}
	if listed[0].ID != "doc-seeded" {
		t.Fatalf("listed[0].ID = %q, want doc-seeded", listed[0].ID)
	}

	createResponse := doPromptRequest(t, http.MethodPost, server.URL+"/api/v1/prompts/documents", map[string]any{
		"id":          "doc-created",
		"name":        "Created",
		"description": "Created description",
		"content":     "Created content",
		"scope":       "admin",
		"status":      "active",
	}, adminCookie)
	defer createResponse.Body.Close()
	created := decodePromptDocumentResponse(t, createResponse.Body)
	if created.ID != "doc-created" {
		t.Fatalf("created.ID = %q, want doc-created", created.ID)
	}
	if created.CreatedBy != deps.adminUsername {
		t.Fatalf("created.CreatedBy = %q, want %q", created.CreatedBy, deps.adminUsername)
	}
	if created.UpdatedBy != deps.adminUsername {
		t.Fatalf("created.UpdatedBy = %q, want %q", created.UpdatedBy, deps.adminUsername)
	}

	getResponse := doPromptRequest(t, http.MethodGet, server.URL+"/api/v1/prompts/documents/doc-created", nil, adminCookie)
	defer getResponse.Body.Close()
	got := decodePromptDocumentResponse(t, getResponse.Body)
	if got.Name != "Created" {
		t.Fatalf("got.Name = %q, want Created", got.Name)
	}

	updateResponse := doPromptRequest(t, http.MethodPut, server.URL+"/api/v1/prompts/documents/doc-created", map[string]any{
		"name":        "Updated",
		"description": "Updated description",
		"content":     "Updated content",
		"scope":       "workspace",
		"status":      "disabled",
	}, adminCookie)
	defer updateResponse.Body.Close()
	updated := decodePromptDocumentResponse(t, updateResponse.Body)
	if updated.Name != "Updated" {
		t.Fatalf("updated.Name = %q, want Updated", updated.Name)
	}
	if updated.Scope != "workspace" {
		t.Fatalf("updated.Scope = %q, want workspace", updated.Scope)
	}
	if updated.Status != "disabled" {
		t.Fatalf("updated.Status = %q, want disabled", updated.Status)
	}
	if updated.UpdatedBy != deps.adminUsername {
		t.Fatalf("updated.UpdatedBy = %q, want %q", updated.UpdatedBy, deps.adminUsername)
	}
}

func TestPromptHandlerAdminCanDeleteDocumentAndAssociatedBindings(t *testing.T) {
	deps, server := newPromptHandlerTestServer(t)
	ctx := context.Background()

	if _, err := deps.store.CreateDocument(ctx, coreprompt.CreateDocumentInput{
		ID:        "doc-delete",
		Name:      "Delete",
		Content:   "Delete content",
		Scope:     "admin",
		Status:    "active",
		CreatedBy: "seed",
		UpdatedBy: "seed",
	}); err != nil {
		t.Fatalf("CreateDocument(delete) error = %v", err)
	}
	binding, err := deps.store.CreateBinding(ctx, coreprompt.CreateBindingInput{
		PromptID:  "doc-delete",
		Scene:     "agent.run.default",
		Phase:     "session",
		IsDefault: true,
		Priority:  1,
		Status:    "active",
		CreatedBy: "seed",
		UpdatedBy: "seed",
	})
	if err != nil {
		t.Fatalf("CreateBinding(delete) error = %v", err)
	}

	adminCookie := newPromptHandlerSessionCookie(t, deps.authLogic, deps.adminUsername)
	response := doPromptRequest(t, http.MethodDelete, server.URL+"/api/v1/prompts/documents/doc-delete", nil, adminCookie)
	defer response.Body.Close()

	deleted := decodePromptDeleteResponse(t, response.Body)
	if !deleted.Deleted {
		t.Fatal("deleted.Deleted = false, want true")
	}
	if _, err := deps.store.GetDocument(ctx, "doc-delete"); !errors.Is(err, coreprompt.ErrPromptDocumentNotFound) {
		t.Fatalf("GetDocument(deleted) error = %v, want ErrPromptDocumentNotFound", err)
	}
	if _, err := deps.store.GetBinding(ctx, binding.ID); !errors.Is(err, coreprompt.ErrPromptBindingNotFound) {
		t.Fatalf("GetBinding(cascaded) error = %v, want ErrPromptBindingNotFound", err)
	}
}

func TestPromptHandlerAdminCanListGetCreateUpdateAndDeleteBindings(t *testing.T) {
	deps, server := newPromptHandlerTestServer(t)
	ctx := context.Background()

	if _, err := deps.store.CreateDocument(ctx, coreprompt.CreateDocumentInput{
		ID:        "doc-a",
		Name:      "Doc A",
		Content:   "Doc A content",
		Scope:     "admin",
		Status:    "active",
		CreatedBy: "seed",
		UpdatedBy: "seed",
	}); err != nil {
		t.Fatalf("CreateDocument(doc-a) error = %v", err)
	}
	if _, err := deps.store.CreateDocument(ctx, coreprompt.CreateDocumentInput{
		ID:        "doc-b",
		Name:      "Doc B",
		Content:   "Doc B content",
		Scope:     "admin",
		Status:    "active",
		CreatedBy: "seed",
		UpdatedBy: "seed",
	}); err != nil {
		t.Fatalf("CreateDocument(doc-b) error = %v", err)
	}
	seeded, err := deps.store.CreateBinding(ctx, coreprompt.CreateBindingInput{
		PromptID:   "doc-a",
		Scene:      "agent.run.default",
		Phase:      "session",
		IsDefault:  true,
		Priority:   10,
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		Status:     "active",
		CreatedBy:  "seed",
		UpdatedBy:  "seed",
	})
	if err != nil {
		t.Fatalf("CreateBinding(seed) error = %v", err)
	}

	adminCookie := newPromptHandlerSessionCookie(t, deps.authLogic, deps.adminUsername)

	listResponse := doPromptRequest(t, http.MethodGet, server.URL+"/api/v1/prompts/bindings", nil, adminCookie)
	defer listResponse.Body.Close()
	listed := decodePromptBindingListResponse(t, listResponse.Body)
	if len(listed) != 1 {
		t.Fatalf("len(bindings) = %d, want 1", len(listed))
	}
	if listed[0].ID != seeded.ID {
		t.Fatalf("listed[0].ID = %d, want %d", listed[0].ID, seeded.ID)
	}

	getResponse := doPromptRequest(t, http.MethodGet, fmt.Sprintf("%s/api/v1/prompts/bindings/%d", server.URL, seeded.ID), nil, adminCookie)
	defer getResponse.Body.Close()
	got := decodePromptBindingResponse(t, getResponse.Body)
	if got.PromptID != "doc-a" {
		t.Fatalf("got.PromptID = %q, want doc-a", got.PromptID)
	}

	createResponse := doPromptRequest(t, http.MethodPost, server.URL+"/api/v1/prompts/bindings", map[string]any{
		"prompt_id":   "doc-a",
		"scene":       "agent.run.default",
		"phase":       "step_pre_model",
		"is_default":  false,
		"priority":    20,
		"provider_id": "openai",
		"model_id":    "gpt-5.4",
		"status":      "active",
	}, adminCookie)
	defer createResponse.Body.Close()
	created := decodePromptBindingResponse(t, createResponse.Body)
	if created.ID == 0 {
		t.Fatal("created.ID = 0, want non-zero")
	}
	if created.CreatedBy != deps.adminUsername {
		t.Fatalf("created.CreatedBy = %q, want %q", created.CreatedBy, deps.adminUsername)
	}
	if created.UpdatedBy != deps.adminUsername {
		t.Fatalf("created.UpdatedBy = %q, want %q", created.UpdatedBy, deps.adminUsername)
	}

	updateResponse := doPromptRequest(t, http.MethodPut, fmt.Sprintf("%s/api/v1/prompts/bindings/%d", server.URL, created.ID), map[string]any{
		"prompt_id":   "doc-b",
		"scene":       "agent.run.default",
		"phase":       "tool_result",
		"is_default":  true,
		"priority":    1,
		"provider_id": "google",
		"model_id":    "gemini-2.5-flash",
		"status":      "disabled",
	}, adminCookie)
	defer updateResponse.Body.Close()
	updated := decodePromptBindingResponse(t, updateResponse.Body)
	if updated.PromptID != "doc-b" {
		t.Fatalf("updated.PromptID = %q, want doc-b", updated.PromptID)
	}
	if updated.Phase != "tool_result" {
		t.Fatalf("updated.Phase = %q, want tool_result", updated.Phase)
	}
	if !updated.IsDefault {
		t.Fatal("updated.IsDefault = false, want true")
	}
	if updated.Status != "disabled" {
		t.Fatalf("updated.Status = %q, want disabled", updated.Status)
	}

	deleteResponse := doPromptRequest(t, http.MethodDelete, fmt.Sprintf("%s/api/v1/prompts/bindings/%d", server.URL, created.ID), nil, adminCookie)
	defer deleteResponse.Body.Close()
	deleted := decodePromptDeleteResponse(t, deleteResponse.Body)
	if !deleted.Deleted {
		t.Fatal("deleted.Deleted = false, want true")
	}
	if _, err := deps.store.GetBinding(ctx, created.ID); !errors.Is(err, coreprompt.ErrPromptBindingNotFound) {
		t.Fatalf("GetBinding(deleted) error = %v, want ErrPromptBindingNotFound", err)
	}
}

func TestPromptHandlerBindingResponsesIncludeEmptyProviderAndModelFields(t *testing.T) {
	deps, server := newPromptHandlerTestServer(t)
	ctx := context.Background()

	if _, err := deps.store.CreateDocument(ctx, coreprompt.CreateDocumentInput{
		ID:        "doc-empty-fields",
		Name:      "Doc",
		Content:   "Doc content",
		Scope:     "admin",
		Status:    "active",
		CreatedBy: "seed",
		UpdatedBy: "seed",
	}); err != nil {
		t.Fatalf("CreateDocument() error = %v", err)
	}
	adminCookie := newPromptHandlerSessionCookie(t, deps.authLogic, deps.adminUsername)

	createResponse := doPromptRequest(t, http.MethodPost, server.URL+"/api/v1/prompts/bindings", map[string]any{
		"prompt_id":  "doc-empty-fields",
		"scene":      "agent.run.default",
		"phase":      "session",
		"is_default": true,
		"priority":   1,
		"status":     "active",
	}, adminCookie)
	defer createResponse.Body.Close()
	createdEnvelope := decodeEnvelope(t, createResponse.Body)
	if !createdEnvelope.OK {
		t.Fatalf("create response ok = false, message = %q", createdEnvelope.Message)
	}
	createdMap := decodePromptDataMap(t, createdEnvelope.Data)
	assertPromptBindingEmptyProviderModelFields(t, createdMap)
	createdID := uint64(createdMap["id"].(float64))

	getResponse := doPromptRequest(t, http.MethodGet, fmt.Sprintf("%s/api/v1/prompts/bindings/%d", server.URL, createdID), nil, adminCookie)
	defer getResponse.Body.Close()
	getEnvelope := decodeEnvelope(t, getResponse.Body)
	if !getEnvelope.OK {
		t.Fatalf("get response ok = false, message = %q", getEnvelope.Message)
	}
	getMap := decodePromptDataMap(t, getEnvelope.Data)
	assertPromptBindingEmptyProviderModelFields(t, getMap)

	listResponse := doPromptRequest(t, http.MethodGet, server.URL+"/api/v1/prompts/bindings", nil, adminCookie)
	defer listResponse.Body.Close()
	listEnvelope := decodeEnvelope(t, listResponse.Body)
	if !listEnvelope.OK {
		t.Fatalf("list response ok = false, message = %q", listEnvelope.Message)
	}
	listMaps := decodePromptDataSliceMap(t, listEnvelope.Data)
	if len(listMaps) != 1 {
		t.Fatalf("len(listMaps) = %d, want 1", len(listMaps))
	}
	assertPromptBindingEmptyProviderModelFields(t, listMaps[0])

	updateResponse := doPromptRequest(t, http.MethodPut, fmt.Sprintf("%s/api/v1/prompts/bindings/%d", server.URL, createdID), map[string]any{
		"provider_id": "",
		"model_id":    "",
	}, adminCookie)
	defer updateResponse.Body.Close()
	updateEnvelope := decodeEnvelope(t, updateResponse.Body)
	if !updateEnvelope.OK {
		t.Fatalf("update response ok = false, message = %q", updateEnvelope.Message)
	}
	updateMap := decodePromptDataMap(t, updateEnvelope.Data)
	assertPromptBindingEmptyProviderModelFields(t, updateMap)
}

func TestPromptHandlerDuplicateDocumentCreateReturnsConflict(t *testing.T) {
	deps, server := newPromptHandlerTestServer(t)
	ctx := context.Background()

	if _, err := deps.store.CreateDocument(ctx, coreprompt.CreateDocumentInput{
		ID:        "doc-duplicate",
		Name:      "Existing",
		Content:   "Existing content",
		Scope:     "admin",
		Status:    "active",
		CreatedBy: "seed",
		UpdatedBy: "seed",
	}); err != nil {
		t.Fatalf("CreateDocument() error = %v", err)
	}

	adminCookie := newPromptHandlerSessionCookie(t, deps.authLogic, deps.adminUsername)
	response := doPromptRequest(t, http.MethodPost, server.URL+"/api/v1/prompts/documents", map[string]any{
		"id":      "doc-duplicate",
		"name":    "Duplicate",
		"content": "Duplicate content",
		"scope":   "admin",
		"status":  "active",
	}, adminCookie)
	defer response.Body.Close()

	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("response ok = true, want false")
	}
	if envelope.Code != http.StatusConflict {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusConflict)
	}
	if envelope.Message != "prompt document already exists" {
		t.Fatalf("envelope.Message = %q, want %q", envelope.Message, "prompt document already exists")
	}
}

func TestPromptHandlerBindingMissingPromptDocumentReturnsNotFound(t *testing.T) {
	deps, server := newPromptHandlerTestServer(t)
	ctx := context.Background()
	adminCookie := newPromptHandlerSessionCookie(t, deps.authLogic, deps.adminUsername)

	t.Run("create", func(t *testing.T) {
		response := doPromptRequest(t, http.MethodPost, server.URL+"/api/v1/prompts/bindings", map[string]any{
			"prompt_id":  "doc-missing",
			"scene":      "agent.run.default",
			"phase":      "session",
			"is_default": true,
			"priority":   1,
			"status":     "active",
		}, adminCookie)
		defer response.Body.Close()

		envelope := decodeEnvelope(t, response.Body)
		if envelope.OK {
			t.Fatal("response ok = true, want false")
		}
		if envelope.Code != http.StatusNotFound {
			t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusNotFound)
		}
		if envelope.Message != "referenced prompt document not found" {
			t.Fatalf("envelope.Message = %q, want %q", envelope.Message, "referenced prompt document not found")
		}
	})

	t.Run("update", func(t *testing.T) {
		if _, err := deps.store.CreateDocument(ctx, coreprompt.CreateDocumentInput{
			ID:        "doc-existing",
			Name:      "Existing",
			Content:   "Existing content",
			Scope:     "admin",
			Status:    "active",
			CreatedBy: "seed",
			UpdatedBy: "seed",
		}); err != nil {
			t.Fatalf("CreateDocument(existing) error = %v", err)
		}
		binding, err := deps.store.CreateBinding(ctx, coreprompt.CreateBindingInput{
			PromptID:  "doc-existing",
			Scene:     "agent.run.default",
			Phase:     "session",
			IsDefault: true,
			Priority:  1,
			Status:    "active",
			CreatedBy: "seed",
			UpdatedBy: "seed",
		})
		if err != nil {
			t.Fatalf("CreateBinding(existing) error = %v", err)
		}

		response := doPromptRequest(t, http.MethodPut, fmt.Sprintf("%s/api/v1/prompts/bindings/%d", server.URL, binding.ID), map[string]any{
			"prompt_id": "doc-missing",
		}, adminCookie)
		defer response.Body.Close()

		envelope := decodeEnvelope(t, response.Body)
		if envelope.OK {
			t.Fatal("response ok = true, want false")
		}
		if envelope.Code != http.StatusNotFound {
			t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusNotFound)
		}
		if envelope.Message != "referenced prompt document not found" {
			t.Fatalf("envelope.Message = %q, want %q", envelope.Message, "referenced prompt document not found")
		}
	})
}

func TestPromptHandlerRejectsOrdinaryAuthenticatedUser(t *testing.T) {
	deps, server := newPromptHandlerTestServer(t)
	memberCookie := newPromptHandlerSessionCookie(t, deps.authLogic, deps.userUsername)

	for _, tc := range []struct {
		name    string
		method  string
		path    string
		payload map[string]any
	}{
		{name: "list documents", method: http.MethodGet, path: "/api/v1/prompts/documents"},
		{name: "create document", method: http.MethodPost, path: "/api/v1/prompts/documents", payload: map[string]any{"id": "doc-user", "name": "User", "content": "content", "scope": "admin", "status": "active"}},
		{name: "delete document", method: http.MethodDelete, path: "/api/v1/prompts/documents/doc-user"},
		{name: "list bindings", method: http.MethodGet, path: "/api/v1/prompts/bindings"},
		{name: "delete binding", method: http.MethodDelete, path: "/api/v1/prompts/bindings/1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := doPromptRequest(t, tc.method, server.URL+tc.path, tc.payload, memberCookie)
			defer response.Body.Close()
			envelope := decodeEnvelope(t, response.Body)
			if envelope.OK {
				t.Fatal("response ok = true, want false")
			}
			if envelope.Code != http.StatusUnauthorized {
				t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestPromptHandlerRejectsAnonymousUser(t *testing.T) {
	_, server := newPromptHandlerTestServer(t)

	for _, tc := range []struct {
		name    string
		method  string
		path    string
		payload map[string]any
	}{
		{name: "list documents", method: http.MethodGet, path: "/api/v1/prompts/documents"},
		{name: "create document", method: http.MethodPost, path: "/api/v1/prompts/documents", payload: map[string]any{"id": "doc-anon", "name": "Anon", "content": "content", "scope": "admin", "status": "active"}},
		{name: "delete document", method: http.MethodDelete, path: "/api/v1/prompts/documents/doc-anon"},
		{name: "list bindings", method: http.MethodGet, path: "/api/v1/prompts/bindings"},
		{name: "delete binding", method: http.MethodDelete, path: "/api/v1/prompts/bindings/1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := doPromptRequest(t, tc.method, server.URL+tc.path, tc.payload, nil)
			defer response.Body.Close()
			envelope := decodeEnvelope(t, response.Body)
			if envelope.OK {
				t.Fatal("response ok = true, want false")
			}
			if envelope.Code != http.StatusUnauthorized {
				t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestPromptHandlerRegisterAddsExpectedRoutes(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	store := coreprompt.NewStore(db)
	engine := rest.Init()

	NewPromptHandler(store).Register(engine.Group("/api/v1"))

	routes := map[string]struct{}{}
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	for _, route := range []string{
		http.MethodGet + " /api/v1/prompts/documents",
		http.MethodGet + " /api/v1/prompts/documents/:id",
		http.MethodPost + " /api/v1/prompts/documents",
		http.MethodPut + " /api/v1/prompts/documents/:id",
		http.MethodDelete + " /api/v1/prompts/documents/:id",
		http.MethodGet + " /api/v1/prompts/bindings",
		http.MethodGet + " /api/v1/prompts/bindings/:id",
		http.MethodPost + " /api/v1/prompts/bindings",
		http.MethodPut + " /api/v1/prompts/bindings/:id",
		http.MethodDelete + " /api/v1/prompts/bindings/:id",
	} {
		if _, ok := routes[route]; !ok {
			t.Fatalf("route %q missing", route)
		}
	}
}

func TestPromptHandlerSwaggerAnnotationsMatchCreateErrorSemantics(t *testing.T) {
	raw, err := os.ReadFile("prompt_handler.go")
	if err != nil {
		t.Fatalf("os.ReadFile(prompt_handler.go) error = %v", err)
	}
	content := string(raw)

	documentBlock := promptHandlerCommentBlock(t, content, "func (h *PromptHandler) handleCreateDocument()")
	if !strings.Contains(documentBlock, "// @Failure 409 {object} ErrorSwaggerResponse") {
		t.Fatal("handleCreateDocument swagger comment missing @Failure 409")
	}

	bindingBlock := promptHandlerCommentBlock(t, content, "func (h *PromptHandler) handleCreateBinding()")
	if !strings.Contains(bindingBlock, "// @Failure 404 {object} ErrorSwaggerResponse") {
		t.Fatal("handleCreateBinding swagger comment missing @Failure 404")
	}
}

func TestPromptHandlerSwaggerAnnotationsMatchBindingIDParseErrors(t *testing.T) {
	raw, err := os.ReadFile("prompt_handler.go")
	if err != nil {
		t.Fatalf("os.ReadFile(prompt_handler.go) error = %v", err)
	}
	content := string(raw)

	getBlock := promptHandlerCommentBlock(t, content, "func (h *PromptHandler) handleGetBinding()")
	if !strings.Contains(getBlock, "// @Failure 400 {object} ErrorSwaggerResponse") {
		t.Fatal("handleGetBinding swagger comment missing @Failure 400")
	}

	deleteBlock := promptHandlerCommentBlock(t, content, "func (h *PromptHandler) handleDeleteBinding()")
	if !strings.Contains(deleteBlock, "// @Failure 400 {object} ErrorSwaggerResponse") {
		t.Fatal("handleDeleteBinding swagger comment missing @Failure 400")
	}
}

func TestPromptHandlerCommentBlockTargetsOnlyRequestedFunctionComment(t *testing.T) {
	content := strings.Join([]string{
		"// first comment",
		"// @Failure 409 {object} ErrorSwaggerResponse",
		"func (h *PromptHandler) handleCreateDocument() {}",
		"",
		"// second comment",
		"func (h *PromptHandler) handleCreateBinding() {}",
	}, "\n")

	bindingBlock := promptHandlerCommentBlock(t, content, "func (h *PromptHandler) handleCreateBinding()")
	if strings.Contains(bindingBlock, "@Failure 409") {
		t.Fatal("binding block unexpectedly included earlier function comment")
	}
	if !strings.Contains(bindingBlock, "// second comment") {
		t.Fatal("binding block missing its own comment")
	}
}

func TestSwaggerJSONIncludesPromptPathsAndDefinitions(t *testing.T) {
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
	for _, path := range []string{"/prompts/documents", "/prompts/documents/{id}", "/prompts/bindings", "/prompts/bindings/{id}"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("swagger paths missing %q", path)
		}
	}

	definitions, ok := document["definitions"].(map[string]any)
	if !ok {
		t.Fatalf("definitions = %#v, want object", document["definitions"])
	}
	for _, definition := range []string{"handlers.PromptDocumentSwaggerDoc", "handlers.PromptBindingSwaggerDoc"} {
		if _, ok := definitions[definition]; !ok {
			t.Fatalf("swagger definitions missing %q", definition)
		}
	}
}

type promptHandlerTestDeps struct {
	store         *coreprompt.Store
	authLogic     *logics.AuthLogic
	adminUsername string
	userUsername  string
}

type promptDeleteResponse struct {
	Deleted bool `json:"deleted"`
}

func newPromptHandlerTestServer(t *testing.T) (*promptHandlerTestDeps, *httptest.Server) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	store := coreprompt.NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("PromptStore.AutoMigrate() error = %v", err)
	}
	authLogic := newAuthLogicForTest(t, db)
	authMiddleware := NewAuthMiddleware(authLogic)

	if _, err := authLogic.Register(context.Background(), "admin", "secret-123", "secret-123"); err != nil {
		t.Fatalf("Register(admin) error = %v", err)
	}
	if _, err := authLogic.Register(context.Background(), "member", "secret-123", "secret-123"); err != nil {
		t.Fatalf("Register(member) error = %v", err)
	}

	engine := rest.Init()
	NewPromptHandler(store, authMiddleware.RequireSession()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	return &promptHandlerTestDeps{store: store, authLogic: authLogic, adminUsername: "admin", userUsername: "member"}, server
}

func newPromptHandlerSessionCookie(t *testing.T, logic *logics.AuthLogic, username string) *http.Cookie {
	t.Helper()

	_, session, err := logic.Login(context.Background(), username, "secret-123")
	if err != nil {
		t.Fatalf("Login(%q) error = %v", username, err)
	}
	return &http.Cookie{Name: logic.CookieName(), Value: session.ID}
}

func doPromptRequest(t *testing.T, method, url string, payload map[string]any, cookie *http.Cookie) *http.Response {
	t.Helper()

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		body = bytes.NewReader(raw)
	}

	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	return response
}

func decodePromptDocumentResponse(t *testing.T, body io.Reader) coreprompt.PromptDocument {
	t.Helper()

	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var document coreprompt.PromptDocument
	if err := json.Unmarshal(envelope.Data, &document); err != nil {
		t.Fatalf("json.Unmarshal(document) error = %v", err)
	}
	return document
}

func decodePromptDocumentListResponse(t *testing.T, body io.Reader) []coreprompt.PromptDocument {
	t.Helper()

	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var documents []coreprompt.PromptDocument
	if err := json.Unmarshal(envelope.Data, &documents); err != nil {
		t.Fatalf("json.Unmarshal(documents) error = %v", err)
	}
	return documents
}

func decodePromptBindingResponse(t *testing.T, body io.Reader) coreprompt.PromptBinding {
	t.Helper()

	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var binding coreprompt.PromptBinding
	if err := json.Unmarshal(envelope.Data, &binding); err != nil {
		t.Fatalf("json.Unmarshal(binding) error = %v", err)
	}
	return binding
}

func decodePromptBindingListResponse(t *testing.T, body io.Reader) []coreprompt.PromptBinding {
	t.Helper()

	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var bindings []coreprompt.PromptBinding
	if err := json.Unmarshal(envelope.Data, &bindings); err != nil {
		t.Fatalf("json.Unmarshal(bindings) error = %v", err)
	}
	return bindings
}

func decodePromptDeleteResponse(t *testing.T, body io.Reader) promptDeleteResponse {
	t.Helper()

	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var deleted promptDeleteResponse
	if err := json.Unmarshal(envelope.Data, &deleted); err != nil {
		t.Fatalf("json.Unmarshal(delete) error = %v", err)
	}
	return deleted
}

func decodePromptDataMap(t *testing.T, data json.RawMessage) map[string]any {
	t.Helper()

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(map) error = %v", err)
	}
	return got
}

func decodePromptDataSliceMap(t *testing.T, data json.RawMessage) []map[string]any {
	t.Helper()

	var got []map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(slice map) error = %v", err)
	}
	return got
}

func assertPromptBindingEmptyProviderModelFields(t *testing.T, got map[string]any) {
	t.Helper()

	providerID, ok := got["provider_id"]
	if !ok {
		t.Fatal("provider_id key missing")
	}
	providerValue, ok := providerID.(string)
	if !ok {
		t.Fatalf("provider_id type = %T, want string", providerID)
	}
	if providerValue != "" {
		t.Fatalf("provider_id = %q, want empty string", providerValue)
	}

	modelID, ok := got["model_id"]
	if !ok {
		t.Fatal("model_id key missing")
	}
	modelValue, ok := modelID.(string)
	if !ok {
		t.Fatalf("model_id type = %T, want string", modelID)
	}
	if modelValue != "" {
		t.Fatalf("model_id = %q, want empty string", modelValue)
	}
}

func promptHandlerCommentBlock(t *testing.T, content string, marker string) string {
	t.Helper()

	index := strings.Index(content, marker)
	if index < 0 {
		t.Fatalf("marker %q not found", marker)
	}

	before := content[:index]
	lines := strings.Split(before, "\n")
	start := len(lines)
	for start > 0 {
		line := lines[start-1]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || trimmed == "" {
			start--
			continue
		}
		break
	}
	block := strings.Join(lines[start:], "\n")
	return strings.Trim(block, "\n")
}
