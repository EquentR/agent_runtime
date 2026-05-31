<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { Close, Menu } from '@element-plus/icons-vue'

import ConversationSidebar from '../components/ConversationSidebar.vue'
import ProfileDialog from '../components/ProfileDialog.vue'
import MessageComposer from '../components/MessageComposer.vue'
import MessageList from '../components/MessageList.vue'
import {
  ApiError,
  cancelTask,
  confirmConversationWorkspaceMerge,
  confirmTaskWorkspaceMerge,
  createRunTask,
  decideTaskApproval,
  deleteAttachment,
  deleteConversation,
  discardConversationWorkspaceChanges,
  discardTaskWorkspaceChanges,
  fetchConversationWorkspaceState,
  fetchConversationMessages,
  fetchConversations,
  fetchModelCatalog,
  fetchSkills,
  fetchTaskApprovals,
  fetchTaskDetails,
  uploadAttachment,
  fetchTaskInteractions,
  findRunningTaskByConversation,
  respondTaskInteraction,
  streamRunTask,
  TASK_STREAM_ABORTED_MESSAGE,
} from '../lib/api'
import { clearChatState, loadChatState, saveChatState, scheduleChatStateSave } from '../lib/chat-state'
import { formatConversationTitle, formatDocumentTitle } from '../lib/chat'
import { filterUsableModelProviders, resolveModelSelection } from '../lib/model-selection'
import { getSessionName, getSessionRole, logout } from '../lib/session'
import {
  buildApprovalEntriesFromList,
  isTaskActive,
  isTaskPendingWorkspaceMerge,
  isTaskWaitingForInput,
  TASK_WAITING_FOR_INTERACTION_REASON,
  resolveTaskConversationId,
} from '../lib/task-runtime'
import {
  attachReplyMetaToLatestReply,
  buildApprovalStreamEvent,
  buildTranscriptEntries,
  updateTranscriptFromStreamEvent,
} from '../lib/transcript'
import type {
  AttachmentRef,
  Conversation,
  MemoryContextState,
  ModelCatalog,
  ModelCatalogEntry,
  QuestionInteractionSubmitInput,
  RunTaskResult,
  TaskDetails,
  TaskWorkspaceState,
  TaskWorkspaceStateStatus,
  TranscriptEntry,
  WorkspaceActionErrorData,
  WorkspaceMode,
  WorkspaceSkillListItem,
} from '../types/api'

const NEW_CONVERSATION_SENDING_KEY = '__new__'
const DEFAULT_WORKSPACE_MODE: WorkspaceMode = 'mutable'

interface DraftAttachmentItem extends Omit<AttachmentRef, 'id'> {
  local_id: string
  id?: string
  preview_url?: string
  upload_state: 'uploading' | 'uploaded' | 'failed'
  error_message?: string
}

interface WorkspaceErrorAction {
  message: string
  conversationId: string
}

const router = useRouter()
const route = useRoute()

const messagesLoading = ref(false)
const sidebarLoading = ref(false)
const sidebarCollapsed = ref(false)
const sidebarMobile = ref(false)
const sidebarDrawerOpen = ref(false)
const profileDialogOpen = ref(false)
const username = ref(getSessionName())
const activeConversationId = ref('')
const activeTaskId = ref('')
const activeTaskEventSeq = ref(0)
const activeTaskIdByConversation = ref<Record<string, string>>({})
const activeTaskEventSeqByConversation = ref<Record<string, number>>({})
const sendingConversationKey = ref('')
const conversations = ref<Conversation[]>([])
const entries = ref<TranscriptEntry[]>([])
const draftEntriesByConversation = ref<Record<string, TranscriptEntry[]>>({})
const draftAttachmentsByConversation = ref<Record<string, DraftAttachmentItem[]>>({})
const pendingConversationById = ref<Record<string, Conversation>>({})
const approvalDecisionStateById = ref<Record<string, { pending: boolean; decision: 'approve' | 'reject' }>>({})
const questionResponseStateById = ref<Record<string, { pending: boolean }>>({})
const errorMessage = ref('')
const workspaceErrorAction = ref<WorkspaceErrorAction | null>(null)
const modelCatalog = ref<ModelCatalog | null>(null)
const catalogLoading = ref(false)
const availableSkills = ref<WorkspaceSkillListItem[]>([])
const selectedSkillsByConversation = ref<Record<string, string[]>>({})
const selectedWorkspaceModeByConversation = ref<Record<string, WorkspaceMode>>({})
const pendingWorkspaceMergeTaskIdByConversation = ref<Record<string, string>>({})
const workspaceStateByTaskId = ref<Record<string, TaskWorkspaceStateStatus>>({})
const workspaceStateByConversationId = ref<Record<string, TaskWorkspaceStateStatus>>({})
const selectedProviderId = ref('')
const selectedModelId = ref('')
const modelMenuOpen = ref(false)
const modelMenuRef = ref<HTMLElement | null>(null)
const contextStatsOpen = ref(false)
const contextStatsRef = ref<HTMLElement | null>(null)
const composerRef = ref<InstanceType<typeof MessageComposer> | null>(null)
const showThinkingAndTools = ref(true)
const workspaceMergeActionPending = ref('')
const initialized = ref(false)
let activeStreamAbortController: AbortController | null = null
let activeStreamingTaskId = ''

const isAdmin = computed(() => getSessionRole() === 'admin')
const routeConversationId = computed(() => (typeof route.params.conversationId === 'string' ? route.params.conversationId : ''))
const sidebarConversations = computed(() => {
  const merged = [...conversations.value]
  for (const pendingConversation of Object.values(pendingConversationById.value)) {
    if (!merged.some((conversation) => conversation.id === pendingConversation.id)) {
      merged.unshift(pendingConversation)
    }
  }
  return merged
})
const chatShellClass = computed(() => ({
  'sidebar-hidden': !sidebarMobile.value && sidebarCollapsed.value,
  'sidebar-mobile': sidebarMobile.value,
  'sidebar-open': sidebarMobile.value && sidebarDrawerOpen.value,
}))
const sidebarDesktopHidden = computed(() => !sidebarMobile.value && sidebarCollapsed.value)
const availableProviders = computed(() => filterUsableModelProviders(modelCatalog.value?.providers ?? []))
const selectedProvider = computed(
  () => availableProviders.value.find((provider) => provider.id === selectedProviderId.value) ?? availableProviders.value[0] ?? null,
)
const availableModels = computed(() => selectedProvider.value?.models ?? [])
const selectedModel = computed<ModelCatalogEntry | null>(
  () => availableModels.value.find((item) => item.id === selectedModelId.value) ?? availableModels.value[0] ?? null,
)
const selectedModelLabel = computed(() => selectedModel.value?.name || selectedModelId.value || '选择模型')
const selectedSkillNames = computed(() => selectedSkillsByConversation.value[activeConversationId.value] ?? [])
const selectedWorkspaceMode = computed(() => selectedWorkspaceModeByConversation.value[activeConversationId.value] ?? DEFAULT_WORKSPACE_MODE)
const currentAttachmentDraftKey = computed(() => activeConversationId.value || NEW_CONVERSATION_SENDING_KEY)
const currentDraftAttachments = computed(() => draftAttachmentsByConversation.value[currentAttachmentDraftKey.value] ?? [])
const attachmentsUploading = computed(() => currentDraftAttachments.value.some((attachment) => attachment.upload_state === 'uploading'))
const selectedModelSupportsAttachments = computed(() => selectedModel.value?.capabilities?.attachments === true)
const activeConversationTaskId = computed(() => (activeConversationId.value ? activeTaskIdByConversation.value[activeConversationId.value] ?? '' : ''))
const currentConversationBusy = computed(() => {
  if (activeConversationTaskId.value) {
    return true
  }
  if (!activeConversationId.value && activeTaskId.value) {
    return true
  }
  const currentKey = activeConversationId.value || NEW_CONVERSATION_SENDING_KEY
  return sendingConversationKey.value === currentKey
})
const modelMenuDisabled = computed(() => currentConversationBusy.value || catalogLoading.value || availableProviders.value.length === 0)
const noUsableModels = computed(() => !catalogLoading.value && availableProviders.value.length === 0)
const composerDisabled = computed(() => catalogLoading.value || noUsableModels.value || !selectedProviderId.value || !selectedModelId.value)
const stoppingTask = ref(false)
const currentPendingWorkspaceMergeTaskId = computed(() => {
  const conversationId = activeConversationId.value || routeConversationId.value
  if (conversationId) {
    return pendingWorkspaceMergeTaskIdByConversation.value[conversationId] ?? ''
  }
  const confirmedPendingEntries = Object.entries(pendingWorkspaceMergeTaskIdByConversation.value).filter(
    ([pendingConversationId]) => workspaceStateByConversationId.value[pendingConversationId] === 'pending_merge',
  )
  return confirmedPendingEntries.length === 1 ? confirmedPendingEntries[0][1] : ''
})
const currentConversationEntries = computed(() => {
  const conversationId = activeConversationId.value || routeConversationId.value
  if (conversationId) {
    return draftEntriesByConversation.value[conversationId] ?? entries.value
  }
  return entries.value
})
const visibleConversationEntries = computed(() => currentConversationEntries.value.filter((entry) => !entry.memory_context_state))
const showNoModelEmpty = computed(() => noUsableModels.value && visibleConversationEntries.value.length === 0)
const topbarStatusLabel = computed(() => (messagesLoading.value || currentConversationBusy.value ? '同步中' : '就绪'))
const topbarStatusClass = computed(() => ({
  'status-pill': true,
  idle: !messagesLoading.value && !currentConversationBusy.value,
  loading: messagesLoading.value || currentConversationBusy.value,
}))

