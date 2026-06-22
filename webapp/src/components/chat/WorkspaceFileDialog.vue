<script setup lang="ts">
import { Close, Download } from '@element-plus/icons-vue'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'

import { buildConversationWorkspaceDownloadUrl, fetchConversationWorkspaceDiff, fetchConversationWorkspaceFile } from '../../lib/api'
import { getMonaco } from '../../lib/monaco'
import type { WorkspaceDiffResult, WorkspaceFileDetail, WorkspaceTreeNode } from '../../types/api'

type ViewMode = 'file' | 'diff'
type MonacoApi = Awaited<ReturnType<typeof getMonaco>>
type MonacoModel = ReturnType<MonacoApi['editor']['createModel']>
type MonacoEditor = ReturnType<MonacoApi['editor']['create']>
type MonacoDiffEditor = ReturnType<MonacoApi['editor']['createDiffEditor']>
interface RenderToken {
  sessionId: number
  renderId: number
  mode: ViewMode
  path: string
}

const props = defineProps<{
  open: boolean
  conversationId: string
  node: WorkspaceTreeNode | null
  initialMode?: 'file' | 'diff'
}>()

const emit = defineEmits<{ close: [] }>()

const titleId = `workspace-file-dialog-title-${Math.random().toString(36).slice(2)}`
const editorHost = ref<HTMLDivElement | null>(null)
const mode = ref<ViewMode>(props.initialMode ?? 'file')
const fileDetail = ref<WorkspaceFileDetail | null>(null)
const diffDetail = ref<WorkspaceDiffResult | null>(null)
const fileLoading = ref(false)
const diffLoading = ref(false)
const fileError = ref('')
const diffError = ref('')

let sessionId = 0
let renderId = 0
let escapeListenerAttached = false
let bodyOverflowBefore = ''
let monacoEditor: MonacoEditor | MonacoDiffEditor | null = null
let monacoModels: MonacoModel[] = []

const filePath = computed(() => props.node?.type === 'file' ? props.node.path : '')
const displayTitle = computed(() => filePath.value || 'Workspace file')
const hasFileNode = computed(() => Boolean(filePath.value))
const activeBinary = computed(() => {
  if (mode.value === 'diff') {
    return Boolean(diffDetail.value?.binary ?? props.node?.binary)
  }
  return Boolean(fileDetail.value?.binary ?? props.node?.binary)
})
const downloadUrl = computed(() => (filePath.value ? buildConversationWorkspaceDownloadUrl(props.conversationId, filePath.value) : ''))
const displaySize = computed(() => formatSize(fileDetail.value?.size ?? props.node?.size))
const showEditorHost = computed(() => {
  if (!hasFileNode.value || activeBinary.value) {
    return false
  }
  if (mode.value === 'file') {
    return Boolean(fileDetail.value)
  }
  return Boolean(diffDetail.value)
})
const statusMessage = computed(() => {
  if (!hasFileNode.value) {
    return 'No file selected'
  }
  if (fileLoading.value) {
    return 'Loading file...'
  }
  if (fileError.value) {
    return fileError.value
  }
  if (mode.value === 'diff' && diffLoading.value && !diffDetail.value) {
    return 'Loading diff...'
  }
  if (mode.value === 'diff' && diffError.value) {
    return diffError.value
  }
  if (mode.value === 'diff' && !diffDetail.value) {
    return 'Diff not loaded'
  }
  return ''
})

function lockBodyScroll() {
  if (escapeListenerAttached) {
    return
  }
  bodyOverflowBefore = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  window.addEventListener('keydown', handleKeydown)
  escapeListenerAttached = true
}

function unlockBodyScroll() {
  if (!escapeListenerAttached) {
    return
  }
  window.removeEventListener('keydown', handleKeydown)
  document.body.style.overflow = bodyOverflowBefore
  bodyOverflowBefore = ''
  escapeListenerAttached = false
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    emit('close')
  }
}

function resetEditorState() {
  monacoEditor?.dispose()
  monacoEditor = null
  for (const model of monacoModels) {
    model.dispose()
  }
  monacoModels = []
  if (editorHost.value) {
    editorHost.value.innerHTML = ''
  }
}

function invalidateRender() {
  renderId += 1
}

function createRenderToken(expectedMode: ViewMode): RenderToken {
  return {
    mode: expectedMode,
    path: filePath.value,
    renderId,
    sessionId,
  }
}

function isRenderTokenCurrent(token: RenderToken) {
  return props.open
    && hasFileNode.value
    && token.sessionId === sessionId
    && token.renderId === renderId
    && token.mode === mode.value
    && token.path === filePath.value
}

