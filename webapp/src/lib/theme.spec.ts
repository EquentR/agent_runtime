import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import {
  applyTheme,
  getNextThemeMode,
  getStoredTheme,
  setStoredTheme,
  syncThemeFromStorage,
  THEME_STORAGE_KEY,
} from './theme'

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

  it('restores the saved theme on startup', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'teal')

    syncThemeFromStorage()

    expect(document.documentElement.classList.contains('theme-teal')).toBe(true)
    expect(getStoredTheme()).toBe('teal')
  })

  it('restores teal-dark from storage', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'teal-dark')

    syncThemeFromStorage()

    expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
    expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(true)
    expect(getStoredTheme()).toBe('teal-dark')
  })

  it('clears the teal class when the stored theme is default', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'default')
    document.documentElement.classList.add('theme-teal')

    syncThemeFromStorage()

    expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
    expect(getStoredTheme()).toBe('default')
  })

  it('cycles themes in order', () => {
    expect(getNextThemeMode('default')).toBe('teal')
    expect(getNextThemeMode('teal')).toBe('teal-dark')
    expect(getNextThemeMode('teal-dark')).toBe('default')
  })

  it('updates the active theme and persists it when toggled', () => {
    setStoredTheme('teal')

    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('teal')
    expect(document.documentElement.classList.contains('theme-teal')).toBe(true)

    applyTheme('default')

    expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
  })

  it('applies teal-dark without leaving teal behind', () => {
    applyTheme('teal')
    applyTheme('teal-dark')

    expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
    expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(true)
  })
})