function clearErrorState() {
  errorMessage.value = ''
  workspaceErrorAction.value = null
}

function workspaceErrorActionFromError(error: unknown): WorkspaceErrorAction | null {
  if (!(error instanceof ApiError) || !error.data || typeof error.data !== 'object') {
    return null
  }
  const data = error.data as WorkspaceActionErrorData
  if (data.code !== 'workspace_home_changed' && data.code !== 'workspace_pending_merge') {
    return null
  }
  const conversationId = typeof data.conversation_id === 'string' ? data.conversation_id.trim() : ''
  if (!conversationId) {
    return null
  }
  const message = typeof data.message === 'string' && data.message.trim()
    ? data.message.trim()
    : error.message
  return { message, conversationId }
}

function applyErrorState(error: unknown, fallback: string) {
  const action = workspaceErrorActionFromError(error)
  workspaceErrorAction.value = action
  errorMessage.value = action?.message ?? (error instanceof Error ? error.message : fallback)
}

async function handleWorkspaceErrorNavigation() {
  const targetConversationId = workspaceErrorAction.value?.conversationId
  if (!targetConversationId) {
    return
  }
  clearErrorState()
  await navigateToConversation(targetConversationId)
}

function activeConversationTitle() {
  const current = sidebarConversations.value.find((conversation) => conversation.id === activeConversationId.value)
  if (activeConversationId.value) {
    return formatConversationTitle(current?.title ?? '', '未命名对话')
  }
  return '新对话'
}

function syncDocumentTitle() {
  document.title = formatDocumentTitle(activeConversationTitle())
}

watch([activeConversationId, conversations], syncDocumentTitle, { deep: true, immediate: true })

function currentConversationContextEntries(conversationId: string) {
  if (!conversationId) {
    return entries.value
  }
  return draftEntriesByConversation.value[conversationId] ?? (conversationId === activeConversationId.value ? entries.value : [])
}

function setDraftEntries(conversationId: string, nextEntries: TranscriptEntry[]) {
  if (!conversationId) {
    return
  }
  draftEntriesByConversation.value = {
    ...draftEntriesByConversation.value,
    [conversationId]: nextEntries,
  }
}

function setTaskStateForConversation(conversationId: string, taskId: string, eventSeq?: number) {
  if (!conversationId) {
    activeTaskId.value = taskId
    if (typeof eventSeq === 'number') {
      activeTaskEventSeq.value = eventSeq
    }
    return
  }

  activeTaskIdByConversation.value = {
    ...activeTaskIdByConversation.value,
    [conversationId]: taskId,
  }

  if (typeof eventSeq === 'number') {
    activeTaskEventSeqByConversation.value = {
      ...activeTaskEventSeqByConversation.value,
      [conversationId]: eventSeq,
    }
  }

  if (conversationId === activeConversationId.value) {
    activeTaskId.value = taskId
    activeTaskEventSeq.value = typeof eventSeq === 'number' ? eventSeq : activeTaskEventSeqByConversation.value[conversationId] ?? 0
  }
}

function clearTaskStateForConversation(conversationId: string) {
  if (conversationId) {
    const nextTaskIds = { ...activeTaskIdByConversation.value }
    delete nextTaskIds[conversationId]
    activeTaskIdByConversation.value = nextTaskIds

    const nextTaskEventSeqs = { ...activeTaskEventSeqByConversation.value }
    delete nextTaskEventSeqs[conversationId]
    activeTaskEventSeqByConversation.value = nextTaskEventSeqs
  }

  if (!conversationId || conversationId === activeConversationId.value) {
    activeTaskId.value = ''
    activeTaskEventSeq.value = 0
  }
}

function syncActiveTaskStateFromConversation(conversationId: string) {
  activeConversationId.value = conversationId
  activeTaskId.value = conversationId ? activeTaskIdByConversation.value[conversationId] ?? '' : ''
  activeTaskEventSeq.value = conversationId ? activeTaskEventSeqByConversation.value[conversationId] ?? 0 : 0
}

function setWorkspaceModeForConversation(conversationId: string, mode: WorkspaceMode) {
  selectedWorkspaceModeByConversation.value = {
    ...selectedWorkspaceModeByConversation.value,
    [conversationId]: mode,
  }
}

function handleWorkspaceModeChange(mode: WorkspaceMode) {
  setWorkspaceModeForConversation(activeConversationId.value, mode)
}

function setPendingWorkspaceMergeTask(conversationId: string, taskId: string) {
  if (!conversationId || !taskId) {
    return
  }
  pendingWorkspaceMergeTaskIdByConversation.value = {
    ...pendingWorkspaceMergeTaskIdByConversation.value,
    [conversationId]: taskId,
  }
}

function clearPendingWorkspaceMergeTask(conversationId: string) {
  if (!conversationId || !(conversationId in pendingWorkspaceMergeTaskIdByConversation.value)) {
    return
  }
  const nextPending = { ...pendingWorkspaceMergeTaskIdByConversation.value }
  delete nextPending[conversationId]
  pendingWorkspaceMergeTaskIdByConversation.value = nextPending
}

function resolvePendingWorkspaceMergeConversationId(taskId: string) {
  if (!taskId) {
    return ''
  }
  const entry = Object.entries(pendingWorkspaceMergeTaskIdByConversation.value).find(([, pendingTaskId]) => pendingTaskId === taskId)
  return entry?.[0] ?? ''
}

function syncWorkspaceMergeStateFromTask(task: TaskDetails | null | undefined, fallbackConversationId = '') {
  if (!task) {
    return
  }
  const conversationId = resolveTaskConversationId(task) || fallbackConversationId
  if (!conversationId) {
    return
  }
  const conversationWorkspaceState = workspaceStateByConversationId.value[conversationId]
  if (conversationWorkspaceState && conversationWorkspaceState !== 'pending_merge') {
    clearPendingWorkspaceMergeTask(conversationId)
    return
  }
  const workspaceState = workspaceStateByTaskId.value[task.id]
  if (workspaceState && workspaceState !== 'pending_merge') {
    clearPendingWorkspaceMergeTask(conversationId)
    return
  }
  if (isTaskPendingWorkspaceMerge(task)) {
    setPendingWorkspaceMergeTask(conversationId, task.id)
  } else {
    clearPendingWorkspaceMergeTask(conversationId)
  }
}

