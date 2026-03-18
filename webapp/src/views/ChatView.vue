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
  streamRunTask,
} from '../lib/api'
import { clearChatState, loadChatState, saveChatState } from '../lib/chat-state'
import { DEFAULT_MODEL_ID, DEFAULT_PROVIDER_ID } from '../lib/chat'
import { clearSession, getSessionName } from '../lib/session'
import { buildTranscriptEntries, updateTranscriptFromStreamEvent } from '../lib/transcript'
import type { Conversation, TaskStreamEvent, TranscriptEntry } from '../types/api'

const router = useRouter()

const messagesLoading = ref(false)
const sending = ref(false)
const sidebarLoading = ref(false)
const sidebarCollapsed = ref(false)
const sidebarMobile = ref(false)
const sidebarDrawerOpen = ref(false)
const username = ref(getSessionName())
const activeConversationId = ref('')
const conversations = ref<Conversation[]>([])
const entries = ref<TranscriptEntry[]>([])
const errorMessage = ref('')

const chatShellClass = computed(() => ({
  'sidebar-collapsed': !sidebarMobile.value && sidebarCollapsed.value,
  'sidebar-mobile': sidebarMobile.value,
  'sidebar-open': sidebarMobile.value && sidebarDrawerOpen.value,
}))

const topbarStatusLabel = computed(() => (messagesLoading.value || sending.value ? 'Syncing' : 'Ready'))
const topbarStatusClass = computed(() => ({
  'status-pill': true,
  idle: !messagesLoading.value && !sending.value,
  loading: messagesLoading.value || sending.value,
}))

function activeConversationTitle() {
  const current = conversations.value.find((conversation) => conversation.id === activeConversationId.value)
  if (current?.title?.trim()) {
    return current.title.trim()
  }
  if (activeConversationId.value) {
    return 'Untitled conversation'
  }
  return '新对话'
}

function syncChatState() {
  saveChatState({
    activeConversationId: activeConversationId.value,
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
        return
      }
    }

    if (activeConversationId.value) {
      const exists = conversations.value.some((conversation) => conversation.id === activeConversationId.value)
      if (exists) {
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
  sidebarDrawerOpen.value = false
  messagesLoading.value = true
  errorMessage.value = ''

  try {
    const messages = await fetchConversationMessages(conversationId)
    entries.value = buildTranscriptEntries(messages)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : 'Failed to load messages'
  } finally {
    messagesLoading.value = false
  }
}

function startNewConversation() {
  activeConversationId.value = ''
  entries.value = []
  errorMessage.value = ''
  sidebarDrawerOpen.value = false
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
    errorMessage.value = error instanceof Error ? error.message : 'Failed to delete conversation'
  }
}

function applyStreamEvent(event: TaskStreamEvent) {
  entries.value = updateTranscriptFromStreamEvent(entries.value, event)
}

async function handleSend(message: string) {
  sending.value = true
  errorMessage.value = ''

  entries.value = [...entries.value, { id: `user-${Date.now()}`, kind: 'user', title: 'You', content: message }]

  try {
    const task = await createRunTask({
      createdBy: username.value,
      conversationId: activeConversationId.value || undefined,
      providerId: DEFAULT_PROVIDER_ID,
      modelId: DEFAULT_MODEL_ID,
      message,
    })
    const result = await streamRunTask(task.id, () => {
      void 0
    }, (event) => {
      applyStreamEvent(event)
    })

    activeConversationId.value = result.conversation_id
    const messages = await fetchConversationMessages(result.conversation_id)
    entries.value = buildTranscriptEntries(messages)
    await loadConversations(result.conversation_id)
  } catch (error) {
    const taskError = error instanceof Error ? error.message : 'Failed to send message'
    errorMessage.value = taskError
    entries.value = updateTranscriptFromStreamEvent(entries.value, {
      type: 'task.failed',
      payload: { error: taskError },
    })
  } finally {
    sending.value = false
  }
}

async function handleLogout() {
  clearSession()
  clearChatState()
  await router.push('/login')
}

onMounted(async () => {
  syncSidebarViewport()
  window.addEventListener('resize', syncSidebarViewport)

  const saved = loadChatState()
  activeConversationId.value = saved.activeConversationId
  entries.value = saved.entries
  await loadConversations()

  if (!activeConversationId.value && entries.value.length > 0) {
    return
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', syncSidebarViewport)
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
          v-if="sidebarMobile"
          class="ghost-button icon-button topbar-sidebar-toggle"
          type="button"
          aria-label="Open conversations"
          @click="toggleSidebarCollapsed"
        >
          <component :is="sidebarDrawerOpen ? Close : Menu" />
        </button>
        <div class="topbar-title-block">
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
        <MessageComposer :disabled="sending" @send="handleSend" />
      </div>
    </section>
  </main>
</template>
