import { flushPromises, mount } from '@vue/test-utils'
import type { Component } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  fetchAdminRegistrationSettings: vi.fn(),
  fetchAdminSMTPSettings: vi.fn(),
  fetchAdminTurnstileSettings: vi.fn(),
  testAdminSMTPSettings: vi.fn(),
  updateAdminRegistrationSettings: vi.fn(),
  updateAdminSMTPSettings: vi.fn(),
  updateAdminTurnstileSettings: vi.fn(),
}))

vi.mock('../lib/api', () => api)

async function loadAdminSettingsView() {
  const modules = import.meta.glob('./AdminSettingsView.vue')
  const loader = modules['./AdminSettingsView.vue']
  expect(loader).toBeTypeOf('function')

  const module = (await loader()) as { default: Component }
  return module.default
}

describe('AdminSettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.fetchAdminSMTPSettings.mockResolvedValue({
      enabled: true,
      host: 'smtp.example.com',
      port: 587,
      username: 'smtp-user',
      password: '',
      password_masked: '********word',
      from: 'noreply@example.com',
      use_tls: false,
      use_start_tls: true,
    })
    api.fetchAdminTurnstileSettings.mockResolvedValue({
      enabled: true,
      site_key: 'site-key',
      secret: '',
      secret_masked: '********tile',
      protect_login: true,
      protect_registration: true,
      protect_verification: false,
    })
    api.fetchAdminRegistrationSettings.mockResolvedValue({ enabled: true })
    api.updateAdminSMTPSettings.mockResolvedValue({
      enabled: true,
      host: 'smtp.example.com',
      port: 587,
      username: 'smtp-user',
      password: '',
      password_masked: '********cret',
      from: 'noreply@example.com',
      use_tls: false,
      use_start_tls: true,
    })
    api.updateAdminTurnstileSettings.mockResolvedValue({
      enabled: true,
      site_key: 'site-key',
      secret: '',
      secret_masked: '',
      protect_login: true,
      protect_registration: true,
      protect_verification: false,
    })
    api.updateAdminRegistrationSettings.mockResolvedValue({ enabled: false })
    api.testAdminSMTPSettings.mockResolvedValue({ sent: true })
  })

  it('masks SMTP and Turnstile secrets and updates runtime settings', async () => {
    const AdminSettingsView = await loadAdminSettingsView()
    const wrapper = mount(AdminSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-smtp-password-mask]').text()).toContain('********word')
    expect(wrapper.get('[data-turnstile-secret-mask]').text()).toContain('********tile')
    expect((wrapper.get('[data-smtp-password-input]').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('[data-turnstile-secret-input]').element as HTMLInputElement).value).toBe('')

    await wrapper.get('[data-smtp-password-input]').setValue('replacement-secret')
    await wrapper.get('[data-smtp-form]').trigger('submit')
    await flushPromises()

    expect(api.updateAdminSMTPSettings).toHaveBeenCalledWith(expect.objectContaining({
      enabled: true,
      host: 'smtp.example.com',
      port: 587,
      username: 'smtp-user',
      password: 'replacement-secret',
      clear_password: false,
      from: 'noreply@example.com',
      use_start_tls: true,
    }))

    await wrapper.get('[data-smtp-test-to-input]').setValue('ops@example.com')
    await wrapper.get('[data-smtp-test-form]').trigger('submit')
    await flushPromises()

    expect(api.testAdminSMTPSettings).toHaveBeenCalledWith({ to: 'ops@example.com' })

    await wrapper.get('[data-turnstile-clear-secret-input]').setValue(true)
    await wrapper.get('[data-turnstile-form]').trigger('submit')
    await flushPromises()

    expect(api.updateAdminTurnstileSettings).toHaveBeenCalledWith(expect.objectContaining({
      enabled: true,
      site_key: 'site-key',
      secret: '',
      clear_secret: true,
      protect_login: true,
      protect_registration: true,
    }))

    await wrapper.get('[data-registration-enabled-input]').setValue(false)
    await wrapper.get('[data-registration-form]').trigger('submit')
    await flushPromises()

    expect(api.updateAdminRegistrationSettings).toHaveBeenCalledWith({ enabled: false })
  })

  it('keeps SMTP implicit TLS and STARTTLS mutually exclusive', async () => {
    const AdminSettingsView = await loadAdminSettingsView()
    const wrapper = mount(AdminSettingsView)
    await flushPromises()

    const tlsInput = wrapper.findAll('label.admin-check-row')
      .find((label) => label.text().includes('Use TLS'))
      ?.get('input')
    expect(tlsInput).toBeDefined()

    await tlsInput!.setValue(true)
    await wrapper.get('[data-smtp-form]').trigger('submit')
    await flushPromises()

    expect(api.updateAdminSMTPSettings).toHaveBeenCalledWith(expect.objectContaining({
      use_tls: true,
      use_start_tls: false,
    }))
  })
})
