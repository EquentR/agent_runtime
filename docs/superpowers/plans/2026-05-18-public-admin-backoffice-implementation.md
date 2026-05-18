# Public Admin Backoffice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the public-use admin backoffice foundation: verified email registration, optional Turnstile protection, runtime SMTP/settings, user management, user profile settings, custom LLM models with context budgets, model access control, and admin operation audit.

**Architecture:** Keep existing layering: `app/handlers` parses HTTP and maps status codes, `app/logics` owns application workflows, `core` owns reusable runtime/model resolution, and `pkg` owns shared infrastructure such as encryption and SMTP. Implement backend security gates before frontend UI, then make `/models` and `/tasks` enforce the same server-side authorization rules as the UI. Use append-only migrations and focused stores/logics with narrow responsibilities.

**Tech Stack:** Go 1.25, Gin, GORM/SQLite, bcrypt, AES-GCM, net/smtp, Vue 3, TypeScript, Vue Router, Element Plus, Vitest, Vue Test Utils.

---

## File Structure

Backend files to create:

- `pkg/secret/codec.go`, `pkg/secret/codec_test.go`: AES-GCM secret encryption using an `APP_SECRET`-derived key and masked output helpers.
- `pkg/mail/smtp.go`, `pkg/mail/smtp_test.go`: SMTP sender config, validation, and send abstraction with a fake-friendly interface.
- `app/models/settings.go`: `SystemSetting`, `EmailVerification`, `AdminAuditEvent`, `LLMModelOverride`, `CustomLLMModel`.
- `app/logics/settings_logic.go`, `app/logics/settings_logic_test.go`: YAML defaults + DB override settings access for SMTP, Turnstile, and public registration.
- `app/logics/email_verification_logic.go`, `app/logics/email_verification_logic_test.go`: 6-digit verification code workflow.
- `app/logics/admin_audit_logic.go`, `app/logics/admin_audit_logic_test.go`: admin operation audit writer and list filter.
- `app/logics/model_logic.go`, `app/logics/model_logic_test.go`: YAML model overrides, custom models, context budget normalization, access filtering, and connection test audit.
- `app/handlers/user_handler.go`, `app/handlers/user_handler_test.go`: `/users/me` profile, password, email verification, and user-owned model endpoints.
- `app/handlers/admin_user_handler.go`, `app/handlers/admin_user_handler_test.go`: `/admin/users` management endpoints.
- `app/handlers/admin_settings_handler.go`, `app/handlers/admin_settings_handler_test.go`: `/admin/settings` endpoints.
- `app/handlers/admin_model_handler.go`, `app/handlers/admin_model_handler_test.go`: `/admin/models` endpoints.
- `app/handlers/admin_audit_event_handler.go`, `app/handlers/admin_audit_event_handler_test.go`: `/admin/audit-events` endpoint.

Backend files to modify:

- `app/models/user.go`: add email, display name, status, verification, and force-password-change fields.
- `app/migration/define.go`, `app/migration/init.go`, `app/migration/task_migration_test.go`: append migrations for new columns/tables.
- `app/config/app.go`, `app/config/app_test.go`, `conf/app.yaml`: add YAML defaults for SMTP, Turnstile, public registration, and `APP_SECRET` requirement.
- `app/commands/serve.go`, `app/commands/serve_test.go`: wire settings, secret codec, email verification, admin audit, model logic, and user-state gates.
- `app/router/deps.go`, `app/router/init.go`: pass and register new dependencies.
- `app/logics/auth_logic.go`, `app/logics/auth_logic_test.go`: extend register/login/current-user behavior.
- `app/handlers/auth_handler.go`, `app/handlers/auth_handler_test.go`, `app/handlers/auth_middleware.go`: auth DTOs, Turnstile token handling, user state response, and reusable active-user middleware.
- `app/handlers/model_catalog_handler.go`, `app/handlers/model_catalog_handler_test.go`: return current-user-filtered catalog.
- `app/handlers/task_handler.go`, `app/handlers/task_handler_test.go`: require active verified user and server-side model authorization at task creation.
- `core/agent/executor.go`, `core/agent/executor_test.go`: accept resolved custom models and context budgets without trusting raw frontend catalog data.
- `core/types/provider_config.go`, `core/types/provider_config_test.go`: add optional YAML model scope/enabled metadata without changing provider SDK details.
- `docs/swagger/docs.go`, `docs/swagger/swagger.json`, `docs/swagger/swagger.yaml`: regenerate after handler DTOs stabilize.

Frontend files to create:

- `webapp/src/views/ProfileView.vue`, `webapp/src/views/ProfileView.spec.ts`.
- `webapp/src/views/AdminUsersView.vue`, `webapp/src/views/AdminUsersView.spec.ts`.
- `webapp/src/views/AdminSettingsView.vue`, `webapp/src/views/AdminSettingsView.spec.ts`.
- `webapp/src/views/AdminModelsView.vue`, `webapp/src/views/AdminModelsView.spec.ts`.
- `webapp/src/views/AdminOperationAuditView.vue`, `webapp/src/views/AdminOperationAuditView.spec.ts`.
- `webapp/src/components/AdminLayout.vue`, `webapp/src/components/AdminLayout.spec.ts`.
- `webapp/src/lib/user-state.ts`, `webapp/src/lib/user-state.spec.ts`.

Frontend files to modify:

