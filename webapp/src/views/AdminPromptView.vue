<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { ArrowLeft, Close, Menu, Plus, QuestionFilled, Tickets } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

import {
  createPromptBinding,
  createPromptDocument,
  deletePromptBinding,
  deletePromptDocument,
  fetchModelCatalog,
  fetchPromptBindings,
  fetchPromptDocuments,
  updatePromptBinding,
  updatePromptDocument,
} from '../lib/api'
import type {
  ModelCatalog,
  ModelCatalogEntry,
  ModelCatalogProvider,
  PromptBinding,
  PromptBindingInput,
  PromptDocument,
  UpdatePromptDocumentInput,
} from '../types/api'

type DocumentDraft = {
  id: string
  name: string
  description: string
  content: string
  scope: string
  status: string
}

type BindingDraft = {
  id: number | null
  prompt_id: string
  scene: string
  phase: string
  is_default: boolean
  priority: number
  provider_id: string
  model_id: string
  status: string
}

type BindingEditorMode = 'idle' | 'create' | 'edit'

type FieldHelpKey = 'document-id' | 'scope' | 'scene' | 'phase'

type TooltipState = {
  key: FieldHelpKey
  style: Record<string, string>
}

const loading = ref(false)
const savingDocument = ref(false)
const savingBinding = ref(false)
const deletingDocumentId = ref('')
const catalogLoading = ref(false)
const sidebarMobile = ref(false)
const sidebarDrawerOpen = ref(false)
const bindingDialogVisible = ref(false)
const bindingEditorMode = ref<BindingEditorMode>('idle')
const bootstrapSelectionLocked = ref(false)
const documentDraftDirty = ref(false)
const documentIdManuallyEdited = ref(false)
const bindingDraftDirty = ref(false)
const tooltipState = ref<TooltipState | null>(null)
const documentContentInput = ref<HTMLTextAreaElement | null>(null)
let bootstrapRequestId = 0
let collectionVersion = 0

const documents = ref<PromptDocument[]>([])
const bindings = ref<PromptBinding[]>([])
const modelCatalog = ref<ModelCatalog | null>(null)
const selectedDocumentId = ref('')
const documentDraft = ref<DocumentDraft>(emptyDocumentDraft())
const bindingDraft = ref<BindingDraft>(emptyBindingDraft())

const selectedDocument = computed(() => documents.value.find((document) => document.id === selectedDocumentId.value) ?? null)
const selectedBindings = computed(() =>
  bindings.value
    .filter((binding) => binding.prompt_id === selectedDocumentId.value)
    .slice()
    .sort((left, right) => left.priority - right.priority || left.id - right.id),
)
const isCreatingDocument = computed(() => !selectedDocument.value)
const bindingPaneLocked = computed(() => savingDocument.value || savingBinding.value)
const bindingFormVisible = computed(() => bindingEditorMode.value !== 'idle')
const selectedBindingId = computed(() => (bindingEditorMode.value === 'edit' ? bindingDraft.value.id : null))
const availableBindingProviders = computed<ModelCatalogProvider[]>(() => {
  const providers = modelCatalog.value?.providers ?? []
  const currentProviderId = bindingDraft.value.provider_id.trim()
  if (!currentProviderId || providers.some((provider) => provider.id === currentProviderId)) {
    return providers
  }

  return [
    ...providers,
    {
      id: currentProviderId,
      name: currentProviderId,
      models: bindingDraft.value.model_id
        ? [{ id: bindingDraft.value.model_id, name: bindingDraft.value.model_id, type: 'custom' }]
        : [],
    },
  ]
})
const selectedBindingProvider = computed(
  () => availableBindingProviders.value.find((provider) => provider.id === bindingDraft.value.provider_id) ?? null,
)
const availableBindingModels = computed<ModelCatalogEntry[]>(() => {
  const models = selectedBindingProvider.value?.models ?? []
  const currentModelId = bindingDraft.value.model_id.trim()
  if (!currentModelId || models.some((model) => model.id === currentModelId)) {
    return models
  }

  return [...models, { id: currentModelId, name: currentModelId, type: 'custom' }]
})
const supportsBindingCatalog = computed(() => availableBindingProviders.value.length > 0)
const bindingPlaceholderTitle = computed(() => (selectedBindings.value.length ? '选择一个场景绑定' : '还没有场景绑定'))
const bindingPlaceholderCopy = computed(() =>
  selectedBindings.value.length
    ? '从左侧选择已有绑定，或点击“添加绑定”创建新的场景配置。'
    : '当前提示词还没有场景绑定，点击左侧“添加绑定”开始创建。',
)
const pageStatus = computed(() => {
  if (loading.value) return '加载中'
  if (savingDocument.value || savingBinding.value) return '保存中'
  return '就绪'
})
const promptShellClass = computed(() => ({
  'sidebar-mobile': sidebarMobile.value,
  'sidebar-open': sidebarMobile.value && sidebarDrawerOpen.value,
}))

const fieldHelpContent: Record<FieldHelpKey, { title: string; body: string[] }> = {
  'document-id': {
    title: '提示词 ID',
    body: ['新建提示词时会根据名称自动生成，你也可以手动调整。', '它是提示词的稳定主键，建议使用英文、数字和中划线。'],
  },
  scope: {
    title: 'Scope',
    body: ['Scope 是提示词的作用域标签，用来标记这份提示词属于哪个平台范围。', '它不是用户私有权限；当前管理页维护的是平台级提示词，默认值通常是 admin。'],
  },
  scene: {
    title: 'Scene',
    body: ['Scene 表示提示词注入的运行场景，普通聊天默认会使用 agent.run.default。', '这里保留可编辑，是为了支持扩展场景，例如 agent.run.review 这类专用流程。'],
  },
  phase: {
    title: 'Phase',
    body: ['session：本次运行开始时注入的基础系统提示。', 'step_pre_model：每次发模型请求前都会追加。', 'tool_result：工具执行后，继续让模型处理工具结果时追加。'],
  },
}

