package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/app/config"
	applogging "github.com/EquentR/agent_runtime/app/logging"
	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/migration"
	"github.com/EquentR/agent_runtime/app/router"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/approvals"
	"github.com/EquentR/agent_runtime/core/attachments"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/interactions"
	corelog "github.com/EquentR/agent_runtime/core/log"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	googleclient "github.com/EquentR/agent_runtime/core/providers/client/google"
	openaichat "github.com/EquentR/agent_runtime/core/providers/client/openai_chat"
	openaicompletions "github.com/EquentR/agent_runtime/core/providers/client/openai_completions"
	openairesponses "github.com/EquentR/agent_runtime/core/providers/client/openai_responses"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coreskills "github.com/EquentR/agent_runtime/core/skills"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	builtin "github.com/EquentR/agent_runtime/core/tools/builtin"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	coreupdater "github.com/EquentR/agent_runtime/core/updater"
	"github.com/EquentR/agent_runtime/core/workspaces"
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/mail"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/EquentR/agent_runtime/pkg/secret"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Serve 负责装配应用依赖并启动 HTTP 服务。
func Serve(c *config.Config, version, commit string, buildArgs ...string) {
	GracefulExit()
	log.Init(&c.Log)
	corelog.SetLogger(applogging.NewCoreAdapter(log.Log().WithOptions(zap.AddCallerSkip(3))))

	log.Infof("Application: Version: %s, Git Commit: %s", version, commit)
	distribution, signingKeyID, signingPublicKey, configPath := coreupdater.DistributionSource, "", "", ""
	if len(buildArgs) > 0 && strings.TrimSpace(buildArgs[0]) != "" {
		distribution = buildArgs[0]
	}
	if len(buildArgs) > 1 {
		signingKeyID = buildArgs[1]
	}
	if len(buildArgs) > 2 {
		signingPublicKey = buildArgs[2]
	}
	if len(buildArgs) > 3 {
		configPath = buildArgs[3]
	}
	runtimeMode, serviceName := c.Updates.RuntimeMode, c.Updates.ServiceName
	if len(buildArgs) > 4 && strings.TrimSpace(buildArgs[4]) != "" {
		runtimeMode = buildArgs[4]
	}
	if len(buildArgs) > 5 && strings.TrimSpace(buildArgs[5]) != "" {
		serviceName = buildArgs[5]
	}
	currentBuild := coreupdater.CurrentBuildInfo(version, commit, distribution)

	db.Init(&c.Sqlite)
	migration.Bootstrap(version)

	engine := rest.Init()

	taskStore := coretasks.NewStore(db.DB())
	approvalStore := approvals.NewStore(db.DB())
	interactionStore := interactions.NewStore(db.DB())
	auditRuntime, err := initAuditRuntime(db.DB())
	if err != nil {
		log.Panicf("Failed to init audit runtime: %v", err)
	}
	startAuditGCLoop(globalCtx, auditRuntime.Store, c.Storage.ResolvedMaintenanceInterval(), c.Storage.ResolvedAuditRetention())
	taskManager := newTaskManager(taskStore, approvalStore, interactionStore, c.Tasks, auditRuntime.TaskRecorder)
	startTaskEventGCLoop(globalCtx, taskStore, c.Storage.ResolvedMaintenanceInterval(), c.Storage.ResolvedTaskEventRetention())
	conversationStore := coreagent.NewConversationStore(db.DB())
	if err := conversationStore.AutoMigrate(); err != nil {
		log.Panicf("Failed to migrate conversation store: %v", err)
	}
	attachmentStorage, err := initAttachmentStorage(c.Attachments)
	if err != nil {
		log.Panicf("Failed to init attachment storage: %v", err)
	}
	attachmentStore := attachments.NewStore(db.DB(), attachmentStorage)
	startAttachmentGCLoop(globalCtx, attachmentStore, c.Attachments.ResolvedGCInterval())
	promptRuntime, err := initPromptRuntime(db.DB())
	if err != nil {
		log.Panicf("Failed to init prompt runtime: %v", err)
	}
	workspaceRuntime, err := initWorkspaceRuntime(*c)
	if err != nil {
		log.Panicf("Failed to init workspace runtime: %v", err)
	}
	startWorkspaceGCLoop(globalCtx, workspaceRuntime.Manager, c.Workspaces.ResolvedGCInterval(), c.Workspaces.ResolvedTaskRetention(), c.Workspaces.ResolvedBackupRetention())
	skillLoader := coreskills.NewLoader(workspaceRuntime.TemplateRoot)
	authRuntime, err := initAuthRuntime(db.DB(), c.Security)
	if err != nil {
		log.Panicf("Failed to init auth runtime: %v", err)
	}
	startSessionGCLoop(globalCtx, authRuntime.AuthLogic, c.Storage.ResolvedMaintenanceInterval(), c.Storage.ResolvedSessionRetention())
	resolver := &coreagent.ModelResolver{Providers: c.LLM}
	modelLogic, err := logics.NewModelLogic(db.DB(), c.LLM, authRuntime.SecretCodec)
	if err != nil {
		log.Panicf("Failed to init model logic: %v", err)
	}
	authRuntime.ModelLogic = modelLogic
	authRuntime.ModelTester = &modelConnectionTester{clientFactory: buildConfiguredLLMClientFactory(c)}
	resolver = modelLogic.Resolver()
	commandJudge := newCommandJudge(resolver, buildConfiguredLLMClientFactory(c))
	toolRegistryFactory := func(workspaceRoot string, skillsRoot string) (*coretools.Registry, error) {
		return newDefaultToolRegistryWithJudge(workspaceRoot, skillsRoot, c.Tools.WebSearch.BuiltinOptions(), c.Tools.ImageGen.BuiltinOptions(), attachmentStore, attachmentStorage, c.Attachments.ResolvedSentRetention(), commandJudge)
	}
	toolRegistry, err := toolRegistryFactory(workspaceRuntime.TemplateRoot, workspaceRuntime.TemplateRoot)
	if err != nil {
		log.Panicf("Failed to register builtin tools: %v", err)
	}
	if err := registerAgentRunExecutor(taskManager, approvalStore, interactionStore, attachmentStore, attachmentStorage, resolver, conversationStore, toolRegistry, promptRuntime.Resolver, workspaceRuntime.TemplateRoot, workspaceRuntime.Manager, toolRegistryFactory, buildConfiguredLLMClientFactory(c), auditRuntime.RunRecorder); err != nil {
		log.Panicf("Failed to register agent.run executor: %v", err)
	}
	taskManager.Start(globalCtx)
	maintenanceGate := coreupdater.NewMaintenanceGate()

	deps := buildRouterDependencies(taskManager, approvalStore, attachmentStore, attachmentStorage, c.Attachments.ResolvedDraftTTL(), conversationStore, auditRuntime.Store, resolver, promptRuntime.Store, promptRuntime.Resolver, skillLoader, authRuntime.AuthLogic, authRuntime, interactionStore)
	deps.WorkspaceManager = workspaceRuntime.Manager
	deps.MaintenanceGate = maintenanceGate
	deps.CurrentBuild = currentBuild
	updateManager, updateErr := initUpdateManager(c, currentBuild, configPath, signingKeyID, signingPublicKey, runtimeMode, serviceName, taskManager, maintenanceGate, authRuntime.AuthLogic, deps.AdminAuditLogic)
	if updateErr != nil {
		log.Warnf("Self-update runtime unavailable: %v", updateErr)
	} else {
		deps.UpdateManager = updateManager
		deps.UpdateHealthStore = updateManager.HealthStore()
		if c.Updates.Enabled {
			startUpdateCheckLoop(updateManager, c.Updates.ResolvedCheckInterval())
		}
	}
	router.Init(engine, c.Server.ApiBasePath, c.Server.StaticPaths, deps)

	addr := fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Panicf("Failed to listen on %s: %v", addr, err)
	}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- serveHTTPUntilCanceled(globalCtx, ln, engine, 30*time.Second)
	}()
	log.Infof("gin listening on %s", addr)

	select {
	case <-globalCtx.Done():
		log.Info("Shutting down server...")
		if err := <-serverDone; err != nil {
			log.Warnf("HTTP server shutdown: %v", err)
		}
		if err := checkpointDatabase(db.DB()); err != nil {
			log.Warnf("Database checkpoint failed: %v", err)
		}
	case err := <-serverDone:
		if err != nil {
			log.Panicf("Failed to run server: %v", err)
		}
	}
}

