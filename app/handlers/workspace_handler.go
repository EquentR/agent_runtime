package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"reflect"
	"strings"
	"unsafe"

	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coreworkspaces "github.com/EquentR/agent_runtime/core/workspaces"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type workspaceBrowser interface {
	Snapshot(ctx context.Context, conversationID string, filterPath string) (*coreworkspaces.WorkspaceSnapshot, error)
	File(ctx context.Context, conversationID string, filePath string) (*coreworkspaces.WorkspaceFileDetail, error)
	Diff(ctx context.Context, conversationID string, filePath string) (*coreworkspaces.WorkspaceDiffResult, error)
	Download(ctx context.Context, conversationID string, filePath string) (*coreworkspaces.WorkspaceDownload, error)
}

type taskManagerStoreAdapter struct {
	manager *coretasks.Manager
}

func (a taskManagerStoreAdapter) GetTask(ctx context.Context, id string) (*coretasks.Task, error) {
	store := a.store()
	if store == nil {
		return nil, fmt.Errorf("task manager is not configured")
	}
	return store.GetTask(ctx, id)
}

func (a taskManagerStoreAdapter) FindLatestTaskByConversation(ctx context.Context, conversationID string) (*coretasks.Task, error) {
	store := a.store()
	if store == nil {
		return nil, fmt.Errorf("task manager is not configured")
	}
	return store.FindLatestTaskByConversation(ctx, conversationID)
}

func (a taskManagerStoreAdapter) store() *coretasks.Store {
	if a.manager == nil {
		return nil
	}
	value := reflect.ValueOf(a.manager)
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	if !value.IsValid() {
		return nil
	}
	field := value.FieldByName("store")
	if !field.IsValid() || field.IsNil() {
		return nil
	}
	if field.CanInterface() {
		if store, ok := field.Interface().(*coretasks.Store); ok {
			return store
		}
	}
	ptr := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	if store, ok := ptr.Interface().(*coretasks.Store); ok {
		return store
	}
	return nil
}

type WorkspaceHandler struct {
	conversations *coreagent.ConversationStore
	tasks         *coretasks.Manager
	workspaces    *coreworkspaces.Manager
	browser       workspaceBrowser
	middlewares   []gin.HandlerFunc
	authRequired  bool
}

type WorkspaceBrowserHandler struct {
	*WorkspaceHandler
}

func NewWorkspaceHandler(conversations *coreagent.ConversationStore, tasks *coretasks.Manager, workspaces *coreworkspaces.Manager, middlewares ...gin.HandlerFunc) *WorkspaceHandler {
	return &WorkspaceHandler{
		conversations: conversations,
		tasks:         tasks,
		workspaces:    workspaces,
		browser: coreworkspaces.NewBrowser(coreworkspaces.BrowserDeps{
			TaskStore:        taskManagerStoreAdapter{manager: tasks},
			WorkspaceManager: workspaces,
		}),
		middlewares:  middlewares,
		authRequired: len(middlewares) > 0,
	}
}

func NewWorkspaceBrowserHandler(conversations *coreagent.ConversationStore, tasks *coretasks.Manager, workspaces *coreworkspaces.Manager, middlewares ...gin.HandlerFunc) *WorkspaceBrowserHandler {
	return &WorkspaceBrowserHandler{WorkspaceHandler: NewWorkspaceHandler(conversations, tasks, workspaces, middlewares...)}
}

func (h *WorkspaceHandler) Register(rg *gin.RouterGroup) {
	if h == nil || h.conversations == nil || h.browser == nil {
		return
	}
	if len(h.middlewares) > 0 {
		rg.Use(h.middlewares...)
	}
	rg.Use(h.BrowserMiddleware())
	resp.HandlerWrapper(rg, "conversations", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleGetConversationWorkspaceFiles),
		resp.NewJsonOptionsHandler(h.handleGetConversationWorkspaceFile),
		resp.NewJsonOptionsHandler(h.handleGetConversationWorkspaceDiff),
		resp.NewHandler(http.MethodGet, "/:id/workspace/download", h.handleDownloadConversationWorkspace()),
	})
}