- `webapp/src/types/api.ts`: auth status, settings, users, model, and admin audit types.
- `webapp/src/lib/api.ts`, `webapp/src/lib/api.spec.ts`: API helpers and normalization.
- `webapp/src/lib/session.ts`, `webapp/src/lib/session.spec.ts`: session state with email/status/required actions.
- `webapp/src/router/index.ts`, `webapp/src/router/index.spec.ts`: profile/admin/verification/force-password routes and gates.
- `webapp/src/views/LoginView.vue`, `webapp/src/views/LoginView.spec.ts`: email registration, Turnstile widget integration, verification flow, hidden registration.
- `webapp/src/views/ChatView.vue`, `webapp/src/views/ChatView.spec.ts`: no-model empty state and active-user gate.
- `webapp/src/lib/model-selection.ts`, `webapp/src/lib/model-selection.spec.ts`: filtered catalog handling.
- `webapp/src/style.css`: shared admin/profile layout styles.

---

### Task 1: Persistence Models And Migrations

**Files:**
- Modify: `app/models/user.go`
- Create: `app/models/settings.go`
- Modify: `app/migration/define.go`
- Modify: `app/migration/init.go`
- Modify: `app/migration/task_migration_test.go`
- Test: `app/migration/task_migration_test.go`

- [ ] **Step 1: Write failing migration tests**

Add tests in `app/migration/task_migration_test.go`:

```go
func TestPublicAdminBackofficeMigrationCreatesSecurityAndModelTables(t *testing.T) {
	db := openMigrationTestDB(t)
	runAllMigrationsForTest(t, db)

	for _, table := range []string{
		"system_settings",
		"email_verifications",
		"admin_audit_events",
		"llm_model_overrides",
		"custom_llm_models",
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("table %s was not created", table)
		}
	}
}

func TestPublicAdminBackofficeMigrationExtendsUsers(t *testing.T) {
	db := openMigrationTestDB(t)
	runAllMigrationsForTest(t, db)

	for _, column := range []string{
		"email",
		"display_name",
		"status",
		"email_verified_at",
		"force_password_change",
	} {
		if !db.Migrator().HasColumn(&models.User{}, column) {
			t.Fatalf("users.%s column missing", column)
		}
	}
}

func TestPublicAdminBackofficeMigrationMarksLegacyUsersForEmailBinding(t *testing.T) {
	db := openMigrationTestDB(t)
	runMigrationsThroughForTest(t, db, "0.1.4")
	if err := db.Create(&models.User{Username: "legacy", PasswordHash: "hash", Role: models.UserRoleAdmin}).Error; err != nil {
		t.Fatalf("seed legacy user: %v", err)
	}
	runRemainingMigrationsForTest(t, db)

	var user models.User
	if err := db.Where("username = ?", "legacy").Take(&user).Error; err != nil {
		t.Fatalf("load migrated user: %v", err)
	}
	if user.Status != models.UserStatusNeedsEmailBinding {
		t.Fatalf("status = %q, want %q", user.Status, models.UserStatusNeedsEmailBinding)
	}
}
```

If helper names differ, add small package-local helpers with these behaviors instead of changing production migration APIs.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./app/migration -run 'PublicAdminBackoffice' -v`

Expected: FAIL because the tables and user columns do not exist.

- [ ] **Step 3: Add models and migration**

In `app/models/user.go`, add constants:

```go
const (
	UserStatusPendingEmailVerification = "pending_email_verification"
	UserStatusActive                   = "active"
	UserStatusDisabled                 = "disabled"
	UserStatusNeedsEmailBinding        = "needs_email_binding"
)
```

Extend `User`:

```go
Email               string     `json:"email" gorm:"type:varchar(255);uniqueIndex:,where:email <> ''"`
DisplayName         string     `json:"display_name" gorm:"type:varchar(128)"`
Status              string     `json:"status" gorm:"type:varchar(64);not null;default:active;index"`
EmailVerifiedAt     *time.Time `json:"email_verified_at"`
ForcePasswordChange bool       `json:"force_password_change" gorm:"not null;default:false"`
```

Create `app/models/settings.go` with table-name methods for:

```go
type SystemSetting struct {
	Key       string    `json:"key" gorm:"type:varchar(128);primaryKey"`
	ValueJSON []byte   `json:"value_json" gorm:"type:blob;not null"`
	Encrypted bool     `json:"encrypted" gorm:"not null;default:false"`
	UpdatedBy string   `json:"updated_by" gorm:"type:varchar(128);index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type EmailVerification struct {
	ID          string     `json:"id" gorm:"type:varchar(128);primaryKey"`
	UserID      uint64     `json:"user_id" gorm:"not null;index"`
	Email       string     `json:"email" gorm:"type:varchar(255);not null;index"`
	Purpose     string     `json:"purpose" gorm:"type:varchar(64);not null;index"`
	CodeHash    string     `json:"-" gorm:"type:varchar(255);not null"`
	Attempts    int        `json:"attempts" gorm:"not null;default:0"`
	MaxAttempts int        `json:"max_attempts" gorm:"not null;default:5"`
	ExpiresAt   time.Time  `json:"expires_at" gorm:"not null;index"`
	LastSentAt  time.Time  `json:"last_sent_at" gorm:"not null"`
	ConsumedAt  *time.Time `json:"consumed_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AdminAuditEvent struct {
	ID          uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	ActorID     uint64    `json:"actor_id" gorm:"index"`
	ActorUsername string  `json:"actor_username" gorm:"type:varchar(128);index"`
	TargetKind  string    `json:"target_kind" gorm:"type:varchar(64);not null;index"`
	TargetID    string    `json:"target_id" gorm:"type:varchar(128);not null;index"`
	Action      string    `json:"action" gorm:"type:varchar(128);not null;index"`
	BeforeJSON []byte    `json:"before_json" gorm:"type:blob"`
	AfterJSON  []byte    `json:"after_json" gorm:"type:blob"`
	IPAddress  string    `json:"ip_address" gorm:"type:varchar(64)"`
	UserAgent  string    `json:"user_agent" gorm:"type:varchar(255)"`
	CreatedAt  time.Time `json:"created_at"`
}

