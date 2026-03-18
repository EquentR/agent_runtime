import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import MessageComposer from './MessageComposer.vue'

describe('MessageComposer', () => {
  it('renders an icon send button inside the composer', () => {
    const wrapper = mount(MessageComposer, {
      props: {
        disabled: false,
      },
    })

    expect(wrapper.text()).not.toContain('Send message')
    expect(wrapper.text()).not.toContain('Messages are sent to the existing `agent.run` task API.')
    expect(wrapper.find('.composer-submit').attributes('aria-label')).toBe('发送')
    expect(wrapper.find('.composer-submit').text()).toBe('')
    expect(wrapper.find('.composer-submit svg').exists()).toBe(true)
  })

  it('submits on Enter and preserves newline on Shift+Enter', async () => {
    const wrapper = mount(MessageComposer, {
      props: {
        disabled: false,
      },
    })

    const textarea = wrapper.find('textarea')
    await textarea.setValue('hello world')
    await textarea.trigger('keydown', { key: 'Enter', shiftKey: false })

    expect(wrapper.emitted('send')).toEqual([['hello world']])

    await textarea.setValue('line one')
    await textarea.trigger('keydown', { key: 'Enter', shiftKey: true })

    expect(wrapper.emitted('send')).toHaveLength(1)
    expect((textarea.element as HTMLTextAreaElement).value).toBe('line one')
  })
})
