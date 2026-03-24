import type {
  ConversationMessage,
  TaskStreamEvent,
  TranscriptEntry,
  TranscriptEntryDetail,
  TranscriptEntryDetailBlock,
  TranscriptTokenUsage,
} from '../types/api'

import { normalizeTranscriptTokenUsage } from './api'

function createEntryId(prefix: string) {
  return `${prefix}-${Math.random().toString(36).slice(2, 10)}`
}

function compactWhitespace(value: string) {
  return value.replace(/\s+/g, ' ').trim()
}

function isRenderableSystemMessage(message: ConversationMessage) {
  if (message.role !== 'system') {
    return false
  }

  return message.provider_data?.system_message?.visible_to_user === true
}

function previewText(value: string, maxLength = 64) {
  const normalized = compactWhitespace(value)
  if (!normalized) {
    return ''
  }
  if (normalized.length <= maxLength) {
    return normalized
  }
  return `${normalized.slice(0, maxLength - 3)}...`
}

function summarizeReasoning(message: ConversationMessage) {
  if (message.reasoning && compactWhitespace(message.reasoning)) {
    return compactWhitespace(message.reasoning)
  }

  if (message.reasoning_items && message.reasoning_items.length > 0) {
    return compactWhitespace(
      message.reasoning_items
        .map((item) => (typeof item.text === 'string' ? item.text : ''))
        .filter(Boolean)
        .join(' '),
    )
  }

  return ''
}

function normalizeStreamMessage(
  message: Record<string, unknown>,
): Pick<ConversationMessage, 'role' | 'content' | 'provider_id' | 'model_id' | 'reasoning' | 'tool_call_id' | 'tool_calls'> {
  const rawToolCalls = Array.isArray(message.tool_calls)
    ? message.tool_calls
    : Array.isArray(message.toolCalls)
      ? message.toolCalls
      : Array.isArray(message.ToolCalls)
        ? message.ToolCalls
        : []

  return {
    role:
      typeof message.role === 'string'
        ? (message.role as ConversationMessage['role'])
        : typeof message.Role === 'string'
          ? (String(message.Role) as ConversationMessage['role'])
          : 'assistant',
    content: typeof message.content === 'string' ? message.content : typeof message.Content === 'string' ? String(message.Content) : '',
    provider_id:
      typeof message.provider_id === 'string'
        ? message.provider_id
        : typeof message.providerId === 'string'
          ? message.providerId
          : typeof message.ProviderID === 'string'
            ? String(message.ProviderID)
            : undefined,
    model_id:
      typeof message.model_id === 'string'
        ? message.model_id
        : typeof message.modelId === 'string'
          ? message.modelId
          : typeof message.ModelID === 'string'
            ? String(message.ModelID)
            : undefined,
    reasoning:
      typeof message.reasoning === 'string'
        ? message.reasoning
        : typeof message.Reasoning === 'string'
          ? String(message.Reasoning)
          : '',
    tool_call_id:
      typeof message.tool_call_id === 'string'
        ? message.tool_call_id
        : typeof message.toolCallId === 'string'
          ? message.toolCallId
        : typeof message.ToolCallId === 'string'
          ? String(message.ToolCallId)
          : typeof message.ToolCallID === 'string'
            ? String(message.ToolCallID)
            : '',
    tool_calls: (rawToolCalls as Array<Record<string, unknown>>).map((toolCall) => ({
      id: String(toolCall.id ?? toolCall.ID ?? ''),
      name: String(toolCall.name ?? toolCall.Name ?? ''),
      arguments:
        typeof toolCall.arguments === 'string'
          ? toolCall.arguments
          : typeof toolCall.Arguments === 'string'
            ? toolCall.Arguments
            : '',
    })),
  }
}

function makeBlocks(argumentsText?: string, resultText?: string, loading?: boolean): TranscriptEntryDetailBlock[] {
  const blocks: TranscriptEntryDetailBlock[] = []
  if (argumentsText && argumentsText.trim()) {
    blocks.push({ label: 'Params', value: argumentsText })
  }
  if (resultText && resultText.trim()) {
    blocks.push({ label: 'Result', value: resultText, loading })
  }
  return blocks
}