function syncWorkspaceMergeStateFromWorkspaceState(state: TaskWorkspaceState | null | undefined, fallbackConversationId = '') {
  if (!state) {
    if (fallbackConversationId) {
      workspaceStateByConversationId.value = {
        ...workspaceStateByConversationId.value,
        [fallbackConversationId]: 'completed',
      }
      clearPendingWorkspaceMergeTask(fallbackConversationId)
    }
    return
  }
  const conversationId =
    fallbackConversationId ||
    state.conversation_id ||
    state.workspace_id ||
    activeConversationId.value ||
    routeConversationId.value ||
    resolvePendingWorkspaceMergeConversationId(state.task_id)
  if (!conversationId) {
    return
  }
  const workspaceKey = state.task_id || state.workspace_id || conversationId
  workspaceStateByConversationId.value = {
    ...workspaceStateByConversationId.value,
    [conversationId]: state.state,
  }
  workspaceStateByTaskId.value = {
    ...workspaceStateByTaskId.value,
    [workspaceKey]: state.state,
  }
  if (state.state === 'pending_merge') {
    setPendingWorkspaceMergeTask(conversationId, workspaceKey)
  } else {
    clearPendingWorkspaceMergeTask(conversationId)
  }
}

async function syncConversationWorkspaceState(conversationId: string) {
  if (!conversationId) {
    return
  }
  const state = await fetchConversationWorkspaceState(conversationId)
  syncWorkspaceMergeStateFromWorkspaceState(state, conversationId)
}

function currentChatState() {
  return {
    activeConversationId: activeConversationId.value,
    activeTaskId: activeTaskId.value,
    activeTaskEventSeq: activeTaskEventSeq.value,
    activeTaskIdByConversation: activeTaskIdByConversation.value,
    activeTaskEventSeqByConversation: activeTaskEventSeqByConversation.value,
    selectedSkillsByConversation: selectedSkillsByConversation.value,
    selectedWorkspaceModeByConversation: selectedWorkspaceModeByConversation.value,
    pendingWorkspaceMergeTaskIdByConversation: pendingWorkspaceMergeTaskIdByConversation.value,
  }
}

function syncChatState() {
  if (!initialized.value) {
    return
  }
  scheduleChatStateSave(currentChatState())
}

function flushChatState() {
  saveChatState(currentChatState())
}

watch(
  [activeConversationId, activeTaskId, activeTaskEventSeq, activeTaskIdByConversation, activeTaskEventSeqByConversation, selectedSkillsByConversation, selectedWorkspaceModeByConversation, pendingWorkspaceMergeTaskIdByConversation],
  syncChatState,
  { deep: true },
)

function setDraftAttachments(key: string, attachments: DraftAttachmentItem[]) {
  draftAttachmentsByConversation.value = {
    ...draftAttachmentsByConversation.value,
    [key]: attachments,
  }
}

function clearDraftAttachments(key: string) {
  if (!key || !(key in draftAttachmentsByConversation.value)) {
    return
  }
  const removing = draftAttachmentsByConversation.value[key] ?? []
  for (const item of removing) {
    if (item.preview_url) {
      URL.revokeObjectURL(item.preview_url)
    }
  }
  const next = { ...draftAttachmentsByConversation.value }
  delete next[key]
  draftAttachmentsByConversation.value = next
}

function upsertPendingConversation(conversation: Conversation) {
  pendingConversationById.value = {
    ...pendingConversationById.value,
    [conversation.id]: conversation,
  }
}

function dropPendingConversation(conversationId: string) {
  if (!conversationId || !(conversationId in pendingConversationById.value)) {
    return
  }
  const nextPending = { ...pendingConversationById.value }
  delete nextPending[conversationId]
  pendingConversationById.value = nextPending
}

function prunePendingConversations(loadedConversations: Conversation[]) {
  if (Object.keys(pendingConversationById.value).length === 0) {
    return
  }
  const loadedConversationIds = new Set(loadedConversations.map((conversation) => conversation.id))
  const nextPending = Object.fromEntries(
    Object.entries(pendingConversationById.value).filter(([conversationId]) => !loadedConversationIds.has(conversationId)),
  )
  if (Object.keys(nextPending).length !== Object.keys(pendingConversationById.value).length) {
    pendingConversationById.value = nextPending
  }
}

async function navigateToConversation(conversationId = '') {
  const target = conversationId ? `/chat/${encodeURIComponent(conversationId)}` : '/chat'
  if (route.fullPath === target) {
    return
  }
  await router.push(target)
}

function applySelection(providerId: string, modelId: string) {
  const nextSelection = resolveModelSelection(modelCatalog.value, providerId, modelId)
  selectedProviderId.value = nextSelection.providerId
  selectedModelId.value = nextSelection.modelId
}

function applyDefaultSelection() {
  if (!modelCatalog.value) {
    return
  }
  applySelection(modelCatalog.value.default_provider_id, modelCatalog.value.default_model_id)
}

function syncSelectionFromConversation(conversationId: string) {
  const conversation = sidebarConversations.value.find((item) => item.id === conversationId)
  if (!conversation) {
    return false
  }
  applySelection(conversation.provider_id, conversation.model_id)
  return true
}

function closeModelMenu() {
  modelMenuOpen.value = false
}

function toggleModelMenu() {
  if (modelMenuDisabled.value) {
    return
  }
  modelMenuOpen.value = !modelMenuOpen.value
}

function closeContextStats() {
  contextStatsOpen.value = false
}

function chooseModel(providerId: string, modelId: string) {
  applySelection(providerId, modelId)
  closeModelMenu()
}

function toggleSidebarCollapsed() {
  if (sidebarMobile.value) {
    sidebarDrawerOpen.value = !sidebarDrawerOpen.value
    return
  }
  sidebarCollapsed.value = !sidebarCollapsed.value
}

function closeSidebarDrawer() {
  sidebarDrawerOpen.value = false
}

function syncSidebarViewport() {
  const mobile = window.innerWidth <= 960
  sidebarMobile.value = mobile

  if (!mobile) {
    sidebarDrawerOpen.value = false
  }
}

async function loadCatalog() {
  catalogLoading.value = true
  try {
    modelCatalog.value = await fetchModelCatalog()
    applySelection(selectedProviderId.value || modelCatalog.value.default_provider_id, selectedModelId.value || modelCatalog.value.default_model_id)
  } catch (error) {
    applyErrorState(error, '加载模型目录失败')
  } finally {
    catalogLoading.value = false
  }
}

async function loadAvailableSkills() {
  try {
    availableSkills.value = await fetchSkills()
  } catch {
    availableSkills.value = []
  }
}

async function loadConversations(preferredConversationId = routeConversationId.value) {
  sidebarLoading.value = true

  try {
    const loadedConversations = await fetchConversations()
    conversations.value = Array.isArray(loadedConversations) ? loadedConversations : []
    prunePendingConversations(conversations.value)

    if (preferredConversationId) {
      const exists = sidebarConversations.value.some((conversation) => conversation.id === preferredConversationId)
      if (exists) {
        syncSelectionFromConversation(preferredConversationId)
      } else if (routeConversationId.value === preferredConversationId && !(preferredConversationId in pendingConversationById.value)) {
        await navigateToConversation('')
      }
    }
  } finally {
    sidebarLoading.value = false
  }
}

function formatCount(value: number | undefined) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return '--'
  }
  return value.toLocaleString('en-US')
}

function formatPercent(value: number | undefined) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return '--'
  }
  if (value === 0) {
    return '0%'
  }
  return `${(value * 100).toFixed(value >= 0.1 ? 1 : 2)}%`
}

const currentContextUsage = computed(() => {
  const activeConversation = sidebarConversations.value.find((conversation) => conversation.id === activeConversationId.value)
  const preferTranscriptMemory = Boolean(activeConversationTaskId.value)
  let latestReply: TranscriptEntry | null = null
  let latestContextState: MemoryContextState | null = null
  let latestCompression = null

  for (const entry of currentConversationEntries.value) {
    if (entry.kind === 'reply' && (entry.token_usage || entry.provider_id || entry.model_id)) {
      latestReply = entry
    }
    if (entry.kind === 'memory' && entry.memory_context_state) {
      latestContextState = entry.memory_context_state
    }
    if (entry.kind === 'memory' && entry.memory_compression) {
      latestCompression = entry.memory_compression
    }
  }

  const memoryState = preferTranscriptMemory
    ? latestContextState ?? activeConversation?.memory_context ?? null
    : activeConversation?.memory_context ?? null
  const compression = preferTranscriptMemory
    ? latestCompression ?? activeConversation?.memory_compression ?? null
    : activeConversation?.memory_compression ?? null

  const modelId = latestReply?.model_id || selectedModelId.value
  const providerId = latestReply?.provider_id || selectedProviderId.value

  const maxContextTokens = memoryState?.max_context_tokens
  const shortTermLimit = memoryState?.short_term_limit

  const usedTokens = memoryState?.total_tokens

  const ratio = typeof maxContextTokens === 'number' && maxContextTokens > 0
    ? typeof usedTokens === 'number'
      ? Math.min(usedTokens / maxContextTokens, 1)
      : undefined
    : undefined

  return {
    modelLabel: [providerId, modelId].filter(Boolean).join(' / '),
    usedTokens,
    maxContextTokens,
    shortTermLimit,
    ratio,
    compression: compression
      ? {
        before: compression.total_tokens_before,
        after: compression.total_tokens_after,
        saved: Math.max(compression.total_tokens_before - compression.total_tokens_after, 0),
        ratio: compression.total_tokens_before > 0 ? compression.total_tokens_after / compression.total_tokens_before : undefined,
      }
      : null,
    memoryState,
  }
})

