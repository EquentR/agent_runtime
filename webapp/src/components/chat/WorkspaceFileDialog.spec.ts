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
  }> = []
  const diffEditors: Array<{
    dispose: ReturnType<typeof vi.fn>
    layout: ReturnType<typeof vi.fn>
    setModel: ReturnType<typeof vi.fn>
    setValue: ReturnType<typeof vi.fn>
  }> = []
  const models: Array<{
    dispose: ReturnType<typeof vi.fn>
    value: string
    language?: string
  }> = []

  return {
    diffEditors,
    editors,
    getMonaco: vi.fn(),
    models,
  }
})

vi.mock('../../lib/api', () => api)

vi.mock('../../lib/monaco', () => {
  monaco.getMonaco.mockResolvedValue({
    editor: {
      create: vi.fn((_host: HTMLElement, options: Record<string, unknown>) => {
        const editor = {
          dispose: vi.fn(),
          layout: vi.fn(),
          setModel: vi.fn(),
          setValue: vi.fn(),
          options,
        }
        monaco.editors.push(editor)
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
        monaco.diffEditors.push(editor)
        return editor
      }),
      createModel: vi.fn((value: string, language?: string) => {
        const model = {
          dispose: vi.fn(),
          value,
          language,
        }
        monaco.models.push(model)
        return model
      }),
    },
  })

  return {
    getMonaco: monaco.getMonaco,
  }
})

const textNode: WorkspaceTreeNode = {
  path: 'src/app.ts',
  name: 'app.ts',
  type: 'file',
  size: 42,
  has_diff: true,
}

function mountDialog(node: WorkspaceTreeNode | null = textNode) {
  return mount(WorkspaceFileDialog, {
    attachTo: document.body,
    props: {
      conversationId: 'conv_1',
      node,
      open: true,
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
    monaco.getMonaco.mockClear()
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

    const wrapper = mountDialog()

    await flushPromises()

    expect(api.fetchConversationWorkspaceDiff).not.toHaveBeenCalled()

    const diffButton = document.body.querySelector<HTMLButtonElement>('[data-workspace-dialog-mode="diff"]')
    expect(diffButton).not.toBeNull()
    await diffButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    await flushPromises()

    expect(api.fetchConversationWorkspaceDiff).toHaveBeenCalledWith('conv_1', 'src/app.ts')
    expect(monaco.diffEditors).toHaveLength(1)
    expect(monaco.diffEditors[0].setModel).toHaveBeenCalled()

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
})
