import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  buildRunTaskRequest,
  cancelTask,
  extractStreamText,
  formatTaskError,
  normalizeToolApproval,
  normalizeConversationMessage,
  normalizeRunTaskResult,
  streamRunTask,
  unwrapEnvelope,
} from './api'

describe('auth normalization helpers', () => {
  it('includes role from backend auth payloads', async () => {
    const api = (await import('./api')) as Record<string, unknown>

    expect(typeof api.normalizeAuthUser).toBe('function')

    if (typeof api.normalizeAuthUser !== 'function') {
      return
    }

    expect(api.normalizeAuthUser({ id: 7, username: ' admin ', role: 'admin' })).toEqual({
      id: 7,
      username: 'admin',
      role: 'admin',
    })
  })
})

describe('approval API helpers', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.stubGlobal('fetch', vi.fn())
  })

  it('normalizes approval payloads from REST and SSE shapes', () => {
    expect(
      normalizeToolApproval({
        ID: 'approval_1',
        TaskID: 'task_1',
        ConversationID: 'conv_1',
        StepIndex: 3,
        ToolCallID: 'call_1',
        ToolName: 'bash',
        ArgumentsSummary: 'rm -rf /tmp/demo',
        RiskLevel: 'high',
        Reason: 'dangerous filesystem mutation',
        Status: 'pending',
      } as any),
    ).toMatchObject({
      id: 'approval_1',
      task_id: 'task_1',
      conversation_id: 'conv_1',
      step_index: 3,
      tool_call_id: 'call_1',
      tool_name: 'bash',
      arguments_summary: 'rm -rf /tmp/demo',
      risk_level: 'high',
      reason: 'dangerous filesystem mutation',
      status: 'pending',
    })
  })

  it('lists approvals and submits approval decisions with shared helpers', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: [
            {
              id: 'approval_1',
              task_id: 'task_1',
              conversation_id: 'conv_1',
              step_index: 3,
              tool_call_id: 'call_1',
              tool_name: 'bash',
              arguments_summary: 'rm -rf /tmp/demo',
              risk_level: 'high',
              reason: 'dangerous filesystem mutation',
              status: 'pending',
              created_at: '2026-03-29T09:00:00Z',
              updated_at: '2026-03-29T09:00:00Z',
            },
          ],
          time: '',
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: {
            id: 'approval_1',
            task_id: 'task_1',
            conversation_id: 'conv_1',
            step_index: 3,
            tool_call_id: 'call_1',
            tool_name: 'bash',
            arguments_summary: 'rm -rf /tmp/demo',
            risk_level: 'high',
            status: 'approved',
            decision_by: 'demo-user',
            decision_reason: 'looks safe now',
            decision_at: '2026-03-29T09:01:00Z',
            created_at: '2026-03-29T09:00:00Z',
            updated_at: '2026-03-29T09:01:00Z',
          },
          time: '',
        }),
      } as Response)

    const api = (await import('./api')) as Record<string, unknown>

    expect(typeof api.fetchTaskApprovals).toBe('function')
    expect(typeof api.decideTaskApproval).toBe('function')

    if (typeof api.fetchTaskApprovals !== 'function' || typeof api.decideTaskApproval !== 'function') {
      return
    }

    const approvals = await api.fetchTaskApprovals('task_1')
    const resolved = await api.decideTaskApproval('task_1', 'approval_1', {
      decision: 'approve',
      reason: 'looks safe now',
    })

    expect(fetch).toHaveBeenNthCalledWith(1, '/api/v1/tasks/task_1/approvals', expect.objectContaining({ credentials: 'include' }))
    expect(fetch).toHaveBeenNthCalledWith(
      2,
      '/api/v1/tasks/task_1/approvals/approval_1/decision',
      expect.objectContaining({
        credentials: 'include',
        method: 'POST',
        body: JSON.stringify({ decision: 'approve', reason: 'looks safe now' }),
      }),
    )
    expect(approvals).toEqual([
      expect.objectContaining({ id: 'approval_1', task_id: 'task_1', reason: 'dangerous filesystem mutation', status: 'pending' }),
    ])
    expect(resolved).toMatchObject({ id: 'approval_1', status: 'approved', decision_reason: 'looks safe now' })
  })

  it('submits task cancellation through the shared helper', async () => {
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        ok: true,
        code: 200,
        message: 'OK',
        data: {
          id: 'task_1',
          task_type: 'agent.run',
          status: 'cancel_requested',
          input: { conversation_id: 'conv_1' },
          created_by: 'demo-user',
          created_at: '2026-03-29T09:00:00Z',
          updated_at: '2026-03-29T09:01:00Z',
        },
        time: '',
      }),
    } as Response)

    const task = await cancelTask('task_1')

    expect(fetch).toHaveBeenCalledWith(
      '/api/v1/tasks/task_1/cancel',
      expect.objectContaining({
        credentials: 'include',
        method: 'POST',
      }),
    )
    expect(task).toMatchObject({ id: 'task_1', status: 'cancel_requested' })
  })
})

