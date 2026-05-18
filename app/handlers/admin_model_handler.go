package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type ModelTester interface {
	TestModel(ctx context.Context, resolved *coreagent.ResolvedModel) error
}

type AdminModelHandler struct {
	models      *logics.ModelLogic
	audit       *logics.AdminAuditLogic
	tester      ModelTester
	middlewares []gin.HandlerFunc
}

type UserModelHandler struct {
	models      *logics.ModelLogic
	tester      ModelTester
	middlewares []gin.HandlerFunc
}

type updateYAMLModelOverrideRequest struct {
	Enabled *bool  `json:"enabled"`
	Scope   string `json:"scope"`
}

type createCustomModelRequest struct {
	OwnerUserID      uint64                      `json:"owner_user_id"`
	ProviderID       string                      `json:"provider_id"`
	ModelID          string                      `json:"model_id"`
	DisplayName      string                      `json:"display_name"`
	ProviderType     string                      `json:"provider_type"`
	BaseURL          string                      `json:"base_url"`
	APIKey           string                      `json:"api_key"`
	Scope            string                      `json:"scope"`
	Enabled          *bool                       `json:"enabled"`
	ContextMaxTokens int64                       `json:"context_max_tokens"`
	Capabilities     coretypes.ModelCapabilities `json:"capabilities"`
	Cost             *coretypes.ModelPricing     `json:"cost"`
}

type updateCustomModelRequest struct {
	OwnerUserID      *uint64                     `json:"owner_user_id"`
	ProviderID       string                      `json:"provider_id"`
	ModelID          string                      `json:"model_id"`
	DisplayName      string                      `json:"display_name"`
	ProviderType     string                      `json:"provider_type"`
	BaseURL          string                      `json:"base_url"`
	APIKey           string                      `json:"api_key"`
	ClearBaseURL     bool                        `json:"clear_base_url"`
	ClearAPIKey      bool                        `json:"clear_api_key"`
	Scope            string                      `json:"scope"`
	Enabled          *bool                       `json:"enabled"`
	ContextMaxTokens int64                       `json:"context_max_tokens"`
	Capabilities     coretypes.ModelCapabilities `json:"capabilities"`
	Cost             *coretypes.ModelPricing     `json:"cost"`
}

func NewAdminModelHandler(modelLogic *logics.ModelLogic, audit *logics.AdminAuditLogic, tester ModelTester, middlewares ...gin.HandlerFunc) *AdminModelHandler {
	return &AdminModelHandler{models: modelLogic, audit: audit, tester: tester, middlewares: middlewares}
}

func NewUserModelHandler(modelLogic *logics.ModelLogic, tester ModelTester, middlewares ...gin.HandlerFunc) *UserModelHandler {
	return &UserModelHandler{models: modelLogic, tester: tester, middlewares: middlewares}
}

func (h *AdminModelHandler) Register(rg *gin.RouterGroup) {
	if h.models == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "admin/models", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleListYAMLModels),
		resp.NewJsonOptionsHandler(h.handleUpdateYAMLModel),
		resp.NewJsonOptionsHandler(h.handleListCustomModels),
		resp.NewJsonOptionsHandler(h.handleCreateCustomModel),
		resp.NewJsonOptionsHandler(h.handleUpdateCustomModel),
		resp.NewJsonOptionsHandler(h.handleDeleteCustomModel),
		resp.NewJsonOptionsHandler(h.handleTestCustomModel),
	}, options...)
}

func (h *UserModelHandler) Register(rg *gin.RouterGroup) {
	if h.models == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "users/me/models", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleListCustomModels),
		resp.NewJsonOptionsHandler(h.handleCreateCustomModel),
		resp.NewJsonOptionsHandler(h.handleUpdateCustomModel),
		resp.NewJsonOptionsHandler(h.handleDeleteCustomModel),
		resp.NewJsonOptionsHandler(h.handleTestCustomModel),
	}, options...)
}

// @Summary 管理员获取 YAML 模型目录
// @Description 返回配置文件模型及其数据库启用状态和可用范围覆盖。
// @Tags admin-models
// @Produce json
// @Success 200 {object} AdminYAMLModelCatalogSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /admin/models [get]
func (h *AdminModelHandler) handleListYAMLModels() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		catalog, err := h.models.ListYAMLModels(c.Request.Context())
		return catalog, nil, err
	}, nil
}

