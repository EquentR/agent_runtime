import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'

const chatStyles = readFileSync(resolve(process.cwd(), 'src/style.css'), 'utf8')

const api = vi.hoisted(() => ({
  TASK_STREAM_ABORTED_MESSAGE: 'Task event stream aborted',
  cancelTask: vi.fn(),
  createRunTask: vi.fn(),
  decideTaskApproval: vi.fn(),
  deleteConversation: vi.fn(),
  fetchModelCatalog: vi.fn(),
  fetchConversationMessages: vi.fn(),
  fetchConversations: vi.fn(),
  fetchTaskApprovals: vi.fn(),
  findRunningTaskByConversation: vi.fn(),
  fetchTaskDetails: vi.fn(),
  normalizeToolApproval: vi.fn((approval: any) => ({
    id: approval.id ?? approval.approval_id ?? '',
    task_id: approval.task_id ?? '',
    conversation_id: approval.conversation_id ?? '',
    step_index: approval.step_index ?? approval.step,
    tool_call_id: approval.tool_call_id ?? '',
    tool_name: approval.tool_name ?? '',
    arguments_summary: approval.arguments_summary ?? '',
    risk_level: approval.risk_level ?? '',
    reason: approval.reason,
    status: approval.status ?? '',
    decision: approval.decision,
    decision_by: approval.decision_by,
    decision_reason: approval.decision_reason,
    decision_at: approval.decision_at,
    created_at: approval.created_at,
    updated_at: approval.updated_at,
  })),
  normalizeTranscriptTokenUsage: vi.fn((usage: any) => usage),
  streamRunTask: vi.fn(),
}))

vi.mock('../lib/api', () => api)

import ChatView from './ChatView.vue'
import MessageList from '../components/MessageList.vue'

function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    writable: true,
    value: width,
  })
  window.dispatchEvent(new Event('resize'))
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/chat', component: ChatView }],
  })
}

function createDeferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('ChatView', () => {
  beforeEach(() => {
    setViewportWidth(1280)
    localStorage.setItem('agent-runtime.user', 'demo-user')
    localStorage.removeItem('agent-runtime.chat-state')
    api.fetchModelCatalog.mockReset()
    api.fetchConversations.mockReset()
    api.fetchConversationMessages.mockReset()
    api.cancelTask.mockReset()
    api.createRunTask.mockReset()
    api.decideTaskApproval.mockReset()
    api.deleteConversation.mockReset()
    api.findRunningTaskByConversation.mockReset()
    api.fetchTaskDetails.mockReset()
    api.fetchTaskApprovals.mockReset()
    api.streamRunTask.mockReset()
    api.fetchModelCatalog.mockResolvedValue({
      default_provider_id: 'openai',
      default_model_id: 'gpt-5.4',
      providers: [
        {
          id: 'openai',
          name: 'openai',
          models: [
            { id: 'gpt-5.4', name: 'GPT 5.4', type: 'openai_responses' },
            { id: 'gpt-4.1-mini', name: 'GPT 4.1 Mini', type: 'openai_responses' },
          ],
        },
        {
          id: 'google',
          name: 'google',
          models: [{ id: 'gemini-2.5-flash', name: 'Gemini 2.5 Flash', type: 'google' }],
        },
      ],
    })
  })

  afterEach(() => {
    document.body.querySelectorAll('.sidebar-user-menu-panel, .sidebar-confirm-overlay').forEach((node) => {
      node.parentNode?.removeChild(node)
    })
  })

  it('resumes SSE for a running task after reopening the chat view', async () => {
    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: 'conv_1',
        activeTaskId: 'task_1',
        entries: [{ id: 'reply-1', kind: 'reply', title: '', content: 'partial answer' }],
      }),
    )
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: 'First chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_1',
      status: 'running',
      input: { conversation_id: 'conv_1' },
    })
    api.streamRunTask.mockResolvedValue({ conversation_id: 'conv_1' })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(api.fetchTaskDetails).toHaveBeenCalledWith('task_1')
    expect(api.streamRunTask).toHaveBeenCalledWith(
      'task_1',
      expect.any(Function),
      expect.any(Function),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
  })

  it('hydrates pending approvals when reopening a waiting task even if SSE does not replay approval.requested', async () => {
    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: 'conv_waiting',
        activeTaskId: 'task_waiting',
        activeTaskEventSeq: 0,
        entries: [],
        draftEntriesByConversation: {},
      }),
    )
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_waiting',
        title: 'Waiting chat',
        last_message: 'hello',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'user', content: 'hello' }])
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_waiting',
      status: 'waiting',
      input: { conversation_id: 'conv_waiting' },
      suspend_reason: 'waiting_for_tool_approval',
    })
    api.fetchTaskApprovals.mockResolvedValue([
      {
        id: 'approval_waiting',
        task_id: 'task_waiting',
        conversation_id: 'conv_waiting',
        step_index: 4,
        tool_call_id: 'call_waiting',
        tool_name: 'bash',
        arguments_summary: 'rm -rf /tmp/demo',
        risk_level: 'high',
        reason: 'dangerous filesystem mutation',
        status: 'pending',
      },
    ])
    api.streamRunTask.mockRejectedValue(new Error('Task event stream disconnected'))

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(api.fetchTaskDetails).toHaveBeenCalledWith('task_waiting')
    expect(api.fetchTaskApprovals).toHaveBeenCalledWith('task_waiting')
    expect(wrapper.find('.approval-card').exists()).toBe(true)
    expect(wrapper.text()).toContain('bash')
    expect(wrapper.text()).not.toContain('运行失败')
  })

  it('still reattaches SSE when waiting-task approval hydration fails', async () => {
    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: 'conv_waiting',
        activeTaskId: 'task_waiting',
        activeTaskEventSeq: 0,
        entries: [],
        draftEntriesByConversation: {},
      }),
    )
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_waiting',
        title: 'Waiting chat',
        last_message: 'hello',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'user', content: 'hello' }])
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_waiting',
      status: 'waiting',
      input: { conversation_id: 'conv_waiting' },
      suspend_reason: 'waiting_for_tool_approval',
    })
    api.fetchTaskApprovals.mockRejectedValue(new Error('approval list unavailable'))
    api.streamRunTask.mockResolvedValue({ conversation_id: 'conv_waiting' })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(api.fetchTaskApprovals).toHaveBeenCalledWith('task_waiting')
    expect(api.streamRunTask).toHaveBeenCalledWith(
      'task_waiting',
      expect.any(Function),
      expect.any(Function),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
  })

  it('keeps composer enabled after loading an existing conversation', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: 'First chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(wrapper.find('.composer-submit').exists()).toBe(true)
    expect(wrapper.find('.composer-submit').attributes('aria-label')).toBe('发送')
    expect(wrapper.find('.composer-submit svg').exists()).toBe(true)
    expect(wrapper.text()).toContain('First chat')
    expect(wrapper.text()).toContain('当前账号')
    expect(wrapper.find('.topbar .status-pill').text()).toContain('就绪')
  })

  it('shows admin links only inside the user menu', async () => {
    localStorage.setItem('agent-runtime.user', JSON.stringify({ username: 'demo-user', role: 'admin' }))
    api.fetchConversations.mockResolvedValue([])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    const trigger = wrapper.find('.sidebar-user-menu-trigger')
    expect(trigger.exists()).toBe(true)
    await trigger.trigger('click')
    await flushPromises()

    const link = document.body.querySelector('.sidebar-admin-link') as HTMLAnchorElement | null
    expect(link).not.toBeNull()
    expect(Array.from(document.body.querySelectorAll('.sidebar-admin-link')).map((node) => node.textContent ?? '')).toEqual(
      expect.arrayContaining(['审计', '提示词管理']),
    )
    expect(wrapper.find('.topbar-audit-link').exists()).toBe(false)
  })

  it('does not show a separate approval entry beside the composer even when waiting for approval', async () => {
    localStorage.setItem(
      'agent-runtime.user',
      JSON.stringify({ username: 'demo-user', role: 'admin' }),
    )
    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: 'conv_waiting',
        activeTaskId: 'task_waiting',
        activeTaskEventSeq: 0,
        entries: [],
        draftEntriesByConversation: {},
      }),
    )
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_waiting',
        title: 'Waiting chat',
        last_message: 'hello',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'user', content: 'hello' }])
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_waiting',
      status: 'waiting',
      input: { conversation_id: 'conv_waiting' },
      suspend_reason: 'waiting_for_tool_approval',
    })
    api.fetchTaskApprovals.mockResolvedValue([])
    api.streamRunTask.mockRejectedValue(new Error('Task event stream disconnected'))

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(wrapper.find('.composer-approval-entry').exists()).toBe(false)
  })

  it('does not show a separate approval entry beside the composer without an active approval task', async () => {
    api.fetchConversations.mockResolvedValue([])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(wrapper.find('.composer-approval-entry').exists()).toBe(false)
  })

  it('opens on a new conversation instead of auto-selecting an existing one after refresh', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: 'First chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(api.fetchConversationMessages).not.toHaveBeenCalled()
    expect(wrapper.find('.conversation-card.active').exists()).toBe(false)
    expect(wrapper.find('.topbar-conversation-title').text()).toBe('新对话')
  })

  it('shows the Chinese default title for a new conversation', async () => {
    api.fetchConversations.mockResolvedValue([])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(wrapper.find('.topbar-conversation-title').text()).toBe('新对话')
  })

  it('updates the browser title with the active conversation name', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: '项目周报',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('.conversation-card').trigger('click')
    await flushPromises()

    expect(document.title).toBe('项目周报 - Agent Runtime')
  })

  it('renders conversation history after selecting a sidebar conversation', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: 'First chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
      {
        id: 'conv_2',
        title: 'Second chat',
        last_message: 'world',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockImplementation(async (conversationId: string) => {
      if (conversationId === 'conv_2') {
        return [{ role: 'assistant', content: 'second' }]
      }
      return [{ role: 'assistant', content: 'first' }]
    })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    const buttons = wrapper.findAll('.conversation-card')
    await buttons[1].trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('second')
  })

  it('reconnects SSE when selecting a conversation with a running task', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: 'First chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])
    api.findRunningTaskByConversation.mockResolvedValue({
      id: 'task_1',
      status: 'running',
      input: { conversation_id: 'conv_1' },
    })
    api.streamRunTask.mockResolvedValue({ conversation_id: 'conv_1' })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('.conversation-card').trigger('click')
    await flushPromises()

    expect(api.findRunningTaskByConversation).toHaveBeenCalledWith('conv_1')
    expect(api.streamRunTask).toHaveBeenCalledWith(
      'task_1',
      expect.any(Function),
      expect.any(Function),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
  })

  it('restores saved error trace when reopening without a persisted conversation', async () => {
    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: '',
        entries: [{ id: 'err-1', kind: 'error', title: '运行失败', content: 'network lost' }],
      }),
    )
    api.fetchConversations.mockResolvedValue([])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('network lost')
  })

  it('refreshes conversations after send and keeps the returned conversation selected', async () => {
    api.fetchConversations
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        {
          id: 'conv_new',
          title: 'Newest chat',
          last_message: 'assistant answer',
          message_count: 2,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 'conv_new',
          title: 'Newest chat',
          last_message: 'assistant answer',
          message_count: 2,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
      ])
    api.createRunTask.mockResolvedValue({ id: 'task_1', input: { conversation_id: 'conv_new' } })
    api.streamRunTask.mockResolvedValue({ conversation_id: 'conv_new' })
    api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'assistant answer' }])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(api.fetchConversations).toHaveBeenCalledTimes(3)
    expect(api.fetchConversationMessages).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Newest chat')
    expect(wrapper.find('.conversation-card.active').text()).toContain('Newest chat')
  })

  it('shows and selects the new conversation immediately after send before the task finishes', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()

    api.fetchConversations
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        {
          id: 'conv_new',
          title: 'Pending new chat',
          last_message: 'hello',
          message_count: 1,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 'conv_new',
          title: 'Pending new chat',
          last_message: 'hello',
          message_count: 1,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
      ])
    api.createRunTask.mockResolvedValue({ id: 'task_1', input: { conversation_id: 'conv_new' } })
    api.streamRunTask.mockImplementation(() => runningStream.promise)
    api.fetchConversationMessages.mockResolvedValue([])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(api.fetchConversations).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.conversation-card.active').text()).toContain('Pending new chat')
    expect(wrapper.find('.topbar-conversation-title').text()).toBe('Pending new chat')

    runningStream.resolve({ conversation_id: 'conv_new' })
    await flushPromises()
  })

  it('keeps a pending conversation visible in the sidebar even if the refreshed list has not included it yet', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()

    api.fetchConversations
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([])
    api.createRunTask.mockResolvedValue({ id: 'task_1', input: { conversation_id: 'conv_new' } })
    api.streamRunTask.mockImplementation(() => runningStream.promise)

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    const cards = wrapper.findAll('.conversation-card')
    expect(cards).toHaveLength(1)
    expect(cards[0].text()).toContain('未命名对话')
    expect(cards[0].classes()).toContain('active')

    runningStream.resolve({ conversation_id: 'conv_new' })
    await flushPromises()
  })

  it('refreshes the conversation list again on the first streamed text chunk for a new conversation', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()
    let emitStreamEvent: ((event: any) => void) | null = null

    api.fetchConversations
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        {
          id: 'conv_new',
          title: 'Pending new chat',
          last_message: 'hello',
          message_count: 1,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
      ])
    api.createRunTask.mockResolvedValue({ id: 'task_1', input: { conversation_id: 'conv_new' } })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onOpen: () => void, onEvent: (event: any) => void) => {
      emitStreamEvent = onEvent
      return runningStream.promise
    })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.findAll('.conversation-card')).toHaveLength(1)
    expect(wrapper.find('.conversation-card').text()).toContain('未命名对话')

    emitStreamEvent?.({ type: 'log.message', seq: 1, payload: { Kind: 'text_delta', Text: 'assistant' } })
    await flushPromises()

    expect(api.fetchConversations).toHaveBeenCalledTimes(3)
    expect(wrapper.find('.conversation-card').text()).toContain('Pending new chat')

    runningStream.resolve({ conversation_id: 'conv_new' })
    await flushPromises()
  })

  it('does not show a user-facing error when the task stream disconnects while the task is still running', async () => {
    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_1', input: { conversation_id: 'conv_new' } })
    api.streamRunTask.mockRejectedValue(new Error('Task event stream disconnected'))
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_1',
      status: 'running',
      input: { conversation_id: 'conv_new' },
    })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(api.fetchTaskDetails).toHaveBeenCalledWith('task_1')
    expect(wrapper.find('.error-banner').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Task event stream disconnected')
  })

  it('turns the send button into a stop button while syncing and cancels the active task', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()

    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_1', input: { conversation_id: 'conv_new' } })
    api.streamRunTask.mockImplementation(() => runningStream.promise)
    api.cancelTask
      .mockResolvedValueOnce({
        id: 'task_1',
        status: 'cancel_requested',
        input: { conversation_id: 'conv_new' },
      })
      .mockResolvedValueOnce({
        id: 'task_1',
        status: 'cancelled',
        input: { conversation_id: 'conv_new' },
      })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    const stopButton = wrapper.find('.composer-submit')
    expect(stopButton.attributes('aria-label')).toBe('停止')

    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(api.cancelTask).toHaveBeenNthCalledWith(1, 'task_1')
    expect(api.cancelTask).toHaveBeenNthCalledWith(2, 'task_1')

    runningStream.resolve({ conversation_id: 'conv_new' })
    await flushPromises()
  })

  it('submits approval decisions from the inline approval card through shared API helpers', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()

    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_1', input: { conversation_id: 'conv_new' } })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onTextDelta: () => void, onEvent: (event: any) => void) => {
      onEvent({
        type: 'approval.requested',
        seq: 1,
        payload: {
          approval_id: 'approval_1',
          task_id: 'task_1',
          conversation_id: 'conv_new',
          step: 4,
          tool_call_id: 'call_1',
          tool_name: 'bash',
          arguments_summary: 'rm -rf /tmp/demo',
          risk_level: 'high',
          reason: 'dangerous filesystem mutation',
          status: 'pending',
        },
      })
      return runningStream.promise
    })
    api.decideTaskApproval.mockResolvedValue({
      id: 'approval_1',
      task_id: 'task_1',
      conversation_id: 'conv_new',
      step_index: 4,
      tool_call_id: 'call_1',
      tool_name: 'bash',
      arguments_summary: 'rm -rf /tmp/demo',
      risk_level: 'high',
      status: 'approved',
      decision_reason: 'approved inline',
      decision_by: 'demo-user',
      created_at: '2026-03-29T09:00:00Z',
      updated_at: '2026-03-29T09:01:00Z',
    })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    wrapper.findComponent(MessageList).vm.$emit('approval-decision', {
      taskId: 'task_1',
      approvalId: 'approval_1',
      decision: 'approve',
      reason: 'approved inline',
    })
    await flushPromises()

    expect(api.decideTaskApproval).toHaveBeenCalledWith('task_1', 'approval_1', {
      decision: 'approve',
      reason: 'approved inline',
    })

    runningStream.resolve({ conversation_id: 'conv_new' })
    await flushPromises()
  })

  it('resumes live streaming after approving a waiting task so resumed events appear in transcript', async () => {
    const resumedStream = createDeferred<{ conversation_id: string; provider_id?: string; model_id?: string }>()
    let streamCallCount = 0

    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_resume', input: { conversation_id: 'conv_resume' } })
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_resume',
      status: 'waiting',
      input: { conversation_id: 'conv_resume' },
      suspend_reason: 'waiting_for_tool_approval',
    })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onTextDelta: () => void, onEvent: (event: any) => void) => {
      streamCallCount += 1
      if (streamCallCount === 1) {
        onEvent({
          type: 'approval.requested',
          seq: 1,
          payload: {
            approval_id: 'approval_resume',
            task_id: 'task_resume',
            conversation_id: 'conv_resume',
            step: 4,
            tool_call_id: 'call_resume',
            tool_name: 'bash',
            arguments_summary: 'rm -rf /tmp/demo',
            risk_level: 'high',
            reason: 'dangerous filesystem mutation',
            status: 'pending',
          },
        })
        throw new Error('Task event stream disconnected')
      }

      onEvent({
        type: 'log.message',
        seq: 2,
        payload: { Kind: 'text_delta', Text: 'resumed output' },
      })
      onEvent({
        type: 'task.finished',
        seq: 3,
        payload: { status: 'succeeded' },
      })
      return resumedStream.promise
    })
    api.decideTaskApproval.mockResolvedValue({
      id: 'approval_resume',
      task_id: 'task_resume',
      conversation_id: 'conv_resume',
      step_index: 4,
      tool_call_id: 'call_resume',
      tool_name: 'bash',
      arguments_summary: 'rm -rf /tmp/demo',
      risk_level: 'high',
      status: 'approved',
      decision_reason: 'resume it',
      decision_by: 'demo-user',
      created_at: '2026-03-29T09:00:00Z',
      updated_at: '2026-03-29T09:01:00Z',
    })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(api.streamRunTask).toHaveBeenCalledTimes(1)

    wrapper.findComponent(MessageList).vm.$emit('approval-decision', {
      taskId: 'task_resume',
      approvalId: 'approval_resume',
      decision: 'approve',
      reason: 'resume it',
    })
    await flushPromises()

    expect(api.decideTaskApproval).toHaveBeenCalledWith('task_resume', 'approval_resume', {
      decision: 'approve',
      reason: 'resume it',
    })
    expect(api.streamRunTask).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('resumed output')

    resumedStream.resolve({ conversation_id: 'conv_resume' })
    await flushPromises()
  })

  it('submits reject decisions from the inline approval card through shared API helpers', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()

    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_2', input: { conversation_id: 'conv_reject' } })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onTextDelta: () => void, onEvent: (event: any) => void) => {
      onEvent({
        type: 'approval.requested',
        seq: 1,
        payload: {
          approval_id: 'approval_2',
          task_id: 'task_2',
          conversation_id: 'conv_reject',
          step: 5,
          tool_call_id: 'call_2',
          tool_name: 'delete_file',
          arguments_summary: '{"path":"danger.txt"}',
          risk_level: 'high',
          reason: 'dangerous file mutation',
          status: 'pending',
        },
      })
      return runningStream.promise
    })
    api.decideTaskApproval.mockResolvedValue({
      id: 'approval_2',
      task_id: 'task_2',
      conversation_id: 'conv_reject',
      step_index: 5,
      tool_call_id: 'call_2',
      tool_name: 'delete_file',
      arguments_summary: '{"path":"danger.txt"}',
      risk_level: 'high',
      status: 'rejected',
      decision_reason: 'not safe',
      decision_by: 'demo-user',
      created_at: '2026-03-29T09:00:00Z',
      updated_at: '2026-03-29T09:01:00Z',
    })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    wrapper.findComponent(MessageList).vm.$emit('approval-decision', {
      taskId: 'task_2',
      approvalId: 'approval_2',
      decision: 'reject',
      reason: 'not safe',
    })
    await flushPromises()

    expect(api.decideTaskApproval).toHaveBeenCalledWith('task_2', 'approval_2', {
      decision: 'reject',
      reason: 'not safe',
    })
    expect(wrapper.find('.error-banner').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('运行失败')

    runningStream.resolve({ conversation_id: 'conv_reject' })
    await flushPromises()
  })

  it('does not render waiting_for_tool_approval as a failure state after stream interruption', async () => {
    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_waiting', input: { conversation_id: 'conv_waiting' } })
    api.streamRunTask.mockRejectedValue(new Error('Task event stream disconnected'))
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_waiting',
      status: 'waiting',
      input: { conversation_id: 'conv_waiting' },
      suspend_reason: 'waiting_for_tool_approval',
    })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(api.fetchTaskDetails).toHaveBeenCalledWith('task_waiting')
    expect(wrapper.find('.error-banner').exists()).toBe(false)
    expect(wrapper.find('.trace-block.error').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('运行失败')
  })

  it('sends the currently selected provider/model instead of a hardcoded default', async () => {
    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_1' })
    api.streamRunTask.mockResolvedValue({ conversation_id: 'conv_new', provider_id: 'google', model_id: 'gemini-2.5-flash' })
    api.fetchConversationMessages.mockResolvedValue([
      { role: 'assistant', content: 'assistant answer', provider_id: 'google', model_id: 'gemini-2.5-flash' },
    ])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('.model-menu-trigger').trigger('click')
    await flushPromises()
    expect(wrapper.find('.model-menu-panel').exists()).toBe(true)
    expect(wrapper.findAll('.model-menu-group-label').map((item) => item.text())).toEqual(['openai', 'google'])
    expect(wrapper.find('.model-menu-option.active').find('.model-menu-option-check').exists()).toBe(true)
    await wrapper.find('[data-model-option="gemini-2.5-flash"]').trigger('click')
    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(api.createRunTask).toHaveBeenCalledWith({
      createdBy: 'demo-user',
      conversationId: undefined,
      providerId: 'google',
      modelId: 'gemini-2.5-flash',
      message: 'hello',
    })
    expect(wrapper.find('.model-menu-trigger').text()).toContain('Gemini 2.5 Flash')
    expect(wrapper.find('.model-menu-trigger').text()).not.toContain('google')
    expect(wrapper.findAll('select')).toHaveLength(0)
  })

  it('places the compact model menu before the conversation title in the topbar', async () => {
    api.fetchConversations.mockResolvedValue([])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    const titleBlock = wrapper.find('.topbar-title-block')
    expect(titleBlock.find('.model-menu').exists()).toBe(true)
    expect(titleBlock.find('.topbar-conversation-title').exists()).toBe(true)
    expect(titleBlock.element.firstElementChild?.classList.contains('model-menu')).toBe(true)
  })

  it('keeps finish token stats visible without refetching the full conversation after completion', async () => {
    api.fetchConversations
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        {
          id: 'conv_new',
          title: 'Newest chat',
          last_message: 'assistant answer',
          message_count: 2,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
      ])
    api.createRunTask.mockResolvedValue({ id: 'task_1' })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onOpen: () => void, onEvent: (event: any) => void) => {
      onEvent({ type: 'log.message', payload: { Kind: 'text_delta', Text: 'assistant answer' } })
      return {
        conversation_id: 'conv_new',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        usage: {
          prompt_tokens: 321,
          completion_tokens: 54,
          total_tokens: 375,
        },
      }
    })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    const usage = wrapper.find('.trace-reply-usage')
    expect(usage.exists()).toBe(true)
    expect(api.fetchConversationMessages).not.toHaveBeenCalled()
    expect(usage.text()).toContain('321')
    expect(usage.text()).toContain('54')
    expect(usage.text()).toContain('375')
  })

  it('keeps a background new-conversation stream out of the currently selected history conversation', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()
    let emitStreamEvent: ((event: any) => void) | null = null

    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_old',
        title: 'Old chat',
        last_message: 'old history',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockImplementation(async (conversationId: string) => {
      if (conversationId === 'conv_old') {
        return [{ role: 'assistant', content: 'old history' }]
      }
      return []
    })
    api.createRunTask.mockResolvedValue({ id: 'task_new', input: { conversation_id: 'conv_new' } })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onOpen: () => void, onEvent: (event: any) => void) => {
      emitStreamEvent = onEvent
      return runningStream.promise
    })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello from new chat')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    const conversations = wrapper.findAll('.conversation-card')
    expect(conversations).toHaveLength(2)
    await conversations[1].trigger('click')
    await flushPromises()

    emitStreamEvent?.({ type: 'log.message', payload: { Kind: 'text_delta', Text: 'background partial' } })
    await flushPromises()

    expect(wrapper.text()).toContain('old history')
    expect(wrapper.text()).not.toContain('background partial')

    runningStream.resolve({ conversation_id: 'conv_new' })
    await flushPromises()
  })

  it('replays buffered stream content after reopening and selecting the pending conversation before completion', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()
    const streamListeners: Array<(event: any) => void> = []

    api.fetchConversations
      .mockResolvedValueOnce([
        {
          id: 'conv_old',
          title: 'Old chat',
          last_message: 'old history',
          message_count: 2,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 'conv_old',
          title: 'Old chat',
          last_message: 'old history',
          message_count: 2,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
        {
          id: 'conv_new',
          title: 'Pending new chat',
          last_message: '',
          message_count: 1,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 'conv_old',
          title: 'Old chat',
          last_message: 'old history',
          message_count: 2,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
        {
          id: 'conv_new',
          title: 'Pending new chat',
          last_message: '',
          message_count: 1,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 'conv_old',
          title: 'Old chat',
          last_message: 'old history',
          message_count: 2,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
        {
          id: 'conv_new',
          title: 'Pending new chat',
          last_message: '',
          message_count: 1,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          created_by: 'demo-user',
          created_at: '',
          updated_at: '',
        },
      ])
    api.fetchConversationMessages.mockImplementation(async (conversationId: string) => {
      if (conversationId === 'conv_old') {
        return [{ role: 'assistant', content: 'old history' }]
      }
      if (conversationId === 'conv_new') {
        return []
      }
      return []
    })
    api.createRunTask.mockResolvedValue({ id: 'task_new', input: { conversation_id: 'conv_new' } })
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_new',
      status: 'running',
      input: { conversation_id: 'conv_new' },
    })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onOpen: () => void, onEvent: (event: any) => void) => {
      streamListeners.push(onEvent)
      return runningStream.promise
    })

    const firstRouter = makeRouter()
    await firstRouter.push('/chat')
    await firstRouter.isReady()

    const firstWrapper = mount(ChatView, {
      global: {
        plugins: [firstRouter],
      },
    })

    await flushPromises()
    await firstWrapper.find('textarea').setValue('hello from new chat')
    await firstWrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    const firstConversationButtons = firstWrapper.findAll('.conversation-card')
    await firstConversationButtons[0].trigger('click')
    await flushPromises()

    streamListeners[0]?.({ type: 'log.message', payload: { Kind: 'text_delta', Text: 'background partial' } })
    await flushPromises()

    firstWrapper.unmount()
    await flushPromises()

    const secondRouter = makeRouter()
    await secondRouter.push('/chat')
    await secondRouter.isReady()

    const secondWrapper = mount(ChatView, {
      global: {
        plugins: [secondRouter],
      },
    })

    await flushPromises()
    const conversationButtons = secondWrapper.findAll('.conversation-card')
    expect(conversationButtons).toHaveLength(2)
    await conversationButtons[1].trigger('click')
    await flushPromises()

    expect(secondWrapper.text()).toContain('background partial')

    streamListeners[1]?.({ type: 'log.message', payload: { Kind: 'text_delta', Text: ' continued' } })
    await flushPromises()

    expect(secondWrapper.text()).toContain('background partial continued')

    runningStream.resolve({ conversation_id: 'conv_new' })
    await flushPromises()
  })

  it('uses a drawer sidebar on narrow screens and removes the redundant conversation eyebrow', async () => {
    setViewportWidth(800)
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: 'Mobile chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(wrapper.find('.chat-shell').classes()).toContain('sidebar-mobile')
    expect(wrapper.find('.chat-shell').classes()).not.toContain('sidebar-open')
    expect(wrapper.find('.topbar-sidebar-toggle').exists()).toBe(true)
    expect(wrapper.find('.topbar-user').exists()).toBe(false)
    expect(wrapper.find('.topbar-actions').exists()).toBe(false)
    expect(wrapper.find('.topbar-title-block').text()).not.toContain('Conversation')
    expect(wrapper.find('.topbar-conversation-title').text()).toBe('新对话')
    expect(wrapper.find('.topbar .status-pill').exists()).toBe(true)
    expect(wrapper.find('.sidebar-account-name').text()).toContain('demo-user')
    expect(wrapper.find('.sidebar-user-menu-trigger').exists()).toBe(true)

    await wrapper.find('.topbar-sidebar-toggle').trigger('click')

    expect(wrapper.find('.chat-shell').classes()).toContain('sidebar-open')
  })

  it('keeps the mobile account menu above the drawer and uses a blurred backdrop', async () => {
    setViewportWidth(800)
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: 'Mobile chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('.topbar-sidebar-toggle').trigger('click')
    await flushPromises()

    expect(wrapper.find('.sidebar-backdrop').exists()).toBe(true)

    await wrapper.find('.sidebar-user-menu-trigger').trigger('click')
    await flushPromises()

    expect(document.body.querySelector('.sidebar-user-menu-panel')).not.toBeNull()
    expect(chatStyles).toMatch(/\.sidebar-panel\s*\{[\s\S]*?z-index:\s*30;/)
    expect(chatStyles).toMatch(/\.sidebar-user-menu-panel\s*\{[\s\S]*?z-index:\s*(?:3[1-9]|[4-9]\d|\d{3,});/)
    expect(chatStyles).toMatch(/\.sidebar-backdrop\s*\{[\s\S]*?backdrop-filter:\s*blur\(/)
  })

  it('moves the sidebar reopen control into the chat stage when desktop sidebar is collapsed', async () => {
    setViewportWidth(1280)
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: 'Desktop chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    expect(wrapper.find('.topbar-sidebar-toggle').exists()).toBe(false)

    await wrapper.find('.sidebar-toggle').trigger('click')
    await flushPromises()

    expect(wrapper.find('.chat-shell').classes()).toContain('sidebar-hidden')
    expect(wrapper.find('.topbar-sidebar-toggle').exists()).toBe(true)

    await wrapper.find('.topbar-sidebar-toggle').trigger('click')
    await flushPromises()

    expect(wrapper.find('.chat-shell').classes()).not.toContain('sidebar-hidden')
    expect(wrapper.find('.topbar-sidebar-toggle').exists()).toBe(false)
  })

  it('keeps the mobile drawer fully expanded after resizing from desktop hidden mode', async () => {
    setViewportWidth(1280)
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: 'Desktop chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('.sidebar-toggle').trigger('click')
    await flushPromises()

    setViewportWidth(800)
    await flushPromises()
    await wrapper.find('.topbar-sidebar-toggle').trigger('click')
    await flushPromises()

    expect(wrapper.find('.chat-shell').classes()).toContain('sidebar-mobile')
    expect(wrapper.find('.chat-shell').classes()).toContain('sidebar-open')
    expect(wrapper.find('.conversation-title').exists()).toBe(true)
    expect(wrapper.find('.conversation-compact-label').exists()).toBe(false)
    expect(wrapper.find('.conversation-delete-button').exists()).toBe(true)
  })

  it('marks the desktop-hidden sidebar inert while collapsed', async () => {
    setViewportWidth(1280)
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: 'Desktop chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    expect(wrapper.find('.sidebar-panel').attributes('aria-hidden')).toBeUndefined()
    expect(wrapper.find('.sidebar-panel').attributes('inert')).toBeUndefined()

    await wrapper.find('.sidebar-toggle').trigger('click')
    await flushPromises()

    expect(wrapper.find('.sidebar-panel').attributes('aria-hidden')).toBe('true')
    expect(wrapper.find('.sidebar-panel').attributes('inert')).toBe('true')
  })

  it('shows animated syncing status beside the title while sending', async () => {
    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_1' })
    api.streamRunTask.mockImplementation(() => new Promise(() => {}))

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    const status = wrapper.find('.topbar .status-pill')
    expect(status.exists()).toBe(true)
    expect(status.text()).toContain('同步中')
    expect(status.classes()).toContain('loading')
    expect(wrapper.find('.messages-generating-indicator').exists()).toBe(true)
  })
})
