import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  buildRunTaskRequest,
  extractStreamText,
  formatTaskError,
  normalizeConversationMessage,
  normalizeRunTaskResult,
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
