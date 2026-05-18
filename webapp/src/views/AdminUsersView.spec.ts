import { flushPromises, mount } from '@vue/test-utils'
import type { Component } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  fetchAdminUsers: vi.fn(),
  resetAdminUserPassword: vi.fn(),
  updateAdminUser: vi.fn(),
}))

vi.mock('../lib/api', () => api)

function buildUser(overrides: Record<string, unknown> = {}) {
  return {
    id: 2,
    username: 'alice',
    email: 'alice@example.com',
    display_name: 'Alice',
    role: 'user',
    status: 'active',
    email_verified: true,
    force_password_change: false,
    required_actions: [],
    ...overrides,
  }
}

async function loadAdminUsersView() {
  const modules = import.meta.glob('./AdminUsersView.vue')
  const loader = modules['./AdminUsersView.vue']
  expect(loader).toBeTypeOf('function')

  const module = (await loader()) as { default: Component }
  return module.default
}

describe('AdminUsersView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.fetchAdminUsers.mockResolvedValue([
      buildUser(),
      buildUser({
        id: 3,
        username: 'bob',
        email: 'bob@example.com',
        display_name: 'Bob',
        status: 'disabled',
      }),
    ])
    api.updateAdminUser.mockResolvedValue(buildUser({
      role: 'admin',
      status: 'active',
      email_verified: false,
    }))
    api.resetAdminUserPassword.mockResolvedValue(buildUser({ force_password_change: true }))
  })

  it('filters users and updates role status email verification', async () => {
    const AdminUsersView = await loadAdminUsersView()
    const wrapper = mount(AdminUsersView)
    await flushPromises()

    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('bob')

    await wrapper.get('[data-user-search-input]').setValue('alice')
    await wrapper.get('[data-user-status-filter]').setValue('active')
    await wrapper.get('[data-user-search-form]').trigger('submit')
    await flushPromises()

    expect(api.fetchAdminUsers).toHaveBeenLastCalledWith({
      q: 'alice',
      role: '',
      status: 'active',
    })

    await wrapper.get('[data-user-row="2"]').trigger('click')
    await wrapper.get('[data-user-role-select]').setValue('admin')
    await wrapper.get('[data-user-status-select]').setValue('disabled')
    await wrapper.get('[data-user-email-verified-input]').setValue(false)
    await wrapper.get('[data-user-detail-form]').trigger('submit')
    await flushPromises()

    expect(api.updateAdminUser).toHaveBeenCalledWith(2, expect.objectContaining({
      role: 'admin',
      status: 'disabled',
      email_verified: false,
    }))

    await wrapper.get('[data-user-password-reset-input]').setValue('temporary-123')
    await wrapper.get('[data-user-password-reset-form]').trigger('submit')
    await flushPromises()

    expect(api.resetAdminUserPassword).toHaveBeenCalledWith(2, { password: 'temporary-123' })
  })

  it('clears the selected user when filters remove it from the result list', async () => {
    const AdminUsersView = await loadAdminUsersView()
    const wrapper = mount(AdminUsersView)
    await flushPromises()

    await wrapper.get('[data-user-row="3"]').trigger('click')
    expect(wrapper.text()).toContain('bob@example.com')

    api.fetchAdminUsers.mockResolvedValueOnce([buildUser()])
    await wrapper.get('[data-user-search-input]').setValue('alice')
    await wrapper.get('[data-user-search-form]').trigger('submit')
    await flushPromises()

    expect(wrapper.find('[data-user-row="3"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('从左侧选择一个用户。')
    expect(wrapper.text()).not.toContain('bob@example.com')
  })

  it('omits an empty email when updating legacy users without bound email', async () => {
    api.fetchAdminUsers.mockResolvedValueOnce([buildUser({
      id: 4,
      username: 'legacy',
      email: '',
      display_name: 'Legacy',
      status: 'needs_email_binding',
      email_verified: false,
    })])

    const AdminUsersView = await loadAdminUsersView()
    const wrapper = mount(AdminUsersView)
    await flushPromises()

    await wrapper.get('[data-user-row="4"]').trigger('click')
    await wrapper.get('[data-user-status-select]').setValue('disabled')
    await wrapper.get('[data-user-detail-form]').trigger('submit')
    await flushPromises()

    expect(api.updateAdminUser).toHaveBeenCalledWith(4, expect.not.objectContaining({
      email: '',
    }))
  })

  it('removes an updated user that no longer matches the active status filter', async () => {
    const AdminUsersView = await loadAdminUsersView()
    const wrapper = mount(AdminUsersView)
    await flushPromises()

    api.updateAdminUser.mockResolvedValueOnce(buildUser({
      status: 'disabled',
      email_verified: true,
    }))

    await wrapper.get('[data-user-status-filter]').setValue('active')
    await wrapper.get('[data-user-search-form]').trigger('submit')
    await flushPromises()

    await wrapper.get('[data-user-row="2"]').trigger('click')
    await wrapper.get('[data-user-status-select]').setValue('disabled')
    await wrapper.get('[data-user-detail-form]').trigger('submit')
    await flushPromises()

    expect(api.updateAdminUser).toHaveBeenCalledWith(2, expect.objectContaining({
      status: 'disabled',
    }))
    expect(wrapper.find('[data-user-row="2"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('从左侧选择一个用户。')
  })
})