type LLMModelOverride struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	ProviderID string   `json:"provider_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_llm_model_override"`
	ModelID    string   `json:"model_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_llm_model_override"`
	Enabled    bool     `json:"enabled" gorm:"not null;default:true"`
	Scope      string   `json:"scope" gorm:"type:varchar(32);not null;default:admin;index"`
	UpdatedBy  string   `json:"updated_by" gorm:"type:varchar(128)"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CustomLLMModel struct {
	ID               string    `json:"id" gorm:"type:varchar(128);primaryKey"`
	OwnerUserID      uint64    `json:"owner_user_id" gorm:"not null;index"`
	ProviderID       string    `json:"provider_id" gorm:"type:varchar(128);not null;index:idx_custom_model_owner_provider,unique"`
	ModelID          string    `json:"model_id" gorm:"type:varchar(128);not null"`
	DisplayName      string    `json:"display_name" gorm:"type:varchar(128);not null"`
	ProviderType     string    `json:"provider_type" gorm:"type:varchar(64);not null"`
	BaseURL          string    `json:"base_url" gorm:"type:varchar(512)"`
	EncryptedAPIKey  string    `json:"-" gorm:"type:text;not null"`
	Scope            string    `json:"scope" gorm:"type:varchar(32);not null;default:owner;index"`
	Enabled          bool      `json:"enabled" gorm:"not null;default:true"`
	ContextMaxTokens int64     `json:"context_max_tokens" gorm:"not null"`
	CapabilitiesJSON []byte   `json:"capabilities_json" gorm:"type:blob"`
	CostJSON          []byte   `json:"cost_json" gorm:"type:blob"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
```

Add `TableName()` methods returning exact table names from the spec.

Append migration `to015` in `app/migration/define.go`, register it in `app/migration/init.go`, and backfill existing users with:

- empty email remains empty, and non-empty email values are unique
- status becomes `needs_email_binding`
- display name defaults to username
- force password change remains false

- [ ] **Step 4: Run migration tests**

Run: `go test ./app/migration -run 'PublicAdminBackoffice' -v`

Expected: PASS.

- [ ] **Step 5: Run focused model tests**

Run: `go test ./app/models ./app/migration`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add app/models/user.go app/models/settings.go app/migration/define.go app/migration/init.go app/migration/task_migration_test.go
git commit -m "feat: add public admin persistence models"
```

### Task 2: Secret Codec, SMTP Sender, And Runtime Settings

**Files:**
- Create: `pkg/secret/codec.go`
- Create: `pkg/secret/codec_test.go`
- Create: `pkg/mail/smtp.go`
- Create: `pkg/mail/smtp_test.go`
- Modify: `app/config/app.go`
- Modify: `app/config/app_test.go`
- Modify: `conf/app.yaml`
- Create: `app/logics/settings_logic.go`
- Create: `app/logics/settings_logic_test.go`
- Test: `pkg/secret/codec_test.go`, `pkg/mail/smtp_test.go`, `app/logics/settings_logic_test.go`, `app/config/app_test.go`

- [ ] **Step 1: Write failing tests**

Add tests:

```go
func TestSecretCodecEncryptsDecryptsAndMasks(t *testing.T)
func TestSecretCodecRejectsEmptySecret(t *testing.T)
func TestSMTPConfigValidationRequiresHostFromAndCredentialsWhenEnabled(t *testing.T)
func TestSettingsLogicUsesYAMLDefaultsWhenNoDBOverride(t *testing.T)
func TestSettingsLogicDBOverrideMasksSecretsAndPreservesEncryptedValues(t *testing.T)
func TestSettingsLogicPublicRegistrationDefaultsToEnabled(t *testing.T)
func TestConfigParsesSMTPAndTurnstileDefaults(t *testing.T)
```

Use in-memory SQLite for settings logic tests and `secret.NewCodec("test-secret")`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/secret ./pkg/mail ./app/logics ./app/config -run 'SecretCodec|SMTPConfig|SettingsLogic|ConfigParsesSMTP' -v`

Expected: FAIL because packages and logic are missing.

- [ ] **Step 3: Implement secret codec**

`pkg/secret.Codec` must expose:

```go
func NewCodec(appSecret string) (*Codec, error)
func (c *Codec) EncryptString(plaintext string) (string, error)
func (c *Codec) DecryptString(ciphertext string) (string, error)
func MaskSecret(value string) string
```

Use SHA-256 over `appSecret` as the AES-256-GCM key. Encoded ciphertext format is `v1:<base64 nonce+ciphertext>`. `MaskSecret("sk-abcdef")` returns `sk-****cdef`; values shorter than 8 characters return `****`.

- [ ] **Step 4: Implement SMTP config and sender interface**

`pkg/mail` must define:

```go
type SMTPConfig struct {
	Enabled bool
	Host string
	Port int
	Username string
	Password string
	From string
	UseTLS bool
	UseStartTLS bool
}

type Message struct {
	To string
	Subject string
	Body string
}

type Sender interface {
	Send(ctx context.Context, message Message) error
}

func (c SMTPConfig) ValidateForSend() error
func NewSMTPSender(config SMTPConfig) (Sender, error)
```

Keep SMTP network behavior behind `Sender` so logics tests use fakes.

- [ ] **Step 5: Implement settings config and logic**

Add config structs in `app/config/app.go`:

```go
Security SecurityConfig `yaml:"security"`

