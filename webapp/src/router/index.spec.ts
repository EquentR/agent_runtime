import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { beforeEach, describe, expect, it, vi } from 'vitest'

const session = vi.hoisted(() => ({
  hasActiveSession: vi.fn(),
  syncSession: vi.fn(),
}))

vi.mock('../lib/session', () => session)

describe('app router session guard', () => {
  beforeEach(() => {
    vi.resetModules()
    session.hasActiveSession.mockReset()
    session.syncSession.mockReset()
    document.title = 'webapp'
  })

  it('uses a Chinese default title in the HTML shell', () => {
    const html = readFileSync(resolve(process.cwd(), 'index.html'), 'utf8')

    expect(html).toContain('<title>智能体工作台 - Agent Runtime</title>')
  })

  it('updates the browser title when the route changes', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue({ username: 'alice', role: 'admin' })

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/chat')
    await router.isReady()

    expect(document.title).toBe('聊天 - Agent Runtime')

    await router.push('/admin/audit')

    expect(document.title).toBe('审计会话 - Agent Runtime')

    await router.push('/admin/prompts')

    expect(document.title).toBe('提示词管理 - Agent Runtime')

    await router.push('/approvals/task_1')

    expect(document.title).toBe('审批管理 - Agent Runtime')
  })

  it('forces backend session validation before entering protected pages', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue({ username: 'alice', role: 'user' })

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/chat')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/chat')
  })

  it('redirects protected pages to login when backend session validation fails', async () => {
    session.hasActiveSession.mockReturnValue(false)
    session.syncSession.mockResolvedValue(null)

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/chat')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('redirects unauthenticated users away from the approval route', async () => {
    session.hasActiveSession.mockReturnValue(false)
    session.syncSession.mockResolvedValue(null)

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/approvals/task_1')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('allows admin users to enter the admin audit route', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue({ username: 'alice', role: 'admin' })

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/admin/audit')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/admin/audit')
  })

  it('redirects non-admin users away from the admin audit route', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue({ username: 'alice', role: 'user' })

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/admin/audit')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/chat')
  })

  it('allows signed-in users to enter the approval route', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue({ username: 'alice', role: 'user' })

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/approvals/task_1')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/approvals/task_1')
    expect(router.currentRoute.value.meta.requiresSession).toBe(true)
    expect(router.currentRoute.value.meta.requiresAdmin).toBeUndefined()
  })

  it('allows admin users to enter the admin prompts route', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue({ username: 'alice', role: 'admin' })

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/admin/prompts')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/admin/prompts')
    expect(router.currentRoute.value.meta.requiresSession).toBe(true)
    expect(router.currentRoute.value.meta.requiresAdmin).toBe(true)
  })

  it('redirects non-admin users away from the admin prompts route', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue({ username: 'alice', role: 'user' })

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/admin/prompts')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/chat')
  })
})
