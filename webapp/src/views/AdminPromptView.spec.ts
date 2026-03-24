import { flushPromises, mount } from '@vue/test-utils'
import type { Component } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ElementPlus from 'element-plus'

const api = vi.hoisted(() => ({
  fetchPromptDocuments: vi.fn(),
  createPromptDocument: vi.fn(),
  deletePromptDocument: vi.fn(),
  updatePromptDocument: vi.fn(),
  fetchModelCatalog: vi.fn(),
  fetchPromptBindings: vi.fn(),
  createPromptBinding: vi.fn(),
  updatePromptBinding: vi.fn(),
  deletePromptBinding: vi.fn(),
}))

vi.mock('../lib/api', () => api)

function buildDocument(overrides: Record<string, unknown> = {}) {
  return {
    id: 'doc-welcome',
    name: 'Welcome Prompt',
    description: 'Primary session instructions',
    content: 'Always explain the plan before acting.',
    scope: 'admin',
    status: 'active',
    created_by: 'alice',
    updated_by: 'alice',
    created_at: '2026-03-23T09:00:00Z',
    updated_at: '2026-03-23T09:00:00Z',
    ...overrides,
  }
}

function buildBinding(overrides: Record<string, unknown> = {}) {
  return {
    id: 21,
    prompt_id: 'doc-welcome',
    scene: 'agent.run.default',
    phase: 'session',
    is_default: true,
    priority: 10,
    provider_id: '',
    model_id: '',
    status: 'active',
    created_by: 'alice',
    updated_by: 'alice',
    created_at: '2026-03-23T09:00:00Z',
    updated_at: '2026-03-23T09:00:00Z',
    ...overrides,
  }
}

async function loadAdminPromptView() {
  const modules = import.meta.glob('./AdminPromptView.vue')
  const loader = modules['./AdminPromptView.vue']
  expect(loader).toBeTypeOf('function')
  if (!loader) return null
  const module = (await loader()) as { default: Component }
  return module.default
}

async function mountAdminPromptView() {
  const AdminPromptView = await loadAdminPromptView()
  if (!AdminPromptView) return null

  return mount(AdminPromptView, {
    attachTo: document.body,
    global: {
      plugins: [ElementPlus],
      stubs: {
        RouterLink: {
          props: ['to'],
          template: '<a :href="to" v-bind="$attrs"><slot /></a>',
        },
      },
    },
  })
}

async function openBindingDialog(wrapper: NonNullable<Awaited<ReturnType<typeof mountAdminPromptView>>>) {
  await wrapper.get('[data-open-binding-dialog]').trigger('click')
  await flushPromises()
}

