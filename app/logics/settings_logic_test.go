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

func TestSettingsLogicClearsSMTPPasswordAndTurnstileSecret(t *testing.T) {
	logic, db, _ := newSettingsLogicTestSubject(t, SettingsDefaults{})

	if _, err := logic.UpdateSMTP(context.Background(), UpdateSMTPInput{
		Enabled:  true,
		Host:     "smtp.example.com",
		Port:     587,
		Username: "smtp-user",
		Password: "stored-password",
		From:     "noreply@example.com",
	}); err != nil {
		t.Fatalf("UpdateSMTP(seed) error = %v", err)
	}
	preservedSMTP, err := logic.UpdateSMTP(context.Background(), UpdateSMTPInput{
		Enabled:  true,
		Host:     "smtp.example.com",
		Port:     587,
		Username: "smtp-user",
		From:     "noreply@example.com",
	})
	if err != nil {
		t.Fatalf("UpdateSMTP(preserve) error = %v", err)
	}
	if preservedSMTP.PasswordMasked == "" {
		t.Fatal("UpdateSMTP(preserve) PasswordMasked = empty, want preserved password mask")
	}
	clearedSMTP, err := logic.UpdateSMTP(context.Background(), UpdateSMTPInput{
		Enabled:       false,
		Host:          "smtp.example.com",
		Port:          587,
		Username:      "smtp-user",
		From:          "noreply@example.com",
		ClearPassword: true,
	})
	if err != nil {
		t.Fatalf("UpdateSMTP(clear) error = %v", err)
	}
	if clearedSMTP.PasswordMasked != "" || clearedSMTP.Password != "" {
		t.Fatalf("UpdateSMTP(clear) returned secret fields: %#v", clearedSMTP)
	}
	storedSMTP := loadSettingsTestSetting(t, db, "smtp")
	if storedSMTP.Encrypted {
		t.Fatal("cleared smtp setting Encrypted = true, want false")
	}
	var storedSMTPPayload struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(storedSMTP.ValueJSON, &storedSMTPPayload); err != nil {
		t.Fatalf("unmarshal cleared smtp payload: %v", err)
	}
	if storedSMTPPayload.Password != "" {
		t.Fatalf("cleared smtp password = %q, want empty", storedSMTPPayload.Password)
	}

	if _, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
		Enabled: true,
		SiteKey: "site-key",
		Secret:  "stored-secret",
	}); err != nil {
		t.Fatalf("UpdateTurnstile(seed) error = %v", err)
	}
	preservedTurnstile, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
		Enabled: true,
		SiteKey: "site-key",
	})
	if err != nil {
		t.Fatalf("UpdateTurnstile(preserve) error = %v", err)
	}
	if preservedTurnstile.SecretMasked == "" {
		t.Fatal("UpdateTurnstile(preserve) SecretMasked = empty, want preserved secret mask")
	}
	clearedTurnstile, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
		Enabled:     false,
		SiteKey:     "site-key",
		ClearSecret: true,
	})
	if err != nil {
		t.Fatalf("UpdateTurnstile(clear) error = %v", err)
	}
	if clearedTurnstile.SecretMasked != "" || clearedTurnstile.Secret != "" {
		t.Fatalf("UpdateTurnstile(clear) returned secret fields: %#v", clearedTurnstile)
	}
	storedTurnstile := loadSettingsTestSetting(t, db, "turnstile")
	if storedTurnstile.Encrypted {
		t.Fatal("cleared turnstile setting Encrypted = true, want false")
	}
	var storedTurnstilePayload struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(storedTurnstile.ValueJSON, &storedTurnstilePayload); err != nil {
		t.Fatalf("unmarshal cleared turnstile payload: %v", err)
	}
	if storedTurnstilePayload.Secret != "" {
		t.Fatalf("cleared turnstile secret = %q, want empty", storedTurnstilePayload.Secret)
	}
}

