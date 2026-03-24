package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

var errPromptAccessDenied = errors.New("无权访问平台提示词")

var errPromptDocumentNotFound = errors.New("prompt document not found")
var errPromptDocumentAlreadyExists = errors.New("prompt document already exists")
var errPromptBindingNotFound = errors.New("prompt binding not found")
var errReferencedPromptDocumentNotFound = errors.New("referenced prompt document not found")
var errInvalidPromptBindingID = errors.New("invalid binding id")

type PromptHandler struct {
	store       *coreprompt.Store
	middlewares []gin.HandlerFunc
}

type createPromptDocumentRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Scope       string `json:"scope"`
	Status      string `json:"status"`
}

type updatePromptDocumentRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Content     *string `json:"content"`
	Scope       *string `json:"scope"`
	Status      *string `json:"status"`
}

type createPromptBindingRequest struct {
	PromptID   string `json:"prompt_id"`
	Scene      string `json:"scene"`
	Phase      string `json:"phase"`
	IsDefault  bool   `json:"is_default"`
	Priority   int    `json:"priority"`
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
	Status     string `json:"status"`
}

type updatePromptBindingRequest struct {
	PromptID   *string `json:"prompt_id"`
	Scene      *string `json:"scene"`
	Phase      *string `json:"phase"`
	IsDefault  *bool   `json:"is_default"`
	Priority   *int    `json:"priority"`
	ProviderID *string `json:"provider_id"`
	ModelID    *string `json:"model_id"`
	Status     *string `json:"status"`
}

func NewPromptHandler(store *coreprompt.Store, middlewares ...gin.HandlerFunc) *PromptHandler {
	return &PromptHandler{store: store, middlewares: middlewares}
}

func (h *PromptHandler) Register(rg *gin.RouterGroup) {
	if h.store == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "prompts", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleListDocuments),
		resp.NewJsonOptionsHandler(h.handleGetDocument),
		resp.NewJsonOptionsHandler(h.handleCreateDocument),
		resp.NewJsonOptionsHandler(h.handleUpdateDocument),
		resp.NewJsonOptionsHandler(h.handleDeleteDocument),
		resp.NewJsonOptionsHandler(h.handleListBindings),
		resp.NewJsonOptionsHandler(h.handleGetBinding),
		resp.NewJsonOptionsHandler(h.handleCreateBinding),
		resp.NewJsonOptionsHandler(h.handleUpdateBinding),
		resp.NewJsonOptionsHandler(h.handleDeleteBinding),
	}, options...)
}

// handleListDocuments 返回提示词文档列表接口定义。
//
// @Summary 获取提示词文档列表
// @Description 仅管理员可读取平台提示词文档列表，可按 status 和 scope 过滤。
// @Tags prompts
// @Produce json
// @Param status query string false "状态"
// @Param scope query string false "作用域"
// @Success 200 {object} PromptDocumentListSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /prompts/documents [get]
func (h *PromptHandler) handleListDocuments() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/documents", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := h.requireStore(); err != nil {
			return nil, nil, err
		}
		_, resOpts, err := h.requireAdminUser(c)
		if err != nil {
			return nil, resOpts, err
		}
		documents, err := h.store.ListDocuments(c.Request.Context(), coreprompt.ListDocumentsFilter{
			Status: c.Query("status"),
			Scope:  c.Query("scope"),
		})
		if err != nil {
			return nil, nil, err
		}
		return toPromptDocumentSwaggerDocs(documents), nil, nil
	}, nil
}

// handleGetDocument 返回提示词文档详情接口定义。
//
// @Summary 获取提示词文档详情
// @Description 仅管理员可读取指定提示词文档。
// @Tags prompts
// @Produce json
// @Param id path string true "提示词文档 ID"
// @Success 200 {object} PromptDocumentSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /prompts/documents/{id} [get]
func (h *PromptHandler) handleGetDocument() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/documents/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := h.requireStore(); err != nil {
			return nil, nil, err
		}
		_, resOpts, err := h.requireAdminUser(c)
		if err != nil {
			return nil, resOpts, err
		}
		document, err := h.store.GetDocument(c.Request.Context(), c.Param("id"))
		if errors.Is(err, coreprompt.ErrPromptDocumentNotFound) {
			return nil, []resp.ResOpt{resp.WithCode(resp.NotFound)}, errPromptDocumentNotFound
		}
		if err != nil {
			return nil, nil, err
		}
		return toPromptDocumentSwaggerDoc(document), nil, nil
	}, nil
}

