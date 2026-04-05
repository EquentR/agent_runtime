<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Close, Menu } from '@element-plus/icons-vue'

import ConversationSidebar from '../components/ConversationSidebar.vue'
import MessageComposer from '../components/MessageComposer.vue'
import MessageList from '../components/MessageList.vue'
import {
  cancelTask,
  createRunTask,
  decideTaskApproval,
  fetchTaskInteractions,
  deleteConversation,
  fetchConversationMessages,
  fetchConversations,
  fetchModelCatalog,
  fetchSkills,
  fetchTaskApprovals,
  fetchTaskDetails,
  findRunningTaskByConversation,
	respondTaskInteraction,
  streamRunTask,
  TASK_STREAM_ABORTED_MESSAGE,
} from '../lib/api'
import { clearChatState, loadChatState, saveChatState, scheduleChatStateSave } from '../lib/chat-state'
import { formatConversationTitle, formatDocumentTitle } from '../lib/chat'
import { resolveModelSelection } from '../lib/model-selection'
import { getSessionName, getSessionRole, logout } from '../lib/session'
import {
  buildApprovalEntriesFromList,
  isTaskActive,
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
  Conversation,
  ModelCatalog,
  ModelCatalogEntry,
	QuestionInteractionSubmitInput,
  TaskDetails,
  TranscriptEntry,
  TranscriptTokenUsage,
  WorkspaceSkillListItem,
} from '../types/api'

const router = useRouter()

const messagesLoading = ref(false)
const sending = ref(false)
const sidebarLoading = ref(false)
const sidebarCollapsed = ref(false)
const sidebarMobile = ref(false)
const sidebarDrawerOpen = ref(false)
const username = ref(getSessionName())
const activeConversationId = ref('')
const activeTaskId = ref('')
const activeTaskEventSeq = ref(0)
const conversations = ref<Conversation[]>([])
const entries = ref<TranscriptEntry[]>([])
const draftEntriesByConversation = ref<Record<string, TranscriptEntry[]>>({})
const pendingConversationById = ref<Record<string, Conversation>>({})
const approvalDecisionStateById = ref<Record<string, { pending: boolean; decision: 'approve' | 'reject' }>>({})
const questionResponseStateById = ref<Record<string, { pending: boolean }>>({})
const errorMessage = ref('')
const modelCatalog = ref<ModelCatalog | null>(null)
const catalogLoading = ref(false)
const availableSkills = ref<WorkspaceSkillListItem[]>([])
const selectedSkillsByConversation = ref<Record<string, string[]>>({})
const selectedProviderId = ref('')
const selectedModelId = ref('')
const modelMenuOpen = ref(false)
const modelMenuRef = ref<HTMLElement | null>(null)
const composerRef = ref<InstanceType<typeof MessageComposer> | null>(null)
const showThinkingAndTools = ref(true)
let activeStreamAbortController: AbortController | null = null
let activeStreamingTaskId = ''
const isAdmin = computed(() => getSessionRole() === 'admin')
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

const topbarStatusLabel = computed(() => (messagesLoading.value || sending.value ? '同步中' : '就绪'))
const topbarStatusClass = computed(() => ({
  'status-pill': true,
  idle: !messagesLoading.value && !sending.value,
  loading: messagesLoading.value || sending.value,
}))
const sidebarDesktopHidden = computed(() => !sidebarMobile.value && sidebarCollapsed.value)
const availableProviders = computed(() => modelCatalog.value?.providers ?? [])
const selectedProvider = computed(
  () => availableProviders.value.find((provider) => provider.id === selectedProviderId.value) ?? availableProviders.value[0] ?? null,
)
const availableModels = computed(() => selectedProvider.value?.models ?? [])
const selectedModel = computed<ModelCatalogEntry | null>(
  () => availableModels.value.find((item) => item.id === selectedModelId.value) ?? availableModels.value[0] ?? null,
)
const selectedModelLabel = computed(() => selectedModel.value?.name || selectedModelId.value || '选择模型')
const selectedSkillNames = computed(() => selectedSkillsByConversation.value[activeConversationId.value] ?? [])
const modelMenuDisabled = computed(() => sending.value || catalogLoading.value || availableProviders.value.length === 0)
const composerDisabled = computed(() => catalogLoading.value || !selectedProviderId.value || !selectedModelId.value)
const stoppingTask = ref(false)

