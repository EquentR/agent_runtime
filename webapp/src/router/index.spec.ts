import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { beforeEach, describe, expect, it, vi } from 'vitest'

const session = vi.hoisted(() => ({
  hasActiveSession: vi.fn(),
  syncSession: vi.fn(),
}))

vi.mock('../lib/session', () => session)

describe('app router session guard', () => {
  const activeUser = {
    id: 1,
    username: 'alice',
    email: 'alice@example.com',
    display_name: 'Alice',
    role: 'user',
    status: 'active',
    email_verified: true,
    force_password_change: false,
    required_actions: [],
  }

  const adminUser = {
    ...activeUser,
    role: 'admin',
  }

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
    session.syncSession.mockResolvedValue(adminUser)

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/chat')
    await router.isReady()

    expect(document.title).toBe('聊天 - Agent Runtime')

    await router.push('/admin/audit')

    expect(document.title).toBe('审计会话 - Agent Runtime')

    await router.push('/admin/prompts')

    expect(document.title).toBe('提示词管理 - Agent Runtime')

    await router.push('/admin/audit')

    expect(document.title).toBe('审计会话 - Agent Runtime')
  })

  it('forces backend session validation before entering protected pages', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue(activeUser)

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

  it('redirects disabled sessions to login', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue({
      ...activeUser,
      status: 'disabled',
    })

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/chat')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('redirects unauthenticated users away from the admin audit route', async () => {
    session.hasActiveSession.mockReturnValue(false)
    session.syncSession.mockResolvedValue(null)

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/admin/audit')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('allows admin users to enter the admin audit route', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue(adminUser)

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/admin/audit')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/admin/audit')
  })

  it('redirects non-admin users away from the admin audit route', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue(activeUser)

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/admin/audit')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/chat')
  })

  it('does not register a standalone approval route', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue(activeUser)

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/approvals')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith()
    expect(router.currentRoute.value.matched).toHaveLength(0)
  })

  it('allows admin users to enter the admin prompts route', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue(adminUser)

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
    session.syncSession.mockResolvedValue(activeUser)

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/admin/prompts')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/chat')
  })

  it('uses the real profile view instead of the temporary placeholder', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue(activeUser)

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    const route = router.resolve('/profile')
    const component = route.matched[0]?.components?.default as { template?: string } | undefined

    expect(component).toBeTruthy()
    expect(component?.template).toBeUndefined()
  })

  it('redirects non-admin users away from public admin backoffice routes', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue(activeUser)

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    for (const path of ['/admin/users', '/admin/models', '/admin/settings', '/admin/audit-events']) {
      await router.push(path)
      await router.isReady()

      expect(session.syncSession).toHaveBeenCalledWith(true)
      expect(router.currentRoute.value.path).toBe('/chat')
    }
  })

  it('routes force password change users to profile security', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue({
      ...activeUser,
      force_password_change: true,
      required_actions: ['change_password'],
    })

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/chat')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/profile')
    expect(router.currentRoute.value.query).toEqual({ section: 'security' })
  })

  it('routes needs email binding users to profile email', async () => {
    session.hasActiveSession.mockReturnValue(true)
    session.syncSession.mockResolvedValue({
      ...activeUser,
      email: '',
      status: 'needs_email_binding',
      email_verified: false,
      required_actions: ['bind_email'],
    })

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/chat')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/profile')
    expect(router.currentRoute.value.query).toEqual({ section: 'email' })
  })
})
