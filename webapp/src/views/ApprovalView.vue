<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { ArrowLeft } from '@element-plus/icons-vue'

import ApprovalRecordCard from '../components/ApprovalRecordCard.vue'
import {
  decideTaskApproval,
  fetchTaskApprovals,
  fetchTaskDetails,
  streamRunTask,
  TASK_STREAM_ABORTED_MESSAGE,
} from '../lib/api'
import { buildApprovalStreamEvent, updateTranscriptFromStreamEvent } from '../lib/transcript'
import type { TaskDetails, ToolApproval, TranscriptEntry } from '../types/api'

const route = useRoute()

const loading = ref(false)
const errorMessage = ref('')
const task = ref<TaskDetails | null>(null)
const approvalEntries = ref<TranscriptEntry[]>([])
const approvalDecisionStateById = ref<Record<string, { pending: boolean; decision: 'approve' | 'reject' }>>({})
let activeStreamAbortController: AbortController | null = null
let activeStreamingTaskId = ''
let activeLoadToken = 0

const selectedTaskId = computed(() => String(route.params.taskId ?? '').trim())
const approvals = computed(() =>
  approvalEntries.value.flatMap((entry) => (entry.kind === 'approval' && entry.approval ? [entry.approval] : [])),
)
const pendingCount = computed(() => approvals.value.filter((approval) => approval.status === 'pending').length)
const resolvedCount = computed(() => approvals.value.length - pendingCount.value)
const taskStatusLabel = computed(() => task.value?.status || '未加载')
const taskConversationId = computed(
  () => task.value?.result?.conversation_id ?? task.value?.result_json?.conversation_id ?? task.value?.input?.conversation_id ?? '',
)

function isTaskActive(nextTask: TaskDetails | null | undefined) {
  return (
    nextTask?.status === 'queued' ||
    nextTask?.status === 'running' ||
    nextTask?.status === 'waiting' ||
    nextTask?.status === 'cancel_requested'
  )
}

function stopTaskStream() {
  activeStreamAbortController?.abort()
  activeStreamAbortController = null
  activeStreamingTaskId = ''
}

function applyApprovalList(nextApprovals: ToolApproval[]) {
  let nextEntries: TranscriptEntry[] = []
  for (const approval of nextApprovals) {
    nextEntries = updateTranscriptFromStreamEvent(nextEntries, buildApprovalStreamEvent(approval))
  }
  approvalEntries.value = nextEntries
}

async function attachTaskStream(taskId: string) {
  if (!taskId || activeStreamingTaskId === taskId) {
    return
  }

  stopTaskStream()
  const abortController = new AbortController()
  activeStreamAbortController = abortController
  activeStreamingTaskId = taskId

  try {
    await streamRunTask(
      taskId,
      () => {
        void 0
      },
      (event) => {
        if (event.type === 'approval.requested' || event.type === 'approval.resolved') {
          approvalEntries.value = updateTranscriptFromStreamEvent(approvalEntries.value, event)
        }
      },
      { signal: abortController.signal },
    )
  } catch (error) {
    if (!(error instanceof Error) || error.message !== TASK_STREAM_ABORTED_MESSAGE) {
      errorMessage.value = error instanceof Error ? error.message : '审批事件流连接失败'
    }
  } finally {
    if (activeStreamAbortController === abortController) {
      activeStreamAbortController = null
    }
    if (activeStreamingTaskId === taskId) {
      activeStreamingTaskId = ''
    }
  }
}

async function loadApprovalView(taskId: string) {
  const loadToken = ++activeLoadToken
  stopTaskStream()
  approvalDecisionStateById.value = {}
  if (!taskId) {
    task.value = null
    approvalEntries.value = []
    errorMessage.value = ''
    loading.value = false
    return
  }

  loading.value = true
  errorMessage.value = ''
  task.value = null
  approvalEntries.value = []

  try {
    const [nextTask, nextApprovals] = await Promise.all([fetchTaskDetails(taskId), fetchTaskApprovals(taskId)])
    if (loadToken !== activeLoadToken || selectedTaskId.value !== taskId) {
      return
    }
    task.value = nextTask
    applyApprovalList(nextApprovals)

    if (isTaskActive(nextTask)) {
      void attachTaskStream(taskId)
    }
  } catch (error) {
    if (loadToken !== activeLoadToken || selectedTaskId.value !== taskId) {
      return
    }
    task.value = null
    approvalEntries.value = []
    errorMessage.value = error instanceof Error ? error.message : '加载审批失败'
  } finally {
    if (loadToken === activeLoadToken && selectedTaskId.value === taskId) {
      loading.value = false
    }
  }
}

