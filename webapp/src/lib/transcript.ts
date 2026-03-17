import type { ConversationMessage, TaskStreamEvent, TranscriptEntry } from '../types/api'

function createEntryId(prefix: string) {
  return `${prefix}-${Math.random().toString(36).slice(2, 10)}`
}

function compactWhitespace(value: string) {
  return value.replace(/\s+/g, ' ').trim()
}

function summarizeReasoning(message: ConversationMessage) {
  if (message.reasoning && compactWhitespace(message.reasoning)) {
    return compactWhitespace(message.reasoning)
  }

  if (message.reasoning_items && message.reasoning_items.length > 0) {
    const text = message.reasoning_items
      .map((item) => (typeof item.text === 'string' ? item.text : ''))
      .filter(Boolean)
      .join(' ')
    return compactWhitespace(text)
  }

  return ''
}

function normalizeStreamMessage(
  message: Record<string, unknown>,
): Pick<ConversationMessage, 'content' | 'reasoning' | 'tool_calls'> {
  return {
    content: typeof message.content === 'string' ? message.content : typeof message.Content === 'string' ? String(message.Content) : '',
    reasoning:
      typeof message.reasoning === 'string'
        ? message.reasoning
        : typeof message.Reasoning === 'string'
          ? String(message.Reasoning)
          : '',
    tool_calls: Array.isArray(message.tool_calls)
      ? (message.tool_calls as ConversationMessage['tool_calls'])
      : Array.isArray(message.ToolCalls)
        ? (message.ToolCalls as Array<Record<string, unknown>>).map((toolCall) => ({
            id: String(toolCall.ID ?? ''),
            name: String(toolCall.Name ?? ''),
            arguments: typeof toolCall.Arguments === 'string' ? toolCall.Arguments : '',
          }))
        : [],
  }
}

export function summarizeToolResult(output: string) {
  const trimmed = compactWhitespace(output)
  if (!trimmed) {
    return 'No output'
  }

  try {
    const parsed = JSON.parse(trimmed) as Record<string, unknown>
    return Object.entries(parsed)
      .slice(0, 3)
      .map(([key, value]) => `${key}=${String(value)}`)
      .join(' ')
  } catch {
    return trimmed.length > 120 ? `${trimmed.slice(0, 117)}...` : trimmed
  }
}

export function buildTranscriptEntries(messages: ConversationMessage[]): TranscriptEntry[] {
  const entries: TranscriptEntry[] = []
  const toolNames = new Map<string, string>()

  for (const message of messages) {
	    if (message.role === 'system') {
	      if (message.content.trim()) {
	        entries.push({ id: createEntryId('error'), kind: 'error', title: 'Run failed', content: message.content })
	      }
	      continue
	    }

    if (message.role === 'user') {
      entries.push({ id: createEntryId('user'), kind: 'user', title: 'You', content: message.content })
      continue
    }

    if (message.role === 'assistant') {
      const reasoning = summarizeReasoning(message)
      if (reasoning) {
        entries.push({ id: createEntryId('reasoning'), kind: 'reasoning', title: 'Thinking', content: reasoning })
      }

      const toolCalls = message.tool_calls ?? []
      for (const toolCall of toolCalls) {
        toolNames.set(toolCall.id, toolCall.name)
      }

      if (toolCalls.length > 0) {
        for (const toolCall of toolCalls) {
          entries.push({
            id: createEntryId('tool'),
            kind: 'tool',
            title: toolCall.name,
            summary: 'Waiting for result...',
            tool_call_id: toolCall.id,
            status: 'running' as const,
          })
        }
      }

      if (message.content.trim()) {
        entries.push({ id: createEntryId('reply'), kind: 'reply', title: 'Reply', content: message.content })
      }
      continue
    }

    if (message.role === 'tool') {
      const toolCallId = message.tool_call_id ?? ''
      const existingIndex = entries.findIndex(
        (entry) => entry.kind === 'tool' && entry.tool_call_id === toolCallId,
      )
      if (existingIndex >= 0) {
        entries[existingIndex] = {
          ...entries[existingIndex],
          title: toolNames.get(toolCallId) ?? entries[existingIndex].title,
          summary: summarizeToolResult(message.content),
          status: 'done' as const,
        }
      } else {
        entries.push({
          id: createEntryId('tool'),
          kind: 'tool',
          title: toolNames.get(toolCallId) ?? 'Tool',
          summary: summarizeToolResult(message.content),
          tool_call_id: toolCallId,
          status: 'done' as const,
        })
      }
    }
  }

  return entries
}

