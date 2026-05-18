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

func (h *AdminModelHandler) handleListYAMLModels() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		catalog, err := h.models.ListYAMLModels(c.Request.Context())
		return catalog, nil, err
	}, nil
}

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
		var updated coreagent.ModelOptionEntry
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

func (h *AdminModelHandler) handleListCustomModels() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/custom", func(c *gin.Context) (any, []resp.ResOpt, error) {
		models, err := h.models.ListCustomModels(c.Request.Context())
		return models, nil, err
	}, nil
}

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

func (h *AdminModelHandler) handleTestCustomModel() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/custom/:id/test", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := requireModelActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		resolved, err := h.models.ResolveCustomForAdmin(c.Request.Context(), c.Param("id"))
		if err != nil {
			return nil, modelErrorOptions(err), err
		}
		if h.tester == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, fmt.Errorf("model tester is not configured")
		}
		if err := h.tester.TestModel(c.Request.Context(), resolved); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, err
		}
		after := gin.H{"ok": true}
		if err := h.recordAudit(c, *actor, "model", c.Param("id"), "admin.models.custom.test", nil, after); err != nil {
			return nil, nil, err
		}
		return after, nil, nil
	}, nil
}

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
	case errors.Is(err, logics.ErrModelContextTooSmall):
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