async function handleApprovalDecision(input: {
  taskId: string
  approvalId: string
  decision: 'approve' | 'reject'
  reason: string
}) {
  if (!input.taskId || !input.approvalId) {
    return
  }

  if (approvalDecisionStateById.value[input.approvalId]?.pending) {
    return
  }

  const loadToken = activeLoadToken

  try {
    errorMessage.value = ''
    approvalDecisionStateById.value = {
      ...approvalDecisionStateById.value,
      [input.approvalId]: { pending: true, decision: input.decision },
    }
    const approval = await decideTaskApproval(input.taskId, input.approvalId, {
      decision: input.decision,
      reason: input.reason,
    })
    if (loadToken !== activeLoadToken || selectedTaskId.value !== input.taskId) {
      return
    }
    approvalEntries.value = updateTranscriptFromStreamEvent(
      approvalEntries.value,
      buildApprovalStreamEvent(approval, { type: 'approval.resolved', decision: input.decision }),
    )
  } catch (error) {
    if (loadToken !== activeLoadToken || selectedTaskId.value !== input.taskId) {
      return
    }
    errorMessage.value = error instanceof Error ? error.message : '审批提交失败'
  } finally {
    const nextState = { ...approvalDecisionStateById.value }
    delete nextState[input.approvalId]
    approvalDecisionStateById.value = nextState
  }
}

watch(
  selectedTaskId,
  (taskId) => {
    void loadApprovalView(taskId)
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  stopTaskStream()
})
</script>

<template>
  <main class="approval-shell admin-audit-shell">
    <section class="approval-sidebar sidebar-panel">
      <div class="sidebar-header approval-sidebar-header">
        <div>
          <h2>审批管理</h2>
          <p class="sidebar-copy">按任务查看并处理待审批工具调用。</p>
        </div>
      </div>

      <div class="approval-sidebar-body">
        <div class="admin-audit-card">
          <h3>任务范围</h3>
          <dl>
            <div>
              <dt>Task ID</dt>
              <dd data-task-id>{{ selectedTaskId || '-' }}</dd>
            </div>
            <div>
              <dt>Status</dt>
              <dd>{{ taskStatusLabel }}</dd>
            </div>
            <div>
              <dt>Conversation</dt>
              <dd>{{ taskConversationId || '-' }}</dd>
            </div>
            <div>
              <dt>Pending</dt>
              <dd>{{ pendingCount }}</dd>
            </div>
            <div>
              <dt>Resolved</dt>
              <dd>{{ resolvedCount }}</dd>
            </div>
          </dl>
        </div>
      </div>
    </section>

    <section class="approval-stage chat-stage">
      <header class="topbar approval-topbar">
        <RouterLink class="ghost-button icon-button admin-audit-back-link" to="/chat" title="返回聊天" aria-label="返回聊天">
          <ArrowLeft />
        </RouterLink>
      <div class="topbar-title-block">
        <h1 class="topbar-conversation-title">{{ selectedTaskId || '审批管理' }}</h1>
      </div>
        <div class="status-pill" :class="task ? 'idle' : 'loading'">{{ taskStatusLabel }}</div>
      </header>

      <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>
      <div v-else-if="!selectedTaskId" class="messages-empty approval-empty-state">
        <p>选择一个任务</p>
        <p>请从聊天中的审批入口进入，或指定任务后查看审批记录。</p>
      </div>
      <p v-else-if="loading" class="messages-empty">正在加载审批...</p>
      <div v-else-if="approvals.length === 0" class="messages-empty">当前任务暂无审批记录。</div>
      <div v-else class="approval-card-list">
        <ApprovalRecordCard
          v-for="approval in approvals"
          :key="approval.id"
          :approval="approval"
          :pending-decision="approvalDecisionStateById[approval.id]?.pending ? approvalDecisionStateById[approval.id]?.decision : ''"
          @approval-decision="handleApprovalDecision"
        />
      </div>
    </section>
  </main>
</template>
