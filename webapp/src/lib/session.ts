import { normalizeAuthUser, unwrapEnvelope } from './api'
import type { ApiEnvelope, AuthUser, SessionUser, UserRole } from '../types/api'

export const SESSION_STORAGE_KEY = 'agent-runtime.user'

function normalizeSessionRole(value: unknown): UserRole {
  return value === 'admin' ? 'admin' : 'user'
}

function parseSessionValue(raw: string | null): SessionUser | null {
  const value = raw?.trim() ?? ''
  if (!value) {
    return null
  }

  try {
    const parsed = JSON.parse(value) as Partial<SessionUser>
    const username = typeof parsed.username === 'string' ? parsed.username.trim() : ''
    if (!username) {
      return null
    }

    return {
      username,
      role: normalizeSessionRole(parsed.role),
    }
  } catch {
    return {
      username: value,
      role: 'user',
    }
  }
}

function setSessionUser(session: SessionUser | null) {
  const username = session?.username.trim() ?? ''
  if (!username) {
    clearSession()
    return null
  }

  const normalized = {
    username,
    role: normalizeSessionRole(session?.role),
  } satisfies SessionUser

  localStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(normalized))
  return normalized
}

async function requestAuth<T>(path: string, init?: RequestInit) {
  const response = await fetch(`/api/v1/auth${path}`, {
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
    ...init,
  })

  const payload = (await response.json()) as ApiEnvelope<T>
  return unwrapEnvelope(payload)
}

export function saveSession(name: string) {
  return setSessionUser({ username: name, role: 'user' })
}

export function clearSession() {
  localStorage.removeItem(SESSION_STORAGE_KEY)
}

export function getSessionUser() {
  return parseSessionValue(localStorage.getItem(SESSION_STORAGE_KEY))
}

export function getSessionName() {
  return getSessionUser()?.username ?? ''
}

export function getSessionRole() {
  return getSessionUser()?.role ?? 'user'
}

export function hasActiveSession() {
  return getSessionUser() !== null
}

export async function login(username: string, password: string) {
  const user = normalizeAuthUser(await requestAuth<AuthUser>('/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  }))
  setSessionUser({ username: user.username, role: user.role })
  return user
}

export async function register(username: string, password: string, confirmPassword: string) {
  await requestAuth<AuthUser>('/register', {
    method: 'POST',
    body: JSON.stringify({
      username,
      password,
      confirm_password: confirmPassword,
    }),
  })

  return login(username, password)
}

export async function syncSession(force = false) {
  if (!force && hasActiveSession()) {
    return getSessionUser()
  }

  try {
    const user = normalizeAuthUser(await requestAuth<AuthUser>('/me', {
      method: 'GET',
    }))
    return setSessionUser({ username: user.username, role: user.role })
  } catch {
    clearSession()
    return null
  }
}

export async function logout() {
  try {
    await requestAuth<{ logged_out: boolean }>('/logout', {
      method: 'POST',
      body: JSON.stringify({}),
    })
  } finally {
    clearSession()
  }
}