describe('audit API helpers', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.stubGlobal('fetch', vi.fn())
  })

  it('fetches audit run, events, and replay payloads from the backend', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: {
            id: 'run_1',
            task_id: 'task_1',
            conversation_id: 'conv_1',
            task_type: 'agent.run',
            provider_id: 'openai',
            model_id: 'gpt-5.4',
            runner_id: 'runner_1',
            status: 'succeeded',
            created_by: 'alice',
            replayable: true,
            schema_version: 'v1',
            created_at: '2026-03-22T09:00:00Z',
            updated_at: '2026-03-22T09:00:02Z',
          },
          time: '',
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: [
            {
              id: 1,
              run_id: 'run_1',
              task_id: 'task_1',
              seq: 1,
              phase: 'run',
              event_type: 'run.started',
              level: 'info',
              step_index: 0,
              parent_seq: 0,
              ref_artifact_id: '',
              payload: { status: 'running' },
              created_at: '2026-03-22T09:00:00Z',
            },
          ],
          time: '',
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: {
            run: {
              id: 'run_1',
              task_id: 'task_1',
              conversation_id: 'conv_1',
              task_type: 'agent.run',
              provider_id: 'openai',
              model_id: 'gpt-5.4',
              runner_id: 'runner_1',
              status: 'succeeded',
              created_by: 'alice',
              replayable: true,
              schema_version: 'v1',
              created_at: '2026-03-22T09:00:00Z',
              updated_at: '2026-03-22T09:00:02Z',
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
            ],
            artifacts: [],
          },
          time: '',
        }),
      } as Response)

    const api = (await import('./api')) as Record<string, unknown>

    expect(typeof api.fetchAuditRun).toBe('function')
    expect(typeof api.fetchAuditRunEvents).toBe('function')
    expect(typeof api.fetchAuditRunReplay).toBe('function')

    if (
      typeof api.fetchAuditRun !== 'function' ||
      typeof api.fetchAuditRunEvents !== 'function' ||
      typeof api.fetchAuditRunReplay !== 'function'
    ) {
      return
    }

    const run = await api.fetchAuditRun('run_1')
    const events = await api.fetchAuditRunEvents('run_1')
    const replay = await api.fetchAuditRunReplay('run_1')

    expect(fetch).toHaveBeenNthCalledWith(1, '/api/v1/audit/runs/run_1', expect.objectContaining({ credentials: 'include' }))
    expect(fetch).toHaveBeenNthCalledWith(2, '/api/v1/audit/runs/run_1/events', expect.objectContaining({ credentials: 'include' }))
    expect(fetch).toHaveBeenNthCalledWith(3, '/api/v1/audit/runs/run_1/replay', expect.objectContaining({ credentials: 'include' }))
    expect(run).toMatchObject({ id: 'run_1', conversation_id: 'conv_1', created_by: 'alice' })
    expect(events).toEqual([
      expect.objectContaining({ run_id: 'run_1', event_type: 'run.started', payload: { status: 'running' } }),
    ])
    expect(replay).toMatchObject({
      run: expect.objectContaining({ id: 'run_1', conversation_id: 'conv_1' }),
      timeline: [expect.objectContaining({ event_type: 'run.started' })],
      artifacts: [],
    })
  })

  it('fetches all audit runs for a conversation via the conversation-level endpoint', async () => {
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        ok: true,
        code: 200,
        message: 'OK',
        data: [
          {
            id: 'run_1',
            task_id: 'task_1',
            conversation_id: 'conv_1',
            task_type: 'agent.run',
            status: 'succeeded',
            created_by: 'alice',
            schema_version: 'v1',
            created_at: '2026-03-22T09:00:00Z',
          },
          {
            id: 'run_2',
            task_id: 'task_2',
            conversation_id: 'conv_1',
            task_type: 'agent.run',
            status: 'succeeded',
            created_by: 'alice',
            schema_version: 'v1',
            created_at: '2026-03-22T09:05:00Z',
          },
        ],
        time: '',
      }),
    } as Response)

    const api = (await import('./api')) as Record<string, unknown>

    expect(typeof api.fetchAuditConversationRuns).toBe('function')

    if (typeof api.fetchAuditConversationRuns !== 'function') {
      return
    }

    const runs = await api.fetchAuditConversationRuns('conv_1')

    expect(fetch).toHaveBeenCalledWith(
      '/api/v1/audit/conversations/conv_1/runs',
      expect.objectContaining({ credentials: 'include' }),
    )
    expect(runs).toHaveLength(2)
    expect(runs).toEqual([
      expect.objectContaining({ id: 'run_1', conversation_id: 'conv_1' }),
      expect.objectContaining({ id: 'run_2', conversation_id: 'conv_1' }),
    ])
  })

  it('fetches all audit events for a conversation via the conversation-level endpoint', async () => {
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        ok: true,
        code: 200,
        message: 'OK',
        data: [
          {
            id: 1,
            run_id: 'run_1',
            task_id: 'task_1',
            seq: 1,
            phase: 'run',
            event_type: 'step.started',
            level: 'info',
            step_index: 0,
            parent_seq: 0,
            payload: {},
            created_at: '2026-03-22T09:00:00Z',
          },
          {
            id: 2,
            run_id: 'run_1',
            task_id: 'task_1',
            seq: 2,
            phase: 'run',
            event_type: 'step.finished',
            level: 'info',
            step_index: 0,
            parent_seq: 1,
            payload: {},
            created_at: '2026-03-22T09:00:01Z',
          },
          {
            id: 3,
            run_id: 'run_2',
            task_id: 'task_2',
            seq: 1,
            phase: 'run',
            event_type: 'step.started',
            level: 'info',
            step_index: 0,
            parent_seq: 0,
            payload: {},
            created_at: '2026-03-22T09:05:00Z',
          },
        ],
        time: '',
      }),
    } as Response)

    const api = (await import('./api')) as Record<string, unknown>

    expect(typeof api.fetchAuditConversationEvents).toBe('function')

    if (typeof api.fetchAuditConversationEvents !== 'function') {
      return
    }

    const events = await api.fetchAuditConversationEvents('conv_1')

    expect(fetch).toHaveBeenCalledWith(
      '/api/v1/audit/conversations/conv_1/events',
      expect.objectContaining({ credentials: 'include' }),
    )
    expect(events).toHaveLength(3)
    expect(events).toEqual([
      expect.objectContaining({ run_id: 'run_1', event_type: 'step.started' }),
      expect.objectContaining({ run_id: 'run_1', event_type: 'step.finished' }),
      expect.objectContaining({ run_id: 'run_2', event_type: 'step.started' }),
    ])
  })
})

