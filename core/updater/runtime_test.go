package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDirectRuntimeHealthWaitsForReadyTargetVersionAndSendsToken(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.Header.Get(UpdateHealthTokenHeader) != "health-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if attempts == 1 {
			_ = json.NewEncoder(w).Encode(HealthResponse{Ready: false, Version: "v1.2.2"})
			return
		}
		_ = json.NewEncoder(w).Encode(HealthResponse{Ready: true, Version: "v1.2.3"})
	}))
	defer server.Close()
	runtime := NewDirectRuntime(server.URL, server.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Health(ctx, 123, "v1.2.3", "health-token"); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want retry before ready", attempts)
	}
}
