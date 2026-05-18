import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  clearSession,
  getSessionName,
  hasActiveSession,
  login,
  logout,
  register,
  SESSION_STORAGE_KEY,
  syncSession,
  verifyRegistrationEmail,
} from './session'

describe('session helpers', () => {
  const activeAdminUser = {
    id: 1,
    username: 'alice',
    email: 'alice@example.com',
    display_name: 'Alice',
    role: 'admin' as const,
    status: 'active' as const,
    email_verified: true,
    force_password_change: false,
    required_actions: [],
  }

  const pendingUser = {
    id: 2,
    username: 'bob',
    email: 'bob@example.com',
    display_name: 'Bob',
    role: 'user' as const,
    status: 'pending_email_verification' as const,
    email_verified: false,
    force_password_change: false,
    required_actions: ['verify_email' as const],
  }

  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
    vi.stubGlobal('fetch', vi.fn())
  })

  it('logs in through the backend and caches username plus role locally', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({
        ok: true,
        code: 200,
        message: 'OK',
        data: activeAdminUser,
        time: '2026-03-19 10:00:00',
      }),
    } as Response)

    const user = await login('alice', 'secret-123')

    expect(fetch).toHaveBeenCalledWith('/api/v1/auth/login', expect.objectContaining({
      method: 'POST',
      credentials: 'include',
    }))
    expect(user).toEqual(activeAdminUser)
    expect(JSON.parse(localStorage.getItem(SESSION_STORAGE_KEY) ?? '{}')).toEqual(activeAdminUser)
    expect(getSessionName()).toBe('alice')
    expect(hasActiveSession()).toBe(true)
  })

  it('sends a turnstile token with protected login requests', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({
        ok: true,
        code: 200,
        message: 'OK',
        data: activeAdminUser,
        time: '2026-03-19 10:00:00',
      }),
    } as Response)

    await login('alice', 'secret-123', 'login-token')

    expect(fetch).toHaveBeenCalledWith('/api/v1/auth/login', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({
        username: 'alice',
        password: 'secret-123',
        turnstile_token: 'login-token',
      }),
    }))
  })

  it('registers a verified first admin with email and then keeps the full auth user active', async () => {
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: true,
      json: async () => ({ ok: true, code: 200, message: 'OK', data: activeAdminUser, time: '' }),
    } as Response)

    const result = await register('alice', 'alice@example.com', 'secret-123', 'secret-123')

    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledWith('/api/v1/auth/register', expect.objectContaining({ method: 'POST' }))
    expect(result).toEqual({ user: activeAdminUser, verification_required: false })
    expect(JSON.parse(localStorage.getItem(SESSION_STORAGE_KEY) ?? '{}')).toEqual(activeAdminUser)
    expect(getSessionName()).toBe('alice')
  })

  it('keeps pending registration out of local session until email verification finishes', async () => {
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: true,
      json: async () => ({ ok: true, code: 200, message: 'OK', data: pendingUser, time: '' }),
    } as Response)

    const result = await register('bob', 'bob@example.com', 'secret-123', 'secret-123')

    expect(fetch).toHaveBeenCalledTimes(1)
    expect(result).toEqual({ user: pendingUser, verification_required: true })
    expect(localStorage.getItem(SESSION_STORAGE_KEY)).toBeNull()
  })

  it('sends a turnstile token with protected registration requests', async () => {
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: true,
      json: async () => ({ ok: true, code: 200, message: 'OK', data: pendingUser, time: '' }),
    } as Response)

    await register('bob', 'bob@example.com', 'secret-123', 'secret-123', 'register-token')

    expect(fetch).toHaveBeenCalledWith('/api/v1/auth/register', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({
        username: 'bob',
        email: 'bob@example.com',
        password: 'secret-123',
        confirm_password: 'secret-123',
        turnstile_token: 'register-token',
      }),
    }))
  })

  it('verifies a registration email without caching a session cookie-backed user', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ ok: true, code: 200, message: 'OK', data: activeAdminUser, time: '' }),
    } as Response)

    const user = await verifyRegistrationEmail(1, 'alice@example.com', '123456')

    expect(fetch).toHaveBeenCalledWith('/api/v1/auth/email-verification/verify', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({
        user_id: 1,
        email: 'alice@example.com',
        purpose: 'registration',
        code: '123456',
      }),
    }))
    expect(user).toEqual(activeAdminUser)
    expect(localStorage.getItem(SESSION_STORAGE_KEY)).toBeNull()
  })

  it('syncs an existing cookie session back into local cache with username and role', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ ok: true, code: 200, message: 'OK', data: activeAdminUser, time: '' }),
    } as Response)

    const session = await syncSession()

    expect(fetch).toHaveBeenCalledWith('/api/v1/auth/me', expect.objectContaining({ credentials: 'include' }))
    expect(session).toEqual(activeAdminUser)
    expect(JSON.parse(localStorage.getItem(SESSION_STORAGE_KEY) ?? '{}')).toEqual(activeAdminUser)
    expect(getSessionName()).toBe('alice')
  })

  it('clears a disabled cookie session instead of caching it as active', async () => {
    localStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(activeAdminUser))
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({
        ok: true,
        code: 200,
        message: 'OK',
        data: { ...activeAdminUser, status: 'disabled' },
        time: '',
      }),
    } as Response)

    const session = await syncSession(true)

    expect(session).toBeNull()
    expect(getSessionName()).toBe('')
    expect(hasActiveSession()).toBe(false)
  })

  it('clears stale local cache when forced session validation fails', async () => {
    localStorage.setItem(SESSION_STORAGE_KEY, '{"username":"alice","role":"admin"}')
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ ok: false, code: 401, message: '未登录或会话已失效', data: null, time: '' }),
    } as Response)

    const session = await syncSession(true)

    expect(fetch).toHaveBeenCalledWith('/api/v1/auth/me', expect.objectContaining({ credentials: 'include' }))
    expect(session).toBeNull()
    expect(getSessionName()).toBe('')
    expect(hasActiveSession()).toBe(false)
  })

  it('clears local cache after logout', async () => {
    localStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(activeAdminUser))
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ ok: true, code: 200, message: 'OK', data: { logged_out: true }, time: '' }),
    } as Response)

    await logout()

    expect(fetch).toHaveBeenCalledWith('/api/v1/auth/logout', expect.objectContaining({
      method: 'POST',
      credentials: 'include',
    }))
    expect(getSessionName()).toBe('')
    expect(hasActiveSession()).toBe(false)
  })

  it('clearSession removes the cached username immediately', () => {
    localStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(activeAdminUser))

    clearSession()

    expect(getSessionName()).toBe('')
    expect(hasActiveSession()).toBe(false)
  })
})
