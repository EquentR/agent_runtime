<script setup lang='ts'>
import { computed, ref, watch } from 'vue'
import { ArrowRight, Download, Document, FolderOpened, Refresh, Search } from '@element-plus/icons-vue'

import WorkspaceFileDialog from './WorkspaceFileDialog.vue'
import { buildConversationWorkspaceDownloadUrl, fetchConversationWorkspaceSnapshot } from '../../lib/api'
import type { WorkspaceSnapshot, WorkspaceTreeNode } from '../../types/api'

interface FlatNode extends WorkspaceTreeNode {
  depth: number
  ancestorPaths: string[]
}

const ROOT_DIRECTORY = ''

const props = defineProps<{
  conversationId: string
  conversationTitle?: string
  open?: boolean
}>()

const rootNodes = ref<WorkspaceTreeNode[]>([])
const searchQuery = ref('')
const loadingRoot = ref(false)
const errorMessage = ref('')
const expandedPaths = ref<Set<string>>(new Set())
const loadedDirectoryPaths = ref<Set<string>>(new Set())
const loadingDirectoryPaths = ref<Set<string>>(new Set())
const dialogOpen = ref(false)
const dialogNode = ref<WorkspaceTreeNode | null>(null)

let rootLoadToken = 0

const normalizedQuery = computed(() => searchQuery.value.trim().toLowerCase())
const flatTree = computed(() => flattenTree(rootNodes.value))
const visibleTree = computed(() => {
  if (!normalizedQuery.value) {
    return flatTree.value.filter((node) => node.ancestorPaths.every((ancestorPath) => expandedPaths.value.has(ancestorPath)))
  }

  const visiblePaths = new Set<string>()
  for (const node of flatTree.value) {
    if (!nodeMatchesQuery(node, normalizedQuery.value)) {
      continue
    }
    visiblePaths.add(node.path)
    for (const ancestorPath of node.ancestorPaths) {
      visiblePaths.add(ancestorPath)
    }
  }
  return flatTree.value.filter((node) => visiblePaths.has(node.path))
})
const displayTitle = computed(() => props.conversationTitle?.trim() || 'Workspace')

function normalizeNode(node: WorkspaceTreeNode, parentPath = ROOT_DIRECTORY): WorkspaceTreeNode {
  const path = node.path || (parentPath ? parentPath + '/' + node.name : node.name)
  const name = node.name || path.split('/').pop() || path
  const children = node.children?.map((child) => normalizeNode(child, path))
  return {
    ...node,
    path,
    name,
    children: children && children.length > 0 ? children : undefined,
  }
}

function normalizeNodes(nodes: WorkspaceTreeNode[], parentPath = ROOT_DIRECTORY) {
  return nodes.map((node) => normalizeNode(node, parentPath))
}

function flattenTree(nodes: WorkspaceTreeNode[], depth = 0, ancestorPaths: string[] = []): FlatNode[] {
  return nodes.flatMap((node) => {
    const current: FlatNode = { ...node, depth, ancestorPaths }
    const children = node.children?.length ? flattenTree(node.children, depth + 1, [...ancestorPaths, node.path]) : []
    return [current, ...children]
  })
}

function nodeMatchesQuery(node: WorkspaceTreeNode, query: string) {
  return (node.path + ' ' + node.name).toLowerCase().includes(query)
}

function collectLoadedDirectoryPaths(nodes: WorkspaceTreeNode[], paths = new Set<string>([ROOT_DIRECTORY])) {
  for (const node of nodes) {
    if (node.type !== 'directory') {
      continue
    }
    const hasPreloadedChildren = node.children_loaded !== false && Boolean(node.children?.length)
    if (node.children_loaded === true || hasPreloadedChildren) {
      paths.add(node.path)
    }
    if (node.children?.length) {
      collectLoadedDirectoryPaths(node.children, paths)
    }
  }
  return paths
}

function isDirectoryLoaded(node: WorkspaceTreeNode) {
  const hasPreloadedChildren = node.children_loaded !== false && Boolean(node.children?.length)
  return loadedDirectoryPaths.value.has(node.path) || node.children_loaded === true || hasPreloadedChildren
}

function isDirectoryLoading(path: string) {
  return loadingDirectoryPaths.value.has(path)
}

