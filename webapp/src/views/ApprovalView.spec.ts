import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

const api = vi.hoisted(() => ({
  TASK_STREAM_ABORTED_MESSAGE: 'Task event stream aborted',
  fetchTaskApprovals: vi.fn(),
  fetchTaskDetails: vi.fn(),
  decideTaskApproval: vi.fn(),
  normalizeToolApproval: vi.fn((approval: any) => ({
    id: approval.id ?? approval.approval_id ?? '',
    task_id: approval.task_id ?? '',
    conversation_id: approval.conversation_id ?? '',
    step_index: approval.step_index ?? approval.step,
    tool_call_id: approval.tool_call_id ?? '',
    tool_name: approval.tool_name ?? '',
    arguments_summary: approval.arguments_summary ?? '',
    risk_level: approval.risk_level ?? '',
    reason: approval.reason,
    status: approval.status ?? '',
    decision: approval.decision,
    decision_by: approval.decision_by,
    decision_reason: approval.decision_reason,
    decision_at: approval.decision_at,
    created_at: approval.created_at,
    updated_at: approval.updated_at,
  })),
  streamRunTask: vi.fn(),
}))

vi.mock('../lib/api', () => api)

import ApprovalView from './ApprovalView.vue'

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/approvals/:taskId', component: ApprovalView },
      { path: '/chat', component: { template: '<div>chat</div>' } },
    ],
  })
}

function createDeferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function buildPendingApproval(overrides: Record<string, unknown> = {}) {
  return {
    id: 'approval_1',
    task_id: 'task_waiting',
    conversation_id: 'conv_waiting',
    step_index: 3,
    tool_call_id: 'call_1',
    tool_name: 'bash',
    arguments_summary: 'rm -rf /tmp/demo',
    risk_level: 'high',
    reason: 'dangerous filesystem mutation',
    status: 'pending',
    ...overrides,
  }
}

