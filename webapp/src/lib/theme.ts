export const THEME_STORAGE_KEY = 'app-theme'

export type ThemeMode = 'default' | 'teal' | 'teal-dark'

function normalizeThemeMode(value: string | null | undefined): ThemeMode {
  return value === 'teal' || value === 'teal-dark' ? value : 'default'
}

export function getNextThemeMode(theme: ThemeMode): ThemeMode {
  if (theme === 'default') {
    return 'teal'
  }

  if (theme === 'teal') {
    return 'teal-dark'
  }

  return 'default'
}

export function getStoredTheme(): ThemeMode {
  try {
    return normalizeThemeMode(localStorage.getItem(THEME_STORAGE_KEY))
  } catch {
    return 'default'
  }
}

export function applyTheme(theme: ThemeMode) {
  document.documentElement.classList.toggle('theme-teal', theme === 'teal')
  document.documentElement.classList.toggle('theme-teal-dark', theme === 'teal-dark')
}

export function syncThemeFromStorage() {
  applyTheme(getStoredTheme())
}

export function setStoredTheme(theme: ThemeMode) {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, theme)
  } catch {
    // Ignore storage failures and still keep the active document in sync.
  }
  applyTheme(theme)
}
