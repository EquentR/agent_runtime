export interface ApiEnvelope<T> {
  code: number
  data: T
  message: string
  ok: boolean
  time: string
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
}

export interface ConversationMessage {
  role: 'user' | 'assistant' | 'tool' | 'system'
  content: string
  provider_id?: string
  model_id?: string
  usage?: TranscriptTokenUsage
  reasoning?: string
  tool_call_id?: string
  reasoning_items?: Array<{ text?: string }>
  tool_calls?: Array<{ id: string; name: string; arguments?: string }>
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
}

export interface TaskSnapshot {
  id: string
  task_type: string
  status: 'queued' | 'running' | 'cancel_requested' | 'cancelled' | 'succeeded' | 'failed'
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
  created_by?: string
}

export interface TaskDetails extends TaskSnapshot {
  input?: TaskInput
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
}

export interface TaskStreamEvent {
  task_id: string
  seq: number
  type: string
  ts?: string
  payload?: Record<string, unknown>
}

export interface TranscriptEntry {
  id: string
  kind: 'user' | 'reasoning' | 'tool' | 'reply' | 'error'
  title: string
  content?: string
  provider_id?: string
  model_id?: string
  details?: TranscriptEntryDetail[]
  status?: 'running' | 'done' | 'error'
  group_key?: string
  token_usage?: TranscriptTokenUsage
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

export interface RunTaskRequest {
  task_type: 'agent.run'
  created_by: string
  input: {
    conversation_id?: string
    provider_id: string
    model_id: string
    message: string
    created_by: string
  }
}
