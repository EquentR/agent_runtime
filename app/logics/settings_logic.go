package logics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/pkg/mail"
	"github.com/EquentR/agent_runtime/pkg/secret"
	"gorm.io/gorm"
)

const (
	settingKeyPublicRegistration = "public_registration"
	settingKeySMTP               = "smtp"
	settingKeyTurnstile          = "turnstile"
)

type SettingsDefaults struct {
	PublicRegistrationConfigured bool
	PublicRegistration           PublicRegistrationSettings
	SMTP                         SMTPSettings
	Turnstile                    TurnstileSettings
}

type PublicRegistrationSettings struct {
	Enabled bool `json:"enabled"`
}

type UpdatePublicRegistrationInput struct {
	Enabled   bool
	UpdatedBy string
}

type SMTPSettings struct {
	Enabled        bool   `json:"enabled"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password,omitempty"`
	PasswordMasked string `json:"password_masked,omitempty"`
	From           string `json:"from"`
	UseTLS         bool   `json:"use_tls"`
	UseStartTLS    bool   `json:"use_start_tls"`
}

type UpdateSMTPInput struct {
	Enabled       bool
	Host          string
	Port          int
	Username      string
	Password      string
	ClearPassword bool
	From          string
	UseTLS        bool
	UseStartTLS   bool
	UpdatedBy     string
}

type TurnstileSettings struct {
	Enabled             bool   `json:"enabled"`
	SiteKey             string `json:"site_key"`
	Secret              string `json:"secret,omitempty"`
	SecretMasked        string `json:"secret_masked,omitempty"`
	ProtectLogin        bool   `json:"protect_login"`
	ProtectRegistration bool   `json:"protect_registration"`
	ProtectVerification bool   `json:"protect_verification"`
}

type PublicTurnstileSettings struct {
	Enabled             bool   `json:"enabled"`
	SiteKey             string `json:"site_key"`
	ProtectLogin        bool   `json:"protect_login"`
	ProtectRegistration bool   `json:"protect_registration"`
	ProtectVerification bool   `json:"protect_verification"`
}

type UpdateTurnstileInput struct {
	Enabled             bool
	SiteKey             string
	Secret              string
	ClearSecret         bool
	ProtectLogin        bool
	ProtectRegistration bool
	ProtectVerification bool
	UpdatedBy           string
}

type SettingsLogic struct {
	db       *gorm.DB
	defaults SettingsDefaults
	codec    *secret.Codec
}

type smtpSettingPayload struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	From        string `json:"from"`
	UseTLS      bool   `json:"use_tls"`
	UseStartTLS bool   `json:"use_start_tls"`
}

type turnstileSettingPayload struct {
	Enabled             bool   `json:"enabled"`
	SiteKey             string `json:"site_key"`
	Secret              string `json:"secret"`
	ProtectLogin        bool   `json:"protect_login"`
	ProtectRegistration bool   `json:"protect_registration"`
	ProtectVerification bool   `json:"protect_verification"`
}

func NewSettingsLogic(db *gorm.DB, defaults SettingsDefaults, codec *secret.Codec) (*SettingsLogic, error) {
	if db == nil {
		return nil, fmt.Errorf("settings db is required")
	}
	if codec == nil {
		return nil, fmt.Errorf("settings secret codec is required")
	}
	if !defaults.PublicRegistrationConfigured {
		defaults.PublicRegistration.Enabled = true
	}
	return &SettingsLogic{db: db, defaults: defaults, codec: codec}, nil
}

func (l *SettingsLogic) Transaction(ctx context.Context, fn func(*SettingsLogic) error) error {
	if l == nil || l.db == nil {
		return fmt.Errorf("settings db is required")
	}
	if fn == nil {
		return nil
	}
	return l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		clone := *l
		clone.db = tx
		return fn(&clone)
	})
}

func (l *SettingsLogic) DB() *gorm.DB {
	if l == nil {
		return nil
	}
	return l.db
}

func (l *SettingsLogic) GetPublicRegistration(ctx context.Context) (PublicRegistrationSettings, error) {
	var settings PublicRegistrationSettings
	found, err := l.loadSetting(ctx, settingKeyPublicRegistration, &settings)
	if err != nil {
		return PublicRegistrationSettings{}, err
	}
	if found {
		return settings, nil
	}
	return l.defaults.PublicRegistration, nil
}

func (l *SettingsLogic) UpdatePublicRegistration(ctx context.Context, input UpdatePublicRegistrationInput) (PublicRegistrationSettings, error) {
	settings := PublicRegistrationSettings{Enabled: input.Enabled}
	if err := l.saveSetting(ctx, settingKeyPublicRegistration, settings, false, input.UpdatedBy); err != nil {
		return PublicRegistrationSettings{}, err
	}
	return settings, nil
}

func (l *SettingsLogic) GetSMTP(ctx context.Context) (SMTPSettings, error) {
	settings, err := l.loadSMTP(ctx)
	if err != nil {
		return SMTPSettings{}, err
	}
	return maskSMTP(settings), nil
}

