package logics

import (
	"context"
	"errors"
	"testing"
)

func TestAuthLogicVerifyPassword(t *testing.T) {
	logic := newAuthLogicTestSubject(t)
	user, err := logic.RegisterWithInput(context.Background(), RegisterInput{Username: "verify-user", Email: "verify@example.com", Password: "secret-123", ConfirmPassword: "secret-123"})
	if err != nil {
		t.Fatal(err)
	}
	if err := logic.VerifyPassword(context.Background(), user.ID, "secret-123"); err != nil {
		t.Fatalf("VerifyPassword(valid) error = %v", err)
	}
	if err := logic.VerifyPassword(context.Background(), user.ID, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("VerifyPassword(wrong) error = %v, want ErrInvalidCredentials", err)
	}
}
