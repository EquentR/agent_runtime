import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import WorkspaceFileDialog from './WorkspaceFileDialog.vue'
import type { WorkspaceTreeNode } from '../../types/api'

const api = vi.hoisted(() => {
  return {
    buildConversationWorkspaceDownloadUrl: vi.fn((conversationId: string, path: string) =>
      `/api/v1/conversations/${conversationId}/workspace/download?path=${encodeURIComponent(path)}`,
    ),
    fetchConversationWorkspaceDiff: vi.fn(),
    fetchConversationWorkspaceFile: vi.fn(),
  }
})

const monaco = vi.hoisted(() => {
  const editors: Array<{
    dispose: ReturnType<typeof vi.fn>
    layout: ReturnType<typeof vi.fn>
    setModel: ReturnType<typeof vi.fn>
    setValue: ReturnType<typeof vi.fn>
    options: Record<string, unknown>
  }> = []
  const diffEditors: Array<{
    dispose: ReturnType<typeof vi.fn>
    layout: ReturnType<typeof vi.fn>
    setModel: ReturnType<typeof vi.fn>
    setValue: ReturnType<typeof vi.fn>
    options: Record<string, unknown>
  }> = []
  const models: Array<{
    dispose: ReturnType<typeof vi.fn>
    value: string
    language?: string
  }> = []

  const createMonacoApi = () => ({
    editor: {
      create: vi.fn((_host: HTMLElement, options: Record<string, unknown>) => {
        const editor = {
          dispose: vi.fn(),
          layout: vi.fn(),
          setModel: vi.fn(),
          setValue: vi.fn(),
          options,
        }
        editors.push(editor)
        return editor
      }),
      createDiffEditor: vi.fn((_host: HTMLElement, options: Record<string, unknown>) => {
        const editor = {
          dispose: vi.fn(),
          layout: vi.fn(),
          setModel: vi.fn(),
          setValue: vi.fn(),
          options,
        }
        diffEditors.push(editor)
        return editor
      }),
      createModel: vi.fn((value: string, language?: string) => {
        const model = {
          dispose: vi.fn(),
          value,
          language,
        }
        models.push(model)
        return model
      }),
    },
  })

  return {
    createMonacoApi,
    diffEditors,
    editors,
    getMonaco: vi.fn(),
    models,
  }
})

vi.mock('../../lib/api', () => api)

vi.mock('../../lib/monaco', () => ({
  getMonaco: monaco.getMonaco,
}))

const textNode: WorkspaceTreeNode = {
  path: 'src/app.ts',
  name: 'app.ts',
  type: 'file',
  size: 42,
  has_diff: true,
}

function createDeferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })
  return { promise, reject, resolve }
}

function mountDialog(node: WorkspaceTreeNode | null = textNode, props: Partial<InstanceType<typeof WorkspaceFileDialog>['$props']> = {}) {
  return mount(WorkspaceFileDialog, {
    attachTo: document.body,
    props: {
      conversationId: 'conv_1',
      node,
      open: true,
      ...props,
    },
  })
}

