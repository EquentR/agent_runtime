import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import ConversationSidebar from './ConversationSidebar.vue'

describe('ConversationSidebar', () => {
  it('renders only one-line titles without preview metadata rows', () => {
    const wrapper = mount(ConversationSidebar, {
      props: {
        activeConversationId: 'conv_1',
        loading: false,
        conversations: [
          {
            id: 'conv_1',
            title: 'A very long conversation title that should truncate visually',
            last_message: 'this preview should not render',
            message_count: 2,
            provider_id: 'openai',
            model_id: 'gpt-5.4',
            created_by: 'demo',
            created_at: '',
            updated_at: '',
          },
        ],
      },
    })

    expect(wrapper.find('.conversation-title').exists()).toBe(true)
    expect(wrapper.find('.conversation-title').classes()).toContain('truncate-text')
    expect(wrapper.find('.conversation-title').attributes('title')).toContain('A very long conversation title')
    expect(wrapper.find('.conversation-preview').exists()).toBe(false)
    expect(wrapper.find('.conversation-meta').exists()).toBe(false)
    expect(wrapper.find('.sidebar-list').exists()).toBe(true)
    expect(wrapper.find('.ghost-button').text()).toBe('+')
  })
})
