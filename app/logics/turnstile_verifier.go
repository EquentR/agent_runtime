package logics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultTurnstileVerifyEndpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type TurnstileSettingsReader interface {
	GetTurnstile(ctx context.Context) (TurnstileSettings, error)
}

type TurnstileSecretReader interface {
	GetTurnstileForVerify(ctx context.Context) (TurnstileSettings, error)
}

type TurnstileVerifier interface {
	Verify(ctx context.Context, token string, remoteIP string) error
}

type CloudflareTurnstileVerifier struct {
	settings TurnstileSecretReader
	client   *http.Client
	endpoint string
}

func NewCloudflareTurnstileVerifier(settings TurnstileSecretReader) (*CloudflareTurnstileVerifier, error) {
	if settings == nil {
		return nil, fmt.Errorf("turnstile settings are required")
	}
	return &CloudflareTurnstileVerifier{
		settings: settings,
		client:   &http.Client{Timeout: 5 * time.Second},
		endpoint: defaultTurnstileVerifyEndpoint,
	}, nil
}

func (v *CloudflareTurnstileVerifier) Verify(ctx context.Context, token string, remoteIP string) error {
	if v == nil || v.settings == nil {
		return fmt.Errorf("turnstile verifier is not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("turnstile token is required")
	}
	settings, err := v.settings.GetTurnstileForVerify(ctx)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return nil
	}
	secret := strings.TrimSpace(settings.Secret)
	if secret == "" {
		return fmt.Errorf("turnstile secret is required")
	}

	form := url.Values{}
	form.Set("secret", secret)
	form.Set("response", token)
	if remoteIP = strings.TrimSpace(remoteIP); remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := v.client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("verify turnstile: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("turnstile verification returned status %d", response.StatusCode)
	}

	var payload struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode turnstile verification response: %w", err)
	}
	if !payload.Success {
		if len(payload.ErrorCodes) > 0 {
			return fmt.Errorf("turnstile verification failed: %s", strings.Join(payload.ErrorCodes, ","))
		}
		return fmt.Errorf("turnstile verification failed")
	}
	return nil
}
