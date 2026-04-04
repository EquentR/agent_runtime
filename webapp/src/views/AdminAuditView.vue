<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { ArrowLeft, CircleCheck, Cpu, InfoFilled, Operation, WarningFilled } from '@element-plus/icons-vue'

import {
  fetchAuditConversationRuns,
  fetchAuditRunReplay,
  fetchConversation,
  fetchConversations,
} from '../lib/api'
import type { AuditReplayArtifact, AuditReplayBundle, AuditReplayEvent, AuditRun, Conversation } from '../types/api'

type TimelineFilter = 'all' | 'request' | 'tool' | 'error'
type SummaryFact = { label: string; value: string }

const loading = ref(false)
const detailLoading = ref(false)
const errorMessage = ref('')
const conversations = ref<Conversation[]>([])
const selectedConversationId = ref('')
const selectedConversation = ref<Conversation | null>(null)
const auditRuns = ref<AuditRun[]>([])
const auditReplays = ref<AuditReplayBundle[]>([])
const selectedTurnIndex = ref<number | null>(null)
const expandedTimelineKey = ref<string | null>(null)
const activeFilter = ref<TimelineFilter>('all')
const summaryExpanded = ref(false)
const turnMenuOpen = ref(false)
const turnMenuRef = ref<HTMLElement | null>(null)

const selectedConversationSummary = computed(() => {
  if (selectedConversation.value) {
    return selectedConversation.value
  }
  return conversations.value.find((conversation) => conversation.id === selectedConversationId.value) ?? null
})

const selectedAuditRun = computed(() => {
  if (selectedTurnIndex.value != null && selectedTurnIndex.value < auditRuns.value.length) {
    return auditRuns.value[selectedTurnIndex.value] ?? null
  }
  return auditRuns.value[auditRuns.value.length - 1] ?? null
})

const summaryFacts = computed<SummaryFact[]>(() => {
  const conversation = selectedConversationSummary.value
  return [
    { label: '创建者', value: conversation?.created_by || '-' },
    { label: '开始时间', value: formatConversationTime(conversation?.created_at) },
    { label: '轮次数', value: String(auditRuns.value.length) },
    { label: '状态', value: selectedAuditRun.value?.status || '未找到审计运行' },
    { label: 'Run ID', value: selectedAuditRun.value?.id || resolveAuditRunId(conversation) || '未暴露 run_id' },
    { label: 'Task ID', value: selectedAuditRun.value?.task_id || '-' },
    { label: '对话 ID', value: conversation?.id || '-' },
  ]
})

const visibleSummaryFacts = computed(() => {
  if (summaryExpanded.value) {
    return summaryFacts.value
  }
  return summaryFacts.value.slice(0, 4)
})

const selectedTurnLabel = computed(() => {
  if (selectedTurnIndex.value == null) {
    return '全部轮次'
  }
  return `轮次 ${selectedTurnIndex.value + 1}`
})

const mergedTimeline = computed(() => {
  const entries: Array<AuditReplayEvent & { turnIndex: number }> = []
  for (let i = 0; i < auditReplays.value.length; i++) {
    const replay = auditReplays.value[i]
    if (!replay) continue
    for (const entry of replay.timeline) {
      entries.push({ ...entry, turnIndex: i })
    }
  }
  return entries
})

const mergedArtifactsById = computed(() => {
  const map = new Map<string, AuditReplayArtifact>()
  for (const replay of auditReplays.value) {
    for (const artifact of replay.artifacts ?? []) {
      map.set(artifact.id, artifact)
    }
  }
  return map
})

const filteredTimeline = computed(() => {
  let timeline = mergedTimeline.value
  if (selectedTurnIndex.value != null) {
    timeline = timeline.filter((entry) => entry.turnIndex === selectedTurnIndex.value)
  }
  switch (activeFilter.value) {
    case 'tool':
      return timeline.filter((entry) => entry.phase === 'tool')
    case 'request':
      return timeline.filter((entry) => entry.phase === 'request')
    case 'error':
      return timeline.filter((entry) => entry.level === 'error' || entry.event_type.includes('fail') || entry.event_type.includes('error'))
    default:
      return timeline
  }
})