const contextStatsSummary = computed(() => {
  const cu = currentContextUsage.value
  return {
    used: formatCount(cu.usedTokens),
    max: formatCount(cu.maxContextTokens),
    shortTermLimit: formatCount(cu.shortTermLimit),
    ratio: formatPercent(cu.ratio),
    compressionBefore: formatCount(cu.compression?.before),
    compressionAfter: formatCount(cu.compression?.after),
    compressionSaved: formatCount(cu.compression?.saved),
    compressionRatio: formatPercent(cu.compression?.ratio),
    // New: backend memory state breakdown
    shortTermTokens: formatCount(cu.memoryState?.short_term_tokens),
    summaryTokens: formatCount(cu.memoryState?.summary_tokens),
  }
})

const contextRingProgress = computed(() => {
  const ratio = currentContextUsage.value.ratio
  return typeof ratio === 'number' && Number.isFinite(ratio) ? Math.max(0, Math.min(ratio, 1)) : 0
})

const contextRingPercent = computed(() => Math.round(contextRingProgress.value * 100))

const contextRingColor = computed(() => {
  const p = contextRingPercent.value
  if (p >= 90) return '#c0392b'
  if (p >= 75) return '#e67e22'
  return '#56726a'
})

async function resumeStreamForConversation(conversationId: string) {
  if (!conversationId) {
    return
  }

  const savedTaskId = activeTaskIdByConversation.value[conversationId] ?? ''
  if (savedTaskId) {
    try {
      const task = await fetchTaskDetails(savedTaskId)
      const taskConversationId = resolveTaskConversationId(task)
      if (!taskConversationId || taskConversationId === conversationId) {
        await resumeTask(task, conversationId)
        if (activeTaskIdByConversation.value[conversationId]) {
          return
        }
      }
    } catch {
      clearTaskStateForConversation(conversationId)
    }
  }

  const task = await findRunningTaskByConversation(conversationId)
  await resumeTask(task, conversationId)
}

async function loadConversationForRoute(conversationId: string) {
  contextStatsOpen.value = false
  syncActiveTaskStateFromConversation(conversationId)
  sidebarDrawerOpen.value = false
  clearErrorState()

  if (!conversationId) {
    if (!selectedProviderId.value || !selectedModelId.value) {
      applyDefaultSelection()
    }
    return
  }

  syncSelectionFromConversation(conversationId)

  // If this conversation has an active streaming task, use the in-memory cache
  // to avoid losing real-time data. Otherwise always fetch from API.
  const isStreaming = Boolean(activeTaskIdByConversation.value[conversationId])
  const draft = draftEntriesByConversation.value[conversationId]
  if (isStreaming && draft && draft.length > 0) {
    entries.value = draft
  } else {
    messagesLoading.value = true
    try {
      const messages = await fetchConversationMessages(conversationId)
      const nextEntries = buildTranscriptEntries(messages)
      setDraftEntries(conversationId, nextEntries)
      // Update entries if user is viewing this conversation (via route or programmatic selection)
      if (conversationId === routeConversationId.value || conversationId === activeConversationId.value) {
        entries.value = nextEntries
      }
    } catch (error) {
      applyErrorState(error, '加载消息失败')
    } finally {
      messagesLoading.value = false
    }
  }

  try {
    await syncConversationWorkspaceState(conversationId)
  } catch (error) {
    applyErrorState(error, '同步工作区状态失败')
  }

  await resumeStreamForConversation(conversationId)

  try {
    await syncConversationWorkspaceState(conversationId)
  } catch (error) {
    applyErrorState(error, '同步工作区状态失败')
  }
}

function applyEntriesForConversation(conversationId: string, nextEntries: TranscriptEntry[]) {
  if (conversationId) {
    setDraftEntries(conversationId, nextEntries)
  }
  if (conversationId === activeConversationId.value) {
    entries.value = nextEntries
  }
}

function stopActiveStream() {
  activeStreamAbortController?.abort()
  activeStreamAbortController = null
  activeStreamingTaskId = ''
}

async function completeTaskConversation(conversationId: string, taskId: string, result: RunTaskResult) {
  const sourceEntries = currentConversationContextEntries(conversationId).length > 0 ? currentConversationContextEntries(conversationId) : entries.value
  const nextEntries = attachReplyMetaToLatestReply(sourceEntries, {
    provider_id: result.provider_id || selectedProviderId.value,
    model_id: result.model_id || selectedModelId.value,
    token_usage: result.usage,
  })
  setDraftEntries(conversationId, nextEntries)
  if (conversationId === activeConversationId.value || routeConversationId.value === conversationId || !routeConversationId.value) {
    entries.value = nextEntries
  }
  dropPendingConversation(conversationId)
  if (result.workspace_state) {
    workspaceStateByConversationId.value = {
      ...workspaceStateByConversationId.value,
      [conversationId]: result.workspace_state,
    }
    workspaceStateByTaskId.value = {
      ...workspaceStateByTaskId.value,
      [taskId]: result.workspace_state,
    }
    if (result.workspace_state === 'pending_merge') {
      setPendingWorkspaceMergeTask(conversationId, taskId)
    } else {
      clearPendingWorkspaceMergeTask(conversationId)
    }
  }
  clearTaskStateForConversation(conversationId)
  await loadConversations(routeConversationId.value || conversationId)

  // After streaming completes, re-fetch from API to ensure consistency
  // between the streaming incremental state and the server's final state.
  try {
    const messages = await fetchConversationMessages(conversationId)
    const freshEntries = buildTranscriptEntries(messages)
    setDraftEntries(conversationId, freshEntries)
    // Update displayed entries if this conversation is currently visible:
    // - directly selected, matched by route, or user is on the home route
    if (conversationId === activeConversationId.value || routeConversationId.value === conversationId || !routeConversationId.value) {
      entries.value = freshEntries
    }
  } catch {
    // Non-critical: keep the streaming-derived entries as fallback
  }
}

