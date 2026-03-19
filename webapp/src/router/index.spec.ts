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
    session.syncSession.mockResolvedValue('alice')

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/chat')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/chat')
  })

  it('redirects protected pages to login when backend session validation fails', async () => {
    session.hasActiveSession.mockReturnValue(false)
    session.syncSession.mockResolvedValue('')

    const { createAppRouter } = await import('./index')
    const router = createAppRouter(true)

    await router.push('/chat')
    await router.isReady()

    expect(session.syncSession).toHaveBeenCalledWith(true)
    expect(router.currentRoute.value.path).toBe('/login')
  })
})
