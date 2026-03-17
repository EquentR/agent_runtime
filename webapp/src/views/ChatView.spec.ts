import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'

const api = vi.hoisted(() => ({
  createRunTask: vi.fn(),
  fetchConversationMessages: vi.fn(),
  fetchConversations: vi.fn(),
  fetchTaskDetails: vi.fn(),
  streamRunTask: vi.fn(),
}))

vi.mock('../lib/api', () => api)

import ChatView from './ChatView.vue'

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/chat', component: ChatView }],
  })
}

describe('ChatView', () => {
  beforeEach(() => {
    localStorage.setItem('agent-runtime.user', 'demo-user')
    api.fetchConversations.mockReset()
    api.fetchConversationMessages.mockReset()
    api.createRunTask.mockReset()
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

    expect(wrapper.text()).toContain('Send message')
    expect(wrapper.text()).not.toContain('Sending...')
    expect(wrapper.text()).toContain('First chat')
    expect(wrapper.text()).toContain('Signed in as')
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
    api.fetchConversationMessages.mockResolvedValueOnce([{ role: 'assistant', content: 'first' }])
    api.fetchConversationMessages.mockResolvedValueOnce([{ role: 'assistant', content: 'second' }])

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
})