function emptyDocumentDraft(): DocumentDraft {
  return {
    id: '',
    name: '',
    description: '',
    content: '',
    scope: 'admin',
    status: 'active',
  }
}

function emptyBindingDraft(promptId = ''): BindingDraft {
  return {
    id: null,
    prompt_id: promptId,
    scene: 'agent.run.default',
    phase: 'session',
    is_default: true,
    priority: 10,
    provider_id: '',
    model_id: '',
    status: 'active',
  }
}

function markCollectionMutated() {
  collectionVersion += 1
}

function mergeDocumentsByID(localDocuments: PromptDocument[], serverDocuments: PromptDocument[]) {
  const merged = new Map<string, PromptDocument>()
  for (const document of serverDocuments) merged.set(document.id, document)
  for (const document of localDocuments) merged.set(document.id, document)
  return Array.from(merged.values()).sort((left, right) => left.id.localeCompare(right.id))
}

function mergeBindingsByID(localBindings: PromptBinding[], serverBindings: PromptBinding[]) {
  const merged = new Map<number, PromptBinding>()
  for (const binding of serverBindings) merged.set(binding.id, binding)
  for (const binding of localBindings) merged.set(binding.id, binding)
  return Array.from(merged.values()).sort((left, right) => left.priority - right.priority || left.id - right.id)
}

function buildFallbackPromptDocumentID(name: string) {
  let hash = 0
  for (let index = 0; index < name.length; index += 1) {
    hash = (hash * 31 + name.charCodeAt(index)) >>> 0
  }
  return `prompt-${hash.toString(36)}`
}

function buildPromptDocumentID(name: string) {
  const trimmed = name.trim()
  if (!trimmed) return ''

  const slug = trimmed
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/-{2,}/g, '-')
    .replace(/^-|-$/g, '')

  return slug || buildFallbackPromptDocumentID(trimmed)
}

function syncDocumentDraft(document: PromptDocument | null) {
  documentDraftDirty.value = false
  documentIdManuallyEdited.value = Boolean(document)

  if (!document) {
    documentDraft.value = emptyDocumentDraft()
    return
  }

  documentDraft.value = {
    id: document.id,
    name: document.name,
    description: document.description,
    content: document.content,
    scope: document.scope,
    status: document.status,
  }
}

function syncBindingDraft(binding: PromptBinding | null, mode: BindingEditorMode = binding ? 'edit' : 'idle') {
  bindingDraftDirty.value = false
  bindingEditorMode.value = mode
  if (!binding) {
    bindingDraft.value = emptyBindingDraft(selectedDocumentId.value)
    return
  }

  bindingDraft.value = {
    id: binding.id,
    prompt_id: binding.prompt_id,
    scene: binding.scene,
    phase: binding.phase,
    is_default: binding.is_default,
    priority: binding.priority,
    provider_id: binding.provider_id,
    model_id: binding.model_id,
    status: binding.status,
  }
}

function closeSidebarDrawer() {
  sidebarDrawerOpen.value = false
}

function toggleSidebarDrawer() {
  if (!sidebarMobile.value) return
  sidebarDrawerOpen.value = !sidebarDrawerOpen.value
}

function syncSidebarViewport() {
  const mobile = window.innerWidth <= 960
  sidebarMobile.value = mobile
  if (!mobile) sidebarDrawerOpen.value = false
}

function showFieldHelp(key: FieldHelpKey, event: MouseEvent) {
  const target = event.currentTarget
  if (!(target instanceof HTMLElement)) return
  const rect = target.getBoundingClientRect()
  const tooltipWidth = Math.min(280, Math.max(window.innerWidth - 32, 180))
  const left = Math.min(Math.max(rect.right - tooltipWidth, 16), Math.max(window.innerWidth - tooltipWidth - 16, 16))
  const top = Math.min(rect.bottom + 8, Math.max(window.innerHeight - 16, 16))
  tooltipState.value = {
    key,
    style: {
      position: 'fixed',
      top: `${top}px`,
      left: `${left}px`,
      width: `${tooltipWidth}px`,
    },
  }
}

function hideFieldHelp(key: FieldHelpKey) {
  if (tooltipState.value?.key === key) tooltipState.value = null
}

async function syncDocumentContentTextareaHeight() {
  await nextTick()
  const textarea = documentContentInput.value
  if (!textarea) return
  textarea.style.height = 'auto'
  textarea.style.height = `${Math.max(textarea.scrollHeight + 12, 8 * 24)}px`
}

function setSelectedDocument(documentId: string) {
  bootstrapSelectionLocked.value = true
  selectedDocumentId.value = documentId
  closeSidebarDrawer()
  syncDocumentDraft(documents.value.find((document) => document.id === documentId) ?? null)
  syncBindingDraft(null)
  void syncDocumentContentTextareaHeight()
}

function refreshSelectedDocumentDraft(documentId: string) {
  bootstrapSelectionLocked.value = true
  selectedDocumentId.value = documentId
  syncDocumentDraft(documents.value.find((document) => document.id === documentId) ?? null)
  void syncDocumentContentTextareaHeight()
}

function patchDocumentDraft(patch: Partial<DocumentDraft>) {
  documentDraftDirty.value = true
  bootstrapSelectionLocked.value = true
  documentDraft.value = { ...documentDraft.value, ...patch }
  if (patch.content !== undefined) void syncDocumentContentTextareaHeight()
}

