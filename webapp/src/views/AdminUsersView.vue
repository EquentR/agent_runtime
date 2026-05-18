<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import { fetchAdminUsers, resetAdminUserPassword, updateAdminUser } from '../lib/api'
import type { AdminUserFilter, AuthUser, AuthUserStatus, UserRole } from '../types/api'

const users = ref<AuthUser[]>([])
const selectedUser = ref<AuthUser | null>(null)
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

async function loadUsers() {
  loading.value = true
  errorMessage.value = ''
  try {
    users.value = await fetchAdminUsers(buildFilter())
    if (selectedUser.value) {
      selectedUser.value = users.value.find((user) => user.id === selectedUser.value?.id) ?? selectedUser.value
      syncDraft(selectedUser.value)
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
}

function replaceUser(nextUser: AuthUser) {
  users.value = users.value.map((user) => user.id === nextUser.id ? nextUser : user)
  selectedUser.value = nextUser
  syncDraft(nextUser)
}

async function submitUserUpdate() {
  if (!selectedUser.value) return
  saving.value = true
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    replaceUser(await updateAdminUser(selectedUser.value.id, {
      role: userDraft.role,
      status: userDraft.status,
      email: userDraft.email.trim(),
      display_name: userDraft.displayName.trim(),
      email_verified: userDraft.emailVerified,
      force_password_change: userDraft.forcePasswordChange,
    }))
    statusMessage.value = '用户已更新'
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
        <button class="primary-button" type="submit">筛选</button>
      </form>
    </section>

    <div class="admin-two-column">
      <section class="admin-section admin-table-panel">
        <div class="admin-section-heading">
          <h2>用户列表</h2>
        </div>
        <div class="admin-table">
          <button
            v-for="user in users"
            :key="user.id"
            class="admin-table-row button-row"
            :class="{ active: selectedUser?.id === user.id }"
            type="button"
            :data-user-row="user.id"
            @click="selectUser(user)"
          >
            <span>{{ user.username }}</span>
            <span>{{ user.email || '-' }}</span>
            <span>{{ user.role }}</span>
            <span>{{ user.status }}</span>
          </button>
        </div>
      </section>

      <section class="admin-section admin-detail-panel">
        <div class="admin-section-heading">
          <h2>{{ selectedTitle }}</h2>
        </div>

        <div v-if="selectedUser" class="admin-detail-stack">
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
              <button class="primary-button" type="submit" :disabled="saving">
                {{ saving ? '保存中' : '保存用户' }}
              </button>
            </div>
          </form>

          <form class="admin-form-grid compact" data-user-password-reset-form @submit.prevent="submitPasswordReset">
            <label>
              <span class="field-label">临时密码</span>
              <input v-model="resetDraft.password" class="text-input" type="password" data-user-password-reset-input>
            </label>
            <div class="admin-form-actions">
              <button class="ghost-button" type="submit" :disabled="resettingPassword">
                {{ resettingPassword ? '重置中' : '重置密码' }}
              </button>
            </div>
          </form>
        </div>
        <p v-else class="admin-empty">从左侧选择一个用户。</p>
      </section>
    </div>
  </main>
</template>
