<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import { fetchPublicRegistrationSettings, fetchPublicTurnstileSettings } from '../lib/api'
import { login, register, verifyRegistrationEmail } from '../lib/session'
import type { AuthUser, PublicTurnstileSettings } from '../types/api'

interface TurnstileRenderOptions {
  sitekey: string
  callback: (token: string) => void
  'expired-callback': () => void
  'error-callback': () => void
}

interface TurnstileClient {
  render: (element: HTMLElement, options: TurnstileRenderOptions) => string
  reset?: (widgetId?: string) => void
  remove?: (widgetId: string) => void
}

declare global {
  interface Window {
    turnstile?: TurnstileClient
  }
}

let turnstileScriptPromise: Promise<void> | null = null

function loadTurnstileScript() {
  if (window.turnstile) {
    return Promise.resolve()
  }
  if (!turnstileScriptPromise) {
    turnstileScriptPromise = new Promise((resolve, reject) => {
      const existing = document.querySelector<HTMLScriptElement>('script[data-agent-runtime-turnstile]')
      if (existing) {
        if (existing.dataset.loaded === 'true') {
          if (window.turnstile) {
            resolve()
          } else {
            reject(new Error('turnstile script loaded without client'))
          }
          return
        }
        existing.addEventListener('load', () => resolve(), { once: true })
        existing.addEventListener('error', () => {
          turnstileScriptPromise = null
          reject(new Error('turnstile script failed to load'))
        }, { once: true })
        return
      }
      const script = document.createElement('script')
      script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
      script.async = true
      script.defer = true
      script.dataset.agentRuntimeTurnstile = 'true'
      script.addEventListener('load', () => {
        script.dataset.loaded = 'true'
        resolve()
      }, { once: true })
      script.addEventListener('error', () => {
        turnstileScriptPromise = null
        reject(new Error('turnstile script failed to load'))
      }, { once: true })
      document.head.appendChild(script)
    })
  }
  return turnstileScriptPromise
}

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
const turnstileSettings = ref<PublicTurnstileSettings>({
  enabled: false,
  site_key: '',
  protect_login: false,
  protect_registration: false,
  protect_verification: false,
})
const turnstileToken = ref('')
const turnstileLoadError = ref('')
const turnstileElement = ref<HTMLElement | null>(null)
const turnstileWidgetId = ref<string | null>(null)
const turnstileRequired = computed(() => {
  const settings = turnstileSettings.value
  if (!settings.enabled || !settings.site_key) {
    return false
  }
  if (mode.value === 'login' || mode.value === 'verify') {
    return settings.protect_login
  }
  return settings.protect_registration
})
const canSubmit = computed(() => {
  if (mode.value === 'verify') {
    return Boolean(pendingRegistrationUser.value?.id && pendingRegistrationUser.value.email && verificationCode.value.trim() && (!turnstileRequired.value || turnstileToken.value))
  }

  if (!username.value.trim() || !password.value) {
    return false
  }

  if (mode.value === 'register') {
    return Boolean(email.value.trim() && confirmPassword.value.length > 0 && (!turnstileRequired.value || turnstileToken.value))
  }

  return Boolean(!turnstileRequired.value || turnstileToken.value)
})

onMounted(async () => {
  try {
    const [registration, turnstile] = await Promise.allSettled([
      fetchPublicRegistrationSettings(),
      fetchPublicTurnstileSettings(),
    ])
    if (registration.status === 'fulfilled') {
      publicRegistrationEnabled.value = registration.value.enabled
    }
    if (turnstile.status === 'fulfilled') {
      turnstileSettings.value = turnstile.value
    }
    if (!publicRegistrationEnabled.value && mode.value === 'register') {
      mode.value = 'login'
    }
  } catch {
    publicRegistrationEnabled.value = true
  }
})

onBeforeUnmount(() => {
  removeTurnstileWidget()
})

watch([mode, turnstileRequired, () => turnstileSettings.value.site_key], async () => {
  turnstileToken.value = ''
  await renderTurnstile()
}, { flush: 'post' })

async function handleLogin() {
  if (!canSubmit.value || submitting.value) {
    return
  }

  submitting.value = true
  errorMessage.value = ''

  try {
    if (mode.value === 'login') {
      if (turnstileRequired.value) {
        await login(username.value, password.value, turnstileToken.value)
      } else {
        await login(username.value, password.value)
      }
      await router.push('/chat')
      return
    }

    if (mode.value === 'verify') {
      const user = pendingRegistrationUser.value
      if (!user) {
        throw new Error('注册信息已失效，请重新注册')
      }
      await verifyRegistrationEmail(user.id, user.email, verificationCode.value)
      if (turnstileRequired.value) {
        await login(user.username, password.value, turnstileToken.value)
      } else {
        await login(user.username, password.value)
      }
      await router.push('/chat')
      return
    }

    const result = turnstileRequired.value
      ? await register(username.value, email.value, password.value, confirmPassword.value, turnstileToken.value)
      : await register(username.value, email.value, password.value, confirmPassword.value)
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
    resetTurnstileWidget()
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

async function renderTurnstile() {
  if (!turnstileRequired.value) {
    removeTurnstileWidget()
    turnstileLoadError.value = ''
    return
  }
  await nextTick()
  const element = turnstileElement.value
  if (!element) {
    return
  }
  removeTurnstileWidget()
  turnstileLoadError.value = ''
  try {
    await loadTurnstileScript()
    if (!window.turnstile) {
      throw new Error('turnstile is unavailable')
    }
    turnstileWidgetId.value = window.turnstile.render(element, {
      sitekey: turnstileSettings.value.site_key,
      callback: (token: string) => {
        turnstileToken.value = token
        turnstileLoadError.value = ''
      },
      'expired-callback': () => {
        turnstileToken.value = ''
      },
      'error-callback': () => {
        turnstileToken.value = ''
        turnstileLoadError.value = '人机验证失败，请重试'
      },
    })
  } catch {
    turnstileToken.value = ''
    turnstileLoadError.value = '人机验证加载失败，请刷新后重试'
  }
}

function removeTurnstileWidget() {
  const widgetId = turnstileWidgetId.value
  if (!widgetId) {
    return
  }
  if (window.turnstile?.remove) {
    window.turnstile.remove(widgetId)
  } else {
    window.turnstile?.reset?.(widgetId)
  }
  turnstileWidgetId.value = null
}

function resetTurnstileWidget() {
  if (!turnstileRequired.value || !turnstileWidgetId.value) {
    return
  }
  turnstileToken.value = ''
  window.turnstile?.reset?.(turnstileWidgetId.value)
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

      <div v-if="turnstileRequired" ref="turnstileElement" class="turnstile-widget" aria-label="Cloudflare Turnstile"></div>
      <p v-if="turnstileLoadError" class="error-banner auth-error">{{ turnstileLoadError }}</p>
      <p v-if="errorMessage" class="error-banner auth-error">{{ errorMessage }}</p>

      <button class="primary-button wide" type="button" :disabled="!canSubmit || submitting" @click="handleLogin">
        {{ submitting ? '提交中...' : mode === 'login' ? '登录' : mode === 'register' ? '注册' : '验证并进入' }}
      </button>
    </section>
  </main>
</template>
