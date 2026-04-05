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
          ElSelect: true,
          ElOption: true,
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
      global: {
        stubs: { ElSelect: true, ElOption: true },
      },
      props: {
        disabled: false,
      },
    })

    expect(wrapper.find('.composer-approval-entry').exists()).toBe(false)
  })

  it('places the send button in the composer toolbar', () => {
    expect(chatStyles).toMatch(/\.composer-submit\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(chatStyles).toMatch(/\.composer-toolbar\s*\{[\s\S]*?display:\s*flex;/)
  })

  it('hides the textarea vertical scrollbar while auto-resizing', () => {
    expect(chatStyles).toMatch(/\.composer-input\s*\{[\s\S]*?overflow-y:\s*hidden;/)
  })

  it('switches the submit button into a stop button while busy and emits stop', async () => {
    const wrapper = mount(MessageComposer, {
      global: {
        stubs: {
          RouterLink: routerLinkStub,
          ElSelect: true,
          ElOption: true,
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
          ElSelect: true,
          ElOption: true,
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

  it('auto-resizes textarea up to 4-line max height', async () => {
    const wrapper = mount(MessageComposer, {
      global: {
        stubs: {
          RouterLink: routerLinkStub,
          ElSelect: true,
          ElOption: true,
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
      get: () => (element.value.includes('\n') ? 120 : 24),
    })

    await textarea.setValue('first line')
    await textarea.trigger('input')

    expect(textarea.attributes('rows')).toBe('1')
    expect(element.style.height).toBe('24px')

    await textarea.setValue('first line\nsecond line\nthird line')
    await textarea.trigger('input')

    /* 120px > maxTextareaHeight (96px), so it should be capped at 96px with overflow-y: auto */
    expect(element.style.height).toBe('96px')
    expect(element.style.overflowY).toBe('auto')
  })

  it('renders the unified card container layout', () => {
    const wrapper = mount(MessageComposer, {
      global: {
        stubs: { ElSelect: true, ElOption: true },
      },
      props: {
        disabled: false,
      },
    })

    expect(wrapper.find('.composer-card').exists()).toBe(true)
    expect(wrapper.find('.composer-textarea-wrapper').exists()).toBe(true)
    expect(wrapper.find('.composer-toolbar').exists()).toBe(true)
    expect(wrapper.find('.composer-attach-btn').exists()).toBe(true)
  })

  it('renders skills selector when skills are provided', () => {
    const wrapper = mount(MessageComposer, {
      global: {
        stubs: { ElSelect: true, ElOption: true },
      },
      props: {
        disabled: false,
        skills: [
          { name: 'test-skill', title: 'Test Skill', source_ref: 'test' },
        ],
        selectedSkillNames: [],
      },
    })

    expect(wrapper.find('.composer-skill-select').exists()).toBe(true)
  })

  it('does not render skills selector when no skills are available', () => {
    const wrapper = mount(MessageComposer, {
      global: {
        stubs: { ElSelect: true, ElOption: true },
      },
      props: {
        disabled: false,
        skills: [],
        selectedSkillNames: [],
      },
    })

    expect(wrapper.find('.composer-skill-select').exists()).toBe(false)
  })
})
