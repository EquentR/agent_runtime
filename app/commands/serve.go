package commands

import (
	"fmt"
	"net"
	"time"

	"github.com/EquentR/agent_runtime/app/config"
	"github.com/EquentR/agent_runtime/app/migration"
	"github.com/EquentR/agent_runtime/app/router"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
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
	taskManager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{
		RunnerID: "example-agent",
	})
	conversationStore := coreagent.NewConversationStore(db.DB())
	if err := conversationStore.AutoMigrate(); err != nil {
		log.Panicf("Failed to migrate conversation store: %v", err)
	}
	toolRegistry, err := newDefaultToolRegistry("")
	if err != nil {
		log.Panicf("Failed to register builtin tools: %v", err)
	}
	if err := taskManager.RegisterExecutor("agent.run", coreagent.NewTaskExecutor(coreagent.ExecutorDependencies{
		Resolver:          &coreagent.ModelResolver{Provider: &c.LLM[0]},
		ConversationStore: conversationStore,
		Registry:          toolRegistry,
		ClientFactory:     buildLLMClientFactory(&c.LLM[0]),
	})); err != nil {
		log.Panicf("Failed to register agent.run executor: %v", err)
	}
	taskManager.Start(globalCtx)

	// 将任务管理器作为依赖注入路由层。
	router.Init(engine, c.Server.ApiBasePath, c.Server.StaticPaths, router.Dependencies{
		TaskManager:       taskManager,
		ConversationStore: conversationStore,
	})

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

func buildLLMClientFactory(provider *coretypes.LLMProvider) coreagent.ClientFactory {
	return func(model *coretypes.LLMModel) (model.LlmClient, error) {
		if provider == nil {
			return nil, fmt.Errorf("llm provider is not configured")
		}
		switch model.ModelType() {
		case coretypes.LLMTypeOpenAIResponses:
			return openairesponses.NewOpenAiResponsesClient(provider.AuthKey(), provider.BaseURL(), 30*time.Second), nil
		case coretypes.LLMTypeOpenAICompletions:
			return openaicompletions.NewOpenAiCompletionsClient(provider.BaseURL(), provider.AuthKey()), nil
		case coretypes.LLMTypeGoogle:
			return googleclient.NewGoogleGenAIClient(provider.BaseURL(), provider.AuthKey())
		default:
			return nil, fmt.Errorf("unsupported llm model type %q", model.ModelType())
		}
	}
}

func newDefaultToolRegistry(workspaceRoot string) (*coretools.Registry, error) {
	registry := coretools.NewRegistry()
	if err := builtin.Register(registry, builtin.Options{WorkspaceRoot: workspaceRoot}); err != nil {
		return nil, err
	}
	return registry, nil
}