function appendOrMerge(entries: TranscriptEntry[], incoming: TranscriptEntry) {
  const next = [...entries]
  const last = next[next.length - 1]
  if (last && last.kind === incoming.kind && incoming.kind !== 'tool') {
    last.content = `${last.content ?? ''}${incoming.content ?? ''}`
    return next
  }
  next.push(incoming)
  return next
}

export function updateTranscriptFromStreamEvent(
  entries: TranscriptEntry[],
  event: Partial<TaskStreamEvent>,
): TranscriptEntry[] {
  const payload = event.payload ?? {}

  if (event.type === 'log.message') {
    const kind = typeof payload.Kind === 'string' ? payload.Kind : ''
    if (kind === 'reasoning_delta') {
      return appendOrMerge(entries, {
        id: createEntryId('reasoning'),
        kind: 'reasoning',
        title: 'Thinking',
        content: String(payload.Reasoning ?? ''),
      })
    }

    if (kind === 'text_delta') {
      return appendOrMerge(entries, {
        id: createEntryId('reply'),
        kind: 'reply',
        title: 'Reply',
        content: String(payload.Text ?? ''),
      })
    }

    if (kind === 'completed' && payload.Message && typeof payload.Message === 'object') {
	      const message = normalizeStreamMessage(payload.Message as Record<string, unknown>)
	      let next = [...entries]
	      if ((message.reasoning ?? '').trim()) {
	        next = appendOrMerge(next, {
	          id: createEntryId('reasoning'),
	          kind: 'reasoning',
	          title: 'Thinking',
	          content: message.reasoning ?? '',
	        })
	      }
	      for (const toolCall of message.tool_calls ?? []) {
	        next.push({
	          id: createEntryId('tool'),
	          kind: 'tool',
	          title: toolCall.name,
	          summary: 'Running...',
	          tool_call_id: toolCall.id,
	          status: 'running' as const,
	        })
	      }
	      const content = message.content
	      if (!content) {
	        return next
	      }
	      const last = next[next.length - 1]
	      if (last?.kind === 'reply') {
	        last.content = content
        return next
      }
      next.push({ id: createEntryId('reply'), kind: 'reply', title: 'Reply', content })
      return next
    }
  }

  if (event.type === 'tool.started') {
      return [
        ...entries,
        {
          id: createEntryId('tool'),
          kind: 'tool',
          title: String(payload.ToolName ?? 'Tool'),
          summary: 'Running...',
          tool_call_id: String(payload.ToolCallID ?? ''),
          status: 'running' as const,
        },
      ] as TranscriptEntry[]
    }

  if (event.type === 'tool.finished') {
    const toolCallId = String(payload.ToolCallID ?? '')
    return entries.map((entry) => {
      if (entry.kind !== 'tool' || entry.tool_call_id !== toolCallId) {
        return entry
      }
      const err = payload.Err ? String(payload.Err) : ''
      return {
        ...entry,
        title: String(payload.ToolName ?? entry.title),
        summary: err || summarizeToolResult(String(payload.Output ?? '')),
        status: (err ? 'error' : 'done') as TranscriptEntry['status'],
      }
    }) as TranscriptEntry[]
  }

  if (event.type === 'task.failed') {
    return [
      ...entries,
      {
        id: createEntryId('error'),
        kind: 'error',
        title: 'Run failed',
        content: String(payload.error ?? 'Unknown error'),
      },
    ] as TranscriptEntry[]
  }

	if (event.type === 'task.finished') {
	  const status = String(payload.status ?? '')
	  if (status === 'failed' || status === 'cancelled') {
	    const nestedError = payload.error && typeof payload.error === 'object' ? String((payload.error as Record<string, unknown>).message ?? '') : ''
	    const content = nestedError || String(payload.error ?? `Task ${status}`)
	    return [
	      ...entries,
	      { id: createEntryId('error'), kind: 'error', title: 'Run failed', content },
	    ] as TranscriptEntry[]
	  }
	}

  return entries
}
