<script setup lang="ts">
import { computed } from 'vue'

import { formatConversationTitle } from '../lib/chat'
import type { Conversation } from '../types/api'

const props = defineProps<{
  activeConversationId: string
  conversations: Conversation[]
  loading: boolean
}>()

const emit = defineEmits<{
  select: [conversationId: string]
  create: []
}>()

const items = computed(() => props.conversations)
</script>

<template>
  <aside class="sidebar-panel">
    <div class="sidebar-header">
      <div>
        <p class="eyebrow">Workspace</p>
        <h2>Conversations</h2>
      </div>
      <button class="ghost-button icon-button" type="button" aria-label="New chat" @click="emit('create')">
        +
      </button>
    </div>

    <div class="sidebar-list">
      <p v-if="loading" class="sidebar-empty">Loading conversations...</p>

      <div v-else-if="items.length === 0" class="sidebar-empty">
        No conversations yet. Send the first message to create one.
      </div>

      <button
        v-for="conversation in items"
        :key="conversation.id"
        class="conversation-card"
        :class="{ active: conversation.id === activeConversationId }"
        type="button"
        @click="emit('select', conversation.id)"
      >
        <span
          class="conversation-title truncate-text"
          :title="formatConversationTitle(conversation.title, 'Untitled conversation')"
        >
          {{ formatConversationTitle(conversation.title, 'Untitled conversation') }}
        </span>
      </button>
    </div>
  </aside>
</template>
