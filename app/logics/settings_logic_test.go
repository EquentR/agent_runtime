package logics

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/pkg/secret"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSettingsLogicUsesYAMLDefaultsWhenNoDBOverride(t *testing.T) {
	logic, _, _ := newSettingsLogicTestSubject(t, SettingsDefaults{
		PublicRegistration: PublicRegistrationSettings{Enabled: true},
		SMTP: SMTPSettings{
			Enabled:     true,
			Host:        "smtp.default.example.com",
			Port:        587,
			Username:    "default-user",
			Password:    "default-password",
			From:        "Agent Runtime <noreply@example.com>",
			UseStartTLS: true,
		},
		Turnstile: TurnstileSettings{
			Enabled:             true,
			SiteKey:             "site-key",
			Secret:              "turnstile-secret",
			ProtectLogin:        true,
			ProtectRegistration: true,
			ProtectVerification: true,
		},
	})

	smtp, err := logic.GetSMTP(context.Background())
	if err != nil {
		t.Fatalf("GetSMTP() error = %v", err)
	}
	if smtp.Host != "smtp.default.example.com" {
		t.Fatalf("smtp.Host = %q, want default host", smtp.Host)
	}
	if smtp.Password != "" {
		t.Fatalf("smtp.Password = %q, want plaintext password omitted", smtp.Password)
	}
	if smtp.PasswordMasked != "defa****word" {
		t.Fatalf("smtp.PasswordMasked = %q, want masked default password", smtp.PasswordMasked)
	}

	turnstile, err := logic.GetTurnstile(context.Background())
	if err != nil {
		t.Fatalf("GetTurnstile() error = %v", err)
	}
	if !turnstile.Enabled || !turnstile.ProtectLogin || !turnstile.ProtectRegistration || !turnstile.ProtectVerification {
		t.Fatalf("turnstile defaults not preserved: %#v", turnstile)
	}
	if turnstile.Secret != "" {
		t.Fatalf("turnstile.Secret = %q, want plaintext secret omitted", turnstile.Secret)
	}
	if turnstile.SecretMasked != "turn****cret" {
		t.Fatalf("turnstile.SecretMasked = %q, want masked default secret", turnstile.SecretMasked)
	}
}

