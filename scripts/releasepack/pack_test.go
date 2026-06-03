package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EquentR/agent_runtime/app/config"
	"gopkg.in/yaml.v3"
)

func TestPackReleaseCopiesReleaseConfigAndWorkspaceTemplate(t *testing.T) {
	sourceDir := t.TempDir()
	destDir := t.TempDir()

	writeFile(t, filepath.Join(sourceDir, "conf", "app.release.yaml"), []byte(`
workspaceDir: workspace
security:
  appSecret: release-default-secret
tools: {}
llmProviders: []
`))
	writeFile(t, filepath.Join(sourceDir, "workspace", "AGENTS.md"), []byte("# Workspace rules\n"))
	writeFile(t, filepath.Join(sourceDir, "workspace", "skills", "test-skill", "SKILL.md"), []byte("# Test skill\n"))
	writeFile(t, filepath.Join(sourceDir, "workspace", "ignored.txt"), []byte("ignore me\n"))

	if err := PackRelease(sourceDir, destDir); err != nil {
		t.Fatalf("PackRelease() error = %v", err)
	}

	configPath := filepath.Join(destDir, "conf", "app.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", configPath, err)
	}
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if cfg.Security.AppSecret != "release-default-secret" {
		t.Fatalf("cfg.Security.AppSecret = %q, want release-default-secret", cfg.Security.AppSecret)
	}
	if len(cfg.LLM) != 0 {
		t.Fatalf("len(cfg.LLM) = %d, want 0", len(cfg.LLM))
	}
	if cfg.Tools.ImageGen.Openai != nil {
		t.Fatal("cfg.Tools.ImageGen.Openai = non-nil, want release defaults without model config")
	}
	if cfg.Tools.WebSearch.Tavily != nil || cfg.Tools.WebSearch.SerpAPI != nil || cfg.Tools.WebSearch.Bing != nil {
		t.Fatal("cfg.Tools.WebSearch has configured providers, want release defaults without provider config")
	}

	assertFileContent(t, filepath.Join(destDir, "workspace", "AGENTS.md"), "# Workspace rules\n")
	assertFileContent(t, filepath.Join(destDir, "workspace", "skills", "test-skill", "SKILL.md"), "# Test skill\n")
	if _, err := os.Stat(filepath.Join(destDir, "workspace", "ignored.txt")); !os.IsNotExist(err) {
		t.Fatalf("workspace ignored file was copied: %v", err)
	}
}

func TestReleaseConfigFileIsParseableWithoutActiveModelProviders(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "conf", "app.release.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if cfg.Security.AppSecret == "" {
		t.Fatal("cfg.Security.AppSecret = empty, want a packaged default value")
	}
	if len(cfg.LLM) != 0 {
		t.Fatalf("len(cfg.LLM) = %d, want 0", len(cfg.LLM))
	}
	if cfg.Tools.ImageGen.DefaultProvider != "" {
		t.Fatalf("cfg.Tools.ImageGen.DefaultProvider = %q, want empty", cfg.Tools.ImageGen.DefaultProvider)
	}
	if cfg.Tools.ImageGen.Openai != nil {
		t.Fatal("cfg.Tools.ImageGen.Openai = non-nil, want commented-out release defaults")
	}
	if cfg.Tools.WebSearch.DefaultProvider != "" {
		t.Fatalf("cfg.Tools.WebSearch.DefaultProvider = %q, want empty", cfg.Tools.WebSearch.DefaultProvider)
	}
	if cfg.Tools.WebSearch.Tavily != nil || cfg.Tools.WebSearch.SerpAPI != nil || cfg.Tools.WebSearch.Bing != nil {
		t.Fatal("cfg.Tools.WebSearch has configured providers, want commented-out release defaults")
	}
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}
