package handlers

import (
	"errors"
	"fmt"
	"net/http"

	coreskills "github.com/EquentR/agent_runtime/core/skills"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type SkillHandler struct {
	loader *coreskills.Loader
}

func NewSkillHandler(loader *coreskills.Loader) *SkillHandler {
	return &SkillHandler{loader: loader}
}

func (h *SkillHandler) Register(rg *gin.RouterGroup) {
	if h == nil || h.loader == nil {
		return
	}
	resp.HandlerWrapper(rg, "skills", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleListSkills),
		resp.NewJsonOptionsHandler(h.handleGetSkill),
	})
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
		if h == nil || h.loader == nil {
			return nil, nil, fmt.Errorf("skill loader is not configured")
		}
		items, err := h.loader.List(c.Request.Context())
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
		if h == nil || h.loader == nil {
			return nil, nil, fmt.Errorf("skill loader is not configured")
		}
		skill, err := h.loader.Get(c.Request.Context(), c.Param("name"))
		if err != nil {
			if errors.Is(err, coreskills.ErrSkillNotFound) {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusNotFound)}, err
			}
			return nil, nil, err
		}
		return skill, nil, nil
	}, nil
}
