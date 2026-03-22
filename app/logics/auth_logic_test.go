package logics

import (
	"context"
	"fmt"
	"testing"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuthLogicRegisterAssignsAdminToFirstUser(t *testing.T) {
	logic := newAuthLogicTestSubject(t)

	user, err := logic.Register(context.Background(), "alice", "secret-123", "secret-123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if user.Role != models.UserRoleAdmin {
		t.Fatalf("first user role = %q, want %q", user.Role, models.UserRoleAdmin)
	}

	stored := loadAuthTestUser(t, logic.db, user.ID)
	if stored.Role != models.UserRoleAdmin {
		t.Fatalf("stored first user role = %q, want %q", stored.Role, models.UserRoleAdmin)
	}
}

func TestAuthLogicRegisterAssignsUserRoleToLaterUsers(t *testing.T) {
	logic := newAuthLogicTestSubject(t)

	firstUser, err := logic.Register(context.Background(), "alice", "secret-123", "secret-123")
	if err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	if firstUser.Role != models.UserRoleAdmin {
		t.Fatalf("first user role = %q, want %q", firstUser.Role, models.UserRoleAdmin)
	}

	secondUser, err := logic.Register(context.Background(), "bob", "secret-123", "secret-123")
	if err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}
	if secondUser.Role != models.UserRoleUser {
		t.Fatalf("second user role = %q, want %q", secondUser.Role, models.UserRoleUser)
	}

	stored := loadAuthTestUser(t, logic.db, secondUser.ID)
	if stored.Role != models.UserRoleUser {
		t.Fatalf("stored second user role = %q, want %q", stored.Role, models.UserRoleUser)
	}
}

func TestAuthLogicCurrentUserIncludesRole(t *testing.T) {
	logic := newAuthLogicTestSubject(t)

	registered, err := logic.Register(context.Background(), "alice", "secret-123", "secret-123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	loggedIn, session, err := logic.Login(context.Background(), "alice", "secret-123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loggedIn.Role != registered.Role {
		t.Fatalf("Login() role = %q, want %q", loggedIn.Role, registered.Role)
	}

	current, currentSession, err := logic.CurrentUser(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("CurrentUser() error = %v", err)
	}
	if currentSession.ID != session.ID {
		t.Fatalf("CurrentUser() session id = %q, want %q", currentSession.ID, session.ID)
	}
	if current.Role != registered.Role {
		t.Fatalf("CurrentUser() role = %q, want %q", current.Role, registered.Role)
	}
}

func newAuthLogicTestSubject(t *testing.T) *AuthLogic {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	logic, err := NewAuthLogic(db, AuthConfig{})
	if err != nil {
		t.Fatalf("NewAuthLogic() error = %v", err)
	}
	if err := logic.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return logic
}

func loadAuthTestUser(t *testing.T, db *gorm.DB, userID uint64) models.User {
	t.Helper()

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		t.Fatalf("db.First(user) error = %v", err)
	}
	return user
}
