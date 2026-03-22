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
        title: 'First chat',
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
        title: 'Second chat',
        last_message: 'world',
        message_count: 4,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        created_by: 'bob',
        created_at: '2026-03-22T10:00:00Z',
        updated_at: '2026-03-22T10:02:00Z',
        audit_run_id: 'run_2',
      },
    ])
    api.fetchConversation.mockImplementation(async (conversationId: string) => ({
      id: conversationId,
      title: conversationId === 'conv_2' ? 'Second chat' : 'First chat',
      last_message: conversationId === 'conv_2' ? 'world' : 'hello',
      message_count: conversationId === 'conv_2' ? 4 : 2,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: conversationId === 'conv_2' ? 'bob' : 'alice',
      created_at: conversationId === 'conv_2' ? '2026-03-22T10:00:00Z' : '2026-03-22T09:00:00Z',
      updated_at: conversationId === 'conv_2' ? '2026-03-22T10:02:00Z' : '2026-03-22T09:01:00Z',
      audit_run_id: conversationId === 'conv_2' ? 'run_2' : 'run_1',
    }))
    api.fetchAuditRun.mockImplementation(async (runId: string) => ({
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
    }))
    api.fetchAuditRunEvents.mockImplementation(async (runId: string) => ([
      {
        id: runId === 'run_2' ? 2 : 1,
        run_id: runId,
        task_id: runId === 'run_2' ? 'tsk_2' : 'tsk_1',
        seq: 1,
        phase: 'run',
        event_type: 'run.started',
        level: 'info',
        step_index: 0,
        parent_seq: 0,
        ref_artifact_id: '',
        payload: { status: 'running' },
        created_at: '2026-03-22T10:00:00Z',
      },
    ]))
    api.fetchAuditRunReplay.mockImplementation(async (runId: string) => ({
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
          seq: 1,
          phase: 'run',
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
      ],
    }))

    const wrapper = mount(AdminAuditView)

    await flushPromises()
    await wrapper.find('[data-conversation-id="conv_2"]').trigger('click')
    await flushPromises()

    expect(api.fetchConversations).toHaveBeenCalledTimes(1)
    expect(api.fetchConversation).toHaveBeenCalledWith('conv_2')
    expect(api.fetchAuditRun).toHaveBeenCalledWith('run_2')
    expect(api.fetchAuditRunEvents).toHaveBeenCalledWith('run_2')
    expect(api.fetchAuditRunReplay).toHaveBeenCalledWith('run_2')
    expect(wrapper.text()).toContain('Second chat')
    expect(wrapper.text()).toContain('bob')
    expect(wrapper.text()).toContain('run_2')
    expect(wrapper.text()).toContain('run.started')
    expect(wrapper.find('.admin-audit-timeline').exists()).toBe(true)
    expect(wrapper.findAll('.admin-audit-timeline-item')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('暂无回放时间线')
    expect(wrapper.text()).not.toContain('暂无事件')

    expect(wrapper.text()).toContain('对话历史')
    expect(wrapper.text()).toContain('hello')
    expect(wrapper.text()).toContain('assistant')
  })
})
