import { unwrapEnvelope } from './api'
import type { ApiEnvelope } from '../types/api'

export const SESSION_STORAGE_KEY = 'agent-runtime.user'

interface AuthUser {
  id: number
  username: string
}

function setSessionName(name: string) {
  const value = name.trim()
  if (!value) {
    clearSession()
    return ''
  }

  localStorage.setItem(SESSION_STORAGE_KEY, value)
  return value
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
  return setSessionName(name)
}

export function clearSession() {
  localStorage.removeItem(SESSION_STORAGE_KEY)
}

export function getSessionName() {
  return localStorage.getItem(SESSION_STORAGE_KEY)?.trim() ?? ''
}

export function hasActiveSession() {
  return getSessionName().length > 0
}

export async function login(username: string, password: string) {
  const user = await requestAuth<AuthUser>('/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
  setSessionName(user.username)
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
    return getSessionName()
  }

  try {
    const user = await requestAuth<AuthUser>('/me', {
      method: 'GET',
    })
    return setSessionName(user.username)
  } catch {
    clearSession()
    return ''
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