func (l *SettingsLogic) GetSMTPForSend(ctx context.Context) (SMTPSettings, error) {
	return l.loadSMTP(ctx)
}

func (l *SettingsLogic) UpdateSMTP(ctx context.Context, input UpdateSMTPInput) (SMTPSettings, error) {
	settings := SMTPSettings{
		Enabled:     input.Enabled,
		Host:        strings.TrimSpace(input.Host),
		Port:        input.Port,
		Username:    strings.TrimSpace(input.Username),
		From:        strings.TrimSpace(input.From),
		UseTLS:      input.UseTLS,
		UseStartTLS: input.UseStartTLS,
	}
	if input.ClearPassword {
		settings.Password = ""
	} else if input.Password != "" {
		settings.Password = input.Password
	} else {
		current, err := l.loadSMTP(ctx)
		if err != nil {
			return SMTPSettings{}, err
		}
		settings.Password = current.Password
	}
	if err := validateSMTPSettingsForSend(settings); err != nil {
		return SMTPSettings{}, err
	}
	payload, err := l.encryptSMTP(settings)
	if err != nil {
		return SMTPSettings{}, err
	}
	if err := l.saveSetting(ctx, settingKeySMTP, payload, payload.Password != "", input.UpdatedBy); err != nil {
		return SMTPSettings{}, err
	}
	return maskSMTP(settings), nil
}

func (l *SettingsLogic) GetTurnstile(ctx context.Context) (TurnstileSettings, error) {
	settings, err := l.loadTurnstile(ctx)
	if err != nil {
		return TurnstileSettings{}, err
	}
	return maskTurnstile(settings), nil
}

func (l *SettingsLogic) GetPublicTurnstile(ctx context.Context) (PublicTurnstileSettings, error) {
	settings, err := l.loadPublicTurnstile(ctx)
	if err != nil {
		return PublicTurnstileSettings{}, err
	}
	return settings, nil
}

func (l *SettingsLogic) loadPublicTurnstile(ctx context.Context) (PublicTurnstileSettings, error) {
	var payload turnstileSettingPayload
	found, err := l.loadSetting(ctx, settingKeyTurnstile, &payload)
	if err != nil {
		return PublicTurnstileSettings{}, err
	}
	settings := l.defaults.Turnstile
	if found {
		settings = TurnstileSettings{
			Enabled:             payload.Enabled,
			SiteKey:             payload.SiteKey,
			ProtectLogin:        payload.ProtectLogin,
			ProtectRegistration: payload.ProtectRegistration,
			ProtectVerification: payload.ProtectVerification,
		}
	}
	return PublicTurnstileSettings{
		Enabled:             settings.Enabled,
		SiteKey:             settings.SiteKey,
		ProtectLogin:        settings.ProtectLogin,
		ProtectRegistration: settings.ProtectRegistration,
		ProtectVerification: settings.ProtectVerification,
	}, nil
}

func (l *SettingsLogic) GetTurnstileForVerify(ctx context.Context) (TurnstileSettings, error) {
	return l.loadTurnstile(ctx)
}

func (l *SettingsLogic) UpdateTurnstile(ctx context.Context, input UpdateTurnstileInput) (TurnstileSettings, error) {
	settings := TurnstileSettings{
		Enabled:             input.Enabled,
		SiteKey:             strings.TrimSpace(input.SiteKey),
		ProtectLogin:        input.ProtectLogin,
		ProtectRegistration: input.ProtectRegistration,
		ProtectVerification: input.ProtectVerification,
	}
	if input.ClearSecret {
		settings.Secret = ""
	} else if input.Secret != "" {
		settings.Secret = input.Secret
	} else {
		current, err := l.loadTurnstile(ctx)
		if err != nil {
			return TurnstileSettings{}, err
		}
		settings.Secret = current.Secret
	}
	if err := validateTurnstileSettings(settings); err != nil {
		return TurnstileSettings{}, err
	}
	payload, err := l.encryptTurnstile(settings)
	if err != nil {
		return TurnstileSettings{}, err
	}
	if err := l.saveSetting(ctx, settingKeyTurnstile, payload, payload.Secret != "", input.UpdatedBy); err != nil {
		return TurnstileSettings{}, err
	}
	return maskTurnstile(settings), nil
}

func (l *SettingsLogic) loadSMTP(ctx context.Context) (SMTPSettings, error) {
	var payload smtpSettingPayload
	found, err := l.loadSetting(ctx, settingKeySMTP, &payload)
	if err != nil {
		return SMTPSettings{}, err
	}
	if !found {
		return l.defaults.SMTP, nil
	}
	return l.decryptSMTP(payload)
}

func (l *SettingsLogic) loadTurnstile(ctx context.Context) (TurnstileSettings, error) {
	var payload turnstileSettingPayload
	found, err := l.loadSetting(ctx, settingKeyTurnstile, &payload)
	if err != nil {
		return TurnstileSettings{}, err
	}
	if !found {
		return l.defaults.Turnstile, nil
	}
	return l.decryptTurnstile(payload)
}

