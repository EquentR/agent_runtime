import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import WorkspaceBrowserPanel from './WorkspaceBrowserPanel.vue'
import type { WorkspaceSnapshot, WorkspaceTreeNode } from '../../types/api'

const api = vi.hoisted(() => {
  return {
    buildConversationWorkspaceDownloadUrl: vi.fn((conversationId: string, path: string) =>
      '/api/v1/conversations/' + conversationId + '/workspace/download?path=' + encodeURIComponent(path),
    ),
    fetchConversationWorkspaceSnapshot: vi.fn(),
  }
})

vi.mock('../../lib/api', () => api)

vi.mock('./WorkspaceFileDialog.vue', () => ({
  default: {
    name: 'WorkspaceFileDialog',
    props: ['open', 'conversationId', 'node'],
    emits: ['close'],
    template: '<div data-stub-workspace-file-dialog></div>',
  },
}))

function snapshot(tree: WorkspaceTreeNode[], overrides: Partial<WorkspaceSnapshot> = {}): WorkspaceSnapshot {
  return {
    task_id: 'task_1',
    tree,
    ...overrides,
  }
}

function mountPanel(props: Partial<InstanceType<typeof WorkspaceBrowserPanel>['$props']> = {}) {
  return mount(WorkspaceBrowserPanel, {
    props: {
      conversationId: 'conv_1',
      open: true,
      ...props,
    },
  })
}

function getByDataPath(wrapper: ReturnType<typeof mountPanel>, attribute: string, path: string) {
  const element = wrapper.findAll('[' + attribute + ']').find((candidate) => candidate.attributes(attribute) === path)
  if (!element) {
    throw new Error('missing ' + attribute + ' for ' + path)
  }
  return element
}

