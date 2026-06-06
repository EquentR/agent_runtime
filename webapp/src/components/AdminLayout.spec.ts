import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { mount } from '@vue/test-utils'
import type { Component } from 'vue'
import { describe, expect, it } from 'vitest'

const appStyles = readFileSync(resolve(process.cwd(), 'src/style.css'), 'utf8')

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

  it('frames ordinary admin pages while internal-scroll pages own their shell', () => {
    expect(appStyles).not.toMatch(/\.admin-layout-main,\s*\.admin-section\s*\{/)
    const mainRule = appStyles.match(/\.admin-layout-main\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? ''
    expect(mainRule).toContain('background:')
    expect(mainRule).toContain('border:')
    expect(mainRule).toContain('box-shadow:')
    expect(mainRule).toContain('padding: 0.9rem;')

    const managedPageRule = appStyles.match(
      /\.admin-layout-main:has\(\.admin-audit-shell\),\s*\.admin-layout-main:has\(\.admin-prompt-shell\)\s*\{(?<body>[^}]*)\}/,
    )?.groups?.body ?? ''
    expect(managedPageRule).toContain('background: transparent;')
    expect(managedPageRule).toContain('border: 0;')
    expect(managedPageRule).toContain('box-shadow: none;')
    expect(managedPageRule).toContain('padding: 0;')

    const managedWrapRule = appStyles.match(
      /\.admin-layout-main:has\(\.admin-audit-shell\) \.admin-layout-main-scrollbar > \.el-scrollbar__wrap,\s*\.admin-layout-main:has\(\.admin-prompt-shell\) \.admin-layout-main-scrollbar > \.el-scrollbar__wrap\s*\{(?<body>[^}]*)\}/,
    )?.groups?.body ?? ''
    expect(managedWrapRule).toContain('overflow: hidden;')
  })
})