func TestSettingsLogicSMTPReplaceAndClearDoNotDecryptCorruptStoredPassword(t *testing.T) {
	logic, db, codec := newSettingsLogicTestSubject(t, SettingsDefaults{})
	seedCorruptSMTPSetting(t, db)

	replaced, err := logic.UpdateSMTP(context.Background(), UpdateSMTPInput{
		Enabled:  true,
		Host:     "smtp.example.com",
		Port:     587,
		Username: "smtp-user",
		Password: "replacement-password",
		From:     "noreply@example.com",
	})
	if err != nil {
		t.Fatalf("UpdateSMTP(replace corrupt) error = %v", err)
	}
	if replaced.PasswordMasked == "" {
		t.Fatal("UpdateSMTP(replace corrupt) PasswordMasked = empty, want replacement mask")
	}
	storedSMTP := loadSettingsTestSetting(t, db, "smtp")
	var storedSMTPPayload struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(storedSMTP.ValueJSON, &storedSMTPPayload); err != nil {
		t.Fatalf("unmarshal replaced smtp payload: %v", err)
	}
	decrypted, err := codec.DecryptString(storedSMTPPayload.Password)
	if err != nil {
		t.Fatalf("DecryptString(replaced smtp password) error = %v", err)
	}
	if decrypted != "replacement-password" {
		t.Fatalf("replaced smtp password decrypts to %q, want replacement-password", decrypted)
	}

	seedCorruptSMTPSetting(t, db)
	cleared, err := logic.UpdateSMTP(context.Background(), UpdateSMTPInput{
		Enabled:       false,
		Host:          "smtp.example.com",
		Port:          587,
		Username:      "smtp-user",
		From:          "noreply@example.com",
		ClearPassword: true,
	})
	if err != nil {
		t.Fatalf("UpdateSMTP(clear corrupt) error = %v", err)
	}
	if cleared.PasswordMasked != "" {
		t.Fatalf("UpdateSMTP(clear corrupt) PasswordMasked = %q, want empty", cleared.PasswordMasked)
	}
	storedSMTP = loadSettingsTestSetting(t, db, "smtp")
	if storedSMTP.Encrypted {
		t.Fatal("cleared corrupt smtp setting Encrypted = true, want false")
	}
	if err := json.Unmarshal(storedSMTP.ValueJSON, &storedSMTPPayload); err != nil {
		t.Fatalf("unmarshal cleared corrupt smtp payload: %v", err)
	}
	if storedSMTPPayload.Password != "" {
		t.Fatalf("cleared corrupt smtp password = %q, want empty", storedSMTPPayload.Password)
	}
}

func TestSettingsLogicTurnstileReplaceAndClearDoNotDecryptCorruptStoredSecret(t *testing.T) {
	logic, db, codec := newSettingsLogicTestSubject(t, SettingsDefaults{})
	seedCorruptTurnstileSetting(t, db)

	replaced, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
		Enabled: true,
		SiteKey: "site-key",
		Secret:  "replacement-secret",
	})
	if err != nil {
		t.Fatalf("UpdateTurnstile(replace corrupt) error = %v", err)
	}
	if replaced.SecretMasked == "" {
		t.Fatal("UpdateTurnstile(replace corrupt) SecretMasked = empty, want replacement mask")
	}
	storedTurnstile := loadSettingsTestSetting(t, db, "turnstile")
	var storedTurnstilePayload struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(storedTurnstile.ValueJSON, &storedTurnstilePayload); err != nil {
		t.Fatalf("unmarshal replaced turnstile payload: %v", err)
	}
	decrypted, err := codec.DecryptString(storedTurnstilePayload.Secret)
	if err != nil {
		t.Fatalf("DecryptString(replaced turnstile secret) error = %v", err)
	}
	if decrypted != "replacement-secret" {
		t.Fatalf("replaced turnstile secret decrypts to %q, want replacement-secret", decrypted)
	}

	seedCorruptTurnstileSetting(t, db)
	cleared, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
		Enabled:     false,
		SiteKey:     "site-key",
		ClearSecret: true,
	})
	if err != nil {
		t.Fatalf("UpdateTurnstile(clear corrupt) error = %v", err)
	}
	if cleared.SecretMasked != "" {
		t.Fatalf("UpdateTurnstile(clear corrupt) SecretMasked = %q, want empty", cleared.SecretMasked)
	}
	storedTurnstile = loadSettingsTestSetting(t, db, "turnstile")
	if storedTurnstile.Encrypted {
		t.Fatal("cleared corrupt turnstile setting Encrypted = true, want false")
	}
	if err := json.Unmarshal(storedTurnstile.ValueJSON, &storedTurnstilePayload); err != nil {
		t.Fatalf("unmarshal cleared corrupt turnstile payload: %v", err)
	}
	if storedTurnstilePayload.Secret != "" {
		t.Fatalf("cleared corrupt turnstile secret = %q, want empty", storedTurnstilePayload.Secret)
	}
}

