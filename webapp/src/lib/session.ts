import { normalizeAuthUser, requestJSON } from './api'
import type { AuthUser, RegistrationResult, SessionUser, UserRole } from '../types/api'

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
    const parsed = JSON.parse(value) as Partial<SessionUser> & Record<string, unknown>
    const username = typeof parsed.username === 'string' ? parsed.username.trim() : ''
    if (!username) {
      return null
    }

    return normalizeAuthUser(parsed)
  } catch {
    return normalizeAuthUser({
      username: value,
      role: 'user',
    })
  }
}

function setSessionUser(session: SessionUser | null) {
  const username = session?.username.trim() ?? ''
  if (!username) {
    clearSession()
    return null
  }

  const normalized = normalizeAuthUser({
    ...session,
    username,
    role: normalizeSessionRole(session?.role),
  })

  localStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(normalized))
  return normalized
}

async function requestAuth<T>(path: string, init?: RequestInit) {
  return requestJSON<T>('/api/v1/auth', path, init)
}

export function saveSession(name: string) {
  return setSessionUser(normalizeAuthUser({ username: name, role: 'user' }))
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
  setSessionUser(user)
  return user
}

export async function register(username: string, email: string, password: string, confirmPassword: string): Promise<RegistrationResult> {
  const user = normalizeAuthUser(await requestAuth<AuthUser>('/register', {
    method: 'POST',
    body: JSON.stringify({
      username,
      email,
      password,
      confirm_password: confirmPassword,
    }),
  }))

  const verificationRequired = user.required_actions.includes('verify_email') || user.status === 'pending_email_verification'
  if (verificationRequired) {
    clearSession()
    return { user, verification_required: true }
  }

  return { user: await login(username, password), verification_required: false }
}

export async function verifyRegistrationEmail(userId: number, email: string, code: string) {
  return normalizeAuthUser(await requestAuth<AuthUser>('/email-verification/verify', {
    method: 'POST',
    body: JSON.stringify({
      user_id: userId,
      email,
      purpose: 'registration',
      code,
    }),
  }))
}

export async function syncSession(force = false) {
  if (!force && hasActiveSession()) {
    return getSessionUser()
  }

  try {
    const user = normalizeAuthUser(await requestAuth<AuthUser>('/me', {
      method: 'GET',
    }))
    return setSessionUser(user)
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
