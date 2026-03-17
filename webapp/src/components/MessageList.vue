<script setup lang="ts">
import { computed } from 'vue'

import { formatMessageContent } from '../lib/chat'
import type { TranscriptEntry } from '../types/api'

const props = defineProps<{
  loading: boolean
  entries: TranscriptEntry[]
}>()

const normalizedEntries = computed(() => props.entries)

function statusLabel(entry: TranscriptEntry) {
  if (entry.kind !== 'tool') {
    return ''
  }
  if (entry.status === 'running') return 'Running'
  if (entry.status === 'error') return 'Failed'
  return 'Done'
}
</script>

<template>
  <section class="messages-panel">
    <div class="messages-header">
      <p class="eyebrow">Session</p>
      <span class="status-pill" :class="{ idle: !loading }">{{ loading ? 'Syncing' : 'Ready' }}</span>
    </div>

    <div class="messages-body">
      <div v-if="normalizedEntries.length === 0" class="messages-empty">
        <p>Start a conversation and the backend history will show up here.</p>
      </div>

      <div v-else class="messages-stack wide-stack">
        <article v-for="entry in normalizedEntries" :key="entry.id" class="trace-block" :class="entry.kind">
          <div class="trace-header">
            <span class="message-role">{{ entry.title }}</span>
            <span v-if="statusLabel(entry)" class="trace-status">{{ statusLabel(entry) }}</span>
          </div>
          <p v-if="entry.content" class="trace-content">{{ formatMessageContent(entry.content) }}</p>
          <p v-if="entry.summary" class="trace-summary">{{ entry.summary }}</p>
        </article>
      </div>
    </div>
  </section>
</template>