function resetSessionState() {
  fileDetail.value = null
  diffDetail.value = null
  fileLoading.value = false
  diffLoading.value = false
  fileError.value = ''
  diffError.value = ''
  mode.value = props.initialMode ?? 'file'
}

function disposeCurrentSession() {
  sessionId += 1
  invalidateRender()
  resetEditorState()
  resetSessionState()
}

function formatSize(size?: number) {
  if (typeof size !== 'number' || !Number.isFinite(size) || size < 0) {
    return ''
  }
  if (size < 1024) {
    return `${size} B`
  }
  const kib = size / 1024
  if (kib < 1024) {
    return `${Number.isInteger(kib) ? kib : kib.toFixed(1)} KB`
  }
  const mib = kib / 1024
  return `${Number.isInteger(mib) ? mib : mib.toFixed(1)} MB`
}

function guessLanguage(path: string, mimeType?: string) {
  const extension = path.split('.').pop()?.toLowerCase() ?? ''
  if (mimeType === 'application/json' || extension === 'json') {
    return 'json'
  }
  if (extension === 'ts' || extension === 'tsx') {
    return 'typescript'
  }
  if (extension === 'js' || extension === 'mjs' || extension === 'cjs') {
    return 'javascript'
  }
  if (extension === 'css' || extension === 'scss' || extension === 'less') {
    return 'css'
  }
  if (extension === 'html' || extension === 'htm') {
    return 'html'
  }
  if (extension === 'md' || extension === 'markdown') {
    return 'markdown'
  }
  if (extension === 'yaml' || extension === 'yml') {
    return 'yaml'
  }
  if (extension === 'go') {
    return 'go'
  }
  return 'plaintext'
}

async function renderFileView(token: RenderToken = createRenderToken('file')) {
  if (!isRenderTokenCurrent(token) || activeBinary.value || !fileDetail.value) {
    if (isRenderTokenCurrent(token)) {
      resetEditorState()
    }
    return
  }

  await nextTick()
  if (!isRenderTokenCurrent(token) || activeBinary.value || !fileDetail.value) {
    return
  }

  const host = editorHost.value
  if (!host) {
    resetEditorState()
    return
  }

  resetEditorState()
  const monaco = await getMonaco()
  if (!isRenderTokenCurrent(token) || activeBinary.value || !fileDetail.value || editorHost.value !== host) {
    return
  }

  const detail = fileDetail.value
  const editor = monaco.editor.create(host, {
    automaticLayout: true,
    fontSize: 13,
    minimap: { enabled: false },
    readOnly: true,
    renderLineHighlightOnlyWhenFocus: true,
    scrollBeyondLastLine: false,
    wordWrap: 'off',
  })
  const model = monaco.editor.createModel(detail.content ?? '', guessLanguage(detail.path, detail.mime_type))
  editor.setModel(model)
  monacoEditor = editor
  monacoModels = [model]
  editor.layout()
}

async function renderDiffView(token: RenderToken = createRenderToken('diff')) {
  if (!isRenderTokenCurrent(token) || activeBinary.value || !diffDetail.value) {
    if (isRenderTokenCurrent(token)) {
      resetEditorState()
    }
    return
  }

  await nextTick()
  if (!isRenderTokenCurrent(token) || activeBinary.value || !diffDetail.value) {
    return
  }

  const host = editorHost.value
  if (!host) {
    resetEditorState()
    return
  }

  resetEditorState()
  const monaco = await getMonaco()
  if (!isRenderTokenCurrent(token) || activeBinary.value || !diffDetail.value || editorHost.value !== host) {
    return
  }

  const detail = diffDetail.value
  if (detail.home_content !== undefined && detail.task_content !== undefined) {
    const editor = monaco.editor.createDiffEditor(host, {
      automaticLayout: true,
      fontSize: 13,
      minimap: { enabled: false },
      readOnly: true,
      renderSideBySide: true,
      scrollBeyondLastLine: false,
    })
    const original = monaco.editor.createModel(detail.home_content, guessLanguage(detail.path))
    const modified = monaco.editor.createModel(detail.task_content, guessLanguage(detail.path))
    editor.setModel({ original, modified })
    monacoEditor = editor
    monacoModels = [original, modified]
    editor.layout()
    return
  }

  const editor = monaco.editor.create(host, {
    automaticLayout: true,
    fontSize: 13,
    minimap: { enabled: false },
    readOnly: true,
    renderLineHighlightOnlyWhenFocus: true,
    scrollBeyondLastLine: false,
    wordWrap: 'off',
  })
  const model = monaco.editor.createModel(detail.diff ?? '', guessLanguage(detail.path))
  editor.setModel(model)
  monacoEditor = editor
  monacoModels = [model]
  editor.layout()
}