async function attachTaskStream(taskId: string, conversationId = '') {
  if (!taskId || activeStreamingTaskId === taskId) {
    return
  }

  stopActiveStream()
  const abortController = new AbortController()
  activeStreamAbortController = abortController
  activeStreamingTaskId = taskId
  const initialConversationId = conversationId || activeConversationId.value
  setTaskStateForConversation(initialConversationId, taskId, activeTaskEventSeqByConversation.value[initialConversationId] ?? 0)

  let streamConversationId = initialConversationId
  let firstVisibleChunkSeen = false

  try {
    const result = await streamRunTask(
      taskId,
      () => {
        void 0
      },
      (event) => {
        const eventConversationId =
          (typeof event.payload?.conversation_id === 'string' ? event.payload.conversation_id : '') || streamConversationId || initialConversationId
        if (eventConversationId && !streamConversationId) {
          streamConversationId = eventConversationId
        }

        const nextSeq = Math.max(activeTaskEventSeqByConversation.value[eventConversationId] ?? 0, event.seq ?? 0)
        setTaskStateForConversation(eventConversationId, taskId, nextSeq)
        const currentEntries = currentConversationContextEntries(eventConversationId)
        const nextEntries = updateTranscriptFromStreamEvent(currentEntries, event)
        applyEntriesForConversation(eventConversationId, nextEntries)

        const isVisibleTextChunk =
          !firstVisibleChunkSeen &&
          !!eventConversationId &&
          event.type === 'log.message' &&
          typeof event.payload?.Kind === 'string' &&
          event.payload.Kind === 'text_delta' &&
          typeof event.payload?.Text === 'string' &&
          event.payload.Text.length > 0
        if (isVisibleTextChunk) {
          firstVisibleChunkSeen = true
          void loadConversations(eventConversationId)
        }
      },
      { signal: abortController.signal, afterSeq: activeTaskEventSeqByConversation.value[initialConversationId] ?? 0 },
    )

    streamConversationId = result.conversation_id || streamConversationId
    await completeTaskConversation(streamConversationId, taskId, result)
  } catch (error) {
    if (error instanceof Error && error.message === TASK_STREAM_ABORTED_MESSAGE) {
      return
    }

    const taskError = error instanceof Error ? error.message : '发送消息失败'
    try {
      const task = await fetchTaskDetails(taskId)
      if (!isTaskActive(task)) {
        const taskConversationId = resolveTaskConversationId(task) || streamConversationId || initialConversationId
        syncWorkspaceMergeStateFromTask(task, taskConversationId)
        if (task.status === 'cancelled') {
          clearTaskStateForConversation(taskConversationId)
          return
        }
        if (isTaskPendingWorkspaceMerge(task)) {
          clearTaskStateForConversation(taskConversationId)
          return
        }
        if (workspaceErrorActionFromError(error) && taskConversationId === activeConversationId.value) {
          applyErrorState(error, '发送消息失败')
        }
        const currentEntries = currentConversationContextEntries(taskConversationId)
        const nextEntries = updateTranscriptFromStreamEvent(currentEntries, {
          type: 'task.failed',
          payload: { error: taskError },
        })
        applyEntriesForConversation(taskConversationId, nextEntries)
        clearTaskStateForConversation(taskConversationId)
      }
    } catch {
      if (taskError !== 'Task event stream disconnected') {
        if (workspaceErrorActionFromError(error) && streamConversationId === activeConversationId.value) {
          applyErrorState(error, '发送消息失败')
        }
        const currentEntries = currentConversationContextEntries(streamConversationId)
        const nextEntries = updateTranscriptFromStreamEvent(currentEntries, {
          type: 'task.failed',
          payload: { error: taskError },
        })
        applyEntriesForConversation(streamConversationId, nextEntries)
      }
    }
  } finally {
    if (activeStreamAbortController === abortController) {
      activeStreamAbortController = null
    }
    if (activeStreamingTaskId === taskId) {
      activeStreamingTaskId = ''
    }
  }
}

async function resumeTask(task: TaskDetails | null | undefined, conversationId = '') {
  if (!task || !isTaskActive(task)) {
    if (task) {
      const taskConversationId = resolveTaskConversationId(task) || conversationId
      syncWorkspaceMergeStateFromTask(task, taskConversationId)
      clearTaskStateForConversation(taskConversationId)
    }
    return
  }

  const taskConversationId = resolveTaskConversationId(task)
  if (conversationId && taskConversationId && taskConversationId !== conversationId) {
    return
  }

  const targetConversationId = taskConversationId || conversationId
  setTaskStateForConversation(targetConversationId, task.id, activeTaskEventSeqByConversation.value[targetConversationId] ?? 0)
  await hydratePendingApprovals(task, targetConversationId)
  await attachTaskStream(task.id, targetConversationId)
}

async function hydratePendingApprovals(task: TaskDetails | null | undefined, conversationId = '') {
  if (!task || !isTaskWaitingForInput(task)) {
    return
  }

  const taskConversationId = resolveTaskConversationId(task) || conversationId || activeConversationId.value
  if (!taskConversationId) {
    return
  }

  let nextEntries = currentConversationContextEntries(taskConversationId)
  if (task.suspend_reason === TASK_WAITING_FOR_INTERACTION_REASON) {
    try {
      const interactions = await fetchTaskInteractions(task.id)
      for (const interaction of interactions.filter((item) => item.status === 'pending')) {
        nextEntries = updateTranscriptFromStreamEvent(nextEntries, {
          type: 'interaction.requested',
          payload: interaction as unknown as Record<string, unknown>,
        })
      }
      applyEntriesForConversation(taskConversationId, nextEntries)
      return
    } catch {
      return
    }
  }

  let approvals
  try {
    approvals = await fetchTaskApprovals(task.id)
  } catch {
    return
  }
  const pendingApprovals = approvals.filter((approval) => approval.status === 'pending')
  if (pendingApprovals.length === 0) {
    return
  }
  applyEntriesForConversation(taskConversationId, buildApprovalEntriesFromList(pendingApprovals, nextEntries))
}

async function handleApprovalDecision(input: {
  taskId: string
  approvalId: string
  decision: 'approve' | 'reject'
  reason: string
}) {
  const taskId = input.taskId || activeTaskId.value
  if (!taskId || !input.approvalId || approvalDecisionStateById.value[input.approvalId]?.pending) {
    return
  }

  try {
    clearErrorState()
    approvalDecisionStateById.value = {
      ...approvalDecisionStateById.value,
      [input.approvalId]: { pending: true, decision: input.decision },
    }
    const approval = await decideTaskApproval(taskId, input.approvalId, {
      decision: input.decision,
      reason: input.reason,
    })
    const conversationId = approval.conversation_id || activeConversationId.value
    const nextEntries = updateTranscriptFromStreamEvent(
      currentConversationContextEntries(conversationId),
      buildApprovalStreamEvent(approval, { type: 'approval.resolved', decision: input.decision }),
    )
    applyEntriesForConversation(conversationId, nextEntries)
    void attachTaskStream(taskId, conversationId)
  } catch (error) {
    applyErrorState(error, '审批提交失败')
  } finally {
    const nextState = { ...approvalDecisionStateById.value }
    delete nextState[input.approvalId]
    approvalDecisionStateById.value = nextState
  }
}

async function handleInteractionRespond(input: QuestionInteractionSubmitInput) {
  const taskId = input.taskId || activeTaskId.value
  if (!taskId || !input.interactionId || questionResponseStateById.value[input.interactionId]?.pending) {
    return
  }

  try {
    clearErrorState()
    questionResponseStateById.value = {
      ...questionResponseStateById.value,
      [input.interactionId]: { pending: true },
    }
    const interaction = await respondTaskInteraction(taskId, input.interactionId, {
      selected_option_id: input.selectedOptionId,
      selected_option_ids: input.selectedOptionIds,
      custom_text: input.customText,
    })
    const conversationId = interaction.conversation_id || activeConversationId.value
    const nextEntries = updateTranscriptFromStreamEvent(currentConversationContextEntries(conversationId), {
      type: 'interaction.responded',
      payload: interaction as unknown as Record<string, unknown>,
    })
    applyEntriesForConversation(conversationId, nextEntries)
    void attachTaskStream(taskId, conversationId)
  } catch (error) {
    applyErrorState(error, '提交回答失败')
  } finally {
    const nextState = { ...questionResponseStateById.value }
    delete nextState[input.interactionId]
    questionResponseStateById.value = nextState
  }
}

async function handleWorkspaceMergeAction(action: 'confirm' | 'discard') {
  const taskId = currentPendingWorkspaceMergeTaskId.value
  if (!taskId || workspaceMergeActionPending.value) {
    return
  }

  const conversationId = activeConversationId.value || routeConversationId.value || resolvePendingWorkspaceMergeConversationId(taskId)
  try {
    clearErrorState()
    workspaceMergeActionPending.value = action
    let workspaceState
    if (conversationId) {
      workspaceState =
        action === 'confirm'
          ? await confirmConversationWorkspaceMerge(conversationId)
          : await discardConversationWorkspaceChanges(conversationId)
    } else if (action === 'confirm') {
      workspaceState = await confirmTaskWorkspaceMerge(taskId)
    } else {
      workspaceState = await discardTaskWorkspaceChanges(taskId)
    }
    syncWorkspaceMergeStateFromWorkspaceState(workspaceState, conversationId)
    if (workspaceState?.state && workspaceState.state !== 'pending_merge') {
      workspaceStateByTaskId.value = {
        ...workspaceStateByTaskId.value,
        [taskId]: workspaceState.state,
      }
      if (conversationId) {
        workspaceStateByConversationId.value = {
          ...workspaceStateByConversationId.value,
          [conversationId]: workspaceState.state,
        }
      }
    }
    await loadConversations(conversationId || routeConversationId.value)
  } catch (error) {
    applyErrorState(error, '工作区变更处理失败')
  } finally {
    workspaceMergeActionPending.value = ''
  }
}

