import { buildApprovalStreamEvent, updateTranscriptFromStreamEvent } from './transcript'
import type { TaskDetails, ToolApproval, TranscriptEntry } from '../types/api'

export const ACTIVE_TASK_STATUSES = ['queued', 'running', 'waiting', 'cancel_requested'] as const
export const TASK_WAITING_FOR_TOOL_APPROVAL_REASON = 'waiting_for_tool_approval'
export const TASK_WAITING_FOR_INTERACTION_REASON = 'waiting_for_interaction'

export function resolveTaskConversationId(task: TaskDetails | null | undefined) {
  return task?.result?.conversation_id ?? task?.result_json?.conversation_id ?? task?.input?.conversation_id ?? ''
}

export function isTaskActive(task: TaskDetails | null | undefined) {
  return ACTIVE_TASK_STATUSES.includes((task?.status ?? '') as (typeof ACTIVE_TASK_STATUSES)[number])
}

export function isTaskWaitingForInput(task: TaskDetails | null | undefined) {
  return !!task && task.status === 'waiting' && (
    task.suspend_reason === TASK_WAITING_FOR_TOOL_APPROVAL_REASON ||
    task.suspend_reason === TASK_WAITING_FOR_INTERACTION_REASON
  )
}

export function buildApprovalEntriesFromList(nextApprovals: ToolApproval[], initialEntries: TranscriptEntry[] = []) {
  let nextEntries: TranscriptEntry[] = [...initialEntries]
  for (const approval of nextApprovals) {
    nextEntries = updateTranscriptFromStreamEvent(nextEntries, buildApprovalStreamEvent(approval, { type: 'approval.requested' }))
  }
  return nextEntries
}
