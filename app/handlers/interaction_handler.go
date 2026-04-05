package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	coreagent "github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/interactions"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type InteractionHandler struct {
	manager       *coretasks.Manager
	interactions  *interactions.Store
	conversations *coreagent.ConversationStore
	middlewares   []gin.HandlerFunc
	authRequired  bool
}

type InteractionResponseRequest struct {
	SelectedOptionID  string   `json:"selected_option_id"`
	SelectedOptionIDs []string `json:"selected_option_ids"`
	CustomText        string   `json:"custom_text"`
}

func NewInteractionHandler(manager *coretasks.Manager, interactionStore *interactions.Store, conversations *coreagent.ConversationStore, middlewares ...gin.HandlerFunc) *InteractionHandler {
	return &InteractionHandler{
		manager:       manager,
		interactions:  interactionStore,
		conversations: conversations,
		middlewares:   middlewares,
		authRequired:  len(middlewares) > 0,
	}
}

func (h *InteractionHandler) Register(rg *gin.RouterGroup) {
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "tasks", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleListTaskInteractions),
		resp.NewJsonOptionsHandler(h.handleRespondInteraction),
	}, options...)
}

func (h *InteractionHandler) handleListTaskInteractions() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/:id/interactions", func(c *gin.Context) (any, []resp.ResOpt, error) {
		task, opts, err := h.loadAccessibleTask(c)
		if err != nil {
			return nil, opts, err
		}
		listed, err := h.interactions.ListTaskInteractions(c.Request.Context(), task.ID)
		if err != nil {
			return nil, nil, err
		}
		return mapInteractionResponses(listed), nil, nil
	}, nil
}

func (h *InteractionHandler) handleRespondInteraction() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/:id/interactions/:interactionID/respond", func(c *gin.Context) (any, []resp.ResOpt, error) {
		task, opts, err := h.loadAccessibleTask(c)
		if err != nil {
			return nil, opts, err
		}
		var request InteractionResponseRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, nil, err
		}
		interactionRecord, err := h.interactions.GetInteraction(c.Request.Context(), task.ID, c.Param("interactionID"))
		if err != nil {
			if errors.Is(err, interactions.ErrInteractionNotFound) {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusNotFound)}, err
			}
			return nil, nil, err
		}
		responsePayload, err := buildValidatedQuestionResponsePayload(interactionRecord, request)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		_, interaction, _, err := h.manager.RespondQuestionInteraction(c.Request.Context(), task.ID, c.Param("interactionID"), interactions.ResponseInput{
			Status:      interactions.StatusResponded,
			Response:    responsePayload,
			RespondedBy: resolveTaskActor(c, task),
			RespondedAt: ptrTime(time.Now().UTC()),
		})
		if err != nil {
			if errors.Is(err, interactions.ErrInteractionNotFound) {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusNotFound)}, err
			}
			return nil, nil, err
		}
		return mapInteractionResponse(interaction), opts, nil
	}, nil
}

func buildValidatedQuestionResponsePayload(interaction *interactions.Interaction, request InteractionResponseRequest) (map[string]any, error) {
	if interaction == nil {
		return nil, fmt.Errorf("interaction is required")
	}
	if interaction.Kind != interactions.KindQuestion {
		return nil, fmt.Errorf("interaction kind must be question")
	}
	requestPayload := decodeInteractionResponse(interaction.RequestJSON)
	allowCustom := requestPayload["allow_custom"] == true
	multiple := requestPayload["multiple"] == true
	selectedOptionID := strings.TrimSpace(request.SelectedOptionID)
	selectedOptionIDs := make([]string, 0, len(request.SelectedOptionIDs))
	for _, optionID := range request.SelectedOptionIDs {
		if trimmed := strings.TrimSpace(optionID); trimmed != "" {
			selectedOptionIDs = append(selectedOptionIDs, trimmed)
		}
	}
	customText := strings.TrimSpace(request.CustomText)
	if !allowCustom {
		customText = ""
	}
	if !multiple && len(selectedOptionIDs) > 0 {
		return nil, fmt.Errorf("selected_option_ids is only allowed when multiple is true")
	}
	if multiple && selectedOptionID != "" {
		return nil, fmt.Errorf("selected_option_id is not allowed when multiple is true")
	}
	if !multiple && selectedOptionID == "" && len(selectedOptionIDs) == 1 {
		selectedOptionID = selectedOptionIDs[0]
		selectedOptionIDs = nil
	}
	payload := map[string]any{}
	if multiple {
		if len(selectedOptionIDs) > 0 {
			payload["selected_option_ids"] = selectedOptionIDs
		}
	} else if selectedOptionID != "" {
		payload["selected_option_id"] = selectedOptionID
	}
	if customText != "" {
		payload["custom_text"] = customText
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("response cannot be empty")
	}
	return payload, nil
}

type interactionResponse struct {
	ID             string         `json:"id"`
	TaskID         string         `json:"task_id"`
	ConversationID string         `json:"conversation_id"`
	StepIndex      int            `json:"step_index"`
	ToolCallID     string         `json:"tool_call_id"`
	Kind           string         `json:"kind"`
	Status         string         `json:"status"`
	RequestJSON    map[string]any `json:"request_json,omitempty"`
	ResponseJSON   map[string]any `json:"response_json,omitempty"`
	RespondedBy    string         `json:"responded_by,omitempty"`
	RespondedAt    *time.Time     `json:"responded_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func mapInteractionResponses(listed []interactions.Interaction) []interactionResponse {
	responses := make([]interactionResponse, 0, len(listed))
	for _, interaction := range listed {
		responses = append(responses, mapInteractionResponse(&interaction))
	}
	return responses
}

func mapInteractionResponse(interaction *interactions.Interaction) interactionResponse {
	if interaction == nil {
		return interactionResponse{}
	}
	return interactionResponse{
		ID:             interaction.ID,
		TaskID:         interaction.TaskID,
		ConversationID: interaction.ConversationID,
		StepIndex:      interaction.StepIndex,
		ToolCallID:     interaction.ToolCallID,
		Kind:           string(interaction.Kind),
		Status:         string(interaction.Status),
		RequestJSON:    decodeInteractionResponse(interaction.RequestJSON),
		ResponseJSON:   decodeInteractionResponse(interaction.ResponseJSON),
		RespondedBy:    interaction.RespondedBy,
		RespondedAt:    interaction.RespondedAt,
		CreatedAt:      interaction.CreatedAt,
		UpdatedAt:      interaction.UpdatedAt,
	}
}

func buildQuestionResponsePayload(selectedOptionID string, customText string) map[string]any {
	payload := map[string]any{}
	if selectedOptionID != "" {
		payload["selected_option_id"] = selectedOptionID
	}
	if customText != "" {
		payload["custom_text"] = customText
	}
	return payload
}

func (h *InteractionHandler) loadAccessibleTask(c *gin.Context) (*coretasks.Task, []resp.ResOpt, error) {
	if h == nil || h.manager == nil {
		return nil, nil, fmt.Errorf("task manager is not configured")
	}
	if h.interactions == nil {
		return nil, nil, fmt.Errorf("interaction store is not configured")
	}
	return loadOwnedTask(c, h.manager, h.authRequired)
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func decodeInteractionResponse(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	return payload
}