function isExpanded(path: string) {
  return expandedPaths.value.has(path)
}

function nodeIndent(node: FlatNode) {
  return { paddingLeft: 0.5 + node.depth * 1 + 'rem' }
}

function downloadUrl(path: string) {
  return buildConversationWorkspaceDownloadUrl(props.conversationId, path)
}

function replaceDirectoryChildren(nodes: WorkspaceTreeNode[], path: string, children: WorkspaceTreeNode[]): WorkspaceTreeNode[] {
  return nodes.map((node) => {
    if (node.path === path) {
      return {
        ...node,
        children,
        children_loaded: true,
      }
    }
    if (!node.children?.length) {
      return node
    }
    return {
      ...node,
      children: replaceDirectoryChildren(node.children, path, children),
    }
  })
}

function setExpanded(path: string, expanded: boolean) {
  const next = new Set(expandedPaths.value)
  if (expanded) {
    next.add(path)
  } else {
    next.delete(path)
  }
  expandedPaths.value = next
}

function markDirectoryLoaded(path: string) {
  loadedDirectoryPaths.value = new Set(loadedDirectoryPaths.value).add(path)
}

function setDirectoryLoading(path: string, loading: boolean) {
  const next = new Set(loadingDirectoryPaths.value)
  if (loading) {
    next.add(path)
  } else {
    next.delete(path)
  }
  loadingDirectoryPaths.value = next
}

function resetBrowserState() {
  rootLoadToken += 1
  rootNodes.value = []
  searchQuery.value = ''
  loadingRoot.value = false
  errorMessage.value = ''
  expandedPaths.value = new Set()
  loadedDirectoryPaths.value = new Set()
  loadingDirectoryPaths.value = new Set()
  dialogOpen.value = false
  dialogNode.value = null
}

async function loadRootSnapshot() {
  if (!props.open || !props.conversationId) {
    return
  }

  const token = ++rootLoadToken
  loadingRoot.value = true
  errorMessage.value = ''
  try {
    const workspaceSnapshot: WorkspaceSnapshot = await fetchConversationWorkspaceSnapshot(props.conversationId)
    if (token !== rootLoadToken) {
      return
    }
    const nodes = normalizeNodes(workspaceSnapshot.tree)
    rootNodes.value = nodes
    expandedPaths.value = new Set()
    loadedDirectoryPaths.value = collectLoadedDirectoryPaths(nodes)
    loadingDirectoryPaths.value = new Set()
    dialogOpen.value = false
    dialogNode.value = null
  } catch (error) {
    if (token !== rootLoadToken) {
      return
    }
    rootNodes.value = []
    loadedDirectoryPaths.value = new Set()
    errorMessage.value = error instanceof Error ? error.message : 'Failed to load workspace'
  } finally {
    if (token === rootLoadToken) {
      loadingRoot.value = false
    }
  }
}

async function loadDirectory(node: WorkspaceTreeNode) {
  if (node.type !== 'directory' || !props.conversationId || isDirectoryLoaded(node) || isDirectoryLoading(node.path)) {
    if (node.type === 'directory' && isDirectoryLoaded(node)) {
      markDirectoryLoaded(node.path)
    }
    return
  }

  const token = rootLoadToken
  const conversationId = props.conversationId
  setDirectoryLoading(node.path, true)
  errorMessage.value = ''
  try {
    const workspaceSnapshot: WorkspaceSnapshot = await fetchConversationWorkspaceSnapshot(conversationId, node.path)
    if (token !== rootLoadToken || conversationId !== props.conversationId) {
      return
    }
    const children = normalizeNodes(workspaceSnapshot.tree, node.path)
    rootNodes.value = replaceDirectoryChildren(rootNodes.value, node.path, children)
    loadedDirectoryPaths.value = collectLoadedDirectoryPaths(rootNodes.value).add(node.path)
  } catch (error) {
    if (token === rootLoadToken) {
      errorMessage.value = error instanceof Error ? error.message : 'Failed to load directory'
    }
  } finally {
    if (token === rootLoadToken && conversationId === props.conversationId) {
      setDirectoryLoading(node.path, false)
    }
  }
}