function activeConversationTitle() {
  const current = conversations.value.find((conversation) => conversation.id === activeConversationId.value)
  if (activeConversationId.value) {
    return formatConversationTitle(current?.title ?? '', '未命名对话')
  }
  return '新对话'
}

function syncDocumentTitle() {
  document.title = formatDocumentTitle(activeConversationTitle())
}

watch([activeConversationId, conversations], syncDocumentTitle, { deep: true, immediate: true })

function currentChatState() {
  return {
    activeConversationId: activeConversationId.value,
    activeTaskId: activeTaskId.value,
    activeTaskEventSeq: activeTaskEventSeq.value,
    entries: entries.value,
    draftEntriesByConversation: draftEntriesByConversation.value,
    selectedSkillsByConversation: selectedSkillsByConversation.value,
  }
}

function syncChatState() {
  scheduleChatStateSave(currentChatState())
}

function flushChatState() {
  saveChatState(currentChatState())
}

watch([activeConversationId, activeTaskId, activeTaskEventSeq, entries, draftEntriesByConversation, selectedSkillsByConversation], syncChatState, { deep: true })

function setDraftEntries(conversationId: string, nextEntries: TranscriptEntry[]) {
  if (!conversationId) {
    return
  }
  draftEntriesByConversation.value = {
    ...draftEntriesByConversation.value,
    [conversationId]: nextEntries,
  }
}

function dropDraftEntries(conversationId: string) {
  if (!conversationId || !(conversationId in draftEntriesByConversation.value)) {
    return
  }
  const nextDrafts = { ...draftEntriesByConversation.value }
  delete nextDrafts[conversationId]
  draftEntriesByConversation.value = nextDrafts
}

