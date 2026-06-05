import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'

import OpenSourceLicensesPanel from './OpenSourceLicensesPanel.vue'

describe('OpenSourceLicensesPanel', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('opens a dialog that shows the project source first and labels all direct dependencies as Components', async () => {
    const wrapper = mount(OpenSourceLicensesPanel, {
      attachTo: document.body,
    })

    await wrapper.get('[data-open-source-licenses-toggle]').trigger('click')
    await flushPromises()

    const dialog = document.body.querySelector<HTMLElement>('[data-open-source-licenses-dialog]')
    expect(dialog).not.toBeNull()
    expect(dialog?.textContent).toContain('开放源代码许可')
    expect(dialog?.textContent).toContain('本应用基于如下同名项目开发')
    expect(dialog?.textContent).toContain('Components')
    expect(dialog?.textContent).not.toContain('全部直接依赖')
    expect(dialog?.textContent).not.toContain('前端直接依赖')
    expect(dialog?.textContent).not.toContain('后端直接依赖')

    const links = Array.from(dialog?.querySelectorAll('a') ?? []).map((anchor) => (anchor as HTMLAnchorElement).getAttribute('href'))
    expect(links[0]).toBe('https://github.com/EquentR/agent_runtime')
    expect(links).toContain('https://github.com/vuejs/core')
    expect(links).toContain('https://github.com/googleapis/go-genai')
  })

  it('closes the dialog when the dismiss button is clicked', async () => {
    const wrapper = mount(OpenSourceLicensesPanel, {
      attachTo: document.body,
    })

    await wrapper.get('[data-open-source-licenses-toggle]').trigger('click')
    await flushPromises()

    expect(document.body.querySelector('[data-open-source-licenses-dialog]')).not.toBeNull()

    const closeButton = document.body.querySelector<HTMLButtonElement>('[data-open-source-licenses-close]')
    expect(closeButton).not.toBeNull()
    closeButton?.click()
    await flushPromises()

    expect(document.body.querySelector('[data-open-source-licenses-dialog]')).toBeNull()
  })
})
