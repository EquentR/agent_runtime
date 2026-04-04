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