function timelineEntryKey(entry: AuditReplayEvent & { turnIndex: number }) {
  return `${entry.turnIndex}-${entry.seq}`
}

const activeTimelineEntry = computed(() => {
  if (expandedTimelineKey.value == null) {
    return filteredTimeline.value[0] ?? null
  }
  return filteredTimeline.value.find((entry) => timelineEntryKey(entry) === expandedTimelineKey.value) ?? filteredTimeline.value[0] ?? null
})

const activeArtifact = computed(() => {
  const artifactId = activeTimelineEntry.value?.artifact?.id
  if (!artifactId) {
    return null
  }
  return mergedArtifactsById.value.get(artifactId) ?? null
})

const activeArtifactBody = computed(() => {
  if (!activeArtifact.value?.body) {
    return ''
  }
  return JSON.stringify(activeArtifact.value.body, null, 2)
})

const detailHeading = computed(() => {
  if (activeTimelineEntry.value) {
    return displayTimelineTitle(activeTimelineEntry.value)
  }
  if (activeArtifact.value) {
    return formatArtifactTitle(activeArtifact.value.kind)
  }
  return '选择时间线条目'
})

function displayTimelineTitle(entry: AuditReplayEvent) {
  return entry.display_name || formatEventType(entry.event_type)
}

function toggleSummaryExpanded() {
  summaryExpanded.value = !summaryExpanded.value
}

function closeTurnMenu() {
  turnMenuOpen.value = false
}

function toggleTurnMenu() {
  turnMenuOpen.value = !turnMenuOpen.value
}

function handleGlobalPointerDown(event: PointerEvent) {
  if (!turnMenuOpen.value) {
    return
  }
  const target = event.target
  if (target instanceof Node && turnMenuRef.value?.contains(target)) {
    return
  }
  closeTurnMenu()
}

function handleGlobalKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    closeTurnMenu()
  }
}

function resolveDetailMetaLabel(entry: AuditReplayEvent & { turnIndex: number }) {
  return `${displayTimelineTitle(entry)} · ${entry.event_type}`
}

function resolveTimelineSummary(entry: AuditReplayEvent & { turnIndex: number }) {
  return `${entry.event_type} · ${formatPhase(entry.phase)} · #${entry.seq}${auditRuns.value.length > 1 ? ` · 轮次 ${entry.turnIndex + 1}` : ''}`
}

function resolveAuditRunId(conversation: Conversation | null) {
  if (!conversation) {
    return ''
  }
  return String(conversation.audit_run_id ?? conversation.auditRunId ?? conversation.run_id ?? conversation.runId ?? '').trim()
}

function formatPhase(phase: string) {
  switch (phase) {
    case 'request':
      return '请求'
    case 'tool':
      return '工具'
    case 'run':
      return '运行'
    default:
      return phase || '事件'
  }
}

function formatArtifactTitle(kind?: string) {
  switch (kind) {
    case 'request_messages':
      return '对话历史'
    case 'resolved_prompt':
      return '系统提示'
    case 'model_request':
      return '模型请求'
    case 'model_response':
      return '模型响应'
    case 'tool_arguments':
      return '工具调用参数'
    case 'tool_output':
      return '工具调用结果'
    case 'error_snapshot':
      return '错误快照'
    default:
      return kind || '审计详情'
  }
}

