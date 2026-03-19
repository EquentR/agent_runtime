import { describe, expect, it } from 'vitest'

import {
  buildRunTaskRequest,
  extractStreamText,
  formatTaskError,
  normalizeConversationMessage,
  normalizeRunTaskResult,
  unwrapEnvelope,
} from './api'

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
        Reasoning: 'trace',
        ToolCallId: 'call_1',
        ReasoningItems: [{ Summary: [{ Text: 'plan first' }] }],
        ToolCalls: [{ ID: 'call_1', Name: 'read_file', Arguments: '{}' }],
      }),
    ).toEqual({
      role: 'assistant',
      content: 'hello',
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
        },
      }),
    ).toEqual({
      conversation_id: 'conv_1',
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      messages_appended: 2,
      final_message: {
        role: 'assistant',
        content: 'done',
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
