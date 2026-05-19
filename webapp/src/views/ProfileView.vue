<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'

import {
  changeUserPassword,
  confirmUserEmailVerification,
  createUserCustomModel,
  deleteUserCustomModel,
  fetchPublicTurnstileSettings,
  fetchUserCustomModels,
  fetchUserProfile,
  startUserEmailVerification,
  testUserCustomModel,
  updateUserCustomModel,
  updateUserProfile,
} from '../lib/api'
import type { AuthUser, CustomLLMModel, CustomLLMModelInput, PublicTurnstileSettings } from '../types/api'

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

let profileTurnstileScriptPromise: Promise<void> | null = null

function loadProfileTurnstileScript() {
  if (window.turnstile) {
    return Promise.resolve()
  }
  if (!profileTurnstileScriptPromise) {
    profileTurnstileScriptPromise = new Promise((resolve, reject) => {
      const existing = document.querySelector<HTMLScriptElement>('script[data-agent-runtime-turnstile]')
      if (existing) {
        if (existing.dataset.loaded === 'true') {
          window.turnstile ? resolve() : reject(new Error('turnstile script loaded without client'))
          return
        }
        existing.addEventListener('load', () => resolve(), { once: true })
        existing.addEventListener('error', () => {
          profileTurnstileScriptPromise = null
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
        profileTurnstileScriptPromise = null
        reject(new Error('turnstile script failed to load'))
      }, { once: true })
      document.head.appendChild(script)
    })
  }
  return profileTurnstileScriptPromise
}

const loading = ref(false)
const savingProfile = ref(false)
const savingPassword = ref(false)
const sendingEmailCode = ref(false)
const confirmingEmail = ref(false)
const modelSaving = ref('')
const profile = ref<AuthUser | null>(null)
const customModels = ref<CustomLLMModel[]>([])
const selectedModelId = ref('')
const statusMessage = ref('')
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

const modelDraft = reactive({
  providerType: 'openai_responses',
  providerId: '',
  modelId: '',
  displayName: '',
  baseURL: '',
  apiKey: '',
  enabled: true,
  contextMaxTokens: 32768,
  attachments: false,
})

const requiredActions = computed(() => profile.value?.required_actions ?? [])
const needsEmail = computed(() => requiredActions.value.includes('bind_email') || requiredActions.value.includes('verify_email'))
const needsPassword = computed(() => requiredActions.value.includes('change_password'))
const selectedModel = computed(() => customModels.value.find((model) => model.id === selectedModelId.value) ?? null)
const modelFormTitle = computed(() => selectedModel.value ? `编辑模型：${selectedModel.value.display_name}` : '新增我的模型')
const turnstileRequired = computed(() => {
  const settings = turnstileSettings.value
  return Boolean(settings.enabled && settings.site_key && settings.protect_verification)
})
const emailStatus = computed(() => {
  if (!profile.value?.email) return '未绑定'
  return profile.value.email_verified ? '已验证' : '待验证'
})

function formatNumber(value: number | undefined) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return '-'
  }
  return value.toLocaleString('en-US')
}

function syncProfile(nextProfile: AuthUser) {
  profile.value = nextProfile
  profileDraft.displayName = nextProfile.display_name
  emailDraft.email = nextProfile.email
}

function syncModelDraft(model: CustomLLMModel) {
  selectedModelId.value = model.id
  modelDraft.providerType = model.provider_type || 'openai_responses'
  modelDraft.providerId = model.provider_id
  modelDraft.modelId = model.model_id
  modelDraft.displayName = model.display_name
  modelDraft.baseURL = model.base_url
  modelDraft.apiKey = ''
  modelDraft.enabled = model.enabled
  modelDraft.contextMaxTokens = model.context_max_tokens || 32768
  modelDraft.attachments = model.capabilities.attachments
}

function resetModelDraft() {
  selectedModelId.value = ''
  modelDraft.providerType = 'openai_responses'
  modelDraft.providerId = ''
  modelDraft.modelId = ''
  modelDraft.displayName = ''
  modelDraft.baseURL = ''
  modelDraft.apiKey = ''
  modelDraft.enabled = true
  modelDraft.contextMaxTokens = 32768
  modelDraft.attachments = false
}

