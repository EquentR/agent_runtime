import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import ElementPlus from 'element-plus'
import { config, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'

const chatStyles = readFileSync(resolve(process.cwd(), 'src/style.css'), 'utf8')

config.global.plugins = [ElementPlus]

const api = vi.hoisted(() => ({
  TASK_STREAM_ABORTED_MESSAGE: 'Task event stream aborted',
  cancelTask: vi.fn(),
  createRunTask: vi.fn(),
  decideTaskApproval: vi.fn(),
  deleteAttachment: vi.fn(),
  deleteConversation: vi.fn(),
  getAttachmentContentURL: vi.fn(() => ''),
  fetchModelCatalog: vi.fn(),
  fetchConversationMessages: vi.fn(),
  fetchConversations: vi.fn(),
  fetchSkills: vi.fn(),
  fetchTaskInteractions: vi.fn(),
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
  normalizeInteractionRecord: vi.fn((interaction: any) => ({
    id: interaction.id ?? '',
    task_id: interaction.task_id ?? '',
    conversation_id: interaction.conversation_id ?? '',
    step_index: interaction.step_index,
    tool_call_id: interaction.tool_call_id ?? '',
    kind: interaction.kind ?? '',
    status: interaction.status ?? '',
    request_json: interaction.request_json,
    response_json: interaction.response_json,
    responded_by: interaction.responded_by,
    responded_at: interaction.responded_at,
    created_at: interaction.created_at,
    updated_at: interaction.updated_at,
  })),
  normalizeMemoryCompressionSnapshot: vi.fn((compression: any) => compression),
  normalizeMemoryContextSnapshot: vi.fn((state: any) => state),
  normalizeTranscriptTokenUsage: vi.fn((usage: any) => usage),
  respondTaskInteraction: vi.fn(),
  streamRunTask: vi.fn(),
  uploadAttachment: vi.fn(),
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
    routes: [{ path: '/chat/:conversationId?', component: ChatView }],
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
    api.fetchSkills.mockReset()
    api.createRunTask.mockReset()
    api.decideTaskApproval.mockReset()
    api.deleteAttachment.mockReset()
    api.deleteConversation.mockReset()
    api.fetchTaskInteractions.mockReset()
    api.findRunningTaskByConversation.mockReset()
    api.fetchTaskDetails.mockReset()
    api.fetchTaskApprovals.mockReset()
    api.respondTaskInteraction.mockReset()
    api.streamRunTask.mockReset()
    api.uploadAttachment.mockReset()
    api.fetchModelCatalog.mockResolvedValue({
      default_provider_id: 'openai',
      default_model_id: 'gpt-5.4',
      providers: [
        {
          id: 'openai',
          name: 'openai',
          models: [
            { id: 'gpt-5.4', name: 'GPT-5.4', type: 'chat', capabilities: { attachments: true } },
          ],
        },
        {
          id: 'google',
          name: 'google',
          models: [
            { id: 'gemini-2.5-flash', name: 'Gemini 2.5 Flash', type: 'chat', capabilities: { attachments: true } },
          ],
        },
      ],
    })
    api.fetchSkills.mockResolvedValue([
      { name: 'debugging', source_ref: 'skills/debugging/SKILL.md' },
      { name: 'review', source_ref: 'skills/review/SKILL.md' },
    ])
  })

  afterEach(() => {
    document.body.querySelectorAll('.sidebar-user-menu-panel, .sidebar-confirm-overlay').forEach((node) => {
      node.parentNode?.removeChild(node)
    })
  })

  it('starts catalog, skills, and conversations loading concurrently on mount', async () => {
    const catalogDeferred = createDeferred<{
      default_provider_id: string
      default_model_id: string
      providers: Array<{ id: string; name: string; models: Array<{ id: string; name: string; type: string }> }>
    }>()
    const skillsDeferred = createDeferred<Array<{ name: string; source_ref: string }>>()
    const conversationsDeferred = createDeferred<any[]>()

    api.fetchModelCatalog.mockImplementation(() => catalogDeferred.promise)
    api.fetchSkills.mockImplementation(() => skillsDeferred.promise)
    api.fetchConversations.mockImplementation(() => conversationsDeferred.promise)

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    expect(api.fetchModelCatalog).toHaveBeenCalledTimes(1)
    expect(api.fetchSkills).toHaveBeenCalledTimes(1)
    expect(api.fetchConversations).toHaveBeenCalledTimes(1)

    catalogDeferred.resolve({
      default_provider_id: 'openai',
      default_model_id: 'gpt-5.4',
      providers: [
        {
          id: 'openai',
          name: 'openai',
          models: [{ id: 'gpt-5.4', name: 'GPT-5.4', type: 'chat' }],
        },
      ],
    })
    skillsDeferred.resolve([{ name: 'debugging', source_ref: 'skills/debugging/SKILL.md' }])
    conversationsDeferred.resolve([])

    await flushPromises()
  })

  it('resumes SSE for a running task after reopening the chat view', async () => {
    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: 'conv_1',
        activeTaskId: 'task_1',
        entries: [{ id: 'reply-1', kind: 'reply', title: '', content: 'partial answer' }],
        selectedSkillsByConversation: {},
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
    await router.push('/chat/conv_1')
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
        selectedSkillsByConversation: {},
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
    await router.push('/chat/conv_waiting')
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
        selectedSkillsByConversation: {},
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
    await router.push('/chat/conv_waiting')
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

  it('navigates to the selected conversation route and keeps background stream updates isolated', async () => {
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
      return []
    })
    api.findRunningTaskByConversation.mockResolvedValue(null)
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_new',
      status: 'running',
      input: { conversation_id: 'conv_new' },
    })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onOpen: () => void, onEvent: (event: any) => void) => {
      emitStreamEvent = onEvent
      return runningStream.promise
    })

    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: 'conv_new',
        activeTaskId: 'task_new',
        activeTaskEventSeq: 0,
        entries: [],
        draftEntriesByConversation: {
          conv_new: [{ id: 'reply-new', kind: 'reply', title: '', content: 'background partial' }],
          conv_old: [{ id: 'reply-old', kind: 'reply', title: '', content: 'old history' }],
        },
        selectedSkillsByConversation: {},
      }),
    )

    const router = makeRouter()
    await router.push('/chat/conv_old')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    expect(wrapper.text()).toContain('old history')
    expect(wrapper.text()).not.toContain('background partial')

    emitStreamEvent?.({
      task_id: 'task_new',
      seq: 1,
      type: 'log.message',
      payload: { Kind: 'text_delta', Text: 'background partial' },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('old history')
    expect(wrapper.text()).not.toContain('background partial')
    expect(String(router.currentRoute.value.params.conversationId ?? '')).toBe('conv_old')

    const buttons = wrapper.findAll('.conversation-card')
    await buttons[1].trigger('click')
    await flushPromises()

    expect(String(router.currentRoute.value.params.conversationId ?? '')).toBe('conv_new')
    expect(wrapper.text()).toContain('Pending new chat')

    runningStream.resolve({ conversation_id: 'conv_new' })
    await flushPromises()
  })

  it('shows context usage from backend memory snapshot instead of reply prompt tokens', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_stats',
        title: 'Stats chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'aihubmix',
        model_id: 'gpt-5.4',
        memory_context: {
          short_term_tokens: 100,
          summary_tokens: 50,
          rendered_summary_tokens: 50,
          total_tokens: 150,
          short_term_limit: 2000,
          summary_limit: 4000,
          max_context_tokens: 922000,
          has_summary: true,
        },
        memory_compression: {
          tokens_before: 999,
          tokens_after: 888,
          short_term_tokens_before: 999,
          short_term_tokens_after: 888,
          summary_tokens_before: 0,
          summary_tokens_after: 0,
          rendered_summary_tokens_before: 0,
          rendered_summary_tokens_after: 0,
          total_tokens_before: 999,
          total_tokens_after: 888,
        },
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([
      {
        role: 'assistant',
        content: 'done',
        provider_id: 'aihubmix',
        model_id: 'gpt-5.4',
        usage: {
          prompt_tokens: 320,
          cached_prompt_tokens: 80,
          completion_tokens: 40,
          total_tokens: 360,
        },
      },
    ])

    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: 'conv_stats',
        activeTaskId: 'task_stats',
        activeTaskEventSeq: 0,
        activeTaskIdByConversation: {
          conv_stats: 'task_stats',
        },
        entries: [{ id: 'reply-stats', kind: 'reply', title: '', content: 'done', provider_id: 'aihubmix', model_id: 'gpt-5.4', token_usage: { prompt_tokens: 320, cached_prompt_tokens: 80, completion_tokens: 40, total_tokens: 360 } }],
        draftEntriesByConversation: {
          conv_stats: [
            { id: 'reply-stats', kind: 'reply', title: '', content: 'done', provider_id: 'aihubmix', model_id: 'gpt-5.4', token_usage: { prompt_tokens: 320, cached_prompt_tokens: 80, completion_tokens: 40, total_tokens: 360 } },
            {
              id: 'memory-context-stats',
              kind: 'memory',
              title: '',
              memory_context_state: {
                short_term_tokens: 610,
                summary_tokens: 290,
                rendered_summary_tokens: 290,
                total_tokens: 900,
                short_term_limit: 2000,
                summary_limit: 4000,
                max_context_tokens: 922000,
                has_summary: true,
              },
            },
            {
              id: 'memory-stats',
              kind: 'memory',
              title: '记忆压缩',
              content: '1,200 → 900 tokens',
              memory_compression: {
                tokens_before: 1200,
                tokens_after: 900,
                short_term_tokens_before: 1200,
                short_term_tokens_after: 600,
                summary_tokens_before: 0,
                summary_tokens_after: 300,
                rendered_summary_tokens_before: 0,
                rendered_summary_tokens_after: 300,
                total_tokens_before: 1200,
                total_tokens_after: 900,
              },
            },
          ],
        },
        selectedSkillsByConversation: {},
      }),
    )

    const router = makeRouter()
    await router.push('/chat/conv_stats')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    const trigger = wrapper.find('[data-context-stats-trigger]')
    expect(trigger.exists()).toBe(true)
    expect(trigger.text()).toContain('0.10%')
    expect(trigger.attributes('title')).toContain('900')
    expect(trigger.attributes('title')).toContain('922,000')
    expect(trigger.attributes('title')).not.toContain('320')

    await trigger.trigger('click')
    await flushPromises()

    const panels = Array.from(document.body.querySelectorAll('[data-context-stats-panel]'))
    const panel = panels.at(-1) ?? null
    expect(panel).not.toBeNull()
    expect(panel?.textContent ?? '').toContain('gpt-5.4')
    expect(panel?.textContent ?? '').toContain('900')
    expect(panel?.textContent ?? '').toContain('922,000')
    expect(panel?.textContent ?? '').toContain('1,200')
    expect(panel?.textContent ?? '').not.toContain('Prompt')
    expect(panel?.textContent ?? '').not.toContain('Cached')
    expect(panel?.textContent ?? '').not.toContain('Output')
    expect(panel?.textContent ?? '').not.toContain('320')
  })

  it('prefers persisted conversation memory snapshot over stale draft memory when conversation is idle', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_reload',
        title: 'Reloaded chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        memory_context: {
          short_term_tokens: 450,
          summary_tokens: 150,
          rendered_summary_tokens: 150,
          total_tokens: 600,
          short_term_limit: 2000,
          summary_limit: 4000,
          max_context_tokens: 922000,
          has_summary: true,
        },
        memory_compression: {
          tokens_before: 1500,
          tokens_after: 600,
          short_term_tokens_before: 1500,
          short_term_tokens_after: 450,
          summary_tokens_before: 0,
          summary_tokens_after: 150,
          rendered_summary_tokens_before: 0,
          rendered_summary_tokens_after: 150,
          total_tokens_before: 1500,
          total_tokens_after: 600,
        },
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([
      {
        role: 'assistant',
        content: 'done',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        usage: {
          prompt_tokens: 320,
          cached_prompt_tokens: 80,
          completion_tokens: 40,
          total_tokens: 360,
        },
      },
    ])

    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: 'conv_reload',
        activeTaskId: '',
        activeTaskEventSeq: 0,
        entries: [],
        draftEntriesByConversation: {
          conv_reload: [
            { id: 'reply-stats', kind: 'reply', title: '', content: 'done', provider_id: 'openai', model_id: 'gpt-5.4', token_usage: { prompt_tokens: 320, cached_prompt_tokens: 80, completion_tokens: 40, total_tokens: 360 } },
            {
              id: 'memory-context-stats',
              kind: 'memory',
              title: '',
              memory_context_state: {
                short_term_tokens: 77,
                summary_tokens: 34,
                rendered_summary_tokens: 34,
                total_tokens: 111,
                short_term_limit: 888,
                summary_limit: 999,
                max_context_tokens: 123456,
                has_summary: true,
              },
            },
            {
              id: 'memory-stats',
              kind: 'memory',
              title: '记忆压缩',
              content: '9,999 → 111 tokens',
              memory_compression: {
                tokens_before: 9999,
                tokens_after: 111,
                short_term_tokens_before: 9999,
                short_term_tokens_after: 77,
                summary_tokens_before: 0,
                summary_tokens_after: 34,
                rendered_summary_tokens_before: 0,
                rendered_summary_tokens_after: 34,
                total_tokens_before: 9999,
                total_tokens_after: 111,
              },
            },
          ],
        },
        selectedSkillsByConversation: {},
      }),
    )

    const router = makeRouter()
    await router.push('/chat/conv_reload')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    const trigger = wrapper.find('[data-context-stats-trigger]')
    expect(trigger.exists()).toBe(true)
    expect(trigger.text()).toContain('0.07%')
    expect(trigger.attributes('title')).toContain('600')
    expect(trigger.attributes('title')).toContain('922,000')
    expect(trigger.attributes('title')).not.toContain('111')
    expect(trigger.attributes('title')).not.toContain('123,456')

    await trigger.trigger('click')
    await flushPromises()

    const panels = Array.from(document.body.querySelectorAll('[data-context-stats-panel]'))
    const panel = panels.at(-1) ?? null
    expect(panel).not.toBeNull()
    expect(panel?.textContent ?? '').toContain('600')
    expect(panel?.textContent ?? '').toContain('1,500')
    expect(panel?.textContent ?? '').not.toContain('9,999')
    expect(panel?.textContent ?? '').not.toContain('111')
    expect(panel?.textContent ?? '').not.toContain('Prompt')
    expect(panel?.textContent ?? '').not.toContain('Cached')
    expect(panel?.textContent ?? '').not.toContain('Output')
  })

  it('shows unknown context usage when no backend memory snapshot exists', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_unknown',
        title: 'Unknown stats chat',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'demo-user',
        created_at: '',
        updated_at: '',
      },
    ])
    api.fetchConversationMessages.mockResolvedValue([
      {
        role: 'assistant',
        content: 'done',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        usage: {
          prompt_tokens: 320,
          cached_prompt_tokens: 80,
          completion_tokens: 40,
          total_tokens: 360,
        },
      },
    ])

    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: 'conv_unknown',
        activeTaskId: '',
        activeTaskEventSeq: 0,
        entries: [],
        draftEntriesByConversation: {
          conv_unknown: [
            { id: 'reply-stats', kind: 'reply', title: '', content: 'done', provider_id: 'openai', model_id: 'gpt-5.4', token_usage: { prompt_tokens: 320, cached_prompt_tokens: 80, completion_tokens: 40, total_tokens: 360 } },
            { id: 'memory-stats', kind: 'memory', title: '记忆压缩', content: '1,200 → 900 tokens' },
          ],
        },
        selectedSkillsByConversation: {},
      }),
    )

    const router = makeRouter()
    await router.push('/chat/conv_unknown')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    const trigger = wrapper.find('[data-context-stats-trigger]')
    expect(trigger.exists()).toBe(true)
    expect(trigger.text()).toContain('--')
    expect(trigger.attributes('title')).toContain('-- / --')
    expect(trigger.attributes('title')).not.toContain('922,000')

    await trigger.trigger('click')
    await flushPromises()

    const panels = Array.from(document.body.querySelectorAll('[data-context-stats-panel]'))
    const panel = panels.at(-1) ?? null
    expect(panel).not.toBeNull()
    expect(panel?.textContent ?? '').toContain('已用 Token')
    expect(panel?.textContent ?? '').toContain('--')
    expect(panel?.textContent ?? '').not.toContain('900')
    expect(panel?.textContent ?? '').not.toContain('922,000')
    expect(panel?.textContent ?? '').not.toContain('Prompt')
    expect(panel?.textContent ?? '').not.toContain('Cached')
    expect(panel?.textContent ?? '').not.toContain('Output')
  })

  it('renders memory.compressed events in the active conversation transcript while streaming', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()

    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_1', input: { conversation_id: 'conv_new' } })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onTextDelta: () => void, onEvent: (event: any) => void) => {
      onEvent({
        task_id: 'task_1',
        seq: 1,
        type: 'memory.compressed',
        payload: {
          conversation_id: 'conv_new',
          tokens_before: 1200,
          tokens_after: 900,
        },
      })
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

    expect(wrapper.text()).toContain('记忆压缩')
    expect(wrapper.text()).toContain('1,200 → 900 tokens')

    runningStream.resolve({ conversation_id: 'conv_new' })
    await flushPromises()
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
    api.streamRunTask.mockImplementation(async () => runningStream.promise)

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

    expect(wrapper.find('.conversation-card.active').text()).toContain('Pending new chat')
    expect(wrapper.find('.topbar-conversation-title').text()).toContain('Pending new chat')

    runningStream.resolve({ conversation_id: 'conv_new' })
    await flushPromises()
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
        selectedSkillsByConversation: {},
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
    await router.push('/chat/conv_waiting')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(wrapper.find('.composer-approval-entry').exists()).toBe(false)
    expect(wrapper.find('.approval-card').exists()).toBe(true)
  })

  it('submits selected and custom answers for question interactions', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()

    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_question', input: { conversation_id: 'conv_question' } })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onTextDelta: () => void, onEvent: (event: any) => void) => {
      onEvent({
        type: 'interaction.requested',
        seq: 1,
        payload: {
          id: 'interaction_question_1',
          task_id: 'task_question',
          conversation_id: 'conv_question',
          kind: 'question',
          status: 'pending',
          request_json: {
            question: 'Which environment?',
            options: ['staging', 'production'],
            allow_custom: true,
          },
        },
      })
      return runningStream.promise
    })
    api.respondTaskInteraction.mockResolvedValue({
      id: 'interaction_question_1',
      task_id: 'task_question',
      conversation_id: 'conv_question',
      kind: 'question',
      status: 'responded',
      request_json: { question: 'Which environment?', options: ['staging', 'production'], allow_custom: true },
      response_json: { selected_option_id: 'staging', custom_text: 'Blue env' },
    })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    wrapper.findComponent(MessageList).vm.$emit('interaction-respond', {
      taskId: 'task_question',
      interactionId: 'interaction_question_1',
      selectedOptionId: 'staging',
      customText: 'Blue env',
    })
    await flushPromises()

    expect(api.respondTaskInteraction).toHaveBeenCalledWith('task_question', 'interaction_question_1', {
      selected_option_id: 'staging',
      custom_text: 'Blue env',
    })
  })

  it('submits multiple selected options for question interactions', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()

    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_question_multi', input: { conversation_id: 'conv_question_multi' } })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onTextDelta: () => void, onEvent: (event: any) => void) => {
      onEvent({
        type: 'interaction.requested',
        seq: 1,
        payload: {
          id: 'interaction_question_multi',
          task_id: 'task_question_multi',
          conversation_id: 'conv_question_multi',
          kind: 'question',
          status: 'pending',
          request_json: {
            question: 'Choose environments',
            options: ['staging', 'preview'],
            multiple: true,
            allow_custom: false,
          },
        },
      })
      return runningStream.promise
    })
    api.respondTaskInteraction.mockResolvedValue({
      id: 'interaction_question_multi',
      task_id: 'task_question_multi',
      conversation_id: 'conv_question_multi',
      kind: 'question',
      status: 'responded',
      request_json: { question: 'Choose environments', options: ['staging', 'preview'], multiple: true, allow_custom: false },
      response_json: { selected_option_ids: ['staging', 'preview'] },
    })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    wrapper.findComponent(MessageList).vm.$emit('interaction-respond', {
      taskId: 'task_question_multi',
      interactionId: 'interaction_question_multi',
      selectedOptionIds: ['staging', 'preview'],
    })
    await flushPromises()

    expect(api.respondTaskInteraction).toHaveBeenCalledWith('task_question_multi', 'interaction_question_multi', {
      selected_option_ids: ['staging', 'preview'],
      custom_text: undefined,
    })
  })

  it('prevents duplicate question submissions while the first response is still pending', async () => {
    const runningStream = createDeferred<{ conversation_id: string }>()
    const responding = createDeferred<any>()

    api.fetchConversations.mockResolvedValue([])
    api.createRunTask.mockResolvedValue({ id: 'task_question_lock', input: { conversation_id: 'conv_question_lock' } })
    api.streamRunTask.mockImplementation(async (_taskId: string, _onTextDelta: () => void, onEvent: (event: any) => void) => {
      onEvent({
        type: 'interaction.requested',
        seq: 1,
        payload: {
          id: 'interaction_question_lock',
          task_id: 'task_question_lock',
          conversation_id: 'conv_question_lock',
          kind: 'question',
          status: 'pending',
          request_json: {
            question: 'Choose one',
            options: ['staging', 'production'],
            allow_custom: false,
          },
        },
      })
      return runningStream.promise
    })
    api.respondTaskInteraction.mockReturnValue(responding.promise)

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    wrapper.findComponent(MessageList).vm.$emit('interaction-respond', {
      taskId: 'task_question_lock',
      interactionId: 'interaction_question_lock',
      selectedOptionId: 'staging',
    })
    await flushPromises()

    wrapper.findComponent(MessageList).vm.$emit('interaction-respond', {
      taskId: 'task_question_lock',
      interactionId: 'interaction_question_lock',
      selectedOptionId: 'production',
    })
    await flushPromises()

    expect(api.respondTaskInteraction).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent(MessageList).props('questionResponseStateById')).toEqual({
      interaction_question_lock: { pending: true },
    })

    responding.resolve({
      id: 'interaction_question_lock',
      task_id: 'task_question_lock',
      conversation_id: 'conv_question_lock',
      kind: 'question',
      status: 'responded',
      request_json: { question: 'Choose one', options: ['staging', 'production'], allow_custom: false },
      response_json: { selected_option_id: 'staging' },
    })
    runningStream.resolve({ conversation_id: 'conv_question_lock' })
    await flushPromises()
  })

  it('hydrates waiting_for_interaction cards when reopening a waiting task', async () => {
    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: 'conv_waiting_interaction',
        activeTaskId: 'task_waiting_interaction',
        activeTaskEventSeq: 0,
        entries: [],
        draftEntriesByConversation: {},
        selectedSkillsByConversation: {},
      }),
    )
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_waiting_interaction',
        title: 'Waiting interaction chat',
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
      id: 'task_waiting_interaction',
      status: 'waiting',
      input: { conversation_id: 'conv_waiting_interaction' },
      suspend_reason: 'waiting_for_interaction',
    })
    api.fetchTaskInteractions.mockResolvedValue([
      {
        id: 'interaction_waiting_1',
        task_id: 'task_waiting_interaction',
        conversation_id: 'conv_waiting_interaction',
        kind: 'question',
        status: 'pending',
        request_json: {
          question: 'Which environment?',
          options: ['staging', 'production'],
          allow_custom: true,
        },
      },
    ])
    api.streamRunTask.mockRejectedValue(new Error('Task event stream disconnected'))

    const router = makeRouter()
    await router.push('/chat/conv_waiting_interaction')
    await router.isReady()
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    const hydratedEntries = wrapper.findComponent(MessageList).props('entries') as Array<any>

    expect(api.fetchTaskInteractions).toHaveBeenCalledWith('task_waiting_interaction')
    expect(
      hydratedEntries.some(
        (entry) => entry.kind === 'question' && entry.question_interaction?.id === 'interaction_waiting_1',
      ),
    ).toBe(true)
    expect(wrapper.text()).not.toContain('运行失败')
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

    expect(api.createRunTask).toHaveBeenCalledWith(expect.objectContaining({
      providerId: 'google',
      modelId: 'gemini-2.5-flash',
    }))
  })

  it('sends attachment_ids with run task request', async () => {
    api.fetchConversations.mockResolvedValue([])
    api.uploadAttachment.mockResolvedValue({
      id: 'att_1',
      file_name: 'note.txt',
      mime_type: 'text/plain',
      status: 'draft',
      kind: 'text',
    })
    api.createRunTask.mockResolvedValue({ id: 'task_1', input: { conversation_id: 'conv_new' } })
    api.streamRunTask.mockResolvedValue({ conversation_id: 'conv_new' })

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    const composer = wrapper.findComponent({ name: 'MessageComposer' })
    composer.vm.$emit('add-attachments', [new File(['hello'], 'note.txt', { type: 'text/plain' })])
    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(api.createRunTask).toHaveBeenCalledWith(expect.objectContaining({
      attachmentIds: ['att_1'],
    }))
  })

  it('hides uploader for models without attachment capability', async () => {
    api.fetchModelCatalog.mockResolvedValue({
      default_provider_id: 'openai',
      default_model_id: 'gpt-5.4',
      providers: [
        {
          id: 'openai',
          name: 'openai',
          models: [
            { id: 'gpt-5.4', name: 'GPT-5.4', type: 'chat', capabilities: { attachments: false } },
          ],
        },
      ],
    })
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

    const composer = wrapper.findComponent({ name: 'MessageComposer' })
    expect(composer.props('attachmentsEnabled')).toBe(false)
  })

  it('keeps attachment in failed state when backend delete fails', async () => {
    api.fetchConversations.mockResolvedValue([])
    api.uploadAttachment.mockResolvedValue({
      id: 'att_1',
      file_name: 'note.txt',
      mime_type: 'text/plain',
      status: 'draft',
      kind: 'text',
    })
    api.deleteAttachment.mockRejectedValue(new Error('delete failed'))

    const router = makeRouter()
    await router.push('/chat')
    await router.isReady()

    const wrapper = mount(ChatView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    const composer = wrapper.findComponent({ name: 'MessageComposer' })
    composer.vm.$emit('add-attachments', [new File(['hello'], 'note.txt', { type: 'text/plain' })])
    await flushPromises()
    const uploadedAttachments = wrapper.findComponent({ name: 'MessageComposer' }).props('attachments') as Array<Record<string, unknown>>
    composer.vm.$emit('remove-attachment', uploadedAttachments[0].local_id)
    await flushPromises()

    const attachments = wrapper.findComponent({ name: 'MessageComposer' }).props('attachments') as Array<Record<string, unknown>>
    expect(attachments).toHaveLength(1)
    expect(attachments[0].upload_state).toBe('failed')
    expect(attachments[0].error_message).toBe('delete failed')
  })

  it('restores per-conversation skill selection from local storage', async () => {
    localStorage.setItem(
      'agent-runtime.chat-state',
      JSON.stringify({
        activeConversationId: 'conv_1',
        activeTaskId: '',
        activeTaskEventSeq: 0,
        entries: [],
        draftEntriesByConversation: {},
        selectedSkillsByConversation: {
          conv_1: ['review'],
        },
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

    const router = makeRouter()
    await router.push('/chat/conv_1')
    await router.isReady()

    const wrapper = mount(ChatView, {
      attachTo: document.body,
      global: {
        plugins: [router],
        stubs: { ElSelect: true, ElOption: true },
      },
    })

    await flushPromises()

    const composer = wrapper.findComponent({ name: 'MessageComposer' })
    expect(composer.exists()).toBe(true)
    expect(composer.props('selectedSkillNames')).toEqual(['review'])
  })

  it('falls back to the provider default model when a saved conversation model is unavailable', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_missing_model',
        title: 'Saved chat',
        last_message: 'hello',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'missing-model',
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

    expect(wrapper.find('.model-menu-trigger').text()).toContain('GPT-5.4')
  })

  it('keeps the model menu inline with the title block', async () => {
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
    await wrapper.find('.sidebar-toggle').trigger('click')
    await flushPromises()

    const panel = wrapper.find('.sidebar-panel')
    expect(panel.attributes('inert')).toBe('true')
    expect(panel.attributes('aria-hidden')).toBe('true')
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
