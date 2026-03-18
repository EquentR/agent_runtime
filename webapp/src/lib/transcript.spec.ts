import { describe, expect, it } from 'vitest'

import { buildTranscriptEntries, summarizeToolResult, updateTranscriptFromStreamEvent } from './transcript'
import type { TranscriptEntry } from '../types/api'

function comparableEntries(entries: TranscriptEntry[]) {
  return entries.map(({ id: _id, group_key: _groupKey, ...entry }) => entry)
}

describe('buildTranscriptEntries', () => {
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

  it('renders persisted system failure messages as error entries', () => {
    const entries = buildTranscriptEntries([{ role: 'system', content: 'Run failed: upstream 502' }])

    expect(entries).toEqual([
      expect.objectContaining({ kind: 'error', title: 'Run failed', content: 'Run failed: upstream 502' }),
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
})

describe('summarizeToolResult', () => {
  it('condenses json output into a short summary', () => {
    expect(summarizeToolResult('{"forecast":"sunny","city":"Beijing","temp":26}')).toContain('forecast')
  })
})
