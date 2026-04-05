import type { TranscriptEntry } from '../types/api'

const CHAT_STATE_KEY = 'agent-runtime.chat-state'
const CHAT_STATE_SAVE_DELAY_MS = 75

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

let pendingSaveTimer: ReturnType<typeof setTimeout> | null = null
let pendingState: ChatState | null = null

function persistChatState(state: ChatState) {
  localStorage.setItem(CHAT_STATE_KEY, JSON.stringify(state))
}

function clearPendingChatStateSave() {
  if (!pendingSaveTimer) {
    return
  }
  clearTimeout(pendingSaveTimer)
  pendingSaveTimer = null
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
  clearPendingChatStateSave()
  pendingState = null
  persistChatState(state)
}

export function scheduleChatStateSave(state: ChatState) {
  pendingState = state
  clearPendingChatStateSave()
  pendingSaveTimer = setTimeout(() => {
    pendingSaveTimer = null
    if (!pendingState) {
      return
    }
    const stateToSave = pendingState
    pendingState = null
    persistChatState(stateToSave)
  }, CHAT_STATE_SAVE_DELAY_MS)
}

export function clearChatState() {
  clearPendingChatStateSave()
  pendingState = null
  localStorage.removeItem(CHAT_STATE_KEY)
}
