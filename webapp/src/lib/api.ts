import type {
  ApprovalDecision,
  AdminAuditEvent,
  AdminAuditEventFilter,
  AdminPasswordResetInput,
  AdminRegistrationSettings,
  AdminSMTPSettings,
  AdminSMTPSettingsInput,
  AdminSMTPTestInput,
  AdminTurnstileSettings,
  AdminTurnstileSettingsInput,
  AdminUserFilter,
  AdminUserUpdateInput,
  ApiEnvelope,
  AuditEvent,
  AuditReplayBundle,
  AuditRun,
  AttachmentRef,
  AuthRequiredAction,
  AuthUserStatus,
  AuthUser,
  ChangeUserPasswordInput,
  Conversation,
  ConversationMessage,
  CustomLLMModel,
  CustomLLMModelDeleteResult,
  CustomLLMModelInput,
  EmailVerificationSentResult,
  ModelScope,
  ModelTestResult,
  ModelCatalog,
  ModelCatalogEntry,
  ModelCatalogProvider,
  CreatePromptDocumentInput,
  MemoryCompression,
  MemoryContextState,
  PromptBinding,
  PromptBindingInput,
  PromptDeleteResult,
  PromptDocument,
  PublicRegistrationSettings,
  PublicTurnstileSettings,
  TaskDetails,
  TaskWorkspaceState,
  TaskWorkspaceStateStatus,
  RunTaskRequest,
  RunTaskResult,
  TaskStreamEvent,
  TaskSnapshot,
  ToolApproval,
  ToolApprovalDecisionInput,
  InteractionRecord,
  TranscriptTokenUsage,
  UpdatePromptDocumentInput,
  UpdateUserProfileInput,
  UserEmailVerificationConfirmInput,
  UserEmailVerificationStartInput,
  UserRole,
  WorkspaceSkill,
  WorkspaceSkillListItem,
  WorkspaceMode,
  WorkspaceState,
  YAMLModel,
  YAMLModelCatalog,
  YAMLModelOverrideInput,
} from '../types/api'

const API_BASE = '/api/v1'
const POLL_INTERVAL_MS = 1200
const POLL_TIMEOUT_MS = 90000
export const TASK_STREAM_ABORTED_MESSAGE = 'Task event stream aborted'

export function unwrapEnvelope<T>(envelope: ApiEnvelope<T>) {
  if (!envelope.ok) {
    throw new Error(envelope.message || 'Request failed')
  }

  return envelope.data
}

export async function requestJSON<T>(basePath: string, path: string, init?: RequestInit) {
  const response = await fetch(`${basePath}${path}`, {
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
    ...init,
  })

  const payload = (await response.json()) as ApiEnvelope<T>
  return unwrapEnvelope(payload)
}

function normalizeRole(value: unknown): UserRole {
  return value === 'admin' ? 'admin' : 'user'
}

function normalizeAuthUserStatus(value: unknown): AuthUserStatus {
  switch (value) {
    case 'pending_email_verification':
    case 'disabled':
    case 'needs_email_binding':
      return value
    default:
      return 'active'
  }
}

function normalizeAuthRequiredAction(value: unknown): AuthRequiredAction | undefined {
  switch (value) {
    case 'verify_email':
    case 'bind_email':
    case 'change_password':
      return value
    default:
      return undefined
  }
}

interface AuthUserPayload extends Omit<Partial<AuthUser>, 'required_actions'> {
  displayName?: unknown
  emailVerifiedAt?: unknown
  email_verified_at?: unknown
  required_actions?: unknown
}

function normalizeAuthRequiredActions(raw: Pick<AuthUserPayload, 'required_actions'>, fallback: {
  email: string
  status: AuthUserStatus
  emailVerified: boolean
  forcePasswordChange: boolean
}) {
  if (Array.isArray(raw.required_actions)) {
    return raw.required_actions.flatMap((item) => {
      const action = normalizeAuthRequiredAction(item)
      return action ? [action] : []
    })
  }

  const actions: AuthRequiredAction[] = []
  if (fallback.status === 'pending_email_verification') {
    actions.push('verify_email')
  }
  if (fallback.status === 'needs_email_binding' || !fallback.email.trim()) {
    actions.push('bind_email')
  }
  if (fallback.forcePasswordChange) {
    actions.push('change_password')
  }
  return actions
}

export function normalizeAuthUser(user: AuthUserPayload): AuthUser {
  const username = typeof user.username === 'string' ? user.username.trim() : ''
  const email = normalizeStringValue(user.email) ?? ''
  const displayName = normalizeStringValue(user.display_name) ?? normalizeStringValue(user.displayName) ?? username
  const status = normalizeAuthUserStatus(user.status)
  const emailVerifiedAt = user.email_verified_at ?? user.emailVerifiedAt
  const emailVerified = normalizeBooleanValue(user.email_verified) ?? Boolean(emailVerifiedAt)
  const forcePasswordChange = normalizeBooleanValue(user.force_password_change) ?? false

  return {
    id: typeof user.id === 'number' && Number.isFinite(user.id) ? user.id : 0,
    username,
    email,
    display_name: displayName || username,
    role: normalizeRole(user.role),
    status,
    email_verified: emailVerified,
    force_password_change: forcePasswordChange,
    required_actions: normalizeAuthRequiredActions(user, {
      email,
      status,
      emailVerified,
      forcePasswordChange,
    }),
  }
}

function normalizeAdminSMTPSettings(value: Partial<AdminSMTPSettings> & Record<string, unknown>): AdminSMTPSettings {
  return {
    enabled: normalizeBooleanValue(value.enabled) ?? false,
    host: normalizeFirstStringValue(value.host),
    port: normalizeIntegerValue(value.port) ?? 0,
    username: normalizeFirstStringValue(value.username),
    password: normalizeFirstStringValue(value.password),
    password_masked: normalizeFirstStringValue(value.password_masked),
    from: normalizeFirstStringValue(value.from),
    use_tls: normalizeBooleanValue(value.use_tls) ?? false,
    use_start_tls: normalizeBooleanValue(value.use_start_tls) ?? false,
  }
}

function normalizeAdminTurnstileSettings(value: Partial<AdminTurnstileSettings> & Record<string, unknown>): AdminTurnstileSettings {
  return {
    enabled: normalizeBooleanValue(value.enabled) ?? false,
    site_key: normalizeFirstStringValue(value.site_key),
    secret: normalizeFirstStringValue(value.secret),
    secret_masked: normalizeFirstStringValue(value.secret_masked),
    protect_login: normalizeBooleanValue(value.protect_login) ?? false,
    protect_registration: normalizeBooleanValue(value.protect_registration) ?? false,
    protect_verification: normalizeBooleanValue(value.protect_verification) ?? false,
  }
}

function normalizeAdminAuditEvent(value: Partial<AdminAuditEvent> & Record<string, unknown>): AdminAuditEvent {
  return {
    id: normalizeIntegerValue(value.id) ?? 0,
    actor_id: normalizeIntegerValue(value.actor_id) ?? 0,
    actor_username: normalizeFirstStringValue(value.actor_username),
    actor_email: normalizeFirstStringValue(value.actor_email),
    target_kind: normalizeFirstStringValue(value.target_kind),
    target_id: normalizeFirstStringValue(value.target_id),
    action: normalizeFirstStringValue(value.action),
    before_json: value.before_json,
    after_json: value.after_json,
    ip_address: normalizeFirstStringValue(value.ip_address),
    user_agent: normalizeFirstStringValue(value.user_agent),
    created_at: normalizeFirstStringValue(value.created_at),
  }
}

