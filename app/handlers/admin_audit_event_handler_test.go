package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
)

func TestAdminAuditEventHandlerListsEventsForAdminOnly(t *testing.T) {
	deps, server := newAdminHandlerTestServer(t)
	user := seedAdminHandlerUser(t, deps.db, "viewer", "viewer@example.com", models.UserRoleUser, models.UserStatusActive, true)
	_, userSession, err := deps.authLogic.Login(context.Background(), user.Username, "secret-123")
	if err != nil {
		t.Fatalf("Login(viewer) error = %v", err)
	}

	if err := deps.auditLogic.Record(context.Background(), logics.RecordAdminAuditInput{
		Actor:      loadAdminHandlerUser(t, deps.db, 1),
		TargetKind: "user",
		TargetID:   "2",
		Action:     "admin.users.update",
		After:      map[string]any{"status": models.UserStatusDisabled},
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	adminResponse := doAdminRequest(t, http.MethodGet, server.URL+"/api/v1/admin/audit-events?target_kind=user", nil, deps.adminCookie)
	defer adminResponse.Body.Close()
	events := decodeAdminAuditEventListResponse(t, adminResponse.Body)
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Action != "admin.users.update" || events[0].TargetKind != "user" {
		t.Fatalf("event = %#v, want user update", events[0])
	}

	nonAdminResponse := doAdminRequest(t, http.MethodGet, server.URL+"/api/v1/admin/audit-events", nil, &http.Cookie{Name: authSessionCookieName, Value: userSession.ID})
	defer nonAdminResponse.Body.Close()
	if decodeEnvelope(t, nonAdminResponse.Body).OK {
		t.Fatal("non-admin audit events ok = true, want false")
	}
}

type adminAuditEventTestResponse struct {
	ID            uint64          `json:"id"`
	ActorID       uint64          `json:"actor_id"`
	ActorUsername string          `json:"actor_username"`
	ActorEmail    string          `json:"actor_email"`
	TargetKind    string          `json:"target_kind"`
	TargetID      string          `json:"target_id"`
	Action        string          `json:"action"`
	BeforeJSON    json.RawMessage `json:"before_json"`
	AfterJSON     json.RawMessage `json:"after_json"`
	IPAddress     string          `json:"ip_address"`
	UserAgent     string          `json:"user_agent"`
	CreatedAt     string          `json:"created_at"`
}

func decodeAdminAuditEventListResponse(t *testing.T, body io.Reader) []adminAuditEventTestResponse {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, message = %s, raw data = %s", envelope.Message, string(envelope.Data))
	}
	var events []adminAuditEventTestResponse
	if err := json.Unmarshal(envelope.Data, &events); err != nil {
		t.Fatalf("Unmarshal(admin audit events) error = %v", err)
	}
	return events
}
