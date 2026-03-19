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
	Reasoning  string `json:"reasoning"`
	ToolCallID string `json:"tool_call_id"`
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

// ExampleSayHelloSwaggerResponse 描述示例接口的成功响应结构。
type ExampleSayHelloSwaggerResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
	OK      bool   `json:"ok"`
	Time    string `json:"time"`
}