async function toggleDirectory(node: WorkspaceTreeNode) {
  if (node.type !== 'directory') {
    return
  }
  const nextExpanded = !expandedPaths.value.has(node.path)
  setExpanded(node.path, nextExpanded)
  if (nextExpanded) {
    await loadDirectory(node)
  }
}

function openFile(node: WorkspaceTreeNode) {
  if (node.type !== 'file') {
    return
  }
  dialogNode.value = node
  dialogOpen.value = true
}

function closeDialog() {
  dialogOpen.value = false
  dialogNode.value = null
}

function handleNodeClick(node: WorkspaceTreeNode) {
  if (node.type === 'directory') {
    void toggleDirectory(node)
    return
  }
  openFile(node)
}

watch(
  () => props.conversationId,
  () => {
    resetBrowserState()
    if (props.open) {
      void loadRootSnapshot()
    }
  },
  { immediate: true },
)

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen && props.conversationId && !loadedDirectoryPaths.value.has(ROOT_DIRECTORY) && !loadingRoot.value) {
      void loadRootSnapshot()
    }
  },
)
</script>

<template>
  <section class='workspace-browser-panel' data-workspace-browser-panel>
    <header class='workspace-browser-header'>
      <div class='workspace-browser-title-block'>
        <div class='workspace-browser-eyebrow'>Workspace</div>
        <strong class='workspace-browser-title' :title='displayTitle'>{{ displayTitle }}</strong>
      </div>
      <button class='ghost-button icon-button workspace-browser-icon-button' type='button' aria-label='Refresh workspace' @click='loadRootSnapshot'>
        <Refresh />
      </button>
    </header>

    <div class='workspace-browser-toolbar'>
      <label class='workspace-browser-search'>
        <Search />
        <input v-model='searchQuery' class='workspace-browser-search-input' type='search' placeholder='Search loaded files' />
      </label>
    </div>

    <p v-if='errorMessage' class='workspace-browser-status error'>{{ errorMessage }}</p>
    <p v-else-if='loadingRoot' class='workspace-browser-status'>Loading workspace...</p>

    <div class='workspace-browser-body'>
      <div class='workspace-browser-tree'>
        <el-scrollbar class='workspace-browser-tree-scrollbar' view-class='workspace-browser-tree-view'>
          <div v-if='visibleTree.length === 0' class='workspace-browser-empty'>No loaded items</div>
          <div
            v-for='node in visibleTree'
            :key='node.path'
            class='workspace-browser-row'
            :style='nodeIndent(node)'
          >
            <button
              type='button'
              class='workspace-browser-node'
              :class='{ directory: node.type === `directory`, file: node.type === `file` }'
              :data-workspace-tree-node='node.path'
              :data-workspace-list-item='node.path'
              :aria-expanded='node.type === `directory` ? (isExpanded(node.path) ? `true` : `false`) : undefined'
              @click='handleNodeClick(node)'
            >
              <span class='workspace-browser-node-icon' aria-hidden='true'>
                <ArrowRight v-if='node.type === `directory` && !isExpanded(node.path)' class='workspace-browser-node-toggle' />
                <FolderOpened v-else-if='node.type === `directory`' />
                <Document v-else />
              </span>
              <span class='workspace-browser-node-text'>
                <span class='workspace-browser-node-name' :title='node.name'>{{ node.name }}</span>
                <span class='workspace-browser-node-path' :title='node.path'>{{ node.path }}</span>
              </span>
              <span class='workspace-browser-node-kind'>{{ node.type === `directory` ? `Folder` : `File` }}</span>
              <span v-if='node.has_diff' class='workspace-browser-diff-badge' :data-workspace-diff-badge='node.path'>Diff</span>
              <span v-if='node.type === `directory` && isDirectoryLoading(node.path)' class='workspace-browser-loading'>Loading</span>
            </button>
            <a
              class='workspace-browser-item-download'
              :href='downloadUrl(node.path)'
              target='_blank'
              rel='noreferrer'
              :data-workspace-item-download='node.path'
              :aria-label='`Download ${node.path}`'
              @click.stop
            >
              <Download />
            </a>
          </div>
        </el-scrollbar>
      </div>
    </div>

    <WorkspaceFileDialog
      :open='dialogOpen'
      :conversation-id='props.conversationId'
      :node='dialogNode'
      @close='closeDialog'
    />
  </section>