function appendQueryParam(params: URLSearchParams, key: string, value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) {
    params.set(key, String(value))
    return
  }
  const normalized = normalizeStringValue(value)
  if (normalized !== undefined) {
    params.set(key, normalized)
  }
}

function formatQuery(params: URLSearchParams) {
  const encoded = params.toString()
  return encoded ? `?${encoded}` : ''
}

export function buildRunTaskRequest(input: {
  createdBy: string
  conversationId?: string
  providerId: string
  modelId: string
  message: string
  attachmentIds?: string[]
  skills?: string[]
  workspaceMode?: WorkspaceMode
}): RunTaskRequest {
  const request: RunTaskRequest = {
    task_type: 'agent.run',
    created_by: input.createdBy,
    input: {
      provider_id: input.providerId,
      model_id: input.modelId,
      message: input.message,
      created_by: input.createdBy,
    },
  }

  if (input.conversationId) {
    request.input.conversation_id = input.conversationId
  }
  if (input.skills && input.skills.length > 0) {
    request.input.skills = input.skills
  }
  if (input.attachmentIds && input.attachmentIds.length > 0) {
    request.input.attachment_ids = input.attachmentIds
  }
  if (input.workspaceMode) {
    request.input.workspace_mode = input.workspaceMode
  }

  return request
}

function normalizeConversationRole(...values: unknown[]): ConversationMessage['role'] {
  for (const value of values) {
    if (value === 'user' || value === 'assistant' || value === 'tool' || value === 'system') {
      return value
    }
  }

  return 'assistant'
}

function normalizeSafeStringValue(value: unknown) {
  if (typeof value === 'string') {
    return value
  }

  if (typeof value === 'number' && Number.isFinite(value)) {
    return String(value)
  }

  if (typeof value === 'boolean' || typeof value === 'bigint') {
    return String(value)
  }

  return undefined
}

function normalizeStringValue(value: unknown) {
  const normalized = normalizeSafeStringValue(value)
  return normalized && normalized.trim() ? normalized.trim() : undefined
}

function normalizeFirstStringValue(...values: unknown[]) {
  for (const value of values) {
    const normalized = normalizeSafeStringValue(value)
    if (normalized !== undefined) {
      return normalized
    }
  }

  return ''
}

function normalizeFirstOptionalStringValue(...values: unknown[]) {
  for (const value of values) {
    const normalized = normalizeStringValue(value)
    if (normalized !== undefined) {
      return normalized
    }
  }

  return undefined
}

function normalizeConversationToolCalls(...values: unknown[]): ConversationMessage['tool_calls'] | undefined {
  for (const value of values) {
    if (!Array.isArray(value)) {
      continue
    }

    return value.map((toolCall) => {
      const raw = toolCall && typeof toolCall === 'object' ? (toolCall as Record<string, unknown>) : {}
      return {
        id: normalizeFirstStringValue(raw.id, raw.ID),
        name: normalizeFirstStringValue(raw.name, raw.Name),
        arguments: normalizeFirstStringValue(raw.arguments, raw.Arguments),
      }
    })
  }

  return undefined
}

function normalizeAttachmentRef(
  attachment: Partial<AttachmentRef> & {
    ID?: string
    FileName?: string
    MimeType?: string
    SizeBytes?: number | string
    Kind?: string
    Status?: string
    PreviewText?: string
    ContextText?: string
    Width?: number | string
    Height?: number | string
    ExpiresAt?: string
  },
): AttachmentRef {
  return {
    id: normalizeFirstStringValue(attachment.id, attachment.ID),
    file_name: normalizeFirstStringValue(attachment.file_name, attachment.FileName),
    mime_type: normalizeFirstStringValue(attachment.mime_type, attachment.MimeType),
    size_bytes: normalizeIntegerValue(attachment.size_bytes ?? attachment.SizeBytes),
    kind: normalizeFirstOptionalStringValue(attachment.kind, attachment.Kind),
    status: normalizeFirstOptionalStringValue(attachment.status, attachment.Status),
    preview_text: normalizeFirstOptionalStringValue(attachment.preview_text, attachment.PreviewText),
    context_text: normalizeFirstOptionalStringValue(attachment.context_text, attachment.ContextText),
    width: normalizeIntegerValue(attachment.width ?? attachment.Width),
    height: normalizeIntegerValue(attachment.height ?? attachment.Height),
    expires_at: normalizeFirstOptionalStringValue(attachment.expires_at, attachment.ExpiresAt),
  }
}

function normalizeAttachmentRefs(...values: unknown[]): AttachmentRef[] | undefined {
  for (const value of values) {
    if (!Array.isArray(value)) {
      continue
    }
    return value.map((attachment) =>
      normalizeAttachmentRef(attachment && typeof attachment === 'object' ? (attachment as Record<string, unknown>) : {}),
    )
  }
  return undefined
}

function normalizeIntegerValue(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value)
    ? value
    : typeof value === 'string' && value.trim() && Number.isFinite(Number(value))
      ? Number(value)
      : undefined
}

function normalizeBooleanValue(value: unknown) {
  if (typeof value === 'boolean') {
    return value
  }
  if (typeof value === 'string') {
    const normalized = value.trim().toLowerCase()
    if (normalized === 'true') {
      return true
    }
    if (normalized === 'false') {
      return false
    }
  }
  return undefined
}

function normalizeWorkspaceMode(value: unknown): WorkspaceMode | undefined {
  return value === 'mutable' || value === 'readonly' ? value : undefined
}

function normalizeWorkspaceState(value: unknown): WorkspaceState | undefined {
  return value === 'pending_merge' || value === 'merged' || value === 'discarded' ? value : undefined
}

function normalizeTaskWorkspaceStateStatus(value: unknown): TaskWorkspaceStateStatus | undefined {
  switch (value) {
    case 'active':
    case 'pending_merge':
    case 'merged':
    case 'discarded':
    case 'completed':
      return value
    default:
      return undefined
  }
}

function normalizeModelScope(value: unknown, fallback: ModelScope): ModelScope {
  switch (typeof value === 'string' ? value.trim().toLowerCase() : '') {
    case 'admin':
      return 'admin'
    case 'global':
      return 'global'
    case 'owner':
      return 'owner'
    default:
      return fallback
  }
}

function normalizeModelCapabilities(value: unknown) {
  const raw = value && typeof value === 'object' ? (value as Record<string, unknown>) : {}
  return {
    attachments: normalizeBooleanValue(raw.attachments ?? raw.Attachments) ?? false,
  }
}

function normalizeModelContextBudget(value: unknown) {
  if (!value || typeof value !== 'object') {
    return undefined
  }

  const raw = value as Record<string, unknown>
  const budget = {
    max: normalizeIntegerValue(raw.max ?? raw.Max),
    input: normalizeIntegerValue(raw.input ?? raw.Input),
    output: normalizeIntegerValue(raw.output ?? raw.Output),
    short_term_limit: normalizeIntegerValue(raw.short_term_limit ?? raw.shortTermLimit ?? raw.ShortTermLimit),
  }

  if (Object.values(budget).every((field) => field === undefined)) {
    return undefined
  }

  return budget
}

