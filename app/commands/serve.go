package commands

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/EquentR/agent_runtime/app/config"
	applogging "github.com/EquentR/agent_runtime/app/logging"
	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/migration"
	"github.com/EquentR/agent_runtime/app/router"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/approvals"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	corelog "github.com/EquentR/agent_runtime/core/log"
	"github.com/EquentR/agent_runtime/core/interactions"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	googleclient "github.com/EquentR/agent_runtime/core/providers/client/google"
	openaicompletions "github.com/EquentR/agent_runtime/core/providers/client/openai_completions"
	openairesponses "github.com/EquentR/agent_runtime/core/providers/client/openai_responses"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coreskills "github.com/EquentR/agent_runtime/core/skills"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	builtin "github.com/EquentR/agent_runtime/core/tools/builtin"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"gorm.io/gorm"
)

// Serve 负责装配应用依赖并启动 HTTP 服务。
func Serve(c *config.Config, version, commit string) {
	GracefulExit()
	log.Init(&c.Log)
	corelog.SetLogger(applogging.NewCoreAdapter(log.Log()))

	log.Infof("Application: Version: %s, Git Commit: %s", version, commit)

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
	taskManager := newTaskManager(taskStore, approvalStore, interactionStore, c.Tasks, auditRuntime.TaskRecorder)
	conversationStore := coreagent.NewConversationStore(db.DB())
	if err := conversationStore.AutoMigrate(); err != nil {
		log.Panicf("Failed to migrate conversation store: %v", err)
	}
	promptRuntime, err := initPromptRuntime(db.DB())
	if err != nil {
		log.Panicf("Failed to init prompt runtime: %v", err)
	}
	workspaceRoot, err := resolveEffectiveWorkspaceRoot(c.WorkspaceDir)
	if err != nil {
		log.Panicf("Failed to resolve workspace root: %v", err)
	}
	skillLoader := coreskills.NewLoader(workspaceRoot)
	authLogic, err := logics.NewAuthLogic(db.DB(), logics.AuthConfig{})
	if err != nil {
		log.Panicf("Failed to init auth logic: %v", err)
	}
	toolRegistry, err := newDefaultToolRegistry(workspaceRoot, c.Tools.WebSearch.BuiltinOptions())
	if err != nil {
		log.Panicf("Failed to register builtin tools: %v", err)
	}
	resolver := &coreagent.ModelResolver{Providers: c.LLM}
	if err := registerAgentRunExecutor(taskManager, approvalStore, interactionStore, resolver, conversationStore, toolRegistry, promptRuntime.Resolver, workspaceRoot, buildConfiguredLLMClientFactory(c), auditRuntime.RunRecorder); err != nil {
		log.Panicf("Failed to register agent.run executor: %v", err)
	}
	taskManager.Start(globalCtx)

	router.Init(engine, c.Server.ApiBasePath, c.Server.StaticPaths, buildRouterDependencies(taskManager, approvalStore, conversationStore, auditRuntime.Store, resolver, promptRuntime.Store, promptRuntime.Resolver, skillLoader, authLogic, interactionStore))

	addr := fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Panicf("Failed to listen on %s: %v", addr, err)
	}
	go func() {
		if err := engine.RunListener(ln); err != nil {
			log.Panicf("Failed to run server: %v", err)
		}
	}()
	log.Infof("gin listening on %s", addr)

	select {
	case <-globalCtx.Done():
		_ = ln.Close()
		log.Info("Shutting down server...")
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

func registerAgentRunExecutor(taskManager *coretasks.Manager, approvalStore *approvals.Store, interactionStore *interactions.Store, resolver *coreagent.ModelResolver, conversationStore *coreagent.ConversationStore, toolRegistry *coretools.Registry, promptResolver *coreprompt.Resolver, workspaceRoot string, clientFactory coreagent.ClientFactory, auditRecorder coreaudit.Recorder) error {
	if taskManager == nil {
		return fmt.Errorf("task manager is required")
	}
	return taskManager.RegisterExecutor("agent.run", coreagent.NewTaskExecutor(buildAgentRunExecutorDependencies(resolver, conversationStore, toolRegistry, approvalStore, interactionStore, promptResolver, workspaceRoot, clientFactory, auditRecorder)))
}

func buildRouterDependencies(taskManager *coretasks.Manager, approvalStore *approvals.Store, conversationStore *coreagent.ConversationStore, auditStore *coreaudit.Store, resolver *coreagent.ModelResolver, promptStore *coreprompt.Store, promptResolver *coreprompt.Resolver, skillLoader *coreskills.Loader, authLogic *logics.AuthLogic, interactionStores ...*interactions.Store) router.Dependencies {
	var interactionStore *interactions.Store
	if len(interactionStores) > 0 {
		interactionStore = interactionStores[0]
	}
	return router.Dependencies{
		TaskManager:       taskManager,
		ApprovalStore:     approvalStore,
		InteractionStore:  interactionStore,
		ConversationStore: conversationStore,
		AuditStore:        auditStore,
		ModelResolver:     resolver,
		PromptStore:       promptStore,
		PromptResolver:    promptResolver,
		SkillLoader:       skillLoader,
		AuthLogic:         authLogic,
	}
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

func buildAgentRunExecutorDependencies(resolver *coreagent.ModelResolver, conversationStore *coreagent.ConversationStore, toolRegistry *coretools.Registry, approvalStore *approvals.Store, interactionStore *interactions.Store, promptResolver *coreprompt.Resolver, workspaceRoot string, clientFactory coreagent.ClientFactory, auditRecorder coreaudit.Recorder) coreagent.ExecutorDependencies {
	return coreagent.ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: conversationStore,
		Registry:          toolRegistry,
		ApprovalStore:     approvalStore,
		InteractionStore:  interactionStore,
		PromptResolver:    promptResolver,
		SkillsResolver:    coreskills.NewResolver(coreskills.NewLoader(workspaceRoot)),
		WorkspaceRoot:     workspaceRoot,
		ClientFactory:     clientFactory,
		AuditRecorder:     auditRecorder,
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

func newDefaultToolRegistry(workspaceRoot string, webSearch builtin.WebSearchOptions) (*coretools.Registry, error) {
	registry := coretools.NewRegistry()
	if err := builtin.Register(registry, builtin.Options{WorkspaceRoot: workspaceRoot, WebSearch: webSearch}); err != nil {
		return nil, err
	}
	return registry, nil
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
