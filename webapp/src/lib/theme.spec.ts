import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { applyTheme, getNextThemeMode, getStoredTheme, setStoredTheme, syncThemeFromStorage, THEME_STORAGE_KEY } from './theme'

describe('theme', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('theme-teal')
    document.documentElement.classList.remove('theme-teal-dark')
  })

  afterEach(() => {
    document.documentElement.classList.remove('theme-teal')
    document.documentElement.classList.remove('theme-teal-dark')
  })

  it('restores the saved teal-dark theme on startup', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'teal-dark')

    syncThemeFromStorage()

    expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
    expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(true)
    expect(getStoredTheme()).toBe('teal-dark')
  })

  it('cycles through default, teal, teal-dark, and back to default', () => {
    expect(getNextThemeMode('default')).toBe('teal')
    expect(getNextThemeMode('teal')).toBe('teal-dark')
    expect(getNextThemeMode('teal-dark')).toBe('default')
  })

  it('restores the saved theme on startup', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'teal')

    syncThemeFromStorage()

    expect(document.documentElement.classList.contains('theme-teal')).toBe(true)
    expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(false)
    expect(getStoredTheme()).toBe('teal')
  })

  it('clears the teal class when the stored theme is default', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'default')
    document.documentElement.classList.add('theme-teal')
    document.documentElement.classList.add('theme-teal-dark')

    syncThemeFromStorage()

    expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
    expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(false)
    expect(getStoredTheme()).toBe('default')
  })

  it('updates the active theme and persists it when toggled', () => {
    setStoredTheme('teal')

    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('teal')
    expect(document.documentElement.classList.contains('theme-teal')).toBe(true)
    expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(false)

    applyTheme('teal-dark')

    expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
    expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(true)
  })
})
