import { mount } from '@vue/test-utils'
import type { Component } from 'vue'
import { describe, expect, it } from 'vitest'

async function loadAdminLayout() {
  const modules = import.meta.glob('./AdminLayout.vue')
  const loader = modules['./AdminLayout.vue']
  expect(loader).toBeTypeOf('function')

  const module = (await loader()) as { default: Component }
  return module.default
}

describe('AdminLayout', () => {
  it('renders business-domain navigation', async () => {
    const AdminLayout = await loadAdminLayout()

    const wrapper = mount(AdminLayout, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a data-admin-nav-link :href="typeof to === \'string\' ? to : to.path"><slot /></a>',
          },
          RouterView: {
            template: '<main data-admin-layout-outlet />',
          },
        },
      },
    })

    const text = wrapper.text()
    for (const label of ['仪表盘', '用户管理', '模型管理', '系统设置', '提示词管理', '审计会话', '后台操作审计']) {
      expect(text).toContain(label)
    }

    const hrefs = wrapper.findAll('[data-admin-nav-link]').map((link) => link.attributes('href'))
    expect(hrefs).toEqual(expect.arrayContaining([
      '/chat',
      '/admin/users',
      '/admin/models',
      '/admin/settings',
      '/admin/prompts',
      '/admin/audit',
      '/admin/audit-events',
    ]))
    expect(wrapper.find('[data-admin-layout-outlet]').exists()).toBe(true)
  })
})
