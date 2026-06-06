import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import ConversationSidebar from './ConversationSidebar.vue'
import { THEME_STORAGE_KEY } from '../lib/theme'

describe('ConversationSidebar', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('theme-teal')
    document.documentElement.classList.remove('theme-teal-dark')
  })

  afterEach(() => {
    document.documentElement.classList.remove('theme-teal')
    document.documentElement.classList.remove('theme-teal-dark')
    document.body.querySelectorAll('.sidebar-confirm-overlay, .sidebar-user-menu-panel').forEach((node) => node.remove())
  })

  it('cycles the sidebar theme button through all shared theme states', async () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'default')

    const wrapper = mount(ConversationSidebar, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to"><slot /></a>',
          },
        },
      },
      props: {
        activeConversationId: '',
        loading: false,
        username: 'demo-user',
        conversations: [],
      },
    })

    const themeButton = wrapper.find('.sidebar-theme-toggle')

    expect(themeButton.attributes('aria-label')).toBe('当前主题：默认，切换到 Teal 主题')
    expect(themeButton.attributes('title')).toBe('当前主题：默认，切换到 Teal 主题')
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('default')
    expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
    expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(false)

    await themeButton.trigger('click')
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('teal')
    expect(document.documentElement.classList.contains('theme-teal')).toBe(true)
    expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(false)
    expect(themeButton.attributes('aria-label')).toBe('当前主题：Teal，切换到 Teal Dark 主题')
    expect(themeButton.attributes('title')).toBe('当前主题：Teal，切换到 Teal Dark 主题')

    await themeButton.trigger('click')
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('teal-dark')
    expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
    expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(true)
    expect(themeButton.attributes('aria-label')).toBe('当前主题：Teal Dark，切换到默认主题')
    expect(themeButton.attributes('title')).toBe('当前主题：Teal Dark，切换到默认主题')

    await themeButton.trigger('click')
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('default')
    expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
    expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(false)
    expect(themeButton.attributes('aria-label')).toBe('当前主题：默认，切换到 Teal 主题')
    expect(themeButton.attributes('title')).toBe('当前主题：默认，切换到 Teal 主题')
  })

  it('renders only one-line titles without preview metadata rows', () => {
    const wrapper = mount(ConversationSidebar, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to"><slot /></a>',
          },
        },
      },
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
    expect(wrapper.find('.sidebar-user-menu-trigger').exists()).toBe(true)
  })

  it('uses non-nested actions for selection and delete controls', async () => {
    const wrapper = mount(ConversationSidebar, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to"><slot /></a>',
          },
        },
      },
      props: {
        activeConversationId: 'conv_1',
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

    const conversationItem = wrapper.find('.conversation-list-item')
    expect(conversationItem.exists()).toBe(true)
    expect(conversationItem.findAll('button')).toHaveLength(2)

    await wrapper.find('.conversation-card').trigger('click')
    expect(wrapper.emitted('select')).toEqual([['conv_1']])

    await wrapper.find('.conversation-delete-button').trigger('click')
    expect(wrapper.emitted('select')).toEqual([['conv_1']])
    expect(document.body.querySelector('.sidebar-confirm-overlay')).not.toBeNull()

    await document.body.querySelector('.sidebar-confirm-confirm')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(wrapper.emitted('delete')).toEqual([['conv_1']])
  })

  it('supports collapsing workspace and uses fullscreen delete confirmation', async () => {
    const wrapper = mount(ConversationSidebar, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to"><slot /></a>',
          },
        },
      },
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
    expect(document.body.querySelector('.sidebar-confirm-overlay')).not.toBeNull()
    expect(document.body.querySelector('.sidebar-confirm-dialog')?.textContent).toContain('确认删除这个对话？')
    expect(wrapper.emitted('delete')).toBeUndefined()

    await document.body.querySelector('.sidebar-confirm-cancel')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(document.body.querySelector('.sidebar-confirm-overlay')).toBeNull()

    await wrapper.find('.conversation-delete-button').trigger('click')
    await document.body.querySelector('.sidebar-confirm-confirm')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(wrapper.emitted('delete')).toEqual([['conv_1']])
  })

  it('shows a floating user menu and uses fullscreen confirmation before logout', async () => {
    const wrapper = mount(ConversationSidebar, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to"><slot /></a>',
          },
        },
      },
      props: {
        activeConversationId: '',
        loading: false,
        username: 'demo-user',
        conversations: [],
        isAdmin: true,
      },
    })

    expect(wrapper.find('.sidebar-user-menu-panel').exists()).toBe(false)
    expect(wrapper.find('.sidebar-user-menu-anchor').exists()).toBe(true)

    await wrapper.find('.sidebar-user-menu-trigger').trigger('click')
    await nextTick()
    const bodyMenu = document.body.querySelector('.sidebar-user-menu-panel')
    expect(bodyMenu).not.toBeNull()
    expect(bodyMenu?.classList.contains('upward')).toBe(true)
    const adminLinks = Array.from(document.body.querySelectorAll('.sidebar-admin-link'))
    expect(adminLinks).toHaveLength(2)
    expect(adminLinks.map((link) => link.getAttribute('href'))).toEqual(['/admin/audit', '/admin/prompts'])
    expect(adminLinks.map((link) => link.textContent ?? '')).toEqual(
      expect.arrayContaining(['审计', '提示词管理']),
    )
    expect(document.body.querySelector('.sidebar-user-menu-logout')).not.toBeNull()
    expect(wrapper.find('.sidebar-user-menu-trigger-caret').exists()).toBe(false)
    expect(bodyMenu?.matches('::after')).toBe(false)

    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.sidebar-user-menu-panel').exists()).toBe(false)

    await wrapper.find('.sidebar-user-menu-trigger').trigger('click')

    await document.body.querySelector('.sidebar-user-menu-logout')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(document.body.querySelector('.sidebar-confirm-overlay')).not.toBeNull()
    expect(document.body.querySelector('.sidebar-confirm-dialog')?.textContent).toContain('确认退出登录？')
    expect(wrapper.emitted('logout')).toBeUndefined()

    await document.body.querySelector('.sidebar-confirm-cancel')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(document.body.querySelector('.sidebar-confirm-overlay')).toBeNull()

    await wrapper.find('.sidebar-user-menu-trigger').trigger('click')
    await document.body.querySelector('.sidebar-user-menu-logout')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await wrapper.vm.$nextTick()
    await document.body.querySelector('.sidebar-confirm-confirm')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))

    expect(wrapper.emitted('logout')).toHaveLength(1)
  })

  it('shows profile settings in the user menu for regular users', async () => {
    const wrapper = mount(ConversationSidebar, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to"><slot /></a>',
          },
        },
      },
      props: {
        activeConversationId: '',
        loading: false,
        username: 'demo-user',
        conversations: [],
        isAdmin: false,
      },
    })

    await wrapper.find('.sidebar-user-menu-trigger').trigger('click')
    await nextTick()

    const profileButton = document.body.querySelector('.sidebar-profile-link') as HTMLButtonElement | null
    expect(profileButton).not.toBeNull()
    expect(profileButton?.textContent).toContain('个人设置')

    profileButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()

    expect(wrapper.emitted('open-profile')).toHaveLength(1)
  })

  it('does not show approval management in the user menu', async () => {
    const wrapper = mount(ConversationSidebar, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to"><slot /></a>',
          },
        },
      },
      props: {
        activeConversationId: '',
        loading: false,
        username: 'demo-user',
        conversations: [],
        isAdmin: true,
      },
    })

    await wrapper.find('.sidebar-user-menu-trigger').trigger('click')
    await nextTick()

    const adminLinks = Array.from(document.body.querySelectorAll('.sidebar-admin-link'))
    expect(adminLinks.map((link) => link.textContent ?? '')).not.toContain('审批管理')
  })
})