function makeReasoningEntry(content: string, loading = true): TranscriptEntry {
  return {
    id: createEntryId('reasoning'),
    kind: 'reasoning',
    title: '思考',
    details: [
      {
        label: '思考',
        preview: previewText(content),
        collapsed: true,
        loading,
        blocks: [{ label: 'Trace', value: content, loading }],
      },
    ],
  }
}

function makeToolGroupEntry(groupKey: string, details: TranscriptEntryDetail[]): TranscriptEntry {
  return {
    id: createEntryId('tool-group'),
    kind: 'tool',
    title: details.length > 1 ? `工具调用 (${details.length})` : '工具调用',
    details,
    status: details.some((detail) => detail.loading) ? 'running' : 'done',
    group_key: groupKey,
  }
}

function makeToolDetail(input: {
  toolCallId?: string
  name: string
  argumentsText?: string
  resultText?: string
  loading?: boolean
  error?: boolean
}): TranscriptEntryDetail {
  const preview = input.loading ? 'Running' : previewText(input.resultText ?? '')
  return {
    key: input.toolCallId || `${input.name}-${Math.random().toString(36).slice(2, 8)}`,
    label: input.name || 'Tool',
    preview: preview || (input.error ? 'Failed' : 'Ready'),
    collapsed: true,
    loading: input.loading,
    blocks: makeBlocks(input.argumentsText, input.resultText || (input.loading ? 'Running...' : ''), input.loading),
  }
}

function updateReasoningEntry(entry: TranscriptEntry, content: string, loading = true) {
  return {
    ...entry,
    details: [
      {
        label: '思考',
        preview: previewText(content),
        collapsed: true,
        loading,
        blocks: [{ label: 'Trace', value: content, loading }],
      },
    ],
  }
}

function upsertReasoning(entries: TranscriptEntry[], content: string, loading = true) {
  const next = [...entries]
  const last = next[next.length - 1]
  if (last?.kind === 'reasoning') {
    const previous = last.details?.[0]?.blocks?.[0]?.value ?? ''
    next[next.length - 1] = updateReasoningEntry(last, `${previous}${content}`, loading)
    return next
  }
  next.push(makeReasoningEntry(content, loading))
  return next
}

function completeLatestReasoning(entries: TranscriptEntry[]) {
  const next = [...entries]
  for (let index = next.length - 1; index >= 0; index -= 1) {
    const entry = next[index]
    if (entry.kind !== 'reasoning') {
      continue
    }

    const content = entry.details?.[0]?.blocks?.[0]?.value ?? ''
    next[index] = updateReasoningEntry(entry, content, false)
    return next
  }

  return next
}

function findLatestReplyIndex(entries: TranscriptEntry[]) {
  for (let index = entries.length - 1; index >= 0; index -= 1) {
    if (entries[index]?.kind === 'reply') {
      return index
    }
  }

  return -1
}

function findLatestReasoningBeforeReply(entries: TranscriptEntry[], replyIndex: number) {
  for (let index = replyIndex - 1; index >= 0; index -= 1) {
    const entry = entries[index]
    if (entry.kind === 'reasoning') {
      return index
    }
    if (entry.kind === 'reply' || entry.kind === 'user' || entry.kind === 'error') {
      break
    }
  }

  return -1
}

function upsertReasoningBeforeReply(entries: TranscriptEntry[], replyIndex: number, content: string) {
  const next = [...entries]
  const reasoningIndex = findLatestReasoningBeforeReply(next, replyIndex)

  if (reasoningIndex >= 0) {
    next[reasoningIndex] = updateReasoningEntry(next[reasoningIndex], content, false)
    return next
  }

  next.splice(replyIndex, 0, makeReasoningEntry(content, false))
  return next
}

