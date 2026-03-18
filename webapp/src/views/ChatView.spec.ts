import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'

const api = vi.hoisted(() => ({
  createRunTask: vi.fn(),
  deleteConversation: vi.fn(),
  fetchConversationMessages: vi.fn(),
  fetchConversations: vi.fn(),
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
    api.fetchConversations.mockReset()
    api.fetchConversationMessages.mockReset()
    api.createRunTask.mockReset()
    api.deleteConversation.mockReset()
    api.fetchTaskDetails.mockReset()
    api.streamRunTask.mockReset()
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
    expect(wrapper.text()).toContain('Signed in as')
    expect(wrapper.find('.topbar .status-pill').text()).toContain('Ready')
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

  it('restores saved error trace when reopening without a persisted conversation', async () => {
    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: '',
        entries: [{ id: 'err-1', kind: 'error', title: 'Run failed', content: 'network lost' }],
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
    expect(wrapper.text()).toContain('Newest chat')
    expect(wrapper.find('.conversation-card.active').text()).toContain('Newest chat')
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
    expect(status.text()).toContain('Syncing')
    expect(status.classes()).toContain('loading')
  })
})
