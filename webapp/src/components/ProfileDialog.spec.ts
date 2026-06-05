import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ProfileDialog from './ProfileDialog.vue'

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
    username: 'tester',
    email: '',
    display_name: 'Tester',
    role: 'user',
    status: 'active',
    email_verified: false,
    force_password_change: false,
    required_actions: [],
    ...overrides,
  }
}

async function setBodyInputValue(selector: string, value: string) {
  const input = document.body.querySelector<HTMLInputElement>(selector)
  expect(input).not.toBeNull()
  input!.value = value
  input!.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}

describe('ProfileDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.fetchUserProfile.mockResolvedValue(buildProfile())
    api.fetchUserCustomModels.mockResolvedValue([])
    api.fetchPublicTurnstileSettings.mockResolvedValue({
      enabled: false,
      site_key: '',
      protect_login: false,
      protect_registration: false,
      protect_verification: false,
    })
    api.createUserCustomModel.mockResolvedValue({
      id: 'custom_created',
      owner_user_id: 7,
      provider_id: 'me-openai',
      model_id: 'gpt-me',
      display_name: 'GPT Me',
      provider_type: 'openai_responses',
      base_url: '',
      api_key_masked: 'sk-****mine',
      scope: 'owner',
      enabled: true,
      context_max_tokens: 32768,
      context: { max: 32768, input: 24576, output: 8192 },
      capabilities: { attachments: false },
      created_at: '2026-05-18T10:00:00Z',
      updated_at: '2026-05-18T10:00:00Z',
    })
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('labels attachments capability as direct image input while preserving payload field', async () => {
    const wrapper = mount(ProfileDialog, {
      props: { open: true },
      attachTo: document.body,
    })
    await flushPromises()

    const createButton = document.body.querySelector<HTMLButtonElement>('.profile-action-button')
    expect(createButton).not.toBeNull()
    createButton?.click()
    await flushPromises()

    expect(document.body.textContent).toContain('支持图片输入(直传)')

    await setBodyInputValue('[data-user-model-provider-id]', ' me-openai ')
    await setBodyInputValue('[data-user-model-model-id]', ' gpt-me ')
    await setBodyInputValue('[data-user-model-display-name]', ' GPT Me ')
    await setBodyInputValue('[data-user-model-api-key]', 'sk-secret')
    const form = document.body.querySelector<HTMLFormElement>('[data-user-model-form]')
    expect(form).not.toBeNull()
    form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await flushPromises()

    expect(api.createUserCustomModel).toHaveBeenCalledWith(expect.objectContaining({
      provider_id: 'me-openai',
      model_id: 'gpt-me',
      display_name: 'GPT Me',
      capabilities: { attachments: false },
    }))
  })
})
