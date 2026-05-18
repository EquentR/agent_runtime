<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { fetchPublicRegistrationSettings } from '../lib/api'
import { login, register, verifyRegistrationEmail } from '../lib/session'
import type { AuthUser } from '../types/api'

const router = useRouter()
const mode = ref<'login' | 'register' | 'verify'>('login')
const username = ref('')
const email = ref('')
const password = ref('')
const confirmPassword = ref('')
const verificationCode = ref('')
const pendingRegistrationUser = ref<AuthUser | null>(null)
const publicRegistrationEnabled = ref(true)
const submitting = ref(false)
const errorMessage = ref('')
const canSubmit = computed(() => {
  if (mode.value === 'verify') {
    return Boolean(pendingRegistrationUser.value?.id && pendingRegistrationUser.value.email && verificationCode.value.trim())
  }

  if (!username.value.trim() || !password.value) {
    return false
  }

  if (mode.value === 'register') {
    return Boolean(email.value.trim() && confirmPassword.value.length > 0)
  }

  return true
})

onMounted(async () => {
  try {
    const settings = await fetchPublicRegistrationSettings()
    publicRegistrationEnabled.value = settings.enabled
    if (!settings.enabled && mode.value === 'register') {
      mode.value = 'login'
    }
  } catch {
    publicRegistrationEnabled.value = true
  }
})

async function handleLogin() {
  if (!canSubmit.value || submitting.value) {
    return
  }

  submitting.value = true
  errorMessage.value = ''

  try {
    if (mode.value === 'login') {
      await login(username.value, password.value)
      await router.push('/chat')
      return
    }

    if (mode.value === 'verify') {
      const user = pendingRegistrationUser.value
      if (!user) {
        throw new Error('注册信息已失效，请重新注册')
      }
      await verifyRegistrationEmail(user.id, user.email, verificationCode.value)
      await login(user.username, password.value)
      await router.push('/chat')
      return
    }

    const result = await register(username.value, email.value, password.value, confirmPassword.value)
    if (result.verification_required) {
      pendingRegistrationUser.value = result.user
      verificationCode.value = ''
      mode.value = 'verify'
      return
    } else {
      await router.push('/chat')
      return
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '请求失败，请稍后重试'
  } finally {
    submitting.value = false
  }
}

function switchMode(nextMode: 'login' | 'register') {
  mode.value = nextMode
  errorMessage.value = ''
  if (nextMode === 'login') {
    pendingRegistrationUser.value = null
    verificationCode.value = ''
  }
}
</script>

<template>
  <main class="login-shell">
    <section class="login-card">
      <p class="eyebrow">Agent Runtime</p>
      <div class="login-hero">
        <h1>{{ mode === 'login' ? '欢迎登录' : mode === 'register' ? '创建账号' : '验证邮箱' }}</h1>
        <p class="login-copy">使用用户名或邮箱登录；新账号需要完成邮箱验证后进入系统。</p>
      </div>

      <div v-if="mode !== 'verify'" class="auth-switch" role="tablist" aria-label="登录注册切换">
        <button class="auth-switch-button" :class="{ active: mode === 'login' }" type="button" @click="switchMode('login')">
          登录
        </button>
        <button v-if="publicRegistrationEnabled" class="auth-switch-button" :class="{ active: mode === 'register' }" type="button" @click="switchMode('register')">
          注册
        </button>
      </div>

      <template v-if="mode !== 'verify'">
        <label class="field-label" for="username">{{ mode === 'login' ? '用户名或邮箱' : '用户名' }}</label>
        <input id="username" v-model="username" class="text-input" :placeholder="mode === 'login' ? '请输入用户名或邮箱' : '请输入用户名'" autocomplete="username" />
      </template>

      <template v-if="mode === 'register'">
        <label class="field-label" for="email">邮箱</label>
        <input id="email" v-model="email" class="text-input" type="email" placeholder="请输入邮箱" autocomplete="email" />
      </template>

      <template v-if="mode !== 'verify'">
        <label class="field-label" for="password">密码</label>
        <input id="password" v-model="password" class="text-input" type="password" placeholder="请输入密码" :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" />
      </template>

      <template v-if="mode === 'register'">
        <label class="field-label" for="confirmPassword">确认密码</label>
        <input
          id="confirmPassword"
          v-model="confirmPassword"
          class="text-input"
          type="password"
          placeholder="请再次输入密码"
          autocomplete="new-password"
        />
      </template>

      <template v-if="mode === 'verify'">
        <label class="field-label" for="verificationCode">邮箱验证码</label>
        <input id="verificationCode" v-model="verificationCode" class="text-input" inputmode="numeric" placeholder="请输入 6 位验证码" autocomplete="one-time-code" />
      </template>

      <p v-if="errorMessage" class="error-banner auth-error">{{ errorMessage }}</p>

      <button class="primary-button wide" type="button" :disabled="!canSubmit || submitting" @click="handleLogin">
        {{ submitting ? '提交中...' : mode === 'login' ? '登录' : mode === 'register' ? '注册' : '验证并进入' }}
      </button>
    </section>
  </main>
</template>