function patchDocumentName(name: string) {
  const patch: Partial<DocumentDraft> = { name }
  if (isCreatingDocument.value && !documentIdManuallyEdited.value) {
    patch.id = buildPromptDocumentID(name)
  }
  patchDocumentDraft(patch)
}

function patchDocumentID(id: string) {
  if (!isCreatingDocument.value) {
    patchDocumentDraft({ id })
    return
  }
  const autoID = buildPromptDocumentID(documentDraft.value.name)
  if (id.trim() === '') {
    documentIdManuallyEdited.value = false
    patchDocumentDraft({ id: autoID })
    return
  }
  documentIdManuallyEdited.value = id.trim() !== autoID
  patchDocumentDraft({ id })
}

function patchBindingDraft(patch: Partial<BindingDraft>) {
  bindingDraftDirty.value = true
  bindingDraft.value = { ...bindingDraft.value, ...patch }
}

function resolveBindingProvider(providerId: string) {
  return availableBindingProviders.value.find((provider) => provider.id === providerId) ?? null
}

function resolveBindingProviderDefaultModel(provider: ModelCatalogProvider | null, fallbackModelId = '') {
  if (!provider) {
    return fallbackModelId
  }
  if (fallbackModelId && provider.models.some((model) => model.id === fallbackModelId)) {
    return fallbackModelId
  }
  return provider.models[0]?.id ?? ''
}

function patchBindingProvider(providerId: string) {
  const provider = resolveBindingProvider(providerId)
  patchBindingDraft({
    provider_id: providerId,
    model_id: providerId ? resolveBindingProviderDefaultModel(provider, bindingDraft.value.model_id) : '',
  })
}

async function ensureModelCatalogLoaded() {
  if (modelCatalog.value || catalogLoading.value) return

  catalogLoading.value = true
  try {
    modelCatalog.value = await fetchModelCatalog()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '加载模型目录失败')
  } finally {
    catalogLoading.value = false
  }
}

function formatTime(value: string) {
  if (!value) return '--'
  return value.replace('T', ' ').slice(0, 16)
}

async function openBindingDialog() {
  if (!selectedDocumentId.value) {
    ElMessage.warning('请先选择一个提示词')
    return
  }
  bindingDialogVisible.value = true
  await ensureModelCatalogLoaded()
}

async function loadPromptState() {
  loading.value = true
  const requestId = ++bootstrapRequestId
  const requestCollectionVersion = collectionVersion

  try {
    const [loadedDocuments, loadedBindings] = await Promise.all([fetchPromptDocuments(), fetchPromptBindings()])
    if (requestId !== bootstrapRequestId) return

    const collectionsChangedSinceRequest = requestCollectionVersion !== collectionVersion
    documents.value = collectionsChangedSinceRequest ? mergeDocumentsByID(documents.value, loadedDocuments) : loadedDocuments
    bindings.value = collectionsChangedSinceRequest ? mergeBindingsByID(bindings.value, loadedBindings) : loadedBindings

    if (bootstrapSelectionLocked.value || documentDraftDirty.value || bindingDraftDirty.value) return

    const nextDocumentId = selectedDocumentId.value && loadedDocuments.some((document) => document.id === selectedDocumentId.value)
      ? selectedDocumentId.value
      : loadedDocuments[0]?.id ?? ''

    if (nextDocumentId) {
      setSelectedDocument(nextDocumentId)
    } else {
      selectedDocumentId.value = ''
      syncDocumentDraft(null)
      syncBindingDraft(null)
      void syncDocumentContentTextareaHeight()
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '加载提示词失败')
  } finally {
    loading.value = false
  }
}

function startCreateDocument() {
  bootstrapSelectionLocked.value = true
  selectedDocumentId.value = ''
  closeSidebarDrawer()
  syncDocumentDraft(null)
  syncBindingDraft(null)
  void syncDocumentContentTextareaHeight()
}

function startCreateBinding() {
  syncBindingDraft(null, 'create')
}

function startEditBinding(binding: PromptBinding) {
  syncBindingDraft(binding, 'edit')
}

function buildDocumentUpdateInput(): UpdatePromptDocumentInput {
  return {
    name: documentDraft.value.name.trim(),
    description: documentDraft.value.description,
    content: documentDraft.value.content,
    scope: documentDraft.value.scope.trim(),
    status: documentDraft.value.status,
  }
}

function buildBindingInput(): PromptBindingInput {
  return {
    prompt_id: selectedDocumentId.value,
    scene: bindingDraft.value.scene.trim(),
    phase: bindingDraft.value.phase,
    is_default: bindingDraft.value.is_default,
    priority: Number(bindingDraft.value.priority) || 0,
    provider_id: bindingDraft.value.provider_id.trim(),
    model_id: bindingDraft.value.model_id.trim(),
    status: bindingDraft.value.status,
  }
}

async function handleSubmitDocument() {
  savingDocument.value = true
  try {
    if (isCreatingDocument.value) {
      const created = await createPromptDocument({
        id: documentDraft.value.id.trim(),
        name: documentDraft.value.name.trim(),
        description: documentDraft.value.description,
        content: documentDraft.value.content,
        scope: documentDraft.value.scope.trim(),
        status: documentDraft.value.status,
      })
      markCollectionMutated()
      documents.value = [...documents.value, created].sort((left, right) => left.id.localeCompare(right.id))
      setSelectedDocument(created.id)
      ElMessage.success('提示词文档已创建')
      return
    }

    const updated = await updatePromptDocument(selectedDocumentId.value, buildDocumentUpdateInput())
    markCollectionMutated()
    documents.value = documents.value.map((document) => (document.id === updated.id ? updated : document))
    if (selectedDocumentId.value === updated.id) {
      refreshSelectedDocumentDraft(updated.id)
    } else {
      setSelectedDocument(updated.id)
    }
    ElMessage.success('提示词文档已更新')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '保存提示词文档失败')
  } finally {
    savingDocument.value = false
  }
}

