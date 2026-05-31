import type {
  ApprovalDecision,
  AttachmentRef,
  ConversationMessage,
  ImagePartialPreview,
  ImageToolResult,
  InteractionRecord,
  TaskStreamEvent,
  ToolApproval,
  TranscriptEntry,
  TranscriptEntryDetail,
  TranscriptEntryDetailBlock,
  TranscriptTokenUsage,
} from '../types/api'

import {
  normalizeConversationMessage,
  normalizeInteractionRecord,
  normalizeMemoryCompressionSnapshot,
  normalizeMemoryContextSnapshot,
  normalizeToolApproval,
  normalizeTranscriptTokenUsage,
} from './api'

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

/**
 * Returns true if the string looks like raw JSON data (object or array literal).
 * Such strings should not be shown as preview text to avoid leaking JSON schema.
 */
export function looksLikeJson(value: string): boolean {
  const trimmed = value.trim()
  // Detect complete JSON objects/arrays
  if ((trimmed.startsWith('{') && trimmed.endsWith('}')) || (trimmed.startsWith('[') && trimmed.endsWith(']'))) {
    return true
  }
  // Also detect truncated JSON that starts with { or [ (e.g. from failed parse or truncated content)
  if ((trimmed.startsWith('{') || trimmed.startsWith('[')) && /["':,\[\]{}]/.test(trimmed.slice(1, 20))) {
    return true
  }
  return false
}

function previewText(value: string, maxLength = 64) {
  const normalized = compactWhitespace(value)
  if (!normalized) {
    return ''
  }
  // Do not expose JSON-formatted data as preview summary
  if (looksLikeJson(normalized)) {
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function findLastTranscriptEntry(entries: TranscriptEntry[], predicate: (entry: TranscriptEntry) => boolean) {
  const index = findLastTranscriptEntryIndex(entries, predicate)
  return index >= 0 ? entries[index] : undefined
}

function findLastTranscriptEntryIndex(entries: TranscriptEntry[], predicate: (entry: TranscriptEntry) => boolean) {
  for (let index = entries.length - 1; index >= 0; index -= 1) {
    if (predicate(entries[index])) {
      return index
    }
  }
  return -1
}

/**
 * Safely convert any value to a displayable string.
 * - strings are returned as-is
 * - objects/arrays are JSON-stringified (pretty-printed)
 * - null/undefined become empty string
 * - other primitives use String()
 */
function safeStringify(value: unknown): string {
  if (value === null || value === undefined) {
    return ''
  }
  if (typeof value === 'string') {
    return value
  }
  if (typeof value === 'object') {
    try {
      return JSON.stringify(value, null, 2)
    } catch {
      return '[Unserializable Object]'
    }
  }
  return String(value)
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) {
      return value.trim()
    }
  }
  return ''
}

function optionalNumber(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value
  }
  if (typeof value === 'string' && value.trim() && Number.isFinite(Number(value))) {
    return Number(value)
  }
  return undefined
}

function isImageToolName(name: string) {
  return name === 'generate_image' || name === 'edit_image'
}

function parseImageToolResult(content: string, actualToolName: string): { result: ImageToolResult; attachments: AttachmentRef[] } | undefined {
  if (!isImageToolName(actualToolName)) {
    return undefined
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(content)
  } catch {
    return undefined
  }

  if (!isRecord(parsed)) {
    return undefined
  }

  const tool = firstString(parsed.tool, parsed.Tool)
  if (tool !== actualToolName) {
    return undefined
  }

  const rawImages = parsed.images ?? parsed.Images
  if (!Array.isArray(rawImages)) {
    return undefined
  }

  const operation = firstString(parsed.operation, parsed.Operation) || (tool === 'edit_image' ? 'edit' : 'generate')
  const result = parsed as unknown as ImageToolResult
  const attachments = rawImages.flatMap((item): AttachmentRef[] => {
    if (!isRecord(item)) {
      return []
    }

    const id = firstString(item.attachment_id, item.attachmentId, item.AttachmentID, item.id, item.ID)
    if (!id) {
      return []
    }

    const fileName = firstString(item.file_name, item.fileName, item.FileName) || id
    const mimeType = firstString(item.mime_type, item.mimeType, item.MimeType) || 'image/png'
    const preview = firstString(item.revised_prompt, item.revisedPrompt, item.RevisedPrompt) || operation
    const contentUrl = firstString(item.content_url, item.contentUrl, item.ContentURL)
    const sizeBytes = optionalNumber(item.size_bytes ?? item.sizeBytes ?? item.SizeBytes)
    const width = optionalNumber(item.width ?? item.Width)
    const height = optionalNumber(item.height ?? item.Height)
    return [
      {
        id,
        file_name: fileName,
        mime_type: mimeType,
        kind: 'image',
        status: 'sent',
        ...(preview ? { preview_text: preview } : {}),
        ...(contentUrl ? { content_url: contentUrl } : {}),
        ...(sizeBytes !== undefined ? { size_bytes: sizeBytes } : {}),
        ...(width !== undefined ? { width } : {}),
        ...(height !== undefined ? { height } : {}),
      },
    ]
  })

  if (attachments.length === 0) {
    return undefined
  }

  return { result, attachments }
}

