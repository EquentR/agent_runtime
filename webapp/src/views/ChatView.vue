<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import ConversationSidebar from '../components/ConversationSidebar.vue'
import MessageComposer from '../components/MessageComposer.vue'
import MessageList from '../components/MessageList.vue'
import {
  createRunTask,
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
const username = ref(getSessionName())
const activeConversationId = ref('')
const conversations = ref<Conversation[]>([])
const entries = ref<TranscriptEntry[]>([])
const errorMessage = ref('')

function activeConversationTitle() {
  const current = conversations.value.find((conversation) => conversation.id === activeConversationId.value)
  if (current?.title?.trim()) {
    return current.title.trim()
  }
  if (activeConversationId.value) {
    return 'Untitled conversation'
  }
  return 'New conversation'
}

function syncChatState() {
  saveChatState({
    activeConversationId: activeConversationId.value,
    entries: entries.value,
  })
}

watch([activeConversationId, entries], syncChatState, { deep: true })

async function loadConversations(selectFirst = false) {
  sidebarLoading.value = true

  try {
    conversations.value = await fetchConversations()

    if (selectFirst) {
      if (activeConversationId.value) {
        const exists = conversations.value.some((conversation) => conversation.id === activeConversationId.value)
        if (exists) {
          await selectConversation(activeConversationId.value)
          return
        }
      }

      if (!activeConversationId.value && entries.value.length > 0) {
        return
      }

      if (!activeConversationId.value && conversations.value.length > 0) {
        await selectConversation(conversations.value[0].id)
      }
    }
  } finally {
    sidebarLoading.value = false
  }
}

async function selectConversation(conversationId: string) {
  activeConversationId.value = conversationId
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
    await loadConversations()
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
  const saved = loadChatState()
  activeConversationId.value = saved.activeConversationId
  entries.value = saved.entries
  await loadConversations(true)

  if (!activeConversationId.value && entries.value.length > 0) {
    return
  }
})
</script>

<template>
  <main class="chat-shell">
    <ConversationSidebar
      :active-conversation-id="activeConversationId"
      :conversations="conversations"
      :loading="sidebarLoading"
      @create="startNewConversation"
      @select="selectConversation"
    />

    <section class="chat-stage">
      <header class="topbar">
        <div class="topbar-title-block">
          <p class="eyebrow">Conversation</p>
          <strong class="topbar-conversation-title" :title="activeConversationTitle()">
            {{ activeConversationTitle() }}
          </strong>
        </div>
        <div class="topbar-actions">
          <span class="topbar-user">Signed in as {{ username }}</span>
          <button class="ghost-button" type="button" @click="handleLogout">Log out</button>
        </div>
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
