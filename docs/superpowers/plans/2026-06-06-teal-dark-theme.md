# Teal Dark Theme Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a three-state theme cycle and a full Teal dark mode that covers the entire webapp experience.

**Architecture:** Keep theme state in `webapp/src/lib/theme.ts`, let the sidebar button advance the cycle through a shared helper, and push color treatment into shared CSS tokens plus a small set of component-scoped surface overrides. That keeps behavior centralized while allowing dialogs, menus, dropdowns, and teleported overlays to inherit the same palette.

**Tech Stack:** Vue 3, TypeScript, Vitest, Vue Test Utils, CSS custom properties, Element Plus.

---

### Task 1: Theme State Engine

**Files:**
- Modify: `webapp/src/lib/theme.ts`
- Modify: `webapp/src/lib/theme.spec.ts`

- [ ] **Step 1: Write the failing test**

```ts
it('restores teal-dark from storage', () => {
  localStorage.setItem(THEME_STORAGE_KEY, 'teal-dark')

  syncThemeFromStorage()

  expect(getStoredTheme()).toBe('teal-dark')
  expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
  expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(true)
})

it('cycles default -> teal -> teal-dark -> default', () => {
  expect(getNextThemeMode('default')).toBe('teal')
  expect(getNextThemeMode('teal')).toBe('teal-dark')
  expect(getNextThemeMode('teal-dark')).toBe('default')
})

it('applies the teal-dark class without leaving the teal class behind', () => {
  applyTheme('teal-dark')

  expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
  expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(true)
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
pnpm --dir webapp exec vitest run src/lib/theme.spec.ts -t "cycles default -> teal -> teal-dark -> default"
```

Expected: FAIL because `teal-dark` is not yet part of the theme model and `getNextThemeMode` does not exist.

- [ ] **Step 3: Write the minimal implementation**