describe('WorkspaceBrowserPanel', () => {
  beforeEach(() => {
    api.buildConversationWorkspaceDownloadUrl.mockClear()
    api.fetchConversationWorkspaceSnapshot.mockReset()
  })

  it('does not load workspace snapshots until opened', async () => {
    api.fetchConversationWorkspaceSnapshot.mockResolvedValue(snapshot([]))

    const wrapper = mount(WorkspaceBrowserPanel, {
      props: {
        conversationId: 'conv_1',
        open: false,
      },
    })

    await flushPromises()

    expect(api.fetchConversationWorkspaceSnapshot).not.toHaveBeenCalled()

    await wrapper.setProps({ open: true })
    await flushPromises()

    expect(api.fetchConversationWorkspaceSnapshot).toHaveBeenCalledWith('conv_1')
  })

  it('lazy loads folder children on first expansion', async () => {
    api.fetchConversationWorkspaceSnapshot.mockImplementation((_conversationId: string, path?: string) => {
      if (path === 'docs') {
        return Promise.resolve(snapshot([{ path: 'docs/guide.md', name: 'guide.md', type: 'file', has_diff: true }], { path: 'docs' }))
      }

      return Promise.resolve(snapshot([
        { path: 'docs', name: 'docs', type: 'directory', children_loaded: false },
        { path: 'README.md', name: 'README.md', type: 'file' },
      ]))
    })

    const wrapper = mountPanel()

    await flushPromises()

    expect(wrapper.text()).not.toContain('guide.md')

    await getByDataPath(wrapper, 'data-workspace-tree-node', 'docs').trigger('click')
    await flushPromises()

    expect(api.fetchConversationWorkspaceSnapshot).toHaveBeenLastCalledWith('conv_1', 'docs')
    expect(wrapper.text()).toContain('guide.md')
    expect(getByDataPath(wrapper, 'data-workspace-diff-badge', 'docs/guide.md').exists()).toBe(true)
  })

  it('does not render file bodies, diff bodies, or backend root paths in the side panel', async () => {
    api.fetchConversationWorkspaceSnapshot.mockResolvedValue(snapshot([
      {
        path: 'src/app.ts',
        name: 'app.ts',
        type: 'file',
        has_diff: true,
        diff: '@@ -1 +1 @@',
      } as WorkspaceTreeNode & { diff: string },
    ], { home_root: '/private/home', task_root: '/private/task' }))

    const wrapper = mountPanel()

    await flushPromises()

    const panelText = wrapper.get('[data-workspace-browser-panel]').text()
    expect(panelText).toContain('app.ts')
    expect(panelText).not.toContain('@@ -1 +1 @@')
    expect(panelText).not.toContain('/private/home')
    expect(panelText).not.toContain('/private/task')
  })

  it('renders per-item download links for files and folders', async () => {
    api.fetchConversationWorkspaceSnapshot.mockResolvedValue(snapshot([
      { path: 'docs', name: 'docs', type: 'directory', children_loaded: false },
      { path: 'README.md', name: 'README.md', type: 'file' },
    ]))

    const wrapper = mountPanel()

    await flushPromises()

    expect(getByDataPath(wrapper, 'data-workspace-item-download', 'docs').attributes('href')).toBe(
      '/api/v1/conversations/conv_1/workspace/download?path=docs',
    )
    expect(getByDataPath(wrapper, 'data-workspace-item-download', 'README.md').attributes('href')).toBe(
      '/api/v1/conversations/conv_1/workspace/download?path=README.md',
    )
  })

  it('opens the file dialog when a file row is clicked without loading file or diff bodies in the panel', async () => {
    api.fetchConversationWorkspaceSnapshot.mockResolvedValue(snapshot([
      { path: 'README.md', name: 'README.md', type: 'file', has_diff: true },
    ]))

    const wrapper = mountPanel()

    await flushPromises()

    await getByDataPath(wrapper, 'data-workspace-list-item', 'README.md').trigger('click')
    await flushPromises()

    const dialog = wrapper.getComponent({ name: 'WorkspaceFileDialog' })
    expect(dialog.props('open')).toBe(true)
    expect(dialog.props('conversationId')).toBe('conv_1')
    expect(dialog.props('node')).toMatchObject({ path: 'README.md', type: 'file' })
    expect('fetchConversationWorkspaceFile' in api).toBe(false)
    expect('fetchConversationWorkspaceDiff' in api).toBe(false)
  })

  it('does not refetch a folder after it has already loaded', async () => {
    api.fetchConversationWorkspaceSnapshot.mockImplementation((_conversationId: string, path?: string) => {
      if (path === 'docs') {
        return Promise.resolve(snapshot([{ path: 'docs/guide.md', name: 'guide.md', type: 'file' }], { path: 'docs' }))
      }

      return Promise.resolve(snapshot([{ path: 'docs', name: 'docs', type: 'directory', children_loaded: false }]))
    })

    const wrapper = mountPanel()

    await flushPromises()

    await getByDataPath(wrapper, 'data-workspace-tree-node', 'docs').trigger('click')
    await flushPromises()
    expect(api.fetchConversationWorkspaceSnapshot).toHaveBeenCalledTimes(2)

    await getByDataPath(wrapper, 'data-workspace-tree-node', 'docs').trigger('click')
    await flushPromises()
    await getByDataPath(wrapper, 'data-workspace-tree-node', 'docs').trigger('click')
    await flushPromises()

    expect(api.fetchConversationWorkspaceSnapshot).toHaveBeenCalledTimes(2)
  })

  it('resets the loaded tree and search state when the conversation changes', async () => {
    api.fetchConversationWorkspaceSnapshot.mockImplementation((conversationId: string, path?: string) => {
      if (conversationId === 'conv_1' && path === 'docs') {
        return Promise.resolve(snapshot([{ path: 'docs/guide.md', name: 'guide.md', type: 'file' }], { path: 'docs' }))
      }

      if (conversationId === 'conv_2') {
        return Promise.resolve(snapshot([{ path: 'TODO.md', name: 'TODO.md', type: 'file' }], { conversation_id: 'conv_2' }))
      }

      return Promise.resolve(snapshot([
        { path: 'docs', name: 'docs', type: 'directory', children_loaded: false },
        { path: 'README.md', name: 'README.md', type: 'file' },
      ]))
    })

    const wrapper = mountPanel()

    await flushPromises()
    await getByDataPath(wrapper, 'data-workspace-tree-node', 'docs').trigger('click')
    await flushPromises()
    await wrapper.get('.workspace-browser-search-input').setValue('guide')

    expect(wrapper.text()).toContain('guide.md')
    expect((wrapper.get('.workspace-browser-search-input').element as HTMLInputElement).value).toBe('guide')

    await wrapper.setProps({ conversationId: 'conv_2' })
    await flushPromises()

    expect(api.fetchConversationWorkspaceSnapshot).toHaveBeenLastCalledWith('conv_2')
    expect((wrapper.get('.workspace-browser-search-input').element as HTMLInputElement).value).toBe('')
    expect(wrapper.text()).toContain('TODO.md')
    expect(wrapper.text()).not.toContain('guide.md')
  })
})
