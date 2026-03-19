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
} from './session'

describe('session helpers', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
    vi.stubGlobal('fetch', vi.fn())
  })

  it('logs in through the backend and caches the username locally', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({
        ok: true,
        code: 200,
        message: 'OK',
        data: { id: 1, username: 'alice' },
        time: '2026-03-19 10:00:00',
      }),
    } as Response)

    await login('alice', 'secret-123')

    expect(fetch).toHaveBeenCalledWith('/api/v1/auth/login', expect.objectContaining({
      method: 'POST',
      credentials: 'include',
    }))
    expect(localStorage.getItem(SESSION_STORAGE_KEY)).toBe('alice')
    expect(getSessionName()).toBe('alice')
    expect(hasActiveSession()).toBe(true)
  })

  it('registers with username and two passwords then keeps the session active', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ ok: true, code: 200, message: 'OK', data: { id: 1, username: 'alice' }, time: '' }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ ok: true, code: 200, message: 'OK', data: { id: 1, username: 'alice' }, time: '' }),
      } as Response)

    await register('alice', 'secret-123', 'secret-123')

    expect(fetch).toHaveBeenNthCalledWith(1, '/api/v1/auth/register', expect.objectContaining({ method: 'POST' }))
    expect(fetch).toHaveBeenNthCalledWith(2, '/api/v1/auth/login', expect.objectContaining({ method: 'POST' }))
    expect(getSessionName()).toBe('alice')
  })

  it('syncs an existing cookie session back into local cache', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ ok: true, code: 200, message: 'OK', data: { id: 1, username: 'alice' }, time: '' }),
    } as Response)

    const username = await syncSession()

    expect(fetch).toHaveBeenCalledWith('/api/v1/auth/me', expect.objectContaining({ credentials: 'include' }))
    expect(username).toBe('alice')
    expect(getSessionName()).toBe('alice')
  })

  it('clears stale local cache when forced session validation fails', async () => {
    localStorage.setItem(SESSION_STORAGE_KEY, 'alice')
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ ok: false, code: 401, message: '未登录或会话已失效', data: null, time: '' }),
    } as Response)

    const username = await syncSession(true)

    expect(fetch).toHaveBeenCalledWith('/api/v1/auth/me', expect.objectContaining({ credentials: 'include' }))
    expect(username).toBe('')
    expect(getSessionName()).toBe('')
    expect(hasActiveSession()).toBe(false)
  })

  it('clears local cache after logout', async () => {
    localStorage.setItem(SESSION_STORAGE_KEY, 'alice')
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
    localStorage.setItem(SESSION_STORAGE_KEY, 'alice')

    clearSession()

    expect(getSessionName()).toBe('')
    expect(hasActiveSession()).toBe(false)
  })
})