// handleCreateDocument 返回创建提示词文档接口定义。
//
// @Summary 创建提示词文档
// @Description 仅管理员可创建平台提示词文档。
// @Tags prompts
// @Accept json
// @Produce json
// @Param body body PromptDocumentCreateSwaggerRequest true "提示词文档请求"
// @Success 200 {object} PromptDocumentSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 409 {object} ErrorSwaggerResponse
// @Router /prompts/documents [post]
func (h *PromptHandler) handleCreateDocument() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/documents", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := h.requireStore(); err != nil {
			return nil, nil, err
		}
		username, resOpts, err := h.requireAdminUser(c)
		if err != nil {
			return nil, resOpts, err
		}
		var request createPromptDocumentRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		document, err := h.store.CreateDocument(c.Request.Context(), coreprompt.CreateDocumentInput{
			ID:          request.ID,
			Name:        request.Name,
			Description: request.Description,
			Content:     request.Content,
			Scope:       request.Scope,
			Status:      request.Status,
			CreatedBy:   username,
			UpdatedBy:   username,
		})
		if err != nil {
			mappedOpts, mappedErr := mapPromptDocumentCreateError(err)
			return nil, mappedOpts, mappedErr
		}
		return toPromptDocumentSwaggerDoc(document), nil, nil
	}, nil
}

// handleUpdateDocument 返回更新提示词文档接口定义。
//
// @Summary 更新提示词文档
// @Description 仅管理员可更新指定提示词文档。
// @Tags prompts
// @Accept json
// @Produce json
// @Param id path string true "提示词文档 ID"
// @Param body body PromptDocumentUpdateSwaggerRequest true "提示词文档更新请求"
// @Success 200 {object} PromptDocumentSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /prompts/documents/{id} [put]
func (h *PromptHandler) handleUpdateDocument() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPut, "/documents/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := h.requireStore(); err != nil {
			return nil, nil, err
		}
		username, resOpts, err := h.requireAdminUser(c)
		if err != nil {
			return nil, resOpts, err
		}
		var request updatePromptDocumentRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		document, err := h.store.UpdateDocument(c.Request.Context(), coreprompt.UpdateDocumentInput{
			ID:          c.Param("id"),
			Name:        request.Name,
			Description: request.Description,
			Content:     request.Content,
			Scope:       request.Scope,
			Status:      request.Status,
			UpdatedBy:   stringPointer(username),
		})
		if errors.Is(err, coreprompt.ErrPromptDocumentNotFound) {
			return nil, []resp.ResOpt{resp.WithCode(resp.NotFound)}, errPromptDocumentNotFound
		}
		if err != nil {
			return nil, nil, err
		}
		return toPromptDocumentSwaggerDoc(document), nil, nil
	}, nil
}

// handleDeleteDocument 返回删除提示词文档接口定义。
//
// @Summary 删除提示词文档
// @Description 仅管理员可删除指定提示词文档，并清理关联绑定。
// @Tags prompts
// @Produce json
// @Param id path string true "提示词文档 ID"
// @Success 200 {object} PromptDeleteSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /prompts/documents/{id} [delete]
func (h *PromptHandler) handleDeleteDocument() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodDelete, "/documents/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := h.requireStore(); err != nil {
			return nil, nil, err
		}
		_, resOpts, err := h.requireAdminUser(c)
		if err != nil {
			return gin.H{"deleted": false}, resOpts, err
		}
		if err := h.store.DeleteDocument(c.Request.Context(), c.Param("id")); errors.Is(err, coreprompt.ErrPromptDocumentNotFound) {
			return gin.H{"deleted": false}, []resp.ResOpt{resp.WithCode(resp.NotFound)}, errPromptDocumentNotFound
		} else if err != nil {
			return nil, nil, err
		}
		return gin.H{"deleted": true}, nil, nil
	}, nil
}

// handleListBindings 返回提示词绑定列表接口定义。
//
// @Summary 获取提示词绑定列表
// @Description 仅管理员可读取提示词绑定列表，可按 scene、phase、status、prompt_id、provider_id、model_id 过滤。
// @Tags prompts
// @Produce json
// @Param scene query string false "场景"
// @Param phase query string false "阶段"
// @Param status query string false "状态"
// @Param prompt_id query string false "提示词文档 ID"
// @Param provider_id query string false "Provider ID"
// @Param model_id query string false "Model ID"
// @Success 200 {object} PromptBindingListSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /prompts/bindings [get]
func (h *PromptHandler) handleListBindings() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/bindings", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := h.requireStore(); err != nil {
			return nil, nil, err
		}
		_, resOpts, err := h.requireAdminUser(c)
		if err != nil {
			return nil, resOpts, err
		}
		bindings, err := h.store.ListBindings(c.Request.Context(), coreprompt.ListBindingsFilter{
			Scene:      c.Query("scene"),
			Phase:      c.Query("phase"),
			Status:     c.Query("status"),
			PromptID:   c.Query("prompt_id"),
			ProviderID: c.Query("provider_id"),
			ModelID:    c.Query("model_id"),
		})
		if err != nil {
			return nil, nil, err
		}
		return toPromptBindingSwaggerDocs(bindings), nil, nil
	}, nil
}

