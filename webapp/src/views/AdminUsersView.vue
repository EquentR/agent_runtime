<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import { fetchAdminUsers, resetAdminUserPassword, updateAdminUser } from '../lib/api'
import type { AdminUserFilter, AdminUserUpdateInput, AuthUser, AuthUserStatus, UserRole } from '../types/api'

const users = ref<AuthUser[]>([])
const selectedUser = ref<AuthUser | null>(null)
const showDialog = ref(false)
const loading = ref(false)
const saving = ref(false)
const resettingPassword = ref(false)
const errorMessage = ref('')
const statusMessage = ref('')

const filters = reactive({
  q: '',
  role: '' as UserRole | '',
  status: '' as AuthUserStatus | '',
})

const userDraft = reactive({
  role: 'user' as UserRole,
  status: 'active' as AuthUserStatus,
  email: '',
  displayName: '',
  emailVerified: false,
  forcePasswordChange: false,
})

const resetDraft = reactive({
  password: '',
})

const selectedTitle = computed(() => selectedUser.value ? `${selectedUser.value.username} · ${selectedUser.value.email}` : '选择用户')
const hasActiveFilters = computed(() => Boolean(filters.q.trim() || filters.role || filters.status))

function buildFilter(): AdminUserFilter {
  return {
    q: filters.q.trim(),
    role: filters.role,
    status: filters.status,
  }
}

function syncDraft(user: AuthUser) {
  userDraft.role = user.role
  userDraft.status = user.status
  userDraft.email = user.email
  userDraft.displayName = user.display_name
  userDraft.emailVerified = user.email_verified
  userDraft.forcePasswordChange = user.force_password_change
}

function clearSelection() {
  selectedUser.value = null
  showDialog.value = false
  resetDraft.password = ''
}

async function loadUsers() {
  loading.value = true
  errorMessage.value = ''
  try {
    users.value = await fetchAdminUsers(buildFilter())
    if (selectedUser.value) {
      const refreshed = users.value.find((user) => user.id === selectedUser.value?.id)
      if (refreshed) {
        selectedUser.value = refreshed
        syncDraft(refreshed)
      } else {
        clearSelection()
      }
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载用户失败'
  } finally {
    loading.value = false
  }
}

function selectUser(user: AuthUser) {
  selectedUser.value = user
  syncDraft(user)
  resetDraft.password = ''
  showDialog.value = true
}

function replaceUser(nextUser: AuthUser) {
  if (hasActiveFilters.value && !userMatchesActiveFilters(nextUser)) {
    users.value = users.value.filter((user) => user.id !== nextUser.id)
    clearSelection()
    return
  }
  users.value = users.value.map((user) => user.id === nextUser.id ? nextUser : user)
  selectedUser.value = nextUser
  syncDraft(nextUser)
}

function userMatchesActiveFilters(user: AuthUser) {
  const q = filters.q.trim().toLowerCase()
  if (q) {
    const text = [user.username, user.email, user.display_name].join('\n').toLowerCase()
    if (!text.includes(q)) {
      return false
    }
  }
  if (filters.role && user.role !== filters.role) {
    return false
  }
  if (filters.status && user.status !== filters.status) {
    return false
  }
  return true
}

async function submitUserUpdate() {
  if (!selectedUser.value) return
  saving.value = true
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    const input: AdminUserUpdateInput = {
      role: userDraft.role,
      status: userDraft.status,
      display_name: userDraft.displayName.trim(),
      email_verified: userDraft.emailVerified,
      force_password_change: userDraft.forcePasswordChange,
    }
    const email = userDraft.email.trim()
    if (email) {
      input.email = email
    }
    replaceUser(await updateAdminUser(selectedUser.value.id, input))
    statusMessage.value = '用户已更新'
    showDialog.value = false
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '更新用户失败'
  } finally {
    saving.value = false
  }
}

async function submitPasswordReset() {
  if (!selectedUser.value) return
  resettingPassword.value = true
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    replaceUser(await resetAdminUserPassword(selectedUser.value.id, { password: resetDraft.password }))
    resetDraft.password = ''
    statusMessage.value = '密码已重置'
    showDialog.value = false
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '重置密码失败'
  } finally {
    resettingPassword.value = false
  }
}

onMounted(() => {
  void loadUsers()
})
</script>