func checkpointDatabase(database *gorm.DB) error {
	if database == nil {
		return nil
	}
	var result []struct {
		Busy         int
		Log          int
		Checkpointed int
	}
	if err := database.Raw("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&result).Error; err != nil {
		return err
	}
	if len(result) > 0 && result[0].Busy != 0 {
		return fmt.Errorf("wal checkpoint busy: %d log frames, %d checkpointed", result[0].Log, result[0].Checkpointed)
	}
	return nil
}

func serveHTTPUntilCanceled(ctx context.Context, listener net.Listener, handler http.Handler, shutdownTimeout time.Duration) error {
	server := &http.Server{Handler: handler}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	select {
	case err := <-serveDone:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx := context.Background()
		cancel := func() {}
		if shutdownTimeout > 0 {
			shutdownCtx, cancel = context.WithTimeout(shutdownCtx, shutdownTimeout)
		}
		defer cancel()
		shutdownErr := server.Shutdown(shutdownCtx)
		serveErr := <-serveDone
		if serveErr != nil && serveErr != http.ErrServerClosed {
			return serveErr
		}
		return shutdownErr
	}
}

func buildLLMClientFactory(requestTimeout time.Duration) coreagent.ClientFactory {
	return func(provider *coretypes.LLMProvider, llmModel *coretypes.LLMModel) (model.LlmClient, error) {
		if provider == nil {
			return nil, fmt.Errorf("llm provider is not configured")
		}
		switch llmModel.ModelType() {
		case coretypes.LLMTypeOpenAIResponses:
			return openairesponses.NewOpenAiResponsesClient(provider.AuthKey(), provider.BaseURL(), requestTimeout), nil
		case coretypes.LLMTypeOpenAIChat:
			return openaichat.NewOpenAIChatClient(provider.AuthKey(), provider.BaseURL(), requestTimeout), nil
		case coretypes.LLMTypeOpenAICompletions:
			return openaicompletions.NewOpenAiCompletionsClient(provider.BaseURL(), provider.AuthKey(), requestTimeout), nil
		case coretypes.LLMTypeGoogle:
			return googleclient.NewGoogleGenAIClient(provider.BaseURL(), provider.AuthKey())
		default:
			return nil, fmt.Errorf("unsupported llm model type %q", llmModel.ModelType())
		}
	}
}

