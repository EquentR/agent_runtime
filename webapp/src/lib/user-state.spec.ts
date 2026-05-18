import { describe, expect, it } from 'vitest'

import type { AuthUser } from '../types/api'
import { getRequiredActions, getRequiredActionProfileTarget } from './user-state'

describe('user state helpers', () => {
  it('prefers explicit required actions from backend payloads', () => {
    expect(getRequiredActions(makeUser({ required_actions: ['change_password'] }))).toEqual(['change_password'])
  })

  it('derives profile email target for users that must bind or verify email', () => {
    expect(
      getRequiredActionProfileTarget(makeUser({
        status: 'needs_email_binding',
        email: '',
        email_verified: false,
        required_actions: ['bind_email'],
      })),
    ).toEqual({ path: '/profile', query: { section: 'email' } })
  })

  it('derives profile security target for users that must change password', () => {
    expect(
      getRequiredActionProfileTarget(makeUser({
        force_password_change: true,
        required_actions: ['change_password'],
      })),
    ).toEqual({ path: '/profile', query: { section: 'security' } })
  })
})

function makeUser(overrides: Partial<AuthUser> = {}): AuthUser {
  return {
    id: 1,
    username: 'alice',
    email: 'alice@example.com',
    display_name: 'Alice',
    role: 'user',
    status: 'active',
    email_verified: true,
    force_password_change: false,
    required_actions: [],
    ...overrides,
  }
}