async function handleSubmitBinding() {
  if (!selectedDocumentId.value) {
    ElMessage.warning('请先选择一个提示词')
    return
  }

  savingBinding.value = true
  const submitBindingId = bindingDraft.value.id
  const submitDocumentId = selectedDocumentId.value
  const bindingInput = buildBindingInput()

  try {
    if (submitBindingId == null) {
      const created = await createPromptBinding(bindingInput)
      markCollectionMutated()
      bindings.value = [...bindings.value, created]
      if (selectedDocumentId.value === submitDocumentId && bindingDraft.value.id == null) {
        syncBindingDraft(created, 'edit')
      }
      ElMessage.success('绑定已创建')
      return
    }

    const updated = await updatePromptBinding(submitBindingId, bindingInput)
    markCollectionMutated()
    bindings.value = bindings.value.map((binding) => (binding.id === updated.id ? updated : binding))
    if (selectedDocumentId.value === submitDocumentId && bindingDraft.value.id === submitBindingId) {
      syncBindingDraft(updated, 'edit')
    }
    ElMessage.success('绑定已更新')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '保存绑定失败')
  } finally {
    savingBinding.value = false
  }
}

async function handleDeleteBinding(bindingId: number) {
  savingBinding.value = true
  try {
    await deletePromptBinding(bindingId)
    markCollectionMutated()
    bindings.value = bindings.value.filter((binding) => binding.id !== bindingId)
    if (bindingDraft.value.id === bindingId) {
      syncBindingDraft(null)
    }
    ElMessage.success('绑定已删除')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '删除绑定失败')
  } finally {
    savingBinding.value = false
  }
}

async function handleDeleteDocument(documentId: string) {
  deletingDocumentId.value = documentId
  try {
    await deletePromptDocument(documentId)
    markCollectionMutated()
    documents.value = documents.value.filter((document) => document.id !== documentId)
    bindings.value = bindings.value.filter((binding) => binding.prompt_id !== documentId)
    if (selectedDocumentId.value === documentId) {
      const nextDocument = documents.value[0] ?? null
      if (nextDocument) {
        setSelectedDocument(nextDocument.id)
      } else {
        selectedDocumentId.value = ''
        syncDocumentDraft(null)
        syncBindingDraft(null)
      }
    }
    ElMessage.success('提示词文档已删除')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '删除提示词文档失败')
  } finally {
    deletingDocumentId.value = ''
  }
}

onMounted(async () => {
  syncSidebarViewport()
  window.addEventListener('resize', syncSidebarViewport)
  await loadPromptState()
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', syncSidebarViewport)
})
</script>