describe('WorkspaceFileDialog', () => {
  beforeEach(() => {
    api.buildConversationWorkspaceDownloadUrl.mockClear()
    api.fetchConversationWorkspaceDiff.mockReset()
    api.fetchConversationWorkspaceFile.mockReset()
    monaco.diffEditors.length = 0
    monaco.editors.length = 0
    monaco.models.length = 0
    monaco.getMonaco.mockReset()
    monaco.getMonaco.mockResolvedValue(monaco.createMonacoApi())
    document.body.innerHTML = ''
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('opens a text file in a read-only Monaco editor', async () => {
    api.fetchConversationWorkspaceFile.mockResolvedValue({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      name: 'app.ts',
      type: 'file',
      content: 'const app = true',
      binary: false,
      size: 42,
    })

    const wrapper = mountDialog()

    await flushPromises()
    await flushPromises()

    expect(api.fetchConversationWorkspaceFile).toHaveBeenCalledWith('conv_1', 'src/app.ts')
    expect(monaco.getMonaco).toHaveBeenCalled()
    expect(document.body.textContent).toContain('src/app.ts')
    expect(document.body.querySelector('[data-workspace-dialog-editor]')).not.toBeNull()
    expect(monaco.editors).toHaveLength(1)
    expect(monaco.editors[0].options).toMatchObject({ readOnly: true })

    wrapper.unmount()
  })

  it('loads diff only after selecting diff mode and uses a diff editor for paired content', async () => {
    const diffDeferred = createDeferred({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      diff: '@@ -1 +1 @@',
      home_content: 'const app = false',
      task_content: 'const app = true',
    })
    api.fetchConversationWorkspaceFile.mockResolvedValue({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      name: 'app.ts',
      type: 'file',
      content: 'const app = true',
      binary: false,
      size: 42,
    })
    api.fetchConversationWorkspaceDiff.mockReturnValue(diffDeferred.promise)

    const wrapper = mountDialog()

    await flushPromises()

    expect(api.fetchConversationWorkspaceDiff).not.toHaveBeenCalled()

    const diffButton = document.body.querySelector<HTMLButtonElement>('[data-workspace-dialog-mode="diff"]')
    expect(diffButton).not.toBeNull()
    await diffButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await diffButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(api.fetchConversationWorkspaceDiff).toHaveBeenCalledTimes(1)

    diffDeferred.resolve({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      diff: '@@ -1 +1 @@',
      home_content: 'const app = false',
      task_content: 'const app = true',
    })
    await flushPromises()
    await flushPromises()

    expect(api.fetchConversationWorkspaceDiff).toHaveBeenCalledWith('conv_1', 'src/app.ts')
    expect(monaco.diffEditors).toHaveLength(1)
    expect(monaco.diffEditors[0].setModel).toHaveBeenCalled()

    wrapper.unmount()
  })

  it('loads and renders a paired diff when opened in initial diff mode', async () => {
    api.fetchConversationWorkspaceFile.mockResolvedValue({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      name: 'app.ts',
      type: 'file',
      content: 'const app = true',
      binary: false,
      size: 42,
    })
    api.fetchConversationWorkspaceDiff.mockResolvedValue({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      diff: '@@ -1 +1 @@',
      home_content: 'const app = false',
      task_content: 'const app = true',
    })

    const wrapper = mountDialog(textNode, { initialMode: 'diff' })

    await flushPromises()
    await flushPromises()

    expect(api.fetchConversationWorkspaceDiff).toHaveBeenCalledWith('conv_1', 'src/app.ts')
    expect(monaco.diffEditors).toHaveLength(1)
    expect(monaco.diffEditors[0].setModel).toHaveBeenCalled()

    wrapper.unmount()
  })

  it('renders diff when diff resolves before the initial file request', async () => {
    const fileDeferred = createDeferred({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      name: 'app.ts',
      type: 'file',
      content: 'const app = true',
      binary: false,
      size: 42,
    })
    api.fetchConversationWorkspaceFile.mockReturnValue(fileDeferred.promise)
    api.fetchConversationWorkspaceDiff.mockResolvedValue({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      diff: '@@ -1 +1 @@',
      home_content: 'const app = false',
      task_content: 'const app = true',
    })

    const wrapper = mountDialog()

    const diffButton = document.body.querySelector<HTMLButtonElement>('[data-workspace-dialog-mode="diff"]')
    expect(diffButton).not.toBeNull()
    await diffButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    await flushPromises()

    expect(api.fetchConversationWorkspaceDiff).toHaveBeenCalledWith('conv_1', 'src/app.ts')

    fileDeferred.resolve({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      name: 'app.ts',
      type: 'file',
      content: 'const app = true',
      binary: false,
      size: 42,
    })
    await flushPromises()
    await flushPromises()

    expect(monaco.diffEditors).toHaveLength(1)
    expect(monaco.diffEditors[0].setModel).toHaveBeenCalled()

    wrapper.unmount()
  })

  it('disposes the mounted file editor when a binary diff replaces it', async () => {
    api.fetchConversationWorkspaceFile.mockResolvedValue({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      name: 'app.ts',
      type: 'file',
      content: 'const app = true',
      binary: false,
      size: 42,
    })
    api.fetchConversationWorkspaceDiff.mockResolvedValue({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      diff: '',
      binary: true,
    })

    const wrapper = mountDialog()

    await flushPromises()
    await flushPromises()

    expect(monaco.editors).toHaveLength(1)
    const fileEditor = monaco.editors[0]
    const fileModel = monaco.models[0]

    const diffButton = document.body.querySelector<HTMLButtonElement>('[data-workspace-dialog-mode="diff"]')
    await diffButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    await flushPromises()

    expect(fileEditor.dispose).toHaveBeenCalled()
    expect(fileModel.dispose).toHaveBeenCalled()
    expect(monaco.editors).toHaveLength(1)
    expect(monaco.diffEditors).toHaveLength(0)
    expect(document.body.textContent).toContain('Binary file')

    wrapper.unmount()
  })

  it('shows binary fallback with a download link', async () => {
    const binaryNode: WorkspaceTreeNode = {
      path: 'assets/logo.png',
      name: 'logo.png',
      type: 'file',
      binary: true,
      size: 2048,
    }
    api.fetchConversationWorkspaceFile.mockResolvedValue({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'assets/logo.png',
      name: 'logo.png',
      type: 'file',
      binary: true,
      size: 2048,
    })

    const wrapper = mountDialog(binaryNode)

    await flushPromises()
    await flushPromises()

    expect(document.body.textContent).toContain('assets/logo.png')
    expect(document.body.textContent).toContain('2 KB')
    expect(monaco.editors).toHaveLength(0)
    const download = document.body.querySelector<HTMLAnchorElement>('[data-workspace-dialog-download]')
    expect(download?.getAttribute('href')).toBe('/api/v1/conversations/conv_1/workspace/download?path=assets%2Flogo.png')

    wrapper.unmount()
  })

  it('emits close from backdrop clicks and Escape', async () => {
    api.fetchConversationWorkspaceFile.mockResolvedValue({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      name: 'app.ts',
      type: 'file',
      content: 'const app = true',
      binary: false,
      size: 42,
    })

    const wrapper = mountDialog()

    await flushPromises()

    const backdrop = document.body.querySelector<HTMLElement>('[data-workspace-file-dialog]')
    expect(backdrop).not.toBeNull()
    await backdrop?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(wrapper.emitted('close')).toHaveLength(1)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(2)

    wrapper.unmount()
  })

  it('disposes editor instances and models when closed or unmounted', async () => {
    api.fetchConversationWorkspaceFile.mockResolvedValue({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      name: 'app.ts',
      type: 'file',
      content: 'const app = true',
      binary: false,
      size: 42,
    })

    const closingWrapper = mountDialog()

    await flushPromises()
    await flushPromises()

    const closingEditor = monaco.editors[0]
    const closingModel = monaco.models[0]

    await closingWrapper.setProps({ open: false })
    await flushPromises()

    expect(closingEditor.dispose).toHaveBeenCalled()
    expect(closingModel.dispose).toHaveBeenCalled()
    closingWrapper.unmount()

    monaco.editors.length = 0
    monaco.models.length = 0
    const unmountingWrapper = mountDialog()

    await flushPromises()
    await flushPromises()

    const unmountingEditor = monaco.editors[0]
    const unmountingModel = monaco.models[0]

    unmountingWrapper.unmount()

    expect(unmountingEditor.dispose).toHaveBeenCalled()
    expect(unmountingModel.dispose).toHaveBeenCalled()
  })

  it('does not create a stale editor after closing while Monaco is loading', async () => {
    const monacoDeferred = createDeferred<ReturnType<typeof monaco.createMonacoApi>>()
    monaco.getMonaco.mockReturnValueOnce(monacoDeferred.promise)
    api.fetchConversationWorkspaceFile.mockResolvedValue({
      task_id: 'task_1',
      conversation_id: 'conv_1',
      path: 'src/app.ts',
      name: 'app.ts',
      type: 'file',
      content: 'const app = true',
      binary: false,
      size: 42,
    })

    const wrapper = mountDialog()

    await flushPromises()

    expect(monaco.getMonaco).toHaveBeenCalled()

    await wrapper.setProps({ open: false })
    monacoDeferred.resolve(monaco.createMonacoApi())
    await flushPromises()
    await flushPromises()

    expect(monaco.editors).toHaveLength(0)
    expect(monaco.diffEditors).toHaveLength(0)

    wrapper.unmount()
  })
})
