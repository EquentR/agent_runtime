import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  fetchAuditConversationRuns: vi.fn(),
  fetchAuditRunReplay: vi.fn(),
  fetchConversation: vi.fn(),
  fetchConversations: vi.fn(),
}))

vi.mock('../lib/api', () => api)

import AdminAuditView from './AdminAuditView.vue'

function createDeferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('AdminAuditView', () => {
  beforeEach(() => {
    api.fetchConversations.mockReset()
    api.fetchConversation.mockReset()
    api.fetchAuditConversationRuns.mockReset()
    api.fetchAuditRunReplay.mockReset()
  })


  it('starts conversation and run loading together before fetching replays', async () => {
    const conversationDeferred = createDeferred<{
      id: string
      title: string
      last_message: string
      message_count: number
      provider_id: string
      model_id: string
      created_by: string
      created_at: string
      updated_at: string
      audit_run_id: string
    }>()
    const runsDeferred = createDeferred<Array<{
      id: string
      task_id: string
      conversation_id: string
      task_type: string
      status: 'succeeded'
      created_by: string
      schema_version: string
      created_at: string
      updated_at: string
    }>>()

    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_parallel',
        title: 'Parallel loading chat',
        last_message: 'hello',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'alice',
        created_at: '2026-03-22T14:00:00Z',
        updated_at: '2026-03-22T14:01:00Z',
        audit_run_id: 'run_parallel',
      },
    ])
    api.fetchConversation.mockImplementation(() => conversationDeferred.promise)
    api.fetchAuditConversationRuns.mockImplementation(() => runsDeferred.promise)
    api.fetchAuditRunReplay.mockResolvedValue({
      run: {
        id: 'run_parallel',
        task_id: 'task_parallel',
        conversation_id: 'conv_parallel',
        task_type: 'agent.run',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        runner_id: 'runner_1',
        status: 'succeeded',
        created_by: 'alice',
        replayable: true,
        schema_version: 'v1',
        created_at: '2026-03-22T14:00:00Z',
        updated_at: '2026-03-22T14:01:00Z',
      },
      timeline: [],
      artifacts: [],
    })

    const wrapper = mount(AdminAuditView, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to" v-bind="$attrs"><slot /></a>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper.find('[data-conversation-id="conv_parallel"]').trigger('click')

    expect(api.fetchConversation).toHaveBeenCalledWith('conv_parallel')
    expect(api.fetchAuditConversationRuns).toHaveBeenCalledWith('conv_parallel')
    expect(api.fetchAuditRunReplay).not.toHaveBeenCalled()

    conversationDeferred.resolve({
      id: 'conv_parallel',
      title: 'Parallel loading chat',
      last_message: 'hello',
      message_count: 1,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'alice',
      created_at: '2026-03-22T14:00:00Z',
      updated_at: '2026-03-22T14:01:00Z',
      audit_run_id: 'run_parallel',
    })
    await flushPromises()

    expect(api.fetchAuditRunReplay).not.toHaveBeenCalled()

    runsDeferred.resolve([
      {
        id: 'run_parallel',
        task_id: 'task_parallel',
        conversation_id: 'conv_parallel',
        task_type: 'agent.run',
        status: 'succeeded',
        created_by: 'alice',
        schema_version: 'v1',
        created_at: '2026-03-22T14:00:00Z',
        updated_at: '2026-03-22T14:01:00Z',
      },
    ])
    await flushPromises()

    expect(api.fetchAuditRunReplay).toHaveBeenCalledWith('run_parallel')
  })

  it('loads conversations and selected conversation audit details', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_1',
        title: 'First chat with a much longer title than the sidebar can safely display in full width',
        last_message: 'hello',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'alice',
        created_at: '2026-03-22T09:00:00Z',
        updated_at: '2026-03-22T09:01:00Z',
        audit_run_id: 'run_1',
      },
      {
        id: 'conv_2',
        title: 'Second chat with a much longer title than the sidebar can safely display in full width',
        last_message: 'world',
        message_count: 4,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'bob',
        created_at: '2026-03-22T10:00:00Z',
        updated_at: '2026-03-22T10:02:00Z',
        audit_run_id: 'run_2',
      },
      {
        id: 'conv_3',
        title: 'Third chat',
        last_message: 'later',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'cindy',
        created_at: '2026-03-22T11:00:00Z',
        updated_at: '2026-03-22T11:02:00Z',
        audit_run_id: 'run_3',
      },
    ])
    api.fetchConversation.mockImplementation(async (conversationId: string) => ({
      id: conversationId,
      title: conversationId === 'conv_2' ? 'Second chat' : conversationId === 'conv_3' ? 'Third chat' : 'First chat',
      last_message: conversationId === 'conv_2' ? 'world' : conversationId === 'conv_3' ? 'later' : 'hello',
      message_count: conversationId === 'conv_2' ? 4 : conversationId === 'conv_3' ? 1 : 2,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: conversationId === 'conv_2' ? 'bob' : conversationId === 'conv_3' ? 'cindy' : 'alice',
      created_at: conversationId === 'conv_2' ? '2026-03-22T10:00:00Z' : conversationId === 'conv_3' ? '2026-03-22T11:00:00Z' : '2026-03-22T09:00:00Z',
      updated_at: conversationId === 'conv_2' ? '2026-03-22T10:02:00Z' : conversationId === 'conv_3' ? '2026-03-22T11:02:00Z' : '2026-03-22T09:01:00Z',
      audit_run_id: conversationId === 'conv_2' ? 'run_2' : conversationId === 'conv_3' ? 'run_3' : 'run_1',
    }))
    api.fetchAuditConversationRuns.mockImplementation(async (conversationId: string) => {
      if (conversationId === 'conv_2') {
        return [{
          id: 'run_2',
          task_id: 'tsk_2',
          conversation_id: 'conv_2',
          task_type: 'agent.run',
          status: 'succeeded',
          created_by: 'bob',
          schema_version: 'v1',
          created_at: '2026-03-22T10:00:00Z',
          updated_at: '2026-03-22T10:02:00Z',
        }]
      }
      if (conversationId === 'conv_3') {
        return [{
          id: 'run_3',
          task_id: 'tsk_3',
          conversation_id: 'conv_3',
          task_type: 'agent.run',
          status: 'succeeded',
          created_by: 'cindy',
          schema_version: 'v1',
          created_at: '2026-03-22T11:00:00Z',
          updated_at: '2026-03-22T11:02:00Z',
        }]
      }
      return [{
        id: 'run_1',
        task_id: 'tsk_1',
        conversation_id: 'conv_1',
        task_type: 'agent.run',
        status: 'succeeded',
        created_by: 'alice',
        schema_version: 'v1',
        created_at: '2026-03-22T09:00:00Z',
        updated_at: '2026-03-22T09:01:00Z',
      }]
    })
    api.fetchAuditRunReplay.mockImplementation(async (runId: string) => {
      if (runId === 'run_3') {
        return {
          run: {
            id: 'run_3',
            task_id: 'tsk_3',
            conversation_id: 'conv_3',
            task_type: 'agent.run',
            provider_id: 'openai',
            model_id: 'gpt-5.4',
            runner_id: 'runner_1',
            status: 'succeeded',
            created_by: 'cindy',
            replayable: true,
            schema_version: 'v1',
            created_at: '2026-03-22T11:00:00Z',
            updated_at: '2026-03-22T11:02:00Z',
          },
          timeline: [
            {
              seq: 1,
              phase: 'run',
              event_type: 'run.started',
              level: 'info',
              step_index: 0,
              parent_seq: 0,
              payload: { status: 'running' },
              created_at: '2026-03-22T11:00:00Z',
            },
            {
              seq: 2,
              phase: 'run',
              event_type: 'run.finished',
              level: 'info',
              step_index: 1,
              parent_seq: 1,
              payload: { status: 'done' },
              created_at: '2026-03-22T11:00:12Z',
            },
          ],
          artifacts: [],
        }
      }

      return {
        run: {
          id: runId,
          task_id: runId === 'run_2' ? 'tsk_2' : 'tsk_1',
          conversation_id: runId === 'run_2' ? 'conv_2' : 'conv_1',
          task_type: 'agent.run',
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          runner_id: 'runner_1',
          status: 'succeeded',
          created_by: runId === 'run_2' ? 'bob' : 'alice',
          replayable: true,
          schema_version: 'v1',
          created_at: '2026-03-22T10:00:00Z',
          updated_at: '2026-03-22T10:02:00Z',
        },
        timeline: [
        {
          seq: 0,
          phase: 'run',
          event_type: 'run.created',
          level: 'info',
          step_index: 0,
          parent_seq: 0,
          payload: { status: 'queued' },
          created_at: '2026-03-22T09:59:58Z',
        },
        {
          seq: 1,
          phase: 'request',
          event_type: 'run.started',
          level: 'info',
          step_index: 0,
          parent_seq: 0,
          payload: { status: 'running' },
            created_at: '2026-03-22T10:00:00Z',
            artifact: {
              id: 'art_request',
              kind: 'request_messages',
              mime_type: 'application/json',
              encoding: 'utf-8',
              size_bytes: 128,
              redaction_state: 'raw',
              created_at: '2026-03-22T10:00:01Z',
            },
          },
        {
          seq: 2,
          phase: 'run',
          event_type: 'conversation.loaded',
          level: 'info',
          step_index: 0,
          parent_seq: 1,
          payload: { message_count: 2 },
          created_at: '2026-03-22T10:00:02Z',
        },
        {
          seq: 3,
          phase: 'run',
          event_type: 'step.started',
          level: 'info',
          step_index: 1,
          parent_seq: 2,
          payload: { step: 'tool-call' },
          created_at: '2026-03-22T10:00:03Z',
        },
        {
          seq: 4,
          phase: 'tool',
          event_type: 'tool.called',
          level: 'info',
          step_index: 1,
          parent_seq: 3,
          payload: { tool_name: 'search_web' },
          created_at: '2026-03-22T10:00:05Z',
          artifact: {
              id: 'art_tool_args',
              kind: 'tool_arguments',
              mime_type: 'application/json',
              encoding: 'utf-8',
              size_bytes: 96,
              redaction_state: 'raw',
              created_at: '2026-03-22T10:00:05Z',
            },
          },
        {
          seq: 5,
          phase: 'run',
          event_type: 'approval.requested',
          level: 'info',
          step_index: 1,
          parent_seq: 4,
          payload: { tool_name: 'search_web', reason: 'requires approval' },
          created_at: '2026-03-22T10:00:05Z',
        },
        {
          seq: 6,
          phase: 'run',
          event_type: 'approval.resolved',
          level: 'info',
          step_index: 1,
          parent_seq: 5,
          payload: { tool_name: 'search_web', decision: 'approve' },
          created_at: '2026-03-22T10:00:06Z',
        },
        {
          seq: 7,
          phase: 'run',
          event_type: 'step.finished',
          level: 'info',
          step_index: 1,
          parent_seq: 6,
          payload: { step: 'tool-call' },
          created_at: '2026-03-22T10:00:07Z',
        },
        {
          seq: 8,
          phase: 'run',
          event_type: 'run.failed',
          level: 'error',
          step_index: 1,
          parent_seq: 7,
          payload: { error: 'timeout' },
          created_at: '2026-03-22T10:00:09Z',
        },
        {
          seq: 9,
          phase: 'run',
          event_type: 'messages.persisted',
          level: 'info',
          step_index: 2,
          parent_seq: 8,
          payload: { count: 2 },
          created_at: '2026-03-22T10:00:11Z',
        },
        {
          seq: 10,
          phase: 'run',
          event_type: 'run.succeeded',
          level: 'info',
          step_index: 2,
          parent_seq: 9,
          payload: { status: 'done' },
          created_at: '2026-03-22T10:00:13Z',
        },
        ],
        artifacts: [
          {
            id: 'art_request',
            kind: 'request_messages',
            mime_type: 'application/json',
            encoding: 'utf-8',
            size_bytes: 128,
            redaction_state: 'raw',
            created_at: '2026-03-22T10:00:01Z',
            body: {
              messages: [
                { role: 'user', content: 'hello' },
                { role: 'assistant', content: 'hi' },
              ],
            },
          },
          {
            id: 'art_tool_args',
            kind: 'tool_arguments',
            mime_type: 'application/json',
            encoding: 'utf-8',
            size_bytes: 96,
            redaction_state: 'raw',
            created_at: '2026-03-22T10:00:05Z',
            body: {
              query: 'latest audit logs',
            },
          },
        ],
      }
    })

    const wrapper = mount(AdminAuditView, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to" v-bind="$attrs"><slot /></a>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper.find('[data-conversation-id="conv_2"]').trigger('click')
    await flushPromises()

    expect(api.fetchConversations).toHaveBeenCalledTimes(1)
    expect(api.fetchConversation).toHaveBeenCalledWith('conv_2')
    expect(api.fetchAuditConversationRuns).toHaveBeenCalledWith('conv_2')
    expect(api.fetchAuditRunReplay).toHaveBeenCalledWith('run_2')
    expect(wrapper.find('.admin-audit-back-link').attributes('title')).toBe('返回聊天')
    expect(wrapper.find('.admin-audit-back-link svg').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Replay Timeline')
    expect(wrapper.text()).not.toContain('Admin Audit')
    expect(wrapper.text()).not.toContain('Timeline')
    expect(wrapper.text()).not.toContain('Detail')
    expect(wrapper.text()).toContain('Second chat with a much longer title')
    expect(wrapper.text()).toContain('bob')
    expect(wrapper.text()).toContain('操作时间线')
    expect(wrapper.find('[data-testid="summary-card"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="summary-card"]').classes()).toContain('admin-audit-summary-compact')
    expect(wrapper.findAll('.admin-audit-summary-card')).toHaveLength(1)
    expect(wrapper.findAll('.admin-audit-panel-header')).toHaveLength(3)
    expect(wrapper.find('.admin-audit-timeline-panel .admin-audit-panel-controls').exists()).toBe(true)
    expect(wrapper.find('.admin-audit-timeline-copy').exists()).toBe(false)
    expect(wrapper.find('[data-testid="summary-toggle"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="summary-toggle"]').text()).toContain('展开')
    expect(wrapper.text()).toContain('创建者')
    expect(wrapper.text()).toContain('轮次数')
    expect(wrapper.text()).toContain('状态')
    expect(wrapper.text()).not.toContain('对话信息')
    expect(wrapper.text()).not.toContain('执行信息')
    expect(wrapper.text()).not.toContain('Task ID')
    expect(wrapper.text()).not.toContain('对话 ID')
    expect(wrapper.text()).toContain('run.created')
    expect(wrapper.text()).toContain('运行已创建')
    expect(wrapper.text()).toContain('run.started')
    expect(wrapper.text()).toContain('运行开始')
    expect(wrapper.text()).toContain('会话已加载')
    expect(wrapper.text()).toContain('步骤开始')
    expect(wrapper.text()).toContain('审批请求')
    expect(wrapper.text()).toContain('审批已处理')
    expect(wrapper.text()).toContain('步骤完成')
    expect(wrapper.text()).toContain('运行成功')
    expect(wrapper.text()).toContain('消息已持久化')
    expect(wrapper.find('.admin-audit-timeline').exists()).toBe(true)
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(11)
    expect(wrapper.text()).not.toContain('暂无回放时间线')
    expect(wrapper.text()).not.toContain('暂无事件')
    expect(wrapper.find('.conversation-compact-dot').exists()).toBe(false)
    expect(wrapper.find('.admin-audit-conversation.active .conversation-title').attributes('title')).toContain('Second chat with a much longer title')
    expect(wrapper.text()).toContain('2026-03-22')
    expect(wrapper.find('.admin-audit-conversation.active .admin-audit-conversation-row').text()).not.toContain('2026-03-22')
    expect(wrapper.find('.admin-audit-conversation.active .admin-audit-conversation-meta').text()).toContain('bob')
    expect(wrapper.find('.admin-audit-conversation.active .admin-audit-conversation-meta').text()).toContain('2026-03-22')

    await wrapper.findAll('.admin-audit-timeline-item')[1].trigger('click')
    await flushPromises()
    expect(wrapper.find('.admin-audit-artifact-panel h2').text()).toBe('运行开始')
    expect(wrapper.text()).toContain('hello')
    expect(wrapper.text()).toContain('assistant')

    await wrapper.find('[data-filter="tool"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(1)
    expect(wrapper.text()).toContain('tool.called')
    expect(wrapper.text()).toContain('工具调用')
    expect(wrapper.find('.admin-audit-artifact-panel h2').text()).toBe('工具调用')

    await wrapper.find('.admin-audit-timeline-item').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('latest audit logs')

    await wrapper.find('[data-filter="error"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(1)
    expect(wrapper.text()).toContain('run.failed')
    expect(wrapper.text()).toContain('运行失败')

    await wrapper.find('[data-filter="request"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.admin-audit-filter-bar').exists()).toBe(true)
    expect(wrapper.find('.admin-audit-timeline-panel').text()).toContain('run.started')
    expect(wrapper.find('.admin-audit-timeline-panel').text()).toContain('运行开始')

    await wrapper.find('[data-filter="tool"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.admin-audit-filter-bar').exists()).toBe(true)
    expect(wrapper.find('.admin-audit-timeline-panel').text()).toContain('tool.called')
    expect(wrapper.find('.admin-audit-timeline-panel').text()).toContain('工具调用')

    await wrapper.find('[data-conversation-id="conv_3"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-filter="tool"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.admin-audit-filter-bar').exists()).toBe(true)
    expect(wrapper.find('.admin-audit-timeline-panel').text()).toContain('当前筛选条件下没有可展示的时间线')
  })

  it('renders display_name as the primary timeline title and keeps raw event_type in metadata', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_labels',
        title: 'Replay labels chat',
        last_message: 'labels',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'alice',
        created_at: '2026-03-22T12:00:00Z',
        updated_at: '2026-03-22T12:01:00Z',
        audit_run_id: 'run_labels',
      },
    ])
    api.fetchConversation.mockResolvedValue({
      id: 'conv_labels',
      title: 'Replay labels chat',
      last_message: 'labels',
      message_count: 1,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'alice',
      created_at: '2026-03-22T12:00:00Z',
      updated_at: '2026-03-22T12:01:00Z',
      audit_run_id: 'run_labels',
    })
    api.fetchAuditConversationRuns.mockResolvedValue([
      {
        id: 'run_labels',
        task_id: 'task_labels',
        conversation_id: 'conv_labels',
        task_type: 'agent.run',
        status: 'succeeded',
        created_by: 'alice',
        schema_version: 'v1',
        created_at: '2026-03-22T12:00:00Z',
        updated_at: '2026-03-22T12:01:00Z',
      },
    ])
    api.fetchAuditRunReplay.mockResolvedValue({
      run: {
        id: 'run_labels',
        task_id: 'task_labels',
        conversation_id: 'conv_labels',
        task_type: 'agent.run',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        runner_id: 'runner_1',
        status: 'succeeded',
        created_by: 'alice',
        replayable: true,
        schema_version: 'v1',
        created_at: '2026-03-22T12:00:00Z',
        updated_at: '2026-03-22T12:01:00Z',
      },
      timeline: [
        {
          seq: 1,
          phase: 'run',
          event_type: 'approval.resolved',
          level: 'info',
          step_index: 0,
          parent_seq: 0,
          display_name: '审批已通过（显示名）',
          payload: { tool_name: 'search_web', decision: 'approve' },
          created_at: '2026-03-22T12:00:00Z',
          artifact: {
            id: 'art_approval',
            kind: 'tool_output',
            mime_type: 'application/json',
            encoding: 'utf-8',
            size_bytes: 64,
            redaction_state: 'raw',
            created_at: '2026-03-22T12:00:00Z',
          },
        },
        {
          seq: 2,
          phase: 'run',
          event_type: 'run.failed',
          level: 'error',
          step_index: 1,
          parent_seq: 1,
          payload: { error: 'timeout' },
          created_at: '2026-03-22T12:00:01Z',
        },
      ],
      artifacts: [
        {
          id: 'art_approval',
          kind: 'tool_output',
          mime_type: 'application/json',
          encoding: 'utf-8',
          size_bytes: 64,
          redaction_state: 'raw',
          created_at: '2026-03-22T12:00:00Z',
          body: {
            decision: 'approve',
          },
        },
      ],
    })

    const wrapper = mount(AdminAuditView, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to" v-bind="$attrs"><slot /></a>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper.find('[data-conversation-id="conv_labels"]').trigger('click')
    await flushPromises()

    const timelineItems = wrapper.findAll('.admin-audit-timeline-item')
    expect(timelineItems).toHaveLength(2)

    const displayNameItem = timelineItems[0]
    const fallbackItem = timelineItems[1]

    expect(displayNameItem.find('strong').text()).toBe('审批已通过（显示名）')
    expect(displayNameItem.find('p').text()).toContain('approval.resolved')
    expect(displayNameItem.find('p').text()).not.toContain('审批已通过（显示名）')

    expect(fallbackItem.find('strong').text()).toBe('运行失败')
    expect(fallbackItem.find('p').text()).toContain('run.failed')
    expect(fallbackItem.find('p').text()).not.toContain('运行失败')

    await displayNameItem.trigger('click')
    await flushPromises()
    expect(wrapper.find('.admin-audit-artifact-panel h2').text()).toBe('审批已通过（显示名）')
    expect(wrapper.find('.admin-audit-detail-meta').text()).toContain('approval.resolved')
  })

  it('keeps the selected event title when its artifact is shown', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_heading',
        title: 'Heading precedence chat',
        last_message: 'heading',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'alice',
        created_at: '2026-03-22T13:00:00Z',
        updated_at: '2026-03-22T13:01:00Z',
        audit_run_id: 'run_heading',
      },
    ])
    api.fetchConversation.mockResolvedValue({
      id: 'conv_heading',
      title: 'Heading precedence chat',
      last_message: 'heading',
      message_count: 1,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'alice',
      created_at: '2026-03-22T13:00:00Z',
      updated_at: '2026-03-22T13:01:00Z',
      audit_run_id: 'run_heading',
    })
    api.fetchAuditConversationRuns.mockResolvedValue([
      {
        id: 'run_heading',
        task_id: 'task_heading',
        conversation_id: 'conv_heading',
        task_type: 'agent.run',
        status: 'succeeded',
        created_by: 'alice',
        schema_version: 'v1',
        created_at: '2026-03-22T13:00:00Z',
        updated_at: '2026-03-22T13:01:00Z',
      },
    ])
    api.fetchAuditRunReplay.mockResolvedValue({
      run: {
        id: 'run_heading',
        task_id: 'task_heading',
        conversation_id: 'conv_heading',
        task_type: 'agent.run',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        runner_id: 'runner_1',
        status: 'succeeded',
        created_by: 'alice',
        replayable: true,
        schema_version: 'v1',
        created_at: '2026-03-22T13:00:00Z',
        updated_at: '2026-03-22T13:01:00Z',
      },
      timeline: [
        {
          seq: 1,
          phase: 'tool',
          event_type: 'tool.called',
          level: 'info',
          step_index: 0,
          parent_seq: 0,
          display_name: '调用搜索工具',
          payload: { tool_name: 'search_web' },
          created_at: '2026-03-22T13:00:00Z',
          artifact: {
            id: 'art_tool_output',
            kind: 'tool_output',
            mime_type: 'application/json',
            encoding: 'utf-8',
            size_bytes: 64,
            redaction_state: 'raw',
            created_at: '2026-03-22T13:00:00Z',
          },
        },
      ],
      artifacts: [
        {
          id: 'art_tool_output',
          kind: 'tool_output',
          mime_type: 'application/json',
          encoding: 'utf-8',
          size_bytes: 64,
          redaction_state: 'raw',
          created_at: '2026-03-22T13:00:00Z',
          body: {
            result: 'ok',
          },
        },
      ],
    })

    const wrapper = mount(AdminAuditView, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to" v-bind="$attrs"><slot /></a>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper.find('[data-conversation-id="conv_heading"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('.admin-audit-artifact-panel h2').text()).toBe('调用搜索工具')
    expect(wrapper.text()).toContain('ok')
  })


  it('shows runtime prompt envelope artifacts as 系统提示', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_runtime_prompt_artifact',
        title: 'Runtime prompt artifact chat',
        last_message: 'artifact',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'alice',
        created_at: '2026-03-22T15:00:00Z',
        updated_at: '2026-03-22T15:01:00Z',
        audit_run_id: 'run_runtime_prompt_artifact',
      },
    ])
    api.fetchConversation.mockResolvedValue({
      id: 'conv_runtime_prompt_artifact',
      title: 'Runtime prompt artifact chat',
      last_message: 'artifact',
      message_count: 1,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'alice',
      created_at: '2026-03-22T15:00:00Z',
      updated_at: '2026-03-22T15:01:00Z',
      audit_run_id: 'run_runtime_prompt_artifact',
    })
    api.fetchAuditConversationRuns.mockResolvedValue([
      {
        id: 'run_runtime_prompt_artifact',
        task_id: 'task_runtime_prompt_artifact',
        conversation_id: 'conv_runtime_prompt_artifact',
        task_type: 'agent.run',
        status: 'succeeded',
        created_by: 'alice',
        schema_version: 'v1',
        created_at: '2026-03-22T15:00:00Z',
        updated_at: '2026-03-22T15:01:00Z',
      },
    ])
    api.fetchAuditRunReplay.mockResolvedValue({
      run: {
        id: 'run_runtime_prompt_artifact',
        task_id: 'task_runtime_prompt_artifact',
        conversation_id: 'conv_runtime_prompt_artifact',
        task_type: 'agent.run',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        runner_id: 'runner_1',
        status: 'succeeded',
        created_by: 'alice',
        replayable: true,
        schema_version: 'v1',
        created_at: '2026-03-22T15:00:00Z',
        updated_at: '2026-03-22T15:01:00Z',
      },
      timeline: [
        {
          seq: 1,
          phase: 'prompt',
          event_type: 'prompt.resolved',
          level: 'info',
          step_index: 0,
          parent_seq: 0,
          display_name: '提示词解析',
          payload: { segment_count: 4 },
          created_at: '2026-03-22T15:00:00Z',
          artifact: {
            id: 'art_runtime_prompt_envelope',
            kind: 'runtime_prompt_envelope',
            mime_type: 'application/json',
            encoding: 'utf-8',
            size_bytes: 128,
            redaction_state: 'raw',
            created_at: '2026-03-22T15:00:00Z',
          },
        },
      ],
      artifacts: [
        {
          id: 'art_runtime_prompt_envelope',
          kind: 'runtime_prompt_envelope',
          mime_type: 'application/json',
          encoding: 'utf-8',
          size_bytes: 128,
          redaction_state: 'raw',
          created_at: '2026-03-22T15:00:00Z',
          body: {
            source_counts: { forced_block: 3, resolved_prompt: 1 },
          },
        },
      ],
    })

    const wrapper = mount(AdminAuditView, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to" v-bind="$attrs"><slot /></a>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper.find('[data-conversation-id="conv_runtime_prompt_artifact"]').trigger('click')
    await flushPromises()

  })

  it('preserves active filters when selecting a turn', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_filter_turns',
        title: 'Filter and turn chat',
        last_message: 'filters',
        message_count: 4,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'alice',
        created_at: '2026-03-22T14:00:00Z',
        updated_at: '2026-03-22T14:05:00Z',
        audit_run_ids: ['run_filter_a', 'run_filter_b'],
      },
    ])
    api.fetchConversation.mockResolvedValue({
      id: 'conv_filter_turns',
      title: 'Filter and turn chat',
      last_message: 'filters',
      message_count: 4,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'alice',
      created_at: '2026-03-22T14:00:00Z',
      updated_at: '2026-03-22T14:05:00Z',
      audit_run_ids: ['run_filter_a', 'run_filter_b'],
    })
    api.fetchAuditConversationRuns.mockResolvedValue([
      {
        id: 'run_filter_a',
        task_id: 'task_filter_a',
        conversation_id: 'conv_filter_turns',
        task_type: 'agent.run',
        status: 'succeeded',
        created_by: 'alice',
        schema_version: 'v1',
        created_at: '2026-03-22T14:00:00Z',
        updated_at: '2026-03-22T14:01:00Z',
      },
      {
        id: 'run_filter_b',
        task_id: 'task_filter_b',
        conversation_id: 'conv_filter_turns',
        task_type: 'agent.run',
        status: 'succeeded',
        created_by: 'alice',
        schema_version: 'v1',
        created_at: '2026-03-22T14:03:00Z',
        updated_at: '2026-03-22T14:04:00Z',
      },
    ])
    api.fetchAuditRunReplay.mockImplementation(async (runId: string) => {
      if (runId === 'run_filter_a') {
        return {
          run: {
            id: 'run_filter_a',
            task_id: 'task_filter_a',
            conversation_id: 'conv_filter_turns',
            task_type: 'agent.run',
            status: 'succeeded',
            created_by: 'alice',
            schema_version: 'v1',
            created_at: '2026-03-22T14:00:00Z',
            updated_at: '2026-03-22T14:01:00Z',
          },
          timeline: [
            {
              seq: 1,
              phase: 'request',
              event_type: 'run.started',
              level: 'info',
              step_index: 0,
              parent_seq: 0,
              display_name: '第 1 轮请求',
              payload: { status: 'running' },
              created_at: '2026-03-22T14:00:00Z',
            },
            {
              seq: 2,
              phase: 'tool',
              event_type: 'tool.called',
              level: 'info',
              step_index: 1,
              parent_seq: 1,
              display_name: '第 1 轮工具',
              payload: { tool_name: 'read_file' },
              created_at: '2026-03-22T14:00:01Z',
            },
          ],
          artifacts: [],
        }
      }

      return {
        run: {
          id: 'run_filter_b',
          task_id: 'task_filter_b',
          conversation_id: 'conv_filter_turns',
          task_type: 'agent.run',
          status: 'succeeded',
          created_by: 'alice',
          schema_version: 'v1',
          created_at: '2026-03-22T14:03:00Z',
          updated_at: '2026-03-22T14:04:00Z',
        },
        timeline: [
          {
            seq: 1,
            phase: 'request',
            event_type: 'request.built',
            level: 'info',
            step_index: 0,
            parent_seq: 0,
            display_name: '第 2 轮请求',
            payload: { status: 'built' },
            created_at: '2026-03-22T14:03:00Z',
          },
          {
            seq: 2,
            phase: 'tool',
            event_type: 'tool.finished',
            level: 'info',
            step_index: 1,
            parent_seq: 1,
            display_name: '第 2 轮工具',
            payload: { tool_name: 'read_file' },
            created_at: '2026-03-22T14:03:01Z',
          },
        ],
        artifacts: [],
      }
    })

    const wrapper = mount(AdminAuditView, {
      attachTo: document.body,
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to" v-bind="$attrs"><slot /></a>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper.find('[data-conversation-id="conv_filter_turns"]').trigger('click')
    await flushPromises()

    await wrapper.find('[data-filter="tool"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(2)
    expect(wrapper.find('.admin-audit-timeline').text()).toContain('第 1 轮工具')
    expect(wrapper.find('.admin-audit-timeline').text()).toContain('第 2 轮工具')
    expect(wrapper.find('.admin-audit-timeline').text()).not.toContain('第 1 轮请求')
    expect(wrapper.find('.admin-audit-timeline').text()).not.toContain('第 2 轮请求')

    expect(wrapper.find('[data-testid="turn-menu"]').classes()).toContain('admin-audit-turn-menu')
    await wrapper.find('[data-testid="turn-menu-trigger"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="turn-option-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(1)
    expect(wrapper.find('.admin-audit-timeline').text()).toContain('第 2 轮工具')
    expect(wrapper.find('.admin-audit-timeline').text()).not.toContain('第 2 轮请求')

    wrapper.unmount()
  })
  it('keeps the newly selected conversation summary visible when runs loading fails', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_a',
        title: 'Sidebar conversation A',
        last_message: 'alpha',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'alice',
        created_at: '2026-03-22T09:00:00Z',
        updated_at: '2026-03-22T09:01:00Z',
        audit_run_id: 'run_a',
      },
      {
        id: 'conv_b',
        title: 'Sidebar conversation B',
        last_message: 'beta',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'bob',
        created_at: '2026-03-22T10:00:00Z',
        updated_at: '2026-03-22T10:01:00Z',
        audit_run_id: 'run_b',
      },
    ])
    api.fetchConversation.mockImplementation(async (conversationId: string) => ({
      id: conversationId,
      title: conversationId === 'conv_a' ? 'Loaded conversation A' : 'Loaded conversation B',
      last_message: conversationId === 'conv_a' ? 'alpha' : 'beta',
      message_count: conversationId === 'conv_a' ? 2 : 1,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: conversationId === 'conv_a' ? 'alice' : 'bob',
      created_at: conversationId === 'conv_a' ? '2026-03-22T09:00:00Z' : '2026-03-22T10:00:00Z',
      updated_at: conversationId === 'conv_a' ? '2026-03-22T09:01:00Z' : '2026-03-22T10:01:00Z',
      audit_run_id: conversationId === 'conv_a' ? 'run_a' : 'run_b',
    }))
    api.fetchAuditConversationRuns.mockImplementation(async (conversationId: string) => {
      if (conversationId === 'conv_b') {
        throw new Error('runs failed for B')
      }
      return [
        {
          id: 'run_a',
          task_id: 'task_a',
          conversation_id: 'conv_a',
          task_type: 'agent.run',
          status: 'succeeded',
          created_by: 'alice',
          schema_version: 'v1',
          created_at: '2026-03-22T09:00:00Z',
          updated_at: '2026-03-22T09:01:00Z',
        },
      ]
    })
    api.fetchAuditRunReplay.mockResolvedValue({
      run: {
        id: 'run_a',
        task_id: 'task_a',
        conversation_id: 'conv_a',
        task_type: 'agent.run',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        runner_id: 'runner_1',
        status: 'succeeded',
        created_by: 'alice',
        replayable: true,
        schema_version: 'v1',
        created_at: '2026-03-22T09:00:00Z',
        updated_at: '2026-03-22T09:01:00Z',
      },
      timeline: [],
      artifacts: [],
    })

    const wrapper = mount(AdminAuditView, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to" v-bind="$attrs"><slot /></a>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper.find('[data-conversation-id="conv_a"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('.topbar-conversation-title').text()).toBe('Loaded conversation A')
    expect(wrapper.text()).toContain('alice')

    await wrapper.find('[data-conversation-id="conv_b"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('.topbar-conversation-title').text()).toBe('Sidebar conversation B')
    expect(wrapper.text()).toContain('runs failed for B')
    expect(wrapper.text()).toContain('bob')
    expect(wrapper.text()).not.toContain('Loaded conversation A')
  })

  it('ignores stale out-of-order selection results and keeps the latest conversation details', async () => {
    const conversationDeferredA = createDeferred<{
      id: string
      title: string
      last_message: string
      message_count: number
      provider_id: string
      model_id: string
      created_by: string
      created_at: string
      updated_at: string
      audit_run_id: string
    }>()
    const conversationDeferredB = createDeferred<{
      id: string
      title: string
      last_message: string
      message_count: number
      provider_id: string
      model_id: string
      created_by: string
      created_at: string
      updated_at: string
      audit_run_id: string
    }>()
    const runsDeferredA = createDeferred<Array<{
      id: string
      task_id: string
      conversation_id: string
      task_type: string
      status: 'succeeded'
      created_by: string
      schema_version: string
      created_at: string
      updated_at: string
    }>>()
    const runsDeferredB = createDeferred<Array<{
      id: string
      task_id: string
      conversation_id: string
      task_type: string
      status: 'succeeded'
      created_by: string
      schema_version: string
      created_at: string
      updated_at: string
    }>>()
    const replayDeferredA = createDeferred<{
      run: {
        id: string
        task_id: string
        conversation_id: string
        task_type: string
        provider_id: string
        model_id: string
        runner_id: string
        status: 'succeeded'
        created_by: string
        replayable: true
        schema_version: string
        created_at: string
        updated_at: string
      }
      timeline: Array<{
        seq: number
        phase: string
        event_type: string
        level: string
        step_index: number
        parent_seq: number
        display_name: string
        payload: { owner: string }
        created_at: string
      }>
      artifacts: []
    }>()
    const replayDeferredB = createDeferred<{
      run: {
        id: string
        task_id: string
        conversation_id: string
        task_type: string
        provider_id: string
        model_id: string
        runner_id: string
        status: 'succeeded'
        created_by: string
        replayable: true
        schema_version: string
        created_at: string
        updated_at: string
      }
      timeline: Array<{
        seq: number
        phase: string
        event_type: string
        level: string
        step_index: number
        parent_seq: number
        display_name: string
        payload: { owner: string }
        created_at: string
      }>
      artifacts: []
    }>()

    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_a',
        title: 'Sidebar conversation A',
        last_message: 'alpha',
        message_count: 2,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'alice',
        created_at: '2026-03-22T09:00:00Z',
        updated_at: '2026-03-22T09:01:00Z',
        audit_run_id: 'run_a',
      },
      {
        id: 'conv_b',
        title: 'Sidebar conversation B',
        last_message: 'beta',
        message_count: 1,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'bob',
        created_at: '2026-03-22T10:00:00Z',
        updated_at: '2026-03-22T10:01:00Z',
        audit_run_id: 'run_b',
      },
    ])
    api.fetchConversation.mockImplementation((conversationId: string) => {
      if (conversationId === 'conv_a') {
        return conversationDeferredA.promise
      }
      return conversationDeferredB.promise
    })
    api.fetchAuditConversationRuns.mockImplementation((conversationId: string) => {
      if (conversationId === 'conv_a') {
        return runsDeferredA.promise
      }
      return runsDeferredB.promise
    })
    api.fetchAuditRunReplay.mockImplementation((runId: string) => {
      if (runId === 'run_a') {
        return replayDeferredA.promise
      }
      return replayDeferredB.promise
    })

    const wrapper = mount(AdminAuditView, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to" v-bind="$attrs"><slot /></a>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper.find('[data-conversation-id="conv_a"]').trigger('click')
    await wrapper.find('[data-conversation-id="conv_b"]').trigger('click')

    conversationDeferredB.resolve({
      id: 'conv_b',
      title: 'Loaded conversation B',
      last_message: 'beta',
      message_count: 1,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'bob',
      created_at: '2026-03-22T10:00:00Z',
      updated_at: '2026-03-22T10:01:00Z',
      audit_run_id: 'run_b',
    })
    runsDeferredB.resolve([
      {
        id: 'run_b',
        task_id: 'task_b',
        conversation_id: 'conv_b',
        task_type: 'agent.run',
        status: 'succeeded',
        created_by: 'bob',
        schema_version: 'v1',
        created_at: '2026-03-22T10:00:00Z',
        updated_at: '2026-03-22T10:01:00Z',
      },
    ])
    await flushPromises()

    replayDeferredB.resolve({
      run: {
        id: 'run_b',
        task_id: 'task_b',
        conversation_id: 'conv_b',
        task_type: 'agent.run',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        runner_id: 'runner_1',
        status: 'succeeded',
        created_by: 'bob',
        replayable: true,
        schema_version: 'v1',
        created_at: '2026-03-22T10:00:00Z',
        updated_at: '2026-03-22T10:01:00Z',
      },
      timeline: [
        {
          seq: 1,
          phase: 'run',
          event_type: 'run.started',
          level: 'info',
          step_index: 0,
          parent_seq: 0,
          display_name: 'B timeline entry',
          payload: { owner: 'B' },
          created_at: '2026-03-22T10:00:00Z',
        },
      ],
      artifacts: [],
    })
    await flushPromises()

    expect(wrapper.find('.topbar-conversation-title').text()).toBe('Loaded conversation B')
    expect(wrapper.find('.status-pill').text()).toBe('succeeded')
    expect(wrapper.find('.admin-audit-timeline').text()).toContain('B timeline entry')
    expect(wrapper.text()).toContain('bob')

    conversationDeferredA.resolve({
      id: 'conv_a',
      title: 'Loaded conversation A',
      last_message: 'alpha',
      message_count: 2,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'alice',
      created_at: '2026-03-22T09:00:00Z',
      updated_at: '2026-03-22T09:01:00Z',
      audit_run_id: 'run_a',
    })
    runsDeferredA.resolve([
      {
        id: 'run_a',
        task_id: 'task_a',
        conversation_id: 'conv_a',
        task_type: 'agent.run',
        status: 'succeeded',
        created_by: 'alice',
        schema_version: 'v1',
        created_at: '2026-03-22T09:00:00Z',
        updated_at: '2026-03-22T09:01:00Z',
      },
    ])
    await flushPromises()

    replayDeferredA.resolve({
      run: {
        id: 'run_a',
        task_id: 'task_a',
        conversation_id: 'conv_a',
        task_type: 'agent.run',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        runner_id: 'runner_1',
        status: 'succeeded',
        created_by: 'alice',
        replayable: true,
        schema_version: 'v1',
        created_at: '2026-03-22T09:00:00Z',
        updated_at: '2026-03-22T09:01:00Z',
      },
      timeline: [
        {
          seq: 1,
          phase: 'run',
          event_type: 'run.started',
          level: 'info',
          step_index: 0,
          parent_seq: 0,
          display_name: 'A timeline entry',
          payload: { owner: 'A' },
          created_at: '2026-03-22T09:00:00Z',
        },
      ],
      artifacts: [],
    })
    await flushPromises()

    expect(wrapper.find('.topbar-conversation-title').text()).toBe('Loaded conversation B')
    expect(wrapper.find('.admin-audit-timeline').text()).toContain('B timeline entry')
    expect(wrapper.find('.admin-audit-timeline').text()).not.toContain('A timeline entry')
    expect(wrapper.text()).toContain('bob')
    expect(wrapper.text()).not.toContain('Loaded conversation A')
  })

})