func buildConfiguredLLMClientFactory(cfg *config.Config) coreagent.ClientFactory {
	if cfg == nil {
		return buildLLMClientFactory(0)
	}
	return buildLLMClientFactory(cfg.ResolvedLLMRequestTimeout())
}

func registerAgentRunExecutor(taskManager *coretasks.Manager, approvalStore *approvals.Store, interactionStore *interactions.Store, attachmentStore *attachments.Store, attachmentStorage attachments.Storage, resolver *coreagent.ModelResolver, conversationStore *coreagent.ConversationStore, toolRegistry *coretools.Registry, promptResolver *coreprompt.Resolver, workspaceRoot string, workspaceManager *workspaces.Manager, toolRegistryFactory coreagent.ToolRegistryFactory, clientFactory coreagent.ClientFactory, auditRecorder coreaudit.Recorder) error {
	if taskManager == nil {
		return fmt.Errorf("task manager is required")
	}
	return taskManager.RegisterExecutor("agent.run", coreagent.NewTaskExecutor(buildAgentRunExecutorDependencies(resolver, conversationStore, attachmentStore, attachmentStorage, toolRegistry, approvalStore, interactionStore, promptResolver, workspaceRoot, workspaceManager, toolRegistryFactory, clientFactory, auditRecorder)))
}

type authRuntime struct {
	AuthLogic         *logics.AuthLogic
	Database          *gorm.DB
	SecretCodec       *secret.Codec
	Settings          *logics.SettingsLogic
	EmailVerification *logics.EmailVerificationLogic
	TurnstileVerifier logics.TurnstileVerifier
	ModelLogic        *logics.ModelLogic
	ModelTester       router.ModelTester
}

