<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import MarkdownIt from 'markdown-it'
import { CircleCheckFilled, CopyDocument, Operation, WarningFilled } from '@element-plus/icons-vue'

import ApprovalRecordCard from './ApprovalRecordCard.vue'
import { formatMessageContent } from '../lib/chat'
import type { QuestionInteractionSubmitInput, TranscriptEntry, TranscriptEntryDetail } from '../types/api'

const props = withDefaults(defineProps<{
  loading: boolean
  entries: TranscriptEntry[]
  showThinkingAndTools?: boolean
  approvalDecisionStateById?: Record<string, { pending: boolean; decision: 'approve' | 'reject' }>
  questionResponseStateById?: Record<string, { pending: boolean }>
}>(), {
  showThinkingAndTools: true,
})

const emit = defineEmits<{
  'approval-decision': [payload: { taskId: string; approvalId: string; decision: 'approve' | 'reject'; reason: string }]
  'interaction-respond': [payload: QuestionInteractionSubmitInput]
}>()

const questionSelectionById = ref<Record<string, string>>({})
const questionSelectionsById = ref<Record<string, string[]>>({})
const questionCustomTextById = ref<Record<string, string>>({})

const customQuestionOptionValue = '__custom__'

function questionOptions(entry: TranscriptEntry) {
  if (entry.kind !== 'question') {
    return []
  }
  const raw = entry.question_interaction?.request_json?.options
  return Array.isArray(raw) ? raw.map((item) => String(item)).filter(Boolean) : []
}

function questionPrompt(entry: TranscriptEntry) {
  if (entry.kind !== 'question') {
    return ''
  }
  return String(entry.question_interaction?.request_json?.question ?? '')
}

function questionPlaceholder(entry: TranscriptEntry) {
  if (entry.kind !== 'question') {
    return ''
  }
  return String(entry.question_interaction?.request_json?.placeholder ?? '补充你的回答')
}

function questionAllowsCustom(entry: TranscriptEntry) {
  if (entry.kind !== 'question') {
    return false
  }
  return entry.question_interaction?.request_json?.allow_custom === true
}

function questionMultiple(entry: TranscriptEntry) {
  if (entry.kind !== 'question') {
    return false
  }
  return entry.question_interaction?.request_json?.multiple === true
}

function questionSelection(entry: TranscriptEntry) {
  if (entry.kind !== 'question') {
    return ''
  }
  return questionSelectionById.value[entry.question_interaction?.id ?? ''] ?? ''
}

function questionCustomSelected(entry: TranscriptEntry) {
  if (entry.kind !== 'question' || !questionAllowsCustom(entry)) {
    return false
  }

  if (questionOptions(entry).length === 0) {
    return true
  }

  if (questionMultiple(entry)) {
    return questionSelections(entry).includes(customQuestionOptionValue)
  }

  return questionSelection(entry) === customQuestionOptionValue
}

function questionSelections(entry: TranscriptEntry) {
  if (entry.kind !== 'question') {
    return []
  }
  return questionSelectionsById.value[entry.question_interaction?.id ?? ''] ?? []
}

function questionCustomText(entry: TranscriptEntry) {
  if (entry.kind !== 'question') {
    return ''
  }
  return questionCustomTextById.value[entry.question_interaction?.id ?? ''] ?? ''
}

function questionIsPending(entry: TranscriptEntry) {
  return entry.kind === 'question' && entry.question_interaction?.status === 'pending'
}

// Track question interaction IDs that were ever seen as pending in this session.
// These should stay expanded even after being answered (streaming scenario).
// When entries are fully replaced (switching conversations), the set is reset.
const everPendingQuestionIds = ref(new Set<string>())

watch(
  () => props.entries,
  (nextEntries, previousEntries) => {
    // If entries array is replaced with a shorter or empty list, it means we
    // switched conversations — reset the set so history loads collapsed.
    if (!previousEntries || nextEntries.length < previousEntries.length) {
      everPendingQuestionIds.value = new Set()
    }
    for (const entry of nextEntries) {
      if (entry.kind === 'question' && entry.question_interaction?.status === 'pending') {
        const id = entry.question_interaction.id
        if (id) {
          everPendingQuestionIds.value.add(id)
        }
      }
    }
  },
  { deep: true, immediate: true },
)

function questionIsOpen(entry: TranscriptEntry) {
  if (entry.kind !== 'question' || !entry.question_interaction) {
    return undefined
  }
  if (questionIsPending(entry)) {
    return true
  }
  // Keep expanded if it was pending during this session (stream just answered it).
  // History entries that loaded already answered will NOT be in the set → collapsed.
  const id = entry.question_interaction.id
  return id && everPendingQuestionIds.value.has(id) ? true : undefined
}

function questionSubmissionLocked(entry: TranscriptEntry) {
  if (entry.kind !== 'question') {
    return false
  }

  const interactionId = entry.question_interaction?.id ?? ''
  return Boolean(interactionId && props.questionResponseStateById?.[interactionId]?.pending)
}

