import { flushPromises, mount } from '@vue/test-utils'
import type { Component } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  changeUserPassword: vi.fn(),
  confirmUserEmailVerification: vi.fn(),
  fetchUserProfile: vi.fn(),
  startUserEmailVerification: vi.fn(),
  updateUserProfile: vi.fn(),
}))

vi.mock('../lib/api', () => api)

function buildProfile(overrides: Record<string, unknown> = {}) {
  return {
    id: 7,
    username: 'legacy',
    email: '',
    display_name: 'Legacy User',
    role: 'user',
    status: 'needs_email_binding',
    email_verified: false,
    force_password_change: true,
    required_actions: ['bind_email', 'change_password'],
    ...overrides,
  }
}

async function loadProfileView() {
  const modules = import.meta.glob('./ProfileView.vue')
  const loader = modules['./ProfileView.vue']
  expect(loader).toBeTypeOf('function')

  const module = (await loader()) as { default: Component }
  return module.default
}

async function mountProfileView() {
  const ProfileView = await loadProfileView()
  return mount(ProfileView, {
    global: {
      stubs: {
        RouterLink: {
          props: ['to'],
          template: '<a :href="typeof to === \'string\' ? to : to.path"><slot /></a>',
        },
      },
    },
  })
}

describe('ProfileView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.fetchUserProfile.mockResolvedValue(buildProfile())
    api.updateUserProfile.mockResolvedValue(buildProfile({ display_name: 'Alice Doe' }))
    api.changeUserPassword.mockResolvedValue(buildProfile({
      force_password_change: false,
      required_actions: ['bind_email'],
    }))
    api.startUserEmailVerification.mockResolvedValue({ sent: true })
    api.confirmUserEmailVerification.mockResolvedValue(buildProfile({
      email: 'bound@example.com',
      status: 'active',
      email_verified: true,
      force_password_change: false,
      required_actions: [],
    }))
  })

  it('updates display name and password while showing required action states', async () => {
    const wrapper = await mountProfileView()
    await flushPromises()

    expect(api.fetchUserProfile).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('必须绑定邮箱')
    expect(wrapper.text()).toContain('必须修改密码')
    expect(wrapper.get('[data-profile-models-link]').attributes('href')).toBe('/profile/models')

    await wrapper.get('[data-profile-display-name-input]').setValue(' Alice Doe ')
    await wrapper.get('[data-profile-form]').trigger('submit')
    await flushPromises()

    expect(api.updateUserProfile).toHaveBeenCalledWith({ display_name: 'Alice Doe' })

    await wrapper.get('[data-profile-current-password-input]').setValue('old-secret')
    await wrapper.get('[data-profile-new-password-input]').setValue('new-secret-123')
    await wrapper.get('[data-profile-confirm-password-input]').setValue('new-secret-123')
    await wrapper.get('[data-profile-password-form]').trigger('submit')
    await flushPromises()

    expect(api.changeUserPassword).toHaveBeenCalledWith({
      current_password: 'old-secret',
      password: 'new-secret-123',
      confirm_password: 'new-secret-123',
    })
  })

  it('starts email binding verification and confirms the code', async () => {
    const wrapper = await mountProfileView()
    await flushPromises()

    await wrapper.get('[data-profile-email-input]').setValue(' bound@example.com ')
    await wrapper.get('[data-profile-email-start-form]').trigger('submit')
    await flushPromises()

    expect(api.startUserEmailVerification).toHaveBeenCalledWith({ email: 'bound@example.com' })

    await wrapper.get('[data-profile-email-code-input]').setValue('123456')
    await wrapper.get('[data-profile-email-confirm-form]').trigger('submit')
    await flushPromises()

    expect(api.confirmUserEmailVerification).toHaveBeenCalledWith({
      email: 'bound@example.com',
      code: '123456',
    })
    expect(wrapper.text()).toContain('bound@example.com')
  })
})
