package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/pkg/mail"
)

func TestAdminSettingsHandlerReadsAndUpdatesSMTPTurnstileAndRegistration(t *testing.T) {
	deps, server := newAdminHandlerTestServer(t)

	smtpResponse := doAdminRequest(t, http.MethodPut, server.URL+"/api/v1/admin/settings/smtp", map[string]any{
		"enabled":       true,
		"host":          "smtp.example.com",
		"port":          587,
		"username":      "smtp-user",
		"password":      "smtp-password",
		"from":          "noreply@example.com",
		"use_start_tls": true,
	}, deps.adminCookie)
	defer smtpResponse.Body.Close()
	smtp := decodeAdminSMTPSettingsResponse(t, smtpResponse.Body)
	if smtp.Host != "smtp.example.com" || smtp.Password != "" || smtp.PasswordMasked == "" {
		t.Fatalf("smtp response = %#v, want configured and masked", smtp)
	}

	testResponse := doAdminRequest(t, http.MethodPost, server.URL+"/api/v1/admin/settings/smtp/test", map[string]any{
		"to": "ops@example.com",
	}, deps.adminCookie)
	defer testResponse.Body.Close()
	if !decodeEnvelope(t, testResponse.Body).OK {
		t.Fatal("smtp test ok = false, want true")
	}

	turnstileResponse := doAdminRequest(t, http.MethodPut, server.URL+"/api/v1/admin/settings/turnstile", map[string]any{
		"enabled":              true,
		"site_key":             "site-key",
		"secret":               "turnstile-secret",
		"protect_login":        true,
		"protect_registration": true,
		"protect_verification": true,
	}, deps.adminCookie)
	defer turnstileResponse.Body.Close()
	turnstile := decodeAdminTurnstileSettingsResponse(t, turnstileResponse.Body)
	if !turnstile.Enabled || turnstile.Secret != "" || turnstile.SecretMasked == "" {
		t.Fatalf("turnstile response = %#v, want enabled and masked", turnstile)
	}

	registrationResponse := doAdminRequest(t, http.MethodPut, server.URL+"/api/v1/admin/settings/registration", map[string]any{
		"enabled": false,
	}, deps.adminCookie)
	defer registrationResponse.Body.Close()
	registration := decodeAdminRegistrationSettingsResponse(t, registrationResponse.Body)
	if registration.Enabled {
		t.Fatal("registration.Enabled = true, want false")
	}

	var events []models.AdminAuditEvent
	if err := deps.db.Order("id asc").Find(&events).Error; err != nil {
		t.Fatalf("load audit events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("len(audit events) = %d, want 3 settings mutations", len(events))
	}
	wantActions := []string{"admin.settings.smtp.update", "admin.settings.turnstile.update", "admin.settings.registration.update"}
	for idx, want := range wantActions {
		if events[idx].Action != want {
			t.Fatalf("events[%d].Action = %q, want %q", idx, events[idx].Action, want)
		}
	}
}

func TestAdminSettingsHandlerMasksSecrets(t *testing.T) {
	deps, server := newAdminHandlerTestServer(t)

	updateSMTP := doAdminRequest(t, http.MethodPut, server.URL+"/api/v1/admin/settings/smtp", map[string]any{
		"enabled":  true,
		"host":     "smtp.example.com",
		"port":     465,
		"username": "smtp-user",
		"password": "smtp-secret-password",
		"from":     "noreply@example.com",
		"use_tls":  true,
	}, deps.adminCookie)
	defer updateSMTP.Body.Close()
	_ = decodeAdminSMTPSettingsResponse(t, updateSMTP.Body)

	getSMTP := doAdminRequest(t, http.MethodGet, server.URL+"/api/v1/admin/settings/smtp", nil, deps.adminCookie)
	defer getSMTP.Body.Close()
	smtp := decodeAdminSMTPSettingsResponse(t, getSMTP.Body)
	if smtp.Password != "" || strings.Contains(smtp.PasswordMasked, "smtp-secret-password") {
		t.Fatalf("smtp secret leaked in response: %#v", smtp)
	}

	updateTurnstile := doAdminRequest(t, http.MethodPut, server.URL+"/api/v1/admin/settings/turnstile", map[string]any{
		"enabled":  true,
		"site_key": "site-key",
		"secret":   "turnstile-secret-value",
	}, deps.adminCookie)
	defer updateTurnstile.Body.Close()
	_ = decodeAdminTurnstileSettingsResponse(t, updateTurnstile.Body)

	getTurnstile := doAdminRequest(t, http.MethodGet, server.URL+"/api/v1/admin/settings/turnstile", nil, deps.adminCookie)
	defer getTurnstile.Body.Close()
	turnstile := decodeAdminTurnstileSettingsResponse(t, getTurnstile.Body)
	if turnstile.Secret != "" || strings.Contains(turnstile.SecretMasked, "turnstile-secret-value") {
		t.Fatalf("turnstile secret leaked in response: %#v", turnstile)
	}

	var events []models.AdminAuditEvent
	if err := deps.db.Order("id asc").Find(&events).Error; err != nil {
		t.Fatalf("load audit events: %v", err)
	}
	stored := ""
	for _, event := range events {
		stored += string(event.BeforeJSON) + string(event.AfterJSON)
	}
	for _, plaintext := range []string{"smtp-secret-password", "turnstile-secret-value"} {
		if strings.Contains(stored, plaintext) {
			t.Fatalf("audit JSON leaked %q: %s", plaintext, stored)
		}
	}
}

func TestAdminSettingsMutationRollsBackWhenAuditFails(t *testing.T) {
	deps, server := newAdminHandlerTestServerWithoutAuditTable(t)

	response := doAdminRequest(t, http.MethodPut, server.URL+"/api/v1/admin/settings/registration", map[string]any{
		"enabled": false,
	}, deps.adminCookie)
	defer response.Body.Close()
	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("settings update with failing audit OK = true, want false")
	}

	registration, err := deps.settings.GetPublicRegistration(context.Background())
	if err != nil {
		t.Fatalf("GetPublicRegistration() error = %v", err)
	}
	if !registration.Enabled {
		t.Fatal("registration.Enabled = false, want rollback to default true")
	}
}