// handleGetBinding 返回提示词绑定详情接口定义。
//
// @Summary 获取提示词绑定详情
// @Description 仅管理员可读取指定提示词绑定。
// @Tags prompts
// @Produce json
// @Param id path int true "提示词绑定 ID"
// @Success 200 {object} PromptBindingSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /prompts/bindings/{id} [get]
func (h *PromptHandler) handleGetBinding() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/bindings/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := h.requireStore(); err != nil {
			return nil, nil, err
		}
		_, resOpts, err := h.requireAdminUser(c)
		if err != nil {
			return nil, resOpts, err
		}
		bindingID, parseErr := parsePromptBindingID(c.Param("id"))
		if parseErr != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, parseErr
		}
		binding, err := h.store.GetBinding(c.Request.Context(), bindingID)
		if errors.Is(err, coreprompt.ErrPromptBindingNotFound) {
			return nil, []resp.ResOpt{resp.WithCode(resp.NotFound)}, errPromptBindingNotFound
		}
		if err != nil {
			return nil, nil, err
		}
		return toPromptBindingSwaggerDoc(binding), nil, nil
	}, nil
}

// handleCreateBinding 返回创建提示词绑定接口定义。
//
// @Summary 创建提示词绑定
// @Description 仅管理员可创建提示词绑定。
// @Tags prompts
// @Accept json
// @Produce json
// @Param body body PromptBindingCreateSwaggerRequest true "提示词绑定请求"
// @Success 200 {object} PromptBindingSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /prompts/bindings [post]
func (h *PromptHandler) handleCreateBinding() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/bindings", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := h.requireStore(); err != nil {
			return nil, nil, err
		}
		username, resOpts, err := h.requireAdminUser(c)
		if err != nil {
			return nil, resOpts, err
		}
		var request createPromptBindingRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		binding, err := h.store.CreateBinding(c.Request.Context(), coreprompt.CreateBindingInput{
			PromptID:   request.PromptID,
			Scene:      request.Scene,
			Phase:      request.Phase,
			IsDefault:  request.IsDefault,
			Priority:   request.Priority,
			ProviderID: request.ProviderID,
			ModelID:    request.ModelID,
			Status:     request.Status,
			CreatedBy:  username,
			UpdatedBy:  username,
		})
		if err != nil {
			mappedOpts, mappedErr := mapPromptBindingWriteError(err)
			return nil, mappedOpts, mappedErr
		}
		return toPromptBindingSwaggerDoc(binding), nil, nil
	}, nil
}

// handleUpdateBinding 返回更新提示词绑定接口定义。
//
// @Summary 更新提示词绑定
// @Description 仅管理员可更新指定提示词绑定。
// @Tags prompts
// @Accept json
// @Produce json
// @Param id path int true "提示词绑定 ID"
// @Param body body PromptBindingUpdateSwaggerRequest true "提示词绑定更新请求"
// @Success 200 {object} PromptBindingSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /prompts/bindings/{id} [put]
func (h *PromptHandler) handleUpdateBinding() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPut, "/bindings/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := h.requireStore(); err != nil {
			return nil, nil, err
		}
		username, resOpts, err := h.requireAdminUser(c)
		if err != nil {
			return nil, resOpts, err
		}
		bindingID, parseErr := parsePromptBindingID(c.Param("id"))
		if parseErr != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, parseErr
		}
		var request updatePromptBindingRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		binding, err := h.store.UpdateBinding(c.Request.Context(), coreprompt.UpdateBindingInput{
			ID:         bindingID,
			PromptID:   request.PromptID,
			Scene:      request.Scene,
			Phase:      request.Phase,
			IsDefault:  request.IsDefault,
			Priority:   request.Priority,
			ProviderID: request.ProviderID,
			ModelID:    request.ModelID,
			Status:     request.Status,
			UpdatedBy:  stringPointer(username),
		})
		if err != nil {
			mappedOpts, mappedErr := mapPromptBindingWriteError(err)
			return nil, mappedOpts, mappedErr
		}
		return toPromptBindingSwaggerDoc(binding), nil, nil
	}, nil
}

