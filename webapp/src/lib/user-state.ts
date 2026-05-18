import type { AuthRequiredAction, AuthUser } from '../types/api'

export function getRequiredActions(user: AuthUser | null | undefined): AuthRequiredAction[] {
  if (!user || user.status === 'disabled') {
    return []
  }
  if (Array.isArray(user.required_actions) && user.required_actions.length > 0) {
    return user.required_actions
  }

  const actions: AuthRequiredAction[] = []
  if (user.status === 'pending_email_verification') {
    actions.push('verify_email')
  }
  if (user.status === 'needs_email_binding' || !user.email.trim()) {
    actions.push('bind_email')
  }
  if (user.force_password_change) {
    actions.push('change_password')
  }
  return actions
}

export function getRequiredActionProfileTarget(user: AuthUser | null | undefined) {
  const actions = getRequiredActions(user)
  if (actions.length === 0) {
    return null
  }

  if (actions.includes('change_password')) {
    return { path: '/profile', query: { section: 'security' } }
  }

  return { path: '/profile', query: { section: 'email' } }
}