// @Summary 管理员更新 YAML 模型可用范围
// @Description 只覆盖配置文件模型的 enabled 和 scope。
// @Tags admin-models
// @Accept json
// @Produce json
// @Param provider_id path string true "Provider ID"
// @Param model_id path string true "Model ID"
// @Param request body AdminYAMLModelUpdateSwaggerRequest true "YAML 模型覆盖配置"
// @Success 200 {object} AdminYAMLModelSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /admin/models/yaml/{provider_id}/{model_id} [patch]
func (h *AdminModelHandler) handleUpdateYAMLModel() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPatch, "/yaml/:provider_id/:model_id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := requireModelActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		var request updateYAMLModelOverrideRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		var updated logics.YAMLModelResponse
		if err := h.models.Transaction(c.Request.Context(), func(models *logics.ModelLogic) error {
			before, err := models.ListYAMLModels(c.Request.Context())
			if err != nil {
				return err
			}
			updated, err = models.UpdateYAMLModelOverride(c.Request.Context(), logics.UpdateYAMLModelOverrideInput{
				ProviderID: c.Param("provider_id"),
				ModelID:    c.Param("model_id"),
				Enabled:    request.Enabled,
				Scope:      request.Scope,
				UpdatedBy:  actor.Username,
			})
			if err != nil {
				return err
			}
			return h.recordAuditWithLogic(c, models, *actor, "model", c.Param("provider_id")+"/"+c.Param("model_id"), "admin.models.yaml.update", before, updated)
		}); err != nil {
			return nil, modelErrorOptions(err), err
		}
		return updated, nil, nil
	}, nil
}

// @Summary 管理员获取自定义模型列表
// @Description 返回所有用户创建的自定义模型配置，不包含 API key 明文。
// @Tags admin-models
// @Produce json
// @Success 200 {object} CustomModelListSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /admin/models/custom [get]
func (h *AdminModelHandler) handleListCustomModels() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/custom", func(c *gin.Context) (any, []resp.ResOpt, error) {
		models, err := h.models.ListCustomModels(c.Request.Context())
		return models, nil, err
	}, nil
}

// @Summary 管理员创建自定义模型
// @Description 创建管理员或指定用户拥有的自定义模型配置，API key 加密保存。
// @Tags admin-models
// @Accept json
// @Produce json
// @Param request body CustomModelCreateSwaggerRequest true "自定义模型配置"
// @Success 200 {object} CustomModelSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /admin/models/custom [post]
func (h *AdminModelHandler) handleCreateCustomModel() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/custom", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := requireModelActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		var request createCustomModelRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		input := createCustomInputFromRequest(request)
		if input.OwnerUserID == 0 {
			input.OwnerUserID = actor.ID
		}
		var created logics.CustomModelResponse
		if err := h.models.Transaction(c.Request.Context(), func(models *logics.ModelLogic) error {
			var createErr error
			created, createErr = models.CreateCustomModel(c.Request.Context(), input)
			if createErr != nil {
				return createErr
			}
			return h.recordAuditWithLogic(c, models, *actor, "model", created.ID, "admin.models.custom.create", nil, created)
		}); err != nil {
			return nil, modelErrorOptions(err), err
		}
		return created, nil, nil
	}, nil
}

// @Summary 管理员更新自定义模型
// @Description 更新任意自定义模型配置，敏感字段只在请求中覆盖，不返回明文。
// @Tags admin-models
// @Accept json
// @Produce json
// @Param id path string true "自定义模型 ID"
// @Param request body CustomModelUpdateSwaggerRequest true "自定义模型更新"
// @Success 200 {object} CustomModelSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /admin/models/custom/{id} [put]
func (h *AdminModelHandler) handleUpdateCustomModel() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPut, "/custom/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := requireModelActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		var request updateCustomModelRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		var updated logics.CustomModelResponse
		if err := h.models.Transaction(c.Request.Context(), func(models *logics.ModelLogic) error {
			before, err := models.GetCustomModel(c.Request.Context(), c.Param("id"))
			if err != nil {
				return err
			}
			updated, err = models.UpdateCustomModel(c.Request.Context(), c.Param("id"), updateCustomInputFromRequest(request))
			if err != nil {
				return err
			}
			return h.recordAuditWithLogic(c, models, *actor, "model", updated.ID, "admin.models.custom.update", before, updated)
		}); err != nil {
			return nil, modelErrorOptions(err), err
		}
		return updated, nil, nil
	}, nil
}

