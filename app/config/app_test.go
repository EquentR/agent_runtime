package config

import (
	"reflect"
	"testing"
	"time"

	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	builtin "github.com/EquentR/agent_runtime/core/tools/builtin"
	"gopkg.in/yaml.v3"
)

func TestConfigUnmarshalSupportsTasksAndWebSearchSettings(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(`
tasks:
  workerCount: 4
  runnerId: configured-runner
tools:
  webSearch:
    defaultProvider: tavily
    tavily:
      apiKey: ${TAVILY_API_KEY}
      baseUrl: https://api.tavily.com
    serpApi:
      apiKey: ${SERPAPI_API_KEY}
      baseUrl: https://serpapi.example.com
`), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	cfgValue := reflect.ValueOf(cfg)
	tasks := mustField(t, cfgValue, "Tasks")
	if got := int(mustField(t, tasks, "WorkerCount").Int()); got != 4 {
		t.Fatalf("tasks.workerCount = %d, want 4", got)
	}
	if got := mustField(t, tasks, "RunnerID").String(); got != "configured-runner" {
		t.Fatalf("tasks.runnerId = %q, want %q", got, "configured-runner")
	}

	tools := mustField(t, cfgValue, "Tools")
	webSearch := mustField(t, tools, "WebSearch")
	if got := mustField(t, webSearch, "DefaultProvider").String(); got != "tavily" {
		t.Fatalf("tools.webSearch.defaultProvider = %q, want %q", got, "tavily")
	}
	tavily := mustPointerField(t, webSearch, "Tavily")
	if got := mustField(t, tavily, "APIKey").String(); got != "${TAVILY_API_KEY}" {
		t.Fatalf("tools.webSearch.tavily.apiKey = %q, want %q", got, "${TAVILY_API_KEY}")
	}
	if got := mustField(t, tavily, "BaseURL").String(); got != "https://api.tavily.com" {
		t.Fatalf("tools.webSearch.tavily.baseUrl = %q, want %q", got, "https://api.tavily.com")
	}
	serpAPI := mustPointerField(t, webSearch, "SerpAPI")
	if got := mustField(t, serpAPI, "APIKey").String(); got != "${SERPAPI_API_KEY}" {
		t.Fatalf("tools.webSearch.serpApi.apiKey = %q, want %q", got, "${SERPAPI_API_KEY}")
	}
}

func TestConfigMappingsBuildTaskManagerAndWebSearchOptions(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(`
tasks:
  workerCount: 3
  runnerId: example-agent-configured
tools:
  webSearch:
    defaultProvider: bing
    bing:
      apiKey: ${BING_SEARCH_API_KEY}
      baseUrl: https://api.bing.microsoft.com
`), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	tasks := mustField(t, reflect.ValueOf(cfg), "Tasks")
	taskOptionsProvider, ok := tasks.Interface().(interface {
		ManagerOptions(auditRecorder coretasks.AuditRecorder) coretasks.ManagerOptions
	})
	if !ok {
		t.Fatal("Tasks config should expose ManagerOptions mapping")
	}
	managerOptions := taskOptionsProvider.ManagerOptions(nil)
	if managerOptions.WorkerCount != 3 {
		t.Fatalf("managerOptions.WorkerCount = %d, want 3", managerOptions.WorkerCount)
	}
	if managerOptions.RunnerID != "example-agent-configured" {
		t.Fatalf("managerOptions.RunnerID = %q, want %q", managerOptions.RunnerID, "example-agent-configured")
	}

	webSearch := mustField(t, mustField(t, reflect.ValueOf(cfg), "Tools"), "WebSearch")
	webSearchOptionsProvider, ok := webSearch.Interface().(interface {
		BuiltinOptions() builtin.WebSearchOptions
	})
	if !ok {
		t.Fatal("WebSearch config should expose BuiltinOptions mapping")
	}
	options := webSearchOptionsProvider.BuiltinOptions()
	if options.DefaultProvider != "bing" {
		t.Fatalf("options.DefaultProvider = %q, want %q", options.DefaultProvider, "bing")
	}
	if options.Bing == nil {
		t.Fatal("options.Bing = nil, want configured bing provider")
	}
	if options.Bing.APIKey != "${BING_SEARCH_API_KEY}" {
		t.Fatalf("options.Bing.APIKey = %q, want %q", options.Bing.APIKey, "${BING_SEARCH_API_KEY}")
	}
	if options.Bing.BaseURL != "https://api.bing.microsoft.com" {
		t.Fatalf("options.Bing.BaseURL = %q, want %q", options.Bing.BaseURL, "https://api.bing.microsoft.com")
	}
}