func initAuthRuntime(database *gorm.DB, cfg config.SecurityConfig) (*authRuntime, error) {
	if database == nil {
		return nil, fmt.Errorf("auth runtime db is required")
	}
	codec, err := secret.NewCodec(cfg.AppSecret)
	if err != nil {
		return nil, err
	}
	settings, err := logics.NewSettingsLogic(database, settingsDefaultsFromConfig(cfg), codec)
	if err != nil {
		return nil, err
	}
	emailVerification, err := logics.NewEmailVerificationLogic(database, logics.EmailVerificationConfig{
		Sender: &settingsBackedMailSender{settings: settings},
	})
	if err != nil {
		return nil, err
	}
	turnstileVerifier, err := logics.NewCloudflareTurnstileVerifier(settings)
	if err != nil {
		return nil, err
	}
	authLogic, err := logics.NewAuthLogic(database, logics.AuthConfig{SecureCookie: cfg.SecureCookie}, logics.WithAuthSettings(settings), logics.WithAuthEmailVerification(emailVerification))
	if err != nil {
		return nil, err
	}
	return &authRuntime{
		AuthLogic:         authLogic,
		Database:          database,
		SecretCodec:       codec,
		Settings:          settings,
		EmailVerification: emailVerification,
		TurnstileVerifier: turnstileVerifier,
	}, nil
}

func settingsDefaultsFromConfig(cfg config.SecurityConfig) logics.SettingsDefaults {
	return logics.SettingsDefaults{
		PublicRegistrationConfigured: cfg.PublicRegistration.Configured(),
		PublicRegistration: logics.PublicRegistrationSettings{
			Enabled: cfg.PublicRegistration.ResolvedEnabled(),
		},
		SMTP: logics.SMTPSettings{
			Enabled:     cfg.SMTP.Enabled,
			Host:        cfg.SMTP.Host,
			Port:        cfg.SMTP.Port,
			Username:    cfg.SMTP.Username,
			Password:    cfg.SMTP.Password,
			From:        cfg.SMTP.From,
			UseTLS:      cfg.SMTP.UseTLS,
			UseStartTLS: cfg.SMTP.UseStartTLS,
		},
		Turnstile: logics.TurnstileSettings{
			Enabled:             cfg.Turnstile.Enabled,
			SiteKey:             cfg.Turnstile.SiteKey,
			Secret:              cfg.Turnstile.Secret,
			ProtectLogin:        cfg.Turnstile.ProtectLogin,
			ProtectRegistration: cfg.Turnstile.ProtectRegistration,
			ProtectVerification: cfg.Turnstile.ProtectVerification,
		},
	}
}

type settingsBackedMailSender struct {
	settings *logics.SettingsLogic
}

func (s *settingsBackedMailSender) Available(ctx context.Context) error {
	if s == nil || s.settings == nil {
		return logics.ErrMailServiceUnavailable
	}
	settings, err := s.settings.GetSMTPForSend(ctx)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return logics.ErrMailServiceUnavailable
	}
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

func (s *settingsBackedMailSender) Send(ctx context.Context, message mail.Message) error {
	if err := s.Available(ctx); err != nil {
		return err
	}
	settings, err := s.settings.GetSMTPForSend(ctx)
	if err != nil {
		return err
	}
	sender, err := mail.NewSMTPSender(mail.SMTPConfig{
		Enabled:     settings.Enabled,
		Host:        settings.Host,
		Port:        settings.Port,
		Username:    settings.Username,
		Password:    settings.Password,
		From:        settings.From,
		UseTLS:      settings.UseTLS,
		UseStartTLS: settings.UseStartTLS,
	})
	if err != nil {
		return err
	}
	return sender.Send(ctx, message)
}

