import type { TranscriptEntry } from '../types/api'

const defaultQuestionPlaceholder = '补充你的回答'

export interface NormalizedQuestionEntry {
  interactionId: string
  taskId: string
  toolCallId: string
  status: string
  prompt: string
  options: string[]
  placeholder: string
  allowCustom: boolean
  multiple: boolean
  finalAnswer: string
}

export function summarizeQuestionResponse(response: Record<string, unknown> | undefined) {
  if (!response || typeof response !== 'object') {
    return ''
  }

  const parts: string[] = []
  const selectedOptionId = typeof response.selected_option_id === 'string' ? response.selected_option_id.trim() : ''
  const selectedOptionIds = Array.isArray(response.selected_option_ids)
    ? response.selected_option_ids.map((value) => String(value).trim()).filter(Boolean)
    : []
  const customText = typeof response.custom_text === 'string' ? response.custom_text.trim() : ''

  if (selectedOptionId) {
    parts.push(selectedOptionId)
  }
  if (selectedOptionIds.length > 0) {
    parts.push(selectedOptionIds.join('、'))
  }
  if (customText) {
    parts.push(customText)
  }

  return parts.join('\n')
}

export function normalizeQuestionEntry(entry: TranscriptEntry): NormalizedQuestionEntry {
  if (entry.kind !== 'question') {
    return {
      interactionId: '',
      taskId: '',
      toolCallId: '',
      status: '',
      prompt: '',
      options: [],
      placeholder: defaultQuestionPlaceholder,
      allowCustom: false,
      multiple: false,
      finalAnswer: '',
    }
  }

  const interaction = entry.question_interaction
  const request = interaction?.request_json
  const rawOptions = request?.options

  return {
    interactionId: interaction?.id ?? '',
    taskId: interaction?.task_id ?? '',
    toolCallId: interaction?.tool_call_id ?? '',
    status: interaction?.status ?? '',
    prompt: String(request?.question ?? ''),
    options: Array.isArray(rawOptions) ? rawOptions.map((item) => String(item)).filter(Boolean) : [],
    placeholder: String(request?.placeholder ?? defaultQuestionPlaceholder),
    allowCustom: request?.allow_custom === true,
    multiple: request?.multiple === true,
    finalAnswer: summarizeQuestionResponse(interaction?.response_json),
  }
}
