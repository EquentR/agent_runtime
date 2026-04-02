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

describe('AdminAuditView', () => {
  beforeEach(() => {
    api.fetchConversations.mockReset()
    api.fetchConversation.mockReset()
    api.fetchAuditConversationRuns.mockReset()
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

  it('shows turn selector and merged timeline for multi-turn conversations', async () => {
    api.fetchConversations.mockResolvedValue([
      {
        id: 'conv_multi',
        title: 'Multi-turn chat',
        last_message: 'hello',
        message_count: 4,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'alice',
        created_at: '2026-03-22T09:00:00Z',
        updated_at: '2026-03-22T09:10:00Z',
        audit_run_ids: ['run_a', 'run_b'],
      },
    ])
    api.fetchConversation.mockResolvedValue({
      id: 'conv_multi',
      title: 'Multi-turn chat',
      last_message: 'hello',
      message_count: 4,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'alice',
      created_at: '2026-03-22T09:00:00Z',
      updated_at: '2026-03-22T09:10:00Z',
      audit_run_ids: ['run_a', 'run_b'],
    })
    api.fetchAuditConversationRuns.mockResolvedValue([
      {
        id: 'run_a',
        task_id: 'task_a',
        conversation_id: 'conv_multi',
        task_type: 'agent.run',
        status: 'succeeded',
        created_by: 'alice',
        schema_version: 'v1',
        created_at: '2026-03-22T09:00:00Z',
        updated_at: '2026-03-22T09:01:00Z',
      },
      {
        id: 'run_b',
        task_id: 'task_b',
        conversation_id: 'conv_multi',
        task_type: 'agent.run',
        status: 'succeeded',
        created_by: 'alice',
        schema_version: 'v1',
        created_at: '2026-03-22T09:05:00Z',
        updated_at: '2026-03-22T09:06:00Z',
      },
    ])
    api.fetchAuditRunReplay.mockImplementation(async (runId: string) => {
      if (runId === 'run_a') {
        return {
          run: {
            id: 'run_a',
            task_id: 'task_a',
            conversation_id: 'conv_multi',
            task_type: 'agent.run',
            status: 'succeeded',
            created_by: 'alice',
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
              payload: { status: 'running' },
              created_at: '2026-03-22T09:00:00Z',
            },
            {
              seq: 2,
              phase: 'tool',
              event_type: 'tool.called',
              level: 'info',
              step_index: 1,
              parent_seq: 1,
              payload: { tool_name: 'read_file' },
              created_at: '2026-03-22T09:00:01Z',
            },
            {
              seq: 3,
              phase: 'run',
              event_type: 'run.succeeded',
              level: 'info',
              step_index: 1,
              parent_seq: 2,
              payload: { status: 'done' },
              created_at: '2026-03-22T09:00:02Z',
            },
          ],
          artifacts: [],
        }
      }
      return {
        run: {
          id: 'run_b',
          task_id: 'task_b',
          conversation_id: 'conv_multi',
          task_type: 'agent.run',
          status: 'succeeded',
          created_by: 'alice',
          schema_version: 'v1',
          created_at: '2026-03-22T09:05:00Z',
          updated_at: '2026-03-22T09:06:00Z',
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
            created_at: '2026-03-22T09:05:00Z',
          },
          {
            seq: 2,
            phase: 'run',
            event_type: 'run.succeeded',
            level: 'info',
            step_index: 1,
            parent_seq: 1,
            payload: { status: 'done' },
            created_at: '2026-03-22T09:05:01Z',
          },
        ],
        artifacts: [],
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
    await wrapper.find('[data-conversation-id="conv_multi"]').trigger('click')
    await flushPromises()

    // Verify conversation-level APIs were called
    expect(api.fetchAuditConversationRuns).toHaveBeenCalledWith('conv_multi')
    expect(api.fetchAuditRunReplay).toHaveBeenCalledWith('run_a')
    expect(api.fetchAuditRunReplay).toHaveBeenCalledWith('run_b')

    // Turn selector should be visible (2 runs)
    expect(wrapper.find('[data-testid="turn-bar"]').exists()).toBe(true)
    expect(wrapper.findAll('.admin-audit-turn')).toHaveLength(3) // 全部轮次 + 轮次 1 + 轮次 2
    expect(wrapper.find('[data-turn="all"]').text()).toContain('全部轮次')
    expect(wrapper.find('[data-turn="0"]').text()).toContain('轮次 1')
    expect(wrapper.find('[data-turn="1"]').text()).toContain('轮次 2')

    // 轮次数 should show 2
    expect(wrapper.text()).toContain('2')

    // All timeline items merged: 3 from run_a + 2 from run_b = 5
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(5)

    // Timeline items should show turn labels
    expect(wrapper.text()).toContain('轮次 1')
    expect(wrapper.text()).toContain('轮次 2')

    // Filter by turn 1 only
    await wrapper.find('[data-turn="0"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(3)

    // Filter by turn 2 only
    await wrapper.find('[data-turn="1"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(2)

    // Back to all turns
    await wrapper.find('[data-turn="all"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(5)

    // Tool filter should work across turns
    await wrapper.find('[data-filter="tool"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(1)
    expect(wrapper.text()).toContain('tool.called')
  })
})
