import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { nextTick } from 'vue'

import MessageList from './MessageList.vue'

describe('MessageList', () => {
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
    expect(wrapper.find('.trace-block.tool').classes()).toContain('compact-inline')
    expect(wrapper.find('.trace-block.reasoning').classes()).toContain('compact-inline')
    expect(wrapper.find('.trace-block.reasoning .trace-detail-label').classes()).toContain('loading-marquee')
    expect(wrapper.find('.trace-tool-group-summary .trace-detail-label').classes()).toContain('loading-marquee')
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
})
