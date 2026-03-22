package commands

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/EquentR/agent_runtime/app/config"
	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/migration"
	"github.com/EquentR/agent_runtime/app/router"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	googleclient "github.com/EquentR/agent_runtime/core/providers/client/google"
	openaicompletions "github.com/EquentR/agent_runtime/core/providers/client/openai_completions"
	openairesponses "github.com/EquentR/agent_runtime/core/providers/client/openai_responses"
	model "github.com/EquentR/agent_runtime/core/providers/types"
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
	// 初始化日志
	log.Init(&c.Log)

	// 打印版本信息
	log.Infof("Application: Version: %s, Git Commit: %s", version, commit)

	// 初始化数据库
	db.Init(&c.Sqlite)
	// 迁移表结构
	migration.Bootstrap(version)

	// 初始化web服务器
	engine := rest.Init()

	// 初始化任务持久层与后台管理器，为后续 agent executor 预留接入点。
	taskStore := coretasks.NewStore(db.DB())
	auditRuntime, err := initAuditRuntime(db.DB())
	if err != nil {
		log.Panicf("Failed to init audit runtime: %v", err)
	}
	taskManager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{
		RunnerID:      "example-agent",
		AuditRecorder: auditRuntime.TaskRecorder,
	})
	conversationStore := coreagent.NewConversationStore(db.DB())
	if err := conversationStore.AutoMigrate(); err != nil {
		log.Panicf("Failed to migrate conversation store: %v", err)
	}
	authLogic, err := logics.NewAuthLogic(db.DB(), logics.AuthConfig{})
	if err != nil {
		log.Panicf("Failed to init auth logic: %v", err)
	}
	toolRegistry, err := newDefaultToolRegistry(c.WorkspaceDir)
	if err != nil {
		log.Panicf("Failed to register builtin tools: %v", err)
	}
	resolver := &coreagent.ModelResolver{Providers: c.LLM}
	if err := registerAgentRunExecutor(taskManager, resolver, conversationStore, toolRegistry, buildLLMClientFactory(), auditRuntime.RunRecorder); err != nil {
		log.Panicf("Failed to register agent.run executor: %v", err)
	}
	taskManager.Start(globalCtx)

	// 将任务管理器作为依赖注入路由层。
	router.Init(engine, c.Server.ApiBasePath, c.Server.StaticPaths, buildRouterDependencies(taskManager, conversationStore, auditRuntime.Store, resolver, authLogic))

	addr := fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Panicf("Failed to listen on %s: %v", addr, err)
	}
	// 启动服务器
	go func() {
		if err := engine.RunListener(ln); err != nil {
			log.Panicf("Failed to run server: %v", err)
		}
	}()
	log.Infof("gin listening on %s", addr)

	// 等待关闭信号
	select {
	case <-globalCtx.Done():
		_ = ln.Close()
		log.Info("Shutting down server...")
	}
}

func buildLLMClientFactory() coreagent.ClientFactory {
	return func(provider *coretypes.LLMProvider, llmModel *coretypes.LLMModel) (model.LlmClient, error) {
		if provider == nil {
			return nil, fmt.Errorf("llm provider is not configured")
		}
		switch llmModel.ModelType() {
		case coretypes.LLMTypeOpenAIResponses:
			return openairesponses.NewOpenAiResponsesClient(provider.AuthKey(), provider.BaseURL(), 30*time.Second), nil
		case coretypes.LLMTypeOpenAICompletions:
			return openaicompletions.NewOpenAiCompletionsClient(provider.BaseURL(), provider.AuthKey()), nil
		case coretypes.LLMTypeGoogle:
			return googleclient.NewGoogleGenAIClient(provider.BaseURL(), provider.AuthKey())
		default:
			return nil, fmt.Errorf("unsupported llm model type %q", llmModel.ModelType())
		}
	}
}

func registerAgentRunExecutor(taskManager *coretasks.Manager, resolver *coreagent.ModelResolver, conversationStore *coreagent.ConversationStore, toolRegistry *coretools.Registry, clientFactory coreagent.ClientFactory, auditRecorder coreaudit.Recorder) error {
	if taskManager == nil {
		return fmt.Errorf("task manager is required")
	}
	return taskManager.RegisterExecutor("agent.run", coreagent.NewTaskExecutor(coreagent.ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: conversationStore,
		Registry:          toolRegistry,
		ClientFactory:     clientFactory,
		AuditRecorder:     auditRecorder,
	}))
}

func buildRouterDependencies(taskManager *coretasks.Manager, conversationStore *coreagent.ConversationStore, auditStore *coreaudit.Store, resolver *coreagent.ModelResolver, authLogic *logics.AuthLogic) router.Dependencies {
	return router.Dependencies{
		TaskManager:       taskManager,
		ConversationStore: conversationStore,
		AuditStore:        auditStore,
		ModelResolver:     resolver,
		AuthLogic:         authLogic,
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

func newDefaultToolRegistry(workspaceRoot string) (*coretools.Registry, error) {
	registry := coretools.NewRegistry()
	// 检查 workspace 是否为空；为空时使用当前工作目录；有值时确保目录存在，不存在则创建。
	if workspaceRoot == "" {
		var err error
		workspaceRoot, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get current working directory: %w", err)
		}
	} else {
		if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
			return nil, fmt.Errorf("create workspace root %q: %w", workspaceRoot, err)
		}
	}
	if err := builtin.Register(registry, builtin.Options{WorkspaceRoot: workspaceRoot}); err != nil {
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