async function loadFileDetail(nextSessionId: number) {
  if (!hasFileNode.value) {
    return
  }

  const requestedPath = filePath.value
  fileLoading.value = true
  fileError.value = ''
  try {
    const file = await fetchConversationWorkspaceFile(props.conversationId, requestedPath)
    if (nextSessionId !== sessionId || requestedPath !== filePath.value) {
      return
    }
    fileDetail.value = file
    fileLoading.value = false
    if (mode.value === 'file') {
      await renderFileView(createRenderToken('file'))
    }
  } catch (error) {
    if (nextSessionId !== sessionId || requestedPath !== filePath.value) {
      return
    }
    fileError.value = error instanceof Error ? error.message : 'Failed to load file'
    resetEditorState()
  } finally {
    if (nextSessionId === sessionId && fileLoading.value) {
      fileLoading.value = false
    }
  }
}

async function loadDiffDetail(nextSessionId: number) {
  if (!hasFileNode.value || diffLoading.value) {
    return
  }

  const requestedPath = filePath.value
  diffLoading.value = true
  diffError.value = ''
  try {
    const diff = await fetchConversationWorkspaceDiff(props.conversationId, requestedPath)
    if (nextSessionId !== sessionId || requestedPath !== filePath.value) {
      return
    }
    diffDetail.value = diff
    diffLoading.value = false
    if (mode.value === 'diff') {
      await renderDiffView(createRenderToken('diff'))
    }
  } catch (error) {
    if (nextSessionId !== sessionId || requestedPath !== filePath.value) {
      return
    }
    diffError.value = error instanceof Error ? error.message : 'Failed to load diff'
    resetEditorState()
  } finally {
    if (nextSessionId === sessionId && diffLoading.value) {
      diffLoading.value = false
    }
  }
}

async function selectMode(nextMode: ViewMode) {
  if (!props.open || !hasFileNode.value) {
    return
  }
  if (mode.value === nextMode) {
    if (nextMode === 'diff' && diffLoading.value) {
      return
    }
    if (nextMode === 'diff' && diffDetail.value) {
      return
    }
    if (nextMode === 'file') {
      return
    }
  }

  invalidateRender()
  mode.value = nextMode
  resetEditorState()
  const token = createRenderToken(nextMode)
  if (nextMode === 'file') {
    if (fileDetail.value && !activeBinary.value) {
      await renderFileView(token)
    }
    return
  }

  if (activeBinary.value) {
    resetEditorState()
    return
  }

  if (diffDetail.value) {
    await renderDiffView(token)
    return
  }

  const currentSession = sessionId
  await loadDiffDetail(currentSession)
}

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      lockBodyScroll()
      return
    }
    unlockBodyScroll()
  },
  { immediate: true },
)

