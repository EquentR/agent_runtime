import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { clearChatState, loadChatState, saveChatState, scheduleChatStateSave } from './chat-state'

describe('chat-state', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
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

  it('batches scheduled saves and persists the latest durable state', () => {
    vi.useFakeTimers()
    const setItemSpy = vi.spyOn(Storage.prototype, 'setItem')

    scheduleChatStateSave({
      activeConversationId: 'conv_1',
      activeTaskId: 'task_1',
      activeTaskEventSeq: 1,
      entries: [{ id: 'a', kind: 'user', title: 'You', content: 'hello' }],
      draftEntriesByConversation: {},
      selectedSkillsByConversation: {},
    })
    scheduleChatStateSave({
      activeConversationId: 'conv_2',
      activeTaskId: 'task_2',
      activeTaskEventSeq: 2,
      entries: [{ id: 'b', kind: 'reply', title: '', content: 'latest' }],
      draftEntriesByConversation: {
        conv_2: [{ id: 'c', kind: 'reply', title: '', content: 'draft' }],
      },
      selectedSkillsByConversation: {
        conv_2: ['review'],
      },
    })

    expect(setItemSpy).not.toHaveBeenCalled()

    vi.runAllTimers()

    expect(setItemSpy).toHaveBeenCalledTimes(1)
    expect(loadChatState()).toEqual({
      activeConversationId: 'conv_2',
      activeTaskId: 'task_2',
      activeTaskEventSeq: 2,
      entries: [{ id: 'b', kind: 'reply', title: '', content: 'latest' }],
      draftEntriesByConversation: {
        conv_2: [{ id: 'c', kind: 'reply', title: '', content: 'draft' }],
      },
      selectedSkillsByConversation: {
        conv_2: ['review'],
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