func TestSettingsLogicUpdateTurnstileValidatesEnabledConfigBeforePersisting(t *testing.T) {
	t.Run("enabled missing site key", func(t *testing.T) {
		logic, db, _ := newSettingsLogicTestSubject(t, SettingsDefaults{})

		_, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
			Enabled: true,
			SiteKey: " ",
			Secret:  "turnstile-secret",
		})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "site key") {
			t.Fatalf("UpdateTurnstile(missing site key) error = %v, want site key validation error", err)
		}
		if count := countSettingsTestSettings(t, db, "turnstile"); count != 0 {
			t.Fatalf("turnstile settings count = %d, want 0", count)
		}
	})

	t.Run("enabled missing secret", func(t *testing.T) {
		logic, db, _ := newSettingsLogicTestSubject(t, SettingsDefaults{})

		_, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
			Enabled: true,
			SiteKey: "site-key",
		})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "secret") {
			t.Fatalf("UpdateTurnstile(missing secret) error = %v, want secret validation error", err)
		}
		if count := countSettingsTestSettings(t, db, "turnstile"); count != 0 {
			t.Fatalf("turnstile settings count = %d, want 0", count)
		}
	})

	t.Run("enabled whitespace secret", func(t *testing.T) {
		logic, db, _ := newSettingsLogicTestSubject(t, SettingsDefaults{})

		_, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
			Enabled: true,
			SiteKey: "site-key",
			Secret:  " ",
		})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "secret") {
			t.Fatalf("UpdateTurnstile(whitespace secret) error = %v, want secret validation error", err)
		}
		if count := countSettingsTestSettings(t, db, "turnstile"); count != 0 {
			t.Fatalf("turnstile settings count = %d, want 0", count)
		}
	})

	t.Run("enabled clear secret rejected", func(t *testing.T) {
		logic, db, _ := newSettingsLogicTestSubject(t, SettingsDefaults{})
		if _, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
			Enabled: true,
			SiteKey: "site-key",
			Secret:  "stored-secret",
		}); err != nil {
			t.Fatalf("UpdateTurnstile(seed) error = %v", err)
		}

		_, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
			Enabled:     true,
			SiteKey:     "site-key",
			ClearSecret: true,
		})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "secret") {
			t.Fatalf("UpdateTurnstile(clear enabled secret) error = %v, want secret validation error", err)
		}
		stored := loadSettingsTestSetting(t, db, "turnstile")
		if !stored.Encrypted {
			t.Fatal("stored turnstile setting Encrypted = false, want preserved encrypted secret")
		}
		var payload struct {
			Secret string `json:"secret"`
		}
		if err := json.Unmarshal(stored.ValueJSON, &payload); err != nil {
			t.Fatalf("unmarshal preserved turnstile payload: %v", err)
		}
		if payload.Secret == "" {
			t.Fatal("stored turnstile secret = empty, want preserved secret")
		}
	})

	t.Run("enabled preserves existing secret", func(t *testing.T) {
		logic, _, _ := newSettingsLogicTestSubject(t, SettingsDefaults{})
		if _, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
			Enabled: true,
			SiteKey: "site-key",
			Secret:  "stored-secret",
		}); err != nil {
			t.Fatalf("UpdateTurnstile(seed) error = %v", err)
		}

		settings, err := logic.UpdateTurnstile(context.Background(), UpdateTurnstileInput{
			Enabled: true,
			SiteKey: "updated-site-key",
		})
		if err != nil {
			t.Fatalf("UpdateTurnstile(preserve enabled secret) error = %v", err)
		}
		if settings.Secret != "" {
			t.Fatalf("UpdateTurnstile(preserve enabled secret) Secret = %q, want plaintext omitted", settings.Secret)
		}
		if settings.SecretMasked == "" {
			t.Fatal("UpdateTurnstile(preserve enabled secret) SecretMasked = empty, want preserved secret mask")
		}
	})
}

