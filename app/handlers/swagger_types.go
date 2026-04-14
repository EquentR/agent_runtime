package handlers

// ConversationSwaggerDoc 描述会话元数据的文档结构。
type ConversationSwaggerDoc struct {
	ID                string                       `json:"id"`
	ProviderID        string                       `json:"provider_id"`
	ModelID           string                       `json:"model_id"`
	Title             string                       `json:"title"`
	LastMessage       string                       `json:"last_message"`
	MessageCount      int                          `json:"message_count"`
	CreatedBy         string                       `json:"created_by"`
	CreatedAt         string                       `json:"created_at"`
	UpdatedAt         string                       `json:"updated_at"`
	LastMessageAt     string                       `json:"last_message_at"`
	MemoryContext     *MemoryContextSwaggerDoc     `json:"memory_context,omitempty"`
	MemoryCompression *MemoryCompressionSwaggerDoc `json:"memory_compression,omitempty"`
}

// MemoryContextSwaggerDoc 描述权威记忆上下文快照。
type MemoryContextSwaggerDoc struct {
	ShortTermTokens       int64 `json:"short_term_tokens"`
	SummaryTokens         int64 `json:"summary_tokens"`
	RenderedSummaryTokens int64 `json:"rendered_summary_tokens"`
	TotalTokens           int64 `json:"total_tokens"`
	ShortTermLimit        int64 `json:"short_term_limit"`
	SummaryLimit          int64 `json:"summary_limit"`
	MaxContextTokens      int64 `json:"max_context_tokens"`
	HasSummary            bool  `json:"has_summary"`
}

