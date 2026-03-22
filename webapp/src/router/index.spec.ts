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
})
