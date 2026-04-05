package handlers

import (
	"errors"
	"fmt"
	"net/http"

	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

func loadOwnedTask(c *gin.Context, manager *coretasks.Manager, authRequired bool) (*coretasks.Task, []resp.ResOpt, error) {
	if manager == nil {
		return nil, nil, fmt.Errorf("task manager is not configured")
	}

	task, err := manager.GetTask(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, coretasks.ErrTaskNotFound) {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusNotFound)}, err
		}
		return nil, nil, err
	}
	if err := ensureTaskOwnedByCurrentUser(c, authRequired, task); err != nil {
		return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
	}
	return task, nil, nil
}

func ensureTaskOwnedByCurrentUser(c *gin.Context, authRequired bool, task *coretasks.Task) error {
	if !authRequired || task == nil {
		return nil
	}
	return ensureOwnerReadableByCurrentUser(c, task.CreatedBy, "无权访问该任务")
}

func resolveTaskActor(c *gin.Context, task *coretasks.Task) string {
	if user := currentAuthUser(c); user != nil && user.Username != "" {
		return user.Username
	}
	if task == nil {
		return ""
	}
	return task.CreatedBy
}
