package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/core/attachments"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	builtin "github.com/EquentR/agent_runtime/core/tools/builtin"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/rest"
)

const defaultLLMRequestTimeout = 10 * time.Minute
const defaultAttachmentDraftTTL = 24 * time.Hour
const defaultAttachmentSentRetention = 30 * 24 * time.Hour
const defaultAttachmentGCInterval = 1 * time.Hour
const defaultAttachmentFilesystemRoot = "data/attachments"
const defaultWorkspaceTemplateRoot = "workspace"
const defaultWorkspacesRoot = "data/workspaces"
const defaultWorkspaceTaskRetention = 30 * 24 * time.Hour
const defaultWorkspaceBackupRetention = 30 * 24 * time.Hour
const defaultWorkspaceGCInterval = time.Hour

type Config struct {
	WorkspaceDir      string                      `yaml:"workspaceDir"`
	Workspaces        WorkspacesConfig            `yaml:"workspaces"`
	Server            rest.Config                 `yaml:"server"`
	Sqlite            db.Database                 `yaml:"sqlite"`
	Log               log.Config                  `yaml:"log"`
	Security          SecurityConfig              `yaml:"security"`
	Tasks             TaskManagerConfig           `yaml:"tasks"`
	Tools             ToolsConfig                 `yaml:"tools"`
	Attachments       AttachmentStorageConfig     `yaml:"attachments"`
	Updates           UpdatesConfig               `yaml:"updates"`
	LLMRequestTimeout time.Duration               `yaml:"llmRequestTimeout"`
	LLM               []coretypes.LLMProvider     `yaml:"llmProviders"`
	Embedding         coretypes.EmbeddingProvider `yaml:"embeddingProvider"`
	Rerank            coretypes.RerankingProvider `yaml:"rerankProvider"`
}

type UpdatesConfig struct {
	Enabled             bool               `yaml:"enabled"`
	CheckInterval       time.Duration      `yaml:"checkInterval"`
	RuntimeMode         string             `yaml:"runtimeMode"`
	ServiceName         string             `yaml:"serviceName"`
	GitHubAPIBaseURL    string             `yaml:"githubApiBaseUrl"`
	DownloadURLTemplate string             `yaml:"downloadUrlTemplate"`
	GitHubTokenEnv      string             `yaml:"githubTokenEnv"`
	DrainTimeout        time.Duration      `yaml:"drainTimeout"`
	HealthTimeout       time.Duration      `yaml:"healthTimeout"`
	Backup              UpdateBackupConfig `yaml:"backup"`
}

type UpdateBackupConfig struct {
	DefaultMode string `yaml:"defaultMode"`
	RetainCount int    `yaml:"retainCount"`
	RetainDays  int    `yaml:"retainDays"`
}

func (c UpdatesConfig) Validate() error {
	if c.CheckInterval == 0 {
		c.CheckInterval = time.Hour
	}
	if c.CheckInterval < 15*time.Minute {
		return fmt.Errorf("updates.checkInterval must be at least 15m")
	}
	if c.DrainTimeout < 0 || c.HealthTimeout < 0 {
		return fmt.Errorf("updates timeouts cannot be negative")
	}
	if c.DrainTimeout > time.Hour || c.HealthTimeout > time.Hour {
		return fmt.Errorf("updates timeouts cannot exceed 1h")
	}
	if c.RuntimeMode != "" && c.RuntimeMode != "auto" && c.RuntimeMode != "direct" && c.RuntimeMode != "systemd" && c.RuntimeMode != "windows-service" {
		return fmt.Errorf("updates.runtimeMode %q is invalid", c.RuntimeMode)
	}
	if c.GitHubAPIBaseURL != "" {
		parsed, err := url.Parse(c.GitHubAPIBaseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("updates.githubApiBaseUrl must be an HTTP(S) URL")
		}
	}
	if c.DownloadURLTemplate != "" && !strings.Contains(c.DownloadURLTemplate, "{url}") && !strings.Contains(c.DownloadURLTemplate, "{name}") {
		return fmt.Errorf("updates.downloadUrlTemplate must contain {url} or {name}")
	}
	if c.Backup.DefaultMode != "" && c.Backup.DefaultMode != "compact" && c.Backup.DefaultMode != "full" {
		return fmt.Errorf("updates.backup.defaultMode %q is invalid", c.Backup.DefaultMode)
	}
	if c.Backup.RetainCount < 0 || c.Backup.RetainDays < 0 {
		return fmt.Errorf("updates backup retention cannot be negative")
	}
	if c.Backup.RetainCount > 100 || c.Backup.RetainDays > 3650 {
		return fmt.Errorf("updates backup retention exceeds supported limits")
	}
	return nil
}

