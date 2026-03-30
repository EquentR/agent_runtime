import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import MessageList from './MessageList.vue'

describe('MessageList', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: vi.fn().mockResolvedValue(undefined),
      },
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('does not render the old chat title heading', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [],
      },
    })

    expect(wrapper.text()).not.toContain('Agent Runtime Chat')
  })

  it('does not render the old inline status header inside the message panel', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [],
      },
    })

    expect(wrapper.find('.messages-header').exists()).toBe(false)
    expect(wrapper.find('.status-pill').exists()).toBe(false)
  })

  it('does not render syncing status inside the message panel', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: true,
        entries: [],
      },
    })

    expect(wrapper.find('.messages-header').exists()).toBe(false)
    expect(wrapper.find('.status-pill').exists()).toBe(false)
  })

  it('shows Chinese empty-state copy for a new conversation', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [],
      },
    })

    expect(wrapper.find('.messages-empty').text()).toContain('请尽情使唤 ~')
  })

  it('shows a generating indicator at the bottom while syncing', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: true,
        entries: [
          { id: 'reply-1', kind: 'reply', title: '', content: 'first chunk' },
        ],
      },
    })

    const indicator = wrapper.find('.messages-generating-indicator')
    expect(indicator.exists()).toBe(true)
    expect(indicator.text()).toContain('正在生成')
    expect(indicator.find('.messages-generating-spinner').exists()).toBe(true)
  })

  it('scrolls the message area to the real bottom when entries change', async () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          { id: 'reply-1', kind: 'reply', title: '', content: 'first' },
        ],
      },
      attachTo: document.body,
    })

    const body = wrapper.find('.messages-body').element as HTMLDivElement
    Object.defineProperty(body, 'clientHeight', { configurable: true, value: 100 })
    Object.defineProperty(body, 'scrollHeight', { configurable: true, value: 240 })
    body.scrollTop = 0

    await wrapper.setProps({
      entries: [
        { id: 'reply-1', kind: 'reply', title: '', content: 'first' },
        { id: 'reply-2', kind: 'reply', title: '', content: 'second' },
      ],
    })
    await nextTick()
    await flushPromises()

    expect(body.scrollTop).toBe(140)
    wrapper.unmount()
  })

  it('keeps following streaming updates while the user stays near the bottom', async () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: true,
        entries: [
          { id: 'reply-1', kind: 'reply', title: '', content: 'first' },
        ],
      },
      attachTo: document.body,
    })

    const body = wrapper.find('.messages-body').element as HTMLDivElement
    Object.defineProperty(body, 'clientHeight', { configurable: true, value: 100 })
    Object.defineProperty(body, 'scrollHeight', { configurable: true, value: 240 })

    await nextTick()
    await flushPromises()

    expect(body.scrollTop).toBe(140)

    Object.defineProperty(body, 'scrollHeight', { configurable: true, value: 320 })

    await wrapper.setProps({
      entries: [
        { id: 'reply-1', kind: 'reply', title: '', content: 'first\nsecond' },
      ],
    })
    await nextTick()
    await flushPromises()

    expect(body.scrollTop).toBe(220)
    wrapper.unmount()
  })

  it('stops auto-scrolling after the user scrolls upward and shows a jump button', async () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: true,
        entries: [
          { id: 'reply-1', kind: 'reply', title: '', content: 'first' },
        ],
      },
      attachTo: document.body,
    })

    const body = wrapper.find('.messages-body').element as HTMLDivElement
    Object.defineProperty(body, 'clientHeight', { configurable: true, value: 100 })
    Object.defineProperty(body, 'scrollHeight', { configurable: true, value: 240 })

    await nextTick()
    await flushPromises()

    body.scrollTop = 24
    body.dispatchEvent(new Event('scroll'))

    Object.defineProperty(body, 'scrollHeight', { configurable: true, value: 320 })

    await wrapper.setProps({
      entries: [
        { id: 'reply-1', kind: 'reply', title: '', content: 'first\nsecond' },
      ],
    })
    await nextTick()
    await flushPromises()

    expect(body.scrollTop).toBe(24)
    expect(wrapper.find('.messages-jump-button').exists()).toBe(true)

    await wrapper.find('.messages-jump-button').trigger('click')
    await nextTick()
    await flushPromises()

    expect(body.scrollTop).toBe(220)
    expect(wrapper.find('.messages-jump-button').exists()).toBe(false)
    wrapper.unmount()
  })

  it('renders thinking and grouped tools collapsed by default', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: true,
        entries: [
          {
            id: 'user-1',
            kind: 'user',
            title: 'You',
            content: '请帮我查一下 README',
          },
          {
            id: 'reason-1',
            kind: 'reasoning',
            title: '思考',
            details: [
              {
                label: '思考',
                preview: 'Plan the next steps carefully.',
                collapsed: true,
                loading: true,
                blocks: [{ label: 'Trace', value: 'Plan the next steps carefully.', loading: true }],
              },
            ],
          },
          {
            id: 'tool-1',
            kind: 'tool',
            title: '工具调用 (2)',
            status: 'running',
            details: [
              {
                key: 'call_1',
                label: 'read_file',
                preview: 'Waiting for result...',
                collapsed: true,
                loading: true,
                blocks: [
                  { label: 'Params', value: '{"path":"README.md"}' },
                  { label: 'Result', value: 'Waiting for result...', loading: true },
                ],
              },
              {
                key: 'call_2',
                label: 'glob',
                preview: 'Running',
                collapsed: true,
                loading: true,
                blocks: [{ label: 'Params', value: '{"pattern":"src/**/*.ts"}' }],
              },
            ],
          },
          {
            id: 'reply-1',
            kind: 'reply',
            title: '',
            content: 'Done.',
          },
        ],
      },
    })

    const reasoningDetails = wrapper.findAll('.trace-block.reasoning details.trace-detail')
    const toolGroups = wrapper.findAll('details.trace-tool-group')
    const toolItems = wrapper.findAll('details.trace-tool-item')

    expect(reasoningDetails).toHaveLength(1)
    expect(toolGroups).toHaveLength(1)
    expect(toolItems).toHaveLength(2)
    expect(reasoningDetails[0].attributes('open')).toBeUndefined()
    expect(toolGroups[0].attributes('open')).toBeUndefined()
    expect(toolItems[0].attributes('open')).toBeUndefined()
    expect(wrapper.find('.trace-block.user .trace-header').exists()).toBe(false)
    expect(wrapper.find('.trace-block.reply .trace-header').exists()).toBe(false)
    expect(wrapper.find('.trace-block.user').classes()).toContain('bubble-right')
    expect(wrapper.find('.trace-inline-meta').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('You')
    expect(wrapper.text()).not.toContain('Reply')
    expect(wrapper.find('.trace-tool-group-summary').text()).toContain('工具调用 (2)')
    expect(wrapper.find('.trace-block.reasoning').text()).toContain('思考')
    expect(wrapper.find('.trace-tool-group-summary').text()).toContain('2 running / 0 done')
    expect(wrapper.find('.trace-block.tool .trace-loading').exists()).toBe(true)
    expect(wrapper.find('.trace-block.reply .trace-loading').exists()).toBe(false)
    expect(wrapper.find('.trace-block.tool').classes()).not.toContain('compact-inline')
    expect(wrapper.find('.trace-block.reasoning').classes()).not.toContain('compact-inline')
    expect(wrapper.find('.trace-block.reasoning').classes()).toContain('centered-trace')
    expect(wrapper.find('.trace-block.tool').classes()).toContain('centered-trace')
    expect(wrapper.find('.trace-block.reply').classes()).toContain('centered-trace')
    expect(wrapper.find('.trace-block.reasoning .trace-detail-label').classes()).toContain('loading-marquee')
    expect(wrapper.find('.trace-tool-group-summary .trace-detail-label').classes()).toContain('loading-marquee')
    expect(reasoningDetails[0].classes()).toContain('trace-flat-shell')
    expect(toolGroups[0].classes()).toContain('trace-flat-shell')
    expect(toolItems[0].classes()).toContain('trace-flat-shell')
    expect(wrapper.find('.trace-block.reasoning .trace-detail-block-header').exists()).toBe(false)
    expect(wrapper.find('.trace-block.reasoning').text()).not.toContain('Trace')
    expect(wrapper.find('.trace-block.reasoning .trace-loading.small').exists()).toBe(false)
    expect(wrapper.find('.trace-block.reasoning .trace-kind-badge.reasoning').exists()).toBe(true)
    expect(wrapper.find('.trace-block.tool .trace-kind-badge.tool.operation-badge svg').exists()).toBe(true)
  })

  it('renders finish token stats at the end of assistant reply', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'reply-1',
            kind: 'reply',
            title: '',
            content: 'Done.',
            provider_id: 'openai',
            model_id: 'gpt-5.4',
            token_usage: {
              prompt_tokens: 123,
              completion_tokens: 45,
              total_tokens: 168,
            },
          } as any,
        ],
      },
    })

    const usage = wrapper.find('.trace-reply-usage')
    expect(usage.exists()).toBe(true)
    expect(usage.text()).toContain('openai / gpt-5.4')
    expect(usage.text()).toContain('Token')
    expect(usage.text()).toContain('123')
    expect(usage.text()).toContain('45')
    expect(usage.text()).toContain('168')
  })

  it('centers non-user error messages in the shared message column', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'error-1',
            kind: 'error',
            title: '运行失败',
            content: 'boom',
          } as any,
        ],
      },
    })

    expect(wrapper.find('.trace-block.error').classes()).toContain('centered-trace')
    expect(wrapper.find('.trace-error-detail').classes()).toContain('trace-flat-shell')
    expect(wrapper.find('.trace-error-detail .trace-detail-label').text()).toBe('运行失败')
    expect(wrapper.find('.trace-block.error .trace-kind-badge.error').exists()).toBe(true)
  })

  it('shows a copy button before reply token stats', async () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'reply-1',
            kind: 'reply',
            title: '',
            content: 'Done.',
            token_usage: {
              prompt_tokens: 123,
              completion_tokens: 45,
              total_tokens: 168,
            },
          } as any,
        ],
      },
    })

    const footer = wrapper.find('.trace-reply-footer')
    const copyButton = wrapper.find('.trace-copy-button')

    expect(footer.exists()).toBe(true)
    expect(copyButton.exists()).toBe(true)
    expect(copyButton.attributes('aria-label')).toBe('复制消息')
    expect(copyButton.text()).toBe('')
    expect(copyButton.find('svg').exists()).toBe(true)
    expect(copyButton.classes()).not.toContain('ghost-button')
    expect(copyButton.find('.trace-copy-toast-anchor').exists()).toBe(true)
    expect(footer.element.firstElementChild).toBe(copyButton.element)
    expect(footer.text()).toContain('Token')

    await copyButton.trigger('click')

    expect(globalThis.navigator.clipboard.writeText).toHaveBeenCalledWith('Done.')
  })

  it('shows and auto-hides a subtle copy toast after copying', async () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'reply-1',
            kind: 'reply',
            title: '',
            content: 'Done.',
          } as any,
        ],
      },
    })

    expect(wrapper.find('.trace-copy-toast').exists()).toBe(false)

    await wrapper.find('.trace-copy-button').trigger('click')
    await flushPromises()

    const toast = wrapper.find('.trace-copy-toast')
    expect(toast.exists()).toBe(true)
    expect(toast.text()).toContain('已复制')
    expect(toast.classes()).toContain('success')
    expect(toast.find('svg').exists()).toBe(true)
    expect(wrapper.find('.trace-copy-button .trace-copy-toast-anchor .trace-copy-toast').exists()).toBe(true)

    vi.advanceTimersByTime(1800)
    await flushPromises()

    expect(wrapper.find('.trace-copy-toast').exists()).toBe(false)
  })

  it('shows a failure toast when clipboard copy fails', async () => {
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: vi.fn().mockRejectedValue(new Error('denied')),
      },
    })

    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'reply-1',
            kind: 'reply',
            title: '',
            content: 'Done.',
          } as any,
        ],
      },
    })

    await wrapper.find('.trace-copy-button').trigger('click')
    await flushPromises()

    const toast = wrapper.find('.trace-copy-toast')
    expect(toast.exists()).toBe(true)
    expect(toast.text()).toContain('复制失败')
    expect(toast.classes()).toContain('error')
    expect(toast.find('svg').exists()).toBe(true)

    vi.advanceTimersByTime(1800)
    await flushPromises()

    expect(wrapper.find('.trace-copy-toast').exists()).toBe(false)
  })

  it('renders assistant replies as markdown content', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'reply-markdown',
            kind: 'reply',
            title: '',
            content: '## Plan\n\nUse **markdown** and `code`.',
          },
        ],
      },
    })

    const content = wrapper.find('.trace-block.reply .trace-content.markdown-content')
    expect(content.exists()).toBe(true)
    expect(content.find('h2').text()).toBe('Plan')
    expect(content.find('strong').text()).toBe('markdown')
    expect(content.find('code').text()).toBe('code')
  })

  it('renders fenced code blocks with a copy button', async () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'reply-code',
            kind: 'reply',
            title: '',
            content: '```ts\nconst total = 42\n```',
          },
        ],
      },
    })

    const codeBlock = wrapper.find('.markdown-code-block')
    const copyButton = wrapper.find('.markdown-code-copy')

    expect(codeBlock.exists()).toBe(true)
    expect(copyButton.exists()).toBe(true)
    expect(copyButton.attributes('aria-label')).toBe('复制代码块')
    expect(copyButton.text()).toBe('')
    expect(copyButton.find('svg').exists()).toBe(true)
    expect(copyButton.classes()).toContain('compact-icon-button')
    expect(wrapper.find('.markdown-code-language').text()).toBe('ts')
    expect(codeBlock.find('pre').exists()).toBe(true)

    await copyButton.trigger('click')
    await flushPromises()

    expect(globalThis.navigator.clipboard.writeText).toHaveBeenCalledWith('const total = 42\n')
  })

  it('falls back to code when fenced block has no language label', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'reply-code-fallback',
            kind: 'reply',
            title: '',
            content: '```\nplain text\n```',
          },
        ],
      },
    })

    expect(wrapper.find('.markdown-code-language').text()).toBe('code')
  })

  it('shows 单个工具调用标题 while preserving the tool name', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'tool-single',
            kind: 'tool',
            title: '工具调用',
            status: 'done',
            details: [
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
            ],
          },
        ],
      },
    })

    const summary = wrapper.find('.trace-detail-summary')
    expect(summary.text()).toContain('工具调用')
    expect(summary.text()).toContain('read_file')
  })

  it('renders a warning-styled inline approval card with Chinese approval actions', async () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'approval-entry-1',
            kind: 'approval',
            title: '等待审批',
            approval: {
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
            },
          } as any,
        ],
      },
    })

    expect(wrapper.find('.approval-card').exists()).toBe(true)
    expect(wrapper.find('.approval-card').classes()).toContain('chat-approval-card')
    expect(wrapper.find('.approval-card .trace-kind-badge').classes()).toContain('approval')
    expect(wrapper.text()).toContain('bash')
    expect(wrapper.text()).toContain('high')
    expect(wrapper.text()).toContain('dangerous filesystem mutation')
    expect(wrapper.text()).toContain('rm -rf /tmp/demo')
    expect(wrapper.text()).toContain('等待审批')
    expect(wrapper.text()).toContain('待处理')
    expect(wrapper.text()).toContain('风险等级')
    expect(wrapper.text()).toContain('审批原因')
    expect(wrapper.text()).toContain('调用参数')
    expect(wrapper.text()).toContain('审批说明')
    expect(wrapper.find('[data-approval-action="approve"]').exists()).toBe(true)
    expect(wrapper.find('[data-approval-action="reject"]').exists()).toBe(true)
    expect(wrapper.find('[data-approval-action="approve"]').text()).toBe('同意执行')
    expect(wrapper.find('[data-approval-action="reject"]').text()).toBe('拒绝执行')
    expect(wrapper.find('[data-approval-action="approve"]').classes()).toContain('approval-action-approve')
    expect(wrapper.find('[data-approval-action="reject"]').classes()).toContain('approval-action-reject')
    expect(wrapper.find('.approval-reason-input').attributes('placeholder')).toBe('可选，补充审批说明')

    await wrapper.find('.approval-reason-input').setValue('checked')
    await wrapper.find('[data-approval-action="approve"]').trigger('click')

    expect(wrapper.emitted('approval-decision')).toEqual([
      [{ taskId: 'task_1', approvalId: 'approval_1', decision: 'approve', reason: 'checked' }],
    ])
  })

  it('emits a reject decision from the inline approval card with the optional note', async () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'approval-entry-2',
            kind: 'approval',
            title: '等待审批',
            approval: {
              id: 'approval_2',
              task_id: 'task_2',
              conversation_id: 'conv_2',
              step_index: 5,
              tool_call_id: 'call_2',
              tool_name: 'delete_file',
              arguments_summary: '{"path":"danger.txt"}',
              risk_level: 'high',
              reason: 'dangerous file mutation',
              status: 'pending',
            },
          } as any,
        ],
      },
    })

    await wrapper.find('.approval-reason-input').setValue('reject this')
    await wrapper.find('[data-approval-action="reject"]').trigger('click')

    expect(wrapper.emitted('approval-decision')).toEqual([
      [{ taskId: 'task_2', approvalId: 'approval_2', decision: 'reject', reason: 'reject this' }],
    ])
  })

  it('prevents duplicate or conflicting approval clicks while a decision is in flight', async () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'approval-entry-4',
            kind: 'approval',
            title: '等待审批',
            approval: {
              id: 'approval_4',
              task_id: 'task_4',
              conversation_id: 'conv_4',
              step_index: 6,
              tool_call_id: 'call_4',
              tool_name: 'bash',
              arguments_summary: 'kill 1234',
              risk_level: 'high',
              reason: 'process termination',
              status: 'pending',
            },
          } as any,
        ],
        approvalDecisionStateById: {
          approval_4: { pending: true, decision: 'approve' },
        },
      },
    })

    expect(wrapper.get('.approval-reason-input').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-approval-action="approve"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-approval-action="reject"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-approval-action="approve"]').trigger('click')
    await wrapper.get('[data-approval-action="reject"]').trigger('click')

    expect(wrapper.emitted('approval-decision')).toBeUndefined()
  })

  it('renders waiting approval entries without the error shell', () => {
    const wrapper = mount(MessageList, {
      props: {
        loading: false,
        entries: [
          {
            id: 'approval-entry-3',
            kind: 'approval',
            title: '等待审批',
            approval: {
              id: 'approval_3',
              task_id: 'task_3',
              conversation_id: 'conv_3',
              step_index: 6,
              tool_call_id: 'call_3',
              tool_name: 'bash',
              arguments_summary: 'kill 1234',
              risk_level: 'high',
              reason: 'process termination',
              status: 'pending',
            },
          } as any,
        ],
      },
    })

    expect(wrapper.find('.approval-card').exists()).toBe(true)
    expect(wrapper.find('.trace-error-detail').exists()).toBe(false)
    expect(wrapper.find('.trace-block.error').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('运行失败')
  })
})