func TestImageGenConfigBuiltinOptionsIncludesModelAndStreaming(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(`
tools:
  imageGen:
    defaultProvider: openai
    openai:
      apiKey: ${OPENAI_API_KEY}
      baseUrl: https://api.openai.com/v1
      model: gpt-image-1
      stream: true
      partialImages: 2
      defaultSize: 1024x1024
      defaultQuality: auto
      defaultOutputFormat: png
`), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	options := cfg.Tools.ImageGen.BuiltinOptions()
	if options.DefaultProvider != "openai" {
		t.Fatalf("options.DefaultProvider = %q, want openai", options.DefaultProvider)
	}
	if options.Openai == nil {
		t.Fatal("options.Openai = nil, want configured provider")
	}
	if options.Openai.Model != "gpt-image-1" {
		t.Fatalf("options.Openai.Model = %q, want gpt-image-1", options.Openai.Model)
	}
	if options.Openai.Stream == nil || !*options.Openai.Stream {
		t.Fatalf("options.Openai.Stream = %#v, want true pointer", options.Openai.Stream)
	}
	if options.Openai.PartialImages == nil || *options.Openai.PartialImages != 2 {
		t.Fatalf("options.Openai.PartialImages = %#v, want 2 pointer", options.Openai.PartialImages)
	}
	if options.Openai.DefaultSize != "1024x1024" {
		t.Fatalf("options.Openai.DefaultSize = %q, want 1024x1024", options.Openai.DefaultSize)
	}
	if options.Openai.DefaultQuality != "auto" {
		t.Fatalf("options.Openai.DefaultQuality = %q, want auto", options.Openai.DefaultQuality)
	}
	if options.Openai.DefaultOutputFormat != "png" {
		t.Fatalf("options.Openai.DefaultOutputFormat = %q, want png", options.Openai.DefaultOutputFormat)
	}
}

func TestImageGenConfigBuiltinOptionsPreservesPartialImagesUnsetAndExplicitZero(t *testing.T) {
	tests := []struct {
		name          string
		partialImages string
		wantNil       bool
		wantValue     int
	}{
		{name: "unset", wantNil: true},
		{name: "explicit zero", partialImages: "      partialImages: 0\n", wantValue: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			if err := yaml.Unmarshal([]byte(`
tools:
  imageGen:
    defaultProvider: openai
    openai:
      apiKey: ${OPENAI_API_KEY}
      stream: true
`+tt.partialImages), &cfg); err != nil {
				t.Fatalf("yaml.Unmarshal() error = %v", err)
			}

			options := cfg.Tools.ImageGen.BuiltinOptions()
			if options.Openai == nil {
				t.Fatal("options.Openai = nil, want configured provider")
			}
			partialImages := reflect.ValueOf(options.Openai.PartialImages)
			if partialImages.Kind() != reflect.Ptr {
				t.Fatalf("options.Openai.PartialImages kind = %s, want ptr to preserve unset", partialImages.Kind())
			}
			if tt.wantNil {
				if !partialImages.IsNil() {
					t.Fatalf("options.Openai.PartialImages = %v, want nil", options.Openai.PartialImages)
				}
				return
			}
			if partialImages.IsNil() {
				t.Fatal("options.Openai.PartialImages = nil, want explicit value")
			}
			if got := int(partialImages.Elem().Int()); got != tt.wantValue {
				t.Fatalf("options.Openai.PartialImages = %d, want %d", got, tt.wantValue)
			}
		})
	}
}

func TestConfigUnmarshalSupportsLLMRequestTimeout(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(`
llmRequestTimeout: 3m
`), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if got := cfg.LLMRequestTimeout; got != 3*time.Minute {
		t.Fatalf("cfg.LLMRequestTimeout = %v, want %v", got, 3*time.Minute)
	}
}