func (c UpdatesConfig) ResolvedCheckInterval() time.Duration {
	if c.CheckInterval >= 15*time.Minute {
		return c.CheckInterval
	}
	return time.Hour
}

func (c UpdatesConfig) ResolvedDrainTimeout() time.Duration {
	if c.DrainTimeout > 0 && c.DrainTimeout <= time.Hour {
		return c.DrainTimeout
	}
	return 5 * time.Minute
}

func (c UpdatesConfig) ResolvedHealthTimeout() time.Duration {
	if c.HealthTimeout > 0 && c.HealthTimeout <= time.Hour {
		return c.HealthTimeout
	}
	return 90 * time.Second
}

func (c UpdatesConfig) ResolvedGitHubAPIBaseURL() string {
	if value := strings.TrimSpace(c.GitHubAPIBaseURL); value != "" {
		return value
	}
	return "https://api.github.com"
}

func (c UpdatesConfig) ResolvedGitHubTokenEnv() string {
	if value := strings.TrimSpace(c.GitHubTokenEnv); value != "" {
		return value
	}
	return "GITHUB_TOKEN"
}

func (c UpdateBackupConfig) ResolvedMode() string {
	if c.DefaultMode == "full" {
		return "full"
	}
	return "compact"
}

func (c Config) ResolvedLLMRequestTimeout() time.Duration {
	if c.LLMRequestTimeout > 0 {
		return c.LLMRequestTimeout
	}
	return defaultLLMRequestTimeout
}

func (c Config) ResolvedWorkspaceTemplateRoot() string {
	if root := strings.TrimSpace(c.WorkspaceDir); root != "" {
		return root
	}
	return defaultWorkspaceTemplateRoot
}

type SecurityConfig struct {
	AppSecret          string                   `yaml:"appSecret"`
	SecureCookie       bool                     `yaml:"secureCookie"`
	PublicRegistration PublicRegistrationConfig `yaml:"publicRegistration"`
	SMTP               SMTPConfig               `yaml:"smtp"`
	Turnstile          TurnstileConfig          `yaml:"turnstile"`
}

type PublicRegistrationConfig struct {
	Enabled *bool `yaml:"enabled"`
}

func (c PublicRegistrationConfig) Configured() bool {
	return c.Enabled != nil
}

func (c PublicRegistrationConfig) ResolvedEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

type SMTPConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
	From        string `yaml:"from"`
	UseTLS      bool   `yaml:"useTLS"`
	UseStartTLS bool   `yaml:"useStartTLS"`
}

type TurnstileConfig struct {
	Enabled             bool   `yaml:"enabled"`
	SiteKey             string `yaml:"siteKey"`
	Secret              string `yaml:"secret"`
	ProtectLogin        bool   `yaml:"protectLogin"`
	ProtectRegistration bool   `yaml:"protectRegistration"`
	ProtectVerification bool   `yaml:"protectVerification"`
}

type TaskManagerConfig struct {
	WorkerCount  int           `yaml:"workerCount"`
	RunnerID     string        `yaml:"runnerId"`
	PollInterval time.Duration `yaml:"pollInterval"`
}

func (c TaskManagerConfig) ManagerOptions(auditRecorder coretasks.AuditRecorder) coretasks.ManagerOptions {
	return coretasks.ManagerOptions{
		RunnerID:      c.RunnerID,
		WorkerCount:   c.WorkerCount,
		PollInterval:  c.PollInterval,
		AuditRecorder: auditRecorder,
	}
}

type WorkspacesConfig struct {
	Root            string         `yaml:"root"`
	TaskRetention   *time.Duration `yaml:"taskRetention"`
	BackupRetention *time.Duration `yaml:"backupRetention"`
	GCInterval      *time.Duration `yaml:"gcInterval"`
}

func (c WorkspacesConfig) ResolvedRoot() string {
	if root := strings.TrimSpace(c.Root); root != "" {
		return root
	}
	return defaultWorkspacesRoot
}

func (c WorkspacesConfig) ResolvedTaskRetention() time.Duration {
	if c.TaskRetention != nil {
		return *c.TaskRetention
	}
	return defaultWorkspaceTaskRetention
}

func (c WorkspacesConfig) ResolvedBackupRetention() time.Duration {
	if c.BackupRetention != nil {
		return *c.BackupRetention
	}
	return defaultWorkspaceBackupRetention
}

func (c WorkspacesConfig) ResolvedGCInterval() time.Duration {
	if c.GCInterval != nil {
		return *c.GCInterval
	}
	return defaultWorkspaceGCInterval
}

type ToolsConfig struct {
	WebSearch WebSearchConfig `yaml:"webSearch"`
	ImageGen  ImageGenConfig  `yaml:"imageGen"`
}

