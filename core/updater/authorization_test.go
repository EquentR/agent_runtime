package updater

import (
	"testing"
	"time"
)

func TestOperationAuthorizerBindsAndConsumesTokenOnce(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	authorizer := NewOperationAuthorizer(func() time.Time { return now })
	binding := AuthorizationBinding{UserID: 7, SessionID: "sess-1", Action: "install", Target: "v1.2.3"}
	token, expiresAt, err := authorizer.Issue(binding, 5*time.Minute)
	if err != nil || token == "" || !expiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("Issue() = %q, %v, %v", token, expiresAt, err)
	}
	if err := authorizer.Consume(token, AuthorizationBinding{UserID: 7, SessionID: "sess-2", Action: "install", Target: "v1.2.3"}); err == nil {
		t.Fatal("Consume(wrong session) error = nil")
	}
	if err := authorizer.Consume(token, binding); err != nil {
		t.Fatalf("Consume(valid) error = %v", err)
	}
	if err := authorizer.Consume(token, binding); err == nil {
		t.Fatal("Consume(reused) error = nil")
	}
}

func TestOperationAuthorizerRejectsExpiredToken(t *testing.T) {
	now := time.Now().UTC()
	authorizer := NewOperationAuthorizer(func() time.Time { return now })
	binding := AuthorizationBinding{UserID: 1, SessionID: "session", Action: "rollback", Target: "backup-1"}
	token, _, _ := authorizer.Issue(binding, time.Minute)
	now = now.Add(2 * time.Minute)
	if err := authorizer.Consume(token, binding); err == nil {
		t.Fatal("Consume(expired) error = nil")
	}
}
