package handlers

// ConversationSwaggerDoc 描述会话元数据的文档结构。
type ConversationSwaggerDoc struct {
	ID            string `json:"id"`
	ProviderID    string `json:"provider_id"`
	ModelID       string `json:"model_id"`
	Title         string `json:"title"`
	LastMessage   string `json:"last_message"`
	MessageCount  int    `json:"message_count"`
	CreatedBy     string `json:"created_by"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	LastMessageAt string `json:"last_message_at"`
}

// ConversationDetailSwaggerResponse 描述会话详情接口的返回结构。
type ConversationDetailSwaggerResponse struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    ConversationSwaggerDoc `json:"data"`
	OK      bool                   `json:"ok"`
	Time    string                 `json:"time"`
}

// ConversationListSwaggerResponse 描述会话列表接口的返回结构。
type ConversationListSwaggerResponse struct {
	Code    int                      `json:"code"`
	Message string                   `json:"message"`
	Data    []ConversationSwaggerDoc `json:"data"`
	OK      bool                     `json:"ok"`
	Time    string                   `json:"time"`
}

// ConversationMessagesSwaggerResponse 描述会话消息列表接口的返回结构。
type ConversationMessagesSwaggerResponse struct {
	Code    int                      `json:"code"`
	Message string                   `json:"message"`
	Data    []ConversationMessageDoc `json:"data"`
	OK      bool                     `json:"ok"`
	Time    string                   `json:"time"`
}

// ConversationDeleteSwaggerResponse 描述删除会话接口的返回结构。
type ConversationDeleteSwaggerResponse struct {
	Code    int                           `json:"code"`
	Message string                        `json:"message"`
	Data    ConversationDeleteSwaggerData `json:"data"`
	OK      bool                          `json:"ok"`
	Time    string                        `json:"time"`
}

// ConversationDeleteSwaggerData 描述删除会话结果。
type ConversationDeleteSwaggerData struct {
	Deleted bool `json:"deleted"`
}

// ConversationMessageDoc 描述会话消息列表的文档结构。
type ConversationMessageDoc struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
	Reasoning  string `json:"reasoning"`
	ToolCallID string `json:"tool_call_id"`
}

// ModelCatalogSwaggerResponse 描述模型目录接口的响应结构。
type ModelCatalogSwaggerResponse struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    ModelCatalogSwaggerDoc `json:"data"`
	OK      bool                   `json:"ok"`
	Time    string                 `json:"time"`
}

// ModelCatalogSwaggerDoc 描述前端模型选择所需的 provider/model 目录。
type ModelCatalogSwaggerDoc struct {
	DefaultProviderID string                    `json:"default_provider_id"`
	DefaultModelID    string                    `json:"default_model_id"`
	Providers         []ModelProviderSwaggerDoc `json:"providers"`
}

// ModelProviderSwaggerDoc 描述一个 provider 及其模型列表。
type ModelProviderSwaggerDoc struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	Models []ModelEntrySwaggerDoc `json:"models"`
}

// ModelEntrySwaggerDoc 描述一个模型选项。
type ModelEntrySwaggerDoc struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ErrorSwaggerResponse 描述通用失败响应结构。
type ErrorSwaggerResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
	OK      bool   `json:"ok"`
	Time    string `json:"time"`
}

// AuthUserSwaggerDoc 描述登录态用户信息的文档结构。
type AuthUserSwaggerDoc struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// AuthUserSwaggerResponse 描述登录、注册、当前用户接口的成功响应结构。
type AuthUserSwaggerResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    AuthUserSwaggerDoc `json:"data"`
	OK      bool               `json:"ok"`
	Time    string             `json:"time"`
}

// AuthLogoutSwaggerResponse 描述退出登录接口的成功响应结构。
type AuthLogoutSwaggerResponse struct {
	Code    int                   `json:"code"`
	Message string                `json:"message"`
	Data    AuthLogoutSwaggerData `json:"data"`
	OK      bool                  `json:"ok"`
	Time    string                `json:"time"`
}

// AuthLogoutSwaggerData 描述退出登录结果。
type AuthLogoutSwaggerData struct {
	LoggedOut bool `json:"logged_out"`
}

// AuthRegisterSwaggerRequest 描述注册接口请求结构。
type AuthRegisterSwaggerRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}

// AuthLoginSwaggerRequest 描述登录接口请求结构。
type AuthLoginSwaggerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// TaskSwaggerResponse 描述任务接口的成功响应结构。
type TaskSwaggerResponse struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    TaskSwaggerDoc `json:"data"`
	OK      bool           `json:"ok"`
	Time    string         `json:"time"`
}

// TaskSwaggerDoc 描述任务快照的文档结构。
type TaskSwaggerDoc struct {
	ID               string `json:"id"`
	TaskType         string `json:"task_type"`
	Status           string `json:"status"`
	CurrentStepKey   string `json:"current_step_key"`
	CurrentStepTitle string `json:"current_step_title"`
	CreatedBy        string `json:"created_by"`
	RetryOfTaskID    string `json:"retry_of_task_id"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// AuditRunSwaggerResponse 描述审计运行详情接口的成功响应结构。
type AuditRunSwaggerResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    AuditRunSwaggerDoc `json:"data"`
	OK      bool               `json:"ok"`
	Time    string             `json:"time"`
}

