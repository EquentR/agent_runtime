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
const selectedDirectoryPath = ref(ROOT_DIRECTORY)
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
const selectedDirectoryNode = computed(() =>
  selectedDirectoryPath.value ? flatTree.value.find((node) => node.path === selectedDirectoryPath.value && node.type === 'directory') ?? null : null,
)
const currentDirectoryItems = computed(() => {
  const nodes = selectedDirectoryPath.value ? selectedDirectoryNode.value?.children ?? [] : rootNodes.value
  return filterNodes(nodes, normalizedQuery.value)
})
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
const currentDirectoryLabel = computed(() => selectedDirectoryPath.value || 'Workspace root')

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

function filterNodes(nodes: WorkspaceTreeNode[], query: string) {
  if (!query) {
    return nodes
  }
  return nodes.filter((node) => nodeMatchesQuery(node, query))
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
  selectedDirectoryPath.value = ROOT_DIRECTORY
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
    selectedDirectoryPath.value = ROOT_DIRECTORY
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
  selectedDirectoryPath.value = node.path
  dialogOpen.value = false
  dialogNode.value = null
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

function selectItem(node: WorkspaceTreeNode) {
  if (node.type === 'directory') {
    void toggleDirectory(node)
    return
  }
  openFile(node)
}

function selectRootDirectory() {
  selectedDirectoryPath.value = ROOT_DIRECTORY
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
      <aside class='workspace-browser-tree'>
        <button
          type='button'
          class='workspace-browser-root-button'
          :class='{ active: selectedDirectoryPath === ROOT_DIRECTORY }'
          @click='selectRootDirectory'
        >
          <FolderOpened />
          <span>Workspace root</span>
        </button>

        <el-scrollbar class='workspace-browser-tree-scrollbar' view-class='workspace-browser-tree-view'>
          <button
            v-for='node in visibleTree'
            :key='node.path'
            type='button'
            class='workspace-browser-node'
            :class='{ active: node.path === selectedDirectoryPath, directory: node.type === `directory`, file: node.type === `file` }'
            :style='nodeIndent(node)'
            :data-workspace-tree-node='node.path'
            :aria-expanded='node.type === `directory` ? (isExpanded(node.path) ? `true` : `false`) : undefined'
            @click='node.type === `directory` ? toggleDirectory(node) : openFile(node)'
          >
            <span class='workspace-browser-node-icon' aria-hidden='true'>
              <ArrowRight v-if='node.type === `directory` && !isExpanded(node.path)' class='workspace-browser-node-toggle' />
              <FolderOpened v-else-if='node.type === `directory`' />
              <Document v-else />
            </span>
            <span class='workspace-browser-node-path' :title='node.path'>{{ node.path }}</span>
            <span v-if='node.has_diff' class='workspace-browser-diff-badge' :data-workspace-diff-badge='node.path'>Diff</span>
          </button>
        </el-scrollbar>
      </aside>

      <section class='workspace-browser-list'>
        <header class='workspace-browser-list-header'>
          <div class='workspace-browser-list-title-block'>
            <strong class='workspace-browser-list-title' :title='currentDirectoryLabel'>{{ currentDirectoryLabel }}</strong>
            <span class='workspace-browser-list-count'>{{ currentDirectoryItems.length }} items</span>
          </div>
        </header>

        <el-scrollbar class='workspace-browser-list-scrollbar' view-class='workspace-browser-list-view'>
          <div v-if='currentDirectoryItems.length === 0' class='workspace-browser-empty'>No loaded items</div>
          <div v-for='node in currentDirectoryItems' :key='node.path' class='workspace-browser-list-row'>
            <button
              type='button'
              class='workspace-browser-list-item'
              :data-workspace-list-item='node.path'
              @click='selectItem(node)'
            >
              <span class='workspace-browser-list-icon' aria-hidden='true'>
                <FolderOpened v-if='node.type === `directory`' />
                <Document v-else />
              </span>
              <span class='workspace-browser-list-text'>
                <span class='workspace-browser-list-name' :title='node.name'>{{ node.name }}</span>
                <span class='workspace-browser-list-path' :title='node.path'>{{ node.path }}</span>
              </span>
              <span class='workspace-browser-list-kind'>{{ node.type === `directory` ? `Folder` : `File` }}</span>
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
      </section>
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
.workspace-browser-toolbar,
.workspace-browser-list-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.6rem;
  min-width: 0;
}

.workspace-browser-title-block,
.workspace-browser-list-title-block {
  min-width: 0;
}

.workspace-browser-eyebrow {
  font-size: 0.72rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0;
  color: var(--app-text-muted);
}

.workspace-browser-title,
.workspace-browser-list-title {
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
  display: grid;
  grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.25fr);
  gap: 0.6rem;
}