<template>
  <main class="admin-prompt-shell admin-audit-shell chat-shell" :class="promptShellClass">
    <button
      v-if="sidebarMobile && sidebarDrawerOpen"
      class="sidebar-backdrop"
      type="button"
      aria-label="关闭提示词文档侧栏"
      @click="closeSidebarDrawer"
    ></button>

    <section class="admin-prompt-sidebar admin-audit-sidebar sidebar-panel" :class="{ mobile: sidebarMobile, open: sidebarDrawerOpen }">
      <div class="sidebar-header admin-prompt-sidebar-header">
        <div>
          <h2>提示词</h2>
          <p class="admin-prompt-subtitle">仅管理员可维护默认提示词与场景绑定。</p>
        </div>
        <div class="sidebar-header-actions">
          <button class="ghost-button icon-button" type="button" aria-label="新建提示词" data-create-document :disabled="savingDocument" @click="startCreateDocument">
            <Plus />
          </button>
          <button v-if="sidebarMobile" class="ghost-button icon-button" type="button" aria-label="关闭提示词文档侧栏" @click="closeSidebarDrawer">
            <Close />
          </button>
        </div>
      </div>

      <p v-if="loading && documents.length === 0" class="sidebar-empty">正在加载提示词...</p>
      <div v-else-if="documents.length === 0" class="sidebar-empty">暂无提示词，先创建一个。</div>
      <div v-else class="sidebar-list admin-audit-list admin-prompt-list">
        <div
          v-for="document in documents"
          :key="document.id"
          class="conversation-card admin-audit-conversation admin-prompt-document"
          :class="{ active: document.id === selectedDocumentId, disabled: savingDocument }"
          :data-document-id="document.id"
          :aria-disabled="savingDocument ? 'true' : undefined"
          role="button"
          tabindex="0"
          @click="!savingDocument && setSelectedDocument(document.id)"
          @keydown.enter.prevent="!savingDocument && setSelectedDocument(document.id)"
          @keydown.space.prevent="!savingDocument && setSelectedDocument(document.id)"
        >
          <div class="conversation-preview admin-audit-conversation-main">
            <div class="admin-audit-conversation-row">
              <strong class="conversation-title truncate-text" :title="document.name">{{ document.name }}</strong>
              <span class="admin-prompt-status-chip" :class="document.status">{{ document.status }}</span>
            </div>
            <div class="admin-audit-conversation-meta conversation-meta">
              <span class="truncate-text">{{ document.scope }}</span>
              <span class="admin-audit-conversation-time">{{ formatTime(document.updated_at) }}</span>
            </div>
          </div>
          <button class="ghost-button icon-button conversation-delete-button" type="button" :data-document-delete="document.id" :disabled="savingDocument || deletingDocumentId === document.id" aria-label="删除提示词" @click.stop="handleDeleteDocument(document.id)">
            <Close />
          </button>
        </div>
      </div>
    </section>

    <section class="admin-prompt-stage admin-audit-stage chat-stage">
      <header class="topbar admin-audit-topbar">
        <button v-if="sidebarMobile" class="ghost-button icon-button topbar-sidebar-toggle" type="button" :aria-label="sidebarDrawerOpen ? '关闭提示词文档侧栏' : '打开提示词文档侧栏'" @click="toggleSidebarDrawer">
          <component :is="sidebarDrawerOpen ? Close : Menu" />
        </button>
        <RouterLink class="ghost-button icon-button admin-audit-back-link" to="/chat" title="返回聊天" aria-label="返回聊天">
          <ArrowLeft />
        </RouterLink>
        <div class="topbar-title-block">
          <h1 class="topbar-conversation-title">{{ selectedDocument?.name || '新建提示词' }}</h1>
          <p class="admin-prompt-topbar-copy">维护提示词内容，并在需要时通过按钮打开场景绑定。</p>
        </div>
        <div class="status-pill" :class="{ idle: !loading && !savingDocument && !savingBinding, loading: loading || savingDocument || savingBinding }">
          {{ pageStatus }}
        </div>
      </header>

      <div class="admin-prompt-content">
        <section class="admin-prompt-grid admin-prompt-grid-single">
          <article class="admin-audit-card admin-prompt-card admin-prompt-editor-card">
            <div class="messages-header admin-prompt-card-header admin-prompt-editor-header">
              <div>
                <h2>提示词编辑</h2>
                <p class="admin-prompt-subtitle">重做表单区域，给内容编辑更多空间；场景绑定通过独立 dialog 管理。</p>
              </div>
              <button class="ghost-button admin-prompt-binding-trigger" type="button" data-open-binding-dialog aria-label="打开场景绑定" :disabled="!selectedDocumentId" @click="openBindingDialog">
                <Tickets />
                <span>场景绑定</span>
              </button>
            </div>

            <form class="admin-prompt-form admin-prompt-document-form" data-document-form @submit.prevent="handleSubmitDocument">
              <div class="admin-prompt-form-scroller" data-document-form-scroller>
                <label class="admin-prompt-field">
                  <span class="admin-prompt-field-header">
                    <span class="field-label">提示词 ID</span>
                    <span class="admin-prompt-help-anchor">
                      <button class="admin-prompt-help-button" type="button" data-field-help-button="document-id" aria-label="查看提示词 ID 说明" @mouseenter="showFieldHelp('document-id', $event)" @mouseleave="hideFieldHelp('document-id')">
                        <QuestionFilled />
                      </button>
                    </span>
                  </span>
                  <input class="text-input" data-document-id-input :disabled="!isCreatingDocument || savingDocument" placeholder="会根据名称自动生成" :value="documentDraft.id" @input="patchDocumentID(($event.target as HTMLInputElement).value)" />
                </label>

                <label class="admin-prompt-field">
                  <span class="field-label">名称</span>
                  <input class="text-input" data-document-name-input :disabled="savingDocument" :value="documentDraft.name" @input="patchDocumentName(($event.target as HTMLInputElement).value)" />
                </label>

                <label class="admin-prompt-field admin-prompt-field-span-2">
                  <span class="field-label">说明</span>
                  <input class="text-input" data-document-description-input :disabled="savingDocument" :value="documentDraft.description" @input="patchDocumentDraft({ description: ($event.target as HTMLInputElement).value })" />
                </label>

                <div class="admin-prompt-form-row">
                  <label class="admin-prompt-field">
                    <span class="admin-prompt-field-header">
                      <span class="field-label">Scope</span>
                      <span class="admin-prompt-help-anchor">
                        <button class="admin-prompt-help-button" type="button" data-field-help-button="scope" aria-label="查看 Scope 说明" @mouseenter="showFieldHelp('scope', $event)" @mouseleave="hideFieldHelp('scope')">
                          <QuestionFilled />
                        </button>
                      </span>
                    </span>
                    <input class="text-input" data-document-scope-input :disabled="savingDocument" :value="documentDraft.scope" @input="patchDocumentDraft({ scope: ($event.target as HTMLInputElement).value })" />
                  </label>

                  <label class="admin-prompt-field">
                    <span class="field-label">状态</span>
                    <select class="text-input" data-document-status-input :disabled="savingDocument" :value="documentDraft.status" @change="patchDocumentDraft({ status: ($event.target as HTMLSelectElement).value })">
                      <option value="active">active</option>
                      <option value="disabled">disabled</option>
                    </select>
                  </label>
                </div>

                <label class="admin-prompt-field admin-prompt-field-wide">
                  <span class="field-label">内容</span>
                  <textarea ref="documentContentInput" class="text-input admin-prompt-textarea" rows="8" data-document-content-input :disabled="savingDocument" :value="documentDraft.content" @input="patchDocumentDraft({ content: ($event.target as HTMLTextAreaElement).value })" />
                </label>
              </div>

              <div class="admin-prompt-actions" data-document-form-actions>
                <button class="primary-button admin-prompt-compact-button" type="submit" :disabled="savingDocument">
                  {{ isCreatingDocument ? '创建提示词' : '保存提示词' }}
                </button>
              </div>
            </form>
          </article>
        </section>
      </div>
    </section>

    <div v-if="bindingDialogVisible" class="admin-prompt-dialog-mask" @click.self="bindingDialogVisible = false">
      <section class="admin-prompt-dialog" data-binding-dialog :data-binding-form-visible="bindingFormVisible ? 'true' : 'false'">
        <header class="admin-prompt-dialog-header" data-binding-dialog-header>
          <div>
            <h3>场景绑定</h3>
            <p>为当前提示词维护不同运行场景下的绑定配置。</p>
          </div>
          <button class="ghost-button icon-button" type="button" data-binding-dialog-close aria-label="关闭场景绑定" @click="bindingDialogVisible = false">
            <Close />
          </button>
        </header>

        <div v-if="selectedDocumentId" class="admin-prompt-dialog-layout">
          <section class="admin-prompt-dialog-table" data-binding-dialog-table>
            <div class="admin-prompt-table-toolbar" data-binding-table-header>
              <button class="ghost-button admin-prompt-compact-button admin-prompt-toolbar-button" type="button" data-create-binding aria-label="添加绑定" :disabled="bindingPaneLocked || !selectedDocumentId" @click="startCreateBinding">
                <Plus />
                <span>添加绑定</span>
              </button>
            </div>
            <table class="admin-prompt-table" v-if="selectedBindings.length">
              <thead>
                <tr>
                  <th>Scene</th>
                  <th>Phase</th>
                  <th>默认</th>
                  <th>优先级</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="binding in selectedBindings" :key="binding.id" :data-binding-id="String(binding.id)" :class="{ active: binding.id === selectedBindingId }">
                  <td>{{ binding.scene }}</td>
                  <td>{{ binding.phase }}</td>
                  <td>{{ binding.is_default ? '是' : '否' }}</td>
                  <td>{{ binding.priority }}</td>
                  <td>{{ binding.status }}</td>
                  <td>
                    <div class="admin-prompt-table-actions">
                      <button class="ghost-button admin-prompt-compact-button" type="button" :data-binding-edit="String(binding.id)" :disabled="bindingPaneLocked" @click="startEditBinding(binding)">编辑</button>
                      <button class="ghost-button admin-prompt-compact-button admin-prompt-danger-button" type="button" :data-binding-delete="String(binding.id)" :disabled="bindingPaneLocked" @click="handleDeleteBinding(binding.id)">删除</button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
            <p v-else class="messages-empty">当前提示词还没有绑定，先添加一个。</p>
          </section>

          <section class="admin-prompt-dialog-detail">
            <form v-if="bindingFormVisible" class="admin-prompt-form admin-prompt-binding-form admin-prompt-dialog-form" data-binding-dialog-form data-binding-form @submit.prevent="handleSubmitBinding">
              <div class="admin-prompt-form-scroller" data-binding-form-scroller>
                <label class="admin-prompt-field">
                  <span class="admin-prompt-field-header">
                    <span class="field-label">Scene</span>
                    <span class="admin-prompt-help-anchor">
                      <button class="admin-prompt-help-button" type="button" data-field-help-button="scene" aria-label="查看 Scene 说明" @mouseenter="showFieldHelp('scene', $event)" @mouseleave="hideFieldHelp('scene')">
                        <QuestionFilled />
                      </button>
                    </span>
                  </span>
                  <input class="text-input" list="prompt-scene-options" data-binding-scene-input :disabled="bindingPaneLocked" :value="bindingDraft.scene" @input="patchBindingDraft({ scene: ($event.target as HTMLInputElement).value })" />
                  <datalist id="prompt-scene-options">
                    <option value="agent.run.default"></option>
                    <option value="agent.run.review"></option>
                  </datalist>
                </label>

                <label class="admin-prompt-field">
                  <span class="admin-prompt-field-header">
                    <span class="field-label">Phase</span>
                    <span class="admin-prompt-help-anchor">
                      <button class="admin-prompt-help-button" type="button" data-field-help-button="phase" aria-label="查看 Phase 说明" @mouseenter="showFieldHelp('phase', $event)" @mouseleave="hideFieldHelp('phase')">
                        <QuestionFilled />
                      </button>
                    </span>
                  </span>
                  <select class="text-input" data-binding-phase-input :disabled="bindingPaneLocked" :value="bindingDraft.phase" @change="patchBindingDraft({ phase: ($event.target as HTMLSelectElement).value })">
                    <option value="session">session</option>
                    <option value="step_pre_model">step_pre_model</option>
                    <option value="tool_result">tool_result</option>
                  </select>
                </label>

                <label class="admin-prompt-field">
                  <span class="field-label">默认绑定</span>
                  <select class="text-input" data-binding-default-input :disabled="bindingPaneLocked" :value="bindingDraft.is_default ? 'true' : 'false'" @change="patchBindingDraft({ is_default: ($event.target as HTMLSelectElement).value === 'true' })">
                    <option value="true">是</option>
                    <option value="false">否</option>
                  </select>
                </label>

                <label class="admin-prompt-field">
                  <span class="field-label">优先级</span>
                  <input class="text-input" data-binding-priority-input type="number" :disabled="bindingPaneLocked" :value="bindingDraft.priority" @input="patchBindingDraft({ priority: Number(($event.target as HTMLInputElement).value) })" />
                </label>

                <label class="admin-prompt-field">
                  <span class="field-label">状态</span>
                  <select class="text-input" data-binding-status-input :disabled="bindingPaneLocked" :value="bindingDraft.status" @change="patchBindingDraft({ status: ($event.target as HTMLSelectElement).value })">
                    <option value="active">active</option>
                    <option value="disabled">disabled</option>
                  </select>
                </label>

                <label class="admin-prompt-field">
                  <span class="field-label">Provider</span>
                  <select
                    v-if="supportsBindingCatalog"
                    class="text-input"
                    data-binding-provider-input
                    :disabled="bindingPaneLocked || catalogLoading"
                    :value="bindingDraft.provider_id"
                    @change="patchBindingProvider(($event.target as HTMLSelectElement).value)"
                  >
                    <option value="">未指定 Provider</option>
                    <option v-for="provider in availableBindingProviders" :key="provider.id" :value="provider.id">{{ provider.name }}</option>
                  </select>
                  <input v-else class="text-input" data-binding-provider-input :disabled="bindingPaneLocked" :value="bindingDraft.provider_id" @input="patchBindingDraft({ provider_id: ($event.target as HTMLInputElement).value })" />
                </label>

                <label class="admin-prompt-field">
                  <span class="field-label">Model</span>
                  <select
                    v-if="supportsBindingCatalog"
                    class="text-input"
                    data-binding-model-input
                    :disabled="bindingPaneLocked || catalogLoading || !bindingDraft.provider_id"
                    :value="bindingDraft.model_id"
                    @change="patchBindingDraft({ model_id: ($event.target as HTMLSelectElement).value })"
                  >
                    <option value="">未指定 Model</option>
                    <option v-for="model in availableBindingModels" :key="model.id" :value="model.id">{{ model.name }}</option>
                  </select>
                  <input v-else class="text-input" data-binding-model-input :disabled="bindingPaneLocked" :value="bindingDraft.model_id" @input="patchBindingDraft({ model_id: ($event.target as HTMLInputElement).value })" />
                </label>
              </div>

              <div class="admin-prompt-actions admin-prompt-actions-compact" data-binding-form-actions>
                <button class="primary-button admin-prompt-compact-button" type="submit" :disabled="bindingPaneLocked || !selectedDocumentId">
                  {{ bindingDraft.id == null ? '创建绑定' : '保存绑定' }}
                </button>
              </div>
            </form>

            <div v-else class="admin-prompt-binding-placeholder" data-binding-empty-state>
              <Tickets />
              <strong>{{ bindingPlaceholderTitle }}</strong>
              <p>{{ bindingPlaceholderCopy }}</p>
            </div>
          </section>
        </div>

        <p v-else class="messages-empty">先从左侧选择一个提示词，再配置绑定。</p>
      </section>
    </div>

    <Teleport to="body">
      <div v-if="tooltipState" class="admin-prompt-tooltip" :style="tooltipState.style" :data-field-help-panel="tooltipState.key">
        <strong>{{ fieldHelpContent[tooltipState.key].title }}</strong>
        <p v-for="line in fieldHelpContent[tooltipState.key].body" :key="line">{{ line }}</p>
      </div>
    </Teleport>
  </main>
