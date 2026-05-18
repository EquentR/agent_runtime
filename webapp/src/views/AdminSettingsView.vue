<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

import {
  fetchAdminRegistrationSettings,
  fetchAdminSMTPSettings,
  fetchAdminTurnstileSettings,
  testAdminSMTPSettings,
  updateAdminRegistrationSettings,
  updateAdminSMTPSettings,
  updateAdminTurnstileSettings,
} from '../lib/api'
import type { AdminSMTPSettings, AdminTurnstileSettings } from '../types/api'

const loading = ref(false)
const saving = ref('')
const errorMessage = ref('')
const statusMessage = ref('')

const smtpDraft = reactive({
  enabled: false,
  host: '',
  port: 587,
  username: '',
  password: '',
  passwordMasked: '',
  clearPassword: false,
  from: '',
  useTLS: false,
  useStartTLS: true,
  testTo: '',
})

const turnstileDraft = reactive({
  enabled: false,
  siteKey: '',
  secret: '',
  secretMasked: '',
  clearSecret: false,
  protectLogin: false,
  protectRegistration: false,
  protectVerification: false,
})

const registrationDraft = reactive({
  enabled: true,
})

function syncSMTP(settings: AdminSMTPSettings) {
  smtpDraft.enabled = settings.enabled
  smtpDraft.host = settings.host
  smtpDraft.port = settings.port || 587
  smtpDraft.username = settings.username
  smtpDraft.password = ''
  smtpDraft.passwordMasked = settings.password_masked
  smtpDraft.clearPassword = false
  smtpDraft.from = settings.from
  smtpDraft.useTLS = settings.use_tls
  smtpDraft.useStartTLS = settings.use_start_tls
}

function syncTurnstile(settings: AdminTurnstileSettings) {
  turnstileDraft.enabled = settings.enabled
  turnstileDraft.siteKey = settings.site_key
  turnstileDraft.secret = ''
  turnstileDraft.secretMasked = settings.secret_masked
  turnstileDraft.clearSecret = false
  turnstileDraft.protectLogin = settings.protect_login
  turnstileDraft.protectRegistration = settings.protect_registration
  turnstileDraft.protectVerification = settings.protect_verification
}

async function loadSettings() {
  loading.value = true
  errorMessage.value = ''
  try {
    const [smtp, turnstile, registration] = await Promise.all([
      fetchAdminSMTPSettings(),
      fetchAdminTurnstileSettings(),
      fetchAdminRegistrationSettings(),
    ])
    syncSMTP(smtp)
    syncTurnstile(turnstile)
    registrationDraft.enabled = registration.enabled
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载设置失败'
  } finally {
    loading.value = false
  }
}

async function submitSMTP() {
  saving.value = 'smtp'
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    syncSMTP(await updateAdminSMTPSettings({
      enabled: smtpDraft.enabled,
      host: smtpDraft.host.trim(),
      port: Number(smtpDraft.port) || 0,
      username: smtpDraft.username.trim(),
      password: smtpDraft.password,
      clear_password: smtpDraft.clearPassword,
      from: smtpDraft.from.trim(),
      use_tls: smtpDraft.useTLS,
      use_start_tls: smtpDraft.useStartTLS,
    }))
    statusMessage.value = 'SMTP 设置已保存'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '保存 SMTP 失败'
  } finally {
    saving.value = ''
  }
}

async function submitSMTPTest() {
  saving.value = 'smtp-test'
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    await testAdminSMTPSettings({ to: smtpDraft.testTo.trim() })
    statusMessage.value = '测试邮件已发送'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '发送测试邮件失败'
  } finally {
    saving.value = ''
  }
}

async function submitTurnstile() {
  saving.value = 'turnstile'
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    syncTurnstile(await updateAdminTurnstileSettings({
      enabled: turnstileDraft.enabled,
      site_key: turnstileDraft.siteKey.trim(),
      secret: turnstileDraft.secret,
      clear_secret: turnstileDraft.clearSecret,
      protect_login: turnstileDraft.protectLogin,
      protect_registration: turnstileDraft.protectRegistration,
      protect_verification: turnstileDraft.protectVerification,
    }))
    statusMessage.value = 'Turnstile 设置已保存'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '保存 Turnstile 失败'
  } finally {
    saving.value = ''
  }
}

async function submitRegistration() {
  saving.value = 'registration'
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    const settings = await updateAdminRegistrationSettings({ enabled: registrationDraft.enabled })
    registrationDraft.enabled = settings.enabled
    statusMessage.value = '公开注册设置已保存'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '保存公开注册设置失败'
  } finally {
    saving.value = ''
  }
}

onMounted(() => {
  void loadSettings()
})
</script>