type SecurityConfig struct {
	AppSecret string `yaml:"appSecret"`
	PublicRegistration PublicRegistrationConfig `yaml:"publicRegistration"`
	SMTP SMTPConfig `yaml:"smtp"`
	Turnstile TurnstileConfig `yaml:"turnstile"`
}
```

Implement `app/logics.SettingsLogic` with methods:

```go
func NewSettingsLogic(db *gorm.DB, defaults SettingsDefaults, codec *secret.Codec) (*SettingsLogic, error)
func (l *SettingsLogic) GetPublicRegistration(ctx context.Context) (PublicRegistrationSettings, error)
func (l *SettingsLogic) UpdatePublicRegistration(ctx context.Context, input UpdatePublicRegistrationInput) (PublicRegistrationSettings, error)
func (l *SettingsLogic) GetSMTP(ctx context.Context) (SMTPSettings, error)
func (l *SettingsLogic) UpdateSMTP(ctx context.Context, input UpdateSMTPInput) (SMTPSettings, error)
func (l *SettingsLogic) GetTurnstile(ctx context.Context) (TurnstileSettings, error)
func (l *SettingsLogic) UpdateTurnstile(ctx context.Context, input UpdateTurnstileInput) (TurnstileSettings, error)
```

Responses must return masked secrets and never plaintext passwords or Turnstile secrets.

- [ ] **Step 6: Run tests**

Run: `go test ./pkg/secret ./pkg/mail ./app/config ./app/logics -run 'SecretCodec|SMTPConfig|SettingsLogic|ConfigParsesSMTP' -v`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/secret pkg/mail app/config/app.go app/config/app_test.go conf/app.yaml app/logics/settings_logic.go app/logics/settings_logic_test.go
git commit -m "feat: add encrypted runtime settings"
```

### Task 3: Auth, Email Verification, Turnstile, And User State Gate

**Files:**
- Modify: `app/logics/auth_logic.go`
- Modify: `app/logics/auth_logic_test.go`
- Create: `app/logics/email_verification_logic.go`
- Create: `app/logics/email_verification_logic_test.go`
- Modify: `app/handlers/auth_handler.go`
- Modify: `app/handlers/auth_handler_test.go`
- Modify: `app/handlers/auth_middleware.go`
- Test: `app/logics/auth_logic_test.go`, `app/logics/email_verification_logic_test.go`, `app/handlers/auth_handler_test.go`

- [ ] **Step 1: Write failing auth and verification tests**

Add tests:

```go
func TestAuthLogicFirstUserRequiresEmailAndMarksVerifiedAdmin(t *testing.T)
func TestAuthLogicRegisterCreatesPendingUserAndSendsVerification(t *testing.T)
func TestAuthLogicRejectsPublicRegistrationWhenDisabled(t *testing.T)
func TestAuthLogicRejectsSecondRegistrationWhenSMTPUnavailable(t *testing.T)
func TestAuthLogicLoginAcceptsUsernameOrEmail(t *testing.T)
func TestAuthLogicLoginRejectsPendingDisabledAndNeedsBindingUsers(t *testing.T)
func TestEmailVerificationAcceptsSixDigitCodeAndActivatesUser(t *testing.T)
func TestEmailVerificationRejectsExpiredCodeAndTooManyAttempts(t *testing.T)
func TestEmailVerificationEnforcesResendCooldown(t *testing.T)
func TestAuthHandlerTurnstileProtectsLoginRegisterAndVerificationSend(t *testing.T)
```

Use fake mail sender and fake Turnstile verifier interfaces in tests.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./app/logics ./app/handlers -run 'FirstUserRequiresEmail|RegisterCreatesPending|PublicRegistration|SMTPUnavailable|LoginAccepts|PendingDisabled|EmailVerification|TurnstileProtects' -v`

Expected: FAIL because auth still only supports username/password registration.

- [ ] **Step 3: Implement auth inputs and errors**

Extend auth logic with:

```go
var (
	ErrEmailTaken = errors.New("邮箱已存在")
	ErrEmailRequired = errors.New("邮箱不能为空")
	ErrPublicRegistrationDisabled = errors.New("当前不开放注册")
	ErrEmailVerificationRequired = errors.New("需要验证邮箱")
	ErrEmailBindingRequired = errors.New("需要绑定邮箱")
	ErrUserDisabled = errors.New("用户已被禁用")
	ErrPasswordChangeRequired = errors.New("需要修改密码")
	ErrMailServiceUnavailable = errors.New("邮件服务未配置")
)

type RegisterInput struct {
	Username string
	Email string
	Password string
	ConfirmPassword string
	TurnstileToken string
}
```

Keep compatibility wrapper `Register(ctx, username, password, confirmPassword string)` for existing tests by calling the new path with an empty email only when the user table is empty in test compatibility mode. New production handler must use `RegisterInput`.

- [ ] **Step 4: Implement email verification logic**

`EmailVerificationLogic` must generate 6 numeric digits, store bcrypt hash, enforce 10-minute expiry, 60-second resend cooldown, and 5 attempts. Verification activates `pending_email_verification` users or updates email for binding flows.

- [ ] **Step 5: Implement Turnstile verifier boundary**

Define:

```go
type TurnstileVerifier interface {
	Verify(ctx context.Context, token string, remoteIP string) error
}
```

Auth handler calls verifier for login, register, and send/resend verification when settings say the action is protected.

- [ ] **Step 6: Implement middleware gates**

Add middleware helpers:

```go
func (m *AuthMiddleware) RequireActiveUser() gin.HandlerFunc
func (m *AuthMiddleware) RequireAdmin() gin.HandlerFunc
```

`RequireActiveUser` requires active, verified, not disabled, not force-password-change. `RequireSession` remains usable for profile/verification flows.

- [ ] **Step 7: Run focused tests**

Run: `go test ./app/logics ./app/handlers -run 'AuthLogic|EmailVerification|AuthHandler|AuthMiddleware' -v`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add app/logics/auth_logic.go app/logics/auth_logic_test.go app/logics/email_verification_logic.go app/logics/email_verification_logic_test.go app/handlers/auth_handler.go app/handlers/auth_handler_test.go app/handlers/auth_middleware.go
git commit -m "feat: add verified email auth flow"
```

