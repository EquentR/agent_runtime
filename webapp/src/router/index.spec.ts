import { beforeEach, describe, expect, it } from 'vitest'

import { SESSION_STORAGE_KEY } from '../lib/session'
import { createAppRouter } from './index'

describe('createAppRouter', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('redirects anonymous users from chat to login', async () => {
    const router = createAppRouter(true)

    await router.push('/chat')
    await router.isReady()

    expect(router.currentRoute.value.fullPath).toBe('/login')
  })

  it('allows authenticated users into chat', async () => {
    localStorage.setItem(SESSION_STORAGE_KEY, 'demo-user')
    const router = createAppRouter(true)

    await router.push('/chat')
    await router.isReady()

    expect(router.currentRoute.value.fullPath).toBe('/chat')
  })
})