describe('prompt API helpers', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.stubGlobal('fetch', vi.fn())
  })

  it('fetches prompt documents and bindings from the backend', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: [
            {
              id: 'doc-1',
              name: 'Welcome Prompt',
              description: 'Session prompt',
              content: 'Always explain the plan first.',
              scope: 'admin',
              status: 'active',
              created_by: 'alice',
              updated_by: 'alice',
              created_at: '2026-03-23T09:00:00Z',
              updated_at: '2026-03-23T09:00:00Z',
            },
          ],
          time: '',
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: [
            {
              id: 11,
              prompt_id: 'doc-1',
              scene: 'agent.run.default',
              phase: 'session',
              is_default: true,
              priority: 5,
              provider_id: '',
              model_id: '',
              status: 'active',
              created_by: 'alice',
              updated_by: 'alice',
              created_at: '2026-03-23T09:00:00Z',
              updated_at: '2026-03-23T09:00:00Z',
            },
          ],
          time: '',
        }),
      } as Response)

    const api = (await import('./api')) as Record<string, unknown>

    expect(typeof api.fetchPromptDocuments).toBe('function')
    expect(typeof api.fetchPromptBindings).toBe('function')

    if (typeof api.fetchPromptDocuments !== 'function' || typeof api.fetchPromptBindings !== 'function') {
      return
    }

    const documents = await api.fetchPromptDocuments()
    const bindings = await api.fetchPromptBindings()

    expect(fetch).toHaveBeenNthCalledWith(1, '/api/v1/prompts/documents', expect.objectContaining({ credentials: 'include' }))
    expect(fetch).toHaveBeenNthCalledWith(2, '/api/v1/prompts/bindings', expect.objectContaining({ credentials: 'include' }))
    expect(documents).toEqual([
      expect.objectContaining({ id: 'doc-1', name: 'Welcome Prompt', status: 'active' }),
    ])
    expect(bindings).toEqual([
      expect.objectContaining({ id: 11, prompt_id: 'doc-1', phase: 'session', is_default: true, priority: 5 }),
    ])
  })

  it('creates and updates prompt documents with the expected request payloads', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: {
            id: 'doc-1',
            name: 'Welcome Prompt',
            description: 'Session prompt',
            content: 'Always explain the plan first.',
            scope: 'admin',
            status: 'active',
            created_by: 'alice',
            updated_by: 'alice',
            created_at: '2026-03-23T09:00:00Z',
            updated_at: '2026-03-23T09:00:00Z',
          },
          time: '',
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: {
            id: 'doc-1',
            name: 'Welcome Prompt Updated',
            description: 'Session prompt',
            content: 'Keep the answer concise.',
            scope: 'admin',
            status: 'disabled',
            created_by: 'alice',
            updated_by: 'alice',
            created_at: '2026-03-23T09:00:00Z',
            updated_at: '2026-03-23T09:05:00Z',
          },
          time: '',
        }),
      } as Response)

    const api = (await import('./api')) as Record<string, unknown>

    expect(typeof api.createPromptDocument).toBe('function')
    expect(typeof api.updatePromptDocument).toBe('function')

    if (typeof api.createPromptDocument !== 'function' || typeof api.updatePromptDocument !== 'function') {
      return
    }

    await api.createPromptDocument({
      id: 'doc-1',
      name: 'Welcome Prompt',
      description: 'Session prompt',
      content: 'Always explain the plan first.',
      scope: 'admin',
      status: 'active',
    })
    await api.updatePromptDocument('doc-1', {
      name: 'Welcome Prompt Updated',
      description: 'Session prompt',
      content: 'Keep the answer concise.',
      scope: 'admin',
      status: 'disabled',
    })

    expect(fetch).toHaveBeenNthCalledWith(
      1,
      '/api/v1/prompts/documents',
      expect.objectContaining({
        credentials: 'include',
        method: 'POST',
        body: JSON.stringify({
          id: 'doc-1',
          name: 'Welcome Prompt',
          description: 'Session prompt',
          content: 'Always explain the plan first.',
          scope: 'admin',
          status: 'active',
        }),
      }),
    )
    expect(fetch).toHaveBeenNthCalledWith(
      2,
      '/api/v1/prompts/documents/doc-1',
      expect.objectContaining({
        credentials: 'include',
        method: 'PUT',
        body: JSON.stringify({
          name: 'Welcome Prompt Updated',
          description: 'Session prompt',
          content: 'Keep the answer concise.',
          scope: 'admin',
          status: 'disabled',
        }),
      }),
    )
  })

  it('deletes prompt documents with the expected request payload', async () => {
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        ok: true,
        code: 200,
        message: 'OK',
        data: { deleted: true },
        time: '',
      }),
    } as Response)

    const api = (await import('./api')) as Record<string, unknown>

    expect(typeof api.deletePromptDocument).toBe('function')

    if (typeof api.deletePromptDocument !== 'function') {
      return
    }

    await api.deletePromptDocument('doc-1')

    expect(fetch).toHaveBeenNthCalledWith(
      1,
      '/api/v1/prompts/documents/doc-1',
      expect.objectContaining({
        credentials: 'include',
        method: 'DELETE',
      }),
    )
  })

  it('creates, updates, and deletes prompt bindings with the expected request payloads', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: {
            id: 11,
            prompt_id: 'doc-1',
            scene: 'agent.run.default',
            phase: 'session',
            is_default: true,
            priority: 5,
            provider_id: '',
            model_id: '',
            status: 'active',
            created_by: 'alice',
            updated_by: 'alice',
            created_at: '2026-03-23T09:00:00Z',
            updated_at: '2026-03-23T09:00:00Z',
          },
          time: '',
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: {
            id: 11,
            prompt_id: 'doc-1',
            scene: 'agent.run.default',
            phase: 'tool_result',
            is_default: false,
            priority: 20,
            provider_id: 'openai',
            model_id: 'gpt-5.4',
            status: 'disabled',
            created_by: 'alice',
            updated_by: 'alice',
            created_at: '2026-03-23T09:00:00Z',
            updated_at: '2026-03-23T09:05:00Z',
          },
          time: '',
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ok: true,
          code: 200,
          message: 'OK',
          data: { deleted: true },
          time: '',
        }),
      } as Response)

    const api = (await import('./api')) as Record<string, unknown>

    expect(typeof api.createPromptBinding).toBe('function')
    expect(typeof api.updatePromptBinding).toBe('function')
    expect(typeof api.deletePromptBinding).toBe('function')

    if (
      typeof api.createPromptBinding !== 'function' ||
      typeof api.updatePromptBinding !== 'function' ||
      typeof api.deletePromptBinding !== 'function'
    ) {
      return
    }

    await api.createPromptBinding({
      prompt_id: 'doc-1',
      scene: 'agent.run.default',
      phase: 'session',
      is_default: true,
      priority: 5,
      provider_id: '',
      model_id: '',
      status: 'active',
    })
    await api.updatePromptBinding(11, {
      prompt_id: 'doc-1',
      scene: 'agent.run.default',
      phase: 'tool_result',
      is_default: false,
      priority: 20,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      status: 'disabled',
    })
    await api.deletePromptBinding(11)

    expect(fetch).toHaveBeenNthCalledWith(
      1,
      '/api/v1/prompts/bindings',
      expect.objectContaining({
        credentials: 'include',
        method: 'POST',
        body: JSON.stringify({
          prompt_id: 'doc-1',
          scene: 'agent.run.default',
          phase: 'session',
          is_default: true,
          priority: 5,
          provider_id: '',
          model_id: '',
          status: 'active',
        }),
      }),
    )
    expect(fetch).toHaveBeenNthCalledWith(
      2,
      '/api/v1/prompts/bindings/11',
      expect.objectContaining({
        credentials: 'include',
        method: 'PUT',
        body: JSON.stringify({
          prompt_id: 'doc-1',
          scene: 'agent.run.default',
          phase: 'tool_result',
          is_default: false,
          priority: 20,
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          status: 'disabled',
        }),
      }),
    )
    expect(fetch).toHaveBeenNthCalledWith(
      3,
      '/api/v1/prompts/bindings/11',
      expect.objectContaining({
        credentials: 'include',
        method: 'DELETE',
      }),
    )
  })
})