function questionFinalAnswer(entry: TranscriptEntry) {
  if (entry.kind !== 'question') {
    return ''
  }

  const response = entry.question_interaction?.response_json
  if (!response || typeof response !== 'object') {
    return ''
  }

  const parts: string[] = []
  const selectedOptionId = typeof response.selected_option_id === 'string' ? response.selected_option_id.trim() : ''
  const selectedOptionIds = Array.isArray(response.selected_option_ids)
    ? response.selected_option_ids.map((value) => String(value).trim()).filter(Boolean)
    : []
  const customText = typeof response.custom_text === 'string' ? response.custom_text.trim() : ''

  if (selectedOptionId) {
    parts.push(selectedOptionId)
  }
  if (selectedOptionIds.length > 0) {
    parts.push(selectedOptionIds.join('、'))
  }
  if (customText) {
    parts.push(customText)
  }

  return parts.join('\n')
}

function chooseQuestionOption(entry: TranscriptEntry, option: string) {
  const interactionId = entry.question_interaction?.id ?? ''
  if (!interactionId || !questionIsPending(entry) || questionSubmissionLocked(entry)) {
    return
  }

  if (questionMultiple(entry)) {
    const current = questionSelections(entry)
    let next: string[]

    if (option === customQuestionOptionValue) {
      next = current.includes(customQuestionOptionValue) ? [] : [customQuestionOptionValue]
    } else {
      const clearedCustom = current.filter((item) => item !== customQuestionOptionValue)
      next = clearedCustom.includes(option) ? clearedCustom.filter((item) => item !== option) : [...clearedCustom, option]
    }

    questionSelectionsById.value = {
      ...questionSelectionsById.value,
      [interactionId]: next,
    }
    return
  }

  questionSelectionById.value = {
    ...questionSelectionById.value,
    [interactionId]: option,
  }
}

function updateQuestionCustomText(entry: TranscriptEntry, value: string) {
  const interactionId = entry.question_interaction?.id ?? ''
  if (!interactionId || !questionIsPending(entry) || !questionAllowsCustom(entry) || questionSubmissionLocked(entry)) {
    return
  }
  questionCustomTextById.value = {
    ...questionCustomTextById.value,
    [interactionId]: value,
  }
}

function canSubmitQuestionInteraction(entry: TranscriptEntry) {
  if (!questionIsPending(entry)) {
    return false
  }
  if (questionSubmissionLocked(entry)) {
    return false
  }

  const currentSelections = questionMultiple(entry)
    ? questionSelections(entry).filter((option) => option !== customQuestionOptionValue)
    : questionSelection(entry) && questionSelection(entry) !== customQuestionOptionValue
      ? [questionSelection(entry)]
      : []
  const hasSelections = currentSelections.length > 0
  const hasCustomText = questionCustomSelected(entry) && questionCustomText(entry).trim() !== ''
  return hasSelections || hasCustomText
}

function submitQuestionInteraction(entry: TranscriptEntry) {
  const interaction = entry.question_interaction
  if (!interaction || interaction.status !== 'pending' || !canSubmitQuestionInteraction(entry)) {
    return
  }

  const selectedOptions = questionMultiple(entry)
    ? questionSelections(entry).filter((option) => option !== customQuestionOptionValue)
    : []
  const selectedOption = questionMultiple(entry)
    ? undefined
    : questionSelection(entry) && questionSelection(entry) !== customQuestionOptionValue
      ? questionSelection(entry)
      : undefined

  emit('interaction-respond', {
    taskId: interaction.task_id,
    interactionId: interaction.id,
    selectedOptionId: selectedOption,
    selectedOptionIds: questionMultiple(entry) ? selectedOptions : undefined,
    customText: questionCustomSelected(entry) ? questionCustomText(entry).trim() || undefined : undefined,
  })
}

