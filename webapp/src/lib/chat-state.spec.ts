import { beforeEach, describe, expect, it } from 'vitest'

import { clearChatState, loadChatState, saveChatState } from './chat-state'

describe('chat-state', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('persists active conversation and transcript entries', () => {
    saveChatState({
      activeConversationId: 'conv_1',
      activeTaskId: 'task_1',
      entries: [{ id: 'a', kind: 'error', title: 'Failed', content: 'boom' }],
    })

    expect(loadChatState()).toEqual({
      activeConversationId: 'conv_1',
      activeTaskId: 'task_1',
      entries: [{ id: 'a', kind: 'error', title: 'Failed', content: 'boom' }],
    })
  })

  it('clears stored chat state', () => {
    saveChatState({ activeConversationId: '', activeTaskId: 'task_1', entries: [{ id: 'a', kind: 'user', title: 'You', content: 'hi' }] })

    clearChatState()

    expect(loadChatState()).toEqual({ activeConversationId: '', activeTaskId: '', entries: [] })
  })
})
