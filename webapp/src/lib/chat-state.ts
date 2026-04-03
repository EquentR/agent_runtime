import type { TranscriptEntry } from '../types/api'

const CHAT_STATE_KEY = 'agent-runtime.chat-state'

interface ChatState {
  activeConversationId: string
  activeTaskId: string
  activeTaskEventSeq: number
  entries: TranscriptEntry[]
  draftEntriesByConversation: Record<string, TranscriptEntry[]>
  selectedSkillsByConversation: Record<string, string[]>
}

const EMPTY_STATE: ChatState = {
  activeConversationId: '',
  activeTaskId: '',
  activeTaskEventSeq: 0,
  entries: [],
  draftEntriesByConversation: {},
  selectedSkillsByConversation: {},
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
      activeTaskEventSeq: typeof parsed.activeTaskEventSeq === 'number' && Number.isFinite(parsed.activeTaskEventSeq) ? parsed.activeTaskEventSeq : 0,
      entries: Array.isArray(parsed.entries) ? parsed.entries : [],
      draftEntriesByConversation:
        parsed.draftEntriesByConversation && typeof parsed.draftEntriesByConversation === 'object'
          ? Object.fromEntries(
              Object.entries(parsed.draftEntriesByConversation).filter(
                ([conversationId, entries]) => typeof conversationId === 'string' && Array.isArray(entries),
              ),
            )
          : {},
      selectedSkillsByConversation:
        parsed.selectedSkillsByConversation && typeof parsed.selectedSkillsByConversation === 'object'
          ? Object.fromEntries(
              Object.entries(parsed.selectedSkillsByConversation).filter(
                ([conversationId, skills]) =>
                  typeof conversationId === 'string' && Array.isArray(skills) && skills.every((skill) => typeof skill === 'string'),
              ),
            )
          : {},
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