const normalizedEntries = computed(() => {
  // Collect tool_call_ids from all question interaction entries so we can
  // suppress the corresponding tool-call cards (ask_user tool entries).
  const questionToolCallIds = new Set<string>()
  for (const entry of props.entries) {
    if (entry.kind === 'question') {
      const tcid = entry.question_interaction?.tool_call_id
      if (tcid) {
        questionToolCallIds.add(tcid)
      }
    }
  }

  const hideThinkingAndTools = props.showThinkingAndTools === false

  let filtered = props.entries

  if (hideThinkingAndTools) {
    filtered = filtered.filter((entry) => entry.kind !== 'reasoning' && entry.kind !== 'tool')
  }

  if (questionToolCallIds.size === 0) {
    return filtered
  }

  return filtered.filter((entry) => {
    if (entry.kind !== 'tool') {
      return true
    }
    // Keep this tool entry only if it has at least one detail that is NOT
    // matched by a question interaction's tool_call_id.
    const details = entry.details ?? []
    if (details.length === 0) {
      return true
    }
    return details.some((detail) => !questionToolCallIds.has(detail.key ?? ''))
  })
})
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
      <div v-if="normalizedEntries.length > 0" class="messages-stack wide-stack">
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
          <ApprovalRecordCard
            v-if="entry.kind === 'approval' && entry.approval"
            :approval="entry.approval"
            :pending-decision="props.approvalDecisionStateById?.[entry.approval.id]?.pending ? props.approvalDecisionStateById[entry.approval.id]?.decision : ''"
            variant="chat"
            @approval-decision="emit('approval-decision', $event)"
          />
          <details v-else-if="entry.kind === 'question' && entry.question_interaction" :open="questionIsOpen(entry)" class="question-interaction-card chat-question-card trace-flat-shell">
            <summary class="trace-detail-summary">
              <span class="trace-summary-leading">
                <span class="trace-kind-badge approval operation-badge" aria-hidden="true"><WarningFilled /></span>
                <span class="trace-detail-label">{{ entry.title }}</span>
              </span>
              <span class="trace-status subtle">{{ questionIsPending(entry) ? '待处理' : '已提交' }}</span>
            </summary>
            <div class="trace-detail-blocks">
              <div class="trace-detail-block">
                <div class="trace-detail-block-header"><span>问题</span></div>
                <pre class="trace-detail-content">{{ formatMessageContent(questionPrompt(entry)) }}</pre>
              </div>
              <div v-if="questionOptions(entry).length > 0" class="trace-detail-block">
                <div class="trace-detail-block-header"><span>选项</span></div>
                <ul class="question-options-list">
                  <li v-for="option in questionOptions(entry)" :key="option" class="question-option-item">
                    <button
                      class="question-option-button question-option-wireframe"
                      :class="{ selected: questionMultiple(entry) ? questionSelections(entry).includes(option) : questionSelection(entry) === option }"
                      type="button"
                      :disabled="!questionIsPending(entry) || questionSubmissionLocked(entry)"
                      :data-question-option="option"
                      @click="chooseQuestionOption(entry, option)"
                    >
                      <span class="question-option-indicator" :class="questionMultiple(entry) ? 'square-choice' : 'single-choice'" aria-hidden="true"></span>
                      <span class="question-option-label">{{ option }}</span>
                    </button>
                  </li>
                  <li v-if="questionAllowsCustom(entry)" class="question-option-item">
                    <button
                      class="question-option-button question-option-wireframe"
                      :class="{ selected: questionCustomSelected(entry) }"
                      type="button"
                      :disabled="!questionIsPending(entry) || questionSubmissionLocked(entry)"
                      :data-question-option="customQuestionOptionValue"
                      @click="chooseQuestionOption(entry, customQuestionOptionValue)"
                    >
                      <span class="question-option-indicator" :class="questionMultiple(entry) ? 'square-choice' : 'single-choice'" aria-hidden="true"></span>
                      <span class="question-option-label">自定义回答</span>
                    </button>
                    <input
                      v-if="questionCustomSelected(entry)"
                      class="question-custom-input question-card-input question-option-custom-input"
                      type="text"
                      :disabled="!questionIsPending(entry) || questionSubmissionLocked(entry)"
                      :placeholder="questionPlaceholder(entry)"
                      :value="questionCustomText(entry)"
                      @input="updateQuestionCustomText(entry, ($event.target as HTMLInputElement).value)"
                    />
                  </li>
                </ul>
              </div>
              <div v-else-if="questionAllowsCustom(entry)" class="trace-detail-block">
                <div class="trace-detail-block-header"><span>回答</span></div>
                <input
                  class="question-custom-input question-card-input"
                  data-question-custom-only
                  type="text"
                  :disabled="!questionIsPending(entry) || questionSubmissionLocked(entry)"
                  :placeholder="questionPlaceholder(entry)"
                  :value="questionCustomText(entry)"
                  @input="updateQuestionCustomText(entry, ($event.target as HTMLInputElement).value)"
                />
              </div>
              <div v-if="!questionIsPending(entry) && questionFinalAnswer(entry)" class="trace-detail-block">
                <div class="trace-detail-block-header"><span>最终回答</span></div>
                <pre class="trace-detail-content">{{ formatMessageContent(questionFinalAnswer(entry)) }}</pre>
              </div>
            </div>
            <div v-if="questionIsPending(entry)" class="trace-reply-footer question-card-actions">
              <button data-question-submit class="approval-action-button question-submit-button" type="button" :disabled="!canSubmitQuestionInteraction(entry)" @click="submitQuestionInteraction(entry)">
                提交回答
              </button>
            </div>
          </details>
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
          <template v-else-if="entry.kind === 'reasoning'">
            <details
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
          </template>
          <template v-else>
            <details
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
          </template>
        </article>
        <div v-if="props.loading" class="messages-generating-indicator" aria-live="polite">
          <span class="messages-generating-spinner" aria-hidden="true"></span>
          <span>正在生成</span>
        </div>
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