func buildRouterDependencies(taskManager *coretasks.Manager, approvalStore *approvals.Store, attachmentStore *attachments.Store, attachmentStorage attachments.Storage, attachmentDraftTTL time.Duration, conversationStore *coreagent.ConversationStore, auditStore *coreaudit.Store, resolver *coreagent.ModelResolver, promptStore *coreprompt.Store, promptResolver *coreprompt.Resolver, skillLoader *coreskills.Loader, authLogic *logics.AuthLogic, authRuntime *authRuntime, interactionStores ...*interactions.Store) router.Dependencies {
	var interactionStore *interactions.Store
	if len(interactionStores) > 0 {
		interactionStore = interactionStores[0]
	}
	var userDB *gorm.DB
	var authSettings *logics.SettingsLogic
	var emailVerification *logics.EmailVerificationLogic
	var turnstileVerifier logics.TurnstileVerifier
	var adminAuditLogic *logics.AdminAuditLogic
	var adminSMTPTester router.AdminSMTPTester
	var modelLogic *logics.ModelLogic
	var modelTester router.ModelTester
	modelResolver := resolver
	if authRuntime != nil {
		userDB = authRuntime.Database
		authSettings = authRuntime.Settings
		emailVerification = authRuntime.EmailVerification
		turnstileVerifier = authRuntime.TurnstileVerifier
		modelLogic = authRuntime.ModelLogic
		modelTester = authRuntime.ModelTester
		if modelLogic == nil && authRuntime.Database != nil && authRuntime.SecretCodec != nil {
			if initialized, err := logics.NewModelLogic(authRuntime.Database, resolverProviders(resolver), authRuntime.SecretCodec); err == nil {
				modelLogic = initialized
			}
		}
		if modelLogic != nil {
			modelResolver = modelLogic.Resolver()
		}
		if authRuntime.Database != nil {
			adminAuditLogic = logics.NewAdminAuditLogic(authRuntime.Database)
		}
		if authRuntime.Settings != nil {
			adminSMTPTester = &settingsBackedMailSender{settings: authRuntime.Settings}
		}
	}
	return router.Dependencies{
		TaskManager:        taskManager,
		ApprovalStore:      approvalStore,
		AttachmentStore:    attachmentStore,
		AttachmentStorage:  attachmentStorage,
		AttachmentDraftTTL: attachmentDraftTTL,
		InteractionStore:   interactionStore,
		ConversationStore:  conversationStore,
		AuditStore:         auditStore,
		ModelResolver:      modelResolver,
		ModelLogic:         modelLogic,
		ModelTester:        modelTester,
		PromptStore:        promptStore,
		PromptResolver:     promptResolver,
		SkillLoader:        skillLoader,
		AuthLogic:          authLogic,
		UserDB:             userDB,
		AuthSettings:       authSettings,
		EmailVerification:  emailVerification,
		TurnstileVerifier:  turnstileVerifier,
		AdminAuditLogic:    adminAuditLogic,
		AdminSMTPTester:    adminSMTPTester,
	}
}

type modelConnectionTester struct {
	clientFactory coreagent.ClientFactory
}

func (t *modelConnectionTester) TestModel(ctx context.Context, resolved *coreagent.ResolvedModel) error {
	if t == nil || t.clientFactory == nil {
		return fmt.Errorf("model tester is not configured")
	}
	if resolved == nil || resolved.Provider == nil || resolved.Model == nil {
		return fmt.Errorf("model is not resolved")
	}
	client, err := t.clientFactory(resolved.Provider, resolved.Model)
	if err != nil {
		return err
	}
	_, err = client.Chat(ctx, model.ChatRequest{
		Model:     resolved.Model.ModelID(),
		Messages:  []model.Message{{Role: model.RoleUser, Content: "Reply with OK."}},
		MaxTokens: 1,
	})
	return err
}

func resolverProviders(resolver *coreagent.ModelResolver) []coretypes.LLMProvider {
	if resolver == nil {
		return nil
	}
	return resolver.Providers
}

func initAttachmentStorage(cfg config.AttachmentStorageConfig) (attachments.Storage, error) {
	switch cfg.ResolvedStorageBackend() {
	case attachments.BackendFilesystem:
		return attachments.NewFilesystemStore(cfg.ResolvedFilesystemRoot())
	default:
		return nil, fmt.Errorf("unsupported attachment storage backend %q", cfg.ResolvedStorageBackend())
	}
}

func startAttachmentGCLoop(ctx context.Context, store *attachments.Store, interval time.Duration) {
	if ctx == nil || store == nil || interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if processed, err := store.GCExpired(ctx, time.Now().UTC(), 100); err != nil {
					log.Warnf("Attachment GC failed: %v", err)
				} else if processed > 0 {
					log.Infof("Attachment GC processed %d expired attachments", processed)
				}
			}
		}
	}()
}

func startWorkspaceGCLoop(ctx context.Context, manager *workspaces.Manager, interval time.Duration, taskRetention time.Duration, backupRetention time.Duration) {
	if ctx == nil || manager == nil || interval <= 0 || (taskRetention <= 0 && backupRetention <= 0) {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				report, err := manager.CleanupExpired(ctx, time.Now().UTC(), workspaces.CleanupOptions{
					TaskRetention:   taskRetention,
					BackupRetention: backupRetention,
				})
				if err != nil {
					log.Warnf("Workspace GC failed: %v", err)
					continue
				}
				for _, cleanupErr := range report.Errors {
					log.Warnf("Workspace GC item failed: %v", cleanupErr)
				}
				if report.DeletedTaskWorkspaces > 0 || report.DeletedBackups > 0 {
					log.Infof("Workspace GC deleted %d task workspaces and %d backups", report.DeletedTaskWorkspaces, report.DeletedBackups)
				}
			}
		}
	}()
}