func TestSettingsLogicDBOverrideMasksSecretsAndPreservesEncryptedValues(t *testing.T) {
	logic, db, codec := newSettingsLogicTestSubject(t, SettingsDefaults{
		PublicRegistration: PublicRegistrationSettings{Enabled: true},
		SMTP: SMTPSettings{
			Enabled:  true,
			Host:     "smtp.default.example.com",
			Port:     587,
			Username: "default-user",
			Password: "default-password",
			From:     "default@example.com",
		},
		Turnstile: TurnstileSettings{
			Enabled: true,
			SiteKey: "default-site-key",
			Secret:  "default-turnstile-secret",
		},
	})

	smtp, err := logic.UpdateSMTP(context.Background(), UpdateSMTPInput{
		Enabled:     true,
		Host:        "smtp.override.example.com",
		Port:        465,
		Username:    "override-user",
		Password:    "override-password",
		From:        "override@example.com",
		UseTLS:      true,
		UseStartTLS: false,
		UpdatedBy:   "admin",
	})
	if err != nil {
		t.Fatalf("UpdateSMTP() error = %v", err)
	}
	if smtp.Password != "" {
		t.Fatalf("UpdateSMTP() returned plaintext password %q", smtp.Password)
	}
	if smtp.PasswordMasked != "over****word" {
		t.Fatalf("UpdateSMTP() PasswordMasked = %q, want masked override", smtp.PasswordMasked)
	}

	var storedSMTP models.SystemSetting
	if err := db.Where("key = ?", "smtp").Take(&storedSMTP).Error; err != nil {
		t.Fatalf("load smtp setting: %v", err)
	}
	if !storedSMTP.Encrypted {
		t.Fatal("smtp setting Encrypted = false, want true")
	}
	var storedSMTPPayload struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(storedSMTP.ValueJSON, &storedSMTPPayload); err != nil {
		t.Fatalf("unmarshal smtp payload: %v", err)
	}
	if storedSMTPPayload.Password == "" || storedSMTPPayload.Password == "override-password" {
		t.Fatalf("stored smtp password = %q, want encrypted value", storedSMTPPayload.Password)
	}
	decryptedPassword, err := codec.DecryptString(storedSMTPPayload.Password)
	if err != nil {
		t.Fatalf("DecryptString(stored smtp password) error = %v", err)
	}
	if decryptedPassword != "override-password" {
		t.Fatalf("stored smtp password decrypts to %q, want override-password", decryptedPassword)
	}

	turnstile, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
		Enabled:             true,
		SiteKey:             "override-site-key",
		Secret:              "override-turnstile-secret",
		ProtectLogin:        true,
		ProtectRegistration: false,
		ProtectVerification: true,
		UpdatedBy:           "admin",
	})
	if err != nil {
		t.Fatalf("UpdateTurnstile() error = %v", err)
	}
	if turnstile.Secret != "" {
		t.Fatalf("UpdateTurnstile() returned plaintext secret %q", turnstile.Secret)
	}
	if turnstile.SecretMasked != "over****cret" {
		t.Fatalf("UpdateTurnstile() SecretMasked = %q, want masked override", turnstile.SecretMasked)
	}

	reloadedSMTP, err := logic.GetSMTP(context.Background())
	if err != nil {
		t.Fatalf("GetSMTP() after override error = %v", err)
	}
	if reloadedSMTP.Host != "smtp.override.example.com" {
		t.Fatalf("reloaded SMTP host = %q, want override", reloadedSMTP.Host)
	}
	if reloadedSMTP.Password != "" || strings.Contains(reloadedSMTP.PasswordMasked, "override-password") {
		t.Fatalf("reloaded SMTP leaked plaintext: %#v", reloadedSMTP)
	}

	var storedTurnstile models.SystemSetting
	if err := db.Where("key = ?", "turnstile").Take(&storedTurnstile).Error; err != nil {
		t.Fatalf("load turnstile setting: %v", err)
	}
	if !storedTurnstile.Encrypted {
		t.Fatal("turnstile setting Encrypted = false, want true")
	}
	var storedTurnstilePayload struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(storedTurnstile.ValueJSON, &storedTurnstilePayload); err != nil {
		t.Fatalf("unmarshal turnstile payload: %v", err)
	}
	if storedTurnstilePayload.Secret == "" || storedTurnstilePayload.Secret == "override-turnstile-secret" {
		t.Fatalf("stored turnstile secret = %q, want encrypted value", storedTurnstilePayload.Secret)
	}
	decryptedSecret, err := codec.DecryptString(storedTurnstilePayload.Secret)
	if err != nil {
		t.Fatalf("DecryptString(stored turnstile secret) error = %v", err)
	}
	if decryptedSecret != "override-turnstile-secret" {
		t.Fatalf("stored turnstile secret decrypts to %q, want override-turnstile-secret", decryptedSecret)
	}
}

func TestSettingsLogicPublicRegistrationDefaultsToEnabled(t *testing.T) {
	logic, _, _ := newSettingsLogicTestSubject(t, SettingsDefaults{})

	settings, err := logic.GetPublicRegistration(context.Background())
	if err != nil {
		t.Fatalf("GetPublicRegistration() error = %v", err)
	}
	if !settings.Enabled {
		t.Fatal("public registration default Enabled = false, want true")
	}

	updated, err := logic.UpdatePublicRegistration(context.Background(), UpdatePublicRegistrationInput{
		Enabled:   false,
		UpdatedBy: "admin",
	})
	if err != nil {
		t.Fatalf("UpdatePublicRegistration() error = %v", err)
	}
	if updated.Enabled {
		t.Fatal("updated public registration Enabled = true, want false")
	}
	reloaded, err := logic.GetPublicRegistration(context.Background())
	if err != nil {
		t.Fatalf("GetPublicRegistration() after update error = %v", err)
	}
	if reloaded.Enabled {
		t.Fatal("reloaded public registration Enabled = true, want false")
	}
}

func newSettingsLogicTestSubject(t *testing.T, defaults SettingsDefaults) (*SettingsLogic, *gorm.DB, *secret.Codec) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	if err := db.AutoMigrate(&models.SystemSetting{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	logic, err := NewSettingsLogic(db, defaults, codec)
	if err != nil {
		t.Fatalf("NewSettingsLogic() error = %v", err)
	}
	return logic, db, codec
}