watch(
  () => [props.open, props.conversationId, props.node?.path, props.node?.type] as const,
  async ([isOpen]) => {
    disposeCurrentSession()
    if (!isOpen || !hasFileNode.value) {
      return
    }

    const currentSession = sessionId
    mode.value = props.initialMode ?? 'file'
    await loadFileDetail(currentSession)
    if (props.initialMode === 'diff' && currentSession === sessionId && mode.value === 'diff' && !activeBinary.value) {
      await loadDiffDetail(currentSession)
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  sessionId += 1
  invalidateRender()
  unlockBodyScroll()
  resetEditorState()
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="workspace-file-dialog-backdrop"
      data-workspace-file-dialog
      role="dialog"
      aria-modal="true"
      :aria-labelledby="titleId"
      @click.self="emit('close')"
    >
      <section class="workspace-file-dialog-panel">
        <header class="workspace-file-dialog-header">
          <div class="workspace-file-dialog-heading">
            <p class="workspace-file-dialog-eyebrow">Workspace</p>
            <h2 :id="titleId" class="workspace-file-dialog-title" :title="displayTitle">{{ displayTitle }}</h2>
          </div>
          <button class="workspace-file-dialog-close" type="button" aria-label="Close dialog" @click="emit('close')">
            <Close />
          </button>
        </header>

        <div class="workspace-file-dialog-toolbar">
          <div class="workspace-file-dialog-modes">
            <button
              class="workspace-file-dialog-mode-button"
              :class="{ active: mode === 'file' }"
              type="button"
              data-workspace-dialog-mode="file"
              @click="selectMode('file')"
            >
              File
            </button>
            <button
              class="workspace-file-dialog-mode-button"
              :class="{ active: mode === 'diff' }"
              type="button"
              data-workspace-dialog-mode="diff"
              @click="selectMode('diff')"
            >
              Diff
            </button>
          </div>
          <a
            v-if="downloadUrl"
            class="workspace-file-dialog-download"
            :href="downloadUrl"
            target="_blank"
            rel="noreferrer"
            data-workspace-dialog-download
          >
            <Download />
            Download
          </a>
        </div>

        <div class="workspace-file-dialog-body">
          <p v-if="statusMessage" class="workspace-file-dialog-status" :class="{ error: Boolean(fileError || diffError) }">
            {{ statusMessage }}
          </p>

          <div v-else-if="activeBinary" class="workspace-file-dialog-binary">
            <div class="workspace-file-dialog-binary-summary">
              <strong>Binary file</strong>
              <span>{{ filePath }}</span>
              <span v-if="displaySize">{{ displaySize }}</span>
            </div>
          </div>

          <div v-else-if="showEditorHost" ref="editorHost" class="workspace-file-dialog-editor" data-workspace-dialog-editor />
          <p v-else class="workspace-file-dialog-status">No file selected</p>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.workspace-file-dialog-backdrop {
  position: fixed;
  inset: 0;
  z-index: 980;
  padding: 10vh 1rem;
  display: flex;
  justify-content: center;
  align-items: stretch;
  background: rgba(15, 32, 38, 0.48);
}

.workspace-file-dialog-panel {
  width: min(1120px, calc(100vw - 2rem));
  height: 100%;
  max-height: 80vh;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--app-border, rgba(15, 32, 38, 0.12));
  border-radius: 8px;
  background: var(--app-surface, #fff);
  box-shadow: 0 24px 72px rgba(15, 32, 38, 0.24);
}

.workspace-file-dialog-header,
.workspace-file-dialog-toolbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  min-width: 0;
  padding: 0.9rem 1rem;
}

.workspace-file-dialog-header {
  border-bottom: 1px solid var(--app-border, rgba(15, 32, 38, 0.12));
}

.workspace-file-dialog-heading {
  min-width: 0;
}

.workspace-file-dialog-eyebrow {
  margin: 0 0 0.15rem;
  font-size: 0.72rem;
  font-weight: 700;
  text-transform: uppercase;
  color: var(--app-text-muted, #6e7d86);
}

.workspace-file-dialog-title {
  margin: 0;
  min-width: 0;
  font-size: 0.95rem;
  line-height: 1.35;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.workspace-file-dialog-close,
.workspace-file-dialog-mode-button,
.workspace-file-dialog-download {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.35rem;
  min-height: 2rem;
  padding: 0.38rem 0.62rem;
  border: 1px solid var(--app-border-subtle, rgba(15, 32, 38, 0.1));
  border-radius: 8px;
  background: transparent;
  color: inherit;
}

.workspace-file-dialog-close {
  width: 2rem;
  padding: 0;
}

.workspace-file-dialog-close svg,
.workspace-file-dialog-download svg {
  width: 1rem;
  height: 1rem;
}

.workspace-file-dialog-toolbar {
  padding-top: 0.65rem;
  padding-bottom: 0.65rem;
  border-bottom: 1px solid var(--app-border-subtle, rgba(15, 32, 38, 0.08));
}

.workspace-file-dialog-modes {
  display: inline-flex;
  gap: 0.35rem;
  flex-wrap: wrap;
}

.workspace-file-dialog-mode-button.active {
  background: rgba(var(--app-accent-rgb, 68, 136, 160), 0.12);
  border-color: rgba(var(--app-accent-rgb, 68, 136, 160), 0.3);
}

.workspace-file-dialog-download {
  text-decoration: none;
}

.workspace-file-dialog-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  padding: 1rem;
}

.workspace-file-dialog-status,
.workspace-file-dialog-binary {
  margin: 0;
  color: var(--app-text-muted, #6e7d86);
  font-size: 0.85rem;
  line-height: 1.55;
}

.workspace-file-dialog-status.error {
  color: #b24343;
}

.workspace-file-dialog-binary {
  display: flex;
  align-items: flex-start;
}

.workspace-file-dialog-binary-summary {
  display: grid;
  gap: 0.25rem;
}

.workspace-file-dialog-editor {
  width: 100%;
  height: 100%;
  min-height: 0;
  border: 1px solid var(--app-border-subtle, rgba(15, 32, 38, 0.08));
  border-radius: 8px;
  overflow: hidden;
  background: var(--app-surface-strong, #f8fafb);
}

</style>