<template>
  <main class="admin-workbench">
    <header class="admin-workbench-header">
      <div>
        <p class="eyebrow">Settings</p>
        <h1>系统设置</h1>
      </div>
      <span class="status-pill" :class="{ loading }">{{ loading ? '加载中' : '就绪' }}</span>
    </header>

    <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>
    <p v-if="statusMessage" class="admin-inline-success">{{ statusMessage }}</p>

    <section class="admin-section">
      <div class="admin-section-heading">
        <h2>SMTP</h2>
        <span class="admin-secret-mask" data-smtp-password-mask>{{ smtpDraft.passwordMasked || '未保存密码' }}</span>
      </div>
      <form class="admin-form-grid" data-smtp-form @submit.prevent="submitSMTP">
        <label class="admin-check-row">
          <input v-model="smtpDraft.enabled" type="checkbox">
          <span>启用 SMTP</span>
        </label>
        <label>
          <span class="field-label">Host</span>
          <input v-model="smtpDraft.host" class="text-input">
        </label>
        <label>
          <span class="field-label">Port</span>
          <input v-model.number="smtpDraft.port" class="text-input" type="number">
        </label>
        <label>
          <span class="field-label">Username</span>
          <input v-model="smtpDraft.username" class="text-input">
        </label>
        <label>
          <span class="field-label">Password</span>
          <input v-model="smtpDraft.password" class="text-input" type="password" data-smtp-password-input>
        </label>
        <label>
          <span class="field-label">From</span>
          <input v-model="smtpDraft.from" class="text-input" type="email">
        </label>
        <label class="admin-check-row">
          <input v-model="smtpDraft.useTLS" type="checkbox">
          <span>Use TLS</span>
        </label>
        <label class="admin-check-row">
          <input v-model="smtpDraft.useStartTLS" type="checkbox">
          <span>Use STARTTLS</span>
        </label>
        <label class="admin-check-row">
          <input v-model="smtpDraft.clearPassword" type="checkbox" data-smtp-clear-password-input>
          <span>清除已保存密码</span>
        </label>
        <div class="admin-form-actions">
          <button class="primary-button" type="submit" :disabled="saving === 'smtp'">
            {{ saving === 'smtp' ? '保存中' : '保存 SMTP' }}
          </button>
        </div>
      </form>
      <form class="admin-inline-form" data-smtp-test-form @submit.prevent="submitSMTPTest">
        <input v-model="smtpDraft.testTo" class="text-input" type="email" data-smtp-test-to-input placeholder="测试收件人">
        <button class="ghost-button" type="submit" :disabled="saving === 'smtp-test'">发送测试</button>
      </form>
    </section>

    <section class="admin-section">
      <div class="admin-section-heading">
        <h2>Turnstile</h2>
        <span class="admin-secret-mask" data-turnstile-secret-mask>{{ turnstileDraft.secretMasked || '未保存 Secret' }}</span>
      </div>
      <form class="admin-form-grid" data-turnstile-form @submit.prevent="submitTurnstile">
        <label class="admin-check-row">
          <input v-model="turnstileDraft.enabled" type="checkbox">
          <span>启用 Turnstile</span>
        </label>
        <label>
          <span class="field-label">Site Key</span>
          <input v-model="turnstileDraft.siteKey" class="text-input">
        </label>
        <label>
          <span class="field-label">Secret</span>
          <input v-model="turnstileDraft.secret" class="text-input" type="password" data-turnstile-secret-input>
        </label>
        <label class="admin-check-row">
          <input v-model="turnstileDraft.protectLogin" type="checkbox">
          <span>保护登录</span>
        </label>
        <label class="admin-check-row">
          <input v-model="turnstileDraft.protectRegistration" type="checkbox">
          <span>保护注册</span>
        </label>
        <label class="admin-check-row">
          <input v-model="turnstileDraft.protectVerification" type="checkbox">
          <span>保护验证码</span>
        </label>
        <label class="admin-check-row">
          <input v-model="turnstileDraft.clearSecret" type="checkbox" data-turnstile-clear-secret-input>
          <span>清除已保存 Secret</span>
        </label>
        <div class="admin-form-actions">
          <button class="primary-button" type="submit" :disabled="saving === 'turnstile'">
            {{ saving === 'turnstile' ? '保存中' : '保存 Turnstile' }}
          </button>
        </div>
      </form>
    </section>

    <section class="admin-section">
      <div class="admin-section-heading">
        <h2>公开注册</h2>
      </div>
      <form class="admin-inline-form" data-registration-form @submit.prevent="submitRegistration">
        <label class="admin-check-row">
          <input v-model="registrationDraft.enabled" type="checkbox" data-registration-enabled-input>
          <span>允许公开注册</span>
        </label>
        <button class="primary-button" type="submit" :disabled="saving === 'registration'">
          {{ saving === 'registration' ? '保存中' : '保存公开注册' }}
        </button>
      </form>
    </section>
  </main>
</template>
