import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import MessageComposer from './MessageComposer.vue'

const chatStyles = readFileSync(resolve(process.cwd(), 'src/style.css'), 'utf8')

const routerLinkStub = {
  props: ['to'],
  template: '<a class="composer-approval-entry" :href="to"><slot /></a>',
}

describe('MessageComposer', () => {
  it('renders an icon send button inside the composer', () => {
    const wrapper = mount(MessageComposer, {
      global: {
        stubs: {
          RouterLink: routerLinkStub,
        },
      },
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

  it('does not render a separate approval entry button next to the send action', () => {
    const wrapper = mount(MessageComposer, {
      props: {
        disabled: false,
      },
    })

    expect(wrapper.find('.composer-approval-entry').exists()).toBe(false)
  })

  it('keeps the send button pinned to the bottom-right corner of the textarea', () => {
    expect(chatStyles).toMatch(/\.composer-submit\s*\{[\s\S]*?position:\s*absolute;/)
    expect(chatStyles).toMatch(/\.composer-submit\s*\{[\s\S]*?right:\s*0\.72rem;/)
    expect(chatStyles).toMatch(/\.composer-submit\s*\{[\s\S]*?bottom:\s*0\.72rem;/)
  })

  it('hides the textarea vertical scrollbar while auto-resizing', () => {
    expect(chatStyles).toMatch(/\.composer-input\s*\{[\s\S]*?overflow-y:\s*hidden;/)
  })

  it('switches the submit button into a stop button while busy and emits stop', async () => {
    const wrapper = mount(MessageComposer, {
      global: {
        stubs: {
          RouterLink: routerLinkStub,
        },
      },
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
      global: {
        stubs: {
          RouterLink: routerLinkStub,
        },
      },
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

  it('adds half a line of default height before growing with content', async () => {
    const wrapper = mount(MessageComposer, {
      global: {
        stubs: {
          RouterLink: routerLinkStub,
        },
      },
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
    expect(element.style.height).toBe('72px')

    await textarea.setValue('first line\nsecond line\nthird line')
    await textarea.trigger('input')

    expect(element.style.height).toBe('120px')
  })
})