async function handleStopTask() {
  const taskId = activeTaskId.value
  if (!taskId || stoppingTask.value) {
    return
  }

  try {
    stoppingTask.value = true
    clearErrorState()
    let task = await cancelTask(taskId)
    if (task.status === 'cancel_requested') {
      task = await cancelTask(taskId)
    }
    if (!isTaskActive(task)) {
      stopActiveStream()
      const conversationId = resolveTaskConversationId(task) || activeConversationId.value
      const nextEntries = updateTranscriptFromStreamEvent(currentConversationContextEntries(conversationId), {
        type: 'task.finished',
        payload: { status: task.status, error: task.error },
      })
      applyEntriesForConversation(conversationId, nextEntries)
      clearTaskStateForConversation(conversationId)
    }
  } catch (error) {
    applyErrorState(error, '停止任务失败')
  } finally {
    stoppingTask.value = false
  }
}

function handleSkillSelectionChange(names: string[]) {
  const conversationId = activeConversationId.value
  selectedSkillsByConversation.value = {
    ...selectedSkillsByConversation.value,
    [conversationId]: names,
  }
}

async function handleSend(message: string) {
  clearErrorState()
  const previousConversationId = activeConversationId.value
  const startedFromRouteConversationId = routeConversationId.value
  const sendingKey = previousConversationId || NEW_CONVERSATION_SENDING_KEY
  const workspaceModeForRequest = selectedWorkspaceMode.value
  const readyAttachmentIDs = currentDraftAttachments.value
    .filter((attachment) => attachment.upload_state === 'uploaded' && attachment.id)
    .map((attachment) => attachment.id as string)
  sendingConversationKey.value = sendingKey

  const sentAttachments: AttachmentRef[] = currentDraftAttachments.value
    .filter((a) => a.upload_state === 'uploaded' && a.id)
    .map((a) => ({ id: a.id as string, file_name: a.file_name, mime_type: a.mime_type, size_bytes: a.size_bytes }))

  const nextEntries = [...currentConversationEntries.value, {
    id: `user-${Date.now()}`,
    kind: 'user' as const,
    title: 'You',
    content: message,
    ...(sentAttachments.length > 0 ? { attachments: sentAttachments } : {}),
  }]
  entries.value = nextEntries
  if (previousConversationId) {
    setDraftEntries(previousConversationId, nextEntries)
  }

  try {
    const task = await createRunTask({
      createdBy: username.value,
      conversationId: previousConversationId || undefined,
      providerId: selectedProviderId.value,
      modelId: selectedModelId.value,
      message,
      ...(readyAttachmentIDs.length > 0 ? { attachmentIds: readyAttachmentIDs } : {}),
      ...(selectedSkillNames.value.length > 0 ? { skills: selectedSkillNames.value } : {}),
      workspaceMode: workspaceModeForRequest,
    })
    const createdConversationId = task.input?.conversation_id ?? ''
    clearDraftAttachments(sendingKey)

    if (createdConversationId) {
      if (workspaceModeForRequest === 'mutable') {
        const nextConversationStates = { ...workspaceStateByConversationId.value }
        delete nextConversationStates[createdConversationId]
        workspaceStateByConversationId.value = nextConversationStates
      }
      setWorkspaceModeForConversation(createdConversationId, workspaceModeForRequest)
      setDraftEntries(createdConversationId, nextEntries)
      setTaskStateForConversation(createdConversationId, task.id, 0)
    }

    if (createdConversationId && !previousConversationId) {
      upsertPendingConversation({
        id: createdConversationId,
        title: '',
        last_message: '',
        message_count: 0,
        provider_id: selectedProviderId.value,
        model_id: selectedModelId.value,
        created_by: username.value,
        created_at: '',
        updated_at: '',
      })
      if (routeConversationId.value === startedFromRouteConversationId) {
        await navigateToConversation(createdConversationId)
      }
      await loadConversations(createdConversationId)
    }

    sendingConversationKey.value = ''
    await attachTaskStream(task.id, createdConversationId || previousConversationId)
  } catch (error) {
    if (!(error instanceof Error) || error.message !== TASK_STREAM_ABORTED_MESSAGE) {
      const taskError = error instanceof Error ? error.message : '发送消息失败'
      applyErrorState(error, '发送消息失败')
      entries.value = updateTranscriptFromStreamEvent(entries.value, {
        type: 'task.failed',
        payload: { error: taskError },
      })
    }
    sendingConversationKey.value = ''
  }
}

async function handleAddAttachments(files: File[]) {
  if (!selectedModelSupportsAttachments.value || files.length === 0) {
    return
  }
  const draftKey = currentAttachmentDraftKey.value
  const existing = draftAttachmentsByConversation.value[draftKey] ?? []
  const created = files.map<DraftAttachmentItem>((file, index) => ({
    local_id: `${Date.now()}-${index}-${file.name}`,
    file_name: file.name,
    mime_type: file.type,
    size_bytes: file.size,
    preview_url: file.type.startsWith('image/') ? URL.createObjectURL(file) : undefined,
    upload_state: 'uploading',
  }))
  setDraftAttachments(draftKey, [...existing, ...created])

  await Promise.all(
    created.map(async (draftAttachment, index) => {
      try {
        const uploaded = await uploadAttachment(files[index], activeConversationId.value || undefined)
        const next = (draftAttachmentsByConversation.value[draftKey] ?? []).map((item) =>
          item.local_id === draftAttachment.local_id
            ? { ...item, ...uploaded, upload_state: 'uploaded' as const }
            : item,
        )
        setDraftAttachments(draftKey, next)
      } catch (error) {
        const next = (draftAttachmentsByConversation.value[draftKey] ?? []).map((item) =>
          item.local_id === draftAttachment.local_id
            ? {
                ...item,
                upload_state: 'failed' as const,
                error_message: error instanceof Error ? error.message : '上传失败',
              }
            : item,
        )
        setDraftAttachments(draftKey, next)
      }
    }),
  )
}

async function handleRemoveAttachment(localId: string) {
  const draftKey = currentAttachmentDraftKey.value
  const current = draftAttachmentsByConversation.value[draftKey] ?? []
  const target = current.find((attachment) => attachment.local_id === localId)
  if (!target) {
    return
  }
  if (target.preview_url) {
    URL.revokeObjectURL(target.preview_url)
  }
  if (target.id) {
    try {
      await deleteAttachment(target.id)
    } catch (error) {
      setDraftAttachments(
        draftKey,
        current.map((attachment) =>
          attachment.local_id === localId
            ? {
                ...attachment,
                upload_state: 'failed' as const,
                error_message: error instanceof Error ? error.message : '删除附件失败',
              }
            : attachment,
        ),
      )
      return
    }
  }
  setDraftAttachments(
    draftKey,
    current.filter((attachment) => attachment.local_id !== localId),
  )
}

async function handleDeleteConversation(conversationId: string) {
  clearErrorState()

  try {
    await deleteConversation(conversationId)
    clearTaskStateForConversation(conversationId)
    if (activeConversationId.value === conversationId) {
      await navigateToConversation('')
      entries.value = []
    }
    await loadConversations(routeConversationId.value)
  } catch (error) {
    applyErrorState(error, '删除对话失败')
  }
}

async function selectConversation(conversationId: string) {
  if (conversationId === routeConversationId.value) {
    await loadConversationForRoute(conversationId)
    return
  }
  await navigateToConversation(conversationId)
}

function startNewConversation() {
  clearErrorState()
  contextStatsOpen.value = false
  modelMenuOpen.value = false
  sidebarDrawerOpen.value = false
  entries.value = []
  syncActiveTaskStateFromConversation('')
  setWorkspaceModeForConversation('', DEFAULT_WORKSPACE_MODE)
  applyDefaultSelection()
  void navigateToConversation('')
  void nextTick(() => {
    composerRef.value?.focus()
  })
}

