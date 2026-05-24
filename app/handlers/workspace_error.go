package handlers

import (
	"errors"
	"strings"

	coreworkspaces "github.com/EquentR/agent_runtime/core/workspaces"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
)

const (
	workspaceErrorCodeHomeChanged  = string(coreworkspaces.ActionErrorCodeHomeChanged)
	workspaceErrorCodePendingMerge = string(coreworkspaces.ActionErrorCodePendingMerge)
)

type workspaceActionErrorResponse struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id,omitempty"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
}

func workspaceActionErrorData(err error, workspaceID string) *workspaceActionErrorResponse {
	if err == nil {
		return nil
	}
	var actionErr *coreworkspaces.ActionError
	if errors.As(err, &actionErr) {
		return &workspaceActionErrorResponse{
			Code:           string(actionErr.Code),
			Message:        actionErr.Message,
			ConversationID: firstNonEmptyString(actionErr.ConversationID, workspaceID),
			WorkspaceID:    firstNonEmptyString(actionErr.WorkspaceID, workspaceID),
		}
	}
	if errors.Is(err, coreworkspaces.ErrWorkspaceHomeChanged) {
		return workspaceActionErrorData(coreworkspaces.NewHomeChangedError(workspaceID), workspaceID)
	}
	if errors.Is(err, coreworkspaces.ErrWorkspacePendingMerge) {
		return workspaceActionErrorData(coreworkspaces.NewPendingMergeError(workspaceID), workspaceID)
	}
	return nil
}

func workspaceActionResponseOptions(err error, workspaceID string, fallback func(error) []resp.ResOpt) (any, []resp.ResOpt) {
	detail := workspaceActionErrorData(err, workspaceID)
	if detail == nil {
		return nil, fallback(err)
	}
	return detail, append(fallback(err), resp.WithMessage(detail.Message))
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
