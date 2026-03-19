import type { TranscriptEntry } from '../types/api'

const CHAT_STATE_KEY = 'agent-runtime.chat-state'

interface ChatState {
  activeConversationId: string
  activeTaskId: string
  entries: TranscriptEntry[]
}

const EMPTY_STATE: ChatState = {
  activeConversationId: '',
  activeTaskId: '',
  entries: [],
}

export function loadChatState(): ChatState {
  const raw = localStorage.getItem(CHAT_STATE_KEY)
  if (!raw) {
    return EMPTY_STATE
  }

  try {
    const parsed = JSON.parse(raw) as Partial<ChatState>
    return {
      activeConversationId: typeof parsed.activeConversationId === 'string' ? parsed.activeConversationId : '',
      activeTaskId: typeof parsed.activeTaskId === 'string' ? parsed.activeTaskId : '',
      entries: Array.isArray(parsed.entries) ? parsed.entries : [],
    }
  } catch {
    return EMPTY_STATE
  }
}

export function saveChatState(state: ChatState) {
  localStorage.setItem(CHAT_STATE_KEY, JSON.stringify(state))
}

export function clearChatState() {
  localStorage.removeItem(CHAT_STATE_KEY)
}