<template>
  <main class="admin-workbench">
    <header class="admin-workbench-header">
      <div>
        <p class="eyebrow">Users</p>
        <h1>用户管理</h1>
      </div>
      <span class="status-pill" :class="{ loading }">{{ loading ? '加载中' : `${users.length} 用户` }}</span>
    </header>

    <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>
    <p v-if="statusMessage" class="admin-inline-success">{{ statusMessage }}</p>

    <section class="admin-section">
      <form class="admin-filter-bar" data-user-search-form @submit.prevent="loadUsers">
        <input v-model="filters.q" class="text-input" data-user-search-input placeholder="搜索用户名、邮箱或显示名称">
        <select v-model="filters.role" class="text-input" data-user-role-filter>
          <option value="">全部角色</option>
          <option value="user">普通用户</option>
          <option value="admin">管理员</option>
        </select>
        <select v-model="filters.status" class="text-input" data-user-status-filter>
          <option value="">全部状态</option>
          <option value="active">active</option>
          <option value="pending_email_verification">pending_email_verification</option>
          <option value="needs_email_binding">needs_email_binding</option>
          <option value="disabled">disabled</option>
        </select>
        <button class="primary-button admin-form-button" type="submit">筛选</button>
      </form>
    </section>

    <section class="admin-section">
      <div class="admin-section-heading">
        <h2>用户列表</h2>
      </div>
      <div class="admin-table">
        <div class="admin-table-row users-table-head">
          <span>用户名</span>
          <span>邮箱</span>
          <span>角色</span>
          <span>状态</span>
          <span>操作</span>
        </div>
        <div
          v-for="user in users"
          :key="user.id"
          class="admin-table-row users-table-row button-row"
          :data-user-row="user.id"
          role="button"
          tabindex="0"
          @click="selectUser(user)"
          @keydown.enter.prevent="selectUser(user)"
          @keydown.space.prevent="selectUser(user)"
        >
          <span>{{ user.username }}</span>
          <span>{{ user.email || '-' }}</span>
          <span>{{ user.role }}</span>
          <span>{{ user.status }}</span>
          <span>
            <button class="ghost-button small" type="button" @click.stop="selectUser(user)">编辑</button>
          </span>
        </div>
        <div v-if="users.length === 0 && !loading" class="admin-table-row">
          <span class="admin-empty" style="grid-column: 1/-1">暂无用户数据。</span>
        </div>
      </div>
    </section>

    <!-- Edit Dialog -->
    <div v-if="showDialog && selectedUser" class="admin-dialog-overlay" @click.self="clearSelection">
      <div class="admin-dialog">
        <div class="admin-dialog-header">
          <h2>{{ selectedTitle }}</h2>
          <button class="admin-dialog-close" type="button" aria-label="关闭" @click="clearSelection">
            <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <line x1="3" y1="3" x2="13" y2="13" /><line x1="13" y1="3" x2="3" y2="13" />
            </svg>
          </button>
        </div>

        <form class="admin-form-grid" data-user-detail-form @submit.prevent="submitUserUpdate">
          <label>
            <span class="field-label">角色</span>
            <select v-model="userDraft.role" class="text-input" data-user-role-select>
              <option value="user">普通用户</option>
              <option value="admin">管理员</option>
            </select>
          </label>
          <label>
            <span class="field-label">状态</span>
            <select v-model="userDraft.status" class="text-input" data-user-status-select>
              <option value="active">active</option>
              <option value="pending_email_verification">pending_email_verification</option>
              <option value="needs_email_binding">needs_email_binding</option>
              <option value="disabled">disabled</option>
            </select>
          </label>
          <label>
            <span class="field-label">邮箱</span>
            <input v-model="userDraft.email" class="text-input" type="email">
          </label>
          <label>
            <span class="field-label">显示名称</span>
            <input v-model="userDraft.displayName" class="text-input">
          </label>
          <label class="admin-check-row">
            <input v-model="userDraft.emailVerified" type="checkbox" data-user-email-verified-input>
            <span>邮箱已验证</span>
          </label>
          <label class="admin-check-row">
            <input v-model="userDraft.forcePasswordChange" type="checkbox">
            <span>强制修改密码</span>
          </label>
          <div class="admin-form-actions">
            <button class="primary-button admin-form-button" type="submit" :disabled="saving">
              {{ saving ? '保存中' : '保存用户' }}
            </button>
          </div>
        </form>

        <div class="admin-dialog-divider"></div>

        <form class="admin-form-grid compact" data-user-password-reset-form @submit.prevent="submitPasswordReset">
          <label>
            <span class="field-label">临时密码</span>
            <input v-model="resetDraft.password" class="text-input" type="password" data-user-password-reset-input>
          </label>
          <div class="admin-form-actions">
            <button class="ghost-button admin-form-button" type="submit" :disabled="resettingPassword">
              {{ resettingPassword ? '重置中' : '重置密码' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </main>
</template>

<style scoped>
.users-table-head {
  font-weight: 700;
  color: #36535c;
}

.users-table-row,
.users-table-head {
  grid-template-columns: minmax(120px, 1fr) minmax(160px, 1.5fr) minmax(90px, 0.6fr) minmax(180px, 1.2fr) 80px;
}

.admin-dialog-divider {
  border-top: 1px solid rgba(25, 50, 59, 0.08);
  margin: 0;
}

.ghost-button.small {
  min-height: 2rem;
  padding: 0.3rem 0.58rem;
  font-size: 0.82rem;
}

html.theme-teal-dark .admin-dialog-overlay {
  background: rgba(1, 12, 12, 0.68);
}

html.theme-teal-dark .admin-dialog {
  background: rgba(9, 43, 40, 0.98);
  border-color: rgba(125, 232, 221, 0.16);
  color: #dffbf7;
  box-shadow: 0 28px 64px rgba(0, 0, 0, 0.42);
}

html.theme-teal-dark .admin-dialog-divider {
  border-top-color: rgba(125, 232, 221, 0.12);
}
</style>
