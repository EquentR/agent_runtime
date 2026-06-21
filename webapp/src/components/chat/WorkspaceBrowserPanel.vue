<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ArrowRight, Download, Document, FolderOpened, Refresh, Search } from '@element-plus/icons-vue'

import {
  buildConversationWorkspaceDownloadUrl,
  fetchConversationWorkspaceDiff,
  fetchConversationWorkspaceFile,
  fetchConversationWorkspaceSnapshot,
} from '../../lib/api'
import type { WorkspaceDiffResult, WorkspaceFileDetail, WorkspaceSnapshot, WorkspaceTreeNode } from '../../types/api'

interface FlatNode extends WorkspaceTreeNode {
  depth: number
  ancestorPaths: string[]
}

const props = defineProps<{
  conversationId: string
  conversationTitle?: string
}>()

const snapshot = ref<WorkspaceSnapshot | null>(null)
const selectedPath = ref('')
const searchQuery = ref('')
const previewMode = ref<'file' | 'diff'>('file')
const loading = ref(false)
const loadingPreview = ref(false)
const errorMessage = ref('')
const selectedFile = ref<WorkspaceFileDetail | null>(null)
const selectedDiff = ref<WorkspaceDiffResult | null>(null)
const expandedPaths = ref<Set<string>>(new Set())

const flatTree = computed(() => flattenTree(snapshot.value?.tree ?? []))
const filteredTree = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  if (!query) {
    return flatTree.value
  }
  return flatTree.value.filter((node) => `${node.path} ${node.name}`.toLowerCase().includes(query))
})
const selectedNode = computed(() => flatTree.value.find((node) => node.path === selectedPath.value) ?? null)
const displayTitle = computed(() => props.conversationTitle?.trim() || snapshot.value?.task_id || '当前会话工作区')
const downloadUrl = computed(() =>
  selectedPath.value ? buildConversationWorkspaceDownloadUrl(props.conversationId, selectedPath.value) : '',
)
const previewTitle = computed(() => selectedNode.value?.path || '文件预览')
const previewContent = computed(() => (previewMode.value === 'diff' ? selectedDiff.value?.diff ?? '' : selectedFile.value?.content ?? ''))

function flattenTree(nodes: WorkspaceTreeNode[], depth = 0, parentPath = '', ancestorPaths: string[] = []): FlatNode[] {
  return nodes.flatMap((node) => {
    const path = node.path || (parentPath ? `${parentPath}/${node.name}` : node.name)
    const current: FlatNode = { ...node, path, depth, ancestorPaths, children: undefined }
    const children = node.children?.length ? flattenTree(node.children, depth + 1, path, [...ancestorPaths, path]) : []
    return [current, ...children]
  })
}

function nodeIndent(node: FlatNode) {
  return { paddingLeft: `${0.45 + node.depth * 1.05}rem` }
}

function isExpanded(node: FlatNode) {
  return expandedPaths.value.has(node.path)
}

function visibleNode(node: FlatNode) {
  return node.ancestorPaths.every((ancestorPath) => expandedPaths.value.has(ancestorPath))
}

const visibleTree = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  if (!query) {
    return filteredTree.value.filter((node) => visibleNode(node))
  }

  const visiblePaths = new Set<string>()
  for (const node of flatTree.value) {
    const haystack = `${node.path} ${node.name}`.toLowerCase()
    if (haystack.includes(query)) {
      visiblePaths.add(node.path)
      for (const ancestorPath of node.ancestorPaths) {
        visiblePaths.add(ancestorPath)
      }
    }
  }

  return flatTree.value.filter((node) => visiblePaths.has(node.path))
})

function selectNode(node: FlatNode) {
  if (node.type === 'directory') {
    toggleDirectory(node)
    return
  }
  selectedPath.value = node.path
  previewMode.value = 'file'
  void loadPreview(node.path)
}

function toggleDirectory(node: FlatNode) {
  const next = new Set(expandedPaths.value)
  if (next.has(node.path)) {
    next.delete(node.path)
  } else {
    next.add(node.path)
  }
  expandedPaths.value = next
}

async function loadPreview(path: string) {
  if (!props.conversationId || !path) {
    return
  }
  loadingPreview.value = true
  errorMessage.value = ''
  try {
    const [file, diff] = await Promise.all([
      fetchConversationWorkspaceFile(props.conversationId, path),
      fetchConversationWorkspaceDiff(props.conversationId, path),
    ])
    selectedFile.value = file
    selectedDiff.value = diff
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '无法加载文件'
    selectedFile.value = null
    selectedDiff.value = null
  } finally {
    loadingPreview.value = false
  }
}