// @Summary 管理员删除自定义模型
// @Description 删除任意自定义模型配置。
// @Tags admin-models
// @Produce json
// @Param id path string true "自定义模型 ID"
// @Success 200 {object} CustomModelDeleteSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /admin/models/custom/{id} [delete]
func (h *AdminModelHandler) handleDeleteCustomModel() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodDelete, "/custom/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := requireModelActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		after := gin.H{"deleted": true}
		if err := h.models.Transaction(c.Request.Context(), func(models *logics.ModelLogic) error {
			before, err := models.GetCustomModel(c.Request.Context(), c.Param("id"))
			if err != nil {
				return err
			}
			if err := models.DeleteCustomModel(c.Request.Context(), c.Param("id")); err != nil {
				return err
			}
			return h.recordAuditWithLogic(c, models, *actor, "model", c.Param("id"), "admin.models.custom.delete", before, after)
		}); err != nil {
			return nil, modelErrorOptions(err), err
		}
		return after, nil, nil
	}, nil
}

// @Summary 管理员测试自定义模型
// @Description 管理员可测试任意用户自定义模型连通性，并写入后台操作审计。
// @Tags admin-models
// @Produce json
// @Param id path string true "自定义模型 ID"
// @Success 200 {object} ModelTestSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Failure 503 {object} ErrorSwaggerResponse
// @Router /admin/models/custom/{id}/test [post]
func (h *AdminModelHandler) handleTestCustomModel() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/custom/:id/test", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := requireModelActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		resolved, err := h.models.ResolveCustomForAdmin(c.Request.Context(), c.Param("id"))
		if err != nil {
			after := gin.H{"ok": false, "error": err.Error()}
			if auditErr := h.recordAudit(c, *actor, "model", c.Param("id"), "admin.models.custom.test", nil, after); auditErr != nil {
				return nil, nil, auditErr
			}
			return nil, modelErrorOptions(err), err
		}
		if h.tester == nil {
			testErr := fmt.Errorf("model tester is not configured")
			after := gin.H{"ok": false, "error": testErr.Error()}
			if auditErr := h.recordAudit(c, *actor, "model", c.Param("id"), "admin.models.custom.test", nil, after); auditErr != nil {
				return nil, nil, auditErr
			}
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, testErr
		}
		testErr := h.tester.TestModel(c.Request.Context(), resolved)
		after := gin.H{"ok": testErr == nil}
		if testErr != nil {
			after["error"] = testErr.Error()
		}
		if err := h.recordAudit(c, *actor, "model", c.Param("id"), "admin.models.custom.test", nil, after); err != nil {
			return nil, nil, err
		}
		if testErr != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, testErr
		}
		return after, nil, nil
	}, nil
}

// @Summary 获取当前用户自定义模型
// @Description 返回当前用户拥有的自定义模型配置。
// @Tags user-models
// @Produce json
// @Success 200 {object} CustomModelListSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /users/me/models [get]
func (h *UserModelHandler) handleListCustomModels() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, err := requireModelActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		models, err := h.models.ListCustomModelsForUser(c.Request.Context(), *user)
		return models, nil, err
	}, nil
}

// @Summary 当前用户创建自定义模型
// @Description 创建 owner-scoped 自定义模型配置，API key 加密保存。
// @Tags user-models
// @Accept json
// @Produce json
// @Param request body CustomModelCreateSwaggerRequest true "自定义模型配置"
// @Success 200 {object} CustomModelSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /users/me/models [post]
func (h *UserModelHandler) handleCreateCustomModel() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, err := requireModelActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		var request createCustomModelRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		input := createCustomInputFromRequest(request)
		input.OwnerUserID = user.ID
		input.Scope = logics.ModelScopeOwner
		created, err := h.models.CreateCustomModel(c.Request.Context(), input)
		if err != nil {
			return nil, modelErrorOptions(err), err
		}
		return created, nil, nil
	}, nil
}

// @Summary 当前用户更新自定义模型
// @Description 更新当前用户拥有的自定义模型配置。
// @Tags user-models
// @Accept json
// @Produce json
// @Param id path string true "自定义模型 ID"
// @Param request body CustomModelUpdateSwaggerRequest true "自定义模型更新"
// @Success 200 {object} CustomModelSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /users/me/models/{id} [put]
func (h *UserModelHandler) handleUpdateCustomModel() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPut, "/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, err := requireModelActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		if _, err := h.models.ResolveCustomForOwner(c.Request.Context(), *user, c.Param("id")); err != nil {
			return nil, modelErrorOptions(err), err
		}
		var request updateCustomModelRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		ownerID := user.ID
		input := updateCustomInputFromRequest(request)
		input.OwnerUserID = &ownerID
		input.Scope = logics.ModelScopeOwner
		updated, err := h.models.UpdateCustomModel(c.Request.Context(), c.Param("id"), input)
		if err != nil {
			return nil, modelErrorOptions(err), err
		}
		return updated, nil, nil
	}, nil
}

