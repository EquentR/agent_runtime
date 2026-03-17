import type {
  ApiEnvelope,
  Conversation,
  ConversationMessage,
  RunTaskRequest,
  RunTaskResult,
  TaskStreamEvent,
  TaskSnapshot,
} from '../types/api'

const API_BASE = '/api/v1'
const POLL_INTERVAL_MS = 1200
const POLL_TIMEOUT_MS = 90000

export function unwrapEnvelope<T>(envelope: ApiEnvelope<T>) {
  if (!envelope.ok) {
    throw new Error(envelope.message || 'Request failed')
  }

  return envelope.data
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

export function normalizeConversationMessage(
  message: Partial<ConversationMessage> & {
    Role?: string
    Content?: string
    Reasoning?: string
    ToolCallId?: string
    ReasoningItems?: Array<{ Summary?: Array<{ Text?: string }> }>
    ToolCalls?: Array<{ ID?: string; Name?: string; Arguments?: string }>
  },
): ConversationMessage {
  return {
    role: (message.role ?? message.Role ?? 'assistant') as ConversationMessage['role'],
    content: message.content ?? message.Content ?? '',
    reasoning: message.reasoning ?? message.Reasoning,
    tool_call_id: message.tool_call_id ?? message.ToolCallId,
    reasoning_items:
      message.reasoning_items ??
      message.ReasoningItems?.flatMap((item) =>
        (item.Summary ?? []).map((summary) => ({ text: summary.Text ?? '' })),
      ),
    tool_calls:
      message.tool_calls ??
      message.ToolCalls?.map((toolCall) => ({
        id: toolCall.ID ?? '',
        name: toolCall.Name ?? '',
        arguments: toolCall.Arguments ?? '',
      })),
  }
}

export function normalizeRunTaskResult(
  result: Omit<RunTaskResult, 'final_message'> & {
    final_message: Partial<ConversationMessage> & { Role?: string; Content?: string }
  },
): RunTaskResult {
  return {
    ...result,
    final_message: normalizeConversationMessage(result.final_message),
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

export async function fetchConversationMessages(conversationId: string) {
  const messages = await request<
    Array<Partial<ConversationMessage> & { Role?: string; Content?: string; Reasoning?: string; ToolCallId?: string }>
  >(`/conversations/${conversationId}/messages`)
  return messages.map((message) => normalizeConversationMessage(message))
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
  return fetchTaskResult(taskId)
}

async function fetchTaskResult(taskId: string) {
  const response = await fetch(`${API_BASE}/tasks/${taskId}`)
  const payload = (await response.json()) as ApiEnvelope<
    TaskSnapshot & { result?: RunTaskResult; result_json?: RunTaskResult; error?: unknown }
  >
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
) {
  return new Promise<RunTaskResult>((resolve, reject) => {
    const stream = new EventSource(`${API_BASE}/tasks/${taskId}/events?after_seq=0`)
    let settled = false

    const close = () => {
      if (!settled) {
        settled = true
        stream.close()
      }
    }

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
        close()
        reject(error)
      }
    }

    const finish = async () => {
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
    stream.addEventListener('task.finished', handleEvent)
  
    stream.onerror = () => {
      close()
      reject(new Error('Task event stream disconnected'))
    }
  })
}
