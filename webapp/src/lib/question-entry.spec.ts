import { describe, expect, it } from 'vitest'

import { normalizeQuestionEntry, summarizeQuestionResponse } from './question-entry'

describe('question-entry', () => {
  it('normalizeQuestionEntry returns prompt options and question flags', () => {
    expect(normalizeQuestionEntry({
      id: 'question-entry-1',
      kind: 'question',
      title: '等待回答',
      question_interaction: {
        id: 'interaction_question_1',
        task_id: 'task_1',
        conversation_id: 'conv_1',
        step_index: 3,
        tool_call_id: 'call_ask',
        kind: 'question',
        status: 'pending',
        request_json: {
          question: 'Which environment?',
          options: ['staging', 'production', 42, ''],
          allow_custom: true,
          multiple: true,
          placeholder: 'Other environment',
        },
        response_json: {
          selected_option_ids: ['staging'],
        },
      },
    } as any)).toEqual({
      interactionId: 'interaction_question_1',
      taskId: 'task_1',
      toolCallId: 'call_ask',
      status: 'pending',
      prompt: 'Which environment?',
      options: ['staging', 'production', '42'],
      placeholder: 'Other environment',
      allowCustom: true,
      multiple: true,
      finalAnswer: 'staging',
    })
  })

  it('summarizeQuestionResponse joins selected options and custom text', () => {
    expect(summarizeQuestionResponse({
      selected_option_id: ' production ',
      selected_option_ids: [' staging ', '', 'preview'],
      custom_text: ' Blue env ',
    })).toBe('production\nstaging、preview\nBlue env')
  })
})