// @Summary 当前用户删除自定义模型
// @Description 删除当前用户拥有的自定义模型配置。
// @Tags user-models
// @Produce json
// @Param id path string true "自定义模型 ID"
// @Success 200 {object} CustomModelDeleteSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /users/me/models/{id} [delete]
func (h *UserModelHandler) handleDeleteCustomModel() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodDelete, "/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, err := requireModelActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		if _, err := h.models.ResolveCustomForOwner(c.Request.Context(), *user, c.Param("id")); err != nil {
			return nil, modelErrorOptions(err), err
		}
		if err := h.models.DeleteCustomModel(c.Request.Context(), c.Param("id")); err != nil {
			return nil, modelErrorOptions(err), err
		}
		return gin.H{"deleted": true}, nil, nil
	}, nil
}

// @Summary 当前用户测试自定义模型
// @Description 测试当前用户拥有的自定义模型连通性。
// @Tags user-models
// @Produce json
// @Param id path string true "自定义模型 ID"
// @Success 200 {object} ModelTestSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 503 {object} ErrorSwaggerResponse
// @Router /users/me/models/{id}/test [post]
func (h *UserModelHandler) handleTestCustomModel() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/:id/test", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, err := requireModelActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		resolved, err := h.models.ResolveCustomForOwner(c.Request.Context(), *user, c.Param("id"))
		if err != nil {
			return nil, modelErrorOptions(err), err
		}
		if h.tester == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, fmt.Errorf("model tester is not configured")
		}
		if err := h.tester.TestModel(c.Request.Context(), resolved); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, err
		}
		return gin.H{"ok": true}, nil, nil
	}, nil
}

func createCustomInputFromRequest(request createCustomModelRequest) logics.CreateCustomModelInput {
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	return logics.CreateCustomModelInput{
		OwnerUserID:      request.OwnerUserID,
		ProviderID:       request.ProviderID,
		ModelID:          request.ModelID,
		DisplayName:      request.DisplayName,
		ProviderType:     request.ProviderType,
		BaseURL:          request.BaseURL,
		APIKey:           request.APIKey,
		Scope:            request.Scope,
		Enabled:          enabled,
		ContextMaxTokens: request.ContextMaxTokens,
		Capabilities:     request.Capabilities,
		Cost:             request.Cost,
	}
}

func updateCustomInputFromRequest(request updateCustomModelRequest) logics.UpdateCustomModelInput {
	return logics.UpdateCustomModelInput{
		OwnerUserID:      request.OwnerUserID,
		ProviderID:       request.ProviderID,
		ModelID:          request.ModelID,
		DisplayName:      request.DisplayName,
		ProviderType:     request.ProviderType,
		BaseURL:          request.BaseURL,
		APIKey:           request.APIKey,
		ClearBaseURL:     request.ClearBaseURL,
		ClearAPIKey:      request.ClearAPIKey,
		Scope:            request.Scope,
		Enabled:          request.Enabled,
		ContextMaxTokens: request.ContextMaxTokens,
		Capabilities:     request.Capabilities,
		Cost:             request.Cost,
	}
}

func requireModelActor(c *gin.Context) (*models.User, error) {
	actor := currentAuthUser(c)
	if actor == nil {
		return nil, logics.ErrUnauthorized
	}
	return actor, nil
}

func modelErrorOptions(err error) []resp.ResOpt {
	switch {
	case errors.Is(err, logics.ErrModelNotFound):
		return []resp.ResOpt{resp.WithCode(resp.NotFound)}
	case errors.Is(err, logics.ErrModelUnauthorized):
		return []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}
	case errors.Is(err, logics.ErrModelContextTooSmall),
		errors.Is(err, logics.ErrModelProviderConflict),
		errors.Is(err, logics.ErrModelSelectionConflict),
		errors.Is(err, logics.ErrModelInvalidScope):
		return []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}
	default:
		return nil
	}
}

func (h *AdminModelHandler) recordAudit(c *gin.Context, actor models.User, targetKind string, targetID string, action string, before any, after any) error {
	return h.recordAuditWithLogic(c, nil, actor, targetKind, targetID, action, before, after)
}

func (h *AdminModelHandler) recordAuditWithLogic(c *gin.Context, modelLogic *logics.ModelLogic, actor models.User, targetKind string, targetID string, action string, before any, after any) error {
	if h.audit == nil {
		return fmt.Errorf("admin audit logic is not configured")
	}
	audit := h.audit
	if modelLogic != nil {
		audit = h.audit.WithDB(modelLogic.DB())
	}
	return audit.Record(c.Request.Context(), logics.RecordAdminAuditInput{
		Actor:      actor,
		TargetKind: targetKind,
		TargetID:   strings.TrimSpace(targetID),
		Action:     action,
		Before:     before,
		After:      after,
		IPAddress:  c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	})
}
