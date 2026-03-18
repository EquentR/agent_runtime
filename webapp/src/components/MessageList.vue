<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'

import { formatMessageContent } from '../lib/chat'
import type { TranscriptEntry, TranscriptEntryDetail } from '../types/api'

const props = defineProps<{
  loading: boolean
  entries: TranscriptEntry[]
}>()

const normalizedEntries = computed(() => props.entries)
const messagesBody = ref<HTMLDivElement | null>(null)
const bottomOffsetThreshold = 24
const stickToBottom = ref(true)

function maxScrollTop(element: HTMLDivElement) {
  return Math.max(element.scrollHeight - element.clientHeight, 0)
}

function isNearBottom(element: HTMLDivElement) {
  return maxScrollTop(element) - element.scrollTop <= bottomOffsetThreshold
}

async function scrollToBottom(force = false) {
  await nextTick()

  if (!messagesBody.value) {
    return
  }

  if (!force && !stickToBottom.value) {
    return
  }

  messagesBody.value.scrollTop = maxScrollTop(messagesBody.value)
  stickToBottom.value = true
}

async function smoothScrollToBottom() {
  await nextTick()

  if (!messagesBody.value) {
    return
  }

  const targetTop = maxScrollTop(messagesBody.value)

  if (import.meta.env.MODE !== 'test' && typeof messagesBody.value.scrollTo === 'function') {
    messagesBody.value.scrollTo({ top: targetTop, behavior: 'smooth' })
  } else {
    messagesBody.value.scrollTop = targetTop
  }

  stickToBottom.value = true
}

function shouldForceScroll(nextEntries: TranscriptEntry[], previousEntries: TranscriptEntry[]) {
  if (nextEntries.length === 0 || previousEntries.length === 0) {
    return true
  }

  return nextEntries[0]?.id !== previousEntries[0]?.id
}

function handleScroll() {
  if (!messagesBody.value) {
    return
  }

  stickToBottom.value = isNearBottom(messagesBody.value)
}

function jumpToBottom() {
  stickToBottom.value = true
  void smoothScrollToBottom()
}

onMounted(() => {
  void scrollToBottom(true)
})

watch(() => props.entries, (nextEntries, previousEntries) => {
  const forceScroll = shouldForceScroll(nextEntries, previousEntries)

  if (forceScroll) {
    stickToBottom.value = true
  }

  void scrollToBottom(forceScroll)
}, { deep: true, flush: 'post' })

const showJumpButton = computed(() => normalizedEntries.value.length > 0 && !stickToBottom.value)

function statusLabel(entry: TranscriptEntry) {
  if (entry.kind !== 'tool') {
    return ''
  }
  if (entry.status === 'running') return 'Running'
  if (entry.status === 'error') return 'Failed'
  return 'Done'
}

function detailSections(entry: TranscriptEntry): TranscriptEntryDetail[] {
  return entry.details ?? []
}

function isGroupedToolEntry(entry: TranscriptEntry) {
  return entry.kind === 'tool' && detailSections(entry).length > 1
}

function toolSummary(details: TranscriptEntryDetail[]) {
  const counts = {
    running: 0,
    done: 0,
  }

  for (const detail of details) {
    if (detail.loading) {
      counts.running += 1
      continue
    }
    counts.done += 1
  }

  return [`${counts.running} running`, `${counts.done} done`].join(' / ')
}
</script>