func (h *WorkspaceBrowserHandler) Register(rg *gin.RouterGroup) {
	if h == nil || h.WorkspaceHandler == nil {
		return
	}
	h.WorkspaceHandler.Register(rg)
}

func (h *WorkspaceHandler) BrowserMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			c.Next()
			return
		}
		path := strings.TrimRight(c.Request.URL.Path, "/")
		if !strings.HasSuffix(path, "/workspace") || !c.Request.URL.Query().Has("path") {
			c.Next()
			return
		}
		conversation, resOpts, err := h.loadConversationForWorkspace(c)
		if err != nil {
			resp.BadJson(c, nil, err, resOpts...)
			c.Abort()
			return
		}
		snapshot, err := h.browser.Snapshot(c.Request.Context(), conversation.ID, c.Query("path"))
		if err != nil {
			resp.BadJson(c, nil, err, workspaceBrowserErrorOptions(err)...)
			c.Abort()
			return
		}
		resp.OkJson(c, snapshot)
		c.Abort()
	}
}

// handleGetConversationWorkspaceFiles returns the workspace tree for the current conversation.
//
// @Summary 浏览当前 conversation 的工作区文件树
// @Description 返回当前 conversation 对应 task workspace 的文件树，可选 path 用于过滤子目录。
// @Tags conversations
// @Produce json
// @Param id path string true "Conversation ID"
// @Param path query string false "Filter path"
// @Success 200 {object} WorkspaceBrowserSnapshotSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /conversations/{id}/workspace/files [get]
func (h *WorkspaceHandler) handleGetConversationWorkspaceFiles() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/:id/workspace/files", func(c *gin.Context) (any, []resp.ResOpt, error) {
		conversation, resOpts, err := h.loadConversationForWorkspace(c)
		if err != nil {
			return nil, resOpts, err
		}
		path := strings.TrimSpace(c.Query("path"))
		snapshot, err := h.browser.Snapshot(c.Request.Context(), conversation.ID, path)
		if err != nil {
			return nil, workspaceBrowserErrorOptions(err), err
		}
		return snapshot, nil, nil
	}, nil
}

// handleGetConversationWorkspaceFile returns the selected workspace file content and metadata.
//
// @Summary 查看当前 conversation 工作区文件
// @Description 返回当前 conversation 对应 task workspace 中指定 path 的文件内容和元数据。
// @Tags conversations
// @Produce json
// @Param id path string true "Conversation ID"
// @Param path query string true "File path"
// @Success 200 {object} WorkspaceBrowserFileSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /conversations/{id}/workspace/file [get]
func (h *WorkspaceHandler) handleGetConversationWorkspaceFile() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/:id/workspace/file", func(c *gin.Context) (any, []resp.ResOpt, error) {
		conversation, resOpts, err := h.loadConversationForWorkspace(c)
		if err != nil {
			return nil, resOpts, err
		}
		path := strings.TrimSpace(c.Query("path"))
		if path == "" {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("path is required")
		}
		detail, err := h.browser.File(c.Request.Context(), conversation.ID, path)
		if err != nil {
			return nil, workspaceBrowserErrorOptions(err), err
		}
		return detail, nil, nil
	}, nil
}