func startAuditGCLoop(ctx context.Context, store *coreaudit.Store, interval time.Duration, retention time.Duration) {
	if ctx == nil || store == nil || interval <= 0 || retention <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processed, err := store.CleanupExpiredRuns(ctx, time.Now().UTC(), retention, 100)
				if err != nil {
					log.Warnf("Audit GC failed: %v", err)
					continue
				}
				if processed > 0 {
					log.Infof("Audit GC processed %d expired runs", processed)
				}
			}
		}
	}()
}

func startTaskEventGCLoop(ctx context.Context, store *coretasks.Store, interval time.Duration, retention time.Duration) {
	if ctx == nil || store == nil || interval <= 0 || retention <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processed, err := store.DeleteExpiredEvents(ctx, time.Now().UTC(), retention, 500)
				if err != nil {
					log.Warnf("Task event GC failed: %v", err)
					continue
				}
				if processed > 0 {
					log.Infof("Task event GC processed %d expired events", processed)
				}
			}
		}
	}()
}

func startSessionGCLoop(ctx context.Context, authLogic *logics.AuthLogic, interval time.Duration, retention time.Duration) {
	if ctx == nil || authLogic == nil || interval <= 0 || retention <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processed, err := authLogic.CleanupExpiredSessions(ctx, time.Now().UTC(), 100)
				if err != nil {
					log.Warnf("Session GC failed: %v", err)
					continue
				}
				if processed > 0 {
					log.Infof("Session GC processed %d expired sessions", processed)
				}
			}
		}
	}()
}

type promptRuntime struct {
	Store    *coreprompt.Store
	Resolver *coreprompt.Resolver
}

func initPromptRuntime(database *gorm.DB) (*promptRuntime, error) {
	if database == nil {
		return nil, fmt.Errorf("prompt runtime db is required")
	}
	store := coreprompt.NewStore(database)
	return &promptRuntime{
		Store:    store,
		Resolver: coreprompt.NewResolver(store),
	}, nil
}

type workspaceRuntime struct {
	Manager      *workspaces.Manager
	TemplateRoot string
	Root         string
}

func initWorkspaceRuntime(cfg config.Config) (*workspaceRuntime, error) {
	templateRoot, err := resolveEffectiveWorkspaceRoot(cfg.ResolvedWorkspaceTemplateRoot())
	if err != nil {
		return nil, fmt.Errorf("resolve workspace template root: %w", err)
	}
	root, err := resolveEffectiveWorkspaceRoot(cfg.Workspaces.ResolvedRoot())
	if err != nil {
		return nil, fmt.Errorf("resolve workspaces root: %w", err)
	}
	manager, err := workspaces.NewManager(workspaces.Config{
		TemplateRoot: templateRoot,
		Root:         root,
	})
	if err != nil {
		return nil, err
	}
	return &workspaceRuntime{
		Manager:      manager,
		TemplateRoot: templateRoot,
		Root:         root,
	}, nil
}

func resolveEffectiveWorkspaceRoot(configuredRoot string) (string, error) {
	workspaceRoot := configuredRoot
	if workspaceRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve workspace root: %w", err)
		}
		workspaceRoot = cwd
	} else {
		if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
			return "", fmt.Errorf("create workspace root %q: %w", workspaceRoot, err)
		}
	}
	workspaceRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	return filepath.Clean(workspaceRoot), nil
}

func buildAgentRunExecutorDependencies(resolver *coreagent.ModelResolver, conversationStore *coreagent.ConversationStore, attachmentStore *attachments.Store, attachmentStorage attachments.Storage, toolRegistry *coretools.Registry, approvalStore *approvals.Store, interactionStore *interactions.Store, promptResolver *coreprompt.Resolver, workspaceRoot string, workspaceManager *workspaces.Manager, toolRegistryFactory coreagent.ToolRegistryFactory, clientFactory coreagent.ClientFactory, auditRecorder coreaudit.Recorder) coreagent.ExecutorDependencies {
	return coreagent.ExecutorDependencies{
		Resolver:            resolver,
		ConversationStore:   conversationStore,
		AttachmentStore:     attachmentStore,
		AttachmentStorage:   attachmentStorage,
		Registry:            toolRegistry,
		ApprovalStore:       approvalStore,
		InteractionStore:    interactionStore,
		PromptResolver:      promptResolver,
		SkillsResolver:      coreskills.NewResolver(coreskills.NewLoader(workspaceRoot)),
		WorkspaceRoot:       workspaceRoot,
		WorkspaceManager:    workspaceManager,
		ToolRegistryFactory: toolRegistryFactory,
		ClientFactory:       clientFactory,
		AuditRecorder:       auditRecorder,
	}
}

