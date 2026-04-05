import { describe, expect, it } from 'vitest'
import { buildApprovalEntriesFromList, isTaskActive, resolveTaskConversationId } from './task-runtime'

describe('task-runtime helpers', () => {
  it('resolves task conversation ids from result, result_json, or input', () => {
    expect(resolveTaskConversationId({ result: { conversation_id: 'conv_a' } } as any)).toBe('conv_a')
    expect(resolveTaskConversationId({ result_json: { conversation_id: 'conv_b' } } as any)).toBe('conv_b')
    expect(resolveTaskConversationId({ input: { conversation_id: 'conv_c' } } as any)).toBe('conv_c')
  })

  it('treats queued, running, waiting, and cancel_requested as active', () => {
    expect(isTaskActive({ status: 'queued' } as any)).toBe(true)
    expect(isTaskActive({ status: 'running' } as any)).toBe(true)
    expect(isTaskActive({ status: 'waiting' } as any)).toBe(true)
    expect(isTaskActive({ status: 'cancel_requested' } as any)).toBe(true)
    expect(isTaskActive({ status: 'succeeded' } as any)).toBe(false)
  })

  it('maps approval records directly into approval transcript entries', () => {
    const entries = buildApprovalEntriesFromList([
      {
        id: 'approval_1',
        task_id: 'task_1',
        conversation_id: 'conv_1',
        step_index: 1,
        tool_call_id: 'call_1',
        tool_name: 'bash',
        arguments_summary: 'pwd',
        risk_level: 'low',
        status: 'pending',
      },
    ] as any)

    expect(entries).toEqual([expect.objectContaining({ kind: 'approval', approval: expect.objectContaining({ id: 'approval_1' }) })])
  })
})