```ts
export type ThemeMode = 'default' | 'teal' | 'teal-dark'

function normalizeThemeMode(value: string | null | undefined): ThemeMode {
  if (value === 'teal' || value === 'teal-dark') {
    return value
  }
  return 'default'
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

export function applyTheme(theme: ThemeMode) {
  document.documentElement.classList.toggle('theme-teal', theme === 'teal')
  document.documentElement.classList.toggle('theme-teal-dark', theme === 'teal-dark')
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:

```bash
pnpm --dir webapp exec vitest run src/lib/theme.spec.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add webapp/src/lib/theme.ts webapp/src/lib/theme.spec.ts
git commit -m "feat: add teal dark theme cycle"
```

### Task 2: Sidebar Theme Button

**Files:**
- Modify: `webapp/src/components/ConversationSidebar.vue`
- Modify: `webapp/src/components/ConversationSidebar.spec.ts`
- Modify: `webapp/src/lib/theme.ts` (if the task reuses a shared `cycleStoredTheme()` helper)

- [ ] **Step 1: Write the failing test**

```ts
it('cycles the theme button through all three states', async () => {
  localStorage.setItem(THEME_STORAGE_KEY, 'default')

  const wrapper = mount(ConversationSidebar, {
    global: {
      stubs: {
        RouterLink: {
          props: ['to'],
          template: '<a :href="to"><slot /></a>',
        },
      },
    },
    props: {
      activeConversationId: '',
      loading: false,
      username: 'demo-user',
      conversations: [],
    },
  })

  await wrapper.find('.sidebar-theme-toggle').trigger('click')
  expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('teal')
  expect(document.documentElement.classList.contains('theme-teal')).toBe(true)
  expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(false)

  await wrapper.find('.sidebar-theme-toggle').trigger('click')
  expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('teal-dark')
  expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
  expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(true)

  await wrapper.find('.sidebar-theme-toggle').trigger('click')
  expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('default')
  expect(document.documentElement.classList.contains('theme-teal')).toBe(false)
  expect(document.documentElement.classList.contains('theme-teal-dark')).toBe(false)
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/ConversationSidebar.spec.ts -t "cycles the theme button through all three states"
```

Expected: FAIL because the button still toggles only between two states and does not surface the third state.

- [ ] **Step 3: Write the minimal implementation**

```vue
<script setup lang="ts">
import { computed, ref } from 'vue'
import { getNextThemeMode, getStoredTheme, setStoredTheme, type ThemeMode } from '../lib/theme'

const themeMode = ref<ThemeMode>(getStoredTheme())
const nextThemeMode = computed(() => getNextThemeMode(themeMode.value))

function toggleTheme() {
  themeMode.value = nextThemeMode.value
  setStoredTheme(themeMode.value)
}
</script>

<button
  class="ghost-button icon-button sidebar-theme-toggle"
  type="button"
  :aria-label="themeMode === 'default' ? '切换到 Teal 亮色主题' : themeMode === 'teal' ? '切换到 Teal 暗色主题' : '切换到默认主题'"
  :title="themeMode === 'default' ? '下一次切换到 Teal 亮色主题' : themeMode === 'teal' ? '下一次切换到 Teal 暗色主题' : '下一次切换到默认主题'"
  @click="toggleTheme"
>
  <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <circle cx="3.5" cy="8" r="1.5" :fill="themeMode === 'default' ? 'currentColor' : 'none'" />
    <circle cx="8" cy="8" r="1.5" :fill="themeMode === 'teal' ? 'currentColor' : 'none'" />
    <circle cx="12.5" cy="8" r="1.5" :fill="themeMode === 'teal-dark' ? 'currentColor' : 'none'" />
  </svg>
</button>
```

- [ ] **Step 4: Run the test to verify it passes**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/ConversationSidebar.spec.ts
```

Expected: PASS, including the existing delete/menu/logout assertions.

- [ ] **Step 5: Commit**

```bash
git add webapp/src/components/ConversationSidebar.vue webapp/src/components/ConversationSidebar.spec.ts webapp/src/lib/theme.ts
git commit -m "feat: cycle sidebar theme through teal dark mode"
```

### Task 3: Shared Teal Dark Palette and Global Surfaces

**Files:**
- Modify: `webapp/src/style.css`

- [ ] **Step 1: Refactor the shared theme block into CSS variables**

```css
:root {
  --app-bg: linear-gradient(180deg, #fffaf1 0%, #f3f7f8 100%);
  --app-surface: rgba(255, 255, 255, 0.78);
  --app-surface-strong: #ffffff;
  --app-border: rgba(25, 50, 59, 0.12);
  --app-text: #19323b;
  --app-text-muted: #3b5962;
  --app-text-soft: #8b9e97;
  --app-accent: #14b8a6;
  --app-accent-strong: #4cd4fa;
  --app-focus-ring: rgba(20, 184, 166, 0.12);
}

html.theme-teal-dark {
  color-scheme: dark;
  --app-bg: linear-gradient(180deg, #061215 0%, #08171b 100%);
  --app-surface: rgba(9, 31, 34, 0.92);
  --app-surface-strong: #0b2629;
  --app-border: rgba(94, 234, 212, 0.16);
  --app-text: #ecfffb;
  --app-text-muted: #b6ddd7;
  --app-text-soft: #8aa9a4;
  --app-accent: #5eead4;
  --app-accent-strong: #67e8f9;
  --app-focus-ring: rgba(94, 234, 212, 0.18);
}
```

- [ ] **Step 2: Update the shared primitives and menu surfaces to consume the variables**

Use the existing selectors already in `style.css` and rewrite the hard-coded light colors to use the new tokens for the shared shells below:

```css
body {
  color: var(--app-text);
  background: var(--app-bg);
}

.primary-button {
  background: linear-gradient(135deg, var(--app-accent) 0%, var(--app-accent-strong) 100%);
  color: #f8fffd;
  box-shadow: 0 18px 36px rgba(20, 184, 166, 0.24);
}

.ghost-button,
.model-menu-trigger,
.model-menu-panel,
.model-menu-option,
.admin-dialog,
.sidebar-confirm-dialog,
.sidebar-user-menu-panel,
.conversation-card,
.composer-skill-select .el-select__wrapper,
.composer-skill-popper.el-popper,
.composer-skill-popper .el-select-dropdown__item,
.context-stats-popper.el-popper {
  background: var(--app-surface);
  border-color: var(--app-border);
  color: var(--app-text);
}

.text-input {
  background: var(--app-surface);
  border-color: var(--app-border);
  color: var(--app-text);
}

.eyebrow {
  color: var(--app-accent);
}
```

Make the dark-mode hover/selected states explicit instead of relying on the light palette with reduced opacity, especially for `.model-menu-option.active`, `.conversation-card.active`, `.el-select-dropdown__item.hover`, and `.el-select-dropdown__item.is-selected`.

- [ ] **Step 3: Verify the stylesheet still compiles cleanly**

Run:

```bash
pnpm --dir webapp exec vue-tsc -b
pnpm --dir webapp build
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add webapp/src/style.css
git commit -m "feat: add teal dark surface tokens"
```

### Task 4: Component-Scoped Dark Surfaces

**Files:**
- Modify: `webapp/src/components/ProfileDialog.vue`
- Modify: `webapp/src/components/OpenSourceLicensesPanel.vue`
- Modify: `webapp/src/views/AdminPromptView.vue`
- Modify: `webapp/src/views/AdminModelsView.vue`
- Modify: `webapp/src/views/AdminUsersView.vue`

- [ ] **Step 1: Add dark variants to the unique overlay shells**

Add `html.theme-teal-dark` overrides inside each component’s existing scoped style block for its overlay, backdrop, and shell colors, so the theme does not stop at the shared primitives.

```css
html.theme-teal-dark .profile-dialog-overlay,
html.theme-teal-dark .open-source-overlay,
html.theme-teal-dark .admin-prompt-dialog-mask,
html.theme-teal-dark .admin-dialog-overlay {
  background: rgba(2, 6, 23, 0.66);
}

html.theme-teal-dark .profile-dialog-shell,
html.theme-teal-dark .open-source-dialog,
html.theme-teal-dark .admin-prompt-dialog,
html.theme-teal-dark .admin-dialog,
html.theme-teal-dark .sidebar-confirm-dialog {
  background: rgba(9, 31, 34, 0.96);
  border-color: rgba(94, 234, 212, 0.16);
  color: #ecfffb;
}
```

Keep the backdrop darker than the page, keep the shell surfaces slightly raised, and keep link text readable on dark surfaces.

- [ ] **Step 2: Run the affected component specs**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/ProfileDialog.spec.ts src/components/OpenSourceLicensesPanel.spec.ts src/views/AdminPromptView.spec.ts src/views/AdminModelsView.spec.ts src/views/AdminUsersView.spec.ts
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add webapp/src/components/ProfileDialog.vue webapp/src/components/OpenSourceLicensesPanel.vue webapp/src/views/AdminPromptView.vue webapp/src/views/AdminModelsView.vue webapp/src/views/AdminUsersView.vue
git commit -m "feat: polish teal dark overlays"
```

### Task 5: Browser QA and Final Verification

**Files:**
- No source changes expected unless visual QA reveals a missed surface.

- [ ] **Step 1: Start the webapp**

Run:

```bash
pnpm --dir webapp dev
```

- [ ] **Step 2: Verify the cycle and surfaces in the browser**

Open the app in the in-app browser at the Vite URL, click the theme button three times, and confirm the cycle returns to default on the fourth click.

Check these surfaces in Teal dark mode:

```text
login page
chat page
sidebar
user menu
confirm dialog
model dropdown
skill dropdown
profile dialog
open source dialog
admin dialogs
```

- [ ] **Step 3: Capture desktop and narrow viewport screenshots**

Use the browser screenshots to confirm:

- white text stays readable and slightly bolder on dark surfaces
- hover/highlight states remain teal
- no dialog or dropdown clips offscreen
- the sidebar theme icon still reads as a three-state cycle

- [ ] **Step 4: Finish with the full frontend verification set**

Run:

```bash
pnpm --dir webapp exec vue-tsc -b
pnpm --dir webapp exec vitest run src/lib/theme.spec.ts src/components/ConversationSidebar.spec.ts src/main.spec.ts
pnpm --dir webapp build
```

Expected: PASS.