// AuditRunSwaggerDoc 描述审计运行的文档结构。
type AuditRunSwaggerDoc struct {
	ID             string `json:"id"`
	TaskID         string `json:"task_id"`
	ConversationID string `json:"conversation_id"`
	TaskType       string `json:"task_type"`
	ProviderID     string `json:"provider_id"`
	ModelID        string `json:"model_id"`
	RunnerID       string `json:"runner_id"`
	Status         string `json:"status" enums:"queued,running,cancel_requested,cancelled,succeeded,failed"`
	CreatedBy      string `json:"created_by"`
	Replayable     bool   `json:"replayable"`
	SchemaVersion  string `json:"schema_version"`
	StartedAt      string `json:"started_at"`
	FinishedAt     string `json:"finished_at"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// AuditEventsSwaggerResponse 描述审计事件列表接口的成功响应结构。
type AuditEventsSwaggerResponse struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    []AuditEventSwaggerDoc `json:"data"`
	OK      bool                   `json:"ok"`
	Time    string                 `json:"time"`
}

// AuditEventSwaggerDoc 描述审计事件的文档结构。
type AuditEventSwaggerDoc struct {
	ID            uint64 `json:"id"`
	RunID         string `json:"run_id"`
	TaskID        string `json:"task_id"`
	Seq           int64  `json:"seq"`
	Phase         string `json:"phase"`
	EventType     string `json:"event_type"`
	Level         string `json:"level"`
	StepIndex     int    `json:"step_index"`
	ParentSeq     int64  `json:"parent_seq"`
	RefArtifactID string `json:"ref_artifact_id"`
	Payload       any    `json:"payload"`
	CreatedAt     string `json:"created_at"`
}

// AuditReplaySwaggerResponse 描述审计回放包接口的成功响应结构。
type AuditReplaySwaggerResponse struct {
	Code    int                         `json:"code"`
	Message string                      `json:"message"`
	Data    AuditReplayBundleSwaggerDoc `json:"data"`
	OK      bool                        `json:"ok"`
	Time    string                      `json:"time"`
}

// AuditReplayBundleSwaggerDoc 描述审计回放包的文档结构。
type AuditReplayBundleSwaggerDoc struct {
	Run       AuditRunSwaggerDoc              `json:"run"`
	Timeline  []AuditReplayEventSwaggerDoc    `json:"timeline"`
	Artifacts []AuditReplayArtifactSwaggerDoc `json:"artifacts"`
}

// AuditReplayEventSwaggerDoc 描述回放时间线事件的文档结构。
type AuditReplayEventSwaggerDoc struct {
	Seq       int64                                 `json:"seq"`
	Phase     string                                `json:"phase"`
	EventType string                                `json:"event_type"`
	Level     string                                `json:"level"`
	StepIndex int                                   `json:"step_index"`
	ParentSeq int64                                 `json:"parent_seq"`
	CreatedAt string                                `json:"created_at"`
	Payload   any                                   `json:"payload"`
	Artifact  *AuditReplayArtifactSummarySwaggerDoc `json:"artifact"`
}

// AuditReplayArtifactSummarySwaggerDoc 描述回放时间线中引用的工件摘要。
type AuditReplayArtifactSummarySwaggerDoc struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	MimeType       string `json:"mime_type"`
	Encoding       string `json:"encoding"`
	SizeBytes      int64  `json:"size_bytes"`
	SHA256         string `json:"sha256"`
	RedactionState string `json:"redaction_state"`
	CreatedAt      string `json:"created_at"`
}

// AuditReplayArtifactSwaggerDoc 描述回放包中保留的工件结构。
type AuditReplayArtifactSwaggerDoc struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	MimeType       string `json:"mime_type"`
	Encoding       string `json:"encoding"`
	SizeBytes      int64  `json:"size_bytes"`
	SHA256         string `json:"sha256"`
	RedactionState string `json:"redaction_state"`
	CreatedAt      string `json:"created_at"`
	Body           any    `json:"body"`
}

// ExampleSayHelloSwaggerResponse 描述示例接口的成功响应结构。
type ExampleSayHelloSwaggerResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
	OK      bool   `json:"ok"`
	Time    string `json:"time"`
}
