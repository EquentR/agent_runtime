export interface ApiEnvelope<T> {
  code: number
  data: T
  message: string
  ok: boolean
  time: string
}

export type UserRole = 'admin' | 'user'

export interface AuthUser {
  id: number
  username: string
  role: UserRole
}

export type PromptStatus = 'active' | 'disabled'
export type PromptPhase = 'session' | 'step_pre_model' | 'tool_result'

export interface PromptDocument {
  id: string
  name: string
  description: string
  content: string
  scope: string
  status: PromptStatus | string
  created_by: string
  updated_by: string
  created_at: string
  updated_at: string
}

export interface CreatePromptDocumentInput {
  id: string
  name: string
  description: string
  content: string
  scope: string
  status: PromptStatus | string
}

export interface UpdatePromptDocumentInput {
  name: string
  description: string
  content: string
  scope: string
  status: PromptStatus | string
}

export interface PromptBinding {
  id: number
  prompt_id: string
  scene: string
  phase: PromptPhase | string
  is_default: boolean
  priority: number
  provider_id: string
  model_id: string
  status: PromptStatus | string
  created_by: string
  updated_by: string
  created_at: string
  updated_at: string
}

export interface PromptBindingInput {
  prompt_id: string
  scene: string
  phase: PromptPhase | string
  is_default: boolean
  priority: number
  provider_id: string
  model_id: string
  status: PromptStatus | string
}

export interface PromptDeleteResult {
  deleted: boolean
}

export interface SessionUser {
  username: string
  role: UserRole
}

export interface Conversation {
  id: string
  title: string
  last_message: string
  message_count: number
  provider_id: string
  model_id: string
  created_by: string
  created_at: string
  updated_at: string
  last_message_at?: string
  audit_run_id?: string
  audit_run_ids?: string[]
  auditRunId?: string
  run_id?: string
  runId?: string
  memory_context?: MemoryContextState
  memory_compression?: MemoryCompression
}

export interface AuditRun {
  id: string
  task_id: string
  conversation_id?: string
  task_type: string
  provider_id?: string
  model_id?: string
  runner_id?: string
  status: TaskSnapshot['status']
  created_by: string
  replayable: boolean
  schema_version: string
  started_at?: string
  finished_at?: string
  created_at: string
  updated_at: string
}

export interface AuditEvent {
  id: number
  run_id: string
  task_id: string
  seq: number
  phase: string
  event_type: string
  level: string
  step_index: number
  parent_seq: number
  ref_artifact_id: string
  payload?: unknown
  created_at: string
}

export interface AuditReplayArtifactSummary {
  id: string
  kind: string
  mime_type: string
  encoding: string
  size_bytes: number
  sha256?: string
  redaction_state: string
  created_at: string
}

export interface AuditReplayEvent {
  seq: number
  phase: string
  event_type: string
  display_name?: string
  level: string
  step_index: number
  parent_seq: number
  created_at: string
  payload?: unknown
  artifact?: AuditReplayArtifactSummary | null
}

export interface AuditReplayArtifact extends AuditReplayArtifactSummary {
  body?: unknown
}

export interface AuditReplayBundle {
  run: AuditRun
  timeline: AuditReplayEvent[]
  artifacts: AuditReplayArtifact[]
}

export interface ConversationMessage {
  role: 'user' | 'assistant' | 'tool' | 'system'
  content: string
  attachments?: AttachmentRef[]
  provider_id?: string
  model_id?: string
  provider_data?: {
    system_message?: {
      visible_to_user?: boolean
      kind?: string
    }
  }
  usage?: TranscriptTokenUsage
  reasoning?: string
  tool_call_id?: string
  reasoning_items?: Array<{ text?: string }>
  tool_calls?: Array<{ id: string; name: string; arguments?: string }>
}

export interface AttachmentRef {
  id: string
  file_name: string
  mime_type: string
  size_bytes?: number
  kind?: string
  status?: string
  preview_text?: string
  context_text?: string
  width?: number
  height?: number
  expires_at?: string
}

export interface ModelCatalog {
  default_provider_id: string
  default_model_id: string
  providers: ModelCatalogProvider[]
}

export interface ModelCatalogProvider {
  id: string
  name: string
  models: ModelCatalogEntry[]
}

export interface ModelCatalogEntry {
  id: string
  name: string
  type: string
  capabilities?: {
    attachments: boolean
  }
  context_window?: {
    max?: number
    input?: number
    output?: number
    short_term_limit?: number
  }
}

export type TaskStatus =
  | 'queued'
  | 'running'
  | 'waiting'
  | 'cancel_requested'
  | 'cancelled'
  | 'succeeded'
  | 'failed'