describe('unwrapEnvelope', () => {
  it('returns data when backend reports success', () => {
    const value = unwrapEnvelope({
      ok: true,
      code: 200,
      message: 'OK',
      data: { id: 'conv_1' },
      time: '2026-03-17 12:00:00',
    })

    expect(value).toEqual({ id: 'conv_1' })
  })

  it('throws when backend reports failure', () => {
    expect(() =>
      unwrapEnvelope({
        ok: false,
        code: 500,
        message: 'boom',
        data: null,
        time: '2026-03-17 12:00:00',
      }),
    ).toThrow('boom')
  })
})

describe('buildRunTaskRequest', () => {
  it('creates an agent.run task payload compatible with the Go backend', () => {
    expect(
      buildRunTaskRequest({
        createdBy: 'demo-user',
        conversationId: 'conv_1',
        providerId: 'openai',
        modelId: 'gpt-5.4',
        message: 'hello',
      }),
    ).toEqual({
      task_type: 'agent.run',
      created_by: 'demo-user',
      input: {
        conversation_id: 'conv_1',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        message: 'hello',
        created_by: 'demo-user',
      },
    })
  })
})

describe('normalizeConversationMessage', () => {
  it('maps backend PascalCase message fields into frontend camel/lowercase fields', () => {
    expect(
      normalizeConversationMessage({
        Role: 'assistant',
        Content: 'hello',
        ProviderID: 'openai',
        ModelID: 'gpt-5.4',
        Reasoning: 'trace',
        ToolCallId: 'call_1',
        ReasoningItems: [{ Summary: [{ Text: 'plan first' }] }],
        ToolCalls: [{ ID: 'call_1', Name: 'read_file', Arguments: '{}' }],
      }),
    ).toMatchObject({
      role: 'assistant',
      content: 'hello',
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      reasoning: 'trace',
      tool_call_id: 'call_1',
      reasoning_items: [{ text: 'plan first' }],
      tool_calls: [{ id: 'call_1', name: 'read_file', arguments: '{}' }],
    })
  })

  it('normalizes camelCase tool fields so static replay can merge with SSE cards', () => {
    expect(
      normalizeConversationMessage({
        role: 'tool',
        content: 'README line 3',
        toolCallId: 'call_1',
        toolCalls: [{ id: 'call_1', name: 'read_file', arguments: '{}' }],
      }),
    ).toEqual({
      role: 'tool',
      content: 'README line 3',
      tool_call_id: 'call_1',
      tool_calls: [{ id: 'call_1', name: 'read_file', arguments: '{}' }],
    })
  })

  it('normalizes persisted token usage from backend message payloads', () => {
    expect(
      normalizeConversationMessage({
        Role: 'assistant',
        Content: 'hello',
        Usage: {
          PromptTokens: 123,
          CompletionTokens: 45,
          TotalTokens: 168,
        },
      }),
    ).toMatchObject({
      role: 'assistant',
      content: 'hello',
      usage: {
        prompt_tokens: 123,
        completion_tokens: 45,
        total_tokens: 168,
      },
    })
  })
})

