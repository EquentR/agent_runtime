export const APP_TITLE = 'Agent Runtime'

export function formatConversationTitle(title: string, fallback: string) {
  const trimmed = title.trim()
  return trimmed || fallback
}

export function formatDocumentTitle(title?: string) {
  const trimmed = title?.trim() ?? ''
  return trimmed ? `${trimmed} - ${APP_TITLE}` : APP_TITLE
}

export function formatMessageContent(content: string) {
  const trimmed = content.trim()
  return trimmed || '(empty message)'
}
