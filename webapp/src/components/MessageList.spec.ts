import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

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
})
