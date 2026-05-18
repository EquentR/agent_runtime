<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'

import {
  changeUserPassword,
  confirmUserEmailVerification,
  fetchUserProfile,
  startUserEmailVerification,
  updateUserProfile,
} from '../lib/api'
import type { AuthUser } from '../types/api'

const loading = ref(false)
const savingProfile = ref(false)
const savingPassword = ref(false)
const sendingEmailCode = ref(false)
const confirmingEmail = ref(false)
const profile = ref<AuthUser | null>(null)
const statusMessage = ref('')
const errorMessage = ref('')

const profileDraft = reactive({
  displayName: '',
})

const passwordDraft = reactive({
  currentPassword: '',
  password: '',
  confirmPassword: '',
})

const emailDraft = reactive({
  email: '',
  code: '',
})

const requiredActions = computed(() => profile.value?.required_actions ?? [])
const needsEmail = computed(() => requiredActions.value.includes('bind_email') || requiredActions.value.includes('verify_email'))
const needsPassword = computed(() => requiredActions.value.includes('change_password'))
const emailStatus = computed(() => {
  if (!profile.value?.email) return '未绑定'
  return profile.value.email_verified ? '已验证' : '待验证'
})

function syncProfile(nextProfile: AuthUser) {
  profile.value = nextProfile
  profileDraft.displayName = nextProfile.display_name
  emailDraft.email = nextProfile.email
}

async function loadProfile() {
  loading.value = true
  errorMessage.value = ''
  try {
    syncProfile(await fetchUserProfile())
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载个人资料失败'
  } finally {
    loading.value = false
  }
}

async function submitProfile() {
  savingProfile.value = true
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    const displayName = profileDraft.displayName.trim()
    syncProfile(await updateUserProfile({ display_name: displayName }))
    statusMessage.value = '资料已更新'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '更新资料失败'
  } finally {
    savingProfile.value = false
  }
}

async function submitPassword() {
  savingPassword.value = true
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    syncProfile(await changeUserPassword({
      current_password: passwordDraft.currentPassword,
      password: passwordDraft.password,
      confirm_password: passwordDraft.confirmPassword,
    }))
    passwordDraft.currentPassword = ''
    passwordDraft.password = ''
    passwordDraft.confirmPassword = ''
    statusMessage.value = '密码已更新'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '修改密码失败'
  } finally {
    savingPassword.value = false
  }
}

async function startEmailVerification() {
  sendingEmailCode.value = true
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    emailDraft.email = emailDraft.email.trim()
    await startUserEmailVerification({ email: emailDraft.email })
    statusMessage.value = '验证码已发送'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '发送验证码失败'
  } finally {
    sendingEmailCode.value = false
  }
}

async function confirmEmailVerification() {
  confirmingEmail.value = true
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    emailDraft.email = emailDraft.email.trim()
    emailDraft.code = emailDraft.code.trim()
    syncProfile(await confirmUserEmailVerification({
      email: emailDraft.email,
      code: emailDraft.code,
    }))
    emailDraft.code = ''
    statusMessage.value = '邮箱已验证'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '确认验证码失败'
  } finally {
    confirmingEmail.value = false
  }
}

onMounted(() => {
  void loadProfile()
})
</script>

<template>
  <main class="admin-workbench profile-workbench">
    <header class="admin-workbench-header">
      <div>
        <p class="eyebrow">Profile</p>
        <h1>个人设置</h1>
      </div>
      <RouterLink class="ghost-button" to="/chat">返回聊天</RouterLink>
    </header>

    <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>
    <p v-if="statusMessage" class="admin-inline-success">{{ statusMessage }}</p>

    <section v-if="needsEmail || needsPassword" class="admin-notice-row" aria-label="必要操作">
      <span v-if="needsEmail" class="admin-warning-pill">必须绑定邮箱</span>
      <span v-if="needsPassword" class="admin-warning-pill">必须修改密码</span>
    </section>

    <section class="admin-section">
      <div class="admin-section-heading">
        <h2>账号资料</h2>
        <span class="status-pill" :class="{ loading }">{{ loading ? '加载中' : '就绪' }}</span>
      </div>
      <form class="admin-form-grid" data-profile-form @submit.prevent="submitProfile">
        <label>
          <span class="field-label">用户名</span>
          <input class="text-input" :value="profile?.username ?? ''" readonly>
        </label>
        <label>
          <span class="field-label">显示名称</span>
          <input v-model="profileDraft.displayName" class="text-input" data-profile-display-name-input>
        </label>
        <label>
          <span class="field-label">邮箱</span>
          <input class="text-input" :value="profile?.email || '未绑定'" readonly>
        </label>
        <label>
          <span class="field-label">邮箱状态</span>
          <input class="text-input" :value="emailStatus" readonly>
        </label>
        <div class="admin-form-actions">
          <button class="primary-button" type="submit" :disabled="savingProfile">
            {{ savingProfile ? '保存中' : '保存资料' }}
          </button>
        </div>
      </form>
    </section>

    <section class="admin-section">
      <div class="admin-section-heading">
        <h2>邮箱验证</h2>
        <span class="admin-current-value">当前邮箱：{{ profile?.email || '未绑定' }}</span>
      </div>
      <div class="admin-split-forms">
        <form class="admin-form-grid compact" data-profile-email-start-form @submit.prevent="startEmailVerification">
          <label>
            <span class="field-label">邮箱地址</span>
            <input v-model="emailDraft.email" class="text-input" type="email" data-profile-email-input>
          </label>
          <div class="admin-form-actions">
            <button class="primary-button" type="submit" :disabled="sendingEmailCode">
              {{ sendingEmailCode ? '发送中' : '发送验证码' }}
            </button>
          </div>
        </form>
        <form class="admin-form-grid compact" data-profile-email-confirm-form @submit.prevent="confirmEmailVerification">
          <label>
            <span class="field-label">验证码</span>
            <input v-model="emailDraft.code" class="text-input" inputmode="numeric" data-profile-email-code-input>
          </label>
          <div class="admin-form-actions">
            <button class="primary-button" type="submit" :disabled="confirmingEmail">
              {{ confirmingEmail ? '确认中' : '确认绑定' }}
            </button>
          </div>
        </form>
      </div>
    </section>

    <section class="admin-section">
      <div class="admin-section-heading">
        <h2>安全</h2>
      </div>
      <form class="admin-form-grid" data-profile-password-form @submit.prevent="submitPassword">
        <label>
          <span class="field-label">当前密码</span>
          <input v-model="passwordDraft.currentPassword" class="text-input" type="password" data-profile-current-password-input>
        </label>
        <label>
          <span class="field-label">新密码</span>
          <input v-model="passwordDraft.password" class="text-input" type="password" data-profile-new-password-input>
        </label>
        <label>
          <span class="field-label">确认新密码</span>
          <input v-model="passwordDraft.confirmPassword" class="text-input" type="password" data-profile-confirm-password-input>
        </label>
        <div class="admin-form-actions">
          <button class="primary-button" type="submit" :disabled="savingPassword">
            {{ savingPassword ? '保存中' : '修改密码' }}
          </button>
        </div>
      </form>
    </section>
  </main>
</template>
