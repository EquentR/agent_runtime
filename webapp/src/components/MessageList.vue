<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import MarkdownIt from 'markdown-it'
import { CircleCheckFilled, CopyDocument, Operation, WarningFilled } from '@element-plus/icons-vue'

import { formatMessageContent } from '../lib/chat'
import type { TranscriptEntry, TranscriptEntryDetail } from '../types/api'

const props = defineProps<{
  loading: boolean
  entries: TranscriptEntry[]
}>()

const normalizedEntries = computed(() => props.entries)
const messagesBody = ref<HTMLDivElement | null>(null)
const copyToastVisible = ref(false)
const copyToastMessage = ref('')
const copyToastVariant = ref<'success' | 'error'>('success')
let copyToastTimer: ReturnType<typeof setTimeout> | null = null
const bottomOffsetThreshold = 24
const stickToBottom = ref(true)
const markdown = new MarkdownIt({
  html: false,
  linkify: true,
  breaks: true,
})

markdown.renderer.rules.fence = (tokens, index) => {
  const token = tokens[index]
  const language = token.info.trim().split(/\s+/)[0] ?? ''
  const escapedLanguage = markdown.utils.escapeHtml(language)
  const escapedCode = markdown.utils.escapeHtml(token.content)

  return [
    '<div class="markdown-code-block">',
    '<div class="markdown-code-toolbar">',
    `<span class="markdown-code-language">${escapedLanguage || 'code'}</span>`,
    '<button class="markdown-code-copy icon-button compact-icon-button" type="button" aria-label="复制代码块">',
    '<svg viewBox="0 0 16 16" fill="none" aria-hidden="true">',
    '<path d="M5.5 2.75h5.25A1.75 1.75 0 0 1 12.5 4.5v6.25a1.75 1.75 0 0 1-1.75 1.75H5.5a1.75 1.75 0 0 1-1.75-1.75V4.5A1.75 1.75 0 0 1 5.5 2.75Z" stroke="currentColor" stroke-width="1.25"/>',
    '<path d="M4.75 5.5H4A1.5 1.5 0 0 0 2.5 7v5A1.5 1.5 0 0 0 4 13.5h5A1.5 1.5 0 0 0 10.5 12v-.75" stroke="currentColor" stroke-width="1.25" stroke-linecap="round"/>',
    '</svg>',
    '</button>',
    '</div>',
    `<pre><code class="language-${escapedLanguage}">${escapedCode}</code></pre>`,
    '</div>',
  ].join('')
}

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

onBeforeUnmount(() => {
  if (copyToastTimer) {
    clearTimeout(copyToastTimer)
  }
})

watch(() => props.entries, (nextEntries, previousEntries) => {
  const forceScroll = shouldForceScroll(nextEntries, previousEntries)

  if (forceScroll) {
    stickToBottom.value = true
  }

  void scrollToBottom(forceScroll)
}, { deep: true, flush: 'post' })

const showJumpButton = computed(() => normalizedEntries.value.length > 0 && !stickToBottom.value)

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

function previewError(content: string | undefined) {
  const trimmed = (content ?? '').replace(/\s+/g, ' ').trim()
  if (!trimmed) {
    return '查看详情'
  }
  return trimmed.length > 72 ? `${trimmed.slice(0, 72)}...` : trimmed
}

function primaryBlockValue(detail: TranscriptEntryDetail) {
  return detail.blocks?.[0]?.value ?? ''
}

function hasUsage(entry: TranscriptEntry) {
  return entry.kind === 'reply' && Boolean(entry.token_usage || entry.provider_id || entry.model_id)
}

function canCopyReply(entry: TranscriptEntry) {
  return entry.kind === 'reply' && Boolean(entry.content)
}

function formatUsageValue(value: number | undefined) {
  if (typeof value !== 'number') {
    return '--'
  }

  return value.toLocaleString('en-US')
}

function replyUsageSummary(entry: TranscriptEntry) {
  if (entry.kind !== 'reply') {
    return ''
  }

  const usage = entry.token_usage
  const providerModel = [entry.provider_id, entry.model_id].filter(Boolean).join(' / ')

  return [
    providerModel,
    usage ? `Tokens ${formatUsageValue(usage.total_tokens)}` : '',
    usage ? `Prompt ${formatUsageValue(usage.prompt_tokens)}` : '',
    usage ? `Completion ${formatUsageValue(usage.completion_tokens)}` : '',
    usage?.cached_prompt_tokens ? `Cached ${formatUsageValue(usage.cached_prompt_tokens)}` : '',
  ].filter(Boolean).join(' · ')
}

function renderReplyContent(content: string) {
  return markdown.render(formatMessageContent(content))
}

async function copyReply(entry: TranscriptEntry) {
  if (!canCopyReply(entry) || !navigator.clipboard) {
    showCopyToast('复制失败', 'error')
    return
  }

  await copyText(formatMessageContent(entry.content ?? ''))
}

async function copyText(text: string) {
  if (!navigator.clipboard) {
    showCopyToast('复制失败', 'error')
    return
  }

  try {
    await navigator.clipboard.writeText(text)
    showCopyToast('已复制', 'success')
  } catch {
    showCopyToast('复制失败', 'error')
  }
}