func TestPublicSettingsHandlerReturnsRegistrationWithoutSession(t *testing.T) {
	deps, server := newAdminHandlerTestServer(t)

	registrationResponse := doAdminRequest(t, http.MethodPut, server.URL+"/api/v1/admin/settings/registration", map[string]any{
		"enabled": false,
	}, deps.adminCookie)
	defer registrationResponse.Body.Close()
	_ = decodeAdminRegistrationSettingsResponse(t, registrationResponse.Body)

	publicResponse, err := http.Get(server.URL + "/api/v1/settings/registration")
	if err != nil {
		t.Fatalf("http.Get(public registration settings) error = %v", err)
	}
	defer publicResponse.Body.Close()
	settings := decodeAdminRegistrationSettingsResponse(t, publicResponse.Body)
	if settings.Enabled {
		t.Fatal("public registration Enabled = true, want false without requiring admin session")
	}
}

type adminSMTPSettingsTestResponse struct {
	Enabled        bool   `json:"enabled"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	PasswordMasked string `json:"password_masked"`
	From           string `json:"from"`
	UseTLS         bool   `json:"use_tls"`
	UseStartTLS    bool   `json:"use_start_tls"`
}

type adminTurnstileSettingsTestResponse struct {
	Enabled             bool   `json:"enabled"`
	SiteKey             string `json:"site_key"`
	Secret              string `json:"secret"`
	SecretMasked        string `json:"secret_masked"`
	ProtectLogin        bool   `json:"protect_login"`
	ProtectRegistration bool   `json:"protect_registration"`
	ProtectVerification bool   `json:"protect_verification"`
}

type adminRegistrationSettingsTestResponse struct {
	Enabled bool `json:"enabled"`
}

type fakeAdminSettingsSMTPTester struct {
	messages []mail.Message
}

func (s *fakeAdminSettingsSMTPTester) Send(ctx context.Context, message mail.Message) error {
	s.messages = append(s.messages, message)
	return nil
}

func decodeAdminSMTPSettingsResponse(t *testing.T, body io.Reader) adminSMTPSettingsTestResponse {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, message = %s, raw data = %s", envelope.Message, string(envelope.Data))
	}
	var settings adminSMTPSettingsTestResponse
	if err := json.Unmarshal(envelope.Data, &settings); err != nil {
		t.Fatalf("Unmarshal(smtp settings) error = %v", err)
	}
	return settings
}

func decodeAdminTurnstileSettingsResponse(t *testing.T, body io.Reader) adminTurnstileSettingsTestResponse {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, message = %s, raw data = %s", envelope.Message, string(envelope.Data))
	}
	var settings adminTurnstileSettingsTestResponse
	if err := json.Unmarshal(envelope.Data, &settings); err != nil {
		t.Fatalf("Unmarshal(turnstile settings) error = %v", err)
	}
	return settings
}

func decodeAdminRegistrationSettingsResponse(t *testing.T, body io.Reader) adminRegistrationSettingsTestResponse {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, message = %s, raw data = %s", envelope.Message, string(envelope.Data))
	}
	var settings adminRegistrationSettingsTestResponse
	if err := json.Unmarshal(envelope.Data, &settings); err != nil {
		t.Fatalf("Unmarshal(registration settings) error = %v", err)
	}
	return settings
}
