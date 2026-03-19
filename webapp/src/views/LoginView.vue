<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'

import { login, register } from '../lib/session'

const router = useRouter()
const mode = ref<'login' | 'register'>('login')
const username = ref('')
const password = ref('')
const confirmPassword = ref('')
const submitting = ref(false)
const errorMessage = ref('')
const canSubmit = computed(() => {
  if (!username.value.trim() || !password.value) {
    return false
  }

  if (mode.value === 'register') {
    return confirmPassword.value.length > 0
  }

  return true
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
    } else {
      await register(username.value, password.value, confirmPassword.value)
    }

    await router.push('/chat')
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '请求失败，请稍后重试'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <main class="login-shell">
    <section class="login-card">
      <p class="eyebrow">Agent Runtime</p>
      <div class="login-hero">
        <h1>{{ mode === 'login' ? '欢迎登录' : '创建账号' }}</h1>
        <p class="login-copy">使用用户名和密码即可进入系统，当前注册仅需用户名与两次密码确认。</p>
      </div>

      <div class="auth-switch" role="tablist" aria-label="登录注册切换">
        <button class="auth-switch-button" :class="{ active: mode === 'login' }" type="button" @click="mode = 'login'">
          登录
        </button>
        <button class="auth-switch-button" :class="{ active: mode === 'register' }" type="button" @click="mode = 'register'">
          注册
        </button>
      </div>

      <label class="field-label" for="username">用户名</label>
      <input id="username" v-model="username" class="text-input" placeholder="请输入用户名" autocomplete="username" />

      <label class="field-label" for="password">密码</label>
      <input id="password" v-model="password" class="text-input" type="password" placeholder="请输入密码" autocomplete="current-password" />

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

      <p v-if="errorMessage" class="error-banner auth-error">{{ errorMessage }}</p>

      <button class="primary-button wide" type="button" :disabled="!canSubmit || submitting" @click="handleLogin">
        {{ submitting ? '提交中...' : mode === 'login' ? '登录' : '注册并进入' }}
      </button>
    </section>
  </main>
</template>