// handleGetConversationWorkspaceDiff returns a unified diff for a workspace file.
//
// @Summary 查看当前 conversation 工作区 diff
// @Description 将当前 conversation 的 task workspace 与 home workspace 对比并返回统一 diff。
// @Tags conversations
// @Produce json
// @Param id path string true "Conversation ID"
// @Param path query string true "File path"
// @Success 200 {object} WorkspaceBrowserDiffSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /conversations/{id}/workspace/diff [get]
func (h *WorkspaceHandler) handleGetConversationWorkspaceDiff() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/:id/workspace/diff", func(c *gin.Context) (any, []resp.ResOpt, error) {
		conversation, resOpts, err := h.loadConversationForWorkspace(c)
		if err != nil {
			return nil, resOpts, err
		}
		path := strings.TrimSpace(c.Query("path"))
		if path == "" {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("path is required")
		}
		diff, err := h.browser.Diff(c.Request.Context(), conversation.ID, path)
		if err != nil {
			return nil, workspaceBrowserErrorOptions(err), err
		}
		return diff, nil, nil
	}, nil
}

// handleDownloadConversationWorkspace streams a workspace file or directory zip as an attachment.
//
// @Summary 下载当前 conversation 工作区文件或目录
// @Description 以二进制流方式下载当前 conversation 对应 task workspace 中指定路径的文件；目录会打包为 zip。
// @Tags conversations
// @Produce application/octet-stream
// @Produce application/zip
// @Param id path string true "Conversation ID"
// @Param path query string true "File or directory path"
// @Success 200 {file} string "workspace file content or directory zip"
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /conversations/{id}/workspace/download [get]
func (h *WorkspaceHandler) handleDownloadConversationWorkspace() gin.HandlerFunc {
	return func(c *gin.Context) {
		conversation, resOpts, err := h.loadConversationForWorkspace(c)
		if err != nil {
			resp.BadJson(c, nil, err, resOpts...)
			return
		}
		path := strings.TrimSpace(c.Query("path"))
		if path == "" {
			resp.BadJson(c, nil, fmt.Errorf("path is required"), resp.WithCode(http.StatusBadRequest))
			return
		}
		download, err := h.browser.Download(c.Request.Context(), conversation.ID, path)
		if err != nil {
			resp.BadJson(c, nil, err, workspaceBrowserErrorOptions(err)...)
			return
		}
		headers := map[string]string{}
		if download.FileName != "" {
			if disposition := mime.FormatMediaType("attachment", map[string]string{"filename": download.FileName}); disposition != "" {
				headers["Content-Disposition"] = disposition
			}
		}
		contentType := download.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		reader := download.Reader
		if reader == nil {
			reader = io.NopCloser(bytes.NewReader(download.Data))
		}
		defer reader.Close()
		c.DataFromReader(http.StatusOK, download.ContentLength, contentType, reader, headers)
	}
}

func (h *WorkspaceHandler) loadConversationForWorkspace(c *gin.Context) (*coreagent.Conversation, []resp.ResOpt, error) {
	if h == nil || h.conversations == nil {
		return nil, nil, fmt.Errorf("conversation store is not configured")
	}
	conversationID := strings.TrimSpace(c.Param("id"))
	conversation, err := h.conversations.GetConversation(c.Request.Context(), conversationID)
	if errors.Is(err, coreagent.ErrConversationNotFound) {
		return nil, []resp.ResOpt{resp.WithCode(resp.NotFound)}, err
	}
	if err != nil {
		return nil, nil, err
	}
	if err := h.ensureConversationAccess(c, conversation); err != nil {
		return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
	}
	return conversation, nil, nil
}

func (h *WorkspaceHandler) ensureConversationAccess(c *gin.Context, conversation *coreagent.Conversation) error {
	if !h.authRequired {
		return nil
	}
	if conversation == nil {
		return nil
	}
	return ensureConversationOwnedByCurrentUser(c, conversation)
}

func workspaceBrowserErrorOptions(err error) []resp.ResOpt {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, os.ErrNotExist), strings.Contains(message, "not found"):
		return []resp.ResOpt{resp.WithCode(http.StatusNotFound)}
	case strings.Contains(message, "required"), strings.Contains(message, "directory"), strings.Contains(message, "escapes workspace"), strings.Contains(message, "symlink"):
		return []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}
	default:
		return nil
	}
}