function attachReplyMetaAtIndex(
  entries: TranscriptEntry[],
  replyIndex: number,
  meta: Pick<TranscriptEntry, 'provider_id' | 'model_id' | 'token_usage'>,
) {
  if (replyIndex < 0) {
    return entries
  }

  const reply = entries[replyIndex]
  if (!reply || reply.kind !== 'reply') {
    return entries
  }

  if (!meta.provider_id && !meta.model_id && !meta.token_usage) {
    return entries
  }

  const next = [...entries]
  next[replyIndex] = {
    ...reply,
    ...(meta.provider_id ? { provider_id: meta.provider_id } : {}),
    ...(meta.model_id ? { model_id: meta.model_id } : {}),
    ...(meta.token_usage ? { token_usage: meta.token_usage } : {}),
  }
  return next
}

function stopAllLoading(entries: TranscriptEntry[], toolStatus: TranscriptEntry['status'] = 'done') {
  return entries.map((entry) => {
    const details = (entry.details ?? []).map((detail) => ({
      ...detail,
      loading: false,
      blocks: (detail.blocks ?? []).map((block) => ({
        ...block,
        ...(typeof block.loading === 'boolean' ? { loading: false } : {}),
      })),
    }))

    if (entry.kind === 'tool') {
      return {
        ...entry,
        details,
        status: entry.status === 'running' ? toolStatus : entry.status,
      }
    }

    if (entry.kind === 'reasoning') {
      return {
        ...entry,
        details,
      }
    }

    return entry.details ? { ...entry, details } : entry
  })
}

function appendReply(entries: TranscriptEntry[], content: string) {
  const next = completeLatestReasoning(entries)
  const last = next[next.length - 1]
  if (last?.kind === 'reply') {
    last.content = content
    return next
  }
  next.push({ id: createEntryId('reply'), kind: 'reply', title: '', content })
  return next
}

function findToolGroupIndex(entries: TranscriptEntry[], groupKey: string) {
  return entries.findIndex((entry) => entry.kind === 'tool' && entry.group_key === groupKey)
}

function findToolEntryByCallId(entries: TranscriptEntry[], toolCallId: string) {
  if (!toolCallId) {
    return null
  }

  for (let entryIndex = entries.length - 1; entryIndex >= 0; entryIndex -= 1) {
    const entry = entries[entryIndex]
    if (entry.kind !== 'tool') {
      continue
    }

    const detailIndex = (entry.details ?? []).findIndex((detail) => detail.key === toolCallId)
    if (detailIndex >= 0) {
      return { entryIndex, detailIndex }
    }
  }

  return null
}

function findToolDetailIndex(details: TranscriptEntryDetail[], toolCallId: string, name: string) {
  return details.findIndex((detail) => {
    if (toolCallId && detail.key === toolCallId) {
      return true
    }
    if (!toolCallId && detail.label === name && detail.loading) {
      return true
    }
    return false
  })
}

function upsertToolInGroup(
  entries: TranscriptEntry[],
  input: {
    groupKey: string
    toolCallId?: string
    name: string
    argumentsText?: string
    resultText?: string
    loading?: boolean
    error?: boolean
  },
) {
  const next = completeLatestReasoning(entries)
  const existingLocation = findToolEntryByCallId(next, input.toolCallId ?? '')
  const groupIndex = existingLocation?.entryIndex ?? findToolGroupIndex(next, input.groupKey)
  const detail = makeToolDetail(input)

  if (groupIndex < 0) {
    next.push(makeToolGroupEntry(input.groupKey, [detail]))
    return next
  }

  const group = next[groupIndex]
  const currentDetails = [...(group.details ?? [])]
  const detailIndex = existingLocation?.detailIndex ?? findToolDetailIndex(currentDetails, input.toolCallId ?? '', input.name)

  if (detailIndex >= 0) {
    const existing = currentDetails[detailIndex]
    currentDetails[detailIndex] = makeToolDetail({
      toolCallId: input.toolCallId || existing.key,
      name: input.name || existing.label,
      argumentsText: input.argumentsText || existing.blocks?.find((block) => block.label === 'Params')?.value,
      resultText: input.resultText || existing.blocks?.find((block) => block.label === 'Result')?.value,
      loading: input.loading,
      error: input.error,
    })
  } else {
    currentDetails.push(detail)
  }

  next[groupIndex] = makeToolGroupEntry(input.groupKey, currentDetails)
  return next
}