describe('normalizeRunTaskResult', () => {
  it('normalizes final_message from backend task result payload', () => {
    expect(
      normalizeRunTaskResult({
        conversation_id: 'conv_1',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        messages_appended: 2,
        final_message: {
          Role: 'assistant',
          Content: 'done',
          ProviderID: 'openai',
          ModelID: 'gpt-5.4',
        },
      }),
    ).toMatchObject({
      conversation_id: 'conv_1',
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      messages_appended: 2,
      final_message: {
        role: 'assistant',
        content: 'done',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
      },
    })
  })
})

describe('extractStreamText', () => {
  it('returns text delta from log.message SSE payloads', () => {
    expect(
      extractStreamText({
        type: 'log.message',
        payload: {
          Kind: 'text_delta',
          Text: 'Hello',
        },
      }),
    ).toBe('Hello')
  })

  it('returns final completed assistant message when present', () => {
    expect(
      extractStreamText({
        type: 'log.message',
        payload: {
          Kind: 'completed',
          Message: {
            Role: 'assistant',
            Content: 'Hello there',
          },
        },
      }),
    ).toBe('Hello there')
  })
})

describe('formatTaskError', () => {
  it('extracts message from object-shaped task errors', () => {
    expect(formatTaskError({ message: 'gateway 502' })).toBe('gateway 502')
  })

  it('falls back to task status text when error is empty', () => {
    expect(formatTaskError(undefined, 'failed')).toBe('Task failed')
  })
})