</template>

<style scoped>
.admin-prompt-sidebar-header,
.admin-prompt-card-header {
  align-items: flex-start;
}

.admin-prompt-subtitle,
.admin-prompt-topbar-copy,
.admin-prompt-dialog-header p {
  margin-top: 0.18rem;
  color: #60767d;
  font-size: 0.84rem;
}

.admin-prompt-shell {
  height: 100svh;
}

.admin-prompt-content {
  min-height: 0;
  height: 100%;
  overflow: auto;
}

.admin-prompt-grid {
  min-height: 0;
  height: 100%;
  display: grid;
  gap: 0.75rem;
}

.admin-prompt-grid-single {
  grid-template-columns: minmax(0, 1fr);
}

.admin-prompt-card {
  min-height: 0;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  gap: 0.8rem;
  overflow: hidden;
  padding-inline: 1rem;
  padding-bottom: 1rem;
}

.admin-prompt-editor-header,
.admin-prompt-dialog-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}

.admin-prompt-list {
  align-content: start;
}

.admin-prompt-binding-trigger,
.admin-prompt-toolbar-button {
  display: inline-flex;
  align-items: center;
  gap: 0.42rem;
}

.admin-prompt-binding-trigger {
  padding: 0.56rem 0.78rem;
  font-size: 0.8rem;
  font-weight: 600;
}

