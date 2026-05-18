package mail

import (
	"context"
	"testing"
)

func TestSMTPConfigValidationRequiresHostFromAndCredentialsWhenEnabled(t *testing.T) {
	disabled := SMTPConfig{}
	if err := disabled.ValidateForSend(); err != nil {
		t.Fatalf("disabled ValidateForSend() error = %v", err)
	}

	missingHost := SMTPConfig{
		Enabled:  true,
		Port:     587,
		Username: "smtp-user",
		Password: "smtp-password",
		From:     "noreply@example.com",
	}
	if err := missingHost.ValidateForSend(); err == nil {
		t.Fatal("missing host ValidateForSend() error = nil, want error")
	}

	missingFrom := SMTPConfig{
		Enabled:  true,
		Host:     "smtp.example.com",
		Port:     587,
		Username: "smtp-user",
		Password: "smtp-password",
	}
	if err := missingFrom.ValidateForSend(); err == nil {
		t.Fatal("missing from ValidateForSend() error = nil, want error")
	}

	missingCredentials := SMTPConfig{
		Enabled: true,
		Host:    "smtp.example.com",
		Port:    587,
		From:    "noreply@example.com",
	}
	if err := missingCredentials.ValidateForSend(); err == nil {
		t.Fatal("missing credentials ValidateForSend() error = nil, want error")
	}

	config := SMTPConfig{
		Enabled:     true,
		Host:        "smtp.example.com",
		Port:        587,
		Username:    "smtp-user",
		Password:    "smtp-password",
		From:        "noreply@example.com",
		UseStartTLS: true,
	}
	if err := config.ValidateForSend(); err != nil {
		t.Fatalf("valid ValidateForSend() error = %v", err)
	}
	sender, err := NewSMTPSender(config)
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}
	if sender == nil {
		t.Fatal("NewSMTPSender() = nil, want sender")
	}
	if _, ok := sender.(interface {
		Send(context.Context, Message) error
	}); !ok {
		t.Fatal("sender does not implement Sender")
	}
}