function upsertCustomModel(model: CustomLLMModel) {
  const exists = customModels.value.some((item) => item.id === model.id)
  customModels.value = exists
    ? customModels.value.map((item) => item.id === model.id ? model : item)
    : [model, ...customModels.value]
  syncModelDraft(model)
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

async function loadUserModels() {
  try {
    customModels.value = await fetchUserCustomModels()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载我的模型失败'
  }
}

async function loadTurnstileSettings() {
  try {
    turnstileSettings.value = await fetchPublicTurnstileSettings()
  } catch {
    turnstileSettings.value = {
      enabled: false,
      site_key: '',
      protect_login: false,
      protect_registration: false,
      protect_verification: false,
    }
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
    if (turnstileRequired.value && !turnstileToken.value) {
      throw new Error('请先完成人机验证')
    }
    const input = turnstileRequired.value
      ? { email: emailDraft.email, turnstile_token: turnstileToken.value }
      : { email: emailDraft.email }
    await startUserEmailVerification(input)
    statusMessage.value = '验证码已发送'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '发送验证码失败'
    resetTurnstileWidget()
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

function buildModelInput() {
  const input: CustomLLMModelInput = {
    provider_type: modelDraft.providerType.trim(),
    provider_id: modelDraft.providerId.trim(),
    model_id: modelDraft.modelId.trim(),
    display_name: modelDraft.displayName.trim(),
    base_url: modelDraft.baseURL.trim(),
    clear_base_url: Boolean(selectedModel.value && modelDraft.baseURL.trim() === ''),
    api_key: modelDraft.apiKey,
    scope: 'owner' as const,
    enabled: modelDraft.enabled,
    context_max_tokens: Number(modelDraft.contextMaxTokens) || 0,
    capabilities: {
      attachments: modelDraft.attachments,
    },
  }
  return input
}

function modelNeedsBaseURL(providerType: string) {
  return providerType === 'openai_completions'
}

function validateModelInput(input: ReturnType<typeof buildModelInput>) {
  if (!input.provider_type || !input.provider_id || !input.model_id || !input.display_name) {
    return 'Provider Type、Provider ID、Model ID 和显示名称必填'
  }
  if (modelNeedsBaseURL(input.provider_type) && !input.base_url && !input.clear_base_url) {
    return '当前 Provider Type 需要填写 Base URL'
  }
  if (!selectedModel.value && !input.api_key?.trim()) {
    return '创建模型时必须填写 API Key'
  }
  if ((input.context_max_tokens ?? 0) < 4) {
    return '上下文上限不能小于 4 tokens'
  }
  return ''
}

async function submitUserModel() {
  const input = buildModelInput()
  const validationError = validateModelInput(input)
  if (validationError) {
    errorMessage.value = validationError
    return
  }

  modelSaving.value = selectedModel.value ? 'model-update' : 'model-create'
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    const selected = selectedModel.value
    const saved = selected
      ? await updateUserCustomModel(selected.id, input)
      : await createUserCustomModel(input)
    upsertCustomModel(saved)
    statusMessage.value = selected ? '模型已更新' : '模型已创建'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '保存模型失败'
  } finally {
    modelSaving.value = ''
  }
}

async function testUserModel(model: CustomLLMModel) {
  modelSaving.value = `model-test:${model.id}`
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    await testUserCustomModel(model.id)
    statusMessage.value = '模型连通性测试通过'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '模型连通性测试失败'
  } finally {
    modelSaving.value = ''
  }
}

async function removeUserModel(model: CustomLLMModel) {
  modelSaving.value = `model-delete:${model.id}`
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    await deleteUserCustomModel(model.id)
    customModels.value = customModels.value.filter((item) => item.id !== model.id)
    if (selectedModelId.value === model.id) {
      resetModelDraft()
    }
    statusMessage.value = '模型已删除'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '删除模型失败'
  } finally {
    modelSaving.value = ''
  }
}

onMounted(() => {
  void loadProfile()
  void loadUserModels()
  void loadTurnstileSettings()
})

onBeforeUnmount(() => {
  removeTurnstileWidget()
})

