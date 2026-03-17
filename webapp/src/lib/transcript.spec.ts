import { describe, expect, it } from 'vitest'

import { buildTranscriptEntries, summarizeToolResult, updateTranscriptFromStreamEvent } from './transcript'
import type { TranscriptEntry } from '../types/api'

describe('buildTranscriptEntries', () => {
  it('rebuilds reasoning, tool summary, and final reply from persisted conversation messages', () => {
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
      title: 'weather.lookup',
      summary: 'city=Beijing forecast=sunny',
    })
  })
})

describe('updateTranscriptFromStreamEvent', () => {
  it('appends reasoning, tool, reply, and error traces from stream/task events', () => {
    let entries: TranscriptEntry[] = [{ id: 'user-1', kind: 'user', title: 'You', content: 'hello' }]

    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'log.message',
      payload: { Kind: 'reasoning_delta', Reasoning: 'thinking' },
    })
    entries = updateTranscriptFromStreamEvent(entries, {
      type: 'tool.started',
      payload: { ToolCallID: 'call_1', ToolName: 'read_file' },
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
    expect(entries[2]).toMatchObject({ summary: 'README line 3' })
    expect(entries[3]).toMatchObject({ content: 'Hello' })
    expect(entries[4]).toMatchObject({ content: 'boom' })
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
    expect(entries[0]).toMatchObject({ content: 'need to inspect files' })
    expect(entries[1]).toMatchObject({ title: 'list_files', status: 'running' })
  })

  it('renders persisted system failure messages as error entries', () => {
    const entries = buildTranscriptEntries([{ role: 'system', content: 'Run failed: upstream 502' }])

    expect(entries).toEqual([
      expect.objectContaining({ kind: 'error', title: 'Run failed', content: 'Run failed: upstream 502' }),
    ])
  })
})

describe('summarizeToolResult', () => {
  it('condenses json output into a short summary', () => {
    expect(summarizeToolResult('{"forecast":"sunny","city":"Beijing","temp":26}')).toBe(
      'forecast=sunny city=Beijing temp=26',
    )
  })
})