function updatePendingToolGroupFromMessage(entries: TranscriptEntry[], groupKey: string, toolCalls: NonNullable<ConversationMessage['tool_calls']>) {
  let next = [...entries]
  for (const toolCall of toolCalls) {
    next = upsertToolInGroup(next, {
      groupKey,
      toolCallId: toolCall.id,
      name: toolCall.name,
      argumentsText: toolCall.arguments,
      resultText: 'Running...',
      loading: true,
    })
  }
  return next
}

function attachToolResult(entries: TranscriptEntry[], toolCallId: string, content: string, fallbackName = 'Tool') {
  const next = [...entries]
  for (let i = next.length - 1; i >= 0; i--) {
    const entry = next[i]
    if (entry.kind !== 'tool') {
      continue
    }
    const details = [...(entry.details ?? [])]
    const detailIndex = findToolDetailIndex(details, toolCallId, fallbackName)
    if (detailIndex >= 0) {
      const existing = details[detailIndex]
      details[detailIndex] = makeToolDetail({
        toolCallId: toolCallId || existing.key,
        name: existing.label,
        argumentsText: existing.blocks?.find((block) => block.label === 'Params')?.value,
        resultText: content,
        loading: false,
      })
      next[i] = makeToolGroupEntry(entry.group_key || entry.id, details)
      return next
    }
  }

  return upsertToolInGroup(next, {
    groupKey: `persisted-${toolCallId || next.length}`,
    toolCallId,
    name: fallbackName,
    resultText: content,
    loading: false,
  })
}

function applyConversationMessage(
  entries: TranscriptEntry[],
  message: ConversationMessage,
  options?: {
    groupKey?: string
    toolNames?: Map<string, string>
  },
) {
  let next = [...entries]

  if (message.role === 'system') {
    const content = compactWhitespace(message.content)
    if (content && isRenderableSystemMessage(message)) {
      next.push({ id: createEntryId('error'), kind: 'error', title: '运行失败', content })
    }
    return next
  }

  if (message.role === 'user') {
    next.push({ id: createEntryId('user'), kind: 'user', title: '', content: message.content })
    return next
  }

  if (message.role === 'tool') {
    return attachToolResult(next, message.tool_call_id ?? '', message.content, options?.toolNames?.get(message.tool_call_id ?? '') ?? 'Tool')
  }

  const reasoning = summarizeReasoning(message)
  if (reasoning) {
    next = upsertReasoning(next, reasoning)
  }

  const toolCalls = message.tool_calls ?? []
  for (const toolCall of toolCalls) {
    options?.toolNames?.set(toolCall.id, toolCall.name)
  }
  if (toolCalls.length > 0 && options?.groupKey) {
    next = updatePendingToolGroupFromMessage(next, options.groupKey, toolCalls)
  }

  if (message.content.trim()) {
    next = appendReply(next, message.content)
    next = attachReplyMetaToLatestReply(next, {
      provider_id: message.provider_id,
      model_id: message.model_id,
      token_usage: message.usage,
    })
  }

  return next
}

