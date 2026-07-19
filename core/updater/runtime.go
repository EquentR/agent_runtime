package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var ErrProcessGone = errors.New("updater process is no longer running")

const UpdateHealthTokenHeader = "X-Ice-Art-Update-Token"

type HealthResponse struct {
	Ready   bool   `json:"ready"`
	Version string `json:"version"`
}

type DirectRuntime struct {
	healthURL string
	client    *http.Client
}

func NewDirectRuntime(healthURL string, client *http.Client) *DirectRuntime {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return &DirectRuntime{healthURL: strings.TrimSpace(healthURL), client: client}
}

func (r *DirectRuntime) Health(ctx context.Context, _ int, version, token string) error {
	if r == nil || r.client == nil || r.healthURL == "" {
		return fmt.Errorf("health runtime is not configured")
	}
	var lastErr error
	for {
		if err := r.healthOnce(ctx, version, token); err == nil {
			return nil
		} else {
			lastErr = err
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("health check timed out: %w: %v", ctx.Err(), lastErr)
		case <-timer.C:
		}
	}
}

func (r *DirectRuntime) healthOnce(ctx context.Context, version, token string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.healthURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set(UpdateHealthTokenHeader, token)
	response, err := r.client.Do(request)
	if err != nil {
		return err
	}
	var health HealthResponse
	decodeErr := json.NewDecoder(response.Body).Decode(&health)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", response.Status)
	}
	if decodeErr != nil {
		return decodeErr
	}
	if health.Ready && health.Version == version {
		return nil
	}
	return fmt.Errorf("service is not ready at version %s", version)
}