.admin-prompt-binding-trigger svg,
.admin-prompt-toolbar-button svg {
  width: 0.92rem;
  height: 0.92rem;
}

.admin-prompt-stage {
  min-height: 0;
}

.admin-prompt-document {
  align-items: stretch;
}

.admin-prompt-document.disabled {
  opacity: 0.62;
  pointer-events: none;
}

.admin-prompt-status-chip {
  flex: 0 0 auto;
  border-radius: 999px;
  padding: 0.18rem 0.56rem;
  background: rgba(25, 50, 59, 0.08);
  color: #3b5962;
  font-size: 0.72rem;
  font-weight: 700;
}

.admin-prompt-status-chip.active {
  background: rgba(118, 160, 124, 0.14);
  color: #29593a;
}

.admin-prompt-status-chip.disabled {
  background: rgba(196, 88, 63, 0.12);
  color: #a24b37;
}

.admin-prompt-form {
  min-height: 0;
  display: grid;
  gap: 0.75rem;
}

.admin-prompt-document-form,
.admin-prompt-binding-form {
  min-height: 0;
  grid-template-rows: minmax(0, 1fr) auto;
}

.admin-prompt-form-scroller {
  min-height: 0;
  overflow: auto;
  display: grid;
  align-content: start;
  gap: 0.7rem;
  padding: 0.18rem 0.35rem 0.45rem;
}

