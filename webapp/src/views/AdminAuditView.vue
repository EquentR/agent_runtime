<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import {
  fetchAuditRun,
  fetchAuditRunEvents,
  fetchAuditRunReplay,
  fetchConversation,
  fetchConversations,
} from '../lib/api'
import type { AuditReplayArtifact, AuditReplayBundle, AuditReplayEvent, AuditRun, Conversation } from '../types/api'

const loading = ref(false)
const detailLoading = ref(false)
const errorMessage = ref('')
const conversations = ref<Conversation[]>([])
const selectedConversationId = ref('')
const selectedConversation = ref<Conversation | null>(null)
const auditRun = ref<AuditRun | null>(null)
const auditReplay = ref<AuditReplayBundle | null>(null)
const expandedTimelineSeq = ref<number | null>(null)

const selectedConversationSummary = computed(() => {
  if (selectedConversation.value) {
    return selectedConversation.value
  }
  return conversations.value.find((conversation) => conversation.id === selectedConversationId.value) ?? null
})

const replayArtifactsById = computed(() => {
  const map = new Map<string, AuditReplayArtifact>()
  for (const artifact of auditReplay.value?.artifacts ?? []) {
    map.set(artifact.id, artifact)
  }
  return map
})

const activeTimelineEntry = computed(() => {
  if (expandedTimelineSeq.value == null) {
    return null
  }
  return auditReplay.value?.timeline.find((entry) => entry.seq === expandedTimelineSeq.value) ?? null
})

const activeArtifact = computed(() => {
  const artifactId = activeTimelineEntry.value?.artifact?.id
  if (!artifactId) {
    return null
  }
  return replayArtifactsById.value.get(artifactId) ?? null
})

const activeArtifactBody = computed(() => {
  if (!activeArtifact.value?.body) {
    return ''
  }
  return JSON.stringify(activeArtifact.value.body, null, 2)
})

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

function toggleTimelineEntry(entry: AuditReplayEvent) {
  expandedTimelineSeq.value = expandedTimelineSeq.value === entry.seq ? null : entry.seq
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
  auditRun.value = null
  auditReplay.value = null
  expandedTimelineSeq.value = null

  try {
    const conversation = await fetchConversation(conversationId)
    selectedConversation.value = conversation
    const runId = resolveAuditRunId(conversation)

    if (!runId) {
      return
    }

    const [run, , replay] = await Promise.all([
      fetchAuditRun(runId),
      fetchAuditRunEvents(runId),
      fetchAuditRunReplay(runId),
    ])

    auditRun.value = run
    auditReplay.value = replay
    expandedTimelineSeq.value = replay.timeline[0]?.seq ?? null
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载审计详情失败'
  } finally {
    detailLoading.value = false
  }
}

onMounted(async () => {
  await loadConversationList()
})
</script>

<template>
  <main class="admin-audit-shell chat-shell">
    <section class="admin-audit-sidebar sidebar-panel">
      <div class="sidebar-header">
        <div>
          <p class="eyebrow">Admin Audit</p>
          <h2>审计会话</h2>
        </div>
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
          <div class="conversation-compact-dot" />
          <div class="conversation-preview">
            <strong class="conversation-title truncate-text">{{ conversation.title || '未命名对话' }}</strong>
            <p class="conversation-meta truncate-text">{{ conversation.created_by }}</p>
          </div>
        </button>
      </div>
    </section>

    <section class="admin-audit-stage chat-stage">
      <header class="topbar admin-audit-topbar">
        <div class="topbar-title-block">
          <p class="eyebrow">Replay Timeline</p>
          <h1 class="topbar-conversation-title">{{ selectedConversationSummary?.title || '选择一个会话' }}</h1>
        </div>
        <div class="status-pill idle">{{ auditRun?.status || '未加载' }}</div>
      </header>

      <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>
      <p v-else-if="detailLoading" class="messages-empty">正在加载详情...</p>
      <div v-else-if="selectedConversationSummary" class="admin-audit-content">
        <section class="admin-audit-summary-grid">
          <article class="admin-audit-summary-card">
            <p class="eyebrow">Conversation</p>
            <dl>
              <div>
                <dt>ID</dt>
                <dd>{{ selectedConversationSummary.id }}</dd>
              </div>
              <div>
                <dt>创建者</dt>
                <dd>{{ selectedConversationSummary.created_by }}</dd>
              </div>
              <div>
                <dt>模型</dt>
                <dd>{{ selectedConversationSummary.provider_id }} / {{ selectedConversationSummary.model_id }}</dd>
              </div>
            </dl>
          </article>

          <article class="admin-audit-summary-card">
            <p class="eyebrow">Run</p>
            <dl>
              <div>
                <dt>Run ID</dt>
                <dd>{{ auditRun?.id || resolveAuditRunId(selectedConversationSummary) || '未暴露 run_id' }}</dd>
              </div>
              <div>
                <dt>Task ID</dt>
                <dd>{{ auditRun?.task_id || '-' }}</dd>
              </div>
              <div>
                <dt>状态</dt>
                <dd>{{ auditRun?.status || '未找到审计运行' }}</dd>
              </div>
            </dl>
          </article>
        </section>

        <section v-if="auditReplay?.timeline.length" class="admin-audit-detail-grid">
          <article class="admin-audit-card admin-audit-timeline-panel">
            <div class="messages-header">
              <div>
                <p class="eyebrow">Timeline</p>
                <h2>操作时间线</h2>
              </div>
            </div>
            <div class="admin-audit-timeline">
              <button
                v-for="entry in auditReplay.timeline"
                :key="entry.seq"
                class="admin-audit-timeline-item"
                :class="{ active: expandedTimelineSeq === entry.seq }"
                type="button"
                @click="toggleTimelineEntry(entry)"
              >
                <div class="admin-audit-timeline-leading">
                  <span class="trace-kind-badge" :class="entry.phase === 'tool' ? 'tool' : entry.phase === 'request' ? 'reasoning' : 'error'" />
                  <div>
                    <strong>{{ entry.event_type }}</strong>
                    <p>{{ formatPhase(entry.phase) }} · #{{ entry.seq }}</p>
                  </div>
                </div>
                <span class="admin-audit-artifact-chip" v-if="entry.artifact">{{ formatArtifactTitle(entry.artifact.kind) }}</span>
              </button>
            </div>
          </article>

          <article class="admin-audit-card admin-audit-artifact-panel">
            <div class="messages-header">
              <div>
                <p class="eyebrow">Detail</p>
                <h2>{{ activeArtifact ? formatArtifactTitle(activeArtifact.kind) : '选择时间线条目' }}</h2>
              </div>
            </div>

            <div v-if="activeTimelineEntry" class="admin-audit-artifact-detail">
              <div class="admin-audit-detail-meta">
                <span>{{ activeTimelineEntry.event_type }}</span>
                <span>#{{ activeTimelineEntry.seq }}</span>
              </div>
              <pre v-if="activeArtifactBody" class="trace-detail-content admin-audit-json">{{ activeArtifactBody }}</pre>
              <pre v-else-if="activeTimelineEntry.payload" class="trace-detail-content admin-audit-json">{{ JSON.stringify(activeTimelineEntry.payload, null, 2) }}</pre>
              <p v-else class="messages-empty">当前条目没有可展示的详细内容。</p>
            </div>
            <p v-else class="messages-empty">点击左侧时间线以查看工具参数、输出或对话历史。</p>
          </article>
        </section>

        <div v-else class="messages-empty">暂无可展示的回放时间线。</div>
      </div>
      <div v-else class="messages-empty">请选择左侧会话以查看详情。</div>
    </section>
  </main>
</template>
