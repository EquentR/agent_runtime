<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Operation } from '@element-plus/icons-vue'

import { formatMessageContent } from '../lib/chat'
import type { ToolApproval } from '../types/api'

const props = defineProps<{
  approval: ToolApproval
  pendingDecision?: 'approve' | 'reject' | ''
}>()

const emit = defineEmits<{
  'approval-decision': [payload: { taskId: string; approvalId: string; decision: 'approve' | 'reject'; reason: string }]
}>()

const decisionReason = ref('')

const approvalTitle = computed(() => (props.approval.status === 'pending' ? '等待审批' : '审批已处理'))
const submissionLocked = computed(() => Boolean(props.pendingDecision))

watch(
  () => props.approval.id,
  () => {
    decisionReason.value = ''
  },
  { immediate: true },
)

function approvalStatusText(status: string) {
  if (status === 'pending') return 'Pending'
  if (status === 'approved') return 'Approved'
  if (status === 'rejected') return 'Rejected'
  if (status === 'cancelled') return 'Cancelled'
  if (status === 'expired') return 'Expired'
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
  <div class="approval-card trace-flat-shell">
    <div class="trace-detail-summary">
      <span class="trace-summary-leading">
        <span class="trace-kind-badge tool operation-badge" aria-hidden="true"><Operation /></span>
        <span class="trace-detail-label">{{ approvalTitle }}</span>
        <span class="trace-tool-name">{{ approval.tool_name }}</span>
      </span>
      <span class="trace-status subtle">{{ approvalStatusText(approval.status) }}</span>
    </div>
    <div class="trace-detail-blocks">
      <div class="trace-detail-block">
        <div class="trace-detail-block-header"><span>Risk</span></div>
        <pre class="trace-detail-content">{{ approval.risk_level || 'unknown' }}</pre>
      </div>
      <div v-if="approval.reason" class="trace-detail-block">
        <div class="trace-detail-block-header"><span>Reason</span></div>
        <pre class="trace-detail-content">{{ formatMessageContent(approval.reason) }}</pre>
      </div>
      <div class="trace-detail-block">
        <div class="trace-detail-block-header"><span>Arguments</span></div>
        <pre class="trace-detail-content">{{ formatMessageContent(approval.arguments_summary) }}</pre>
      </div>
      <div v-if="approval.status === 'pending'" class="trace-detail-block">
        <div class="trace-detail-block-header"><span>Decision note</span></div>
        <input
          class="approval-reason-input"
          type="text"
          :value="decisionReason"
          :disabled="submissionLocked"
          placeholder="Optional reason"
          @input="decisionReason = ($event.target as HTMLInputElement).value"
        />
      </div>
      <div v-else-if="approval.decision_reason || approval.decision_by" class="trace-detail-block">
        <div class="trace-detail-block-header"><span>Resolution</span></div>
        <pre class="trace-detail-content">{{ formatMessageContent([approval.decision_reason, approval.decision_by].filter(Boolean).join(' · ')) }}</pre>
      </div>
    </div>
    <div v-if="approval.status === 'pending'" class="trace-reply-footer">
      <button data-approval-action="approve" class="ghost-button" type="button" :disabled="submissionLocked" @click="submitDecision('approve')">Allow</button>
      <button data-approval-action="reject" class="ghost-button" type="button" :disabled="submissionLocked" @click="submitDecision('reject')">Reject</button>
    </div>
  </div>
</template>