// handleDeleteBinding 返回删除提示词绑定接口定义。
//
// @Summary 删除提示词绑定
// @Description 仅管理员可删除指定提示词绑定。
// @Tags prompts
// @Produce json
// @Param id path int true "提示词绑定 ID"
// @Success 200 {object} PromptDeleteSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /prompts/bindings/{id} [delete]
func (h *PromptHandler) handleDeleteBinding() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodDelete, "/bindings/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := h.requireStore(); err != nil {
			return nil, nil, err
		}
		_, resOpts, err := h.requireAdminUser(c)
		if err != nil {
			return gin.H{"deleted": false}, resOpts, err
		}
		bindingID, parseErr := parsePromptBindingID(c.Param("id"))
		if parseErr != nil {
			return gin.H{"deleted": false}, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, parseErr
		}
		if err := h.store.DeleteBinding(c.Request.Context(), bindingID); errors.Is(err, coreprompt.ErrPromptBindingNotFound) {
			return gin.H{"deleted": false}, []resp.ResOpt{resp.WithCode(resp.NotFound)}, errPromptBindingNotFound
		} else if err != nil {
			return nil, nil, err
		}
		return gin.H{"deleted": true}, nil, nil
	}, nil
}

func (h *PromptHandler) requireStore() error {
	if h.store == nil {
		return fmt.Errorf("prompt store is not configured")
	}
	return nil
}

func (h *PromptHandler) requireAdminUser(c *gin.Context) (string, []resp.ResOpt, error) {
	user := currentAuthUser(c)
	if user != nil && isAdminUser(user) {
		return user.Username, nil, nil
	}
	return "", []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, errPromptAccessDenied
}

func parsePromptBindingID(raw string) (uint64, error) {
	bindingID, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, errInvalidPromptBindingID
	}
	return bindingID, nil
}

func stringPointer(value string) *string {
	return &value
}

func mapPromptDocumentCreateError(err error) ([]resp.ResOpt, error) {
	switch {
	case isDuplicatePromptDocumentIDError(err):
		return []resp.ResOpt{resp.WithCode(http.StatusConflict)}, errPromptDocumentAlreadyExists
	default:
		return nil, err
	}
}

func mapPromptBindingWriteError(err error) ([]resp.ResOpt, error) {
	switch {
	case errors.Is(err, coreprompt.ErrPromptDocumentNotFound):
		return []resp.ResOpt{resp.WithCode(resp.NotFound)}, errReferencedPromptDocumentNotFound
	case errors.Is(err, coreprompt.ErrPromptBindingNotFound):
		return []resp.ResOpt{resp.WithCode(resp.NotFound)}, errPromptBindingNotFound
	default:
		return nil, err
	}
}

func isDuplicatePromptDocumentIDError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "prompt_documents") || !strings.Contains(lower, "id") {
		return false
	}
	return strings.Contains(lower, "unique") || strings.Contains(lower, "duplicate")
}

func toPromptDocumentSwaggerDocs(documents []coreprompt.PromptDocument) []PromptDocumentSwaggerDoc {
	result := make([]PromptDocumentSwaggerDoc, 0, len(documents))
	for _, document := range documents {
		documentCopy := document
		result = append(result, toPromptDocumentSwaggerDoc(&documentCopy))
	}
	return result
}

func toPromptDocumentSwaggerDoc(document *coreprompt.PromptDocument) PromptDocumentSwaggerDoc {
	if document == nil {
		return PromptDocumentSwaggerDoc{}
	}
	return PromptDocumentSwaggerDoc{
		ID:          document.ID,
		Name:        document.Name,
		Description: document.Description,
		Content:     document.Content,
		Scope:       document.Scope,
		Status:      document.Status,
		CreatedBy:   document.CreatedBy,
		UpdatedBy:   document.UpdatedBy,
		CreatedAt:   formatPromptTimestamp(document.CreatedAt),
		UpdatedAt:   formatPromptTimestamp(document.UpdatedAt),
	}
}

func toPromptBindingSwaggerDocs(bindings []coreprompt.PromptBinding) []PromptBindingSwaggerDoc {
	result := make([]PromptBindingSwaggerDoc, 0, len(bindings))
	for _, binding := range bindings {
		bindingCopy := binding
		result = append(result, toPromptBindingSwaggerDoc(&bindingCopy))
	}
	return result
}

func toPromptBindingSwaggerDoc(binding *coreprompt.PromptBinding) PromptBindingSwaggerDoc {
	if binding == nil {
		return PromptBindingSwaggerDoc{}
	}
	return PromptBindingSwaggerDoc{
		ID:         binding.ID,
		PromptID:   binding.PromptID,
		Scene:      binding.Scene,
		Phase:      binding.Phase,
		IsDefault:  binding.IsDefault,
		Priority:   binding.Priority,
		ProviderID: binding.ProviderID,
		ModelID:    binding.ModelID,
		Status:     binding.Status,
		CreatedBy:  binding.CreatedBy,
		UpdatedBy:  binding.UpdatedBy,
		CreatedAt:  formatPromptTimestamp(binding.CreatedAt),
		UpdatedAt:  formatPromptTimestamp(binding.UpdatedAt),
	}
}

func formatPromptTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
