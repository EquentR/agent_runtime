package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/EquentR/agent_runtime/app/models"
	coreskills "github.com/EquentR/agent_runtime/core/skills"
	"github.com/EquentR/agent_runtime/core/workspaces"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type SkillHandler struct {
	loader           *coreskills.Loader
	workspaceManager *workspaces.Manager
	middlewares      []gin.HandlerFunc
}

func NewSkillHandler(loader *coreskills.Loader, middlewares ...gin.HandlerFunc) *SkillHandler {
	return &SkillHandler{loader: loader, middlewares: middlewares}
}

func (h *SkillHandler) WithWorkspaceManager(manager *workspaces.Manager) *SkillHandler {
	if h == nil {
		return h
	}
	h.workspaceManager = manager
	return h
}

func (h *SkillHandler) Register(rg *gin.RouterGroup) {
	if h == nil || (h.loader == nil && h.workspaceManager == nil) {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "skills", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleListSkills),
		resp.NewJsonOptionsHandler(h.handleGetSkill),
	}, options...)
}

// handleListSkills 返回当前 workspace 下可见的 skills 列表。
//
// @Summary 获取技能列表
// @Description 返回当前 workspace 下全部非 hidden 的技能列表，不包含 content。
// @Tags skills
// @Produce json
// @Success 200 {object} SkillListSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Router /skills [get]
func (h *SkillHandler) handleListSkills() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		loader, err := h.loaderForRequest(c)
		if err != nil {
			return nil, nil, err
		}
		items, err := loader.List(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
		visible := make([]coreskills.SkillListItem, 0, len(items))
		for _, item := range items {
			if item.Hidden {
				continue
			}
			visible = append(visible, item)
		}
		return visible, nil, nil
	}, nil
}

// handleGetSkill 返回指定 skill 的详情。
//
// @Summary 获取技能详情
// @Description 根据技能名返回 workspace skill 详情，允许读取 hidden 技能。
// @Tags skills
// @Produce json
// @Param name path string true "技能名"
// @Success 200 {object} SkillSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /skills/{name} [get]
func (h *SkillHandler) handleGetSkill() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/:name", func(c *gin.Context) (any, []resp.ResOpt, error) {
		loader, err := h.loaderForRequest(c)
		if err != nil {
			return nil, nil, err
		}
		skill, err := loader.Get(c.Request.Context(), c.Param("name"))
		if err != nil {
			if errors.Is(err, coreskills.ErrSkillNotFound) {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusNotFound)}, err
			}
			return nil, nil, err
		}
		return skill, nil, nil
	}, nil
}

func (h *SkillHandler) loaderForRequest(c *gin.Context) (*coreskills.Loader, error) {
	if h == nil {
		return nil, fmt.Errorf("skill loader is not configured")
	}
	if h.workspaceManager == nil {
		if h.loader == nil {
			return nil, fmt.Errorf("skill loader is not configured")
		}
		return h.loader, nil
	}
	user := currentAuthUser(c)
	userID := workspaceUserIDForAuthenticatedUser(user)
	if userID == "" {
		return nil, fmt.Errorf("authenticated user is required to resolve workspace skills")
	}
	home, err := h.workspaceManager.EnsureHomeWorkspace(c.Request.Context(), userID)
	if err != nil {
		return nil, err
	}
	return coreskills.NewLoader(home.Root), nil
}

func workspaceUserIDForAuthenticatedUser(user *models.User) string {
	if user == nil {
		return ""
	}
	if user.ID != 0 {
		return strconv.FormatUint(user.ID, 10)
	}
	return strings.TrimSpace(user.Username)
}