type auditRuntime struct {
	Store        *coreaudit.Store
	RunRecorder  coreaudit.Recorder
	TaskRecorder coretasks.AuditRecorder
}

func initAuditRuntime(database *gorm.DB) (*auditRuntime, error) {
	if database == nil {
		return nil, fmt.Errorf("audit runtime db is required")
	}
	store := coreaudit.NewStore(database)
	runRecorder := coreaudit.NewRecorder(store)
	return &auditRuntime{
		Store:        store,
		RunRecorder:  runRecorder,
		TaskRecorder: newTaskAuditRecorder(runRecorder),
	}, nil
}

func newTaskManager(store *coretasks.Store, approvalStore *approvals.Store, interactionStore *interactions.Store, cfg config.TaskManagerConfig, auditRecorder coretasks.AuditRecorder) *coretasks.Manager {
	options := cfg.ManagerOptions(auditRecorder)
	options.ApprovalStore = approvalStore
	options.InteractionStore = interactionStore
	return coretasks.NewManager(store, options)
}

func newDefaultToolRegistry(workspaceRoot string, skillsRoot string, webSearch builtin.WebSearchOptions, imageGen builtin.ImageGenOptions, attachmentStore *attachments.Store, attachmentStorage attachments.Storage, attachmentSentRetention time.Duration) (*coretools.Registry, error) {
	return newDefaultToolRegistryWithJudge(workspaceRoot, skillsRoot, webSearch, imageGen, attachmentStore, attachmentStorage, attachmentSentRetention, nil)
}

func newDefaultToolRegistryWithJudge(workspaceRoot string, skillsRoot string, webSearch builtin.WebSearchOptions, imageGen builtin.ImageGenOptions, attachmentStore *attachments.Store, attachmentStorage attachments.Storage, attachmentSentRetention time.Duration, commandJudge builtin.CommandJudge) (*coretools.Registry, error) {
	registry := coretools.NewRegistry()
	if err := builtin.Register(registry, newDefaultBuiltinOptionsWithJudge(workspaceRoot, skillsRoot, webSearch, imageGen, attachmentStore, attachmentStorage, attachmentSentRetention, commandJudge)); err != nil {
		return nil, err
	}
	return registry, nil
}

func newDefaultBuiltinOptions(workspaceRoot string, skillsRoot string, webSearch builtin.WebSearchOptions, imageGen builtin.ImageGenOptions, attachmentStore *attachments.Store, attachmentStorage attachments.Storage, attachmentSentRetention time.Duration) builtin.Options {
	return newDefaultBuiltinOptionsWithJudge(workspaceRoot, skillsRoot, webSearch, imageGen, attachmentStore, attachmentStorage, attachmentSentRetention, nil)
}

func newDefaultBuiltinOptionsWithJudge(workspaceRoot string, skillsRoot string, webSearch builtin.WebSearchOptions, imageGen builtin.ImageGenOptions, attachmentStore *attachments.Store, attachmentStorage attachments.Storage, attachmentSentRetention time.Duration, commandJudge builtin.CommandJudge) builtin.Options {
	imageGen.SentRetention = attachmentSentRetention
	return builtin.Options{
		WorkspaceRoot:     workspaceRoot,
		SkillsRoot:        skillsRoot,
		WorkspaceMode:     builtin.WorkspaceModeFromRoot(workspaceRoot),
		CommandJudge:      commandJudge,
		WebSearch:         webSearch,
		ImageGen:          imageGen,
		AttachmentStore:   attachmentStore,
		AttachmentStorage: attachmentStorage,
	}
}

type configuredCommandJudge struct {
	resolver      *coreagent.ModelResolver
	clientFactory coreagent.ClientFactory
}

func newCommandJudge(resolver *coreagent.ModelResolver, clientFactory coreagent.ClientFactory) builtin.CommandJudge {
	return &configuredCommandJudge{resolver: resolver, clientFactory: clientFactory}
}

