<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { WarningFilled } from '@element-plus/icons-vue'

import { formatMessageContent } from '../lib/chat'
import type { ToolApproval } from '../types/api'

const props = defineProps<{
  approval: ToolApproval
  pendingDecision?: 'approve' | 'reject' | ''
  variant?: 'chat' | 'default'
}>()

const emit = defineEmits<{
  'approval-decision': [payload: { taskId: string; approvalId: string; decision: 'approve' | 'reject'; reason: string }]
}>()

const decisionReason = ref('')

const approvalTitle = computed(() => (props.approval.status === 'pending' ? '等待审批' : '审批已处理'))
const submissionLocked = computed(() => Boolean(props.pendingDecision))
const cardVariant = computed(() => props.variant ?? 'default')

watch(
  () => props.approval.id,
  () => {
    decisionReason.value = ''
  },
  { immediate: true },
)

function approvalStatusText(status: string) {
  if (status === 'pending') return '待处理'
  if (status === 'approved') return '已同意'
  if (status === 'rejected') return '已拒绝'
  if (status === 'cancelled') return '已取消'
  if (status === 'expired') return '已过期'
  return status
}

function submitDecision(decision: 'approve' | 'reject') {
  if (!props.approval.id || props.approval.status !== 'pending' || submissionLocked.value) {
    return
  }

  emit('approval-decision', {
    taskId: props.approval.task_id,
    approvalId: props.approval.id,
    decision,
    reason: decisionReason.value.trim(),
  })
}
</script>

<template>
  <details open class="approval-card trace-flat-shell" :class="{ 'chat-approval-card': cardVariant === 'chat' }">
    <summary class="trace-detail-summary">
      <span class="trace-summary-leading">
        <span class="trace-kind-badge approval operation-badge" aria-hidden="true"><WarningFilled /></span>
        <span class="trace-detail-label">{{ approvalTitle }}</span>
        <span class="trace-tool-name">{{ approval.tool_name }}</span>
      </span>
      <span class="trace-status subtle">{{ approvalStatusText(approval.status) }}</span>
    </summary>
    <div class="trace-detail-blocks">
      <div class="trace-detail-block">
        <div class="trace-detail-block-header"><span>风险等级</span></div>
        <pre class="trace-detail-content">{{ approval.risk_level || '未标注' }}</pre>
      </div>
      <div v-if="approval.reason" class="trace-detail-block">
        <div class="trace-detail-block-header"><span>审批原因</span></div>
        <pre class="trace-detail-content">{{ formatMessageContent(approval.reason) }}</pre>
      </div>
      <div class="trace-detail-block">
        <div class="trace-detail-block-header"><span>调用参数</span></div>
        <pre class="trace-detail-content">{{ formatMessageContent(approval.arguments_summary) }}</pre>
      </div>
      <div v-if="approval.status === 'pending'" class="trace-detail-block">
        <div class="trace-detail-block-header"><span>审批说明</span></div>
        <input
          class="approval-reason-input"
          type="text"
          :value="decisionReason"
          :disabled="submissionLocked"
          placeholder="可选，补充审批说明"
          @input="decisionReason = ($event.target as HTMLInputElement).value"
        />
      </div>
      <div v-else class="trace-detail-block">
        <div class="trace-detail-block-header"><span>处理结果</span></div>
        <pre class="trace-detail-content">{{ formatMessageContent([approvalStatusText(approval.status), approval.decision_reason, approval.decision_by ? `操作人: ${approval.decision_by}` : ''].filter(Boolean).join(' · ')) }}</pre>
      </div>
    </div>
    <div v-if="approval.status === 'pending'" class="trace-reply-footer approval-card-actions">
      <button
        data-approval-action="approve"
        class="ghost-button approval-action-button approval-action-approve"
        type="button"
        :disabled="submissionLocked"
        @click="submitDecision('approve')"
      >
        同意执行
      </button>
      <button
        data-approval-action="reject"
        class="ghost-button approval-action-button approval-action-reject"
        type="button"
        :disabled="submissionLocked"
        @click="submitDecision('reject')"
      >
        拒绝执行
      </button>
    </div>
  </details>
</template>