function normalizeModelCatalogEntry(
  entry: Partial<ModelCatalogEntry> & Record<string, unknown>,
): ModelCatalogEntry {
  const rawContext = entry.context_window ?? entry.context ?? entry.Context
  const rawCapabilities = entry.capabilities ?? entry.Capabilities ?? {}
  const context = normalizeModelContextBudget(rawContext)
  return {
    id: normalizeFirstStringValue(entry.id, entry.ID),
    name: normalizeFirstStringValue(entry.name, entry.Name),
    type: normalizeFirstStringValue(entry.type, entry.Type),
    enabled: normalizeBooleanValue(entry.enabled),
    available: normalizeBooleanValue(entry.available),
    scope: normalizeStringValue(entry.scope),
    context,
    context_window: context,
    capabilities: normalizeModelCapabilities(rawCapabilities),
  }
}

function normalizeModelCatalogProvider(
  provider: Partial<ModelCatalogProvider> & {
    ID?: string
    Name?: string
    Models?: Array<Partial<ModelCatalogEntry> & Record<string, unknown>>
  },
): ModelCatalogProvider {
  const models: Array<Partial<ModelCatalogEntry> & Record<string, unknown>> = Array.isArray(provider.models ?? provider.Models)
    ? ((provider.models ?? provider.Models) as Array<Partial<ModelCatalogEntry> & Record<string, unknown>>)
    : []
  return {
    id: normalizeFirstStringValue(provider.id, provider.ID),
    name: normalizeFirstStringValue(provider.name, provider.Name),
    models: models.map((model) => normalizeModelCatalogEntry(model)),
  }
}

function normalizeModelCatalog(
  catalog: Partial<ModelCatalog> & {
    DefaultProviderID?: string
    DefaultModelID?: string
    Providers?: Array<Partial<ModelCatalogProvider>>
  },
): ModelCatalog {
  const providers: Array<Partial<ModelCatalogProvider>> = Array.isArray(catalog.providers ?? catalog.Providers)
    ? ((catalog.providers ?? catalog.Providers) as Array<Partial<ModelCatalogProvider>>)
    : []
  return {
    default_provider_id: normalizeFirstStringValue(catalog.default_provider_id, catalog.DefaultProviderID),
    default_model_id: normalizeFirstStringValue(catalog.default_model_id, catalog.DefaultModelID),
    providers: providers.map((provider) => normalizeModelCatalogProvider(provider)),
  }
}

function normalizeYAMLModel(
  model: Partial<YAMLModel> & Record<string, unknown>,
): YAMLModel {
  return {
    id: normalizeFirstStringValue(model.id, model.ID),
    name: normalizeFirstStringValue(model.name, model.Name),
    type: normalizeFirstStringValue(model.type, model.Type),
    context: normalizeModelContextBudget(model.context ?? model.Context),
    cost: model.cost,
    capabilities: normalizeModelCapabilities(model.capabilities ?? model.Capabilities),
    scope: normalizeModelScope(model.scope, 'admin'),
    enabled: normalizeBooleanValue(model.enabled) ?? true,
    scope_overridden: normalizeBooleanValue(model.scope_overridden ?? model.ScopeOverridden) ?? false,
    enabled_overridden: normalizeBooleanValue(model.enabled_overridden ?? model.EnabledOverridden) ?? false,
  }
}

function normalizeYAMLModelCatalog(
  catalog: Partial<YAMLModelCatalog> & {
    DefaultProviderID?: string
    DefaultModelID?: string
    Providers?: Array<Record<string, unknown>>
  },
): YAMLModelCatalog {
  const providers = Array.isArray(catalog.providers ?? catalog.Providers)
    ? ((catalog.providers ?? catalog.Providers ?? []) as Array<Record<string, unknown>>)
    : []
  return {
    default_provider_id: normalizeFirstStringValue(catalog.default_provider_id, catalog.DefaultProviderID),
    default_model_id: normalizeFirstStringValue(catalog.default_model_id, catalog.DefaultModelID),
    providers: providers.map((provider) => {
      const rawModels = Array.isArray(provider.models ?? provider.Models)
        ? ((provider.models ?? provider.Models ?? []) as unknown[])
        : []
      return {
        id: normalizeFirstStringValue(provider.id, provider.ID),
        name: normalizeFirstStringValue(provider.name, provider.Name),
        models: rawModels.map((model) =>
          normalizeYAMLModel(model && typeof model === 'object' ? model as Partial<YAMLModel> & Record<string, unknown> : {}),
        ),
      }
    }),
  }
}

export function normalizeCustomLLMModel(
  model: Partial<CustomLLMModel> & {
    ID?: string
    OwnerUserID?: number | string
    ProviderID?: string
    ModelID?: string
    DisplayName?: string
    ProviderType?: string
    BaseURL?: string
    APIKeyMasked?: string
    ContextMaxTokens?: number | string
    Context?: Record<string, unknown>
    Capabilities?: Record<string, unknown>
    CreatedAt?: string
    UpdatedAt?: string
  },
): CustomLLMModel {
  return {
    id: normalizeFirstStringValue(model.id, model.ID),
    owner_user_id: normalizeIntegerValue(model.owner_user_id ?? model.OwnerUserID) ?? 0,
    provider_id: normalizeFirstStringValue(model.provider_id, model.ProviderID),
    model_id: normalizeFirstStringValue(model.model_id, model.ModelID),
    display_name: normalizeFirstStringValue(model.display_name, model.DisplayName),
    provider_type: normalizeFirstStringValue(model.provider_type, model.ProviderType),
    base_url: normalizeFirstStringValue(model.base_url, model.BaseURL),
    api_key: normalizeFirstOptionalStringValue(model.api_key),
    api_key_masked: normalizeFirstStringValue(model.api_key_masked, model.APIKeyMasked),
    scope: normalizeModelScope(model.scope, 'owner'),
    enabled: normalizeBooleanValue(model.enabled) ?? true,
    context_max_tokens: normalizeIntegerValue(model.context_max_tokens ?? model.ContextMaxTokens) ?? 0,
    context: normalizeModelContextBudget(model.context ?? model.Context),
    capabilities: normalizeModelCapabilities(model.capabilities ?? model.Capabilities),
    cost: model.cost,
    created_at: normalizeFirstStringValue(model.created_at, model.CreatedAt),
    updated_at: normalizeFirstStringValue(model.updated_at, model.UpdatedAt),
  }
}

function normalizeCustomLLMModels(value: unknown) {
  return Array.isArray(value)
    ? value.map((model) =>
      normalizeCustomLLMModel(model && typeof model === 'object' ? model as Partial<CustomLLMModel> & Record<string, unknown> : {}),
    )
    : []
}

function normalizeModelTestResult(value: unknown): ModelTestResult {
  const raw = value && typeof value === 'object' ? value as Record<string, unknown> : {}
  return {
    ok: normalizeBooleanValue(raw.ok) ?? false,
    error: normalizeFirstOptionalStringValue(raw.error),
  }
}

function normalizeApprovalDecision(value: unknown): ApprovalDecision | undefined {
  return value === 'approve' || value === 'reject' ? value : undefined
}

