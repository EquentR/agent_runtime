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
      activeTaskEventSeq: 12,
      entries: [{ id: 'a', kind: 'error', title: 'Failed', content: 'boom' }],
      draftEntriesByConversation: {
        conv_1: [{ id: 'b', kind: 'reply', title: '', content: 'partial' }],
      },
      selectedSkillsByConversation: {
        conv_1: ['debugging'],
      },
    })

    expect(loadChatState()).toEqual({
      activeConversationId: 'conv_1',
      activeTaskId: 'task_1',
      activeTaskEventSeq: 12,
      entries: [{ id: 'a', kind: 'error', title: 'Failed', content: 'boom' }],
      draftEntriesByConversation: {
        conv_1: [{ id: 'b', kind: 'reply', title: '', content: 'partial' }],
      },
      selectedSkillsByConversation: {
        conv_1: ['debugging'],
      },
    })
  })

  it('clears stored chat state', () => {
    saveChatState({
      activeConversationId: '',
      activeTaskId: 'task_1',
      activeTaskEventSeq: 4,
      entries: [{ id: 'a', kind: 'user', title: 'You', content: 'hi' }],
      draftEntriesByConversation: { conv_1: [{ id: 'b', kind: 'reply', title: '', content: 'partial' }] },
      selectedSkillsByConversation: { conv_1: ['review'] },
    })

    clearChatState()

    expect(loadChatState()).toEqual({
      activeConversationId: '',
      activeTaskId: '',
      activeTaskEventSeq: 0,
      entries: [],
      draftEntriesByConversation: {},
      selectedSkillsByConversation: {},
    })
  })
})
