import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  fetchAuditRun: vi.fn(),
  fetchAuditRunEvents: vi.fn(),
  fetchAuditRunReplay: vi.fn(),
  fetchConversation: vi.fn(),
  fetchConversations: vi.fn(),
}))

vi.mock('../lib/api', () => api)

import AdminAuditView from './AdminAuditView.vue'

describe('AdminAuditView', () => {
  beforeEach(() => {
    api.fetchConversations.mockReset()
    api.fetchConversation.mockReset()
    api.fetchAuditRun.mockReset()
    api.fetchAuditRunEvents.mockReset()
    api.fetchAuditRunReplay.mockReset()
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
    api.fetchAuditRun.mockImplementation(async (runId: string) => ({
      id: runId,
      task_id: runId === 'run_2' ? 'tsk_2' : runId === 'run_3' ? 'tsk_3' : 'tsk_1',
      conversation_id: runId === 'run_2' ? 'conv_2' : runId === 'run_3' ? 'conv_3' : 'conv_1',
      task_type: 'agent.run',
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      runner_id: 'runner_1',
      status: 'succeeded',
      created_by: runId === 'run_2' ? 'bob' : runId === 'run_3' ? 'cindy' : 'alice',
      replayable: true,
      schema_version: 'v1',
      created_at: runId === 'run_3' ? '2026-03-22T11:00:00Z' : '2026-03-22T10:00:00Z',
      updated_at: runId === 'run_3' ? '2026-03-22T11:02:00Z' : '2026-03-22T10:02:00Z',
    }))
    api.fetchAuditRunEvents.mockResolvedValue([])
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
          event_type: 'step.finished',
          level: 'info',
          step_index: 1,
          parent_seq: 4,
          payload: { step: 'tool-call' },
          created_at: '2026-03-22T10:00:06Z',
        },
        {
          seq: 6,
          phase: 'run',
          event_type: 'run.failed',
          level: 'error',
          step_index: 1,
          parent_seq: 5,
          payload: { error: 'timeout' },
          created_at: '2026-03-22T10:00:08Z',
        },
        {
          seq: 7,
          phase: 'run',
          event_type: 'messages.persisted',
          level: 'info',
          step_index: 2,
          parent_seq: 6,
          payload: { count: 2 },
          created_at: '2026-03-22T10:00:10Z',
        },
        {
          seq: 8,
          phase: 'run',
          event_type: 'run.succeeded',
          level: 'info',
          step_index: 2,
          parent_seq: 7,
          payload: { status: 'done' },
          created_at: '2026-03-22T10:00:12Z',
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
    expect(api.fetchAuditRun).toHaveBeenCalledWith('run_2')
    expect(api.fetchAuditRunEvents).toHaveBeenCalledWith('run_2')
    expect(api.fetchAuditRunReplay).toHaveBeenCalledWith('run_2')
    expect(wrapper.find('.admin-audit-back-link').attributes('title')).toBe('返回聊天')
    expect(wrapper.find('.admin-audit-back-link svg').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Replay Timeline')
    expect(wrapper.text()).not.toContain('Admin Audit')
    expect(wrapper.text()).not.toContain('Timeline')
    expect(wrapper.text()).not.toContain('Detail')
    expect(wrapper.text()).toContain('Second chat with a much longer title')
    expect(wrapper.text()).toContain('bob')
    expect(wrapper.text()).toContain('run_2')
    expect(wrapper.text()).toContain('操作时间线')
    expect(wrapper.text()).toContain('对话信息')
    expect(wrapper.text()).toContain('执行信息')
    expect(wrapper.text()).toContain('run.created')
    expect(wrapper.text()).toContain('运行已创建')
    expect(wrapper.text()).toContain('run.started')
    expect(wrapper.text()).toContain('运行开始')
    expect(wrapper.text()).toContain('会话已加载')
    expect(wrapper.text()).toContain('步骤开始')
    expect(wrapper.text()).toContain('步骤完成')
    expect(wrapper.text()).toContain('运行成功')
    expect(wrapper.text()).toContain('消息已持久化')
    expect(wrapper.find('.admin-audit-timeline').exists()).toBe(true)
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(9)
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
    expect(wrapper.text()).toContain('对话历史')
    expect(wrapper.text()).toContain('hello')
    expect(wrapper.text()).toContain('assistant')

    await wrapper.find('[data-filter="tool"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(1)
    expect(wrapper.text()).toContain('tool.called')
    expect(wrapper.text()).toContain('工具调用')
    expect(wrapper.text()).toContain('工具调用参数')

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
})
