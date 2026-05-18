import { flushPromises, mount } from '@vue/test-utils'
import type { Component } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  createAdminCustomModel: vi.fn(),
  deleteAdminCustomModel: vi.fn(),
  fetchAdminCustomModels: vi.fn(),
  fetchAdminModels: vi.fn(),
  testAdminCustomModel: vi.fn(),
  updateAdminCustomModel: vi.fn(),
  updateAdminYAMLModel: vi.fn(),
}))

vi.mock('../lib/api', () => api)

function buildYamlCatalog() {
  return {
    default_provider_id: 'yaml',
    default_model_id: 'admin-only',
    providers: [
      {
        id: 'yaml',
        name: 'yaml',
        models: [
          {
            id: 'admin-only',
            name: 'Admin Only',
            type: 'openai_responses',
            context: { max: 128000, input: 120000, output: 8000 },
            capabilities: { attachments: true },
            scope: 'admin',
            enabled: true,
            scope_overridden: false,
            enabled_overridden: false,
          },
        ],
      },
    ],
  }
}

function buildCustomModel(overrides: Record<string, unknown> = {}) {
  return {
    id: 'custom_1',
    owner_user_id: 42,
    provider_id: 'team-openai',
    model_id: 'gpt-custom',
    display_name: 'Team GPT',
    provider_type: 'openai_completions',
    base_url: 'https://llm.example.com/v1',
    api_key_masked: 'sk-****abcd',
    scope: 'global',
    enabled: true,
    context_max_tokens: 32768,
    context: { max: 32768, input: 24576, output: 8192 },
    capabilities: { attachments: true },
    created_at: '2026-05-18T10:00:00Z',
    updated_at: '2026-05-18T10:00:00Z',
    ...overrides,
  }
}

async function loadAdminModelsView() {
  const modules = import.meta.glob('./AdminModelsView.vue')
  const loader = modules['./AdminModelsView.vue']
  expect(loader).toBeTypeOf('function')

  const module = (await loader()) as { default: Component }
  return module.default
}

async function mountAdminModelsView() {
  const AdminModelsView = await loadAdminModelsView()
  const wrapper = mount(AdminModelsView)
  await flushPromises()
  return wrapper
}

describe('AdminModelsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.fetchAdminModels.mockResolvedValue(buildYamlCatalog())
    api.fetchAdminCustomModels.mockResolvedValue([buildCustomModel()])
    api.updateAdminYAMLModel.mockImplementation((_providerId: string, _modelId: string, input: Record<string, unknown>) =>
      Promise.resolve({
        ...buildYamlCatalog().providers[0].models[0],
        ...input,
        scope_overridden: true,
        enabled_overridden: true,
      }),
    )
    api.createAdminCustomModel.mockResolvedValue(buildCustomModel({ id: 'custom_created' }))
    api.updateAdminCustomModel.mockResolvedValue(buildCustomModel())
    api.deleteAdminCustomModel.mockResolvedValue({ deleted: true })
    api.testAdminCustomModel.mockResolvedValue({ ok: true })
  })

  it('AdminModelsView edits YAML scope and enabled state', async () => {
    const wrapper = await mountAdminModelsView()

    expect(wrapper.text()).toContain('YAML 配置模型')
    expect(wrapper.text()).toContain('Admin Only')

    await wrapper.get('[data-yaml-enabled="yaml:admin-only"]').setValue(false)
    await flushPromises()

    expect(api.updateAdminYAMLModel).toHaveBeenCalledWith('yaml', 'admin-only', {
      enabled: false,
      scope: 'admin',
    })

    await wrapper.get('[data-yaml-scope="yaml:admin-only"]').setValue('global')
    await flushPromises()

    expect(api.updateAdminYAMLModel).toHaveBeenLastCalledWith('yaml', 'admin-only', {
      enabled: false,
      scope: 'global',
    })
  })

  it('marks YAML models missing context as using system default context', async () => {
    api.fetchAdminModels.mockResolvedValueOnce({
      ...buildYamlCatalog(),
      providers: [
        {
          id: 'yaml',
          name: 'yaml',
          models: [
            {
              id: 'default-context',
              name: 'Default Context',
              type: 'openai_responses',
              capabilities: { attachments: false },
              scope: 'admin',
              enabled: true,
              scope_overridden: false,
              enabled_overridden: false,
            },
          ],
        },
      ],
    })

    const wrapper = await mountAdminModelsView()

    expect(wrapper.text()).toContain('Default Context')
    expect(wrapper.text()).toContain('使用系统默认上下文')
  })

  it('AdminModelsView creates custom admin/global model with context max', async () => {
    api.fetchAdminCustomModels.mockResolvedValueOnce([])
    const wrapper = await mountAdminModelsView()

    await wrapper.get('[data-admin-model-provider-type]').setValue('openai_completions')
    await wrapper.get('[data-admin-model-provider-id]').setValue(' team-openai ')
    await wrapper.get('[data-admin-model-model-id]').setValue(' gpt-custom ')
    await wrapper.get('[data-admin-model-display-name]').setValue(' Team GPT ')
    await wrapper.get('[data-admin-model-base-url]').setValue(' https://llm.example.com/v1 ')
    await wrapper.get('[data-admin-model-api-key]').setValue('sk-secret')
    await wrapper.get('[data-admin-model-scope]').setValue('global')
    await wrapper.get('[data-admin-model-context-max]').setValue('32768')
    await wrapper.get('[data-admin-model-form]').trigger('submit')
    await flushPromises()

    expect(api.createAdminCustomModel).toHaveBeenCalledWith(expect.objectContaining({
      owner_user_id: 0,
      provider_id: 'team-openai',
      model_id: 'gpt-custom',
      display_name: 'Team GPT',
      provider_type: 'openai_completions',
      base_url: 'https://llm.example.com/v1',
      api_key: 'sk-secret',
      scope: 'global',
      enabled: true,
      context_max_tokens: 32768,
    }))
    expect(wrapper.text()).toContain('32,768')
    expect(wrapper.text()).toContain('sk-****abcd')
  })

  it('AdminModelsView tests another user model and shows audit warning', async () => {
    const wrapper = await mountAdminModelsView()

    await wrapper.get('[data-admin-custom-test="custom_1"]').trigger('click')
    await flushPromises()

    expect(api.testAdminCustomModel).toHaveBeenCalledWith('custom_1')
    expect(wrapper.text()).toContain('正在测试其他用户的模型')
    expect(wrapper.text()).toContain('操作会写入后台审计')
  })

  it('AdminModelsView clears base URL without changing owner when editing a custom model', async () => {
    const wrapper = await mountAdminModelsView()

    await wrapper.get('[data-admin-custom-row="custom_1"]').trigger('click')
    await wrapper.get('[data-admin-model-owner-user-id]').setValue('')
    await wrapper.get('[data-admin-model-base-url]').setValue('')
    await wrapper.get('[data-admin-model-context-max]').setValue('32768')
    await wrapper.get('[data-admin-model-form]').trigger('submit')
    await flushPromises()

    expect(api.updateAdminCustomModel).toHaveBeenCalledWith('custom_1', expect.objectContaining({
      base_url: '',
      clear_base_url: true,
      context_max_tokens: 32768,
    }))
    expect(api.updateAdminCustomModel).toHaveBeenCalledWith('custom_1', expect.not.objectContaining({
      owner_user_id: 0,
    }))
  })
})