### Task 4: Admin Audit, User Management, And Settings APIs

**Files:**
- Create: `app/logics/admin_audit_logic.go`
- Create: `app/logics/admin_audit_logic_test.go`
- Create: `app/handlers/admin_user_handler.go`
- Create: `app/handlers/admin_user_handler_test.go`
- Create: `app/handlers/admin_settings_handler.go`
- Create: `app/handlers/admin_settings_handler_test.go`
- Create: `app/handlers/admin_audit_event_handler.go`
- Create: `app/handlers/admin_audit_event_handler_test.go`
- Modify: `app/router/deps.go`
- Modify: `app/router/init.go`
- Modify: `app/commands/serve.go`
- Test: new handler and logic tests

- [ ] **Step 1: Write failing API tests**

Add tests:

```go
func TestAdminUserHandlerListsAndFiltersUsers(t *testing.T)
func TestAdminUserHandlerUpdatesRoleStatusEmailAndVerification(t *testing.T)
func TestAdminUserHandlerResetsPasswordAndForcesPasswordChange(t *testing.T)
func TestAdminUserHandlerRejectsNonAdmin(t *testing.T)
func TestAdminSettingsHandlerReadsAndUpdatesSMTPTurnstileAndRegistration(t *testing.T)
func TestAdminSettingsHandlerMasksSecrets(t *testing.T)
func TestAdminAuditEventHandlerListsEventsForAdminOnly(t *testing.T)
func TestAdminUserMutationsWriteAuditEvents(t *testing.T)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./app/handlers ./app/logics -run 'Admin(User|Settings|Audit)' -v`

Expected: FAIL because routes and logic do not exist.

- [ ] **Step 3: Implement admin audit logic**

Expose:

```go
func (l *AdminAuditLogic) Record(ctx context.Context, input RecordAdminAuditInput) error
func (l *AdminAuditLogic) List(ctx context.Context, filter AdminAuditFilter) ([]models.AdminAuditEvent, error)
```

Never store plaintext secrets in before/after JSON. Store masked values for SMTP password, Turnstile secret, and API keys.

- [ ] **Step 4: Implement admin user handler**

Routes under `/admin/users`:

- `GET ""`
- `GET "/:id"`
- `POST ""`
- `PATCH "/:id"`
- `POST "/:id/reset-password"`
- `POST "/:id/resend-verification"`

All routes require admin middleware. Mutations write `admin_audit_events`.

- [ ] **Step 5: Implement admin settings and audit handlers**

Routes:

- `GET /admin/settings/smtp`
- `PUT /admin/settings/smtp`
- `POST /admin/settings/smtp/test`
- `GET /admin/settings/turnstile`
- `PUT /admin/settings/turnstile`
- `GET /admin/settings/registration`
- `PUT /admin/settings/registration`
- `GET /admin/audit-events`

All settings mutations write audit events.

- [ ] **Step 6: Wire dependencies and routes**

Update `app/router/deps.go`, `app/router/init.go`, and `app/commands/serve.go` so handlers are constructed once with existing startup pattern.

- [ ] **Step 7: Run tests**