function formatEventType(eventType?: string) {
  switch (eventType) {
    case 'run.created':
      return '运行已创建'
    case 'run.started':
      return '运行开始'
    case 'conversation.loaded':
      return '会话已加载'
    case 'user_message.appended':
      return '用户消息追加'
    case 'step.started':
      return '步骤开始'
    case 'prompt.resolved':
      return '提示词解析'
    case 'request.built':
      return '构建 LLM 请求'
    case 'model.completed':
      return '模型生成'
    case 'tool.started':
      return '工具调用开始'
    case 'tool.finished':
      return '工具调用完成'
    case 'step.finished':
      return '步骤完成'
    case 'run.succeeded':
      return '运行成功'
    case 'run.failed':
      return '运行失败'
    case 'messages.persisted':
      return '消息已持久化'
    case 'tool.called':
      return '工具调用'
    case 'approval.requested':
      return '审批请求'
    case 'approval.resolved':
      return '审批已处理'
    case 'run.finished':
      return '运行完成'
    default:
      return eventType || '审计事件'
  }
}

function formatConversationTime(value?: string) {
  if (!value) {
    return '--'
  }
  return value.replace('T', ' ').slice(0, 16)
}

function statusTone(entry: AuditReplayEvent) {
  if (entry.level === 'error' || entry.event_type.includes('fail') || entry.event_type.includes('error')) {
    return 'error'
  }
  if (entry.event_type.startsWith('approval.')) {
    return 'request'
  }
  if (entry.phase === 'tool') {
    return 'tool'
  }
  if (entry.phase === 'request') {
    return 'request'
  }
  return 'run'
}

function iconForEntry(entry: AuditReplayEvent) {
  if (entry.level === 'error' || entry.event_type.includes('fail') || entry.event_type.includes('error')) {
    return WarningFilled
  }
  if (entry.event_type === 'approval.requested') {
    return WarningFilled
  }
  if (entry.event_type === 'approval.resolved') {
    return CircleCheck
  }
  if (entry.phase === 'tool') {
    return Operation
  }
  if (entry.phase === 'request') {
    return Cpu
  }
  if (entry.event_type.includes('finished') || entry.event_type.includes('succeeded')) {
    return CircleCheck
  }
  return InfoFilled
}

function toggleTimelineEntry(entry: AuditReplayEvent & { turnIndex: number }) {
  expandedTimelineKey.value = timelineEntryKey(entry)
}

function applyFilter(filter: TimelineFilter) {
  activeFilter.value = filter
  const first = filteredTimeline.value[0]
  expandedTimelineKey.value = first ? timelineEntryKey(first) : null
}

function selectTurn(index: number | null) {
  selectedTurnIndex.value = index
  closeTurnMenu()
  const first = filteredTimeline.value[0]
  expandedTimelineKey.value = first ? timelineEntryKey(first) : null
}

async function loadConversationList() {
  loading.value = true
  errorMessage.value = ''

  try {
    conversations.value = await fetchConversations()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载会话失败'
  } finally {
    loading.value = false
  }
}

async function selectConversation(conversationId: string) {
  selectedConversationId.value = conversationId
  detailLoading.value = true
  errorMessage.value = ''
  auditRuns.value = []
  auditReplays.value = []
  selectedTurnIndex.value = null
  expandedTimelineKey.value = null
  activeFilter.value = 'all'
  summaryExpanded.value = false
  closeTurnMenu()

  try {
    const conversation = await fetchConversation(conversationId)
    selectedConversation.value = conversation

    const runs = await fetchAuditConversationRuns(conversationId)
    auditRuns.value = runs

    if (runs.length === 0) {
      return
    }

    const replays = await Promise.all(runs.map((run) => fetchAuditRunReplay(run.id)))
    auditReplays.value = replays
    const first = mergedTimeline.value[0]
    expandedTimelineKey.value = first ? timelineEntryKey(first) : null
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载审计详情失败'
  } finally {
    detailLoading.value = false
  }
}

onMounted(async () => {
  window.addEventListener('pointerdown', handleGlobalPointerDown)
  window.addEventListener('keydown', handleGlobalKeydown)
  await loadConversationList()
})

onBeforeUnmount(() => {
  window.removeEventListener('pointerdown', handleGlobalPointerDown)
  window.removeEventListener('keydown', handleGlobalKeydown)
})
</script>

