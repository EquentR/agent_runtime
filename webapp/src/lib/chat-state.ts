import type { WorkspaceMode } from '../types/api'

const CHAT_STATE_KEY = 'agent-runtime.chat-state'
const CHAT_STATE_SAVE_DELAY_MS = 75

interface ChatState {
  activeConversationId: string
  activeTaskId: string
  activeTaskEventSeq: number
  activeTaskIdByConversation: Record<string, string>
  activeTaskEventSeqByConversation: Record<string, number>
  selectedSkillsByConversation: Record<string, string[]>
  selectedWorkspaceModeByConversation: Record<string, WorkspaceMode>
  pendingWorkspaceMergeTaskIdByConversation: Record<string, string>
}

const EMPTY_STATE: ChatState = {
  activeConversationId: '',
  activeTaskId: '',
  activeTaskEventSeq: 0,
  activeTaskIdByConversation: {},
  activeTaskEventSeqByConversation: {},
  selectedSkillsByConversation: {},
  selectedWorkspaceModeByConversation: {},
  pendingWorkspaceMergeTaskIdByConversation: {},
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
      activeTaskIdByConversation:
        parsed.activeTaskIdByConversation && typeof parsed.activeTaskIdByConversation === 'object'
          ? Object.fromEntries(
              Object.entries(parsed.activeTaskIdByConversation).filter(
                ([conversationId, taskId]) => typeof conversationId === 'string' && typeof taskId === 'string',
              ),
            )
          : {},
      activeTaskEventSeqByConversation:
        parsed.activeTaskEventSeqByConversation && typeof parsed.activeTaskEventSeqByConversation === 'object'
          ? Object.fromEntries(
              Object.entries(parsed.activeTaskEventSeqByConversation).filter(
                ([conversationId, seq]) =>
                  typeof conversationId === 'string' && typeof seq === 'number' && Number.isFinite(seq),
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
      selectedWorkspaceModeByConversation:
        parsed.selectedWorkspaceModeByConversation && typeof parsed.selectedWorkspaceModeByConversation === 'object'
          ? Object.fromEntries(
              Object.entries(parsed.selectedWorkspaceModeByConversation).filter(
                ([conversationId, mode]) =>
                  typeof conversationId === 'string' && (mode === 'mutable' || mode === 'readonly'),
              ),
            )
          : {},
      pendingWorkspaceMergeTaskIdByConversation:
        parsed.pendingWorkspaceMergeTaskIdByConversation && typeof parsed.pendingWorkspaceMergeTaskIdByConversation === 'object'
          ? Object.fromEntries(
              Object.entries(parsed.pendingWorkspaceMergeTaskIdByConversation).filter(
                ([conversationId, taskId]) => typeof conversationId === 'string' && typeof taskId === 'string',
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