describe('streamRunTask', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('uses the provided after_seq when opening the SSE stream', async () => {
    class MockEventSource {
      static instances: MockEventSource[] = []

      url: string
      withCredentials: boolean
      onerror: (() => void) | null = null
      private readonly listeners = new Map<string, Array<(event: MessageEvent<string>) => void>>()

      constructor(url: string, options?: { withCredentials?: boolean }) {
        this.url = url
        this.withCredentials = options?.withCredentials ?? false
        MockEventSource.instances.push(this)
      }

      addEventListener(type: string, handler: (event: MessageEvent<string>) => void) {
        this.listeners.set(type, [...(this.listeners.get(type) ?? []), handler])
      }

      close() {
        void 0
      }

      emit(type: string, data: unknown) {
        for (const handler of this.listeners.get(type) ?? []) {
          handler({ data: JSON.stringify(data) } as MessageEvent<string>)
        }
      }
    }

    vi.stubGlobal('EventSource', MockEventSource as unknown as typeof EventSource)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        ok: true,
        code: 200,
        message: 'OK',
        data: {
          id: 'task_1',
          task_type: 'agent.run',
          status: 'succeeded',
          input: { conversation_id: 'conv_new' },
          result: {
            conversation_id: 'conv_new',
            provider_id: 'openai',
            model_id: 'gpt-5.4',
            messages_appended: 2,
            final_message: { Role: 'assistant', Content: 'done' },
          },
        },
      }),
    } as Response))

    const promise = streamRunTask('task_1', () => void 0, undefined, { afterSeq: 7 })

    expect(MockEventSource.instances).toHaveLength(1)
    expect(MockEventSource.instances[0]?.url).toContain('/api/v1/tasks/task_1/events?after_seq=7')

    MockEventSource.instances[0]?.emit('task.finished', {
      task_id: 'task_1',
      seq: 8,
      type: 'task.finished',
      payload: { status: 'succeeded' },
    })

    await expect(promise).resolves.toMatchObject({ conversation_id: 'conv_new' })
  })

  it('forwards approval stream events to the shared event handler', async () => {
    class MockEventSource {
      static instances: MockEventSource[] = []

      url: string
      withCredentials: boolean
      onerror: (() => void) | null = null
      private readonly listeners = new Map<string, Array<(event: MessageEvent<string>) => void>>()

      constructor(url: string, options?: { withCredentials?: boolean }) {
        this.url = url
        this.withCredentials = options?.withCredentials ?? false
        MockEventSource.instances.push(this)
      }

      addEventListener(type: string, handler: (event: MessageEvent<string>) => void) {
        this.listeners.set(type, [...(this.listeners.get(type) ?? []), handler])
      }

      close() {
        void 0
      }

      emit(type: string, data: unknown) {
        for (const handler of this.listeners.get(type) ?? []) {
          handler({ data: JSON.stringify(data) } as MessageEvent<string>)
        }
      }
    }

    const onEvent = vi.fn()

    vi.stubGlobal('EventSource', MockEventSource as unknown as typeof EventSource)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        ok: true,
        code: 200,
        message: 'OK',
        data: {
          id: 'task_1',
          task_type: 'agent.run',
          status: 'succeeded',
          input: { conversation_id: 'conv_new' },
          result: {
            conversation_id: 'conv_new',
            provider_id: 'openai',
            model_id: 'gpt-5.4',
            messages_appended: 1,
            final_message: { Role: 'assistant', Content: 'done' },
          },
        },
      }),
    } as Response))

    const promise = streamRunTask('task_1', () => void 0, onEvent)

    MockEventSource.instances[0]?.emit('approval.requested', {
      task_id: 'task_1',
      seq: 1,
      type: 'approval.requested',
      payload: { approval_id: 'approval_1', tool_name: 'bash', status: 'pending' },
    })
    MockEventSource.instances[0]?.emit('approval.resolved', {
      task_id: 'task_1',
      seq: 2,
      type: 'approval.resolved',
      payload: { approval_id: 'approval_1', status: 'approved' },
    })
    MockEventSource.instances[0]?.emit('task.finished', {
      task_id: 'task_1',
      seq: 3,
      type: 'task.finished',
      payload: { status: 'succeeded' },
    })

    await expect(promise).resolves.toMatchObject({ conversation_id: 'conv_new' })
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({ type: 'approval.requested' }))
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({ type: 'approval.resolved' }))
  })
})