function applyEntriesForConversation(conversationId: string, nextEntries: TranscriptEntry[]) {
  if (conversationId) {
    setDraftEntries(conversationId, nextEntries)
  }
  if (conversationId && activeConversationId.value && conversationId !== activeConversationId.value) {
    return
  }
  entries.value = nextEntries
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

async function loadConversations(preferredConversationId = '') {
  sidebarLoading.value = true

  try {
    const loadedConversations = await fetchConversations()
    conversations.value = Array.isArray(loadedConversations) ? loadedConversations : []
    prunePendingConversations(conversations.value)

    if (preferredConversationId) {
      activeConversationId.value = preferredConversationId
      const exists = conversations.value.some((conversation) => conversation.id === preferredConversationId)
      if (exists) {
        syncSelectionFromConversation(preferredConversationId)
        return
      }
    }

    if (activeConversationId.value) {
      const exists = conversations.value.some((conversation) => conversation.id === activeConversationId.value)
      if (exists) {
        if (entries.value.length > 0) {
          syncSelectionFromConversation(activeConversationId.value)
          return
        }
        await selectConversation(activeConversationId.value)
        return
      }
    }
  } finally {
    sidebarLoading.value = false
  }
}

async function selectConversation(conversationId: string) {
  activeConversationId.value = conversationId
  syncSelectionFromConversation(conversationId)
  sidebarDrawerOpen.value = false
  messagesLoading.value = true
  errorMessage.value = ''

  const draftEntries = draftEntriesByConversation.value[conversationId]
  if (draftEntries) {
    entries.value = draftEntries
  }

  try {
    if (!draftEntries) {
      const messages = await fetchConversationMessages(conversationId)
      entries.value = buildTranscriptEntries(messages)
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载消息失败'
  } finally {
    messagesLoading.value = false
  }

  await resumeStreamForConversation(conversationId)
}

function startNewConversation() {
  activeConversationId.value = ''
  entries.value = []
  errorMessage.value = ''
  sidebarDrawerOpen.value = false
  modelMenuOpen.value = false
  applyDefaultSelection()
  void nextTick(() => {
    composerRef.value?.focus()
  })
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

async function handleDeleteConversation(conversationId: string) {
  errorMessage.value = ''

  try {
    await deleteConversation(conversationId)
    if (activeConversationId.value === conversationId) {
      startNewConversation()
    }
    await loadConversations()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '删除对话失败'
  }
}

async function hydratePendingApprovals(task: TaskDetails | null | undefined, conversationId = '') {
  if (!task || !isTaskWaitingForInput(task)) {
    return
  }

  const taskConversationId = resolveTaskConversationId(task) || conversationId || activeConversationId.value
  if (!taskConversationId) {
    return
  }

  let nextEntries = draftEntriesByConversation.value[taskConversationId] ?? (taskConversationId === activeConversationId.value ? entries.value : [])
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

function stopActiveStream() {
  activeStreamAbortController?.abort()
  activeStreamAbortController = null
  activeStreamingTaskId = ''
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

function clearActiveTask() {
  activeTaskId.value = ''
  activeStreamingTaskId = ''
  activeTaskEventSeq.value = 0
}

async function completeTaskConversation(conversationId: string, usage?: TranscriptTokenUsage, providerId = '', modelId = '') {
  activeConversationId.value = conversationId
  const nextEntries = attachReplyMetaToLatestReply(draftEntriesByConversation.value[conversationId] ?? entries.value, {
    provider_id: providerId || selectedProviderId.value,
    model_id: modelId || selectedModelId.value,
    token_usage: usage,
  })
  dropDraftEntries(conversationId)
  dropPendingConversation(conversationId)
  entries.value = nextEntries
  await loadConversations(conversationId)
  syncSelectionFromConversation(conversationId)
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

function chooseModel(providerId: string, modelId: string) {
  applySelection(providerId, modelId)
  closeModelMenu()
}

async function handleApprovalDecision(input: {
  taskId: string
  approvalId: string
  decision: 'approve' | 'reject'
  reason: string
}) {
  const taskId = input.taskId || activeTaskId.value
  if (!taskId || !input.approvalId) {
    return
  }

   if (approvalDecisionStateById.value[input.approvalId]?.pending) {
    return
  }

  try {
    errorMessage.value = ''
    approvalDecisionStateById.value = {
      ...approvalDecisionStateById.value,
      [input.approvalId]: { pending: true, decision: input.decision },
    }
    const approval = await decideTaskApproval(taskId, input.approvalId, {
      decision: input.decision,
      reason: input.reason,
    })
    const currentEntries = activeConversationId.value
      ? draftEntriesByConversation.value[activeConversationId.value] ?? entries.value
      : entries.value
    const nextEntries = updateTranscriptFromStreamEvent(
      currentEntries,
      buildApprovalStreamEvent(approval, { type: 'approval.resolved', decision: input.decision }),
    )
    applyEntriesForConversation(activeConversationId.value, nextEntries)
    void attachTaskStream(taskId, approval.conversation_id || activeConversationId.value)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '审批提交失败'
  } finally {
    const nextState = { ...approvalDecisionStateById.value }
    delete nextState[input.approvalId]
    approvalDecisionStateById.value = nextState
  }
}

async function handleInteractionRespond(input: QuestionInteractionSubmitInput) {
  const taskId = input.taskId || activeTaskId.value
  if (!taskId || !input.interactionId) {
    return
  }

  if (questionResponseStateById.value[input.interactionId]?.pending) {
    return
  }

  try {
    errorMessage.value = ''
    questionResponseStateById.value = {
      ...questionResponseStateById.value,
      [input.interactionId]: { pending: true },
    }
    const interaction = await respondTaskInteraction(taskId, input.interactionId, {
      selected_option_id: input.selectedOptionId,
      selected_option_ids: input.selectedOptionIds,
      custom_text: input.customText,
    })
    const currentEntries = activeConversationId.value
      ? draftEntriesByConversation.value[activeConversationId.value] ?? entries.value
      : entries.value
    const nextEntries = updateTranscriptFromStreamEvent(currentEntries, {
      type: 'interaction.responded',
      payload: interaction as unknown as Record<string, unknown>,
    })
    applyEntriesForConversation(activeConversationId.value, nextEntries)
    void attachTaskStream(taskId, interaction.conversation_id || activeConversationId.value)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '提交回答失败'
  } finally {
    const nextState = { ...questionResponseStateById.value }
    delete nextState[input.interactionId]
    questionResponseStateById.value = nextState
  }
}

async function handleStopTask() {
  const taskId = activeTaskId.value
  if (!taskId || stoppingTask.value) {
    return
  }

  try {
    stoppingTask.value = true
    errorMessage.value = ''
    let task = await cancelTask(taskId)
    if (task.status === 'cancel_requested') {
      task = await cancelTask(taskId)
    }
    if (!isTaskActive(task)) {
      stopActiveStream()
      const conversationId = resolveTaskConversationId(task) || activeConversationId.value
      const currentEntries = conversationId
        ? draftEntriesByConversation.value[conversationId] ?? (conversationId === activeConversationId.value ? entries.value : [])
        : entries.value
      const nextEntries = updateTranscriptFromStreamEvent(currentEntries, {
        type: 'task.finished',
        payload: { status: task.status, error: task.error },
      })
      applyEntriesForConversation(conversationId, nextEntries)
      clearActiveTask()
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '停止任务失败'
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

function syncSelectionFromConversation(conversationId: string) {
  const conversation = conversations.value.find((item) => item.id === conversationId)
  if (!conversation) {
    return false
  }
  applySelection(conversation.provider_id, conversation.model_id)
  return true
}

async function loadCatalog() {
  catalogLoading.value = true
  try {
    modelCatalog.value = await fetchModelCatalog()
    if (!selectedProviderId.value || !selectedModelId.value) {
      applyDefaultSelection()
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载模型目录失败'
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

function handleGlobalPointerDown(event: PointerEvent) {
  if (!modelMenuOpen.value) {
    return
  }
  const target = event.target
  if (target instanceof Node && modelMenuRef.value?.contains(target)) {
    return
  }
  closeModelMenu()
}

function handleGlobalKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    closeModelMenu()
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
  activeTaskId.value = taskId
  sending.value = true

  let streamConversationId = conversationId || activeConversationId.value
  let firstVisibleChunkSeen = false

  try {
    const result = await streamRunTask(
      taskId,
      () => {
        void 0
      },
      (event) => {
        activeTaskEventSeq.value = Math.max(activeTaskEventSeq.value, event.seq ?? 0)
        const currentEntries = streamConversationId
          ? draftEntriesByConversation.value[streamConversationId] ?? (streamConversationId === activeConversationId.value ? entries.value : [])
          : entries.value
        const nextEntries = updateTranscriptFromStreamEvent(currentEntries, event)
        applyEntriesForConversation(streamConversationId, nextEntries)

        const isVisibleTextChunk =
          !firstVisibleChunkSeen &&
          !!streamConversationId &&
          event.type === 'log.message' &&
          typeof event.payload?.Kind === 'string' &&
          event.payload.Kind === 'text_delta' &&
          typeof event.payload?.Text === 'string' &&
          event.payload.Text.length > 0
        if (isVisibleTextChunk) {
          firstVisibleChunkSeen = true
          void loadConversations(streamConversationId)
        }
      },
      { signal: abortController.signal, afterSeq: activeTaskEventSeq.value },
    )

    streamConversationId = result.conversation_id || streamConversationId
    clearActiveTask()
    await completeTaskConversation(result.conversation_id, result.usage, result.provider_id, result.model_id)
  } catch (error) {
    if (error instanceof Error && error.message === TASK_STREAM_ABORTED_MESSAGE) {
      return
    }

    const taskError = error instanceof Error ? error.message : '发送消息失败'
    try {
      const task = await fetchTaskDetails(taskId)
      if (!isTaskActive(task)) {
        if (task.status === 'cancelled') {
          clearActiveTask()
          return
        }
        errorMessage.value = taskError
        const currentEntries = streamConversationId ? draftEntriesByConversation.value[streamConversationId] ?? [] : entries.value
        const nextEntries = updateTranscriptFromStreamEvent(currentEntries, {
          type: 'task.failed',
          payload: { error: taskError },
        })
        applyEntriesForConversation(streamConversationId, nextEntries)
        clearActiveTask()
      }
    } catch {
      if (taskError !== 'Task event stream disconnected') {
        errorMessage.value = taskError
        const currentEntries = streamConversationId ? draftEntriesByConversation.value[streamConversationId] ?? [] : entries.value
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
    sending.value = false
  }
}

async function resumeTask(task: TaskDetails | null | undefined, conversationId = '') {
  if (!task || !isTaskActive(task)) {
    if (task && activeTaskId.value === task.id) {
      clearActiveTask()
    }
    return
  }

  const taskConversationId = resolveTaskConversationId(task)
  if (conversationId && taskConversationId && taskConversationId !== conversationId) {
    return
  }

  await hydratePendingApprovals(task, taskConversationId || conversationId)
  await attachTaskStream(task.id, taskConversationId || conversationId)
}

async function resumeSavedTask() {
  if (!activeTaskId.value) {
    return
  }

  try {
    const task = await fetchTaskDetails(activeTaskId.value)
    await resumeTask(task, activeConversationId.value)
  } catch {
    clearActiveTask()
  }
}

async function resumeStreamForConversation(conversationId: string) {
  if (!conversationId) {
    return
  }

  if (activeTaskId.value) {
    try {
      const task = await fetchTaskDetails(activeTaskId.value)
      const taskConversationId = resolveTaskConversationId(task)
      if (!taskConversationId || taskConversationId === conversationId) {
        await resumeTask(task, conversationId)
        if (activeTaskId.value) {
          return
        }
      }
    } catch {
      clearActiveTask()
    }
  }

  const task = await findRunningTaskByConversation(conversationId)
  await resumeTask(task, conversationId)
}

async function handleSend(message: string) {
  sending.value = true
  errorMessage.value = ''
  const previousConversationId = activeConversationId.value

  entries.value = [...entries.value, { id: `user-${Date.now()}`, kind: 'user', title: 'You', content: message }]
  if (previousConversationId) {
    setDraftEntries(previousConversationId, entries.value)
  }

  try {
    const task = await createRunTask({
      createdBy: username.value,
      conversationId: previousConversationId || undefined,
      providerId: selectedProviderId.value,
      modelId: selectedModelId.value,
      message,
      ...(selectedSkillNames.value.length > 0 ? { skills: selectedSkillNames.value } : {}),
    })
    const createdConversationId = task.input?.conversation_id ?? ''
    if (createdConversationId && !previousConversationId) {
      setDraftEntries(createdConversationId, entries.value)
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
      activeConversationId.value = createdConversationId
      await loadConversations(createdConversationId)
    }
    activeTaskId.value = task.id
    await attachTaskStream(task.id, createdConversationId || previousConversationId)
  } catch (error) {
    if (!(error instanceof Error) || error.message !== TASK_STREAM_ABORTED_MESSAGE) {
      const taskError = error instanceof Error ? error.message : '发送消息失败'
      errorMessage.value = taskError
      entries.value = updateTranscriptFromStreamEvent(entries.value, {
        type: 'task.failed',
        payload: { error: taskError },
      })
    }
  } finally {
    if (!activeTaskId.value) {
      sending.value = false
    }
  }
}

async function handleLogout() {
  await logout()
  clearChatState()
  await router.push('/login')
}

onMounted(async () => {
  syncSidebarViewport()
  window.addEventListener('resize', syncSidebarViewport)
  window.addEventListener('pointerdown', handleGlobalPointerDown)
  window.addEventListener('keydown', handleGlobalKeydown)

  const saved = loadChatState()
  activeConversationId.value = saved.activeConversationId
  activeTaskId.value = saved.activeTaskId
  activeTaskEventSeq.value = saved.activeTaskEventSeq
  entries.value = saved.entries
  draftEntriesByConversation.value = saved.draftEntriesByConversation
  selectedSkillsByConversation.value = saved.selectedSkillsByConversation

  await Promise.all([loadCatalog(), loadAvailableSkills(), loadConversations()])
  if (!activeConversationId.value || !syncSelectionFromConversation(activeConversationId.value)) {
    applyDefaultSelection()
  }
  await resumeSavedTask()

  if (!activeConversationId.value && entries.value.length > 0) {
    return
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
      @select="selectConversation"
      @toggle-collapse="toggleSidebarCollapsed"
    />

    <section class="chat-stage" :class="{ 'composer-centered': entries.length === 0 }">
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

      <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>

      <div class="chat-main">
        <MessageList
          :loading="messagesLoading || sending"
          :entries="entries"
          :show-thinking-and-tools="showThinkingAndTools"
          :approval-decision-state-by-id="approvalDecisionStateById"
          :question-response-state-by-id="questionResponseStateById"
          @approval-decision="handleApprovalDecision"
          @interaction-respond="handleInteractionRespond"
        />
      </div>
      <div class="chat-composer-dock">
        <p class="composer-welcome">请尽情使唤 ~</p>
        <MessageComposer
          ref="composerRef"
          :disabled="composerDisabled"
          :busy="sending"
          :stop-disabled="stoppingTask"
          :skills="availableSkills"
          :selected-skill-names="selectedSkillNames"
          @send="handleSend"
          @stop="handleStopTask"
          @update:selected-skill-names="handleSkillSelectionChange"
        />
      </div>
    </section>
  </main>
</template>
