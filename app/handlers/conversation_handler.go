package handlers

import (
	"errors"
	"fmt"
	"net/http"

	coreagent "github.com/EquentR/agent_runtime/core/agent"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type ConversationHandler struct {
	store *coreagent.ConversationStore
}

// NewConversationHandler 创建会话查询接口处理器。
func NewConversationHandler(store *coreagent.ConversationStore) *ConversationHandler {
	return &ConversationHandler{store: store}
}

// Register 注册 conversation 只读接口路由。
func (h *ConversationHandler) Register(rg *gin.RouterGroup) {
	if h.store == nil {
		return
	}
	resp.HandlerWrapper(rg, "conversations", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleListConversations),
		resp.NewJsonOptionsHandler(h.handleGetConversation),
		resp.NewJsonOptionsHandler(h.handleGetConversationMessages),
	})
}

// handleListConversations 返回会话列表接口的路由定义。
//
// @Summary 获取会话列表
// @Description 返回按最近活跃时间倒序排列的 conversation 列表，包含标题、最后一条消息摘要和消息数等轻量展示字段。
// @Tags conversations
// @Produce json
// @Success 200 {object} ConversationListSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Router /conversations [get]
func (h *ConversationHandler) handleListConversations() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.store == nil {
			return nil, nil, fmt.Errorf("conversation store is not configured")
		}
		conversations, err := h.store.ListConversations(c.Request.Context())
		return conversations, nil, err
	}, nil
}

// handleGetConversation 返回单个会话详情接口的路由定义。
//
// @Summary 获取会话详情
// @Description 根据 conversation id 返回会话元数据，包括 provider、model、标题、最近消息摘要与消息数。
// @Tags conversations
// @Produce json
// @Param id path string true "会话 ID"
// @Success 200 {object} ConversationDetailSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /conversations/{id} [get]
func (h *ConversationHandler) handleGetConversation() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.store == nil {
			return nil, nil, fmt.Errorf("conversation store is not configured")
		}
		conversation, err := h.store.GetConversation(c.Request.Context(), c.Param("id"))
		if errors.Is(err, coreagent.ErrConversationNotFound) {
			return nil, []resp.ResOpt{resp.WithCode(resp.NotFound)}, err
		}
		return conversation, nil, err
	}, nil
}

// handleGetConversationMessages 返回指定会话历史消息列表接口的路由定义。
//
// @Summary 获取会话消息历史
// @Description 根据 conversation id 返回按写入顺序排列的历史消息列表，供 UI 恢复聊天内容。
// @Tags conversations
// @Produce json
// @Param id path string true "会话 ID"
// @Success 200 {object} ConversationMessagesSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /conversations/{id}/messages [get]
func (h *ConversationHandler) handleGetConversationMessages() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/:id/messages", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.store == nil {
			return nil, nil, fmt.Errorf("conversation store is not configured")
		}
		if _, err := h.store.GetConversation(c.Request.Context(), c.Param("id")); errors.Is(err, coreagent.ErrConversationNotFound) {
			return nil, []resp.ResOpt{resp.WithCode(resp.NotFound)}, err
		} else if err != nil {
			return nil, nil, err
		}
		messages, err := h.store.ListMessages(c.Request.Context(), c.Param("id"))
		return messages, nil, err
	}, nil
}