func TestSettingsLogicUpdateSMTPValidatesEnabledConfigBeforePersisting(t *testing.T) {
	logic, db, _ := newSettingsLogicTestSubject(t, SettingsDefaults{})

	_, err := logic.UpdateSMTP(context.Background(), UpdateSMTPInput{
		Enabled:  true,
		Host:     "smtp.example.com",
		Port:     587,
		Username: "smtp-user",
		From:     "noreply@example.com",
	})
	if err == nil {
		t.Fatal("UpdateSMTP(invalid enabled config) error = nil, want validation error")
	}

	var count int64
	if err := db.Model(&models.SystemSetting{}).Where("key = ?", "smtp").Count(&count).Error; err != nil {
		t.Fatalf("count smtp settings: %v", err)
	}
	if count != 0 {
		t.Fatalf("smtp settings count = %d, want 0", count)
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

func TestSettingsLogicPublicRegistrationExplicitFalseDefaultStaysDisabled(t *testing.T) {
	logic, _, _ := newSettingsLogicTestSubject(t, SettingsDefaults{
		PublicRegistrationConfigured: true,
		PublicRegistration:           PublicRegistrationSettings{Enabled: false},
	})

	settings, err := logic.GetPublicRegistration(context.Background())
	if err != nil {
		t.Fatalf("GetPublicRegistration() error = %v", err)
	}
	if settings.Enabled {
		t.Fatal("explicit false public registration default Enabled = true, want false")
	}
}

func TestSettingsLogicGetPublicTurnstileDoesNotDecryptCorruptSecret(t *testing.T) {
	logic, db, _ := newSettingsLogicTestSubject(t, SettingsDefaults{})
	seedCorruptTurnstileSetting(t, db)

	settings, err := logic.GetPublicTurnstile(context.Background())
	if err != nil {
		t.Fatalf("GetPublicTurnstile() error = %v, want public fields without decrypting secret", err)
	}
	if !settings.Enabled || settings.SiteKey != "site-key" || !settings.ProtectLogin || !settings.ProtectRegistration || !settings.ProtectVerification {
		t.Fatalf("public turnstile settings = %#v, want non-sensitive stored fields", settings)
	}
	if _, err := logic.GetTurnstile(context.Background()); err == nil {
		t.Fatal("GetTurnstile() error = nil, want corrupt secret decrypt error on private path")
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

func loadSettingsTestSetting(t *testing.T, db *gorm.DB, key string) models.SystemSetting {
	t.Helper()

	var setting models.SystemSetting
	if err := db.Where("key = ?", key).Take(&setting).Error; err != nil {
		t.Fatalf("load setting %q: %v", key, err)
	}
	return setting
}

func countSettingsTestSettings(t *testing.T, db *gorm.DB, key string) int64 {
	t.Helper()

	var count int64
	if err := db.Model(&models.SystemSetting{}).Where("key = ?", key).Count(&count).Error; err != nil {
		t.Fatalf("count setting %q: %v", key, err)
	}
	return count
}

func seedCorruptSMTPSetting(t *testing.T, db *gorm.DB) {
	t.Helper()

	seedSettingsTestSetting(t, db, "smtp", smtpSettingPayload{
		Enabled:     true,
		Host:        "smtp.example.com",
		Port:        587,
		Username:    "smtp-user",
		Password:    "v1:not-valid-ciphertext",
		From:        "noreply@example.com",
		UseStartTLS: true,
	}, true)
}

func seedCorruptTurnstileSetting(t *testing.T, db *gorm.DB) {
	t.Helper()

	seedSettingsTestSetting(t, db, "turnstile", turnstileSettingPayload{
		Enabled:             true,
		SiteKey:             "site-key",
		Secret:              "v1:not-valid-ciphertext",
		ProtectLogin:        true,
		ProtectRegistration: true,
		ProtectVerification: true,
	}, true)
}

func seedSettingsTestSetting(t *testing.T, db *gorm.DB, key string, value any, encrypted bool) {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal setting %q: %v", key, err)
	}
	if err := db.Where("key = ?", key).Delete(&models.SystemSetting{}).Error; err != nil {
		t.Fatalf("delete setting %q: %v", key, err)
	}
	if err := db.Create(&models.SystemSetting{Key: key, ValueJSON: payload, Encrypted: encrypted}).Error; err != nil {
		t.Fatalf("create setting %q: %v", key, err)
	}
}
