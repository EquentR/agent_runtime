<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Close, Menu } from '@element-plus/icons-vue'

import ConversationSidebar from '../components/ConversationSidebar.vue'
import MessageComposer from '../components/MessageComposer.vue'
import MessageList from '../components/MessageList.vue'
import {
  createRunTask,
  deleteConversation,
  fetchConversationMessages,
  fetchConversations,
  fetchModelCatalog,
  fetchTaskDetails,
  findRunningTaskByConversation,
  streamRunTask,
  TASK_STREAM_ABORTED_MESSAGE,
} from '../lib/api'
import { formatConversationTitle, formatDocumentTitle } from '../lib/chat'
import { clearChatState, loadChatState, saveChatState } from '../lib/chat-state'
import { getSessionName, getSessionRole, logout } from '../lib/session'
import { attachReplyMetaToLatestReply, buildTranscriptEntries, updateTranscriptFromStreamEvent } from '../lib/transcript'
import type {
  Conversation,
  ModelCatalog,
  ModelCatalogEntry,
  ModelCatalogProvider,
  TaskDetails,
  TranscriptEntry,
  TranscriptTokenUsage,
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
const errorMessage = ref('')
const modelCatalog = ref<ModelCatalog | null>(null)
const catalogLoading = ref(false)
const selectedProviderId = ref('')
const selectedModelId = ref('')
const modelMenuOpen = ref(false)
const modelMenuRef = ref<HTMLElement | null>(null)
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
const modelMenuDisabled = computed(() => sending.value || catalogLoading.value || availableProviders.value.length === 0)
const composerDisabled = computed(() => sending.value || catalogLoading.value || !selectedProviderId.value || !selectedModelId.value)

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

function syncChatState() {
  saveChatState({
    activeConversationId: activeConversationId.value,
    activeTaskId: activeTaskId.value,
    activeTaskEventSeq: activeTaskEventSeq.value,
    entries: entries.value,
    draftEntriesByConversation: draftEntriesByConversation.value,
  })
}

watch([activeConversationId, activeTaskId, activeTaskEventSeq, entries, draftEntriesByConversation], syncChatState, { deep: true })

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

async function loadConversations(preferredConversationId = '') {
  sidebarLoading.value = true

  try {
    const loadedConversations = await fetchConversations()
    conversations.value = Array.isArray(loadedConversations) ? loadedConversations : []
    for (const conversation of conversations.value) {
      dropPendingConversation(conversation.id)
    }

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

function resolveTaskConversationId(task: TaskDetails | null | undefined) {
  return task?.result?.conversation_id ?? task?.result_json?.conversation_id ?? task?.input?.conversation_id ?? ''
}

function isTaskActive(task: TaskDetails | null | undefined) {
  return task?.status === 'queued' || task?.status === 'running' || task?.status === 'waiting' || task?.status === 'cancel_requested'
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

function resolveProvider(providerId: string) {
  return availableProviders.value.find((provider) => provider.id === providerId) ?? null
}

function resolveProviderDefaultModel(provider: ModelCatalogProvider | null, fallbackModelId = '') {
  if (!provider) {
    return ''
  }
  if (fallbackModelId && provider.models.some((model) => model.id === fallbackModelId)) {
    return fallbackModelId
  }
  return provider.models[0]?.id ?? ''
}

function applySelection(providerId: string, modelId: string) {
  const provider = resolveProvider(providerId) ?? availableProviders.value[0] ?? null
  selectedProviderId.value = provider?.id ?? ''
  selectedModelId.value = resolveProviderDefaultModel(provider, modelId)
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
  await loadCatalog()
  await loadConversations()
  if (!activeConversationId.value || !syncSelectionFromConversation(activeConversationId.value)) {
    applyDefaultSelection()
  }
  await resumeSavedTask()

  if (!activeConversationId.value && entries.value.length > 0) {
    return
  }
})

onBeforeUnmount(() => {
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

    <section class="chat-stage">
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
        <span :class="topbarStatusClass">{{ topbarStatusLabel }}</span>
      </header>

      <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>

      <div class="chat-main">
        <MessageList :loading="messagesLoading || sending" :entries="entries" />
      </div>
      <div class="chat-composer-dock">
        <MessageComposer :disabled="composerDisabled" @send="handleSend" />
      </div>
    </section>
  </main>
</template>
