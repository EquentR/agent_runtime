import type {
  ApprovalDecision,
  ApiEnvelope,
  AuditEvent,
  AuditReplayBundle,
  AuditRun,
  AuthUser,
  Conversation,
  ConversationMessage,
  ModelCatalog,
  CreatePromptDocumentInput,
  PromptBinding,
  PromptBindingInput,
  PromptDeleteResult,
  PromptDocument,
  TaskDetails,
  RunTaskRequest,
  RunTaskResult,
  TaskStreamEvent,
  TaskSnapshot,
  ToolApproval,
  ToolApprovalDecisionInput,
  InteractionRecord,
  TranscriptTokenUsage,
  UpdatePromptDocumentInput,
  UserRole,
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

function normalizeRole(value: unknown): UserRole {
  return value === 'admin' ? 'admin' : 'user'
}

export function normalizeAuthUser(user: Partial<AuthUser>): AuthUser {
  return {
    id: typeof user.id === 'number' && Number.isFinite(user.id) ? user.id : 0,
    username: typeof user.username === 'string' ? user.username.trim() : '',
    role: normalizeRole(user.role),
  }
}

export function buildRunTaskRequest(input: {
  createdBy: string
  conversationId?: string
  providerId: string
  modelId: string
  message: string
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

  return request
}

function normalizeStringValue(value: unknown) {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

function normalizeIntegerValue(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value)
    ? value
    : typeof value === 'string' && value.trim() && Number.isFinite(Number(value))
      ? Number(value)
      : undefined
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
    providerId?: string
    modelId?: string
    usage?: unknown
    ReasoningItems?: Array<{ Summary?: Array<{ Text?: string }> }>
    ToolCalls?: Array<{ ID?: string; Name?: string; Arguments?: string; id?: string; name?: string; arguments?: string }>
    toolCalls?: Array<{ ID?: string; Name?: string; Arguments?: string; id?: string; name?: string; arguments?: string }>
  },
): ConversationMessage {
  return {
    role: (message.role ?? message.Role ?? 'assistant') as ConversationMessage['role'],
    content: message.content ?? message.Content ?? '',
    provider_id: normalizeStringValue(message.provider_id ?? message.providerId ?? message.ProviderID),
    model_id: normalizeStringValue(message.model_id ?? message.modelId ?? message.ModelID),
    provider_data: normalizeConversationProviderData(message.provider_data ?? message.providerData ?? message.ProviderData),
    usage: normalizeTranscriptTokenUsage(message.usage ?? message.Usage),
    reasoning: message.reasoning ?? message.Reasoning,
    tool_call_id: message.tool_call_id ?? message.ToolCallId ?? message.ToolCallID ?? message.toolCallId,
    reasoning_items:
      message.reasoning_items ??
      message.ReasoningItems?.flatMap((item) =>
        (item.Summary ?? []).map((summary) => ({ text: summary.Text ?? '' })),
      ),
    tool_calls:
      message.tool_calls ??
      message.toolCalls?.map((toolCall) => ({
        id: toolCall.id ?? toolCall.ID ?? '',
        name: toolCall.name ?? toolCall.Name ?? '',
        arguments: toolCall.arguments ?? toolCall.Arguments ?? '',
      })) ??
      message.ToolCalls?.map((toolCall) => ({
        id: toolCall.id ?? toolCall.ID ?? '',
        name: toolCall.name ?? toolCall.Name ?? '',
        arguments: toolCall.arguments ?? toolCall.Arguments ?? '',
      })),
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
  return {
    ...result,
    final_message: normalizeConversationMessage(result.final_message),
    usage: normalizeTranscriptTokenUsage(result.usage),
  }
}

function normalizeTokenValue(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
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
    input: task.input,
    result: task.result ? normalizeRunTaskResult(task.result) : undefined,
    result_json: task.result_json ? normalizeRunTaskResult(task.result_json) : undefined,
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

export async function fetchConversations() {
  return request<Conversation[]>('/conversations')
}

export async function fetchConversation(conversationId: string) {
  return request<Conversation>(`/conversations/${conversationId}`)
}

export async function fetchModelCatalog() {
  return request<ModelCatalog>('/models')
}

export async function fetchConversationMessages(conversationId: string) {
  const messages = await request<
    Array<Partial<ConversationMessage> & { Role?: string; Content?: string; Reasoning?: string; ToolCallId?: string }>
  >(`/conversations/${conversationId}/messages`)
  return messages.map((message) => normalizeConversationMessage(message))
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
    stream.addEventListener('task.finished', handleEvent)
  
    stream.onerror = () => {
      rejectOnce(new Error('Task event stream disconnected'))
    }
  })
}