func (l *SettingsLogic) loadSetting(ctx context.Context, key string, out any) (bool, error) {
	var row models.SystemSetting
	err := l.db.WithContext(ctx).Where("key = ?", key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(row.ValueJSON, out); err != nil {
		return false, fmt.Errorf("decode setting %q: %w", key, err)
	}
	return true, nil
}

func (l *SettingsLogic) saveSetting(ctx context.Context, key string, value any, encrypted bool, updatedBy string) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode setting %q: %w", key, err)
	}
	now := time.Now().UTC()
	row := models.SystemSetting{
		Key:       key,
		ValueJSON: payload,
		Encrypted: encrypted,
		UpdatedBy: strings.TrimSpace(updatedBy),
		CreatedAt: now,
		UpdatedAt: now,
	}
	return l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.SystemSetting
		err := tx.Where("key = ?", key).Take(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&row).Error
		}
		if err != nil {
			return err
		}
		updates := map[string]any{
			"value_json": payload,
			"encrypted":  encrypted,
			"updated_by": row.UpdatedBy,
			"updated_at": now,
		}
		return tx.Model(&models.SystemSetting{}).Where("key = ?", key).Updates(updates).Error
	})
}

func (l *SettingsLogic) encryptSMTP(settings SMTPSettings) (smtpSettingPayload, error) {
	password := settings.Password
	if password != "" {
		encrypted, err := l.codec.EncryptString(password)
		if err != nil {
			return smtpSettingPayload{}, err
		}
		password = encrypted
	}
	return smtpSettingPayload{
		Enabled:     settings.Enabled,
		Host:        settings.Host,
		Port:        settings.Port,
		Username:    settings.Username,
		Password:    password,
		From:        settings.From,
		UseTLS:      settings.UseTLS,
		UseStartTLS: settings.UseStartTLS,
	}, nil
}

func (l *SettingsLogic) decryptSMTP(payload smtpSettingPayload) (SMTPSettings, error) {
	password := payload.Password
	if password != "" {
		decrypted, err := l.codec.DecryptString(password)
		if err != nil {
			return SMTPSettings{}, err
		}
		password = decrypted
	}
	return SMTPSettings{
		Enabled:     payload.Enabled,
		Host:        payload.Host,
		Port:        payload.Port,
		Username:    payload.Username,
		Password:    password,
		From:        payload.From,
		UseTLS:      payload.UseTLS,
		UseStartTLS: payload.UseStartTLS,
	}, nil
}

func (l *SettingsLogic) encryptTurnstile(settings TurnstileSettings) (turnstileSettingPayload, error) {
	turnstileSecret := settings.Secret
	if turnstileSecret != "" {
		encrypted, err := l.codec.EncryptString(turnstileSecret)
		if err != nil {
			return turnstileSettingPayload{}, err
		}
		turnstileSecret = encrypted
	}
	return turnstileSettingPayload{
		Enabled:             settings.Enabled,
		SiteKey:             settings.SiteKey,
		Secret:              turnstileSecret,
		ProtectLogin:        settings.ProtectLogin,
		ProtectRegistration: settings.ProtectRegistration,
		ProtectVerification: settings.ProtectVerification,
	}, nil
}

func (l *SettingsLogic) decryptTurnstile(payload turnstileSettingPayload) (TurnstileSettings, error) {
	turnstileSecret := payload.Secret
	if turnstileSecret != "" {
		decrypted, err := l.codec.DecryptString(turnstileSecret)
		if err != nil {
			return TurnstileSettings{}, err
		}
		turnstileSecret = decrypted
	}
	return TurnstileSettings{
		Enabled:             payload.Enabled,
		SiteKey:             payload.SiteKey,
		Secret:              turnstileSecret,
		ProtectLogin:        payload.ProtectLogin,
		ProtectRegistration: payload.ProtectRegistration,
		ProtectVerification: payload.ProtectVerification,
	}, nil
}

func maskSMTP(settings SMTPSettings) SMTPSettings {
	if settings.Password != "" {
		settings.PasswordMasked = secret.MaskSecret(settings.Password)
	}
	settings.Password = ""
	return settings
}

func maskTurnstile(settings TurnstileSettings) TurnstileSettings {
	if settings.Secret != "" {
		settings.SecretMasked = secret.MaskSecret(settings.Secret)
	}
	settings.Secret = ""
	return settings
}

func validateSMTPSettingsForSend(settings SMTPSettings) error {
	return (mail.SMTPConfig{
		Enabled:     settings.Enabled,
		Host:        settings.Host,
		Port:        settings.Port,
		Username:    settings.Username,
		Password:    settings.Password,
		From:        settings.From,
		UseTLS:      settings.UseTLS,
		UseStartTLS: settings.UseStartTLS,
	}).ValidateForSend()
}

func validateTurnstileSettings(settings TurnstileSettings) error {
	if !settings.Enabled {
		return nil
	}
	if strings.TrimSpace(settings.SiteKey) == "" {
		return fmt.Errorf("turnstile site key is required")
	}
	if strings.TrimSpace(settings.Secret) == "" {
		return fmt.Errorf("turnstile secret is required")
	}
	return nil
}