watch([turnstileRequired, () => turnstileSettings.value.site_key], async () => {
  turnstileToken.value = ''
  await renderTurnstile()
}, { flush: 'post' })

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
    await loadProfileTurnstileScript()
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
  <main class="admin-workbench profile-workbench">
    <header class="admin-workbench-header">
      <div>
        <p class="eyebrow">Profile</p>
        <h1>个人设置</h1>
      </div>
      <div class="profile-header-actions">
        <a class="ghost-button" href="#profile-models" data-profile-models-link>我的模型</a>
        <RouterLink class="ghost-button" to="/chat">返回聊天</RouterLink>
      </div>
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
          <div v-if="turnstileRequired" ref="turnstileElement" class="turnstile-widget" aria-label="Cloudflare Turnstile"></div>
          <p v-if="turnstileLoadError" class="error-banner auth-error">{{ turnstileLoadError }}</p>
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

    <section id="profile-models" class="admin-section">
      <div class="admin-section-heading">
        <h2>我的模型</h2>
        <button class="ghost-button" type="button" @click="resetModelDraft">新增模型</button>
      </div>

      <div class="admin-table profile-model-table">
        <div class="admin-table-row profile-model-row profile-model-head">
          <span>名称</span>
          <span>Provider</span>
          <span>Context Max</span>
          <span>API Key</span>
          <span>状态</span>
          <span>操作</span>
        </div>
        <div
          v-for="model in customModels"
          :key="model.id"
          class="admin-table-row profile-model-row"
          :class="{ active: selectedModelId === model.id }"
        >
          <button class="profile-model-name" type="button" :data-user-model-row="model.id" @click="syncModelDraft(model)">
            {{ model.display_name }}
          </button>
          <span>{{ model.provider_type }} · {{ model.provider_id }} / {{ model.model_id }}</span>
          <span>{{ formatNumber(model.context_max_tokens) }}</span>
          <span>{{ model.api_key_masked || '未保存' }}</span>
          <span>{{ model.enabled ? '启用' : '停用' }}</span>
          <span class="profile-model-actions">
            <button class="ghost-button small" type="button" :data-user-model-test="model.id" @click="testUserModel(model)">测试</button>
            <button class="ghost-button small" type="button" :disabled="modelSaving === `model-delete:${model.id}`" @click="removeUserModel(model)">删除</button>
          </span>
        </div>
      </div>

      <form class="admin-form-grid" data-user-model-form @submit.prevent="submitUserModel">
        <label>
          <span class="field-label">Provider Type</span>
          <select v-model="modelDraft.providerType" class="text-input" data-user-model-provider-type required>
            <option value="openai_responses">OpenAI Responses</option>
            <option value="openai_completions">OpenAI Completions</option>
            <option value="google">Google</option>
          </select>
        </label>
        <label>
          <span class="field-label">Provider ID</span>
          <input v-model="modelDraft.providerId" class="text-input" data-user-model-provider-id required>
        </label>
        <label>
          <span class="field-label">Model ID</span>
          <input v-model="modelDraft.modelId" class="text-input" data-user-model-model-id required>
        </label>
        <label>
          <span class="field-label">显示名称</span>
          <input v-model="modelDraft.displayName" class="text-input" data-user-model-display-name required>
        </label>
        <label>
          <span class="field-label">Base URL</span>
          <input v-model="modelDraft.baseURL" class="text-input" data-user-model-base-url :required="modelNeedsBaseURL(modelDraft.providerType) && !selectedModel">
        </label>
        <label>
          <span class="field-label">API Key</span>
          <input v-model="modelDraft.apiKey" class="text-input" type="password" data-user-model-api-key :required="!selectedModel">
        </label>
        <label>
          <span class="field-label">Context Max Tokens</span>
          <input v-model.number="modelDraft.contextMaxTokens" class="text-input" type="number" min="4" data-user-model-context-max required>
        </label>
        <label class="admin-check-row">
          <input v-model="modelDraft.enabled" type="checkbox">
          <span>启用</span>
        </label>
        <label class="admin-check-row">
          <input v-model="modelDraft.attachments" type="checkbox">
          <span>支持附件</span>
        </label>
        <div class="admin-form-actions">
          <span class="admin-current-value">{{ modelFormTitle }}</span>
          <button class="primary-button" type="submit" :disabled="modelSaving === 'model-create' || modelSaving === 'model-update'">
            {{ selectedModel ? '保存模型' : '创建模型' }}
          </button>
        </div>
      </form>
    </section>
  </main>
</template>

<style scoped>
.profile-header-actions,
.profile-model-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.profile-model-head {
  font-weight: 700;
  color: #36535c;
}

.profile-model-row {
  grid-template-columns: minmax(120px, 0.9fr) minmax(170px, 1.4fr) minmax(100px, 0.65fr) minmax(110px, 0.7fr) minmax(70px, 0.45fr) minmax(140px, 0.8fr);
}

.profile-model-row.active {
  border-color: rgba(196, 88, 63, 0.24);
  background: linear-gradient(135deg, rgba(255, 238, 221, 0.94), rgba(238, 247, 249, 0.96));
}

.profile-model-name {
  border: 0;
  padding: 0;
  background: transparent;
  color: #203840;
  font: inherit;
  font-weight: 700;
  text-align: left;
  cursor: pointer;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ghost-button.small {
  min-height: 2rem;
  padding: 0.38rem 0.58rem;
}
</style>