func (j *configuredCommandJudge) Evaluate(ctx context.Context, request builtin.CommandJudgeRequest) (builtin.CommandJudgeResult, error) {
	if j == nil || j.resolver == nil || j.clientFactory == nil {
		return builtin.CommandJudgeResult{Verdict: builtin.CommandVerdictUnavailable, Reason: "command judge is not configured"}, nil
	}
	providerID, modelID := j.resolver.DefaultSelection()
	if strings.TrimSpace(providerID) == "" || strings.TrimSpace(modelID) == "" {
		return builtin.CommandJudgeResult{Verdict: builtin.CommandVerdictUnavailable, Reason: "command judge model is not configured"}, nil
	}
	resolved, err := j.resolver.ResolveContext(ctx, providerID, modelID)
	if err != nil {
		return builtin.CommandJudgeResult{Verdict: builtin.CommandVerdictUnavailable, Reason: err.Error()}, nil
	}
	client, err := j.clientFactory(resolved.Provider, resolved.Model)
	if err != nil {
		return builtin.CommandJudgeResult{Verdict: builtin.CommandVerdictUnavailable, Reason: err.Error()}, nil
	}
	payload, err := json.Marshal(map[string]any{
		"tool_name":      request.ToolName,
		"workspace_mode": request.WorkspaceMode,
		"command":        request.Command,
		"arguments":      request.Arguments,
	})
	if err != nil {
		return builtin.CommandJudgeResult{Verdict: builtin.CommandVerdictUnavailable, Reason: err.Error()}, nil
	}
	resp, err := client.Chat(ctx, model.ChatRequest{
		Model: resolved.Model.ModelID(),
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "Classify the shell command as safe, neutral, risky, or unavailable. Reply with one word."},
			{Role: model.RoleUser, Content: string(payload)},
		},
	})
	if err != nil {
		return builtin.CommandJudgeResult{Verdict: builtin.CommandVerdictUnavailable, Reason: err.Error()}, nil
	}
	answer := strings.TrimSpace(resp.Message.Content)
	if answer == "" {
		answer = strings.TrimSpace(resp.Content)
	}
	verdict := builtin.CommandVerdictUnavailable
	switch strings.ToLower(answer) {
	case string(builtin.CommandVerdictSafe):
		verdict = builtin.CommandVerdictSafe
	case string(builtin.CommandVerdictNeutral):
		verdict = builtin.CommandVerdictNeutral
	case string(builtin.CommandVerdictRisky):
		verdict = builtin.CommandVerdictRisky
	case string(builtin.CommandVerdictUnavailable):
		verdict = builtin.CommandVerdictUnavailable
	}
	return builtin.CommandJudgeResult{
		Verdict: verdict,
		Reason:  answer,
	}, nil
}

type taskAuditRecorder struct {
	recorder coreaudit.Recorder
}

func newTaskAuditRecorder(recorder coreaudit.Recorder) coretasks.AuditRecorder {
	if recorder == nil {
		return nil
	}
	return &taskAuditRecorder{recorder: recorder}
}

func (r *taskAuditRecorder) StartRun(ctx context.Context, input coretasks.AuditStartRunInput) (*coretasks.AuditRun, error) {
	run, err := r.recorder.StartRun(ctx, coreaudit.StartRunInput{
		TaskID:        input.TaskID,
		TaskType:      input.TaskType,
		RunnerID:      input.RunnerID,
		CreatedBy:     input.CreatedBy,
		Status:        coreaudit.Status(input.Status),
		StartedAt:     input.StartedAt,
		SchemaVersion: coreaudit.SchemaVersionV1,
	})
	if err != nil {
		return nil, err
	}
	return &coretasks.AuditRun{ID: run.ID, TaskID: run.TaskID}, nil
}

func (r *taskAuditRecorder) AppendEvent(ctx context.Context, runID string, input coretasks.AuditAppendEventInput) (*coretasks.AuditEvent, error) {
	event, err := r.recorder.AppendEvent(ctx, runID, coreaudit.AppendEventInput{
		EventType: input.EventType,
		Payload:   input.Payload,
	})
	if err != nil {
		return nil, err
	}
	return &coretasks.AuditEvent{RunID: event.RunID, EventType: event.EventType}, nil
}

func (r *taskAuditRecorder) FinishRun(ctx context.Context, runID string, input coretasks.AuditFinishRunInput) error {
	return r.recorder.FinishRun(ctx, runID, coreaudit.FinishRunInput{
		Status:     coreaudit.Status(input.Status),
		FinishedAt: input.FinishedAt,
	})
}
