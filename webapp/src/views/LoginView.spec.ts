import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import LoginView from './LoginView.vue'

const api = vi.hoisted(() => ({
  fetchPublicRegistrationSettings: vi.fn(),
}))

const session = vi.hoisted(() => ({
  login: vi.fn(),
  register: vi.fn(),
  verifyRegistrationEmail: vi.fn(),
}))

vi.mock('../lib/api', () => api)
vi.mock('../lib/session', () => session)

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: LoginView },
      { path: '/chat', component: { template: '<div>chat</div>' } },
    ],
  })
}

describe('LoginView', () => {
  const activeUser = {
    id: 1,
    username: 'alice',
    email: 'alice@example.com',
    display_name: 'Alice',
    role: 'admin',
    status: 'active',
    email_verified: true,
    force_password_change: false,
    required_actions: [],
  }

  const pendingUser = {
    ...activeUser,
    id: 2,
    username: 'bob',
    email: 'bob@example.com',
    display_name: 'Bob',
    role: 'user',
    status: 'pending_email_verification',
    email_verified: false,
    required_actions: ['verify_email'],
  }

  beforeEach(() => {
    vi.resetAllMocks()
    api.fetchPublicRegistrationSettings.mockResolvedValue({ enabled: true })
    session.login.mockResolvedValue(activeUser)
    session.register.mockResolvedValue({ user: activeUser, verification_required: false })
    session.verifyRegistrationEmail.mockResolvedValue(activeUser)
  })

  it('renders compact Chinese login and register entry points', async () => {
    const router = makeRouter()
    await router.push('/login')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: {
        plugins: [router],
      },
    })
    await flushPromises()

    await wrapper.findAll('.auth-switch-button')[1].trigger('click')

    expect(wrapper.text()).toContain('登录')
    expect(wrapper.text()).toContain('注册')
    expect(wrapper.text()).toContain('用户名')
    expect(wrapper.text()).toContain('密码')
    expect(wrapper.findAll('input[type="password"]').length).toBeGreaterThanOrEqual(2)
  })

  it('hides register tab when public registration is disabled', async () => {
    api.fetchPublicRegistrationSettings.mockResolvedValue({ enabled: false })
    const router = makeRouter()
    await router.push('/login')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: {
        plugins: [router],
      },
    })
    await flushPromises()

    expect(wrapper.findAll('.auth-switch-button')).toHaveLength(1)
    expect(wrapper.text()).toContain('登录')
    expect(wrapper.text()).not.toContain('注册')
  })

  it('submits registration with email and verification flow', async () => {
    session.register.mockResolvedValue({ user: pendingUser, verification_required: true })
    session.verifyRegistrationEmail.mockResolvedValue({ ...pendingUser, status: 'active', email_verified: true, required_actions: [] })
    const router = makeRouter()
    await router.push('/login')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: {
        plugins: [router],
      },
    })
    await flushPromises()

    await wrapper.findAll('.auth-switch-button')[1].trigger('click')
    await wrapper.find('#username').setValue('bob')
    await wrapper.find('#email').setValue('bob@example.com')
    await wrapper.find('#password').setValue('secret-123')
    await wrapper.find('#confirmPassword').setValue('secret-123')
    await wrapper.find('.primary-button').trigger('click')
    await flushPromises()

    expect(session.register).toHaveBeenCalledWith('bob', 'bob@example.com', 'secret-123', 'secret-123')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(wrapper.text()).toContain('邮箱验证码')

    await wrapper.find('#verificationCode').setValue('123456')
    await wrapper.find('.primary-button').trigger('click')
    await flushPromises()

    expect(session.verifyRegistrationEmail).toHaveBeenCalledWith(2, 'bob@example.com', '123456')
    expect(session.login).toHaveBeenCalledWith('bob', 'secret-123')
    expect(router.currentRoute.value.path).toBe('/chat')
  })
})
