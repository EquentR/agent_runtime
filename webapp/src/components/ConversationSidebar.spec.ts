import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import ConversationSidebar from './ConversationSidebar.vue'

describe('ConversationSidebar', () => {
  it('renders only one-line titles without preview metadata rows', () => {
    const wrapper = mount(ConversationSidebar, {
      props: {
        activeConversationId: 'conv_1',
        loading: false,
        username: 'demo-user',
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
    expect(wrapper.find('[aria-label="新建对话"]').exists()).toBe(true)
    expect(wrapper.find('.sidebar-account-name').text()).toContain('demo-user')
    expect(wrapper.find('.sidebar-account-logout').exists()).toBe(true)
    expect(wrapper.find('.sidebar-account-logout').classes()).toContain('icon-button')
    expect(wrapper.find('.sidebar-account-logout').text()).toBe('')
  })

  it('supports collapsing workspace and uses inline delete confirmation', async () => {
    const wrapper = mount(ConversationSidebar, {
      props: {
        activeConversationId: 'conv_1',
        collapsed: false,
        loading: false,
        username: 'demo-user',
        conversations: [
          {
            id: 'conv_1',
            title: 'First chat',
            last_message: 'hello',
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

    await wrapper.find('.sidebar-toggle').trigger('click')
    expect(wrapper.emitted('toggle-collapse')).toHaveLength(1)

    await wrapper.find('.conversation-delete-button').trigger('click')
    expect(wrapper.find('.conversation-delete-confirm').exists()).toBe(true)
    expect(wrapper.emitted('delete')).toBeUndefined()

    await wrapper.find('.conversation-delete-cancel').trigger('click')
    expect(wrapper.find('.conversation-delete-confirm').exists()).toBe(false)

    await wrapper.find('.conversation-delete-button').trigger('click')
    await wrapper.find('.conversation-delete-confirm-button').trigger('click')
    expect(wrapper.emitted('delete')).toEqual([['conv_1']])
  })

  it('uses inline confirmation before logout from the sidebar account area', async () => {
    const wrapper = mount(ConversationSidebar, {
      props: {
        activeConversationId: '',
        loading: false,
        username: 'demo-user',
        conversations: [],
      },
    })

    await wrapper.find('.sidebar-account-logout').trigger('click')
    expect(wrapper.find('.sidebar-logout-confirm').exists()).toBe(true)
    expect(wrapper.emitted('logout')).toBeUndefined()

    await wrapper.find('.sidebar-logout-cancel').trigger('click')
    expect(wrapper.find('.sidebar-logout-confirm').exists()).toBe(false)

    await wrapper.find('.sidebar-account-logout').trigger('click')
    await wrapper.find('.sidebar-logout-confirm-button').trigger('click')

    expect(wrapper.emitted('logout')).toHaveLength(1)
  })
})
