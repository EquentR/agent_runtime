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
	return loadTaskWithAccess(c, manager, authRequired, ensureTaskOwnedByCurrentUser)
}

func loadTaskForOwnerMutation(c *gin.Context, manager *coretasks.Manager, authRequired bool) (*coretasks.Task, []resp.ResOpt, error) {
	return loadTaskWithAccess(c, manager, authRequired, ensureTaskOwnedByExactCurrentUser)
}

func loadTaskWithAccess(c *gin.Context, manager *coretasks.Manager, authRequired bool, ensureAccess func(*gin.Context, bool, *coretasks.Task) error) (*coretasks.Task, []resp.ResOpt, error) {
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
	if err := ensureAccess(c, authRequired, task); err != nil {
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

func ensureTaskOwnedByExactCurrentUser(c *gin.Context, authRequired bool, task *coretasks.Task) error {
	if !authRequired || task == nil {
		return nil
	}
	user := currentAuthUser(c)
	if user != nil && user.Username == task.CreatedBy {
		return nil
	}
	return errors.New("无权修改该任务工作区")
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
