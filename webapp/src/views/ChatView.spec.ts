import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'

const api = vi.hoisted(() => ({
  TASK_STREAM_ABORTED_MESSAGE: 'Task event stream aborted',
  createRunTask: vi.fn(),
  deleteConversation: vi.fn(),
  fetchModelCatalog: vi.fn(),
  fetchConversationMessages: vi.fn(),
  fetchConversations: vi.fn(),
  findRunningTaskByConversation: vi.fn(),
  fetchTaskDetails: vi.fn(),
  streamRunTask: vi.fn(),
}))

vi.mock('../lib/api', () => api)

import ChatView from './ChatView.vue'

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

describe('ChatView', () => {
  beforeEach(() => {
    setViewportWidth(1280)
    localStorage.setItem('agent-runtime.user', 'demo-user')
    localStorage.removeItem('agent-runtime.chat-state')
    api.fetchModelCatalog.mockReset()
    api.fetchConversations.mockReset()
    api.fetchConversationMessages.mockReset()
    api.createRunTask.mockReset()
    api.deleteConversation.mockReset()
    api.findRunningTaskByConversation.mockReset()
    api.fetchTaskDetails.mockReset()
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

  it('shows an admin audit entry point for admin users only', async () => {
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

    const link = wrapper.find('.topbar-audit-link')
    expect(link.exists()).toBe(true)
    expect(link.text()).toContain('审计')
    expect(link.attributes('href')).toBe('/admin/audit')
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
    api.createRunTask.mockResolvedValue({ id: 'task_1' })
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

    expect(api.fetchConversations).toHaveBeenCalledTimes(2)
    expect(api.fetchConversationMessages).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Newest chat')
    expect(wrapper.find('.conversation-card.active').text()).toContain('Newest chat')
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
    expect(wrapper.find('.sidebar-account-logout').exists()).toBe(true)

    await wrapper.find('.topbar-sidebar-toggle').trigger('click')

    expect(wrapper.find('.chat-shell').classes()).toContain('sidebar-open')
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
  })
})
