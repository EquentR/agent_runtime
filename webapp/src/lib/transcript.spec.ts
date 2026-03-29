import { describe, expect, it } from 'vitest'

import { normalizeConversationMessage } from './api'
import { buildApprovalStreamEvent, buildTranscriptEntries, summarizeToolResult, updateTranscriptFromStreamEvent } from './transcript'
import type { TranscriptEntry } from '../types/api'

function comparableEntries(entries: TranscriptEntry[]) {
  return entries.map(({ id: _id, group_key: _groupKey, ...entry }) => entry)
}

describe('buildTranscriptEntries', () => {
  it('builds a shared approval requested event payload from an approval record', () => {
    expect(
      buildApprovalStreamEvent({
        id: 'approval_1',
        task_id: 'task_1',
        conversation_id: 'conv_1',
        step_index: 4,
        tool_call_id: 'call_1',
        tool_name: 'bash',
        arguments_summary: 'rm -rf /tmp/demo',
        risk_level: 'high',
        reason: 'dangerous filesystem mutation',
        status: 'pending',
      }),
    ).toEqual({
      type: 'approval.requested',
      payload: {
        approval_id: 'approval_1',
        task_id: 'task_1',
        conversation_id: 'conv_1',
        step: 4,
        tool_call_id: 'call_1',
        tool_name: 'bash',
        arguments_summary: 'rm -rf /tmp/demo',
        risk_level: 'high',
        reason: 'dangerous filesystem mutation',
        status: 'pending',
        decision: undefined,
        decision_reason: undefined,
        decision_by: undefined,
        decision_at: undefined,
        created_at: undefined,
        updated_at: undefined,
      },
    })
  })

  it('builds a shared approval resolved event payload from an approval record', () => {
    expect(
      buildApprovalStreamEvent(
        {
          id: 'approval_2',
          task_id: 'task_2',
          conversation_id: 'conv_2',
          step_index: 5,
          tool_call_id: 'call_2',
          tool_name: 'delete_file',
          arguments_summary: '{"path":"danger.txt"}',
          risk_level: 'high',
          reason: 'dangerous file mutation',
          status: 'rejected',
          decision_reason: 'not safe',
          decision_by: 'alice',
        },
        { type: 'approval.resolved', decision: 'reject' },
      ),
    ).toEqual({
      type: 'approval.resolved',
      payload: {
        approval_id: 'approval_2',
        task_id: 'task_2',
        conversation_id: 'conv_2',
        step: 5,
        tool_call_id: 'call_2',
        tool_name: 'delete_file',
        arguments_summary: '{"path":"danger.txt"}',
        risk_level: 'high',
        reason: 'dangerous file mutation',
        status: 'rejected',
        decision: 'reject',
        decision_reason: 'not safe',
        decision_by: 'alice',
        decision_at: undefined,
        created_at: undefined,
        updated_at: undefined,
      },
    })
  })

  it('rebuilds reasoning, merged tool detail, and final reply from persisted conversation messages', () => {
    const entries = buildTranscriptEntries([
      { role: 'user', content: 'Check weather' },
      {
        role: 'assistant',
        content: 'It is sunny.',
        reasoning: 'I should check the tool first.',
        tool_calls: [{ id: 'call_1', name: 'weather.lookup', arguments: '{"city":"Beijing"}' }],
      },
      { role: 'tool', content: '{"city":"Beijing","forecast":"sunny"}', tool_call_id: 'call_1' },
    ])

    expect(entries.map((entry) => entry.kind)).toEqual(['user', 'reasoning', 'tool', 'reply'])
    expect(entries[2]).toMatchObject({
      kind: 'tool',
      title: '工具调用',
      status: 'done',
    })
    expect(entries[2].details).toEqual([
      {
        key: 'call_1',
        label: 'weather.lookup',
        preview: '{"city":"Beijing","forecast":"sunny"}',
        collapsed: true,
        loading: false,
        blocks: [
          { label: 'Params', value: '{"city":"Beijing"}' },
          { label: 'Result', value: '{"city":"Beijing","forecast":"sunny"}', loading: false },
        ],
      },
    ])
  })

  it('attaches persisted token usage to the rebuilt assistant reply', () => {
    const entries = buildTranscriptEntries([
      {
        role: 'assistant',
        content: 'Done.',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        usage: {
          prompt_tokens: 123,
          completion_tokens: 45,
          total_tokens: 168,
        },
      } as any,
    ])

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({
      kind: 'reply',
      content: 'Done.',
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      token_usage: {
        prompt_tokens: 123,
        completion_tokens: 45,
        total_tokens: 168,
      },
    })
  })

  it('ignores persisted system prompt messages in normal transcript rendering', () => {
    const entries = buildTranscriptEntries([
      { role: 'user', content: 'hello' },
      {
        role: 'system',
        content: 'Run failed: hidden system text should still be ignored without explicit visibility metadata',
      },
      { role: 'assistant', content: 'hi' },
    ])

    expect(entries.map((entry) => entry.kind)).toEqual(['user', 'reply'])
    expect(entries.some((entry) => (entry.content ?? '').includes('hidden system text'))).toBe(false)
  })

  it('renders visible persisted system failure messages using explicit metadata instead of content matching', () => {
    const entries = buildTranscriptEntries([
      {
        role: 'system',
        content: 'Upstream 502 while contacting provider',
        provider_data: {
          system_message: {
            visible_to_user: true,
            kind: 'failure',
          },
        },
      },
    ] as any)

    expect(entries).toEqual([
      expect.objectContaining({ kind: 'error', title: '运行失败', content: 'Upstream 502 while contacting provider' }),
    ])
  })

  it('preserves explicit system visibility metadata through API normalization', () => {
    const message = normalizeConversationMessage({
      Role: 'system',
      Content: 'Upstream 502 while contacting provider',
      provider_data: {
        system_message: {
          visible_to_user: true,
          kind: 'failure',
        },
      },
    } as any)

    const entries = buildTranscriptEntries([message])

    expect(entries).toEqual([
      expect.objectContaining({ kind: 'error', title: '运行失败', content: 'Upstream 502 while contacting provider' }),
    ])
  })
})