describe('ApprovalView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists task approvals for the selected task and applies approval SSE updates', async () => {
    const stream = createDeferred<{ conversation_id: string }>()
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_waiting',
      status: 'waiting',
      suspend_reason: 'waiting_for_tool_approval',
      input: { conversation_id: 'conv_waiting' },
      created_by: 'alice',
      created_at: '',
      updated_at: '',
      task_type: 'agent.run',
    })
    api.fetchTaskApprovals.mockResolvedValue([buildPendingApproval()])
    api.streamRunTask.mockReturnValue(stream.promise)

    const router = makeRouter()
    await router.push('/approvals/task_waiting')
    await router.isReady()

    const wrapper = mount(ApprovalView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    expect(api.fetchTaskDetails).toHaveBeenCalledWith('task_waiting')
    expect(api.fetchTaskApprovals).toHaveBeenCalledWith('task_waiting')
    expect(api.streamRunTask).toHaveBeenCalledWith(
      'task_waiting',
      expect.any(Function),
      expect.any(Function),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
    expect(wrapper.text()).toContain('task_waiting')
    expect(wrapper.text()).toContain('bash')
    expect(wrapper.text()).toContain('rm -rf /tmp/demo')

    const onEvent = api.streamRunTask.mock.calls[0]?.[2] as ((event: any) => void) | undefined
    onEvent?.({
      type: 'approval.resolved',
      payload: {
        approval_id: 'approval_1',
        task_id: 'task_waiting',
        decision: 'approve',
        decision_reason: 'checked in queue',
        decision_by: 'alice',
        status: 'approved',
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Approved')
    expect(wrapper.text()).toContain('checked in queue')

    stream.reject(new Error('Task event stream aborted'))
  })

  it('submits an approval decision for the selected task and stays on the same route', async () => {
    const stream = createDeferred<{ conversation_id: string }>()
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_waiting',
      status: 'waiting',
      suspend_reason: 'waiting_for_tool_approval',
      input: { conversation_id: 'conv_waiting' },
      created_by: 'alice',
      created_at: '',
      updated_at: '',
      task_type: 'agent.run',
    })
    api.fetchTaskApprovals.mockResolvedValue([buildPendingApproval()])
    api.decideTaskApproval.mockResolvedValue(
      buildPendingApproval({
        status: 'approved',
        decision: 'approve',
        decision_reason: 'looks safe',
        decision_by: 'alice',
      }),
    )
    api.streamRunTask.mockReturnValue(stream.promise)

    const router = makeRouter()
    await router.push('/approvals/task_waiting')
    await router.isReady()

    const wrapper = mount(ApprovalView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    await wrapper.get('.approval-reason-input').setValue('looks safe')
    await wrapper.get('[data-approval-action="approve"]').trigger('click')
    await flushPromises()

    expect(api.decideTaskApproval).toHaveBeenCalledWith('task_waiting', 'approval_1', {
      decision: 'approve',
      reason: 'looks safe',
    })
    expect(router.currentRoute.value.fullPath).toBe('/approvals/task_waiting')
    expect(wrapper.text()).toContain('Approved')
    expect(wrapper.text()).toContain('looks safe')

    stream.reject(new Error('Task event stream aborted'))
  })

  it('submits a reject decision for the selected task', async () => {
    const stream = createDeferred<{ conversation_id: string }>()
    api.fetchTaskDetails.mockResolvedValue({
      id: 'task_waiting',
      status: 'waiting',
      suspend_reason: 'waiting_for_tool_approval',
      input: { conversation_id: 'conv_waiting' },
      created_by: 'alice',
      created_at: '',
      updated_at: '',
      task_type: 'agent.run',
    })
    api.fetchTaskApprovals.mockResolvedValue([buildPendingApproval()])
    api.decideTaskApproval.mockResolvedValue(
      buildPendingApproval({
        status: 'rejected',
        decision: 'reject',
        decision_reason: 'too risky',
        decision_by: 'alice',
      }),
    )
    api.streamRunTask.mockReturnValue(stream.promise)

    const router = makeRouter()
    await router.push('/approvals/task_waiting')
    await router.isReady()

    const wrapper = mount(ApprovalView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()

    await wrapper.get('.approval-reason-input').setValue('too risky')
    await wrapper.get('[data-approval-action="reject"]').trigger('click')
    await flushPromises()

    expect(api.decideTaskApproval).toHaveBeenCalledWith('task_waiting', 'approval_1', {
      decision: 'reject',
      reason: 'too risky',
    })
    expect(wrapper.text()).toContain('Rejected')
    expect(wrapper.text()).toContain('too risky')

    stream.reject(new Error('Task event stream aborted'))
  })

  it('ignores stale load results after navigating to another task', async () => {
    const firstTask = createDeferred<any>()
    const firstApprovals = createDeferred<any[]>()
    const secondTask = createDeferred<any>()
    const secondApprovals = createDeferred<any[]>()

    api.fetchTaskDetails.mockImplementation((taskId: string) => {
      if (taskId === 'task_old') return firstTask.promise
      return secondTask.promise
    })
    api.fetchTaskApprovals.mockImplementation((taskId: string) => {
      if (taskId === 'task_old') return firstApprovals.promise
      return secondApprovals.promise
    })
    api.streamRunTask.mockResolvedValue({ conversation_id: 'conv_new' })

    const router = makeRouter()
    await router.push('/approvals/task_old')
    await router.isReady()

    const wrapper = mount(ApprovalView, {
      global: {
        plugins: [router],
      },
    })

    await router.push('/approvals/task_new')

    secondTask.resolve({
      id: 'task_new',
      status: 'waiting',
      suspend_reason: 'waiting_for_tool_approval',
      input: { conversation_id: 'conv_new' },
      created_by: 'alice',
      created_at: '',
      updated_at: '',
      task_type: 'agent.run',
    })
    secondApprovals.resolve([
      buildPendingApproval({ id: 'approval_new', task_id: 'task_new', conversation_id: 'conv_new', tool_name: 'delete_file' }),
    ])
    await flushPromises()

    firstTask.resolve({
      id: 'task_old',
      status: 'waiting',
      suspend_reason: 'waiting_for_tool_approval',
      input: { conversation_id: 'conv_old' },
      created_by: 'alice',
      created_at: '',
      updated_at: '',
      task_type: 'agent.run',
    })
    firstApprovals.resolve([
      buildPendingApproval({ id: 'approval_old', task_id: 'task_old', conversation_id: 'conv_old', tool_name: 'bash' }),
    ])
    await flushPromises()

    expect(wrapper.text()).toContain('task_new')
    expect(wrapper.text()).toContain('delete_file')
    expect(wrapper.text()).not.toContain('approval_old')
    expect(wrapper.text()).not.toContain('conv_old')
    expect(wrapper.text()).not.toContain('bash')
  })

  it('ignores stale decision results after navigating to another task', async () => {
    const stream = createDeferred<{ conversation_id: string }>()
    const decision = createDeferred<any>()

    api.fetchTaskDetails.mockImplementation(async (taskId: string) => ({
      id: taskId,
      status: 'waiting',
      suspend_reason: 'waiting_for_tool_approval',
      input: { conversation_id: taskId === 'task_waiting' ? 'conv_waiting' : 'conv_other' },
      created_by: 'alice',
      created_at: '',
      updated_at: '',
      task_type: 'agent.run',
    }))
    api.fetchTaskApprovals.mockImplementation(async (taskId: string) => [
      buildPendingApproval({
        id: taskId === 'task_waiting' ? 'approval_1' : 'approval_other',
        task_id: taskId,
        conversation_id: taskId === 'task_waiting' ? 'conv_waiting' : 'conv_other',
        tool_name: taskId === 'task_waiting' ? 'bash' : 'delete_file',
      }),
    ])
    api.decideTaskApproval.mockReturnValue(decision.promise)
    api.streamRunTask.mockReturnValue(stream.promise)

    const router = makeRouter()
    await router.push('/approvals/task_waiting')
    await router.isReady()

    const wrapper = mount(ApprovalView, {
      global: {
        plugins: [router],
      },
    })

    await flushPromises()
    await wrapper.get('[data-approval-action="approve"]').trigger('click')
    await router.push('/approvals/task_other')
    await flushPromises()

    decision.resolve(
      buildPendingApproval({
        id: 'approval_1',
        task_id: 'task_waiting',
        conversation_id: 'conv_waiting',
        status: 'approved',
        decision: 'approve',
        decision_reason: 'old result',
        decision_by: 'alice',
      }),
    )
    await flushPromises()

    expect(wrapper.text()).toContain('task_other')
    expect(wrapper.text()).toContain('delete_file')
    expect(wrapper.text()).not.toContain('old result')

    stream.reject(new Error('Task event stream aborted'))
  })
})
