package logics

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAdminAuditLogicRecordsMaskedSnapshots(t *testing.T) {
	db := newAdminAuditLogicTestDB(t)
	logic := NewAdminAuditLogic(db)

	if err := logic.Record(context.Background(), RecordAdminAuditInput{
		Actor: models.User{
			ID:       1,
			Username: "root",
			Email:    "root@example.com",
		},
		TargetKind: "setting",
		TargetID:   "smtp",
		Action:     "settings.smtp.update",
		Before: map[string]any{
			"password":         "old-smtp-password",
			"turnstile_secret": "old-turnstile-secret",
			"api_key":          "old-api-key",
		},
		After: map[string]any{
			"password": "new-smtp-password",
			"secret":   "new-turnstile-secret",
			"api_key":  "new-api-key",
		},
		IPAddress: "127.0.0.1",
		UserAgent: "admin-test",
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	var event models.AdminAuditEvent
	if err := db.Take(&event).Error; err != nil {
		t.Fatalf("load admin audit event: %v", err)
	}
	if event.ActorID != 1 || event.ActorUsername != "root" || event.ActorEmail != "root@example.com" {
		t.Fatalf("actor fields = %#v, want root actor", event)
	}
	if event.TargetKind != "setting" || event.TargetID != "smtp" || event.Action != "settings.smtp.update" {
		t.Fatalf("target/action fields = %#v, want setting smtp update", event)
	}
	stored := string(event.BeforeJSON) + string(event.AfterJSON)
	for _, plaintext := range []string{
		"old-smtp-password",
		"new-smtp-password",
		"old-turnstile-secret",
		"new-turnstile-secret",
		"old-api-key",
		"new-api-key",
	} {
		if strings.Contains(stored, plaintext) {
			t.Fatalf("stored audit JSON leaked %q: %s", plaintext, stored)
		}
	}

	var after map[string]string
	if err := json.Unmarshal(event.AfterJSON, &after); err != nil {
		t.Fatalf("unmarshal after JSON: %v", err)
	}
	if after["password"] == "" || after["secret"] == "" || after["api_key"] == "" {
		t.Fatalf("after JSON = %#v, want masked sensitive fields", after)
	}
}

func TestAdminAuditLogicListsEventsWithFilters(t *testing.T) {
	db := newAdminAuditLogicTestDB(t)
	logic := NewAdminAuditLogic(db)

	events := []RecordAdminAuditInput{
		{Actor: models.User{ID: 1, Username: "root"}, TargetKind: "user", TargetID: "2", Action: "admin.users.update"},
		{Actor: models.User{ID: 1, Username: "root"}, TargetKind: "setting", TargetID: "smtp", Action: "settings.smtp.update"},
		{Actor: models.User{ID: 2, Username: "ops"}, TargetKind: "user", TargetID: "3", Action: "admin.users.reset_password"},
	}
	for _, event := range events {
		if err := logic.Record(context.Background(), event); err != nil {
			t.Fatalf("Record(%s) error = %v", event.Action, err)
		}
	}

	filtered, err := logic.List(context.Background(), AdminAuditFilter{
		TargetKind: "user",
		ActorID:    1,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if filtered[0].Action != "admin.users.update" {
		t.Fatalf("filtered[0].Action = %q, want admin.users.update", filtered[0].Action)
	}
}

func newAdminAuditLogicTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	if err := db.AutoMigrate(&models.AdminAuditEvent{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return db
}