type AttachmentStorageConfig struct {
	StorageBackend string                     `yaml:"storageBackend"`
	Filesystem     AttachmentFilesystemConfig `yaml:"filesystem"`
	DraftTTL       time.Duration              `yaml:"draftTTL"`
	SentRetention  time.Duration              `yaml:"sentRetention"`
	GCInterval     time.Duration              `yaml:"gcInterval"`
}

type AttachmentFilesystemConfig struct {
	Root string `yaml:"root"`
}

func (c AttachmentStorageConfig) ResolvedStorageBackend() string {
	if backend := strings.TrimSpace(c.StorageBackend); backend != "" {
		return backend
	}
	return attachments.BackendFilesystem
}

func (c AttachmentStorageConfig) ResolvedFilesystemRoot() string {
	if root := strings.TrimSpace(c.Filesystem.Root); root != "" {
		return root
	}
	return defaultAttachmentFilesystemRoot
}

func (c AttachmentStorageConfig) ResolvedDraftTTL() time.Duration {
	if c.DraftTTL > 0 {
		return c.DraftTTL
	}
	return defaultAttachmentDraftTTL
}

func (c AttachmentStorageConfig) ResolvedSentRetention() time.Duration {
	if c.SentRetention > 0 {
		return c.SentRetention
	}
	return defaultAttachmentSentRetention
}

func (c AttachmentStorageConfig) ResolvedGCInterval() time.Duration {
	if c.GCInterval > 0 {
		return c.GCInterval
	}
	return defaultAttachmentGCInterval
}

type WebSearchConfig struct {
	DefaultProvider string             `yaml:"defaultProvider"`
	Tavily          *WebSearchProvider `yaml:"tavily"`
	SerpAPI         *WebSearchProvider `yaml:"serpApi"`
	Bing            *WebSearchProvider `yaml:"bing"`
}

func (c WebSearchConfig) BuiltinOptions() builtin.WebSearchOptions {
	return builtin.WebSearchOptions{
		DefaultProvider: c.DefaultProvider,
		Tavily:          toTavilyConfig(c.Tavily),
		SerpAPI:         toSerpAPIConfig(c.SerpAPI),
		Bing:            toBingConfig(c.Bing),
	}
}

type WebSearchProvider struct {
	APIKey  string `yaml:"apiKey"`
	BaseURL string `yaml:"baseUrl"`
}

func toTavilyConfig(provider *WebSearchProvider) *builtin.TavilyConfig {
	if provider == nil {
		return nil
	}
	return &builtin.TavilyConfig{APIKey: provider.APIKey, BaseURL: provider.BaseURL}
}

func toSerpAPIConfig(provider *WebSearchProvider) *builtin.SerpAPIConfig {
	if provider == nil {
		return nil
	}
	return &builtin.SerpAPIConfig{APIKey: provider.APIKey, BaseURL: provider.BaseURL}
}

func toBingConfig(provider *WebSearchProvider) *builtin.BingConfig {
	if provider == nil {
		return nil
	}
	return &builtin.BingConfig{APIKey: provider.APIKey, BaseURL: provider.BaseURL}
}

type ImageGenConfig struct {
	DefaultProvider string            `yaml:"defaultProvider"`
	Openai          *ImageGenProvider `yaml:"openai"`
}

func (c ImageGenConfig) BuiltinOptions() builtin.ImageGenOptions {
	return builtin.ImageGenOptions{
		DefaultProvider: c.DefaultProvider,
		Openai:          toImageGenProviderConfig(c.Openai),
	}
}

type ImageGenProvider struct {
	BaseURL             string `yaml:"baseUrl"`
	APIKey              string `yaml:"apiKey"`
	Model               string `yaml:"model"`
	EditModel           string `yaml:"editModel"`
	Stream              *bool  `yaml:"stream"`
	PartialImages       *int   `yaml:"partialImages"`
	DefaultSize         string `yaml:"defaultSize"`
	DefaultQuality      string `yaml:"defaultQuality"`
	DefaultOutputFormat string `yaml:"defaultOutputFormat"`
}

func toImageGenProviderConfig(provider *ImageGenProvider) *builtin.ImageGenProviderConfig {
	if provider == nil {
		return nil
	}
	return &builtin.ImageGenProviderConfig{
		BaseURL:             provider.BaseURL,
		APIKey:              provider.APIKey,
		Model:               provider.Model,
		EditModel:           provider.EditModel,
		Stream:              provider.Stream,
		PartialImages:       provider.PartialImages,
		DefaultSize:         provider.DefaultSize,
		DefaultQuality:      provider.DefaultQuality,
		DefaultOutputFormat: provider.DefaultOutputFormat,
	}
}