<template>
  <section class="messages-panel">
    <div ref="messagesBody" class="messages-body" @scroll="handleScroll">
      <div v-if="normalizedEntries.length === 0" class="messages-empty">
        <p>请尽情使唤 ~</p>
      </div>

      <div v-else class="messages-stack wide-stack">
        <article
          v-for="entry in normalizedEntries"
          :key="entry.id"
          class="trace-block"
          :class="[
            entry.kind,
            {
              compact: entry.kind !== 'reply',
              'compact-inline': entry.kind !== 'reply',
              'bubble-right': entry.kind === 'user',
              'bubble-content': entry.kind === 'user',
            },
          ]"
        >
          <div v-if="entry.kind === 'error'" class="trace-header">
            <span class="message-role">{{ entry.title }}</span>
            <span v-if="statusLabel(entry)" class="trace-status">{{ statusLabel(entry) }}</span>
          </div>
          <p v-if="entry.content && ['reply', 'error', 'user'].includes(entry.kind)" class="trace-content">
            {{ formatMessageContent(entry.content) }}
          </p>
          <details v-if="isGroupedToolEntry(entry)" class="trace-tool-group">
            <summary class="trace-detail-summary trace-tool-group-summary">
              <span class="trace-detail-label" :class="{ 'loading-marquee': entry.status === 'running' }">{{ entry.title }}</span>
              <span class="trace-detail-preview trace-tool-group-text">{{ toolSummary(detailSections(entry)) }}</span>
              <span v-if="entry.status === 'running'" class="trace-loading" aria-hidden="true"></span>
            </summary>
            <div class="trace-tool-items">
              <details
                v-for="detail in detailSections(entry)"
                :key="detail.key ?? `${entry.id}-${detail.label}`"
                class="trace-detail trace-tool-item"
                :open="detail.collapsed ? undefined : true"
              >
                <summary class="trace-detail-summary">
                  <span class="trace-detail-label">{{ detail.label }}</span>
                  <span class="trace-detail-preview">{{ detail.preview }}</span>
                  <span v-if="detail.loading" class="trace-loading" aria-hidden="true"></span>
                </summary>
                <div class="trace-detail-blocks">
                  <div v-for="block in detail.blocks ?? []" :key="`${detail.key ?? detail.label}-${block.label}`" class="trace-detail-block">
                    <div class="trace-detail-block-header">
                      <span>{{ block.label }}</span>
                      <span v-if="block.loading" class="trace-loading small" aria-hidden="true"></span>
                    </div>
                    <pre class="trace-detail-content">{{ formatMessageContent(block.value) }}</pre>
                  </div>
                </div>
              </details>
            </div>
          </details>
          <details
            v-else
            v-for="detail in detailSections(entry)"
            :key="detail.key ?? `${entry.id}-${detail.label}`"
            class="trace-detail"
            :open="detail.collapsed ? undefined : true"
          >
            <summary class="trace-detail-summary">
              <template v-if="entry.kind === 'tool'">
                <span class="trace-summary-leading">
                  <span class="trace-detail-label" :class="{ 'loading-marquee': detail.loading }">{{ entry.title }}</span>
                  <span class="trace-tool-name">{{ detail.label }}</span>
                </span>
                <span v-if="detail.preview" class="trace-status subtle">{{ detail.preview }}</span>
              </template>
              <template v-else>
                <span class="trace-detail-label" :class="{ 'loading-marquee': detail.loading }">{{ detail.label }}</span>
                <span class="trace-detail-preview">{{ detail.preview }}</span>
              </template>
              <span v-if="detail.loading" class="trace-loading" aria-hidden="true"></span>
            </summary>
            <div class="trace-detail-blocks">
              <div v-for="block in detail.blocks ?? []" :key="`${detail.key ?? detail.label}-${block.label}`" class="trace-detail-block">
                <div class="trace-detail-block-header">
                  <span>{{ block.label }}</span>
                  <span v-if="block.loading" class="trace-loading small" aria-hidden="true"></span>
                </div>
                <pre class="trace-detail-content">{{ formatMessageContent(block.value) }}</pre>
              </div>
            </div>
          </details>
        </article>
      </div>
    </div>

    <button
      v-if="showJumpButton"
      class="messages-jump-button ghost-button icon-button"
      type="button"
      aria-label="Jump to latest message"
      @click="jumpToBottom"
    >
      <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <g transform="translate(0.5 0.5)">
          <path d="M7.5 3.25v5.5" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" />
          <path d="M4.75 7.5 7.5 10.25 10.25 7.5" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" />
        </g>
      </svg>
    </button>
  </section>
</template>
