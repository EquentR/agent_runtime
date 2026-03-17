export const SESSION_STORAGE_KEY = 'agent-runtime.user'

export function saveSession(name: string) {
  const value = name.trim()
  if (!value) {
    clearSession()
    return
  }

  localStorage.setItem(SESSION_STORAGE_KEY, value)
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
