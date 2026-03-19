export function formatConversationTitle(title: string, fallback: string) {
  const trimmed = title.trim()
  return trimmed || fallback
}

export function formatMessageContent(content: string) {
  const trimmed = content.trim()
  return trimmed || '(empty message)'
}
