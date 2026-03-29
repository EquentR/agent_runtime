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

  it('switches the submit button into a stop button while busy and emits stop', async () => {
    const wrapper = mount(MessageComposer, {
      props: {
        disabled: false,
        busy: true,
      },
    })

    expect(wrapper.find('.composer-submit').attributes('aria-label')).toBe('停止')

    await wrapper.find('form').trigger('submit.prevent')

    expect(wrapper.emitted('stop')).toEqual([[]])
    expect(wrapper.emitted('send')).toBeUndefined()
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

  it('starts at two rows and grows with content', async () => {
    const wrapper = mount(MessageComposer, {
      props: {
        disabled: false,
      },
      attachTo: document.body,
    })

    const textarea = wrapper.find('textarea')
    const element = textarea.element as HTMLTextAreaElement

    Object.defineProperty(element, 'scrollHeight', {
      configurable: true,
      get: () => (element.value.includes('\n') ? 120 : 56),
    })

    await textarea.setValue('first line')
    await textarea.trigger('input')

    expect(textarea.attributes('rows')).toBe('2')
    expect(element.style.height).toBe('64px')

    await textarea.setValue('first line\nsecond line\nthird line')
    await textarea.trigger('input')

    expect(element.style.height).toBe('120px')
  })
})
