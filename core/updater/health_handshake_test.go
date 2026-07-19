package updater

import (
	"testing"
	"time"
)

func TestHealthHandshakeBindsTokenAndVersion(t *testing.T) {
	store, err := NewHealthHandshakeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Issue("secret-token", "v1.2.3", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify("wrong", "v1.2.3", now); err == nil {
		t.Fatal("Verify(wrong token) error = nil")
	}
	if err := store.Verify("secret-token", "v1.2.2", now); err == nil {
		t.Fatal("Verify(wrong version) error = nil")
	}
	if err := store.Verify("secret-token", "v1.2.3", now); err != nil {
		t.Fatalf("Verify(valid) error = %v", err)
	}
	if err := store.Verify("secret-token", "v1.2.3", now); err == nil {
		t.Fatal("Verify(replayed) error = nil")
	}
}

func TestHealthHandshakeRejectsTokenBeforeIssue(t *testing.T) {
	store, err := NewHealthHandshakeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Verify("token", "v1.2.3", time.Now()); err == nil {
		t.Fatal("Verify(without issue) error = nil")
	}
}