</template>

<style scoped>
.workspace-browser-panel {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  padding: 0.7rem;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface);
}

.workspace-browser-header,
.workspace-browser-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.6rem;
  min-width: 0;
}

.workspace-browser-title-block {
  min-width: 0;
}

.workspace-browser-eyebrow {
  font-size: 0.72rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0;
  color: var(--app-text-muted);
}

.workspace-browser-title {
  display: block;
  min-width: 0;
  overflow: hidden;
  font-size: 0.94rem;
  line-height: 1.3;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.workspace-browser-icon-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
}

.workspace-browser-search {
  flex: 1 1 auto;
  min-width: 0;
  height: 2.15rem;
  display: flex;
  align-items: center;
  gap: 0.45rem;
  padding: 0 0.6rem;
  border: 1px solid var(--app-border-subtle);
  border-radius: 8px;
  background: var(--app-input-bg);
}

.workspace-browser-search-input {
  width: 100%;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--app-text);
  font: inherit;
}

.workspace-browser-body {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.workspace-browser-tree {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--app-border-subtle);
  border-radius: 8px;
  background: var(--app-surface-strong);
  display: flex;
  flex-direction: column;
}

.workspace-browser-tree-scrollbar {
  min-height: 0;
  flex: 1 1 auto;
}

.workspace-browser-tree-view {
  display: grid;
  gap: 0.24rem;
  padding: 0.45rem;
}

.workspace-browser-row {
  min-width: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 2rem;
  align-items: center;
  gap: 0.25rem;
}

.workspace-browser-node,
.workspace-browser-item-download {
  border: 1px solid transparent;
  background: transparent;
  color: inherit;
}

.workspace-browser-node {
  min-width: 0;
  height: 2.85rem;
  display: grid;
  grid-template-columns: 1.75rem minmax(0, 1fr) auto auto auto;
  align-items: center;
  gap: 0.4rem;
  padding: 0 0.45rem;
  border-radius: 8px;
  cursor: pointer;
  text-align: left;
}

.workspace-browser-node.directory {
  font-weight: 650;
}

.workspace-browser-node:hover {
  border-color: rgba(var(--app-accent-rgb), 0.16);
  background: rgba(var(--app-accent-rgb), 0.08);
}

.workspace-browser-node-icon,
.workspace-browser-item-download {
  flex: 0 0 auto;
  width: 1.75rem;
  height: 1.75rem;
  display: inline-grid;
  place-items: center;
  color: var(--app-text-muted);
}

.workspace-browser-node-icon svg,
.workspace-browser-item-download svg,
.workspace-browser-search svg,
.workspace-browser-icon-button svg {
  width: 1rem;
  height: 1rem;
}

.workspace-browser-node-toggle {
  transform: rotate(0deg);
}

.workspace-browser-node-text {
  min-width: 0;
  display: grid;
  gap: 0.12rem;
}

.workspace-browser-node-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.86rem;
  font-weight: 650;
}

.workspace-browser-node-path {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.74rem;
  color: var(--app-text-muted);
}

.workspace-browser-node-kind,
.workspace-browser-loading,
.workspace-browser-diff-badge {
  flex: 0 0 auto;
  border-radius: 999px;
  font-size: 0.7rem;
  font-weight: 700;
  line-height: 1;
  white-space: nowrap;
}

.workspace-browser-node-kind,
.workspace-browser-loading {
  color: var(--app-text-muted);
}

.workspace-browser-diff-badge {
  padding: 0.22rem 0.34rem;
  background: rgba(var(--app-accent-rgb), 0.12);
  color: var(--app-accent);
}

.workspace-browser-item-download {
  border-radius: 8px;
  text-decoration: none;
}

.workspace-browser-item-download:hover {
  border-color: rgba(var(--app-accent-rgb), 0.16);
  background: rgba(var(--app-accent-rgb), 0.08);
}

.workspace-browser-status,
.workspace-browser-empty {
  margin: 0;
  padding: 0.55rem;
  font-size: 0.8rem;
  color: var(--app-text-muted);
}

.workspace-browser-status.error {
  color: #b24343;
}
</style>
