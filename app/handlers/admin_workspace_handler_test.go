package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	coreworkspaces "github.com/EquentR/agent_runtime/core/workspaces"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAdminWorkspaceHandlerReturnsUserWorkspaceSummaryForAdminsOnly(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	mustWriteWorkspaceTestFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")

	workspaceManager, err := coreworkspaces.NewManager(coreworkspaces.Config{
		TemplateRoot: templateRoot,
		Root:         workspacesRoot,
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, err := workspaceManager.EnsureHomeWorkspace(context.Background(), "7"); err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	if _, err := workspaceManager.CreateTaskWorkspace(context.Background(), "7", "tsk_1", coreworkspaces.ModeMutable); err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}

	authLogic, db, adminCookie, userCookie, server := newAdminWorkspaceHandlerTestServer(t, workspaceManager)
	_ = authLogic

	adminResponse := doAdminRequest(t, http.MethodGet, server.URL+"/api/v1/admin/workspaces/users/7", nil, adminCookie)
	defer adminResponse.Body.Close()
	summary := decodeAdminWorkspaceSummaryResponse(t, adminResponse.Body)
	if summary.UserID != "7" {
		t.Fatalf("summary.UserID = %q, want 7", summary.UserID)
	}
	wantHomeRoot := filepath.Join(workspacesRoot, "users", "7", "home")
	if summary.HomeRoot != wantHomeRoot {
		t.Fatalf("summary.HomeRoot = %q, want %q", summary.HomeRoot, wantHomeRoot)
	}
	if len(summary.Tasks) != 1 {
		t.Fatalf("len(summary.Tasks) = %d, want 1", len(summary.Tasks))
	}
	if summary.Tasks[0].TaskID != "tsk_1" || summary.Tasks[0].State != coreworkspaces.StateActive {
		t.Fatalf("summary.Tasks[0] = %#v, want active tsk_1", summary.Tasks[0])
	}
	var auditEvent models.AdminAuditEvent
	if err := db.Where("target_kind = ? AND target_id = ? AND action = ?", "workspace", "7", "admin.workspaces.inspect").Take(&auditEvent).Error; err != nil {
		t.Fatalf("admin workspace inspection audit event missing: %v", err)
	}

	nonAdminResponse := doAdminRequest(t, http.MethodGet, server.URL+"/api/v1/admin/workspaces/users/7", nil, userCookie)
	defer nonAdminResponse.Body.Close()
	if decodeEnvelope(t, nonAdminResponse.Body).OK {
		t.Fatal("non-admin workspace summary ok = true, want false")
	}
}

type adminWorkspaceSummaryTestResponse struct {
	UserID   string                           `json:"user_id"`
	HomeRoot string                           `json:"home_root"`
	Tasks    []adminWorkspaceTaskTestResponse `json:"tasks"`
}

type adminWorkspaceTaskTestResponse struct {
	TaskID     string               `json:"task_id"`
	Mode       coreworkspaces.Mode  `json:"mode"`
	State      coreworkspaces.State `json:"state"`
	TaskRoot   string               `json:"task_root"`
	BackupRoot string               `json:"backup_root"`
}

func newAdminWorkspaceHandlerTestServer(t *testing.T, workspaceManager *coreworkspaces.Manager) (*logics.AuthLogic, *gorm.DB, *http.Cookie, *http.Cookie, *httptest.Server) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserSession{}, &models.AdminAuditEvent{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	authLogic, err := logics.NewAuthLogic(db, logics.AuthConfig{CookieName: authSessionCookieName})
	if err != nil {
		t.Fatalf("NewAuthLogic() error = %v", err)
	}
	admin := seedAdminHandlerUser(t, db, "workspace-admin", "workspace-admin@example.com", models.UserRoleAdmin, models.UserStatusActive, true)
	user := seedAdminHandlerUser(t, db, "workspace-user", "workspace-user@example.com", models.UserRoleUser, models.UserStatusActive, true)
	_, adminSession, err := authLogic.Login(context.Background(), admin.Username, "secret-123")
	if err != nil {
		t.Fatalf("Login(admin) error = %v", err)
	}
	_, userSession, err := authLogic.Login(context.Background(), user.Username, "secret-123")
	if err != nil {
		t.Fatalf("Login(user) error = %v", err)
	}

	authMiddleware := NewAuthMiddleware(authLogic)
	engine := rest.Init()
	NewAdminWorkspaceHandler(workspaceManager, logics.NewAdminAuditLogic(db), authMiddleware.RequireAdmin()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return authLogic,
		db,
		&http.Cookie{Name: authSessionCookieName, Value: adminSession.ID},
		&http.Cookie{Name: authSessionCookieName, Value: userSession.ID},
		server
}

func decodeAdminWorkspaceSummaryResponse(t *testing.T, body io.Reader) adminWorkspaceSummaryTestResponse {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, message = %s, raw data = %s", envelope.Message, string(envelope.Data))
	}
	var summary adminWorkspaceSummaryTestResponse
	if err := json.Unmarshal(envelope.Data, &summary); err != nil {
		t.Fatalf("Unmarshal(admin workspace summary) error = %v", err)
	}
	return summary
}

func mustWriteWorkspaceTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func TestAdminWorkspaceHandlerMissingManagerDoesNotRegisterRoutes(t *testing.T) {
	engine := rest.Init()
	NewAdminWorkspaceHandler(nil, nil).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	response, err := http.Get(fmt.Sprintf("%s/api/v1/admin/workspaces/users/7", server.URL))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when workspace manager is missing", response.StatusCode)
	}
}