Run: `go test ./app/handlers ./app/logics -run 'Admin(User|Settings|Audit)' -v`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add app/logics/admin_audit_logic.go app/logics/admin_audit_logic_test.go app/handlers/admin_user_handler.go app/handlers/admin_user_handler_test.go app/handlers/admin_settings_handler.go app/handlers/admin_settings_handler_test.go app/handlers/admin_audit_event_handler.go app/handlers/admin_audit_event_handler_test.go app/router/deps.go app/router/init.go app/commands/serve.go
git commit -m "feat: add admin users and settings APIs"
```

### Task 5: Model Store, Resolver, Context Budgets, And Task Enforcement

**Files:**
- Modify: `core/types/provider_config.go`
- Modify: `core/types/provider_config_test.go`
- Create: `app/logics/model_logic.go`
- Create: `app/logics/model_logic_test.go`
- Create: `app/handlers/admin_model_handler.go`
- Create: `app/handlers/admin_model_handler_test.go`
- Modify: `app/handlers/model_catalog_handler.go`
- Modify: `app/handlers/model_catalog_handler_test.go`
- Modify: `app/handlers/task_handler.go`
- Modify: `app/handlers/task_handler_test.go`
- Modify: `core/agent/executor.go`
- Modify: `core/agent/executor_test.go`
- Test: listed tests

- [ ] **Step 1: Write failing model tests**

Add tests:

```go
func TestLLMModelYAMLScopeDefaultsToAdmin(t *testing.T)
func TestModelLogicFiltersCatalogByUserRoleAndOwnership(t *testing.T)
func TestModelLogicCustomModelContextBudgetDefaultsOutputToQuarterCappedAt8192(t *testing.T)
func TestModelLogicRejectsCustomModelContextBelowFour(t *testing.T)
func TestModelLogicMasksCustomModelAPIKey(t *testing.T)
func TestAdminModelHandlerAllowsAdminToTestOtherUserModelAndWritesAudit(t *testing.T)
func TestModelCatalogHandlerReturnsOnlyCurrentUserUsableModels(t *testing.T)
func TestTaskHandlerRejectsUnauthorizedModelSelection(t *testing.T)
func TestAgentExecutorUsesResolvedCustomModelContextBudget(t *testing.T)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/types ./core/agent ./app/logics ./app/handlers -run 'LLMModelYAML|ModelLogic|AdminModel|ModelCatalog|UnauthorizedModel|CustomModelContext' -v`

Expected: FAIL because custom model persistence and user-filtered resolver do not exist.

- [ ] **Step 3: Add YAML model scope and enabled metadata**

Extend `coretypes.LLMModel`:

```go
Scope string `yaml:"scope" json:"scope"`
Enabled *bool `yaml:"enabled" json:"enabled"`
```

Add methods:

```go
func (m *LLMModel) EffectiveScope() string
func (m *LLMModel) IsEnabled() bool
```

`EffectiveScope()` returns `admin` when blank.

- [ ] **Step 4: Implement model logic**

`ModelLogic` must:

- list YAML models with override applied
- update YAML override enabled/scope only
- create/update/delete custom models
- encrypt API keys and return masked API keys
- enforce custom `context_max_tokens >= 4`
- compute context budget:
  - `Max = context_max_tokens`
  - `Output = min(8192, floor(context_max_tokens / 4))`
  - `Input = Max - Output`
- filter catalog for current user:
  - admin sees YAML admin/global and own custom owner/admin/global usable models
  - normal user sees YAML global and own custom owner models
  - admin can view all custom models in admin APIs
  - admin cannot use other users' custom models in `/models` or `/tasks`

- [ ] **Step 5: Implement model handlers**

Admin routes:

- `GET /admin/models`
- `PATCH /admin/models/yaml/:provider_id/:model_id`
- `GET /admin/models/custom`
- `POST /admin/models/custom`
- `PUT /admin/models/custom/:id`
- `DELETE /admin/models/custom/:id`
- `POST /admin/models/custom/:id/test`

User-owned routes may live under `/users/me/models`:

- `GET /users/me/models`
- `POST /users/me/models`
- `PUT /users/me/models/:id`
- `DELETE /users/me/models/:id`
- `POST /users/me/models/:id/test`

- [ ] **Step 6: Enforce resolver in catalog and task creation**

Change `/models` to use `ModelLogic.CatalogForUser(ctx, user)`.

Change task creation to validate `provider_id + model_id` through `ModelLogic.ResolveForUse(ctx, user, providerID, modelID)` before persisting the task. The executor should receive a resolver capable of resolving the already-authorized provider/model including custom model client config.

- [ ] **Step 7: Run tests**

Run: `go test ./core/types ./core/agent ./app/logics ./app/handlers -run 'LLMModelYAML|ModelLogic|AdminModel|ModelCatalog|UnauthorizedModel|CustomModelContext' -v`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add core/types/provider_config.go core/types/provider_config_test.go app/logics/model_logic.go app/logics/model_logic_test.go app/handlers/admin_model_handler.go app/handlers/admin_model_handler_test.go app/handlers/model_catalog_handler.go app/handlers/model_catalog_handler_test.go app/handlers/task_handler.go app/handlers/task_handler_test.go core/agent/executor.go core/agent/executor_test.go
git commit -m "feat: add user-scoped model catalog"
```

### Task 6: Profile APIs And Frontend Session/Auth Flow

**Files:**
- Create: `app/handlers/user_handler.go`
- Create: `app/handlers/user_handler_test.go`
- Modify: `app/router/deps.go`
- Modify: `app/router/init.go`
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/lib/api.spec.ts`
- Modify: `webapp/src/lib/session.ts`
- Modify: `webapp/src/lib/session.spec.ts`
- Create: `webapp/src/lib/user-state.ts`
- Create: `webapp/src/lib/user-state.spec.ts`
- Modify: `webapp/src/router/index.ts`
- Modify: `webapp/src/router/index.spec.ts`
- Modify: `webapp/src/views/LoginView.vue`
- Modify: `webapp/src/views/LoginView.spec.ts`
- Test: listed tests

- [ ] **Step 1: Write failing backend and frontend tests**

Backend:

```go
func TestUserHandlerReturnsProfileAndRequiredActions(t *testing.T)
func TestUserHandlerUpdatesDisplayName(t *testing.T)
func TestUserHandlerChangesPassword(t *testing.T)
func TestUserHandlerStartsAndVerifiesEmailBinding(t *testing.T)
```

Frontend:

```ts
it('normalizes auth user status and required actions')
it('routes force password change users to profile security')
it('routes needs email binding users to profile email')
it('hides register tab when public registration is disabled')
it('submits registration with email and verification flow')
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./app/handlers -run 'UserHandler' -v
pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/session.spec.ts src/lib/user-state.spec.ts src/router/index.spec.ts src/views/LoginView.spec.ts
```

Expected: FAIL because profile APIs and frontend state are missing.

- [ ] **Step 3: Implement `/users/me` APIs**

Routes:

- `GET /users/me`
- `PATCH /users/me`
- `POST /users/me/password`
- `POST /users/me/email-verification`
- `POST /users/me/email-verification/confirm`

Use `RequireSession` for these routes and apply state-specific checks inside logic.

- [ ] **Step 4: Implement frontend session state**

Extend `AuthUser` with:

```ts
email: string
display_name: string
status: 'pending_email_verification' | 'active' | 'disabled' | 'needs_email_binding'
email_verified: boolean
force_password_change: boolean
required_actions: Array<'verify_email' | 'bind_email' | 'change_password'>
```

Router guards send users with required actions to `/profile`.

- [ ] **Step 5: Update LoginView**

Login form label becomes “用户名或邮箱”. Register form includes email. Registration tab is hidden when public registration setting says disabled. Registration success shows verification form instead of immediately logging in, except first admin registration.

- [ ] **Step 6: Run tests**

Run:

```bash
go test ./app/handlers -run 'UserHandler' -v
pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/session.spec.ts src/lib/user-state.spec.ts src/router/index.spec.ts src/views/LoginView.spec.ts
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add app/handlers/user_handler.go app/handlers/user_handler_test.go app/router/deps.go app/router/init.go webapp/src/types/api.ts webapp/src/lib/api.ts webapp/src/lib/api.spec.ts webapp/src/lib/session.ts webapp/src/lib/session.spec.ts webapp/src/lib/user-state.ts webapp/src/lib/user-state.spec.ts webapp/src/router/index.ts webapp/src/router/index.spec.ts webapp/src/views/LoginView.vue webapp/src/views/LoginView.spec.ts
git commit -m "feat: add profile and verified auth UI flow"
```

### Task 7: Admin And Profile Frontend Screens

**Files:**
- Create: `webapp/src/components/AdminLayout.vue`
- Create: `webapp/src/components/AdminLayout.spec.ts`
- Create: `webapp/src/views/ProfileView.vue`
- Create: `webapp/src/views/ProfileView.spec.ts`
- Create: `webapp/src/views/AdminUsersView.vue`
- Create: `webapp/src/views/AdminUsersView.spec.ts`
- Create: `webapp/src/views/AdminSettingsView.vue`
- Create: `webapp/src/views/AdminSettingsView.spec.ts`
- Create: `webapp/src/views/AdminOperationAuditView.vue`
- Create: `webapp/src/views/AdminOperationAuditView.spec.ts`
- Modify: `webapp/src/router/index.ts`
- Modify: `webapp/src/style.css`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/types/api.ts`
- Test: listed frontend tests