.workspace-browser-tree,
.workspace-browser-list {
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--app-border-subtle);
  border-radius: 8px;
  background: var(--app-surface-strong);
}

.workspace-browser-tree,
.workspace-browser-list {
  display: flex;
  flex-direction: column;
}

.workspace-browser-root-button,
.workspace-browser-node,
.workspace-browser-list-item,
.workspace-browser-item-download {
  border: 1px solid transparent;
  background: transparent;
  color: inherit;
}

.workspace-browser-root-button,
.workspace-browser-node,
.workspace-browser-list-item {
  min-width: 0;
  cursor: pointer;
  text-align: left;
}

.workspace-browser-root-button {
  height: 2.2rem;
  display: flex;
  align-items: center;
  gap: 0.4rem;
  margin: 0.45rem 0.45rem 0;
  padding: 0 0.55rem;
  border-radius: 8px;
  font-weight: 650;
}

.workspace-browser-tree-scrollbar,
.workspace-browser-list-scrollbar {
  min-height: 0;
  flex: 1 1 auto;
}

.workspace-browser-tree-view,
.workspace-browser-list-view {
  display: grid;
  gap: 0.24rem;
  padding: 0.45rem;
}

.workspace-browser-node {
  width: 100%;
  height: 2.05rem;
  display: flex;
  align-items: center;
  gap: 0.36rem;
  padding-right: 0.42rem;
  border-radius: 8px;
}

.workspace-browser-node.directory {
  font-weight: 650;
}

.workspace-browser-root-button:hover,
.workspace-browser-root-button.active,
.workspace-browser-node:hover,
.workspace-browser-node.active,
.workspace-browser-list-item:hover {
  border-color: rgba(var(--app-accent-rgb), 0.16);
  background: rgba(var(--app-accent-rgb), 0.08);
}

.workspace-browser-node-icon,
.workspace-browser-list-icon,
.workspace-browser-item-download {
  flex: 0 0 auto;
  width: 1.75rem;
  height: 1.75rem;
  display: inline-grid;
  place-items: center;
  color: var(--app-text-muted);
}

.workspace-browser-node-icon svg,
.workspace-browser-list-icon svg,
.workspace-browser-item-download svg,
.workspace-browser-search svg,
.workspace-browser-icon-button svg,
.workspace-browser-root-button svg {
  width: 1rem;
  height: 1rem;
}

.workspace-browser-node-toggle {
  transform: rotate(0deg);
}

.workspace-browser-node-path,
.workspace-browser-list-name,
.workspace-browser-list-path,
.workspace-browser-list-title,
.workspace-browser-title,
.workspace-browser-root-button span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.workspace-browser-node-path {
  flex: 1 1 auto;
  font-size: 0.82rem;
}

.workspace-browser-list {
  padding: 0.55rem;
}

.workspace-browser-list-count {
  display: block;
  margin-top: 0.12rem;
  font-size: 0.76rem;
  color: var(--app-text-muted);
}

.workspace-browser-list-row {
  min-width: 0;
  height: 2.85rem;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 2rem;
  align-items: center;
  gap: 0.25rem;
}

.workspace-browser-list-item {
  min-width: 0;
  height: 2.7rem;
  display: grid;
  grid-template-columns: 1.75rem minmax(0, 1fr) auto auto auto;
  align-items: center;
  gap: 0.4rem;
  padding: 0 0.45rem;
  border-radius: 8px;
}

.workspace-browser-list-text {
  min-width: 0;
  display: grid;
  gap: 0.12rem;
}

.workspace-browser-list-name {
  font-size: 0.86rem;
  font-weight: 650;
}

.workspace-browser-list-path {
  font-size: 0.74rem;
  color: var(--app-text-muted);
}

.workspace-browser-list-kind,
.workspace-browser-loading,
.workspace-browser-diff-badge {
  flex: 0 0 auto;
  border-radius: 999px;
  font-size: 0.7rem;
  font-weight: 700;
  line-height: 1;
  white-space: nowrap;
}

.workspace-browser-list-kind,
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

@media (max-width: 960px) {
  .workspace-browser-body {
    grid-template-columns: 1fr;
  }
}
</style>