async function reloadSnapshot() {
  if (!props.conversationId) {
    return
  }
  loading.value = true
  errorMessage.value = ''
  try {
    snapshot.value = await fetchConversationWorkspaceSnapshot(props.conversationId)
    const tree = flattenTree(snapshot.value.tree)
    expandedPaths.value = new Set(tree.filter((node) => node.type === 'directory' && node.depth === 0).map((node) => node.path))
    const firstFile = tree.find((node) => node.type === 'file')
    if (firstFile) {
      selectedPath.value = firstFile.path
      await loadPreview(firstFile.path)
    } else {
      selectedPath.value = ''
      selectedFile.value = null
      selectedDiff.value = null
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '无法加载工作区'
    snapshot.value = null
  } finally {
    loading.value = false
  }
}

function selectPreviewMode(mode: 'file' | 'diff') {
  previewMode.value = mode
}

watch(
  () => props.conversationId,
  () => {
    snapshot.value = null
    selectedPath.value = ''
    searchQuery.value = ''
    previewMode.value = 'file'
    loading.value = false
    loadingPreview.value = false
    errorMessage.value = ''
    selectedFile.value = null
    selectedDiff.value = null
    expandedPaths.value = new Set()
    if (props.conversationId) {
      void reloadSnapshot()
    }
  },
  { immediate: true },
)
</script>

<template>
  <section class="workspace-browser-panel" data-workspace-browser-panel>
    <header class="workspace-browser-header">
      <div class="workspace-browser-title-block">
        <div class="workspace-browser-eyebrow">Workspace</div>
        <strong class="workspace-browser-title" :title="displayTitle">{{ displayTitle }}</strong>
      </div>
      <button class="ghost-button icon-button workspace-browser-icon-button" type="button" aria-label="刷新工作区" @click="reloadSnapshot">
        <Refresh />
      </button>
    </header>

    <div class="workspace-browser-toolbar">
      <label class="workspace-browser-search">
        <Search />
        <input v-model="searchQuery" class="workspace-browser-search-input" type="search" placeholder="搜索文件" />
      </label>
      <div class="workspace-browser-summary">
        <span>{{ snapshot?.home_root || 'home workspace' }}</span>
        <span>{{ snapshot?.task_root || 'task workspace' }}</span>
      </div>
    </div>

    <div class="workspace-browser-body">
      <aside class="workspace-browser-tree">
        <el-scrollbar class="workspace-browser-tree-scrollbar" view-class="workspace-browser-tree-view">
          <button
            v-for="node in visibleTree"
            :key="node.path"
            type="button"
            class="workspace-browser-node"
            :class="{ active: node.path === selectedPath, directory: node.type === 'directory', file: node.type === 'file' }"
            :style="nodeIndent(node)"
            :aria-expanded="node.type === 'directory' ? (isExpanded(node) ? 'true' : 'false') : undefined"
            @click="selectNode(node)"
          >
            <span class="workspace-browser-node-icon" aria-hidden="true">
              <ArrowRight v-if="node.type === 'directory' && !isExpanded(node)" class="workspace-browser-node-toggle" />
              <FolderOpened v-else-if="node.type === 'directory'" />
              <Document v-else />
            </span>
            <span class="workspace-browser-node-path">{{ node.path }}</span>
          </button>
        </el-scrollbar>
      </aside>

      <div class="workspace-browser-preview">
        <div class="workspace-browser-preview-header">
          <strong class="workspace-browser-preview-title">{{ previewTitle }}</strong>
          <div class="workspace-browser-preview-actions">
            <button class="ghost-button workspace-browser-tab" type="button" :class="{ active: previewMode === 'file' }" @click="selectPreviewMode('file')">文件</button>
            <button class="ghost-button workspace-browser-tab" type="button" :class="{ active: previewMode === 'diff' }" @click="selectPreviewMode('diff')">Diff</button>
            <a v-if="downloadUrl" class="ghost-button workspace-browser-download" :href="downloadUrl" target="_blank" rel="noreferrer">
              <Download />
              下载
            </a>
          </div>
        </div>

        <p v-if="errorMessage" class="workspace-browser-status">{{ errorMessage }}</p>
        <p v-else-if="loading || loadingPreview" class="workspace-browser-status">加载中...</p>
        <p v-else-if="!selectedPath" class="workspace-browser-status">从左侧选择一个文件</p>
        <el-scrollbar v-else class="workspace-browser-preview-scrollbar" view-class="workspace-browser-preview-view">
          <pre class="workspace-browser-preview-content">{{ previewContent || '(empty)' }}</pre>
          <p v-if="previewMode === 'diff' && selectedDiff?.truncated" class="workspace-browser-status">Diff 已截断</p>
        </el-scrollbar>
      </div>
    </div>
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
  border-radius: 12px;
  background: var(--app-surface);
}

