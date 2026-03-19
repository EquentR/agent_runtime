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
import { clearChatState, loadChatState, saveChatState } from '../lib/chat-state'
import { getSessionName, logout } from '../lib/session'
import { attachReplyMetaToLatestReply, buildTranscriptEntries, updateTranscriptFromStreamEvent } from '../lib/transcript'
import type {
  Conversation,
  ModelCatalog,
  ModelCatalogEntry,
  ModelCatalogProvider,
  TaskDetails,
  TaskStreamEvent,
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
const conversations = ref<Conversation[]>([])
const entries = ref<TranscriptEntry[]>([])
const errorMessage = ref('')
const modelCatalog = ref<ModelCatalog | null>(null)
const catalogLoading = ref(false)
const selectedProviderId = ref('')
const selectedModelId = ref('')
const modelMenuOpen = ref(false)
const modelMenuRef = ref<HTMLElement | null>(null)
let activeStreamAbortController: AbortController | null = null
let activeStreamingTaskId = ''

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
  if (current?.title?.trim()) {
    return current.title.trim()
  }
  if (activeConversationId.value) {
      return '未命名对话'
  }
  return '新对话'
}

function syncChatState() {
  saveChatState({
    activeConversationId: activeConversationId.value,
    activeTaskId: activeTaskId.value,
    entries: entries.value,
  })
}

watch([activeConversationId, entries], syncChatState, { deep: true })

async function loadConversations(preferredConversationId = '') {
  sidebarLoading.value = true

  try {
    conversations.value = await fetchConversations()

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

  try {
    const messages = await fetchConversationMessages(conversationId)
    entries.value = buildTranscriptEntries(messages)
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

function applyStreamEvent(event: TaskStreamEvent) {
  entries.value = updateTranscriptFromStreamEvent(entries.value, event)
}

function resolveTaskConversationId(task: TaskDetails | null | undefined) {
  return task?.result?.conversation_id ?? task?.result_json?.conversation_id ?? task?.input?.conversation_id ?? ''
}

function isTaskActive(task: TaskDetails | null | undefined) {
  return task?.status === 'queued' || task?.status === 'running' || task?.status === 'cancel_requested'
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
}

async function completeTaskConversation(conversationId: string, usage?: TranscriptTokenUsage, providerId = '', modelId = '') {
  activeConversationId.value = conversationId
  entries.value = attachReplyMetaToLatestReply(entries.value, {
    provider_id: providerId || selectedProviderId.value,
    model_id: modelId || selectedModelId.value,
    token_usage: usage,
  })
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

async function attachTaskStream(taskId: string) {
  if (!taskId || activeStreamingTaskId === taskId) {
    return
  }

  stopActiveStream()
  const abortController = new AbortController()
  activeStreamAbortController = abortController
  activeStreamingTaskId = taskId
  activeTaskId.value = taskId
  sending.value = true

  try {
    const result = await streamRunTask(
      taskId,
      () => {
        void 0
      },
      (event) => {
        applyStreamEvent(event)
      },
      { signal: abortController.signal },
    )

    clearActiveTask()
    await completeTaskConversation(result.conversation_id, result.usage, result.provider_id, result.model_id)
  } catch (error) {
    if (error instanceof Error && error.message === TASK_STREAM_ABORTED_MESSAGE) {
      return
    }

    const taskError = error instanceof Error ? error.message : '发送消息失败'
    errorMessage.value = taskError
    entries.value = updateTranscriptFromStreamEvent(entries.value, {
      type: 'task.failed',
      payload: { error: taskError },
    })

    try {
      const task = await fetchTaskDetails(taskId)
      if (!isTaskActive(task)) {
        clearActiveTask()
      }
    } catch {
      void 0
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

  await attachTaskStream(task.id)
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

  entries.value = [...entries.value, { id: `user-${Date.now()}`, kind: 'user', title: 'You', content: message }]

  try {
    const task = await createRunTask({
      createdBy: username.value,
      conversationId: activeConversationId.value || undefined,
      providerId: selectedProviderId.value,
      modelId: selectedModelId.value,
      message,
    })
    activeTaskId.value = task.id
    await attachTaskStream(task.id)
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
  entries.value = saved.entries
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
      :conversations="conversations"
      :desktop-hidden="sidebarDesktopHidden"
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