export type TaskSuspendReason = 'waiting_for_tool_approval' | 'waiting_for_interaction' | string

export interface TaskSnapshot {
  id: string
  task_type: string
  status: TaskStatus
  input?: TaskInput
  suspend_reason?: TaskSuspendReason
  created_by: string
  created_at: string
  updated_at: string
  current_step_key?: string
  current_step_title?: string
  retry_of_task_id?: string
}

export interface TaskInput {
  conversation_id?: string
  provider_id?: string
  model_id?: string
  message?: string
  attachment_ids?: string[]
  created_by?: string
  skills?: string[]
}

export interface TaskDetails extends TaskSnapshot {
  result?: RunTaskResult
  result_json?: RunTaskResult
  error?: unknown
}

export interface RunTaskResult {
  conversation_id: string
  provider_id: string
  model_id: string
  final_message: ConversationMessage
  usage?: TranscriptTokenUsage
  messages_appended: number
  memory_context?: MemoryContextState
  memory_compression?: MemoryCompression
}

export interface TaskStreamEvent {
  task_id: string
  seq: number
  type: string
  ts?: string
  payload?: Record<string, unknown>
}

export type ApprovalDecision = 'approve' | 'reject'

export type InteractionKind = 'approval' | 'question' | string

export type InteractionStatus = 'pending' | 'approved' | 'rejected' | 'responded' | 'expired' | 'cancelled' | string

export interface InteractionRecord {
  id: string
  task_id: string
  conversation_id: string
  step_index?: number
  tool_call_id?: string
  kind: InteractionKind
  status: InteractionStatus
  request_json?: Record<string, unknown>
  response_json?: Record<string, unknown>
  responded_by?: string
  responded_at?: string
  created_at?: string
  updated_at?: string
}

export interface QuestionInteractionSubmitInput {
  taskId: string
  interactionId: string
  selectedOptionId?: string
  selectedOptionIds?: string[]
  customText?: string
}

export type ToolApprovalStatus = 'pending' | 'approved' | 'rejected' | 'expired' | 'cancelled' | string

export interface ToolApproval {
  id: string
  task_id: string
  conversation_id: string
  step_index?: number
  tool_call_id: string
  tool_name: string
  arguments_summary: string
  risk_level: string
  reason?: string
  status: ToolApprovalStatus
  decision?: ApprovalDecision
  decision_by?: string
  decision_reason?: string
  decision_at?: string
  created_at?: string
  updated_at?: string
}

export interface ToolApprovalDecisionInput {
  decision: ApprovalDecision
  reason: string
}

export interface MemoryContextState {
  short_term_tokens: number
  summary_tokens: number
  rendered_summary_tokens: number
  total_tokens: number
  short_term_limit: number
  summary_limit: number
  max_context_tokens: number
  has_summary: boolean
}

export interface MemoryCompression {
  tokens_before: number
  tokens_after: number
  short_term_tokens_before: number
  short_term_tokens_after: number
  summary_tokens_before: number
  summary_tokens_after: number
  rendered_summary_tokens_before: number
  rendered_summary_tokens_after: number
  total_tokens_before: number
  total_tokens_after: number
}

export interface TranscriptEntry {
  id: string
  kind: 'user' | 'reasoning' | 'tool' | 'reply' | 'error' | 'approval' | 'question' | 'memory'
  title: string
  content?: string
  attachments?: AttachmentRef[]
  provider_id?: string
  model_id?: string
  details?: TranscriptEntryDetail[]
  status?: 'running' | 'done' | 'error'
  group_key?: string
  token_usage?: TranscriptTokenUsage
  approval?: ToolApproval
  question_interaction?: InteractionRecord
  memory_context_state?: MemoryContextState
  memory_compression?: MemoryCompression
}

export interface TranscriptTokenUsage {
  prompt_tokens?: number
  cached_prompt_tokens?: number
  completion_tokens?: number
  total_tokens?: number
}

export interface TranscriptEntryDetail {
  key?: string
  label: string
  preview?: string
  collapsed?: boolean
  loading?: boolean
  blocks?: TranscriptEntryDetailBlock[]
}

export interface TranscriptEntryDetailBlock {
  label: string
  value: string
  loading?: boolean
}

export interface WorkspaceSkillListItem {
  name: string
  description?: string
  tags?: string[]
  tools?: string[]
  version?: string
  hidden?: boolean
  source_ref: string
}

export interface WorkspaceSkill extends WorkspaceSkillListItem {
  content: string
  resource_refs?: string[]
}

export interface RunTaskRequest {
  task_type: 'agent.run'
  created_by: string
  input: {
    conversation_id?: string
    provider_id: string
    model_id: string
    message: string
    attachment_ids?: string[]
    created_by: string
    skills?: string[]
  }
}