async function handleLogout() {
  await logout()
  clearChatState()
  await router.push('/login')
}

function handleGlobalPointerDown(event: PointerEvent) {
  const target = event.target
  if (modelMenuOpen.value && !(target instanceof Node && modelMenuRef.value?.contains(target))) {
    closeModelMenu()
  }
  if (contextStatsOpen.value && !(target instanceof Node && contextStatsRef.value?.contains(target))) {
    closeContextStats()
  }
}

function handleGlobalKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    closeModelMenu()
    closeContextStats()
  }
}

watch(routeConversationId, (conversationId) => {
  if (!initialized.value) {
    return
  }
  void loadConversationForRoute(conversationId)
})

onMounted(async () => {
  syncSidebarViewport()
  window.addEventListener('resize', syncSidebarViewport)
  window.addEventListener('pointerdown', handleGlobalPointerDown)
  window.addEventListener('keydown', handleGlobalKeydown)

  const saved = loadChatState()
  activeConversationId.value = saved.activeConversationId
  activeTaskId.value = saved.activeTaskId
  activeTaskEventSeq.value = saved.activeTaskEventSeq
  activeTaskIdByConversation.value = { ...saved.activeTaskIdByConversation }
  activeTaskEventSeqByConversation.value = { ...saved.activeTaskEventSeqByConversation }
  if (saved.activeConversationId && saved.activeTaskId && !activeTaskIdByConversation.value[saved.activeConversationId]) {
    activeTaskIdByConversation.value[saved.activeConversationId] = saved.activeTaskId
  }
  if (saved.activeConversationId && !activeTaskEventSeqByConversation.value[saved.activeConversationId]) {
    activeTaskEventSeqByConversation.value[saved.activeConversationId] = saved.activeTaskEventSeq
  }
  selectedSkillsByConversation.value = saved.selectedSkillsByConversation
  selectedWorkspaceModeByConversation.value = saved.selectedWorkspaceModeByConversation
  pendingWorkspaceMergeTaskIdByConversation.value = saved.pendingWorkspaceMergeTaskIdByConversation

  await Promise.all([loadCatalog(), loadAvailableSkills(), loadConversations(routeConversationId.value || saved.activeConversationId)])
  initialized.value = true

  if (routeConversationId.value) {
    await loadConversationForRoute(routeConversationId.value)
  } else {
    syncActiveTaskStateFromConversation('')
    if (!selectedProviderId.value || !selectedModelId.value) {
      applyDefaultSelection()
    }
  }

  if (routeConversationId.value) {
    const savedTaskId = activeTaskIdByConversation.value[routeConversationId.value] ?? ''
    if (savedTaskId) {
      try {
        const task = await fetchTaskDetails(savedTaskId)
        await resumeTask(task, routeConversationId.value)
      } catch {
        clearTaskStateForConversation(routeConversationId.value)
      }
    }
  }
})

onBeforeUnmount(() => {
  flushChatState()
  stopActiveStream()
  window.removeEventListener('resize', syncSidebarViewport)
  window.removeEventListener('pointerdown', handleGlobalPointerDown)
  window.removeEventListener('keydown', handleGlobalKeydown)
})
</script>

