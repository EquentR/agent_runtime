package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestConfigUnmarshalAndValidateUpdatesSettings(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(`
updates:
  enabled: true
  checkInterval: 1h
  runtimeMode: auto
  serviceName: ice-art
  githubApiBaseUrl: https://api.github.com
  downloadUrlTemplate: https://mirror.example/{name}
  githubTokenEnv: GITHUB_TOKEN
  drainTimeout: 5m
  healthTimeout: 90s
  backup:
    defaultMode: full
    retainCount: 3
    retainDays: 30
`), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if err := cfg.Updates.Validate(); err != nil {
		t.Fatalf("Updates.Validate() error = %v", err)
	}
	if cfg.Updates.CheckInterval != time.Hour || cfg.Updates.Backup.DefaultMode != "full" || cfg.Updates.ServiceName != "ice-art" {
		t.Fatalf("Updates = %#v", cfg.Updates)
	}
}

func TestUpdatesConfigRejectsUnsafeIntervalsAndURLs(t *testing.T) {
	for name, cfg := range map[string]UpdatesConfig{
		"short interval": {CheckInterval: time.Minute},
		"unsafe api":     {CheckInterval: time.Hour, GitHubAPIBaseURL: "file:///tmp/github"},
		"invalid mode":   {CheckInterval: time.Hour, RuntimeMode: "unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}