- [ ] **Step 1: Write failing view tests**

Add tests:

```ts
it('AdminLayout renders business-domain navigation')
it('ProfileView updates display name and password')
it('ProfileView starts email binding verification')
it('AdminUsersView filters users and updates role status email verification')
it('AdminSettingsView masks SMTP and Turnstile secrets')
it('AdminOperationAuditView lists admin events')
it('router redirects non-admin users away from admin routes')
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/AdminLayout.spec.ts src/views/ProfileView.spec.ts src/views/AdminUsersView.spec.ts src/views/AdminSettingsView.spec.ts src/views/AdminOperationAuditView.spec.ts src/router/index.spec.ts
```

Expected: FAIL because views do not exist.

- [ ] **Step 3: Implement AdminLayout**

Navigation entries:

- 仪表盘
- 用户管理
- 模型管理
- 系统设置
- 提示词管理
- 审计会话
- 后台操作审计

Reuse existing visual language from `AdminPromptView.vue` and `AdminAuditView.vue` without nesting page cards inside cards.

- [ ] **Step 4: Implement ProfileView**

Sections:

- 账号资料: username read-only, display name editable, email status
- 邮箱验证: bind/change email and code confirmation
- 安全: change password and force-password-change banner
- 我的模型 entry linking to `/profile/models` or the model section implemented in Task 8

- [ ] **Step 5: Implement AdminUsersView and AdminSettingsView**

AdminUsersView supports search/filter, user detail drawer, role/status/email verification/password reset actions.

AdminSettingsView supports SMTP, Turnstile, and public registration panels. Secret inputs show masked values and allow replacement.

- [ ] **Step 6: Implement AdminOperationAuditView**

List events with filters for action, target kind, actor, and date range.

