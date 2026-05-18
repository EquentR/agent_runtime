import { flushPromises, mount } from '@vue/test-utils'
import type { Component } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  fetchAdminAuditEvents: vi.fn(),
}))

vi.mock('../lib/api', () => api)

async function loadAdminOperationAuditView() {
  const modules = import.meta.glob('./AdminOperationAuditView.vue')
  const loader = modules['./AdminOperationAuditView.vue']
  expect(loader).toBeTypeOf('function')

  const module = (await loader()) as { default: Component }
  return module.default
}

describe('AdminOperationAuditView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.fetchAdminAuditEvents.mockResolvedValue([
      {
        id: 31,
        actor_id: 1,
        actor_username: 'admin',
        actor_email: 'admin@example.com',
        target_kind: 'user',
        target_id: '2',
        action: 'admin.users.update',
        before_json: { status: 'active' },
        after_json: { status: 'disabled' },
        ip_address: '127.0.0.1',
        user_agent: 'vitest',
        created_at: '2026-05-18T09:00:00Z',
      },
    ])
  })

  it('lists admin events and supports basic filters', async () => {
    const AdminOperationAuditView = await loadAdminOperationAuditView()
    const wrapper = mount(AdminOperationAuditView)
    await flushPromises()

    expect(api.fetchAdminAuditEvents).toHaveBeenCalledWith({ limit: 100 })
    expect(wrapper.get('[data-admin-audit-row="31"]').text()).toContain('admin.users.update')
    expect(wrapper.text()).toContain('admin')
    expect(wrapper.text()).toContain('user')

    await wrapper.get('[data-admin-audit-action-input]').setValue('admin.users.update')
    await wrapper.get('[data-admin-audit-target-kind-input]').setValue('user')
    await wrapper.get('[data-admin-audit-actor-input]').setValue('admin')
    await wrapper.get('[data-admin-audit-start-date-input]').setValue('2026-05-18')
    await wrapper.get('[data-admin-audit-end-date-input]').setValue('2026-05-19')
    await wrapper.get('[data-admin-audit-filter-form]').trigger('submit')
    await flushPromises()

    expect(api.fetchAdminAuditEvents).toHaveBeenLastCalledWith({
      action: 'admin.users.update',
      target_kind: 'user',
      actor_username: 'admin',
      created_after: '2026-05-18',
      created_before: '2026-05-19',
      limit: 100,
    })
  })
})