// MemoryCompressionSwaggerDoc 描述最近一次记忆压缩快照。
type MemoryCompressionSwaggerDoc struct {
	TokensBefore                int64 `json:"tokens_before"`
	TokensAfter                 int64 `json:"tokens_after"`
	ShortTermTokensBefore       int64 `json:"short_term_tokens_before"`
	ShortTermTokensAfter        int64 `json:"short_term_tokens_after"`
	SummaryTokensBefore         int64 `json:"summary_tokens_before"`
	SummaryTokensAfter          int64 `json:"summary_tokens_after"`
	RenderedSummaryTokensBefore int64 `json:"rendered_summary_tokens_before"`
	RenderedSummaryTokensAfter  int64 `json:"rendered_summary_tokens_after"`
	TotalTokensBefore           int64 `json:"total_tokens_before"`
	TotalTokensAfter            int64 `json:"total_tokens_after"`
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

// AttachmentSwaggerDoc 描述附件元数据的文档结构。
type AttachmentSwaggerDoc struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id,omitempty"`
	FileName       string `json:"file_name"`
	MimeType       string `json:"mime_type"`
	SizeBytes      int64  `json:"size_bytes"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	PreviewText    string `json:"preview_text,omitempty"`
	Width          *int   `json:"width,omitempty"`
	Height         *int   `json:"height,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// AttachmentSwaggerResponse 描述附件元数据接口的成功响应结构。
type AttachmentSwaggerResponse struct {
	Code    int                  `json:"code"`
	Message string               `json:"message"`
	Data    AttachmentSwaggerDoc `json:"data"`
	OK      bool                 `json:"ok"`
	Time    string               `json:"time"`
}

// AttachmentDeleteSwaggerResponse 描述删除附件接口的成功响应结构。
type AttachmentDeleteSwaggerResponse struct {
	Code    int                         `json:"code"`
	Message string                      `json:"message"`
	Data    AttachmentDeleteSwaggerData `json:"data"`
	OK      bool                        `json:"ok"`
	Time    string                      `json:"time"`
}

// AttachmentDeleteSwaggerData 描述删除附件结果。
type AttachmentDeleteSwaggerData struct {
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
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	Type         string                       `json:"type"`
	Context      *ModelContextSwaggerDoc      `json:"context,omitempty"`
	Cost         *ModelPricingSwaggerDoc      `json:"cost,omitempty"`
	Capabilities *ModelCapabilitiesSwaggerDoc `json:"capabilities,omitempty"`
}

// ModelCapabilitiesSwaggerDoc 描述模型能力开关。
type ModelCapabilitiesSwaggerDoc struct {
	Attachments bool `json:"attachments"`
}

// ModelContextSwaggerDoc 描述模型的上下文窗口信息。
type ModelContextSwaggerDoc struct {
	Max    int64 `json:"max"`
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
}

// ModelPricingSwaggerDoc 描述模型定价信息。
type ModelPricingSwaggerDoc struct {
	Input       TokenPriceSwaggerDoc  `json:"input"`
	Output      TokenPriceSwaggerDoc  `json:"output"`
	CachedInput *TokenPriceSwaggerDoc `json:"cached_input,omitempty"`
}

// TokenPriceSwaggerDoc 描述单种 token 价格。
type TokenPriceSwaggerDoc struct {
	AmountUSD float64 `json:"amount_usd"`
	PerTokens int64   `json:"per_tokens"`
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

// PromptDocumentSwaggerDoc 描述提示词文档的文档结构。
type PromptDocumentSwaggerDoc struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Scope       string `json:"scope"`
	Status      string `json:"status"`
	CreatedBy   string `json:"created_by"`
	UpdatedBy   string `json:"updated_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// PromptDocumentSwaggerResponse 描述提示词文档详情接口的成功响应结构。
type PromptDocumentSwaggerResponse struct {
	Code    int                      `json:"code"`
	Message string                   `json:"message"`
	Data    PromptDocumentSwaggerDoc `json:"data"`
	OK      bool                     `json:"ok"`
	Time    string                   `json:"time"`
}

// PromptDocumentListSwaggerResponse 描述提示词文档列表接口的成功响应结构。
type PromptDocumentListSwaggerResponse struct {
	Code    int                        `json:"code"`
	Message string                     `json:"message"`
	Data    []PromptDocumentSwaggerDoc `json:"data"`
	OK      bool                       `json:"ok"`
	Time    string                     `json:"time"`
}

// PromptDocumentCreateSwaggerRequest 描述创建提示词文档接口请求结构。
type PromptDocumentCreateSwaggerRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Scope       string `json:"scope"`
	Status      string `json:"status"`
}

// PromptDocumentUpdateSwaggerRequest 描述更新提示词文档接口请求结构。
type PromptDocumentUpdateSwaggerRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Content     *string `json:"content"`
	Scope       *string `json:"scope"`
	Status      *string `json:"status"`
}

// PromptBindingSwaggerDoc 描述提示词绑定的文档结构。
type PromptBindingSwaggerDoc struct {
	ID         uint64 `json:"id"`
	PromptID   string `json:"prompt_id"`
	Scene      string `json:"scene"`
	Phase      string `json:"phase"`
	IsDefault  bool   `json:"is_default"`
	Priority   int    `json:"priority"`
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
	Status     string `json:"status"`
	CreatedBy  string `json:"created_by"`
	UpdatedBy  string `json:"updated_by"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// PromptBindingSwaggerResponse 描述提示词绑定详情接口的成功响应结构。
type PromptBindingSwaggerResponse struct {
	Code    int                     `json:"code"`
	Message string                  `json:"message"`
	Data    PromptBindingSwaggerDoc `json:"data"`
	OK      bool                    `json:"ok"`
	Time    string                  `json:"time"`
}

// PromptBindingListSwaggerResponse 描述提示词绑定列表接口的成功响应结构。
type PromptBindingListSwaggerResponse struct {
	Code    int                       `json:"code"`
	Message string                    `json:"message"`
	Data    []PromptBindingSwaggerDoc `json:"data"`
	OK      bool                      `json:"ok"`
	Time    string                    `json:"time"`
}

// PromptBindingCreateSwaggerRequest 描述创建提示词绑定接口请求结构。
type PromptBindingCreateSwaggerRequest struct {
	PromptID   string `json:"prompt_id"`
	Scene      string `json:"scene"`
	Phase      string `json:"phase"`
	IsDefault  bool   `json:"is_default"`
	Priority   int    `json:"priority"`
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
	Status     string `json:"status"`
}

// PromptBindingUpdateSwaggerRequest 描述更新提示词绑定接口请求结构。
type PromptBindingUpdateSwaggerRequest struct {
	PromptID   *string `json:"prompt_id"`
	Scene      *string `json:"scene"`
	Phase      *string `json:"phase"`
	IsDefault  *bool   `json:"is_default"`
	Priority   *int    `json:"priority"`
	ProviderID *string `json:"provider_id"`
	ModelID    *string `json:"model_id"`
	Status     *string `json:"status"`
}

// PromptDeleteSwaggerResponse 描述删除提示词绑定接口的成功响应结构。
type PromptDeleteSwaggerResponse struct {
	Code    int                     `json:"code"`
	Message string                  `json:"message"`
	Data    PromptDeleteSwaggerData `json:"data"`
	OK      bool                    `json:"ok"`
	Time    string                  `json:"time"`
}

// PromptDeleteSwaggerData 描述删除提示词绑定结果。
type PromptDeleteSwaggerData struct {
	Deleted bool `json:"deleted"`
}

// SkillSwaggerResponse 描述技能详情接口的成功响应结构。
type SkillSwaggerResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    SkillSwaggerDoc `json:"data"`
	OK      bool            `json:"ok"`
	Time    string          `json:"time"`
}

// SkillListSwaggerResponse 描述技能列表接口的成功响应结构。
type SkillListSwaggerResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    []SkillListItemDoc `json:"data"`
	OK      bool               `json:"ok"`
	Time    string             `json:"time"`
}