describe('updateTranscriptFromStreamEvent', () => {
  it('merges tool started and finished into a single compact tool entry', () => {
    let entries: TranscriptEntry[] = [{ id: 'user-1', kind: 'user', title: '', content: 'hello' }]

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: { Kind: 'reasoning_delta', Reasoning: 'thinking' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Kind: 'completed',
        Message: {
          Role: 'assistant',
          Content: '',
          ToolCalls: [{ ID: 'call_1', Name: 'read_file', Arguments: '{"path":"README.md"}' }],
        },
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'tool.started',
      payload: { ToolCallID: 'call_1', ToolName: 'read_file', Arguments: '{"path":"README.md"}' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'tool.finished',
      payload: { ToolCallID: 'call_1', ToolName: 'read_file', Output: 'README line 3' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: { Kind: 'text_delta', Text: 'Hello' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'task.failed',
      payload: { error: 'boom' },
    })

    expect(entries.map((entry) => entry.kind)).toEqual(['user', 'reasoning', 'tool', 'reply', 'error'])
    expect(entries.filter((entry) => entry.kind === 'tool')).toHaveLength(1)
    expect(entries[2]).toMatchObject({ title: '工具调用', status: 'done' })
    expect(entries[2].details).toEqual([
      {
        key: 'call_1',
        label: 'read_file',
        preview: 'README line 3',
        collapsed: true,
        loading: false,
        blocks: [
          { label: 'Params', value: '{"path":"README.md"}' },
          { label: 'Result', value: 'README line 3', loading: false },
        ],
      },
    ])
    expect(entries[3]).toMatchObject({ content: 'Hello' })
    expect(entries[4]).toMatchObject({ content: 'boom' })
  })

  it('groups multiple tools from the same step into one tools strip', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Step: 2,
        Kind: 'completed',
        Message: {
          Role: 'assistant',
          Content: '',
          ToolCalls: [
            { ID: 'call_1', Name: 'read_file', Arguments: '{"path":"README.md"}' },
            { ID: 'call_2', Name: 'glob', Arguments: '{"pattern":"src/**/*.ts"}' },
          ],
        },
      },
    })

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({ kind: 'tool', title: '工具调用 (2)', status: 'running' })
    expect(entries[0].details?.map((detail) => detail.label)).toEqual(['read_file', 'glob'])
  })

  it('reuses an existing tool entry even when later events arrive without the same step key', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Step: 3,
        Kind: 'completed',
        Message: {
          Role: 'assistant',
          Content: '',
          ToolCalls: [{ ID: 'call_1', Name: 'read_file', Arguments: '{"path":"image.png"}' }],
        },
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'tool.finished',
      payload: {
        ToolCallID: 'call_1',
        ToolName: 'read_file',
        Err: 'Cannot read "image.png" (this model does not support image input). Inform the user.',
      },
    })

    expect(entries.filter((entry) => entry.kind === 'tool')).toHaveLength(1)
    expect(entries[0].details?.[0]).toMatchObject({
      key: 'call_1',
      loading: false,
    })
    expect(entries[0].details?.[0].preview).toContain('Cannot read "image.png"')
  })

  it('does not append a duplicate terminal error when the latest tool already carries the same failure', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'tool.finished',
      payload: {
        ToolCallID: 'call_1',
        ToolName: 'read_file',
        Err: 'Cannot read "image.png" (this model does not support image input). Inform the user.',
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'task.failed',
      payload: { error: 'Cannot read "image.png" (this model does not support image input). Inform the user.' },
    })

    expect(entries.map((entry) => entry.kind)).toEqual(['tool'])
  })

  it('extracts thinking and running tools from completed assistant messages with empty content', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Kind: 'completed',
        Message: {
          Role: 'assistant',
          Content: '',
          Reasoning: 'need to inspect files',
          ToolCalls: [{ ID: 'call_1', Name: 'list_files', Arguments: '{}' }],
        },
      },
    })

    expect(entries.map((entry) => entry.kind)).toEqual(['reasoning', 'tool'])
    expect(entries[0].details?.[0]).toMatchObject({ label: '思考', preview: 'need to inspect files', loading: false })
    expect(entries[1]).toMatchObject({ title: '工具调用', status: 'running' })
  })

  it('stops the matching tool spinner when a tool message arrives for the same tool_call_id', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Step: 7,
        Kind: 'completed',
        Message: {
          Role: 'assistant',
          Content: '',
          ToolCalls: [{ ID: 'call_1', Name: 'read_file', Arguments: '{"path":"README.md"}' }],
        },
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Step: 7,
        Kind: 'completed',
        Message: {
          Role: 'tool',
          Content: 'README line 3',
          ToolCallID: 'call_1',
        },
      },
    })

    expect(entries.filter((entry) => entry.kind === 'tool')).toHaveLength(1)
    expect(entries[0]).toMatchObject({ kind: 'tool', status: 'done' })
    expect(entries[0].details?.[0]).toMatchObject({
      key: 'call_1',
      loading: false,
      preview: 'README line 3',
    })
  })

  it('merges the same tool call across camelCase and PascalCase stream payloads', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Step: 9,
        Kind: 'completed',
        Message: {
          role: 'assistant',
          content: '',
          toolCalls: [{ id: 'call_1', name: 'read_file', arguments: '{"path":"README.md"}' }],
        },
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Step: 9,
        Kind: 'completed',
        Message: {
          role: 'tool',
          content: 'README line 3',
          toolCallId: 'call_1',
        },
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'task.finished',
      payload: { status: 'succeeded' },
    })

    expect(entries.filter((entry) => entry.kind === 'tool')).toHaveLength(1)
    expect(entries[0].details?.[0]).toMatchObject({
      key: 'call_1',
      loading: false,
      preview: 'README line 3',
    })
  })

  it('stops all remaining spinners when the task finishes', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: { Kind: 'reasoning_delta', Reasoning: 'thinking' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'tool.started',
      payload: { ToolCallID: 'call_1', ToolName: 'read_file', Arguments: '{"path":"README.md"}' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'tool.started',
      payload: { ToolCallID: 'call_2', ToolName: 'glob', Arguments: '{"pattern":"src/**/*.ts"}' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'task.finished',
      payload: { status: 'succeeded' },
    })

    expect(entries[0].kind).toBe('reasoning')
    expect(entries[0].details?.[0].loading).toBe(false)
    expect(entries[1]).toMatchObject({ kind: 'tool', status: 'done' })
    expect(entries[1].details?.map((detail) => detail.loading)).toEqual([false, false])
  })

  it('attaches finish usage stats to the latest reply entry', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: { Kind: 'text_delta', Text: 'Final answer' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'task.finished',
      payload: {
        status: 'succeeded',
        usage: {
          prompt_tokens: 300,
          completion_tokens: 120,
          total_tokens: 420,
        },
      },
    })

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({
      kind: 'reply',
      content: 'Final answer',
      token_usage: {
        prompt_tokens: 300,
        completion_tokens: 120,
        total_tokens: 420,
      },
    })
  })

  it('attaches SSE usage events to the latest reply entry immediately', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: { Kind: 'text_delta', Text: 'Final answer' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Kind: 'usage',
        Usage: {
          PromptTokens: 240,
          CompletionTokens: 80,
          TotalTokens: 320,
        },
      },
    })

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({
      kind: 'reply',
      content: 'Final answer',
      token_usage: {
        prompt_tokens: 240,
        completion_tokens: 80,
        total_tokens: 320,
      },
    })
  })

  it('does not append a failure entry when a task is cancelled', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'tool.started',
      payload: { ToolCallID: 'call_1', ToolName: 'bash', Arguments: '{"command":"sleep"}' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'task.finished',
      payload: { status: 'cancelled' },
    })

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({ kind: 'tool', status: 'done' })
  })

  it('renders pending approval requests as approval transcript entries instead of failures', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'approval.requested',
      payload: {
        approval_id: 'approval_1',
        task_id: 'task_1',
        conversation_id: 'conv_1',
        step: 4,
        tool_call_id: 'call_1',
        tool_name: 'bash',
        arguments_summary: 'rm -rf /tmp/demo',
        risk_level: 'high',
        reason: 'dangerous filesystem mutation',
        status: 'pending',
      },
    })

    expect(entries).toEqual([
      expect.objectContaining({
        kind: 'approval',
        title: '等待审批',
        approval: expect.objectContaining({
          id: 'approval_1',
          tool_name: 'bash',
          risk_level: 'high',
          reason: 'dangerous filesystem mutation',
          status: 'pending',
        }),
      }),
    ])
    expect(entries.some((entry) => entry.kind === 'error')).toBe(false)
  })

  it('updates an existing approval entry when the approval is resolved', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'approval.requested',
      payload: {
        approval_id: 'approval_1',
        task_id: 'task_1',
        conversation_id: 'conv_1',
        step: 4,
        tool_call_id: 'call_1',
        tool_name: 'bash',
        arguments_summary: 'rm -rf /tmp/demo',
        risk_level: 'high',
        reason: 'dangerous filesystem mutation',
        status: 'pending',
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'approval.resolved',
      payload: {
        approval_id: 'approval_1',
        task_id: 'task_1',
        decision: 'reject',
        decision_reason: 'not safe',
        decision_by: 'demo-user',
        status: 'rejected',
      },
    })

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({
      kind: 'approval',
      approval: expect.objectContaining({
        id: 'approval_1',
        status: 'rejected',
        decision: 'reject',
        decision_reason: 'not safe',
        decision_by: 'demo-user',
      }),
    })
  })

  it('renders a complete approval entry when approval.resolved arrives before approval.requested', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'approval.resolved',
      payload: {
        approval_id: 'approval_2',
        task_id: 'task_2',
        conversation_id: 'conv_2',
        step: 9,
        tool_call_id: 'call_2',
        tool_name: 'delete_file',
        arguments_summary: '{"path":"danger.txt"}',
        risk_level: 'high',
        reason: 'dangerous file mutation',
        decision: 'reject',
        decision_reason: 'not safe',
        decision_by: 'demo-user',
        status: 'rejected',
      },
    })

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({
      kind: 'approval',
      title: '审批已处理',
      approval: expect.objectContaining({
        id: 'approval_2',
        tool_name: 'delete_file',
        arguments_summary: '{"path":"danger.txt"}',
        risk_level: 'high',
        reason: 'dangerous file mutation',
        decision: 'reject',
        decision_reason: 'not safe',
        status: 'rejected',
      }),
    })
  })

  it('does not regress an approval entry from resolved back to pending when events arrive out of order', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'approval.resolved',
      payload: {
        approval_id: 'approval_3',
        task_id: 'task_3',
        conversation_id: 'conv_3',
        step: 10,
        tool_call_id: 'call_3',
        tool_name: 'bash',
        arguments_summary: 'rm -rf /tmp/demo',
        risk_level: 'high',
        reason: 'dangerous mutation',
        decision: 'approve',
        decision_reason: 'safe',
        decision_by: 'demo-user',
        status: 'approved',
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'approval.requested',
      payload: {
        approval_id: 'approval_3',
        task_id: 'task_3',
        conversation_id: 'conv_3',
        step: 10,
        tool_call_id: 'call_3',
        tool_name: 'bash',
        arguments_summary: 'rm -rf /tmp/demo',
        risk_level: 'high',
        reason: 'dangerous mutation',
        status: 'pending',
      },
    })

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({
      title: '审批已处理',
      status: 'done',
      approval: expect.objectContaining({
        id: 'approval_3',
        status: 'approved',
        decision: 'approve',
        decision_reason: 'safe',
      }),
    })
  })

  it('does not convert waiting_for_tool_approval completion into an error entry', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'approval.requested',
      payload: {
        approval_id: 'approval_waiting',
        task_id: 'task_waiting',
        conversation_id: 'conv_waiting',
        step: 7,
        tool_call_id: 'call_waiting',
        tool_name: 'bash',
        arguments_summary: 'rm -rf /tmp/demo',
        risk_level: 'high',
        reason: 'dangerous mutation',
        status: 'pending',
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'task.finished',
      payload: {
        status: 'waiting',
        suspend_reason: 'waiting_for_tool_approval',
      },
    })

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({
      kind: 'approval',
      approval: expect.objectContaining({ id: 'approval_waiting', status: 'pending' }),
    })
    expect(entries.some((entry) => entry.kind === 'error')).toBe(false)
  })

  it('ignores completed system prompt messages from the task stream', () => {
    let entries: TranscriptEntry[] = [{ id: 'user-1', kind: 'user', title: '', content: 'hello' }]

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Step: 4,
        Kind: 'completed',
        Message: {
          Role: 'system',
          Content: 'Run failed: hidden runtime-only instructions should still be ignored',
        },
      },
    })

    expect(entries).toEqual([{ id: 'user-1', kind: 'user', title: '', content: 'hello' }])
  })

  it('does not create transcript entries from system injections before a normal assistant reply completes', () => {
    let entries: TranscriptEntry[] = []

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Step: 5,
        Kind: 'completed',
        Message: {
          Role: 'system',
          Content: 'Run failed: hidden pre-model injection',
        },
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: { Kind: 'text_delta', Text: 'Visible answer' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: {
        Step: 5,
        Kind: 'completed',
        Message: {
          Role: 'assistant',
          Content: 'Visible answer',
        },
      },
    })

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({ kind: 'reply', content: 'Visible answer' })
    expect(entries.some((entry) => entry.kind === 'error')).toBe(false)
    expect(entries.some((entry) => (entry.content ?? '').includes('hidden pre-model injection'))).toBe(false)
  })

  it('renders persisted system failure messages as error entries when explicitly marked visible', () => {
    const entries = buildTranscriptEntries([
      {
        role: 'system',
        content: 'run failed: upstream 502',
        provider_data: {
          system_message: {
            visible_to_user: true,
            kind: 'failure',
          },
        },
      },
    ] as any)

    expect(entries).toEqual([
      expect.objectContaining({ kind: 'error', title: '运行失败', content: 'run failed: upstream 502' }),
    ])
  })

  it('matches the final rendering between persisted messages and equivalent stream events', () => {
    const persisted = buildTranscriptEntries([
      { role: 'user', content: 'Check weather' },
      {
        role: 'assistant',
        content: '',
        reasoning: 'I should inspect the forecast tool first.',
        tool_calls: [{ id: 'call_1', name: 'weather.lookup', arguments: '{"city":"Beijing"}' }],
      },
      { role: 'tool', content: '{"forecast":"sunny"}', tool_call_id: 'call_1' },
      { role: 'assistant', content: 'It is sunny.' },
    ])

    let streamed: TranscriptEntry[] = [{ id: 'user-1', kind: 'user', title: '', content: 'Check weather' }]
    streamed = updateTranscriptFromStreamEvent(streamed, {
      type: 'log.message',
      payload: {
        Step: 1,
        Kind: 'completed',
        Message: {
          Role: 'assistant',
          Content: '',
          Reasoning: 'I should inspect the forecast tool first.',
          ToolCalls: [{ ID: 'call_1', Name: 'weather.lookup', Arguments: '{"city":"Beijing"}' }],
        },
      },
    })
    streamed = updateTranscriptFromStreamEvent(streamed, {
      type: 'log.message',
      payload: {
        Step: 1,
        Kind: 'completed',
        Message: {
          Role: 'tool',
          Content: '{"forecast":"sunny"}',
          ToolCallId: 'call_1',
        },
      },
    })
    streamed = updateTranscriptFromStreamEvent(streamed, {
      type: 'log.message',
      payload: { Kind: 'text_delta', Text: 'It is sunny.' },
    })
    streamed = updateTranscriptFromStreamEvent(streamed, {
      type: 'task.finished',
      payload: { status: 'succeeded' },
    })

    expect(comparableEntries(streamed)).toEqual(comparableEntries(persisted))
  })

  it('does not duplicate a streamed final reply when the completed assistant message repeats the same content', () => {
    const persisted = buildTranscriptEntries([
      {
        role: 'assistant',
        content: 'Final answer',
        reasoning: 'thinking',
        provider_id: 'openai',
        model_id: 'gpt-5.4',
      },
    ])

    let streamed: TranscriptEntry[] = []
    streamed = updateTranscriptFromStreamEvent(streamed, {
      type: 'log.message',
      payload: { Kind: 'reasoning_delta', Reasoning: 'thinking' },
    })
    streamed = updateTranscriptFromStreamEvent(streamed, {
      type: 'log.message',
      payload: { Kind: 'text_delta', Text: 'Final answer' },
    })
    streamed = updateTranscriptFromStreamEvent(streamed, {
      type: 'log.message',
      payload: {
        Kind: 'completed',
        Message: {
          Role: 'assistant',
          Content: 'Final answer',
          Reasoning: 'thinking',
          ProviderID: 'openai',
          ModelID: 'gpt-5.4',
        },
      },
    })

    expect(comparableEntries(streamed)).toEqual(comparableEntries(persisted))
  })

  it('keeps streamed tool calls from different rounds in separate tool groups when tool events omit step', () => {
    let entries: TranscriptEntry[] = [{ id: 'user-1', kind: 'user', title: '', content: 'first question' }]

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'tool.started',
      payload: {
        ToolCallID: 'call_1',
        ToolName: 'read_file',
        Arguments: '{"path":"README.md"}',
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'tool.finished',
      payload: {
        ToolCallID: 'call_1',
        ToolName: 'read_file',
        Output: 'README line 1',
      },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: { Kind: 'text_delta', Text: 'first answer' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'task.finished',
      payload: { status: 'succeeded' },
    })

    entries = [...entries, { id: 'user-2', kind: 'user', title: '', content: 'second question' }]
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'tool.started',
      payload: {
        ToolCallID: 'call_2',
        ToolName: 'glob',
        Arguments: '{"pattern":"src/**/*.ts"}',
      },
    })

    const toolEntries = entries.filter((entry) => entry.kind === 'tool')

    expect(toolEntries).toHaveLength(2)
    expect(toolEntries[0].details?.map((detail) => detail.key)).toEqual(['call_1'])
    expect(toolEntries[1].details?.map((detail) => detail.key)).toEqual(['call_2'])
    expect(toolEntries[1].status).toBe('running')
  })
})

describe('summarizeToolResult', () => {
  it('condenses json output into a short summary', () => {
    expect(summarizeToolResult('{"forecast":"sunny","city":"Beijing","temp":26}')).toContain('forecast')
  })
})