<template>
  <main class="admin-audit-shell chat-shell">
    <section class="admin-audit-sidebar sidebar-panel">
      <div class="sidebar-header">
        <div><h2>审计会话</h2></div>
      </div>

      <p v-if="loading" class="sidebar-empty">正在加载会话...</p>
      <div v-else-if="conversations.length === 0" class="sidebar-empty">暂无可查看的会话。</div>
      <div v-else class="sidebar-list admin-audit-list">
        <button
          v-for="conversation in conversations"
          :key="conversation.id"
          class="conversation-card admin-audit-conversation"
          :class="{ active: conversation.id === selectedConversationId }"
          type="button"
          :data-conversation-id="conversation.id"
          @click="selectConversation(conversation.id)"
        >
          <div class="conversation-preview admin-audit-conversation-main">
            <div class="admin-audit-conversation-row">
              <strong class="conversation-title truncate-text" :title="conversation.title || '未命名对话'">
                {{ conversation.title || '未命名对话' }}
              </strong>
            </div>
            <div class="admin-audit-conversation-meta conversation-meta">
              <span class="truncate-text">{{ conversation.created_by }}</span>
              <span class="admin-audit-conversation-time">{{ formatConversationTime(conversation.created_at) }}</span>
            </div>
          </div>
        </button>
      </div>
    </section>

    <section class="admin-audit-stage chat-stage">
      <header class="topbar admin-audit-topbar">
        <RouterLink class="ghost-button icon-button admin-audit-back-link" to="/chat" title="返回聊天" aria-label="返回聊天">
          <ArrowLeft />
        </RouterLink>
        <div class="topbar-title-block">
          <h1 class="topbar-conversation-title">{{ selectedConversationSummary?.title || '选择一个会话' }}</h1>
        </div>
        <div class="status-pill idle">{{ selectedAuditRun?.status || '未加载' }}</div>
      </header>

      <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>
      <p v-else-if="detailLoading" class="messages-empty">正在加载详情...</p>
      <div v-else-if="selectedConversationSummary" class="admin-audit-content">
        <section class="admin-audit-summary-grid">
          <article class="admin-audit-summary-card admin-audit-summary-compact" data-testid="summary-card">
            <div class="messages-header admin-audit-panel-header">
              <div><h2>审计概览</h2></div>
              <button type="button" class="ghost-button" data-testid="summary-toggle" @click="toggleSummaryExpanded">
                {{ summaryExpanded ? '收起详情' : '展开详情' }}
              </button>
            </div>
            <dl>
              <div v-for="fact in visibleSummaryFacts" :key="fact.label">
                <dt>{{ fact.label }}</dt>
                <dd>{{ fact.value }}</dd>
              </div>
            </dl>
          </article>
        </section>

        <section class="admin-audit-detail-grid">
          <article class="admin-audit-card admin-audit-timeline-panel">
            <div class="messages-header admin-audit-panel-header">
              <div><h2>操作时间线</h2></div>
              <div class="admin-audit-panel-controls">
                <div v-if="auditRuns.length > 1" ref="turnMenuRef" class="model-menu admin-audit-turn-menu" data-testid="turn-menu">
                  <button
                    class="model-menu-trigger"
                    type="button"
                    aria-haspopup="menu"
                    :aria-expanded="turnMenuOpen ? 'true' : 'false'"
                    data-testid="turn-menu-trigger"
                    @click="toggleTurnMenu"
                  >
                    <span class="model-menu-trigger-label">{{ selectedTurnLabel }}</span>
                    <span class="model-menu-trigger-caret" :class="{ open: turnMenuOpen }" aria-hidden="true"></span>
                  </button>
                  <transition name="model-menu-fade">
                    <div v-if="turnMenuOpen" class="model-menu-panel" role="menu" data-testid="turn-menu-panel">
                      <div class="model-menu-group">
                        <button
                          class="model-menu-option"
                          :class="{ active: selectedTurnIndex == null }"
                          type="button"
                          role="menuitemradio"
                          :aria-checked="selectedTurnIndex == null ? 'true' : 'false'"
                          data-testid="turn-option-all"
                          @click="selectTurn(null)"
                        >
                          <span class="model-menu-option-check" aria-hidden="true"></span>
                          <span class="model-menu-option-label">全部轮次</span>
                        </button>
                        <button
                          v-for="(run, index) in auditRuns"
                          :key="run.id"
                          class="model-menu-option"
                          :class="{ active: selectedTurnIndex === index }"
                          type="button"
                          role="menuitemradio"
                          :aria-checked="selectedTurnIndex === index ? 'true' : 'false'"
                          :data-testid="`turn-option-${index}`"
                          @click="selectTurn(index)"
                        >
                          <span class="model-menu-option-check" aria-hidden="true"></span>
                          <span class="model-menu-option-label">轮次 {{ index + 1 }}</span>
                        </button>
                      </div>
                    </div>
                  </transition>
                </div>
              </div>
            </div>

            <div class="admin-audit-filter-bar">
              <button class="admin-audit-filter" :class="{ active: activeFilter === 'all' }" data-filter="all" type="button" @click="applyFilter('all')">全部</button>
              <button class="admin-audit-filter" :class="{ active: activeFilter === 'request' }" data-filter="request" type="button" @click="applyFilter('request')">请求</button>
              <button class="admin-audit-filter" :class="{ active: activeFilter === 'tool' }" data-filter="tool" type="button" @click="applyFilter('tool')">工具</button>
              <button class="admin-audit-filter" :class="{ active: activeFilter === 'error' }" data-filter="error" type="button" @click="applyFilter('error')">错误</button>
            </div>

            <div v-if="filteredTimeline.length" class="admin-audit-timeline">
              <button
                v-for="entry in filteredTimeline"
                :key="`${entry.turnIndex}-${entry.seq}`"
                class="admin-audit-timeline-item"
                :class="[`tone-${statusTone(entry)}`, { active: activeTimelineEntry && timelineEntryKey(activeTimelineEntry) === timelineEntryKey(entry) }]"
                type="button"
                @click="toggleTimelineEntry(entry)"
              >
                <div class="admin-audit-timeline-leading">
                  <span class="admin-audit-entry-icon" :class="`tone-${statusTone(entry)}`">
                    <component :is="iconForEntry(entry)" />
                  </span>
                  <div>
                    <strong>{{ displayTimelineTitle(entry) }}</strong>
                    <p>{{ resolveTimelineSummary(entry) }}</p>
                  </div>
                </div>
                <span class="admin-audit-artifact-chip">{{ displayTimelineTitle(entry) }}</span>
              </button>
            </div>
            <p v-else class="messages-empty admin-audit-timeline-empty">当前筛选条件下没有可展示的时间线。</p>
          </article>

          <article class="admin-audit-card admin-audit-artifact-panel">
            <div class="messages-header admin-audit-panel-header">
              <div><h2>{{ detailHeading }}</h2></div>
            </div>

            <div v-if="activeTimelineEntry" class="admin-audit-artifact-detail">
              <div class="admin-audit-detail-meta">
                <span>{{ resolveDetailMetaLabel(activeTimelineEntry) }}</span>
                <span>#{{ activeTimelineEntry.seq }}</span>
                <span>{{ formatPhase(activeTimelineEntry.phase) }}</span>
                <span v-if="auditRuns.length > 1">轮次 {{ activeTimelineEntry.turnIndex + 1 }}</span>
              </div>
              <pre v-if="activeArtifactBody" class="trace-detail-content admin-audit-json">{{ activeArtifactBody }}</pre>
              <pre v-else-if="activeTimelineEntry.payload" class="trace-detail-content admin-audit-json">{{ JSON.stringify(activeTimelineEntry.payload, null, 2) }}</pre>
              <p v-else class="messages-empty">当前条目没有可展示的详细内容。</p>
            </div>
            <p v-else class="messages-empty">点击左侧时间线以查看工具参数、输出或对话历史。</p>
          </article>
        </section>
      </div>
      <div v-else class="messages-empty">请选择左侧会话以查看详情。</div>
    </section>
  </main>
</template>