func TestConfigResolvedLLMRequestTimeoutDefaultsToTenMinutes(t *testing.T) {
	var cfg Config

	if got := cfg.ResolvedLLMRequestTimeout(); got != 10*time.Minute {
		t.Fatalf("ResolvedLLMRequestTimeout() = %v, want %v", got, 10*time.Minute)
	}
}

func TestConfigResolvedLLMRequestTimeoutPreservesConfiguredValue(t *testing.T) {
	cfg := Config{LLMRequestTimeout: 3 * time.Minute}

	if got := cfg.ResolvedLLMRequestTimeout(); got != 3*time.Minute {
		t.Fatalf("ResolvedLLMRequestTimeout() = %v, want %v", got, 3*time.Minute)
	}
}

func TestConfigParsesSMTPAndTurnstileDefaults(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(`
security:
  appSecret: ${APP_SECRET}
  publicRegistration:
    enabled: false
  smtp:
    enabled: true
    host: smtp.example.com
    port: 587
    username: smtp-user
    password: ${SMTP_PASSWORD}
    from: Agent Runtime <noreply@example.com>
    useTLS: false
    useStartTLS: true
  turnstile:
    enabled: true
    siteKey: site-key
    secret: ${TURNSTILE_SECRET}
    protectLogin: true
    protectRegistration: true
    protectVerification: false
`), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if cfg.Security.AppSecret != "${APP_SECRET}" {
		t.Fatalf("security.appSecret = %q, want placeholder", cfg.Security.AppSecret)
	}
	if cfg.Security.PublicRegistration.Enabled {
		t.Fatal("security.publicRegistration.enabled = true, want false")
	}
	if cfg.Security.SMTP.Host != "smtp.example.com" {
		t.Fatalf("security.smtp.host = %q, want smtp.example.com", cfg.Security.SMTP.Host)
	}
	if cfg.Security.SMTP.Port != 587 {
		t.Fatalf("security.smtp.port = %d, want 587", cfg.Security.SMTP.Port)
	}
	if cfg.Security.SMTP.Password != "${SMTP_PASSWORD}" {
		t.Fatalf("security.smtp.password = %q, want placeholder", cfg.Security.SMTP.Password)
	}
	if !cfg.Security.SMTP.UseStartTLS || cfg.Security.SMTP.UseTLS {
		t.Fatalf("security.smtp TLS flags = useTLS:%v useStartTLS:%v, want false/true", cfg.Security.SMTP.UseTLS, cfg.Security.SMTP.UseStartTLS)
	}
	if cfg.Security.Turnstile.SiteKey != "site-key" {
		t.Fatalf("security.turnstile.siteKey = %q, want site-key", cfg.Security.Turnstile.SiteKey)
	}
	if cfg.Security.Turnstile.Secret != "${TURNSTILE_SECRET}" {
		t.Fatalf("security.turnstile.secret = %q, want placeholder", cfg.Security.Turnstile.Secret)
	}
	if !cfg.Security.Turnstile.ProtectLogin || !cfg.Security.Turnstile.ProtectRegistration || cfg.Security.Turnstile.ProtectVerification {
		t.Fatalf("security.turnstile protect flags = login:%v registration:%v verification:%v, want true/true/false",
			cfg.Security.Turnstile.ProtectLogin,
			cfg.Security.Turnstile.ProtectRegistration,
			cfg.Security.Turnstile.ProtectVerification,
		)
	}
}

func mustField(t *testing.T, value reflect.Value, name string) reflect.Value {
	t.Helper()
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			t.Fatalf("%s pointer = nil", name)
		}
		value = value.Elem()
	}
	field := value.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("field %q is missing", name)
	}
	return field
}

func mustPointerField(t *testing.T, value reflect.Value, name string) reflect.Value {
	t.Helper()
	field := mustField(t, value, name)
	if field.Kind() != reflect.Pointer {
		t.Fatalf("field %q kind = %s, want pointer", name, field.Kind())
	}
	if field.IsNil() {
		t.Fatalf("field %q = nil, want value", name)
	}
	return field.Elem()
}
