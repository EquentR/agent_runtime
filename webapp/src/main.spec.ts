import { beforeEach, describe, expect, it, vi } from 'vitest'

const harness = vi.hoisted(() => ({
  createApp: vi.fn(),
  mount: vi.fn(),
  syncThemeFromStorage: vi.fn(),
  use: vi.fn(),
}))

vi.mock('vue', () => ({
  createApp: harness.createApp,
}))

vi.mock('element-plus', () => ({
  default: { name: 'ElementPlusMock' },
}))

vi.mock('./App.vue', () => ({
  default: { name: 'AppMock' },
}))

vi.mock('./router', () => ({
  router: { name: 'routerMock' },
}))

vi.mock('./lib/theme', () => ({
  syncThemeFromStorage: harness.syncThemeFromStorage,
}))

describe('app startup', () => {
  beforeEach(() => {
    vi.resetModules()
    harness.createApp.mockReset()
    harness.mount.mockReset()
    harness.syncThemeFromStorage.mockReset()
    harness.use.mockReset()
  })

  it('restores the persisted theme before mounting the app', async () => {
    const calls: string[] = []
    const app = {
      mount: harness.mount,
      use: harness.use,
    }
    harness.syncThemeFromStorage.mockImplementation(() => {
      calls.push('sync-theme')
    })
    harness.createApp.mockImplementation(() => {
      calls.push('create-app')
      return app
    })
    harness.use.mockImplementation(() => {
      calls.push('use-plugin')
      return app
    })
    harness.mount.mockImplementation(() => {
      calls.push('mount')
      return app
    })

    await import('./main')

    expect(harness.syncThemeFromStorage).toHaveBeenCalledOnce()
    expect(harness.mount).toHaveBeenCalledWith('#app')
    expect(calls[0]).toBe('sync-theme')
    expect(calls.at(-1)).toBe('mount')
  })
})
