import { flushPromises, mount } from '@vue/test-utils'
import type { Component } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  changeUserPassword: vi.fn(),
  confirmUserEmailVerification: vi.fn(),
  createUserCustomModel: vi.fn(),
  deleteUserCustomModel: vi.fn(),
  fetchPublicTurnstileSettings: vi.fn(),
  fetchUserCustomModels: vi.fn(),
  fetchUserProfile: vi.fn(),
  startUserEmailVerification: vi.fn(),
  testUserCustomModel: vi.fn(),
  updateUserCustomModel: vi.fn(),
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

function buildCustomModel(overrides: Record<string, unknown> = {}) {
  return {
    id: 'custom_me',
    owner_user_id: 7,
    provider_id: 'me-openai',
    model_id: 'gpt-me',
    display_name: 'GPT Me',
    provider_type: 'openai_responses',
    base_url: '',
    api_key_masked: 'sk-****mine',
    scope: 'owner',
    enabled: true,
    context_max_tokens: 65536,
    context: { max: 65536, input: 57344, output: 8192 },
    capabilities: { attachments: false },
    created_at: '2026-05-18T10:00:00Z',
    updated_at: '2026-05-18T10:00:00Z',
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
    Reflect.deleteProperty(window, 'turnstile')
    api.fetchUserProfile.mockResolvedValue(buildProfile())
    api.fetchPublicTurnstileSettings.mockResolvedValue({
      enabled: false,
      site_key: '',
      protect_login: false,
      protect_registration: false,
      protect_verification: false,
    })
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
    api.fetchUserCustomModels.mockResolvedValue([buildCustomModel()])
    api.createUserCustomModel.mockResolvedValue(buildCustomModel({ id: 'custom_created', display_name: 'Created GPT' }))
    api.updateUserCustomModel.mockResolvedValue(buildCustomModel({ context_max_tokens: 32768 }))
    api.deleteUserCustomModel.mockResolvedValue({ deleted: true })
    api.testUserCustomModel.mockResolvedValue({ ok: true })
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('updates display name and password while showing required action states', async () => {
    const wrapper = await mountProfileView()
    await flushPromises()

    expect(api.fetchUserProfile).toHaveBeenCalledTimes(1)
    expect(wrapper.findAll('h1').filter((heading) => heading.text() === '个人设置')).toHaveLength(1)
    expect(wrapper.text()).toContain('必须绑定邮箱')
    expect(wrapper.text()).toContain('必须修改密码')
    expect(wrapper.find('[data-profile-models-link]').exists()).toBe(true)
    expect(wrapper.get('[data-open-source-licenses-toggle]').exists()).toBe(true)
    expect(wrapper.text()).toContain('我的模型')

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

  it('sends profile email verification with a turnstile token when protected', async () => {
    api.fetchPublicTurnstileSettings.mockResolvedValueOnce({
      enabled: true,
      site_key: 'site-key',
      protect_login: false,
      protect_registration: false,
      protect_verification: true,
    })
    const resetTurnstile = vi.fn()
    Object.defineProperty(window, 'turnstile', {
      configurable: true,
      value: {
        render: vi.fn((_element, options) => {
          options.callback('profile-token')
          return 'profile-widget'
        }),
        reset: resetTurnstile,
        remove: vi.fn(),
      },
    })

    const wrapper = await mountProfileView()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('.turnstile-widget').exists()).toBe(true)

    await wrapper.get('[data-profile-email-input]').setValue(' bound@example.com ')
    await wrapper.get('[data-profile-email-start-form]').trigger('submit')
    await flushPromises()

    expect(api.startUserEmailVerification).toHaveBeenCalledWith({
      email: 'bound@example.com',
      turnstile_token: 'profile-token',
    })
    expect(resetTurnstile).toHaveBeenCalledWith('profile-widget')
  })

  it('ProfileView manages owner-scoped custom models', async () => {
    const wrapper = await mountProfileView()
    await flushPromises()

    expect(api.fetchUserCustomModels).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('我的模型')
    expect(wrapper.text()).toContain('GPT Me')
    expect(wrapper.text()).toContain('启用')

    expect(wrapper.findAll('[data-user-model-provider-type] option').map((option) => option.attributes('value'))).toContain('openai_chat')
    expect(wrapper.text()).toContain('支持图片输入(直传)')

    await wrapper.get('[data-user-model-provider-type]').setValue('openai_responses')
    await wrapper.get('[data-user-model-provider-id]').setValue(' me-openai-2 ')
    await wrapper.get('[data-user-model-model-id]').setValue(' gpt-me-2 ')
    await wrapper.get('[data-user-model-display-name]').setValue(' Created GPT ')
    await wrapper.get('[data-user-model-api-key]').setValue('sk-secret')
    await wrapper.get('[data-user-model-context-max]').setValue('65536')
    await wrapper.get('[data-user-model-form]').trigger('submit')
    await flushPromises()

    expect(api.createUserCustomModel).toHaveBeenCalledWith(expect.objectContaining({
      provider_id: 'me-openai-2',
      model_id: 'gpt-me-2',
      display_name: 'Created GPT',
      provider_type: 'openai_responses',
      api_key: 'sk-secret',
      scope: 'owner',
      enabled: true,
      context_max_tokens: 65536,
      capabilities: { attachments: false },
    }))
    expect(wrapper.text()).toContain('模型已创建')

    await wrapper.get('[data-user-model-test="custom_me"]').trigger('click')
    await flushPromises()

    expect(api.testUserCustomModel).toHaveBeenCalledWith('custom_me')

    await wrapper.get('[data-user-model-row="custom_me"]').trigger('click')
    await wrapper.get('[data-user-model-base-url]').setValue('')
    await wrapper.get('[data-user-model-context-max]').setValue('32768')
    await wrapper.get('[data-user-model-form]').trigger('submit')
    await flushPromises()

    expect(api.updateUserCustomModel).toHaveBeenCalledWith('custom_me', expect.objectContaining({
      base_url: '',
      clear_base_url: true,
      scope: 'owner',
      context_max_tokens: 32768,
    }))
    expect(wrapper.find('[data-user-model-scope]').exists()).toBe(false)
  })

  it('clears base URL for user OpenAI completions models', async () => {
    api.fetchUserCustomModels.mockResolvedValueOnce([buildCustomModel({
      provider_type: 'openai_completions',
      base_url: 'https://old.example.com/v1',
    })])

    const wrapper = await mountProfileView()
    await flushPromises()

    await wrapper.get('[data-user-model-row="custom_me"]').trigger('click')
    await wrapper.get('[data-user-model-base-url]').setValue('')
    await wrapper.get('[data-user-model-form]').trigger('submit')
    await flushPromises()

    expect(api.updateUserCustomModel).toHaveBeenCalledWith('custom_me', expect.objectContaining({
      base_url: '',
      clear_base_url: true,
      provider_type: 'openai_completions',
    }))
  })
})
