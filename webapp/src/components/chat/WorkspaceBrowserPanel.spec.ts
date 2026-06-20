import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import WorkspaceBrowserPanel from './WorkspaceBrowserPanel.vue'

const api = vi.hoisted(() => {
  return {
    buildConversationWorkspaceDownloadUrl: vi.fn((conversationId: string, path: string) =>
      `/api/v1/conversations/${conversationId}/workspace/download?path=${encodeURIComponent(path)}`,
    ),
    fetchConversationWorkspaceDiff: vi.fn(),
    fetchConversationWorkspaceFile: vi.fn(),
    fetchConversationWorkspaceSnapshot: vi.fn(),
  }
})

vi.mock('../../lib/api', () => api)

describe('WorkspaceBrowserPanel', () => {
  beforeEach(() => {
    api.buildConversationWorkspaceDownloadUrl.mockClear()
    api.fetchConversationWorkspaceDiff.mockReset()
    api.fetchConversationWorkspaceFile.mockReset()
    api.fetchConversationWorkspaceSnapshot.mockReset()
  })

  it('loads tree, preview, diff, and download links for a selected file', async () => {
    api.fetchConversationWorkspaceSnapshot.mockResolvedValue({
      task_id: 'task_1',
      home_root: '/home',
      task_root: '/task',
      tree: [
        {
          path: 'src',
          name: 'src',
          type: 'directory',
          children: [
            { path: 'src/app.ts', name: 'app.ts', type: 'file', size: 12 },
          ],
        },
      ],
    })
    api.fetchConversationWorkspaceFile.mockResolvedValue({
      task_id: 'task_1',
      path: 'src/app.ts',
      name: 'app.ts',
      type: 'file',
      content: 'console.log("hi")',
    })
    api.fetchConversationWorkspaceDiff.mockResolvedValue({
      task_id: 'task_1',
      path: 'src/app.ts',
      diff: '@@ -1 +1 @@',
    })

    const wrapper = mount(WorkspaceBrowserPanel, {
      props: {
        conversationId: 'conv_1',
      },
    })

    await flushPromises()

    expect(api.fetchConversationWorkspaceSnapshot).toHaveBeenCalledWith('conv_1')
    expect(wrapper.text()).toContain('src')

    await wrapper.get('.workspace-browser-node.file').trigger('click')
    await flushPromises()

    expect(api.fetchConversationWorkspaceFile).toHaveBeenCalledWith('conv_1', 'src/app.ts')
    expect(api.fetchConversationWorkspaceDiff).toHaveBeenCalledWith('conv_1', 'src/app.ts')
    expect(wrapper.text()).toContain('console.log("hi")')

    await wrapper.get('.workspace-browser-tab:nth-of-type(2)').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('@@ -1 +1 @@')
    expect(wrapper.get('.workspace-browser-download').attributes('href')).toBe(
      '/api/v1/conversations/conv_1/workspace/download?path=src%2Fapp.ts',
    )
  })

  it('filters tree rows by search query', async () => {
    api.fetchConversationWorkspaceSnapshot.mockResolvedValue({
      task_id: 'task_1',
      home_root: '/home',
      task_root: '/task',
      tree: [
        { path: 'README.md', name: 'README.md', type: 'file' },
        { path: 'docs', name: 'docs', type: 'directory', children: [{ path: 'docs/guide.md', name: 'guide.md', type: 'file' }] },
      ],
    })
    api.fetchConversationWorkspaceFile.mockResolvedValue({
      task_id: 'task_1',
      path: 'README.md',
      name: 'README.md',
      type: 'file',
      content: 'readme',
    })
    api.fetchConversationWorkspaceDiff.mockResolvedValue({
      task_id: 'task_1',
      path: 'README.md',
      diff: 'diff',
    })

    const wrapper = mount(WorkspaceBrowserPanel, {
      props: {
        conversationId: 'conv_1',
      },
    })

    await flushPromises()
    await wrapper.get('.workspace-browser-search-input').setValue('guide')
    await flushPromises()

    const treeText = wrapper.find('.workspace-browser-tree').text()
    expect(treeText).toContain('guide.md')
    expect(treeText).not.toContain('README.md')
  })
})