export function normalizeToolApproval(
  approval: Partial<ToolApproval> & {
    ID?: string
    approval_id?: string
    approvalId?: string
    TaskID?: string
    ConversationID?: string
    Step?: number | string
    StepIndex?: number | string
    ToolCallID?: string
    ToolName?: string
    ArgumentsSummary?: string
    RiskLevel?: string
    Reason?: string
    Status?: string
    Decision?: string
    DecisionBy?: string
    DecisionReason?: string
    DecisionAt?: string
    CreatedAt?: string
    UpdatedAt?: string
  },
): ToolApproval {
  return {
    id: String(approval.id ?? approval.approval_id ?? approval.approvalId ?? approval.ID ?? ''),
    task_id: String(approval.task_id ?? approval.TaskID ?? ''),
    conversation_id: String(approval.conversation_id ?? approval.ConversationID ?? ''),
    step_index: normalizeIntegerValue(approval.step_index ?? approval.StepIndex ?? approval.Step),
    tool_call_id: String(approval.tool_call_id ?? approval.ToolCallID ?? ''),
    tool_name: String(approval.tool_name ?? approval.ToolName ?? ''),
    arguments_summary: String(approval.arguments_summary ?? approval.ArgumentsSummary ?? ''),
    risk_level: String(approval.risk_level ?? approval.RiskLevel ?? ''),
    reason: normalizeStringValue(approval.reason ?? approval.Reason),
    status: String(approval.status ?? approval.Status ?? ''),
    decision: normalizeApprovalDecision(approval.decision ?? approval.Decision),
    decision_by: normalizeStringValue(approval.decision_by ?? approval.DecisionBy),
    decision_reason: normalizeStringValue(approval.decision_reason ?? approval.DecisionReason),
    decision_at: normalizeStringValue(approval.decision_at ?? approval.DecisionAt),
    created_at: normalizeStringValue(approval.created_at ?? approval.CreatedAt),
    updated_at: normalizeStringValue(approval.updated_at ?? approval.UpdatedAt),
  }
}

export function normalizeConversationMessage(
  message: Partial<ConversationMessage> & {
    Role?: string
    Content?: string
    ProviderID?: string
    ModelID?: string
    ProviderData?: unknown
    providerData?: unknown
    provider_data?: unknown
    Usage?: unknown
    Reasoning?: string
  ToolCallId?: string
  ToolCallID?: string
  toolCallId?: string
  attachments?: unknown
  Attachments?: unknown
  providerId?: string
  modelId?: string
    usage?: unknown
    ReasoningItems?: Array<{ Summary?: Array<{ Text?: string }> }>
    ToolCalls?: Array<{ ID?: string; Name?: string; Arguments?: string; id?: string; name?: string; arguments?: string }>
    toolCalls?: Array<{ ID?: string; Name?: string; Arguments?: string; id?: string; name?: string; arguments?: string }>
  },
): ConversationMessage {
  return {
    role: normalizeConversationRole(message.role, message.Role),
    content: normalizeFirstStringValue(message.content, message.Content),
    attachments: normalizeAttachmentRefs(message.attachments, message.Attachments),
    provider_id: normalizeFirstOptionalStringValue(message.provider_id, message.providerId, message.ProviderID),
    model_id: normalizeFirstOptionalStringValue(message.model_id, message.modelId, message.ModelID),
    provider_data: normalizeConversationProviderData(message.provider_data ?? message.providerData ?? message.ProviderData),
    usage: normalizeTranscriptTokenUsage(message.usage ?? message.Usage),
    reasoning: normalizeFirstStringValue(message.reasoning, message.Reasoning),
    tool_call_id: normalizeFirstStringValue(message.tool_call_id, message.ToolCallId, message.ToolCallID, message.toolCallId),
    reasoning_items:
      message.reasoning_items ??
      message.ReasoningItems?.flatMap((item) =>
        (item.Summary ?? []).map((summary) => ({ text: summary.Text ?? '' })),
      ),
    tool_calls: normalizeConversationToolCalls(message.tool_calls, message.toolCalls, message.ToolCalls),
  }
}

function normalizeConversationProviderData(value: unknown): ConversationMessage['provider_data'] | undefined {
  if (!value || typeof value !== 'object') {
    return undefined
  }

  const raw = value as Record<string, unknown>
  const rawSystemMessage = raw.system_message
  if (!rawSystemMessage || typeof rawSystemMessage !== 'object') {
    return undefined
  }

  const systemMessage = rawSystemMessage as Record<string, unknown>
  const providerData: NonNullable<ConversationMessage['provider_data']> = {
    system_message: {
      visible_to_user: typeof systemMessage.visible_to_user === 'boolean' ? systemMessage.visible_to_user : undefined,
      kind: normalizeStringValue(systemMessage.kind),
    },
  }

  if (!providerData.system_message?.visible_to_user && !providerData.system_message?.kind) {
    return undefined
  }

  return providerData
}

export function normalizeRunTaskResult(
  result: Omit<RunTaskResult, 'final_message'> & {
    final_message: Partial<ConversationMessage> & {
      Role?: string
      Content?: string
      ProviderID?: string
      ModelID?: string
      providerId?: string
      modelId?: string
    }
  },
): RunTaskResult {
  const raw = result as Record<string, unknown>
  return {
    ...result,
    final_message: normalizeConversationMessage(result.final_message),
    usage: normalizeTranscriptTokenUsage(result.usage),
    memory_context: normalizeMemoryContextSnapshot(raw.memory_context),
    memory_compression: normalizeMemoryCompressionSnapshot(raw.memory_compression),
    workspace_mode: normalizeWorkspaceMode(raw.workspace_mode ?? raw.workspaceMode ?? raw.WorkspaceMode),
    workspace_state: normalizeWorkspaceState(raw.workspace_state ?? raw.workspaceState ?? raw.WorkspaceState),
  }
}

