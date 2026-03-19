import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'

import LoginView from './LoginView.vue'

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
  it('renders compact Chinese login and register entry points', async () => {
    const router = makeRouter()
    await router.push('/login')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: {
        plugins: [router],
      },
    })

    await wrapper.findAll('.auth-switch-button')[1].trigger('click')

    expect(wrapper.text()).toContain('登录')
    expect(wrapper.text()).toContain('注册')
    expect(wrapper.text()).toContain('用户名')
    expect(wrapper.text()).toContain('密码')
    expect(wrapper.findAll('input[type="password"]').length).toBeGreaterThanOrEqual(2)
  })
})
