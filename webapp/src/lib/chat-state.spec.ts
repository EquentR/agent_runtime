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

  it('persists active conversation and metadata (no entries)', () => {
    saveChatState({
      activeConversationId: 'conv_1',
      activeTaskId: 'task_1',
      activeTaskEventSeq: 12,
      activeTaskIdByConversation: {},
      activeTaskEventSeqByConversation: {},
      selectedSkillsByConversation: {
        conv_1: ['debugging'],
      },
      selectedWorkspaceModeByConversation: {
        conv_1: 'readonly',
      },
      pendingWorkspaceMergeTaskIdByConversation: {
        conv_1: 'task_merge',
      },
    })

    expect(loadChatState()).toEqual({
      activeConversationId: 'conv_1',
      activeTaskId: 'task_1',
      activeTaskEventSeq: 12,
      activeTaskIdByConversation: {},
      activeTaskEventSeqByConversation: {},
      selectedSkillsByConversation: {
        conv_1: ['debugging'],
      },
      selectedWorkspaceModeByConversation: {
        conv_1: 'readonly',
      },
      pendingWorkspaceMergeTaskIdByConversation: {
        conv_1: 'task_merge',
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
      activeTaskIdByConversation: {},
      activeTaskEventSeqByConversation: {},
      selectedSkillsByConversation: {},
      selectedWorkspaceModeByConversation: {},
      pendingWorkspaceMergeTaskIdByConversation: {},
    })
    scheduleChatStateSave({
      activeConversationId: 'conv_2',
      activeTaskId: 'task_2',
      activeTaskEventSeq: 2,
      activeTaskIdByConversation: {},
      activeTaskEventSeqByConversation: {},
      selectedSkillsByConversation: {
        conv_2: ['review'],
      },
      selectedWorkspaceModeByConversation: {
        conv_2: 'mutable',
      },
      pendingWorkspaceMergeTaskIdByConversation: {
        conv_2: 'task_merge',
      },
    })

    expect(setItemSpy).not.toHaveBeenCalled()

    vi.runAllTimers()

    expect(setItemSpy).toHaveBeenCalledTimes(1)
    expect(loadChatState()).toEqual({
      activeConversationId: 'conv_2',
      activeTaskId: 'task_2',
      activeTaskEventSeq: 2,
      activeTaskIdByConversation: {},
      activeTaskEventSeqByConversation: {},
      selectedSkillsByConversation: {
        conv_2: ['review'],
      },
      selectedWorkspaceModeByConversation: {
        conv_2: 'mutable',
      },
      pendingWorkspaceMergeTaskIdByConversation: {
        conv_2: 'task_merge',
      },
    })
  })

  it('clears stored chat state', () => {
    saveChatState({
      activeConversationId: '',
      activeTaskId: 'task_1',
      activeTaskEventSeq: 4,
      activeTaskIdByConversation: {},
      activeTaskEventSeqByConversation: {},
      selectedSkillsByConversation: { conv_1: ['review'] },
      selectedWorkspaceModeByConversation: { conv_1: 'readonly' },
      pendingWorkspaceMergeTaskIdByConversation: { conv_1: 'task_merge' },
    })

    clearChatState()

    expect(loadChatState()).toEqual({
      activeConversationId: '',
      activeTaskId: '',
      activeTaskEventSeq: 0,
      activeTaskIdByConversation: {},
      activeTaskEventSeqByConversation: {},
      selectedSkillsByConversation: {},
      selectedWorkspaceModeByConversation: {},
      pendingWorkspaceMergeTaskIdByConversation: {},
    })
  })
})