<template>
  <main class="chat-shell" :class="chatShellClass">
    <button
      v-if="sidebarMobile && sidebarDrawerOpen"
      class="sidebar-backdrop"
      type="button"
      aria-label="Close conversations drawer"
      @click="closeSidebarDrawer"
    ></button>

    <ConversationSidebar
      :active-conversation-id="activeConversationId"
      :collapsed="sidebarCollapsed"
      :conversations="sidebarConversations"
      :desktop-hidden="sidebarDesktopHidden"
      :is-admin="isAdmin"
      :loading="sidebarLoading"
      :mobile="sidebarMobile"
      :open="sidebarDrawerOpen"
      :username="username"
      @create="startNewConversation"
      @close="closeSidebarDrawer"
      @delete="handleDeleteConversation"
      @logout="handleLogout"
      @open-profile="profileDialogOpen = true"
      @select="selectConversation"
      @toggle-collapse="toggleSidebarCollapsed"
    />

    <ProfileDialog :open="profileDialogOpen" @close="profileDialogOpen = false" />

    <section class="chat-stage" :class="{ 'composer-centered': entries.length === 0 && !noUsableModels }">
      <header class="topbar">
        <button
          v-if="sidebarMobile || sidebarCollapsed"
          class="ghost-button icon-button topbar-sidebar-toggle"
          type="button"
          :aria-label="sidebarMobile && sidebarDrawerOpen ? 'Close conversations' : 'Open conversations'"
          @click="toggleSidebarCollapsed"
        >
          <component :is="sidebarDrawerOpen ? Close : Menu" />
        </button>
        <div class="topbar-title-block">
          <div v-if="availableProviders.length > 0" ref="modelMenuRef" class="model-menu">
            <button
              class="model-menu-trigger"
              type="button"
              :disabled="modelMenuDisabled"
              aria-haspopup="menu"
              :aria-expanded="modelMenuOpen ? 'true' : 'false'"
              @click="toggleModelMenu"
            >
              <span class="model-menu-trigger-label">{{ selectedModelLabel }}</span>
              <span class="model-menu-trigger-caret" :class="{ open: modelMenuOpen }" aria-hidden="true"></span>
            </button>
            <transition name="model-menu-fade">
              <div v-if="modelMenuOpen" class="model-menu-panel" role="menu">
                <div v-for="provider in availableProviders" :key="provider.id" class="model-menu-group">
                  <div class="model-menu-group-label">{{ provider.name }}</div>
                  <button
                    v-for="item in provider.models"
                    :key="`${provider.id}:${item.id}`"
                    class="model-menu-option"
                    :class="{ active: provider.id === selectedProviderId && item.id === selectedModelId }"
                    type="button"
                    role="menuitemradio"
                    :aria-checked="provider.id === selectedProviderId && item.id === selectedModelId ? 'true' : 'false'"
                    :data-model-option="item.id"
                    @click="chooseModel(provider.id, item.id)"
                  >
                    <span class="model-menu-option-check" aria-hidden="true"></span>
                    <span class="model-menu-option-label">{{ item.name }}</span>
                  </button>
                </div>
              </div>
            </transition>
          </div>
          <strong class="topbar-conversation-title" :title="activeConversationTitle()">
            {{ activeConversationTitle() }}
          </strong>
        </div>
        <div class="topbar-right">
          <el-popover
              v-model:visible="contextStatsOpen"
              placement="bottom-end"
              trigger="click"
              popper-class="context-stats-popper"
              :width="300"
            >
              <template #reference>
                <button
                  class="context-stats-trigger"
                  type="button"
                  data-context-stats-trigger
                  :title="`上下文用量 ${contextStatsSummary.used} / ${contextStatsSummary.max}`"
                  :aria-expanded="contextStatsOpen ? 'true' : 'false'"
                  :aria-label="`上下文占用 ${contextStatsSummary.ratio}`"
                >
                  <el-progress
                    type="dashboard"
                    :percentage="contextRingPercent"
                    :width="28"
                    :stroke-width="3"
                    :show-text="false"
                    :color="contextRingColor"
                    class="context-stats-ring-el"
                  />
                  <span class="context-stats-ring-pct">{{ contextStatsSummary.ratio }}</span>
                </button>
              </template>
              <div class="context-stats-panel" data-context-stats-panel>
                <div class="context-stats-panel-header">
                  <span class="context-stats-panel-title">上下文用量</span>
                  <span class="context-stats-panel-model">{{ currentContextUsage.modelLabel || '当前模型未知' }}</span>
                </div>
                <div class="context-stats-overview">
                  <el-progress
                    type="dashboard"
                    :percentage="contextRingPercent"
                    :width="80"
                    :stroke-width="6"
                    :color="contextRingColor"
                    class="context-stats-dashboard"
                  >
                    <template #default>
                      <div class="context-stats-dashboard-inner">
                        <span class="context-stats-dashboard-pct">{{ contextStatsSummary.ratio }}</span>
                        <span class="context-stats-dashboard-label">已用</span>
                      </div>
                    </template>
                  </el-progress>
                  <div class="context-stats-overview-nums">
                    <div class="context-stats-num-row">
                      <span class="context-stats-num-label">已用 Token</span>
                      <span class="context-stats-num-val">{{ contextStatsSummary.used }}</span>
                    </div>
                    <div v-if="currentContextUsage.memoryState" class="context-stats-num-row">
                      <span class="context-stats-num-label">短期记忆</span>
                      <span class="context-stats-num-val">{{ contextStatsSummary.shortTermTokens }}</span>
                    </div>
                    <div v-if="currentContextUsage.memoryState?.has_summary" class="context-stats-num-row">
                      <span class="context-stats-num-label">压缩摘要</span>
                      <span class="context-stats-num-val">{{ contextStatsSummary.summaryTokens }}</span>
                    </div>
                    <div class="context-stats-num-row">
                      <span class="context-stats-num-label">上下文上限</span>
                      <span class="context-stats-num-val">{{ contextStatsSummary.max }}</span>
                    </div>
                    <div v-if="currentContextUsage.shortTermLimit" class="context-stats-num-row">
                      <span class="context-stats-num-label context-stats-num-label--compress">压缩触发</span>
                      <span class="context-stats-num-val context-stats-num-val--compress">{{ contextStatsSummary.shortTermLimit }}</span>
                    </div>
                  </div>
                </div>
                <div v-if="currentContextUsage.compression" class="context-stats-compression">
                  <div class="context-stats-compression-header">
                    <span class="context-stats-compression-title">最近压缩</span>
                    <span class="context-stats-compression-badge">-{{ contextStatsSummary.compressionSaved }}</span>
                  </div>
                  <el-progress
                    :percentage="Math.round((currentContextUsage.compression.ratio ?? 1) * 100)"
                    :stroke-width="5"
                    :show-text="false"
                    color="#8fb8ae"
                    class="context-stats-bar"
                  />
                  <div class="context-stats-compression-meta">
                    {{ contextStatsSummary.compressionBefore }} → {{ contextStatsSummary.compressionAfter }}
                    <span class="context-stats-compression-keep">保留 {{ contextStatsSummary.compressionRatio }}</span>
                  </div>
                </div>
              </div>
            </el-popover>
          <button
            class="thinking-toggle"
            :class="{ active: showThinkingAndTools }"
            :title="showThinkingAndTools ? '隐藏思考与工具调用' : '显示思考与工具调用'"
            :aria-label="showThinkingAndTools ? '隐藏思考与工具调用' : '显示思考与工具调用'"
            :aria-pressed="showThinkingAndTools"
            @click="showThinkingAndTools = !showThinkingAndTools"
          >
            <span class="thinking-toggle-track">
              <span class="thinking-toggle-thumb"></span>
            </span>
            <span class="thinking-toggle-label">思考过程</span>
          </button>
          <span :class="topbarStatusClass">{{ topbarStatusLabel }}</span>
        </div>
      </header>

      <div v-if="errorMessage" class="error-banner">
        <span>{{ errorMessage }}</span>
        <button
          v-if="workspaceErrorAction"
          class="ghost-button error-banner-action"
          type="button"
          data-workspace-error-conversation-link
          @click="handleWorkspaceErrorNavigation"
        >
          前往会话处理
        </button>
      </div>

      <div class="chat-main">
        <div v-if="showNoModelEmpty" class="chat-no-model-empty" data-no-model-empty>
          <h2>当前没有可用模型</h2>
          <p>请在个人设置中添加自定义模型，或联系管理员开启全局模型。</p>
          <RouterLink class="primary-button" to="/profile#profile-models" data-no-model-profile-link>打开个人设置</RouterLink>
        </div>
        <MessageList
          v-else
          :loading="messagesLoading || currentConversationBusy"
          :entries="visibleConversationEntries"
          :show-thinking-and-tools="showThinkingAndTools"
          :approval-decision-state-by-id="approvalDecisionStateById"
          :question-response-state-by-id="questionResponseStateById"
          @approval-decision="handleApprovalDecision"
          @interaction-respond="handleInteractionRespond"
        />
      </div>
      <div class="chat-composer-dock">
        <p v-if="!noUsableModels" class="composer-welcome">请尽情使唤 ~</p>
        <div v-if="!noUsableModels" class="workspace-mode-row" data-workspace-mode-toggle>
          <span class="workspace-mode-label">工作区</span>
          <div class="workspace-mode-switch auth-switch" role="radiogroup" aria-label="工作区模式">
            <button
              class="auth-switch-button"
              :class="{ active: selectedWorkspaceMode === 'mutable' }"
              type="button"
              role="radio"
              data-workspace-mode="mutable"
              :aria-checked="selectedWorkspaceMode === 'mutable'"
              @click="handleWorkspaceModeChange('mutable')"
            >
              可写
            </button>
            <button
              class="auth-switch-button"
              :class="{ active: selectedWorkspaceMode === 'readonly' }"
              type="button"
              role="radio"
              data-workspace-mode="readonly"
              :aria-checked="selectedWorkspaceMode === 'readonly'"
              @click="handleWorkspaceModeChange('readonly')"
            >
              只读
            </button>
          </div>
          <div
            v-if="currentPendingWorkspaceMergeTaskId"
            class="workspace-merge-inline"
            data-workspace-merge-inline
            aria-label="工作区变更待确认"
          >
            <span class="workspace-merge-inline-label">待合并</span>
            <button
              class="ghost-button workspace-merge-inline-button"
              type="button"
              data-workspace-merge-discard
              :disabled="workspaceMergeActionPending !== ''"
              @click="handleWorkspaceMergeAction('discard')"
            >
              放弃
            </button>
            <button
              class="primary-button workspace-merge-inline-button"
              type="button"
              data-workspace-merge-confirm
              :disabled="workspaceMergeActionPending !== ''"
              @click="handleWorkspaceMergeAction('confirm')"
            >
              确认
            </button>
          </div>
        </div>
        <MessageComposer
          ref="composerRef"
          :disabled="composerDisabled"
          :busy="currentConversationBusy"
          :stop-disabled="stoppingTask"
          :attachments-enabled="selectedModelSupportsAttachments"
          :attachments-uploading="attachmentsUploading"
          :attachments="currentDraftAttachments"
          :skills="availableSkills"
          :selected-skill-names="selectedSkillNames"
          @send="handleSend"
          @add-attachments="handleAddAttachments"
          @remove-attachment="handleRemoveAttachment"
          @stop="handleStopTask"
          @update:selected-skill-names="handleSkillSelectionChange"
        />
      </div>
    </section>
  </main>
</template>

<style scoped>
.workspace-mode-row {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.65rem;
  margin: 0 0 0.55rem;
  flex-wrap: wrap;
}

.error-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.error-banner span {
  min-width: 0;
}

.error-banner-action {
  flex: 0 0 auto;
  padding: 0.42rem 0.7rem;
  border-radius: 7px;
  font-size: 0.82rem;
  line-height: 1.1;
}

.workspace-mode-label {
  flex: 0 0 auto;
  font-size: 0.78rem;
  color: rgba(25, 50, 59, 0.58);
}

.workspace-mode-switch {
  width: auto;
  min-width: 11.5rem;
}

.workspace-mode-switch .auth-switch-button {
  padding: 0.38rem 0.72rem;
  font-size: 0.82rem;
  cursor: pointer;
}

.workspace-merge-inline {
  display: flex;
  align-items: center;
  gap: 0.38rem;
  min-height: 2.1rem;
  padding: 0.22rem 0.26rem 0.22rem 0.52rem;
  border: 1px solid rgba(143, 184, 174, 0.34);
  border-radius: 8px;
  background: rgba(241, 248, 246, 0.94);
  color: #28454e;
}

.workspace-merge-inline-label {
  white-space: nowrap;
  font-size: 0.78rem;
  color: rgba(25, 50, 59, 0.62);
}

.workspace-merge-inline-button {
  padding: 0.32rem 0.58rem;
  border-radius: 7px;
  font-size: 0.78rem;
  line-height: 1.1;
}

@media (max-width: 640px) {
  .workspace-mode-row {
    align-items: center;
    justify-content: flex-start;
  }

  .workspace-mode-switch {
    width: 100%;
  }

  .workspace-merge-inline {
    flex: 1 1 auto;
    justify-content: flex-end;
  }

  .error-banner {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
