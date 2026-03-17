export const DEFAULT_PROVIDER_ID = 'openai'
export const DEFAULT_MODEL_ID = 'gpt-5.4'

export function formatConversationTitle(title: string, fallback: string) {
  const trimmed = title.trim()
  return trimmed || fallback
}

export function formatMessageContent(content: string) {
  const trimmed = content.trim()
  return trimmed || '(empty message)'
}
