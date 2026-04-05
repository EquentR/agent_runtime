package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/EquentR/agent_runtime/app/models"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/gin-gonic/gin"
)

func TestEnsureTaskOwnedByCurrentUserAllowsOwnerAndAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	task := &coretasks.Task{CreatedBy: "alice"}

	ownerContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ownerContext.Set(authUserContextKey, &models.User{Username: "alice", Role: models.UserRoleUser})
	if err := ensureTaskOwnedByCurrentUser(ownerContext, true, task); err != nil {
		t.Fatalf("ensureTaskOwnedByCurrentUser() owner error = %v", err)
	}

	adminContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	adminContext.Set(authUserContextKey, &models.User{Username: "root", Role: models.UserRoleAdmin})
	if err := ensureTaskOwnedByCurrentUser(adminContext, true, task); err != nil {
		t.Fatalf("ensureTaskOwnedByCurrentUser() admin error = %v", err)
	}

	otherContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	otherContext.Set(authUserContextKey, &models.User{Username: "bob", Role: models.UserRoleUser})
	if err := ensureTaskOwnedByCurrentUser(otherContext, true, task); err == nil {
		t.Fatal("ensureTaskOwnedByCurrentUser() other user unexpectedly succeeded")
	}
}

func TestResolveTaskActorFallsBackToTaskCreator(t *testing.T) {
	gin.SetMode(gin.TestMode)

	task := &coretasks.Task{CreatedBy: "alice"}

	userContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	userContext.Set(authUserContextKey, &models.User{Username: "bob", Role: models.UserRoleUser})
	if got := resolveTaskActor(userContext, task); got != "bob" {
		t.Fatalf("resolveTaskActor() with auth user = %q, want %q", got, "bob")
	}

	anonymousContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	if got := resolveTaskActor(anonymousContext, task); got != "alice" {
		t.Fatalf("resolveTaskActor() fallback = %q, want %q", got, "alice")
	}
}