- [ ] **Step 7: Run tests**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/AdminLayout.spec.ts src/views/ProfileView.spec.ts src/views/AdminUsersView.spec.ts src/views/AdminSettingsView.spec.ts src/views/AdminOperationAuditView.spec.ts src/router/index.spec.ts
pnpm --dir webapp exec vue-tsc -b
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add webapp/src/components/AdminLayout.vue webapp/src/components/AdminLayout.spec.ts webapp/src/views/ProfileView.vue webapp/src/views/ProfileView.spec.ts webapp/src/views/AdminUsersView.vue webapp/src/views/AdminUsersView.spec.ts webapp/src/views/AdminSettingsView.vue webapp/src/views/AdminSettingsView.spec.ts webapp/src/views/AdminOperationAuditView.vue webapp/src/views/AdminOperationAuditView.spec.ts webapp/src/router/index.ts webapp/src/style.css webapp/src/lib/api.ts webapp/src/types/api.ts
git commit -m "feat: add admin users settings and profile UI"
```

### Task 8: Model Management Frontend And Chat Empty State

**Files:**
- Create: `webapp/src/views/AdminModelsView.vue`
- Create: `webapp/src/views/AdminModelsView.spec.ts`
- Modify: `webapp/src/views/ProfileView.vue`
- Modify: `webapp/src/views/ProfileView.spec.ts`
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/views/ChatView.spec.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/lib/api.spec.ts`
- Modify: `webapp/src/lib/model-selection.ts`
- Modify: `webapp/src/lib/model-selection.spec.ts`
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/router/index.ts`
- Test: listed frontend tests

- [ ] **Step 1: Write failing tests**

Add tests:

```ts
it('normalizes custom model context max tokens and masked api key')
it('AdminModelsView edits YAML scope and enabled state')
it('AdminModelsView creates custom admin/global model with context max')
it('AdminModelsView tests another user model and shows audit warning')
it('ProfileView manages owner-scoped custom models')
it('ChatView shows no-model empty state with profile link')
it('model selection ignores unavailable models')
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/model-selection.spec.ts src/views/AdminModelsView.spec.ts src/views/ProfileView.spec.ts src/views/ChatView.spec.ts
```

Expected: FAIL because model management UI and normalization are missing.

- [ ] **Step 3: Implement API types and helpers**

Add types for:

- `ModelScope = 'owner' | 'admin' | 'global'`
- `CustomLLMModel`
- `CustomLLMModelInput`
- `YAMLModelOverrideInput`

Normalize `context_max_tokens`, `api_key_masked`, `scope`, and `enabled`.

- [ ] **Step 4: Implement AdminModelsView**

Admin screen has:

- YAML configured models table with enabled toggle and scope segmented control
- custom models table with owner, provider type, scope, enabled, context max, masked API key
- create/edit drawer
- test connection action with explicit audit warning for other users' models

- [ ] **Step 5: Implement user custom model UI**

Profile screen allows current user to create/edit/delete/test owner-scoped custom models. The form requires provider type, base URL when needed, model ID, display name, API key on create, and `context_max_tokens`.

- [ ] **Step 6: Implement chat empty state**

When filtered catalog has zero usable models, `ChatView` shows empty state text and links to profile models and admin contact guidance. Sending is disabled.

- [ ] **Step 7: Run tests**

Run:

```bash
pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/model-selection.spec.ts src/views/AdminModelsView.spec.ts src/views/ProfileView.spec.ts src/views/ChatView.spec.ts
pnpm --dir webapp exec vue-tsc -b
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add webapp/src/views/AdminModelsView.vue webapp/src/views/AdminModelsView.spec.ts webapp/src/views/ProfileView.vue webapp/src/views/ProfileView.spec.ts webapp/src/views/ChatView.vue webapp/src/views/ChatView.spec.ts webapp/src/lib/api.ts webapp/src/lib/api.spec.ts webapp/src/lib/model-selection.ts webapp/src/lib/model-selection.spec.ts webapp/src/types/api.ts webapp/src/router/index.ts
git commit -m "feat: add model management UI"
```

### Task 9: Startup Wiring, Swagger, And End-To-End Verification

**Files:**
- Modify: `app/commands/serve.go`
- Modify: `app/commands/serve_test.go`
- Modify: `app/router/deps.go`
- Modify: `app/router/init.go`
- Modify: `docs/swagger/docs.go`
- Modify: `docs/swagger/swagger.json`
- Modify: `docs/swagger/swagger.yaml`
- Test: broad backend and frontend verification

- [ ] **Step 1: Write failing startup tests**

Add tests in `app/commands/serve_test.go`:

```go
func TestBuildRouterDependenciesIncludesPublicAdminBackofficeServices(t *testing.T)
func TestServeConfigRequiresAppSecretWhenRuntimeSettingsNeedSecrets(t *testing.T)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./app/commands -run 'PublicAdminBackoffice|AppSecret' -v`

Expected: FAIL until startup wiring is complete.

- [ ] **Step 3: Complete dependency wiring**

Ensure `Serve` constructs:

- secret codec from `security.appSecret`
- settings logic
- email verification logic with mail sender
- admin audit logic
- model logic
- auth logic with settings, email verification, and Turnstile verifier

Ensure router registers:

- auth
- users/me
- admin/users
- admin/settings
- admin/models
- admin/audit-events
- current filtered models
- tasks with active-user middleware

- [ ] **Step 4: Regenerate Swagger**

Run the repository's Swagger generation command if available. If `swag` is unavailable, document the missing binary in the final report and do not hand-edit generated Swagger files.

Preferred command:

```bash
swag init -g cmd/example_agent/main.go -o docs/swagger
```

- [ ] **Step 5: Run full backend verification**

Run:

```bash
go test ./...
go build ./cmd/...
go list ./...
```

Expected: PASS.

- [ ] **Step 6: Run full frontend verification**

Run:

```bash
pnpm --dir webapp exec vue-tsc -b
pnpm --dir webapp exec vitest run
```

Expected: PASS. If Node version is below Vite/Vitest requirements, report exact Node version and required version instead of hiding the failure.

- [ ] **Step 7: Commit**

```bash
git add app/commands/serve.go app/commands/serve_test.go app/router/deps.go app/router/init.go docs/swagger/docs.go docs/swagger/swagger.json docs/swagger/swagger.yaml
git commit -m "chore: wire public admin backoffice runtime"
```

## Plan Self-Review

Spec coverage:

- Email registration and verification: Tasks 3 and 6.
- Turnstile protection: Tasks 2, 3, and 7.
- Admin user management: Tasks 4 and 7.
- User profile and editable display name/password/email: Tasks 6 and 7.
- SMTP settings UI and DB override: Tasks 2, 4, and 7.
- Model scopes, custom models, admin visibility/use restrictions: Tasks 5 and 8.
- Custom model `context_max_tokens` and dynamic input/output budgets: Task 5 and Task 8.
- `APP_SECRET` encryption: Task 2 and Task 9.
- Admin operation audit: Task 4, Task 5, and Task 7.
- `/models` and `/tasks` server-side enforcement: Task 5 and Task 9.
- Swagger and full verification: Task 9.

Placeholder scan:

- This plan intentionally uses exact task names, file paths, commands, and expected outcomes.
- No deferred feature is required for the accepted first-stage spec.

Type consistency:

- User statuses match `UserStatusPendingEmailVerification`, `UserStatusActive`, `UserStatusDisabled`, and `UserStatusNeedsEmailBinding`.
- Model scopes match `owner`, `admin`, and `global`.
- Custom model context uses `context_max_tokens` in DB/API and maps to `LLMContextConfig.Max/Input/Output` at runtime.