.workspace-browser-header,
.workspace-browser-toolbar,
.workspace-browser-preview-header {
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
  text-transform: uppercase;
  letter-spacing: 0;
  color: var(--app-text-muted);
}

.workspace-browser-title,
.workspace-browser-preview-title {
  display: block;
  min-width: 0;
  font-size: 0.94rem;
  line-height: 1.3;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.workspace-browser-icon-button,
.workspace-browser-tab,
.workspace-browser-download {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  border-radius: 8px;
}

.workspace-browser-search {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 0.45rem;
  padding: 0.42rem 0.6rem;
  border: 1px solid var(--app-border-subtle);
  border-radius: 8px;
  background: var(--app-input-bg);
}

.workspace-browser-search > svg {
  width: 0.95rem;
  height: 0.95rem;
  flex: 0 0 auto;
  color: var(--app-text-muted);
}

.workspace-browser-search-input {
  width: 100%;
  border: none;
  outline: none;
  background: transparent;
  font: inherit;
  color: var(--app-text);
}

.workspace-browser-summary {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.45rem;
  font-size: 0.78rem;
  color: var(--app-text-muted);
}

.workspace-browser-body {
  flex: 1 1 auto;
  min-height: 0;
  display: grid;
  grid-template-columns: minmax(0, 0.92fr) minmax(0, 1.26fr);
  gap: 0.6rem;
}

.workspace-browser-tree,
.workspace-browser-preview {
  min-width: 0;
  min-height: 0;
  border: 1px solid var(--app-border-subtle);
  border-radius: 10px;
  background: var(--app-surface-strong);
}

.workspace-browser-tree {
  overflow: hidden;
}

.workspace-browser-tree-scrollbar,
.workspace-browser-preview-scrollbar {
  height: 100%;
}

.workspace-browser-tree-view {
  display: grid;
  gap: 0.22rem;
  padding: 0.5rem;
}

.workspace-browser-node {
  display: flex;
  align-items: center;
  gap: 0.38rem;
  min-width: 0;
  width: 100%;
  padding: 0.4rem 0.48rem;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
}

.workspace-browser-node.directory {
  color: var(--app-text-muted);
  font-weight: 650;
}

.workspace-browser-node:hover,
.workspace-browser-node.active {
  background: rgba(var(--app-accent-rgb), 0.08);
  border-color: rgba(var(--app-accent-rgb), 0.15);
}

.workspace-browser-node-icon {
  flex: 0 0 auto;
  width: 1rem;
  height: 1rem;
  display: inline-grid;
  place-items: center;
  color: var(--app-text-muted);
}

.workspace-browser-node-icon svg,
.workspace-browser-download svg,
.workspace-browser-icon-button svg {
  width: 1rem;
  height: 1rem;
  flex: 0 0 auto;
}

.workspace-browser-node-path {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.82rem;
}

.workspace-browser-preview {
  overflow: hidden;
  display: flex;
  flex-direction: column;
  padding: 0.55rem;
}

.workspace-browser-preview-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.35rem;
}

.workspace-browser-tab {
  border: 1px solid var(--app-border-subtle);
}

.workspace-browser-tab.active {
  background: rgba(var(--app-accent-rgb), 0.08);
}

.workspace-browser-download {
  text-decoration: none;
}

.workspace-browser-status {
  margin: 0;
  padding: 0.55rem 0;
  font-size: 0.8rem;
  color: var(--app-text-muted);
}

.workspace-browser-preview-scrollbar {
  min-height: 0;
  flex: 1 1 auto;
}

.workspace-browser-preview-content {
  margin: 0;
  padding: 0.6rem;
  min-height: 100%;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: ui-monospace, SFMono-Regular, Consolas, 'Liberation Mono', monospace;
  font-size: 0.8rem;
  line-height: 1.55;
}

@media (max-width: 960px) {
  .workspace-browser-body {
    grid-template-columns: 1fr;
  }
}
</style>