function makeImagePartialPreview(payload: Record<string, unknown>): ImagePartialPreview | undefined {
  const b64 = firstString(payload.B64JSON, payload.b64_json, payload.b64JSON, payload.B64Json)
  if (!b64) {
    return undefined
  }

  const mimeType = firstString(payload.MimeType, payload.mime_type, payload.mimeType) || 'image/png'
  return {
    tool: firstString(payload.Tool, payload.tool) || undefined,
    operation: firstString(payload.Operation, payload.operation) || undefined,
    mime_type: mimeType,
    data_url: `data:${mimeType};base64,${b64}`,
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

function makeApprovalEntry(approval: ToolApproval): TranscriptEntry {
  return {
    id: createEntryId('approval'),
    kind: 'approval',
    title: approval.status === 'pending' ? '等待审批' : '审批已处理',
    approval,
    status: approval.status === 'pending' ? 'running' : approval.status === 'approved' ? 'done' : 'error',
  }
}

function makeQuestionInteractionEntry(interaction: InteractionRecord): TranscriptEntry {
  return {
    id: createEntryId('interaction-question'),
    kind: 'question',
    title: interaction.status === 'pending' ? '等待回答' : '已回答问题',
    question_interaction: interaction,
  }
}

function findQuestionInteractionEntryIndex(entries: TranscriptEntry[], interactionId: string) {
  return entries.findIndex((entry) => entry.kind === 'question' && entry.question_interaction?.id === interactionId)
}

function upsertQuestionInteractionEntry(entries: TranscriptEntry[], interaction: InteractionRecord) {
  const next = [...entries]
  const index = findQuestionInteractionEntryIndex(next, interaction.id)
  const entry = makeQuestionInteractionEntry(interaction)
  if (index >= 0) {
    next[index] = {
      ...next[index],
      ...entry,
    }
    return next
  }
  next.push(entry)
  return next
}

export function buildApprovalStreamEvent(
  approval: ToolApproval,
  options?: {
    type?: 'approval.requested' | 'approval.resolved'
    decision?: ApprovalDecision
  },
): Partial<TaskStreamEvent> {
  const type = options?.type ?? (approval.status === 'pending' ? 'approval.requested' : 'approval.resolved')

  return {
    type,
    payload: {
      approval_id: approval.id,
      task_id: approval.task_id,
      conversation_id: approval.conversation_id,
      step: approval.step_index,
      tool_call_id: approval.tool_call_id,
      tool_name: approval.tool_name,
      arguments_summary: approval.arguments_summary,
      risk_level: approval.risk_level,
      reason: approval.reason,
      status: approval.status,
      decision: approval.decision ?? options?.decision,
      decision_reason: approval.decision_reason,
      decision_by: approval.decision_by,
      decision_at: approval.decision_at,
      created_at: approval.created_at,
      updated_at: approval.updated_at,
    },
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

/**
 * Extract the skill name from using_skills tool call arguments.
 */
function extractSkillName(argumentsText?: string): string {
  if (!argumentsText) return ''
  try {
    const args = JSON.parse(argumentsText) as Record<string, unknown>
    return String(args.name ?? '')
  } catch {
    return ''
  }
}

/**
 * Create or update a simplified skill usage entry.
 * Shows just the skill name without raw JSON arguments or full skill.md content.
 */
function upsertSkillEntry(entries: TranscriptEntry[], input: {
  toolCallId: string
  skillName: string
  loading: boolean
  error?: boolean
}): TranscriptEntry[] {
  const next = completeLatestReasoning(entries)
  const entryId = `skill-${input.toolCallId}`

  // Find existing skill entry by toolCallId
  const existingIndex = next.findIndex(
    (e) => e.id === entryId || (e.kind === 'tool' && e.group_key === `skill-${input.toolCallId}`),
  )

  const status = input.loading ? 'running' : input.error ? 'error' : 'done'
  const preview = input.loading ? '加载中...' : input.error ? '加载失败' : '已加载'
  const entry: TranscriptEntry = {
    id: entryId,
    kind: 'tool',
    title: '使用技能',
    details: [{
      key: input.toolCallId,
      label: input.skillName || 'Skill',
      preview,
      collapsed: true,
      loading: input.loading,
    }],
    status,
    group_key: `skill-${input.toolCallId}`,
  }

  if (existingIndex >= 0) {
    next[existingIndex] = entry
    return next
  }
  next.push(entry)
  return next
}

/**
 * Generate a short preview for a completed tool call based on its arguments.
 * Extracts the most meaningful field (path, command, query, url, etc.) to show
 * a one-line summary in the collapsed state.
 */
function toolArgsPreview(name: string, argumentsText?: string): string {
  if (!argumentsText || !argumentsText.trim()) return ''
  const trimmed = argumentsText.trim()
  if (!trimmed.startsWith('{')) return previewText(trimmed, 80)

  try {
    const args = JSON.parse(trimmed) as Record<string, unknown>

    // Try common fields that provide a meaningful summary
    const path = args.path || args.file_path || args.filename || ''
    const command = args.command || ''
    const query = args.query || args.pattern || ''
    const url = args.url || ''

    if (name === 'exec_command' && command) {
      const cmdArgs = Array.isArray(args.args) ? (args.args as unknown[]).map(String).join(' ') : ''
      const full = cmdArgs ? `${command} ${cmdArgs}` : String(command)
      return full.length > 80 ? `$ ${full.slice(0, 77)}...` : `$ ${full}`
    }
    if (path) return String(path)
    if (url) return String(url)
    if (query) return String(query).length > 60 ? `"${String(query).slice(0, 57)}..."` : `"${query}"`
    if (command) return `$ ${command}`

    // Fallback: show first string value
    for (const val of Object.values(args)) {
      if (typeof val === 'string' && val.trim()) {
        return previewText(val, 80)
      }
    }
  } catch {
    // Not valid JSON, fall through
  }
  return previewText(trimmed, 80)
}

function makeToolDetail(input: {
  toolCallId?: string
  name: string
  argumentsText?: string
  resultText?: string
  loading?: boolean
  error?: boolean
}): TranscriptEntryDetail {
  let preview: string
  if (input.loading) {
    preview = 'Running'
  } else if (input.error) {
    preview = 'Failed'
  } else {
    preview = toolArgsPreview(input.name, input.argumentsText)
  }
  return {
    key: input.toolCallId || `${input.name}-${Math.random().toString(36).slice(2, 8)}`,
    label: input.name || 'Tool',
    preview,
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
    if (isReusableTextReply(entries[index])) {
      return index
    }
  }

  return -1
}

function hasReplyAttachments(entry: TranscriptEntry | undefined) {
  return Boolean(entry?.attachments && entry.attachments.length > 0)
}

function isStandaloneAttachmentReply(entry: TranscriptEntry | undefined) {
  return entry?.kind === 'reply' && !entry.image_preview && hasReplyAttachments(entry) && !compactWhitespace(entry.content ?? '')
}

function isReusableTextReply(entry: TranscriptEntry | undefined) {
  return entry?.kind === 'reply' && !entry.image_preview && !isStandaloneAttachmentReply(entry)
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

function appendReply(entries: TranscriptEntry[], content: string, attachments?: ConversationMessage['attachments']) {
  const next = completeLatestReasoning(entries)
  const last = next[next.length - 1]
  if (isReusableTextReply(last)) {
    last.content = content
    if (attachments !== undefined) {
      last.attachments = attachments.length > 0 ? attachments : undefined
    }
    return next
  }
  next.push({ id: createEntryId('reply'), kind: 'reply', title: '', content, ...(attachments && attachments.length > 0 ? { attachments } : {}) })
  return next
}

function upsertImagePartialPreview(entries: TranscriptEntry[], imagePreview: ImagePartialPreview) {
  const next = completeLatestReasoning(entries)
  for (let index = next.length - 1; index >= 0; index -= 1) {
    const entry = next[index]
    if (entry.kind === 'reply' && entry.image_preview) {
      next[index] = {
        ...entry,
        status: 'running',
        image_preview: imagePreview,
      }
      return next
    }
    if (entry.kind === 'user' || entry.kind === 'error' || entry.kind === 'approval' || entry.kind === 'question') {
      break
    }
  }

  next.push({ id: createEntryId('reply'), kind: 'reply', title: '', status: 'running', image_preview: imagePreview })
  return next
}

function upsertImageAttachmentsReply(entries: TranscriptEntry[], attachments: AttachmentRef[]) {
  const next = completeLatestReasoning(entries)
  for (let index = next.length - 1; index >= 0; index -= 1) {
    const entry = next[index]
    if (entry.kind === 'reply' && entry.image_preview) {
      next[index] = {
        ...entry,
        status: 'done',
        attachments,
        image_preview: undefined,
      }
      return next
    }
    if (entry.kind === 'user' || entry.kind === 'error' || entry.kind === 'approval' || entry.kind === 'question') {
      break
    }
  }

  next.push({ id: createEntryId('reply'), kind: 'reply', title: '', content: '', status: 'done', attachments })
  return next
}

function findToolGroupIndex(entries: TranscriptEntry[], groupKey: string) {
  for (let i = entries.length - 1; i >= 0; i -= 1) {
    const entry = entries[i]
    if (entry.kind === 'tool' && entry.group_key === groupKey) {
      return i
    }
    // Stop searching past a turn boundary — tool groups from earlier turns
    // with the same key (e.g. step-1) must not be reused.
    if (entry.kind === 'user' || entry.kind === 'reply' || entry.kind === 'error' || entry.kind === 'approval' || entry.kind === 'question') {
      break
    }
  }
  return -1
}

function findReusableLiveToolGroupKey(entries: TranscriptEntry[]) {
  for (let index = entries.length - 1; index >= 0; index -= 1) {
    const entry = entries[index]
    if (entry.kind === 'tool' && entry.group_key) {
      if (entry.status === 'running') {
        return entry.group_key
      }
      return ''
    }
    if (entry.kind === 'reasoning' || entry.kind === 'memory') {
      continue
    }
    if (entry.kind === 'user' || entry.kind === 'reply' || entry.kind === 'error' || entry.kind === 'approval' || entry.kind === 'question') {
      break
    }
  }

  return ''
}

function makeLiveToolGroupKey(entries: TranscriptEntry[]) {
  const liveGroupCount = entries.filter(
    (entry) => entry.kind === 'tool' && typeof entry.group_key === 'string' && entry.group_key.startsWith('step-live-'),
  ).length
  return `step-live-${liveGroupCount + 1}`
}

function resolveStreamGroupKey(entries: TranscriptEntry[], payload: Record<string, unknown>) {
  const step = payload.Step ?? payload.step ?? payload.step_index ?? payload.StepIndex
  if (typeof step === 'string' || typeof step === 'number') {
    return `step-${String(step)}`
  }

  return findReusableLiveToolGroupKey(entries) || makeLiveToolGroupKey(entries)
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
    // Skip using_skills — they are rendered as dedicated skill entries, not as
    // generic tool calls in the tool group.
    if (toolCall.name === 'using_skills') {
      continue
    }
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
  let next = [...entries]
  for (let i = next.length - 1; i >= 0; i--) {
    const entry = next[i]
    if (entry.kind !== 'tool') {
      continue
    }
    const details = [...(entry.details ?? [])]
    const detailIndex = findToolDetailIndex(details, toolCallId, fallbackName)
    if (detailIndex >= 0) {
      const existing = details[detailIndex]
      const toolName = existing.label || fallbackName
      details[detailIndex] = makeToolDetail({
        toolCallId: toolCallId || existing.key,
        name: toolName,
        argumentsText: existing.blocks?.find((block) => block.label === 'Params')?.value,
        resultText: content,
        loading: false,
      })
      next[i] = makeToolGroupEntry(entry.group_key || entry.id, details)
      return attachImageToolResultToReply(next, toolName, content)
    }
  }

  next = upsertToolInGroup(next, {
    groupKey: `persisted-${toolCallId || next.length}`,
    toolCallId,
    name: fallbackName,
    resultText: content,
    loading: false,
  })
  return attachImageToolResultToReply(next, fallbackName, content)
}

function attachImageToolResultToReply(entries: TranscriptEntry[], toolName: string, content: string) {
  const imageResult = parseImageToolResult(content, toolName)
  return imageResult ? upsertImageAttachmentsReply(entries, imageResult.attachments) : entries
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
    next.push({
      id: createEntryId('user'),
      kind: 'user',
      title: '',
      content: message.content,
      ...(message.attachments && message.attachments.length > 0 ? { attachments: message.attachments } : {}),
    })
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

  if (message.content.trim() || (message.attachments?.length ?? 0) > 0) {
    next = appendReply(next, message.content, message.attachments ?? [])
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

  if (!content && !(message.attachments && message.attachments.length > 0)) {
    return next
  }

  if (hasMatchingLatestReply) {
    next = attachReplyMetaAtIndex(next, findLatestReplyIndex(next), {
      provider_id: message.provider_id,
      model_id: message.model_id,
      token_usage: message.usage,
    })
    return attachReplyAttachmentsToLatestReply(next, message.attachments ?? [])
  }

  next = appendReply(next, message.content, message.attachments ?? [])
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

function findApprovalEntryIndex(entries: TranscriptEntry[], approvalId: string) {
  return entries.findIndex((entry) => entry.kind === 'approval' && entry.approval?.id === approvalId)
}

function isResolvedApprovalStatus(status: string | undefined) {
  return status === 'approved' || status === 'rejected' || status === 'expired' || status === 'cancelled'
}

function upsertApprovalEntry(entries: TranscriptEntry[], approval: ToolApproval) {
  const next = [...entries]
  const approvalIndex = findApprovalEntryIndex(next, approval.id)
  const current = approvalIndex >= 0 ? next[approvalIndex]?.approval : undefined
  const mergedStatus =
    isResolvedApprovalStatus(current?.status) && !isResolvedApprovalStatus(approval.status)
      ? current?.status || ''
      : approval.status || current?.status || ''
  const mergedApproval: ToolApproval = {
    id: approval.id || current?.id || '',
    task_id: approval.task_id || current?.task_id || '',
    conversation_id: approval.conversation_id || current?.conversation_id || '',
    step_index: approval.step_index ?? current?.step_index,
    tool_call_id: approval.tool_call_id || current?.tool_call_id || '',
    tool_name: approval.tool_name || current?.tool_name || '',
    arguments_summary: approval.arguments_summary || current?.arguments_summary || '',
    risk_level: approval.risk_level || current?.risk_level || '',
    reason: approval.reason ?? current?.reason,
    status: mergedStatus,
    decision: approval.decision ?? current?.decision,
    decision_by: approval.decision_by ?? current?.decision_by,
    decision_reason: approval.decision_reason ?? current?.decision_reason,
    decision_at: approval.decision_at ?? current?.decision_at,
    created_at: approval.created_at ?? current?.created_at,
    updated_at: approval.updated_at ?? current?.updated_at,
  }

  if (approvalIndex >= 0) {
    next[approvalIndex] = {
      ...next[approvalIndex],
      title: mergedApproval.status === 'pending' ? '等待审批' : '审批已处理',
      approval: mergedApproval,
      status: mergedApproval.status === 'pending' ? 'running' : mergedApproval.status === 'approved' ? 'done' : 'error',
    }
    return next
  }

  next.push(makeApprovalEntry(mergedApproval))
  return next
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

  // Pre-scan: collect ask_user tool call arguments so we can synthesise
  // question entries instead of plain tool entries for history.
  const askUserArgsByCallId = new Map<string, Record<string, unknown>>()

  // Pre-scan: collect using_skills tool call IDs and their arguments so we can
  // render skill entries during replay instead of raw tool entries.
  const usingSkillsCallIds = new Set<string>()
  const usingSkillsArgsByCallId = new Map<string, string>()

  for (const message of messages) {
    if (message.role === 'assistant') {
      for (const tc of message.tool_calls ?? []) {
        if (tc.name === 'ask_user') {
          let args: Record<string, unknown> = {}
          try {
            args = JSON.parse(tc.arguments ?? '{}') as Record<string, unknown>
          } catch {
            // ignore malformed json
          }
          askUserArgsByCallId.set(tc.id, args)
        }
        if (tc.name === 'using_skills') {
          usingSkillsCallIds.add(tc.id)
          usingSkillsArgsByCallId.set(tc.id, tc.arguments ?? '')
        }
      }
    }
  }

  for (const message of messages) {
    if (message.role === 'assistant') {
      const nonSpecialToolCalls = (message.tool_calls ?? []).filter(
        (tc) => !askUserArgsByCallId.has(tc.id) && !usingSkillsCallIds.has(tc.id),
      )
      if (nonSpecialToolCalls.length > 0) {
        assistantToolGroupIndex += 1
      }

      // Create skill entries for using_skills tool calls in this assistant message
      for (const tc of message.tool_calls ?? []) {
        if (usingSkillsCallIds.has(tc.id)) {
          const argsText = usingSkillsArgsByCallId.get(tc.id) ?? ''
          entries = upsertSkillEntry(entries, {
            toolCallId: tc.id,
            skillName: extractSkillName(argsText),
            loading: false,
          })
        }
      }
    }

    // For tool result messages that correspond to ask_user calls, synthesise a
    // responded question entry and skip the normal tool-result attachment.
    if (message.role === 'tool' && message.tool_call_id && askUserArgsByCallId.has(message.tool_call_id)) {
      const callId = message.tool_call_id
      const args = askUserArgsByCallId.get(callId) ?? {}
      const responseText = message.content ?? ''
      let responseJson: Record<string, unknown> = { custom_text: responseText }
      try {
        const parsed: unknown = JSON.parse(responseText)
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          responseJson = parsed as Record<string, unknown>
        }
      } catch {
        // not valid JSON – keep the plain text as custom_text
      }
      const interaction: InteractionRecord = {
        id: `history-question-${callId}`,
        task_id: '',
        conversation_id: '',
        tool_call_id: callId,
        kind: 'question',
        status: 'responded',
        request_json: args,
        response_json: responseJson,
      }
      entries = upsertQuestionInteractionEntry(entries, interaction)
      continue
    }

    // Skip tool result messages for using_skills calls — the result is ephemeral
    // and never persisted, so there is nothing meaningful to replay.
    if (message.role === 'tool' && message.tool_call_id && usingSkillsCallIds.has(message.tool_call_id)) {
      continue
    }

    // Strip ask_user and using_skills tool calls from assistant messages so they
    // don't produce tool entries in the replay transcript.
    const shouldBeStripped =
      (message.tool_calls ?? []).some((tc) => askUserArgsByCallId.has(tc.id)) ||
      (message.tool_calls ?? []).some((tc) => usingSkillsCallIds.has(tc.id))
    const effectiveMessage: ConversationMessage =
      message.role === 'assistant' && shouldBeStripped
        ? {
            ...message,
            tool_calls: (message.tool_calls ?? []).filter(
              (tc) => !askUserArgsByCallId.has(tc.id) && !usingSkillsCallIds.has(tc.id),
            ),
          }
        : message

    const hasToolCalls = (effectiveMessage.tool_calls ?? []).length > 0
    entries = applyConversationMessage(entries, effectiveMessage, {
      groupKey: effectiveMessage.role === 'assistant' && hasToolCalls ? `persisted-step-${assistantToolGroupIndex}` : undefined,
      toolNames,
    })
  }

  return entries
}

function attachReplyAttachmentsToLatestReply(entries: TranscriptEntry[], attachments: ConversationMessage['attachments']) {
  const next = [...entries]
  for (let index = next.length - 1; index >= 0; index -= 1) {
    const entry = next[index]
    if (entry.kind !== 'reply' || entry.image_preview || isStandaloneAttachmentReply(entry)) {
      continue
    }
    next[index] = {
      ...entry,
      attachments: attachments && attachments.length > 0 ? attachments : undefined,
    }
    return next
  }
  return next
}

export function updateTranscriptFromStreamEvent(entries: TranscriptEntry[], event: Partial<TaskStreamEvent>): TranscriptEntry[] {
  const payload = event.payload ?? {}
  const groupKey = resolveStreamGroupKey(entries, payload)

  if (event.type === 'log.message') {
    const kind = typeof payload.Kind === 'string' ? payload.Kind : typeof payload.kind === 'string' ? payload.kind : ''
    if (kind === 'reasoning_delta') {
      return upsertReasoning(entries, String(payload.Reasoning ?? ''))
    }

    if (kind === 'image_partial') {
      const imagePreview = makeImagePartialPreview(payload)
      return imagePreview ? upsertImagePartialPreview(entries, imagePreview) : entries
    }

    if (kind === 'stream_recovery') {
      const upperMetadata = isRecord(payload.Metadata) ? payload.Metadata : {}
      const lowerMetadata = isRecord(payload.metadata) ? payload.metadata : {}
      const attempt = Number(upperMetadata.attempt ?? lowerMetadata.attempt ?? 1)
      const maxAttempts = Number(upperMetadata.max_attempts ?? lowerMetadata.max_attempts ?? 3)
      const errorMessage = String(payload.Err ?? payload.err ?? 'transient error')
      // Merge consecutive recovery entries into a single entry that updates in place
      const existingIndex = findLastTranscriptEntryIndex(entries, (entry) => entry.kind === 'recovery')
      if (existingIndex >= 0 && existingIndex === entries.length - 1) {
        const next = [...entries]
        next[existingIndex] = {
          ...next[existingIndex],
          title: '自动重试',
          content: `${errorMessage} (${attempt}/${maxAttempts})`,
        }
        return next
      }
      return [
        ...entries,
        {
          id: createEntryId('recovery'),
          kind: 'recovery' as const,
          title: '自动重试',
          content: `${errorMessage} (${attempt}/${maxAttempts})`,
        },
      ]
    }

    if (kind === 'text_delta') {
      const text = String(payload.Text ?? '')
      const next = completeLatestReasoning(entries)
      const last = next[next.length - 1]
      if (isReusableTextReply(last)) {
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
      const toolName = String(toolCall.name ?? toolCall.Name ?? 'Tool')
      const argsText =
        typeof toolCall.arguments === 'string'
          ? toolCall.arguments
          : typeof toolCall.Arguments === 'string'
            ? toolCall.Arguments
            : safeStringify(toolCall.arguments ?? toolCall.Arguments ?? '')
      if (toolName === 'using_skills') {
        return upsertSkillEntry(entries, {
          toolCallId: String(toolCall.id ?? toolCall.ID ?? ''),
          skillName: extractSkillName(argsText),
          loading: true,
        })
      }
      return upsertToolInGroup(entries, {
        groupKey,
        toolCallId: String(toolCall.id ?? toolCall.ID ?? ''),
        name: toolName,
        argumentsText: argsText,
        resultText: 'Running...',
        loading: true,
      })
    }

    if (kind === 'completed' && payload.Message && typeof payload.Message === 'object') {
      const message = normalizeConversationMessage(payload.Message as Partial<ConversationMessage> & Record<string, unknown>)
      if (message.role === 'assistant') {
        return applyCompletedAssistantMessage(entries, message, { groupKey })
      }
      return applyConversationMessage(entries, message, { groupKey })
    }
  }

  if (event.type === 'tool.started') {
    const toolName = String(payload.tool_name ?? payload.toolName ?? payload.ToolName ?? 'Tool')
    const toolCallId = String(payload.tool_call_id ?? payload.toolCallId ?? payload.ToolCallId ?? payload.ToolCallID ?? '')
    if (toolName === 'using_skills') {
      const argsText = typeof payload.Arguments === 'string' ? payload.Arguments : safeStringify(payload.Arguments ?? '')
      return upsertSkillEntry(entries, {
        toolCallId,
        skillName: extractSkillName(argsText),
        loading: true,
      })
    }
    return upsertToolInGroup(entries, {
      groupKey,
      toolCallId,
      name: toolName,
      argumentsText: typeof payload.Arguments === 'string' ? payload.Arguments : safeStringify(payload.Arguments ?? ''),
      resultText: 'Running...',
      loading: true,
    })
  }

  if (event.type === 'tool.finished') {
    const err = payload.Err ? safeStringify(payload.Err) : ''
    const toolName = String(payload.tool_name ?? payload.toolName ?? payload.ToolName ?? 'Tool')
    const toolCallId = String(payload.tool_call_id ?? payload.toolCallId ?? payload.ToolCallId ?? payload.ToolCallID ?? '')
    if (toolName === 'using_skills') {
      const argsText = typeof payload.Arguments === 'string' ? String(payload.Arguments) : ''
      return upsertSkillEntry(entries, {
        toolCallId,
        skillName: extractSkillName(argsText),
        loading: false,
        error: Boolean(err),
      })
    }
    const output = err || safeStringify(payload.Output ?? '')
    const next = upsertToolInGroup(entries, {
      groupKey,
      toolCallId,
      name: toolName,
      argumentsText: typeof payload.Arguments === 'string' ? String(payload.Arguments) : '',
      resultText: output,
      loading: false,
      error: Boolean(err),
    })
    return err ? next : attachImageToolResultToReply(next, toolName, output)
  }

  if (event.type === 'approval.requested') {
    return upsertApprovalEntry(entries, normalizeToolApproval(payload))
  }

  if (event.type === 'approval.resolved') {
    return upsertApprovalEntry(entries, normalizeToolApproval(payload))
  }

  if (event.type === 'interaction.requested' || event.type === 'interaction.responded') {
    const interaction = normalizeInteractionRecord(payload as Record<string, unknown>)
    if (interaction.kind === 'approval') {
      return upsertApprovalEntry(entries, normalizeToolApproval({
        id: interaction.id,
        task_id: interaction.task_id,
        conversation_id: interaction.conversation_id,
        step_index: interaction.step_index,
        tool_call_id: interaction.tool_call_id,
        tool_name: String(interaction.request_json?.tool_name ?? ''),
        arguments_summary: String(interaction.request_json?.arguments_summary ?? ''),
        risk_level: String(interaction.request_json?.risk_level ?? ''),
        reason: typeof interaction.request_json?.reason === 'string' ? interaction.request_json.reason : undefined,
        status: interaction.status,
        decision_reason: typeof interaction.response_json?.reason === 'string' ? interaction.response_json.reason : undefined,
        decision_by: interaction.responded_by,
        decision_at: interaction.responded_at,
        created_at: interaction.created_at,
        updated_at: interaction.updated_at,
      }))
    }
    if (interaction.kind === 'question') {
      return upsertQuestionInteractionEntry(entries, interaction)
    }
  }

  if (event.type === 'task.failed') {
    const settledEntries = stopAllLoading(entries, 'error')
    const message = String(payload.error ?? 'Unknown error')
    if (compactWhitespace(message) && compactWhitespace(message) === latestToolFailureMessage(settledEntries)) {
      return settledEntries
    }
    // Deduplicate: if the last error entry already shows the same message, don't append again
    const lastError = findLastTranscriptEntry(settledEntries, (entry) => entry.kind === 'error')
    if (lastError && compactWhitespace(lastError.content ?? '') === compactWhitespace(message)) {
      return settledEntries
    }
    return [
      ...settledEntries,
      { id: createEntryId('error'), kind: 'error', title: '运行失败', content: message },
    ]
  }

  if (event.type === 'task.finished') {
    const status = String(payload.status ?? '')
    const terminalToolStatus = status === 'failed' ? 'error' : 'done'
    const settledEntries = attachTokenUsageToLatestReply(
      stopAllLoading(entries, terminalToolStatus),
      normalizeTranscriptTokenUsage(payload.usage ?? payload.token_usage ?? payload.Usage ?? payload.TokenUsage),
    )
    if (status === 'failed') {
      const nestedError = payload.error && typeof payload.error === 'object' ? String((payload.error as Record<string, unknown>).message ?? '') : ''
      return [
        ...settledEntries,
        { id: createEntryId('error'), kind: 'error', title: '运行失败', content: nestedError || String(payload.error ?? `Task ${status}`) },
      ]
    }
    return settledEntries
  }

  if (event.type === 'memory.context_state') {
    const state = normalizeMemoryContextSnapshot(payload)
    if (!state) {
      return entries
    }
    const next = [...entries]
    const existingIndex = next.findIndex((e) => e.kind === 'memory' && e.memory_context_state != null)
    const entry: TranscriptEntry = {
      id: existingIndex >= 0 ? next[existingIndex].id : createEntryId('memory-ctx'),
      kind: 'memory' as const,
      title: '',
      memory_context_state: state,
    }
    if (existingIndex >= 0) {
      next[existingIndex] = entry
    } else {
      next.push(entry)
    }
    return next
  }

  if (event.type === 'memory.compressed') {
    const compression = normalizeMemoryCompressionSnapshot(payload)
    if (!compression) {
      return entries
    }
    const tokensBefore = compression.tokens_before
    const tokensAfter = compression.tokens_after
    const detail = tokensBefore > 0
      ? `${tokensBefore.toLocaleString()} → ${tokensAfter.toLocaleString()} tokens`
      : ''
    return [
      ...entries,
      {
        id: createEntryId('memory'),
        kind: 'memory' as const,
        title: '记忆压缩',
        memory_compression: compression,
        content: detail || undefined,
      },
    ]
  }

  return entries
}