function normalizeTokenValue(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

export function normalizeMemoryContextSnapshot(value: unknown): MemoryContextState | undefined {
  if (!value || typeof value !== 'object') {
    return undefined
  }

  const raw = value as Record<string, unknown>
  const snapshot: MemoryContextState = {
    short_term_tokens: normalizeTokenValue(raw.short_term_tokens ?? raw.shortTermTokens ?? raw.ShortTermTokens) ?? 0,
    summary_tokens: normalizeTokenValue(raw.summary_tokens ?? raw.summaryTokens ?? raw.SummaryTokens) ?? 0,
    rendered_summary_tokens:
      normalizeTokenValue(raw.rendered_summary_tokens ?? raw.renderedSummaryTokens ?? raw.RenderedSummaryTokens) ?? 0,
    total_tokens: normalizeTokenValue(raw.total_tokens ?? raw.totalTokens ?? raw.TotalTokens) ?? 0,
    short_term_limit: normalizeTokenValue(raw.short_term_limit ?? raw.shortTermLimit ?? raw.ShortTermLimit) ?? 0,
    summary_limit: normalizeTokenValue(raw.summary_limit ?? raw.summaryLimit ?? raw.SummaryLimit) ?? 0,
    max_context_tokens: normalizeTokenValue(raw.max_context_tokens ?? raw.maxContextTokens ?? raw.MaxContextTokens) ?? 0,
    has_summary: raw.has_summary === true || raw.hasSummary === true || raw.HasSummary === true,
  }

  if (Object.values(snapshot).every((field) => field === 0 || field === false)) {
    return undefined
  }

  return snapshot
}

export function normalizeMemoryCompressionSnapshot(value: unknown): MemoryCompression | undefined {
  if (!value || typeof value !== 'object') {
    return undefined
  }

  const raw = value as Record<string, unknown>
  const snapshot: MemoryCompression = {
    tokens_before: normalizeTokenValue(raw.tokens_before ?? raw.tokensBefore ?? raw.TokensBefore) ?? 0,
    tokens_after: normalizeTokenValue(raw.tokens_after ?? raw.tokensAfter ?? raw.TokensAfter) ?? 0,
    short_term_tokens_before:
      normalizeTokenValue(raw.short_term_tokens_before ?? raw.shortTermTokensBefore ?? raw.ShortTermTokensBefore) ?? 0,
    short_term_tokens_after:
      normalizeTokenValue(raw.short_term_tokens_after ?? raw.shortTermTokensAfter ?? raw.ShortTermTokensAfter) ?? 0,
    summary_tokens_before:
      normalizeTokenValue(raw.summary_tokens_before ?? raw.summaryTokensBefore ?? raw.SummaryTokensBefore) ?? 0,
    summary_tokens_after:
      normalizeTokenValue(raw.summary_tokens_after ?? raw.summaryTokensAfter ?? raw.SummaryTokensAfter) ?? 0,
    rendered_summary_tokens_before:
      normalizeTokenValue(raw.rendered_summary_tokens_before ?? raw.renderedSummaryTokensBefore ?? raw.RenderedSummaryTokensBefore) ?? 0,
    rendered_summary_tokens_after:
      normalizeTokenValue(raw.rendered_summary_tokens_after ?? raw.renderedSummaryTokensAfter ?? raw.RenderedSummaryTokensAfter) ?? 0,
    total_tokens_before:
      normalizeTokenValue(raw.total_tokens_before ?? raw.totalTokensBefore ?? raw.TotalTokensBefore) ?? 0,
    total_tokens_after:
      normalizeTokenValue(raw.total_tokens_after ?? raw.totalTokensAfter ?? raw.TotalTokensAfter) ?? 0,
  }

  if (Object.values(snapshot).every((field) => field === 0)) {
    return undefined
  }

  return snapshot
}

export function normalizeTranscriptTokenUsage(value: unknown): TranscriptTokenUsage | undefined {
  if (!value || typeof value !== 'object') {
    return undefined
  }

  const raw = value as Record<string, unknown>
  const usage: TranscriptTokenUsage = {
    prompt_tokens: normalizeTokenValue(raw.prompt_tokens ?? raw.promptTokens ?? raw.PromptTokens),
    cached_prompt_tokens: normalizeTokenValue(raw.cached_prompt_tokens ?? raw.cachedPromptTokens ?? raw.CachedPromptTokens),
    completion_tokens: normalizeTokenValue(raw.completion_tokens ?? raw.completionTokens ?? raw.CompletionTokens),
    total_tokens: normalizeTokenValue(raw.total_tokens ?? raw.totalTokens ?? raw.TotalTokens),
  }

  if (Object.values(usage).every((field) => field == null)) {
    return undefined
  }

  return usage
}

export function normalizeTaskDetails(task: TaskDetails): TaskDetails {
  return {
    ...task,
    input: normalizeTaskInput(task.input),
    result: task.result ? normalizeRunTaskResult(task.result) : undefined,
    result_json: task.result_json ? normalizeRunTaskResult(task.result_json) : undefined,
  }
}

function normalizeTaskInput(input: TaskDetails['input'] | undefined): TaskDetails['input'] {
  if (!input || typeof input !== 'object') {
    return input
  }

  const raw = input as Record<string, unknown>
  const attachmentIDs = Array.isArray(raw.attachment_ids)
    ? raw.attachment_ids.filter((item): item is string => typeof item === 'string' && item.trim().length > 0)
    : Array.isArray(raw.attachmentIds)
      ? raw.attachmentIds.filter((item): item is string => typeof item === 'string' && item.trim().length > 0)
      : undefined
  const skills = Array.isArray(raw.skills)
    ? raw.skills.filter((item): item is string => typeof item === 'string' && item.trim().length > 0)
    : undefined

  return {
    conversation_id: normalizeFirstOptionalStringValue(raw.conversation_id, raw.conversationId),
    provider_id: normalizeFirstOptionalStringValue(raw.provider_id, raw.providerId),
    model_id: normalizeFirstOptionalStringValue(raw.model_id, raw.modelId),
    message: normalizeFirstOptionalStringValue(raw.message, raw.Message),
    attachment_ids: attachmentIDs,
    created_by: normalizeFirstOptionalStringValue(raw.created_by, raw.createdBy),
    skills,
    workspace_mode: normalizeWorkspaceMode(raw.workspace_mode ?? raw.workspaceMode ?? raw.WorkspaceMode),
  }
}

export function normalizeTaskWorkspaceState(state: Partial<TaskWorkspaceState> & Record<string, unknown>): TaskWorkspaceState {
  return {
    task_id: normalizeFirstStringValue(state.task_id, state.taskId, state.TaskID),
    user_id: normalizeFirstStringValue(state.user_id, state.userId, state.UserID),
    mode: normalizeWorkspaceMode(state.mode ?? state.Mode) ?? 'mutable',
    state: normalizeTaskWorkspaceStateStatus(state.state ?? state.State) ?? 'active',
    home_root: normalizeFirstStringValue(state.home_root, state.homeRoot, state.HomeRoot),
    task_root: normalizeFirstStringValue(state.task_root, state.taskRoot, state.TaskRoot),
    backup_root: normalizeFirstOptionalStringValue(state.backup_root, state.backupRoot, state.BackupRoot),
    created_at: normalizeFirstStringValue(state.created_at, state.createdAt, state.CreatedAt),
    updated_at: normalizeFirstStringValue(state.updated_at, state.updatedAt, state.UpdatedAt),
    merged_at: normalizeFirstOptionalStringValue(state.merged_at, state.mergedAt, state.MergedAt),
    discarded_at: normalizeFirstOptionalStringValue(state.discarded_at, state.discardedAt, state.DiscardedAt),
    error_message: normalizeFirstOptionalStringValue(state.error_message, state.errorMessage, state.ErrorMessage),
  }
}

export function normalizeConversation(
  conversation: Partial<Conversation> & Record<string, unknown>,
): Conversation {
  return {
    id: String(conversation.id ?? ''),
    title: String(conversation.title ?? ''),
    last_message: String(conversation.last_message ?? ''),
    message_count: normalizeIntegerValue(conversation.message_count) ?? 0,
    provider_id: String(conversation.provider_id ?? ''),
    model_id: String(conversation.model_id ?? ''),
    created_by: String(conversation.created_by ?? ''),
    created_at: String(conversation.created_at ?? ''),
    updated_at: String(conversation.updated_at ?? ''),
    last_message_at: normalizeStringValue(conversation.last_message_at),
    audit_run_id: normalizeStringValue(conversation.audit_run_id),
    audit_run_ids: Array.isArray(conversation.audit_run_ids)
      ? conversation.audit_run_ids.filter((item): item is string => typeof item === 'string' && item.trim().length > 0)
      : undefined,
    auditRunId: normalizeStringValue(conversation.auditRunId),
    run_id: normalizeStringValue(conversation.run_id),
    runId: normalizeStringValue(conversation.runId),
    memory_context: normalizeMemoryContextSnapshot(conversation.memory_context),
    memory_compression: normalizeMemoryCompressionSnapshot(conversation.memory_compression),
  }
}

export function extractStreamText(event: Partial<TaskStreamEvent>) {
  if (event.type !== 'log.message' || !event.payload) {
    return ''
  }

  const kind = typeof event.payload.Kind === 'string' ? event.payload.Kind : ''
  if (kind === 'text_delta') {
    return typeof event.payload.Text === 'string' ? event.payload.Text : ''
  }

  if (kind === 'completed' && event.payload.Message && typeof event.payload.Message === 'object') {
    return normalizeConversationMessage(
      event.payload.Message as Partial<ConversationMessage> & { Role?: string; Content?: string },
    ).content
  }

  return ''
}

export function formatTaskError(error: unknown, status?: string) {
  if (error && typeof error === 'object') {
    const message = (error as { message?: unknown }).message
    if (typeof message === 'string' && message.trim()) {
      return message
    }
  }

  if (typeof error === 'string' && error.trim()) {
    return error
  }

  if (status) {
    return `Task ${status}`
  }

  return 'Task failed'
}

async function request<T>(path: string, init?: RequestInit) {
  const response = await fetch(`${API_BASE}${path}`, {
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
    ...init,
  })

  const payload = (await response.json()) as ApiEnvelope<T>
  return unwrapEnvelope(payload)
}

export async function fetchPublicRegistrationSettings() {
  const settings = await request<Partial<PublicRegistrationSettings> & Record<string, unknown>>('/settings/registration')
  return {
    enabled: normalizeBooleanValue(settings.enabled) ?? true,
  } satisfies PublicRegistrationSettings
}

export async function fetchPublicTurnstileSettings() {
  const settings = await request<Partial<PublicTurnstileSettings> & Record<string, unknown>>('/settings/turnstile')
  return {
    enabled: normalizeBooleanValue(settings.enabled) ?? false,
    site_key: normalizeStringValue(settings.site_key) ?? '',
    protect_login: normalizeBooleanValue(settings.protect_login) ?? false,
    protect_registration: normalizeBooleanValue(settings.protect_registration) ?? false,
    protect_verification: normalizeBooleanValue(settings.protect_verification) ?? false,
  } satisfies PublicTurnstileSettings
}

export async function fetchUserProfile() {
  const user = await request<AuthUser>('/users/me')
  return normalizeAuthUser(user)
}

export async function updateUserProfile(input: UpdateUserProfileInput) {
  const user = await request<AuthUser>('/users/me', {
    method: 'PATCH',
    body: JSON.stringify(input),
  })
  return normalizeAuthUser(user)
}

export async function changeUserPassword(input: ChangeUserPasswordInput) {
  const user = await request<AuthUser>('/users/me/password', {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return normalizeAuthUser(user)
}

export async function startUserEmailVerification(input: UserEmailVerificationStartInput) {
  return request<EmailVerificationSentResult>('/users/me/email-verification', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function confirmUserEmailVerification(input: UserEmailVerificationConfirmInput) {
  const user = await request<AuthUser>('/users/me/email-verification/confirm', {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return normalizeAuthUser(user)
}

export async function fetchAdminUsers(filter: AdminUserFilter = {}) {
  const params = new URLSearchParams()
  appendQueryParam(params, 'q', filter.q)
  appendQueryParam(params, 'role', filter.role)
  appendQueryParam(params, 'status', filter.status)
  const users = await request<unknown>(`/admin/users${formatQuery(params)}`)
  return Array.isArray(users) ? users.map((user) => normalizeAuthUser(user as Partial<AuthUser> & Record<string, unknown>)) : []
}

export async function updateAdminUser(userId: number, input: AdminUserUpdateInput) {
  const user = await request<AuthUser>(`/admin/users/${userId}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })
  return normalizeAuthUser(user)
}

export async function resetAdminUserPassword(userId: number, input: AdminPasswordResetInput) {
  const user = await request<AuthUser>(`/admin/users/${userId}/reset-password`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return normalizeAuthUser(user)
}

export async function fetchAdminSMTPSettings() {
  const settings = await request<Partial<AdminSMTPSettings> & Record<string, unknown>>('/admin/settings/smtp')
  return normalizeAdminSMTPSettings(settings)
}

export async function updateAdminSMTPSettings(input: AdminSMTPSettingsInput) {
  const settings = await request<Partial<AdminSMTPSettings> & Record<string, unknown>>('/admin/settings/smtp', {
    method: 'PUT',
    body: JSON.stringify(input),
  })
  return normalizeAdminSMTPSettings(settings)
}

export async function testAdminSMTPSettings(input: AdminSMTPTestInput) {
  return request<EmailVerificationSentResult>('/admin/settings/smtp/test', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function fetchAdminTurnstileSettings() {
  const settings = await request<Partial<AdminTurnstileSettings> & Record<string, unknown>>('/admin/settings/turnstile')
  return normalizeAdminTurnstileSettings(settings)
}

export async function updateAdminTurnstileSettings(input: AdminTurnstileSettingsInput) {
  const settings = await request<Partial<AdminTurnstileSettings> & Record<string, unknown>>('/admin/settings/turnstile', {
    method: 'PUT',
    body: JSON.stringify(input),
  })
  return normalizeAdminTurnstileSettings(settings)
}

export async function fetchAdminRegistrationSettings() {
  const settings = await request<Partial<AdminRegistrationSettings> & Record<string, unknown>>('/admin/settings/registration')
  return {
    enabled: normalizeBooleanValue(settings.enabled) ?? true,
  } satisfies AdminRegistrationSettings
}

export async function updateAdminRegistrationSettings(input: AdminRegistrationSettings) {
  const settings = await request<Partial<AdminRegistrationSettings> & Record<string, unknown>>('/admin/settings/registration', {
    method: 'PUT',
    body: JSON.stringify(input),
  })
  return {
    enabled: normalizeBooleanValue(settings.enabled) ?? true,
  } satisfies AdminRegistrationSettings
}

export async function fetchAdminAuditEvents(filter: AdminAuditEventFilter = {}) {
  const params = new URLSearchParams()
  appendQueryParam(params, 'action', filter.action)
  appendQueryParam(params, 'target_kind', filter.target_kind)
  appendQueryParam(params, 'actor_username', filter.actor_username)
  appendQueryParam(params, 'actor_id', filter.actor_id)
  appendQueryParam(params, 'target_id', filter.target_id)
  appendQueryParam(params, 'created_after', filter.created_after)
  appendQueryParam(params, 'created_before', filter.created_before)
  appendQueryParam(params, 'limit', filter.limit)
  const events = await request<unknown>(`/admin/audit-events${formatQuery(params)}`)
  return Array.isArray(events)
    ? events.map((event) => normalizeAdminAuditEvent(event as Partial<AdminAuditEvent> & Record<string, unknown>))
    : []
}

export async function fetchAdminModels() {
  const catalog = await request<Partial<YAMLModelCatalog> & Record<string, unknown>>('/admin/models')
  return normalizeYAMLModelCatalog(catalog)
}

export async function updateAdminYAMLModel(providerId: string, modelId: string, input: YAMLModelOverrideInput) {
  const model = await request<Partial<YAMLModel> & Record<string, unknown>>(
    `/admin/models/yaml/${encodeURIComponent(providerId)}/${encodeURIComponent(modelId)}`,
    {
      method: 'PATCH',
      body: JSON.stringify(input),
    },
  )
  return normalizeYAMLModel(model)
}

export async function fetchAdminCustomModels() {
  const models = await request<unknown>('/admin/models/custom')
  return normalizeCustomLLMModels(models)
}

export async function createAdminCustomModel(input: CustomLLMModelInput) {
  const model = await request<Partial<CustomLLMModel> & Record<string, unknown>>('/admin/models/custom', {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return normalizeCustomLLMModel(model)
}

export async function updateAdminCustomModel(modelId: string, input: CustomLLMModelInput) {
  const model = await request<Partial<CustomLLMModel> & Record<string, unknown>>(`/admin/models/custom/${encodeURIComponent(modelId)}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
  return normalizeCustomLLMModel(model)
}

export async function deleteAdminCustomModel(modelId: string) {
  return request<CustomLLMModelDeleteResult>(`/admin/models/custom/${encodeURIComponent(modelId)}`, {
    method: 'DELETE',
  })
}

export async function testAdminCustomModel(modelId: string) {
  const result = await request<unknown>(`/admin/models/custom/${encodeURIComponent(modelId)}/test`, {
    method: 'POST',
  })
  return normalizeModelTestResult(result)
}

export async function fetchUserCustomModels() {
  const models = await request<unknown>('/users/me/models')
  return normalizeCustomLLMModels(models)
}

export async function createUserCustomModel(input: CustomLLMModelInput) {
  const model = await request<Partial<CustomLLMModel> & Record<string, unknown>>('/users/me/models', {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return normalizeCustomLLMModel(model)
}

export async function updateUserCustomModel(modelId: string, input: CustomLLMModelInput) {
  const model = await request<Partial<CustomLLMModel> & Record<string, unknown>>(`/users/me/models/${encodeURIComponent(modelId)}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
  return normalizeCustomLLMModel(model)
}

export async function deleteUserCustomModel(modelId: string) {
  return request<CustomLLMModelDeleteResult>(`/users/me/models/${encodeURIComponent(modelId)}`, {
    method: 'DELETE',
  })
}

export async function testUserCustomModel(modelId: string) {
  const result = await request<unknown>(`/users/me/models/${encodeURIComponent(modelId)}/test`, {
    method: 'POST',
  })
  return normalizeModelTestResult(result)
}

export async function fetchConversations() {
  const conversations = await request<Array<Partial<Conversation> & Record<string, unknown>>>('/conversations')
  return conversations.map((conversation) => normalizeConversation(conversation))
}

export async function fetchAuditConversations() {
  const conversations = await request<Array<Partial<Conversation> & Record<string, unknown>>>('/audit/conversations')
  return conversations.map((conversation) => normalizeConversation(conversation))
}

export async function fetchConversation(conversationId: string) {
  const conversation = await request<Partial<Conversation> & Record<string, unknown>>(`/conversations/${conversationId}`)
  return normalizeConversation(conversation)
}

export async function fetchModelCatalog() {
  const catalog = await request<Partial<ModelCatalog> & Record<string, unknown>>('/models')
  return normalizeModelCatalog(catalog)
}

export async function fetchConversationMessages(conversationId: string) {
  const messages = await request<
    Array<Partial<ConversationMessage> & { Role?: string; Content?: string; Reasoning?: string; ToolCallId?: string }>
  >(`/conversations/${conversationId}/messages`)
  return messages.map((message) => normalizeConversationMessage(message))
}

export async function uploadAttachment(file: File, conversationId?: string) {
  const formData = new FormData()
  formData.append('file', file)
  if (conversationId) {
    formData.append('conversation_id', conversationId)
  }

  const response = await fetch(`${API_BASE}/attachments`, {
    method: 'POST',
    credentials: 'include',
    body: formData,
  })

  const payload = (await response.json()) as ApiEnvelope<AttachmentRef>
  return unwrapEnvelope(payload)
}

export async function deleteAttachment(attachmentId: string) {
  return request<{ deleted: boolean }>(`/attachments/${encodeURIComponent(attachmentId)}`, {
    method: 'DELETE',
  })
}

export function getAttachmentContentURL(attachmentId: string) {
  return `${API_BASE}/attachments/${encodeURIComponent(attachmentId)}/content`
}

export async function deleteConversation(conversationId: string) {
  return request<{ deleted: boolean }>(`/conversations/${conversationId}`, {
    method: 'DELETE',
  })
}

export async function fetchAuditRun(runId: string) {
  return request<AuditRun>(`/audit/runs/${runId}`)
}

export async function fetchAuditRunEvents(runId: string) {
  return request<AuditEvent[]>(`/audit/runs/${runId}/events`)
}

export async function fetchAuditRunReplay(runId: string) {
  return request<AuditReplayBundle>(`/audit/runs/${runId}/replay`)
}

export async function fetchAuditConversationRuns(conversationId: string) {
  return request<AuditRun[]>(`/audit/conversations/${conversationId}/runs`)
}

export async function fetchAuditConversationEvents(conversationId: string) {
  return request<AuditEvent[]>(`/audit/conversations/${conversationId}/events`)
}

export async function fetchSkills() {
  return request<WorkspaceSkillListItem[]>('/skills')
}

export async function fetchSkill(name: string) {
  return request<WorkspaceSkill>(`/skills/${encodeURIComponent(name)}`)
}

export async function fetchPromptDocuments() {
  return request<PromptDocument[]>('/prompts/documents')
}

export async function createPromptDocument(input: CreatePromptDocumentInput) {
  return request<PromptDocument>('/prompts/documents', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function updatePromptDocument(documentId: string, input: UpdatePromptDocumentInput) {
  return request<PromptDocument>(`/prompts/documents/${encodeURIComponent(documentId)}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

export async function deletePromptDocument(documentId: string) {
  return request<PromptDeleteResult>(`/prompts/documents/${encodeURIComponent(documentId)}`, {
    method: 'DELETE',
  })
}

export async function fetchPromptBindings() {
  return request<PromptBinding[]>('/prompts/bindings')
}

export async function createPromptBinding(input: PromptBindingInput) {
  return request<PromptBinding>('/prompts/bindings', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function updatePromptBinding(bindingId: number, input: PromptBindingInput) {
  return request<PromptBinding>(`/prompts/bindings/${bindingId}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

export async function deletePromptBinding(bindingId: number) {
  return request<PromptDeleteResult>(`/prompts/bindings/${bindingId}`, {
    method: 'DELETE',
  })
}

export async function createRunTask(input: {
  createdBy: string
  conversationId?: string
  providerId: string
  modelId: string
  message: string
  attachmentIds?: string[]
  skills?: string[]
  workspaceMode?: WorkspaceMode
}) {
  return request<TaskSnapshot>('/tasks', {
    method: 'POST',
    body: JSON.stringify(buildRunTaskRequest(input)),
  })
}

export async function fetchTask(taskId: string) {
  return request<TaskSnapshot>(`/tasks/${taskId}`)
}

export async function fetchTaskDetails(taskId: string) {
  const task = await fetchTaskResult(taskId)
  return normalizeTaskDetails(task)
}

export async function fetchTaskApprovals(taskId: string) {
  const approvals = await request<Array<Partial<ToolApproval>>>(`/tasks/${taskId}/approvals`)
  return approvals.map((approval) => normalizeToolApproval(approval))
}

export async function decideTaskApproval(taskId: string, approvalId: string, input: ToolApprovalDecisionInput) {
  const approval = await request<Partial<ToolApproval>>(`/tasks/${taskId}/approvals/${approvalId}/decision`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return normalizeToolApproval(approval)
}

export async function confirmTaskWorkspaceMerge(taskId: string) {
  const state = await request<Partial<TaskWorkspaceState> & Record<string, unknown>>(`/tasks/${encodeURIComponent(taskId)}/workspace/confirm`, {
    method: 'POST',
  })
  return normalizeTaskWorkspaceState(state)
}

export async function discardTaskWorkspaceChanges(taskId: string) {
  const state = await request<Partial<TaskWorkspaceState> & Record<string, unknown>>(`/tasks/${encodeURIComponent(taskId)}/workspace/discard`, {
    method: 'POST',
  })
  return normalizeTaskWorkspaceState(state)
}

export function normalizeInteractionRecord(value: Partial<InteractionRecord> & Record<string, unknown>): InteractionRecord {
  return {
    id: String(value.id ?? ''),
    task_id: String(value.task_id ?? ''),
    conversation_id: String(value.conversation_id ?? ''),
    step_index: normalizeIntegerValue(value.step_index),
    tool_call_id: normalizeStringValue(value.tool_call_id) ?? '',
    kind: String(value.kind ?? ''),
    status: String(value.status ?? ''),
    request_json: value.request_json && typeof value.request_json === 'object' ? (value.request_json as Record<string, unknown>) : undefined,
    response_json: value.response_json && typeof value.response_json === 'object' ? (value.response_json as Record<string, unknown>) : undefined,
    responded_by: normalizeStringValue(value.responded_by),
    responded_at: normalizeStringValue(value.responded_at),
    created_at: normalizeStringValue(value.created_at),
    updated_at: normalizeStringValue(value.updated_at),
  }
}

export async function fetchTaskInteractions(taskId: string) {
  const interactions = await request<Array<Partial<InteractionRecord>>>(`/tasks/${taskId}/interactions`)
  return interactions.map((interaction) => normalizeInteractionRecord(interaction as Partial<InteractionRecord> & Record<string, unknown>))
}

export async function respondTaskInteraction(taskId: string, interactionId: string, input: { selected_option_id?: string; selected_option_ids?: string[]; custom_text?: string }) {
  const interaction = await request<Partial<InteractionRecord>>(`/tasks/${taskId}/interactions/${interactionId}/respond`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return normalizeInteractionRecord(interaction as Partial<InteractionRecord> & Record<string, unknown>)
}

export async function cancelTask(taskId: string) {
  const task = await request<TaskDetails>(`/tasks/${taskId}/cancel`, {
    method: 'POST',
  })
  return normalizeTaskDetails(task)
}

export async function findRunningTaskByConversation(conversationId: string) {
  const task = await request<TaskDetails | null>(`/tasks/running?conversation_id=${encodeURIComponent(conversationId)}`)
  return task ? normalizeTaskDetails(task) : null
}

async function fetchTaskResult(taskId: string) {
  const response = await fetch(`${API_BASE}/tasks/${taskId}`, {
    credentials: 'include',
  })
  const payload = (await response.json()) as ApiEnvelope<TaskDetails>
  return unwrapEnvelope(payload)
}

export async function waitForRunTask(taskId: string) {
  const startedAt = Date.now()

  while (Date.now() - startedAt < POLL_TIMEOUT_MS) {
    const task = await fetchTaskResult(taskId)
    if (task.status === 'succeeded') {
      if (task.result) {
        return normalizeRunTaskResult(task.result)
      }

      if (task.result_json) {
        return normalizeRunTaskResult(task.result_json)
      }

      throw new Error('Task succeeded but no result payload was returned')
    }

    if (task.status === 'failed' || task.status === 'cancelled') {
      throw new Error(formatTaskError(task.error, task.status))
    }

    await new Promise((resolve) => window.setTimeout(resolve, POLL_INTERVAL_MS))
  }

  throw new Error('Task timed out')
}

export async function streamRunTask(
  taskId: string,
  onTextDelta: (chunk: string) => void,
  onEvent?: (event: TaskStreamEvent) => void,
  options?: { signal?: AbortSignal; afterSeq?: number },
) {
  return new Promise<RunTaskResult>((resolve, reject) => {
    const afterSeq = typeof options?.afterSeq === 'number' && Number.isFinite(options.afterSeq) ? Math.max(0, options.afterSeq) : 0
    const stream = new EventSource(`${API_BASE}/tasks/${taskId}/events?after_seq=${afterSeq}`, { withCredentials: true })
    let settled = false

    const cleanupAbort = () => {
      if (options?.signal) {
        options.signal.removeEventListener('abort', handleAbort)
      }
    }

    const close = () => {
      stream.close()
      cleanupAbort()
    }

    const rejectOnce = (error: unknown) => {
      if (settled) {
        return
      }
      settled = true
      close()
      reject(error)
    }

    const handleAbort = () => {
      rejectOnce(new Error(TASK_STREAM_ABORTED_MESSAGE))
    }

    if (options?.signal?.aborted) {
      rejectOnce(new Error(TASK_STREAM_ABORTED_MESSAGE))
      return
    }
    options?.signal?.addEventListener('abort', handleAbort, { once: true })

    const handleEvent = (message: MessageEvent<string>) => {
      try {
        const event = JSON.parse(message.data) as TaskStreamEvent
        onEvent?.(event)
        const text = extractStreamText(event)
        if (text) {
          onTextDelta(text)
        }

        if (event.type === 'task.finished') {
          void finish()
        }
      } catch (error) {
        rejectOnce(error)
      }
    }

    const finish = async () => {
      if (settled) {
        return
      }
      settled = true
      close()

      try {
        const task = await fetchTaskResult(taskId)
        if (task.status === 'failed' || task.status === 'cancelled') {
          reject(new Error(formatTaskError(task.error, task.status)))
          return
        }
        const result = await waitForRunTask(taskId)
        resolve(result)
      } catch (error) {
        reject(error)
      }
    }

    stream.addEventListener('log.message', handleEvent)
    stream.addEventListener('tool.started', handleEvent)
    stream.addEventListener('tool.finished', handleEvent)
    stream.addEventListener('approval.requested', handleEvent)
    stream.addEventListener('approval.resolved', handleEvent)
    stream.addEventListener('interaction.requested', handleEvent)
    stream.addEventListener('interaction.responded', handleEvent)
    stream.addEventListener('memory.context_state', handleEvent)
    stream.addEventListener('memory.compressed', handleEvent)
    stream.addEventListener('task.finished', handleEvent)
  
    stream.onerror = () => {
      rejectOnce(new Error('Task event stream disconnected'))
    }
  })
}
