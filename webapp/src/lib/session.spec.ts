import { beforeEach, describe, expect, it } from 'vitest'

import { clearSession, getSessionName, hasActiveSession, saveSession, SESSION_STORAGE_KEY } from './session'

describe('session helpers', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('persists a trimmed username', () => {
    saveSession('  demo-user  ')

    expect(localStorage.getItem(SESSION_STORAGE_KEY)).toBe('demo-user')
    expect(getSessionName()).toBe('demo-user')
    expect(hasActiveSession()).toBe(true)
  })

  it('clears the saved username', () => {
    localStorage.setItem(SESSION_STORAGE_KEY, 'demo-user')

    clearSession()

    expect(getSessionName()).toBe('')
    expect(hasActiveSession()).toBe(false)
  })
})