// SkillListItemDoc 描述技能列表项的文档结构。
type SkillListItemDoc struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Version     string   `json:"version,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
	SourceRef   string   `json:"source_ref"`
}

// SkillSwaggerDoc 描述 workspace skill 详情的文档结构。
type SkillSwaggerDoc struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Version      string   `json:"version,omitempty"`
	Hidden       bool     `json:"hidden,omitempty"`
	SourceRef    string   `json:"source_ref"`
	Content      string   `json:"content,omitempty"`
	ResourceRefs []string `json:"resource_refs,omitempty"`
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
	ID               string                   `json:"id"`
	TaskType         string                   `json:"task_type"`
	Status           string                   `json:"status" enums:"queued,running,waiting,cancel_requested,cancelled,succeeded,failed"`
	CurrentStepKey   string                   `json:"current_step_key"`
	CurrentStepTitle string                   `json:"current_step_title"`
	CreatedBy        string                   `json:"created_by"`
	RetryOfTaskID    string                   `json:"retry_of_task_id"`
	CreatedAt        string                   `json:"created_at"`
	UpdatedAt        string                   `json:"updated_at"`
	Result           *RunTaskResultSwaggerDoc `json:"result,omitempty"`
}

// RunTaskResultSwaggerDoc 描述 agent.run 成功结果中的结构化记忆快照。
type RunTaskResultSwaggerDoc struct {
	ConversationID    string                       `json:"conversation_id"`
	ProviderID        string                       `json:"provider_id"`
	ModelID           string                       `json:"model_id"`
	FinalMessage      ConversationMessageDoc       `json:"final_message"`
	MessagesAppended  int                          `json:"messages_appended"`
	MemoryContext     *MemoryContextSwaggerDoc     `json:"memory_context,omitempty"`
	MemoryCompression *MemoryCompressionSwaggerDoc `json:"memory_compression,omitempty"`
}

// ApprovalSwaggerDoc 描述工具审批记录的文档结构。
type ApprovalSwaggerDoc struct {
	ID               string `json:"id"`
	TaskID           string `json:"task_id"`
	ConversationID   string `json:"conversation_id"`
	StepIndex        int    `json:"step_index"`
	ToolCallID       string `json:"tool_call_id"`
	ToolName         string `json:"tool_name"`
	ArgumentsSummary string `json:"arguments_summary"`
	RiskLevel        string `json:"risk_level"`
	Reason           string `json:"reason"`
	Status           string `json:"status" enums:"pending,approved,rejected,expired,cancelled"`
	DecisionBy       string `json:"decision_by"`
	DecisionReason   string `json:"decision_reason"`
	DecisionAt       string `json:"decision_at"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// ApprovalListSwaggerResponse 描述审批列表接口的成功响应结构。
type ApprovalListSwaggerResponse struct {
	Code    int                  `json:"code"`
	Message string               `json:"message"`
	Data    []ApprovalSwaggerDoc `json:"data"`
	OK      bool                 `json:"ok"`
	Time    string               `json:"time"`
}

// ApprovalSwaggerResponse 描述审批详情接口的成功响应结构。
type ApprovalSwaggerResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    ApprovalSwaggerDoc `json:"data"`
	OK      bool               `json:"ok"`
	Time    string             `json:"time"`
}

// ApprovalDecisionSwaggerRequest 描述审批决策请求结构。
type ApprovalDecisionSwaggerRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
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
	Status         string `json:"status" enums:"queued,running,waiting,cancel_requested,cancelled,succeeded,failed"`
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

// AuditRunListSwaggerResponse 描述按会话查询审计运行列表接口的成功响应结构。
type AuditRunListSwaggerResponse struct {
	Code    int                  `json:"code"`
	Message string               `json:"message"`
	Data    []AuditRunSwaggerDoc `json:"data"`
	OK      bool                 `json:"ok"`
	Time    string               `json:"time"`
}

// AuditEventListSwaggerResponse 描述按会话查询审计事件列表接口的成功响应结构。
type AuditEventListSwaggerResponse struct {
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
	Seq         int64                                 `json:"seq"`
	Phase       string                                `json:"phase"`
	EventType   string                                `json:"event_type"`
	DisplayName string                                `json:"display_name"`
	Level       string                                `json:"level"`
	StepIndex   int                                   `json:"step_index"`
	ParentSeq   int64                                 `json:"parent_seq"`
	CreatedAt   string                                `json:"created_at"`
	Payload     any                                   `json:"payload"`
	Artifact    *AuditReplayArtifactSummarySwaggerDoc `json:"artifact"`
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