.admin-prompt-document-form .admin-prompt-form-scroller {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.admin-prompt-field {
  display: grid;
  gap: 0.38rem;
  align-content: start;
}

.admin-prompt-field .field-label {
  font-size: 0.8rem;
}

.admin-prompt-field .text-input {
  font-size: 0.9rem;
  padding: 0.56rem 0.72rem;
  border-radius: 12px;
  border: 1px solid rgba(25, 50, 59, 0.12);
  background: rgba(255, 255, 255, 0.96);
}

.admin-prompt-field textarea.text-input {
  line-height: 1.55;
}

.admin-prompt-field-header {
  display: flex;
  align-items: center;
  gap: 0.35rem;
}

.admin-prompt-help-button {
  width: auto;
  height: auto;
  min-width: 0;
  padding: 0;
  border: 0;
  background: transparent;
  box-shadow: none;
  color: #60767d;
}

.admin-prompt-help-anchor {
  display: inline-flex;
  align-items: center;
}

.admin-prompt-help-button svg {
  width: 0.88rem;
  height: 0.88rem;
}

.admin-prompt-tooltip {
  position: absolute;
  z-index: 16;
  display: grid;
  gap: 0.25rem;
  padding: 0.65rem 0.75rem;
  border-radius: 12px;
  border: 1px solid rgba(25, 50, 59, 0.1);
  background: rgba(255, 255, 255, 0.98);
  box-shadow: 0 16px 28px rgba(39, 61, 68, 0.16);
  color: #547078;
  font-size: 0.78rem;
  line-height: 1.45;
}

.admin-prompt-tooltip strong {
  color: #1f3b44;
}

.admin-prompt-tooltip p {
  margin: 0;
}

.admin-prompt-field-span-2,
.admin-prompt-field-wide,
.admin-prompt-actions {
  grid-column: 1 / -1;
}

.admin-prompt-field-wide {
  min-height: 0;
  grid-template-rows: auto minmax(0, 1fr);
}

.admin-prompt-form-row {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.7rem;
}

.admin-prompt-textarea {
  min-height: calc(8 * 1.55em + 1.4rem);
  resize: none;
  overflow: hidden;
}

.admin-prompt-actions {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  margin: 0 0.35rem;
  padding-top: 0.75rem;
  border-top: 1px solid rgba(25, 50, 59, 0.08);
  background: rgba(245, 249, 249, 0.92);
}

.admin-prompt-actions-compact {
  margin: 0;
  padding-top: 0.6rem;
  background: transparent;
}

.admin-prompt-compact-button {
  padding: 0.5rem 0.82rem;
  font-size: 0.82rem;
}

.admin-prompt-compact-button.primary-button {
  box-shadow: 0 12px 24px rgba(196, 88, 63, 0.2);
}

.admin-prompt-danger-button {
  color: #a24b37;
  border-color: rgba(196, 88, 63, 0.18);
  background: rgba(255, 248, 245, 0.9);
}

.admin-prompt-dialog-mask {
  position: fixed;
  inset: 0;
  z-index: 35;
  background: rgba(17, 29, 34, 0.28);
  display: grid;
  place-items: center;
  padding: 2rem;
}

.admin-prompt-dialog {
  width: min(1200px, calc(100vw - 4rem));
  height: min(760px, calc(100svh - 4rem));
  background: #fff;
  border-radius: 18px;
  padding: 1.1rem 1.15rem;
  box-shadow: 0 24px 48px rgba(28, 48, 55, 0.18);
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  gap: 1rem;
}

.admin-prompt-dialog-layout {
  min-height: 0;
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(340px, 0.95fr);
  gap: 0;
  overflow: hidden;
}

.admin-prompt-dialog-table,
.admin-prompt-dialog-form,
.admin-prompt-dialog-detail {
  min-height: 0;
}

.admin-prompt-dialog-table {
  padding-right: 1rem;
}

.admin-prompt-dialog-detail {
  min-height: 0;
  display: grid;
  padding-left: 1rem;
  border-left: 1px solid rgba(25, 50, 59, 0.1);
}

.admin-prompt-table-toolbar {
  display: flex;
  justify-content: flex-start;
  margin-bottom: 0.8rem;
}

.admin-prompt-table {
  width: 100%;
  border-collapse: collapse;
  overflow: hidden;
  border-radius: 14px;
  border: 1px solid rgba(25, 50, 59, 0.08);
}

.admin-prompt-table th,
.admin-prompt-table td {
  padding: 0.78rem 0.82rem;
  border-bottom: 1px solid rgba(25, 50, 59, 0.08);
  text-align: left;
  vertical-align: middle;
  font-size: 0.88rem;
}

.admin-prompt-table th {
  background: rgba(245, 249, 249, 0.9);
  color: #486169;
  font-size: 0.78rem;
}

.admin-prompt-table-actions {
  display: flex;
  align-items: center;
  gap: 0.45rem;
}

.admin-prompt-table tbody tr.active td {
  background: rgba(224, 109, 79, 0.08);
}

.admin-prompt-binding-placeholder {
  min-height: 0;
  height: 100%;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 0.55rem;
  padding: 1.4rem;
  border: 1px dashed rgba(25, 50, 59, 0.14);
  border-radius: 16px;
  background: linear-gradient(180deg, rgba(247, 250, 250, 0.96) 0%, rgba(243, 247, 247, 0.9) 100%);
  color: #60767d;
  text-align: center;
}

.admin-prompt-binding-placeholder svg {
  width: 2rem;
  height: 2rem;
  color: #c4583f;
}

.admin-prompt-binding-placeholder strong {
  color: #29414a;
  font-size: 0.9rem;
}

.admin-prompt-binding-placeholder p {
  max-width: 24rem;
  margin: 0;
  font-size: 0.82rem;
  line-height: 1.6;
}

@media (max-width: 960px) {
  .admin-prompt-shell {
    grid-template-columns: 1fr;
  }

  .admin-prompt-dialog-layout,
  .admin-prompt-document-form .admin-prompt-form-scroller,
  .admin-prompt-form-row {
    grid-template-columns: 1fr;
  }

  .admin-prompt-dialog-table {
    padding-right: 0;
    padding-bottom: 1rem;
  }

  .admin-prompt-dialog {
    width: min(1200px, calc(100vw - 2rem));
    height: min(760px, calc(100svh - 2rem));
    padding: 1rem;
  }

  .admin-prompt-dialog-detail {
    padding-left: 0;
    padding-top: 1rem;
    border-left: 0;
    border-top: 1px solid rgba(25, 50, 59, 0.1);
  }
}
</style>