function applyCompletedAssistantMessage(
  entries: TranscriptEntry[],
  message: ConversationMessage,
  options?: {
    groupKey?: string
    toolNames?: Map<string, string>
  },
) {
  let next = [...entries]
  const reasoning = summarizeReasoning(message)
  const content = message.content.trim()
  const toolCalls = message.tool_calls ?? []
  const latestReplyIndex = content ? findLatestReplyIndex(next) : -1
  const hasMatchingLatestReply =
    latestReplyIndex >= 0 && next[latestReplyIndex]?.kind === 'reply' && (next[latestReplyIndex].content ?? '') === message.content

  if (reasoning) {
    next = hasMatchingLatestReply ? upsertReasoningBeforeReply(next, latestReplyIndex, reasoning) : upsertReasoning(next, reasoning, false)
  }

  for (const toolCall of toolCalls) {
    options?.toolNames?.set(toolCall.id, toolCall.name)
  }
  if (toolCalls.length > 0 && options?.groupKey) {
    next = updatePendingToolGroupFromMessage(next, options.groupKey, toolCalls)
  }

  if (!content) {
    return next
  }

  if (hasMatchingLatestReply) {
    return attachReplyMetaAtIndex(next, findLatestReplyIndex(next), {
      provider_id: message.provider_id,
      model_id: message.model_id,
      token_usage: message.usage,
    })
  }

  next = appendReply(next, message.content)
  return attachReplyMetaToLatestReply(next, {
    provider_id: message.provider_id,
    model_id: message.model_id,
    token_usage: message.usage,
  })
}

function latestToolFailureMessage(entries: TranscriptEntry[]) {
  for (let i = entries.length - 1; i >= 0; i -= 1) {
    const entry = entries[i]
    if (entry.kind !== 'tool') {
      continue
    }

    const details = entry.details ?? []
    for (let j = details.length - 1; j >= 0; j -= 1) {
      const detail = details[j]
      const resultBlock = detail.blocks?.find((block) => block.label === 'Result')
      if (resultBlock?.value) {
        return compactWhitespace(resultBlock.value)
      }
    }
  }

  return ''
}

export function attachTokenUsageToLatestReply(entries: TranscriptEntry[], usage: TranscriptTokenUsage | undefined) {
  return attachReplyMetaToLatestReply(entries, { token_usage: usage })
}

export function attachReplyMetaToLatestReply(
  entries: TranscriptEntry[],
  meta: Pick<TranscriptEntry, 'provider_id' | 'model_id' | 'token_usage'>,
) {
  if (!meta.provider_id && !meta.model_id && !meta.token_usage) {
    return entries
  }

  const next = [...entries]
  for (let index = next.length - 1; index >= 0; index -= 1) {
    const entry = next[index]
    if (entry.kind !== 'reply') {
      continue
    }

    next[index] = {
      ...entry,
      ...(meta.provider_id ? { provider_id: meta.provider_id } : {}),
      ...(meta.model_id ? { model_id: meta.model_id } : {}),
      ...(meta.token_usage ? { token_usage: meta.token_usage } : {}),
    }
    return next
  }

  return next
}

export function summarizeToolResult(output: string) {
  return previewText(output, 120) || 'No output'
}

export function buildTranscriptEntries(messages: ConversationMessage[]): TranscriptEntry[] {
  let entries: TranscriptEntry[] = []
  const toolNames = new Map<string, string>()
  let assistantToolGroupIndex = 0

  for (const message of messages) {
    if (message.role === 'assistant' && (message.tool_calls ?? []).length > 0) {
      assistantToolGroupIndex += 1
    }

    entries = applyConversationMessage(entries, message, {
      groupKey: message.role === 'assistant' && (message.tool_calls ?? []).length > 0 ? `persisted-step-${assistantToolGroupIndex}` : undefined,
      toolNames,
    })
  }

  return entries
}

