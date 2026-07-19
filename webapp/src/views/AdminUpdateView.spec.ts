import { flushPromises, mount } from '@vue/test-utils'
import type { Component } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  authorizeAdminUpdate: vi.fn(),
  checkAdminUpdate: vi.fn(),
  fetchAdminUpdateStatus: vi.fn(),
  forceInstallAdminUpdate: vi.fn(),
  installAdminUpdate: vi.fn(),
  rollbackAdminUpdate: vi.fn(),
}))

vi.mock('../lib/api', () => api)

async function loadView() {
  const modules = import.meta.glob('./AdminUpdateView.vue')
  const loader = modules['./AdminUpdateView.vue']
  expect(loader).toBeTypeOf('function')
  return ((await loader()) as { default: Component }).default
}

describe('AdminUpdateView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.fetchAdminUpdateStatus.mockResolvedValue({
      current: { version: 'v1.2.3', commit: 'abc', distribution: 'container', goos: 'linux', goarch: 'amd64' },
      latest: { tag_name: 'v1.2.4', name: 'v1.2.4', body: '<script>alert(1)</script>\n**Fixed**', html_url: 'https://github.com/EquentR/agent_runtime/releases/tag/v1.2.4', published_at: '2026-07-19T00:00:00Z' },
      update_available: true,
      cache_stale: false,
      capability: 'notification_only',
      capable: false,
      capability_reason: 'container images must be updated by the operator',
      state: { phase: 'idle', generation: 1 },
      maintenance: { active: false },
    })
  })

  it('shows release information but disables native install for notification-only builds', async () => {
    const View = await loadView()
    const wrapper = mount(View, {
      global: {
        stubs: {
          'el-dialog': { props: ['modelValue'], template: '<section><slot /><slot name="footer" /></section>' },
          'el-radio-group': { template: '<div><slot /></div>' },
          'el-radio-button': { template: '<button><slot /></button>' },
        },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('v1.2.4')
    expect(wrapper.text()).toContain('container images must be updated by the operator')
    expect(wrapper.text()).not.toContain('升级到 v1.2.4')
    expect(wrapper.html()).not.toContain('<script>alert(1)</script>')
  })

  it('uses a distinct authorization action and operation id for force install', async () => {
    api.fetchAdminUpdateStatus.mockResolvedValue({
      current: { version: 'v1.2.3', commit: 'abc', distribution: 'release', goos: 'linux', goarch: 'amd64' },
      latest: { tag_name: 'v1.2.4', name: 'v1.2.4', body: '', html_url: '', published_at: '2026-07-19T00:00:00Z' },
      update_available: true,
      cache_stale: false,
      capability: 'native',
      capable: true,
      runtime_mode: 'systemd',
      signature_status: 'trusted',
      force_install_allowed: true,
      state: { phase: 'failed', generation: 4, target_version: 'v1.2.4' },
      maintenance: { active: false },
    })
    api.authorizeAdminUpdate.mockResolvedValue({ authorization_token: 'force-token', expires_at: '2026-07-19T01:00:00Z' })
    api.forceInstallAdminUpdate.mockResolvedValue({ phase: 'draining', generation: 5, operation_id: 'op-force' })
    const View = await loadView()
    const wrapper = mount(View, {
      global: {
        stubs: {
          'el-dialog': { props: ['modelValue'], template: '<section><slot /><slot name="footer" /></section>' },
          'el-radio-group': { template: '<div><slot /></div>' },
          'el-radio-button': { template: '<button><slot /></button>' },
        },
      },
    })
    await flushPromises()
    const forceButton = wrapper.findAll('button').find((button) => button.text().includes('Force continue'))
    expect(forceButton).toBeTruthy()
    await wrapper.find('input[type="password"]').setValue('stale-secret')
    await forceButton!.trigger('click')
    expect((wrapper.find('input[type="password"]').element as HTMLInputElement).value).toBe('')
    await wrapper.find('input[type="password"]').setValue('secret')
    await wrapper.find('.force-confirmation input').setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.authorizeAdminUpdate).toHaveBeenCalledWith({ password: 'secret', action: 'force_install', target: 'v1.2.4' })
    expect(api.forceInstallAdminUpdate).toHaveBeenCalledWith(expect.objectContaining({
      authorization_token: 'force-token',
      target: 'v1.2.4',
      operation_id: expect.any(String),
    }))
    expect(api.installAdminUpdate).not.toHaveBeenCalled()
  })
})