async function handleMarkdownClick(event: MouseEvent) {
  const target = event.target instanceof HTMLElement ? event.target.closest('.markdown-code-copy') : null
  if (!(target instanceof HTMLButtonElement)) {
    return
  }

  const code = target.closest('.markdown-code-block')?.querySelector('code')?.textContent
  if (!code) {
    showCopyToast('复制失败', 'error')
    return
  }

  await copyText(code)
}

function showCopyToast(message: string, variant: 'success' | 'error') {
  copyToastMessage.value = message
  copyToastVariant.value = variant
  copyToastVisible.value = true

  if (copyToastTimer) {
    clearTimeout(copyToastTimer)
  }

  copyToastTimer = setTimeout(() => {
    copyToastVisible.value = false
    copyToastTimer = null
  }, 1800)
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
              'centered-trace': entry.kind !== 'user',
              compact: entry.kind !== 'reply',
              'bubble-right': entry.kind === 'user',
              'bubble-content': entry.kind === 'user',
            },
          ]"
        >
          <details v-if="entry.kind === 'error'" class="trace-detail trace-error-detail trace-flat-shell">
            <summary class="trace-detail-summary">
              <span class="trace-summary-leading">
                <span class="trace-kind-badge error" aria-hidden="true"></span>
                <span class="trace-detail-label">{{ entry.title }}</span>
              </span>
              <span class="trace-detail-preview">{{ previewError(entry.content) }}</span>
            </summary>
            <pre v-if="entry.content" class="trace-detail-content">{{ formatMessageContent(entry.content) }}</pre>
          </details>
          <div
            v-if="entry.kind === 'reply' && entry.content"
            class="trace-content markdown-content"
            v-html="renderReplyContent(entry.content)"
            @click="handleMarkdownClick"
          ></div>
          <p v-else-if="entry.content && entry.kind === 'user'" class="trace-content">
            {{ formatMessageContent(entry.content) }}
          </p>
          <div v-if="entry.kind === 'reply' && (canCopyReply(entry) || hasUsage(entry))" class="trace-reply-footer">
            <button
              v-if="canCopyReply(entry)"
              class="trace-copy-button icon-button"
              type="button"
              aria-label="复制消息"
              @click="copyReply(entry)"
            >
              <CopyDocument />
              <span class="trace-copy-toast-anchor" aria-hidden="true">
                <transition name="copy-toast-fade">
                  <span v-if="copyToastVisible" class="trace-copy-toast" :class="copyToastVariant">
                    <CircleCheckFilled v-if="copyToastVariant === 'success'" />
                    <WarningFilled v-else />
                    <span>{{ copyToastMessage }}</span>
                  </span>
                </transition>
              </span>
            </button>
            <p v-if="hasUsage(entry)" class="trace-reply-usage">
              {{ replyUsageSummary(entry) }}
            </p>
          </div>
          <details v-if="isGroupedToolEntry(entry)" class="trace-tool-group trace-flat-shell">
            <summary class="trace-detail-summary trace-tool-group-summary">
              <span class="trace-summary-leading">
                <span class="trace-kind-badge tool operation-badge" aria-hidden="true"><Operation /></span>
                <span class="trace-detail-label" :class="{ 'loading-marquee': entry.status === 'running' }">{{ entry.title }}</span>
              </span>
              <span class="trace-detail-preview trace-tool-group-text">{{ toolSummary(detailSections(entry)) }}</span>
              <span v-if="entry.status === 'running'" class="trace-loading" aria-hidden="true"></span>
            </summary>
            <div class="trace-tool-items">
              <details
                v-for="detail in detailSections(entry)"
                :key="detail.key ?? `${entry.id}-${detail.label}`"
                class="trace-detail trace-tool-item trace-flat-shell"
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
            v-else-if="entry.kind === 'reasoning'"
            v-for="detail in detailSections(entry)"
            :key="detail.key ?? `${entry.id}-${detail.label}`"
            class="trace-detail trace-flat-shell"
            :open="detail.collapsed ? undefined : true"
          >
            <summary class="trace-detail-summary">
              <span class="trace-summary-leading">
                <span class="trace-kind-badge reasoning" aria-hidden="true"></span>
                <span class="trace-detail-label" :class="{ 'loading-marquee': detail.loading }">{{ detail.label }}</span>
              </span>
              <span class="trace-detail-preview">{{ detail.preview }}</span>
              <span v-if="detail.loading" class="trace-loading" aria-hidden="true"></span>
            </summary>
            <pre v-if="primaryBlockValue(detail)" class="trace-detail-content">{{ formatMessageContent(primaryBlockValue(detail)) }}</pre>
          </details>
          <details
            v-else
            v-for="detail in detailSections(entry)"
            :key="detail.key ?? `${entry.id}-${detail.label}`"
            class="trace-detail"
            :class="{ 'trace-flat-shell': entry.kind === 'tool' }"
            :open="detail.collapsed ? undefined : true"
          >
            <summary class="trace-detail-summary">
              <template v-if="entry.kind === 'tool'">
                <span class="trace-summary-leading">
                  <span class="trace-kind-badge tool operation-badge" aria-hidden="true"><Operation /></span>
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