export function updateTranscriptFromStreamEvent(entries: TranscriptEntry[], event: Partial<TaskStreamEvent>): TranscriptEntry[] {
  const payload = event.payload ?? {}
  const step = String(payload.Step ?? 'live')
  const groupKey = `step-${step}`

  if (event.type === 'log.message') {
    const kind = typeof payload.Kind === 'string' ? payload.Kind : ''
    if (kind === 'reasoning_delta') {
      return upsertReasoning(entries, String(payload.Reasoning ?? ''))
    }

    if (kind === 'text_delta') {
      const text = String(payload.Text ?? '')
      const next = completeLatestReasoning(entries)
      const last = next[next.length - 1]
      if (last?.kind === 'reply') {
        last.content = `${last.content ?? ''}${text}`
        return next
      }
      next.push({ id: createEntryId('reply'), kind: 'reply', title: '', content: text })
      return next
    }

    if (kind === 'usage') {
      return attachTokenUsageToLatestReply(
        entries,
        normalizeTranscriptTokenUsage(payload.Usage ?? payload.usage ?? payload.token_usage ?? payload.TokenUsage),
      )
    }

    if (kind === 'tool_call_delta') {
      const toolCall = payload.ToolCall as Record<string, unknown> | undefined
      if (!toolCall) {
        return entries
      }
      return upsertToolInGroup(entries, {
        groupKey,
        toolCallId: String(toolCall.id ?? toolCall.ID ?? ''),
        name: String(toolCall.name ?? toolCall.Name ?? 'Tool'),
        argumentsText:
          typeof toolCall.arguments === 'string'
            ? toolCall.arguments
            : typeof toolCall.Arguments === 'string'
              ? toolCall.Arguments
              : '',
        resultText: 'Running...',
        loading: true,
      })
    }

    if (kind === 'completed' && payload.Message && typeof payload.Message === 'object') {
      const message = normalizeStreamMessage(payload.Message as Record<string, unknown>)
      if (message.role === 'assistant') {
        return applyCompletedAssistantMessage(entries, message, { groupKey })
      }
      return applyConversationMessage(entries, message, { groupKey })
    }
  }

  if (event.type === 'tool.started') {
    return upsertToolInGroup(entries, {
      groupKey,
      toolCallId: String(payload.tool_call_id ?? payload.toolCallId ?? payload.ToolCallId ?? payload.ToolCallID ?? ''),
      name: String(payload.tool_name ?? payload.toolName ?? payload.ToolName ?? 'Tool'),
      argumentsText: typeof payload.Arguments === 'string' ? String(payload.Arguments) : '',
      resultText: 'Running...',
      loading: true,
    })
  }

  if (event.type === 'tool.finished') {
    const err = payload.Err ? String(payload.Err) : ''
    return upsertToolInGroup(entries, {
      groupKey,
      toolCallId: String(payload.tool_call_id ?? payload.toolCallId ?? payload.ToolCallId ?? payload.ToolCallID ?? ''),
      name: String(payload.tool_name ?? payload.toolName ?? payload.ToolName ?? 'Tool'),
      argumentsText: typeof payload.Arguments === 'string' ? String(payload.Arguments) : '',
      resultText: err || String(payload.Output ?? ''),
      loading: false,
      error: Boolean(err),
    })
  }

  if (event.type === 'task.failed') {
    const settledEntries = stopAllLoading(entries, 'error')
    const message = String(payload.error ?? 'Unknown error')
    if (compactWhitespace(message) && compactWhitespace(message) === latestToolFailureMessage(settledEntries)) {
      return settledEntries
    }
    return [
      ...settledEntries,
      { id: createEntryId('error'), kind: 'error', title: '运行失败', content: message },
    ]
  }

  if (event.type === 'task.finished') {
    const status = String(payload.status ?? '')
    const terminalToolStatus = status === 'failed' || status === 'cancelled' ? 'error' : 'done'
    const settledEntries = attachTokenUsageToLatestReply(
      stopAllLoading(entries, terminalToolStatus),
      normalizeTranscriptTokenUsage(payload.usage ?? payload.token_usage ?? payload.Usage ?? payload.TokenUsage),
    )
    if (status === 'failed' || status === 'cancelled') {
      const nestedError = payload.error && typeof payload.error === 'object' ? String((payload.error as Record<string, unknown>).message ?? '') : ''
      return [
        ...settledEntries,
        { id: createEntryId('error'), kind: 'error', title: '运行失败', content: nestedError || String(payload.error ?? `Task ${status}`) },
      ]
    }
    return settledEntries
  }

  return entries
}