describe('AdminPromptView', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()

    api.fetchPromptDocuments.mockResolvedValue([
      buildDocument(),
      buildDocument({
        id: 'doc-tool',
        name: 'Tool Follow-up',
        description: 'Tool continuation instructions',
        content: 'Summarize tool results before the next call.',
      }),
    ])
    api.fetchPromptBindings.mockResolvedValue([
      buildBinding(),
      buildBinding({
        id: 22,
        prompt_id: 'doc-tool',
        scene: 'agent.run.default',
        phase: 'tool_result',
        is_default: false,
        priority: 40,
      }),
    ])
    api.fetchModelCatalog.mockResolvedValue({
      default_provider_id: 'openai',
      default_model_id: 'gpt-5.4',
      providers: [
        {
          id: 'openai',
          name: 'OpenAI',
          models: [
            { id: 'gpt-5.4', name: 'GPT 5.4', type: 'chat' },
            { id: 'gpt-5.4-mini', name: 'GPT 5.4 Mini', type: 'chat' },
          ],
        },
        {
          id: 'google',
          name: 'Google',
          models: [{ id: 'gemini-2.5-flash', name: 'Gemini 2.5 Flash', type: 'chat' }],
        },
      ],
    })
  })

  it('renders prompt editor as the primary panel and opens scene binding dialog from the header button', async () => {
    const wrapper = await mountAdminPromptView()
    if (!wrapper) return

    await flushPromises()

    expect(wrapper.text()).toContain('提示词编辑')
    expect(wrapper.text()).toContain('提示词 ID')
    expect(wrapper.text()).toContain('保存提示词')
    expect(wrapper.get('[data-open-binding-dialog]').text()).toContain('场景绑定')

    await openBindingDialog(wrapper)

    expect(wrapper.find('[data-binding-dialog-table]').exists()).toBe(true)
    expect(wrapper.find('[data-binding-dialog-header]').find('[data-binding-dialog-close]').exists()).toBe(true)
    expect(wrapper.find('[data-binding-table-header]').find('[data-create-binding]').exists()).toBe(true)
    expect(wrapper.find('[data-binding-empty-state]').exists()).toBe(true)
    expect(wrapper.find('[data-binding-dialog-form]').exists()).toBe(false)
    expect(wrapper.find('[data-binding-id="21"]').text()).toContain('agent.run.default')

    await wrapper.get('[data-binding-dialog-close]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-binding-dialog]').exists()).toBe(false)
  })

  it('creates and updates a prompt document with auto-generated id support', async () => {
    api.fetchPromptBindings.mockResolvedValue([])
    api.createPromptDocument.mockResolvedValue(
      buildDocument({
        id: 'new-prompt-template',
        name: 'New Prompt Template',
        description: 'Fresh instructions',
        content: 'Reply with a concise implementation plan.',
      }),
    )
    api.updatePromptDocument.mockResolvedValue(
      buildDocument({
        id: 'doc-welcome',
        name: 'Welcome Prompt Updated',
        description: 'Primary session instructions',
        content: 'Explain the plan and keep replies compact.',
        status: 'disabled',
      }),
    )

    const wrapper = await mountAdminPromptView()
    if (!wrapper) return

    await flushPromises()

    await wrapper.get('[data-create-document]').trigger('click')
    await wrapper.get('[data-document-name-input]').setValue(' New Prompt Template ')
    expect((wrapper.get('[data-document-id-input]').element as HTMLInputElement).value).toBe('new-prompt-template')

    await wrapper.get('[data-document-description-input]').setValue('Fresh instructions')
    await wrapper.get('[data-document-content-input]').setValue('Reply with a concise implementation plan.')
    await wrapper.get('[data-document-form]').trigger('submit')
    await flushPromises()

    expect(api.createPromptDocument).toHaveBeenCalledWith({
      id: 'new-prompt-template',
      name: 'New Prompt Template',
      description: 'Fresh instructions',
      content: 'Reply with a concise implementation plan.',
      scope: 'admin',
      status: 'active',
    })

    await wrapper.get('[data-document-id="doc-welcome"]').trigger('click')
    await wrapper.get('[data-document-name-input]').setValue('Welcome Prompt Updated')
    await wrapper.get('[data-document-content-input]').setValue('Explain the plan and keep replies compact.')
    await wrapper.get('[data-document-status-input]').setValue('disabled')
    await wrapper.get('[data-document-form]').trigger('submit')
    await flushPromises()

    expect(api.updatePromptDocument).toHaveBeenCalledWith('doc-welcome', {
      name: 'Welcome Prompt Updated',
      description: 'Primary session instructions',
      content: 'Explain the plan and keep replies compact.',
      scope: 'admin',
      status: 'disabled',
    })
  })

  it('creates and updates bindings inside the dialog table/form layout', async () => {
    api.createPromptBinding.mockResolvedValue(
      buildBinding({
        id: 23,
        scene: 'agent.run.tools',
        phase: 'tool_result',
        priority: 30,
        provider_id: 'openai',
        model_id: 'gpt-5.4',
      }),
    )
    api.updatePromptBinding.mockResolvedValue(
      buildBinding({
        id: 21,
        phase: 'step_pre_model',
        is_default: false,
        priority: 4,
        status: 'disabled',
      }),
    )

    const wrapper = await mountAdminPromptView()
    if (!wrapper) return

    await flushPromises()
    await openBindingDialog(wrapper)

    expect(wrapper.find('[data-binding-empty-state]').exists()).toBe(true)
    expect(api.fetchModelCatalog).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-binding-dialog]').attributes('data-binding-form-visible')).toBe('false')

    await wrapper.get('[data-create-binding]').trigger('click')
    expect(wrapper.find('[data-binding-empty-state]').exists()).toBe(false)
    expect(wrapper.find('[data-binding-dialog-form]').exists()).toBe(true)
    expect(wrapper.get('[data-binding-dialog]').attributes('data-binding-form-visible')).toBe('true')
    await wrapper.get('[data-binding-scene-input]').setValue('agent.run.tools')
    await wrapper.get('[data-binding-phase-input]').setValue('tool_result')
    await wrapper.get('[data-binding-default-input]').setValue('true')
    await wrapper.get('[data-binding-priority-input]').setValue('30')
    await wrapper.get('[data-binding-provider-input]').setValue('openai')
    expect((wrapper.get('[data-binding-model-input]').element as HTMLSelectElement).value).toBe('gpt-5.4')
    await wrapper.get('[data-binding-model-input]').setValue('gpt-5.4')
    await wrapper.get('[data-binding-form]').trigger('submit')
    await flushPromises()

    expect(api.createPromptBinding).toHaveBeenCalledWith({
      prompt_id: 'doc-welcome',
      scene: 'agent.run.tools',
      phase: 'tool_result',
      is_default: true,
      priority: 30,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      status: 'active',
    })

    await wrapper.get('[data-binding-edit="21"]').trigger('click')
    await wrapper.get('[data-binding-phase-input]').setValue('step_pre_model')
    await wrapper.get('[data-binding-default-input]').setValue('false')
    await wrapper.get('[data-binding-priority-input]').setValue('4')
    await wrapper.get('[data-binding-status-input]').setValue('disabled')
    await wrapper.get('[data-binding-form]').trigger('submit')
    await flushPromises()

    expect(api.updatePromptBinding).toHaveBeenCalledWith(21, {
      prompt_id: 'doc-welcome',
      scene: 'agent.run.default',
      phase: 'step_pre_model',
      is_default: false,
      priority: 4,
      provider_id: '',
      model_id: '',
      status: 'disabled',
    })
  })

  it('deletes a binding from the dialog and keeps the panel height stable', async () => {
    api.deletePromptBinding.mockResolvedValue({ deleted: true })

    const wrapper = await mountAdminPromptView()
    if (!wrapper) return

    await flushPromises()
    await openBindingDialog(wrapper)

    const dialog = wrapper.get('[data-binding-dialog]')
    expect(dialog.attributes('data-binding-form-visible')).toBe('false')

    await wrapper.get('[data-binding-edit="21"]').trigger('click')
    expect(dialog.attributes('data-binding-form-visible')).toBe('true')

    await wrapper.get('[data-binding-delete="21"]').trigger('click')
    await flushPromises()

    expect(api.deletePromptBinding).toHaveBeenCalledWith(21)
    expect(wrapper.find('[data-binding-id="21"]').exists()).toBe(false)
    expect(dialog.attributes('data-binding-form-visible')).toBe('false')
    expect(wrapper.find('[data-binding-empty-state]').exists()).toBe(true)
  })

  it('auto-resizes the prompt content textarea from a default eight-line height with extra buffer', async () => {
    const wrapper = await mountAdminPromptView()
    if (!wrapper) return

    await flushPromises()

    const textarea = wrapper.get('[data-document-content-input]').element as HTMLTextAreaElement
    expect(textarea.getAttribute('rows')).toBe('8')

    Object.defineProperty(textarea, 'scrollHeight', {
      configurable: true,
      value: 360,
    })

    await wrapper.get('[data-document-content-input]').setValue('line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9')
    expect(textarea.style.height).toBe('372px')
  })
})
