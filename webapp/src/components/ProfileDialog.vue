<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'

import OpenSourceLicensesPanel from './OpenSourceLicensesPanel.vue'
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

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

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
const showModelDialog = ref(false)
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

function openModelCreate() {
  resetModelDraft()
  errorMessage.value = ''
  statusMessage.value = ''
  showModelDialog.value = true
}

function openModelEdit(model: CustomLLMModel) {
  syncModelDraft(model)
  errorMessage.value = ''
  statusMessage.value = ''
  showModelDialog.value = true
}

function closeModelDialog() {
  showModelDialog.value = false
  resetModelDraft()
}

function upsertCustomModel(model: CustomLLMModel) {
  const exists = customModels.value.some((item) => item.id === model.id)
  customModels.value = exists
    ? customModels.value.map((item) => item.id === model.id ? model : item)
    : [model, ...customModels.value]
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
    resetTurnstileWidget()
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
    closeModelDialog()
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

onBeforeUnmount(() => {
  removeTurnstileWidget()
})

watch([turnstileRequired, () => turnstileSettings.value.site_key], async () => {
  turnstileToken.value = ''
  await renderTurnstile()
}, { flush: 'post' })

// Load data when dialog opens for the first time
watch(() => props.open, async (isOpen) => {
  if (isOpen && !profile.value) {
    void loadProfile()
    void loadUserModels()
    void loadTurnstileSettings()
  }
})

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
  <Teleport to="body">
    <Transition name="profile-dialog-fade">
      <div v-if="open" class="profile-dialog-overlay" role="dialog" aria-label="个人设置" @click.self="emit('close')">
        <div class="profile-dialog-shell">
          <div class="profile-dialog-header">
            <div>
              <p class="eyebrow">Profile</p>
              <h1>个人设置</h1>
            </div>
            <button class="admin-dialog-close" type="button" aria-label="关闭" @click="emit('close')">✕</button>
          </div>

          <div class="profile-dialog-body">
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
                  <button class="primary-button admin-form-button" type="submit" :disabled="savingProfile">
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
                    <button class="primary-button admin-form-button" type="submit" :disabled="sendingEmailCode">
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
                    <button class="primary-button admin-form-button" type="submit" :disabled="confirmingEmail">
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
                  <button class="primary-button admin-form-button" type="submit" :disabled="savingPassword">
                    {{ savingPassword ? '保存中' : '修改密码' }}
                  </button>
                </div>
              </form>
            </section>

            <section class="admin-section">
              <div class="admin-section-heading">
                <h2>我的模型</h2>
                <button class="ghost-button profile-action-button" type="button" @click="openModelCreate">新增模型</button>
              </div>

              <div class="admin-table profile-model-table">
                <div class="admin-table-row profile-model-row profile-model-head">
                  <span>名称</span>
                  <span>状态</span>
                  <span>操作</span>
                </div>
                <div
                  v-for="model in customModels"
                  :key="model.id"
                  class="admin-table-row profile-model-row"
                >
                  <span class="profile-model-name-cell">{{ model.display_name }}</span>
                  <span :class="model.enabled ? 'profile-model-enabled' : 'profile-model-disabled'">{{ model.enabled ? '启用' : '停用' }}</span>
                  <span class="profile-model-actions">
                    <button class="ghost-button small" type="button" :data-user-model-row="model.id" @click="openModelEdit(model)">编辑</button>
                    <button class="ghost-button small" type="button" :data-user-model-test="model.id" @click="testUserModel(model)">测试</button>
                    <button class="ghost-button small" type="button" :disabled="modelSaving === `model-delete:${model.id}`" @click="removeUserModel(model)">删除</button>
                  </span>
                </div>
                <div v-if="customModels.length === 0" class="admin-table-row">
                  <span class="admin-empty" style="grid-column: 1/-1">暂无自定义模型，点击"新增模型"添加。</span>
                </div>
              </div>
            </section>

            <OpenSourceLicensesPanel />
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>

  <!-- Model create/edit sub-dialog, teleported to body to escape profile panel stacking context -->
  <Teleport to="body">
    <div v-if="showModelDialog" class="profile-model-dialog-overlay" @click.self="closeModelDialog">
      <div class="admin-dialog" role="dialog" :aria-label="modelFormTitle">
        <div class="admin-dialog-header">
          <h2>{{ modelFormTitle }}</h2>
          <button class="admin-dialog-close" type="button" aria-label="关闭" @click="closeModelDialog">✕</button>
        </div>
        <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>
        <form class="admin-form-grid" data-user-model-form @submit.prevent="submitUserModel">
          <label>
            <span class="field-label">Provider Type</span>
            <select v-model="modelDraft.providerType" class="text-input" data-user-model-provider-type required>
              <option value="openai_responses">OpenAI Responses</option>
              <option value="openai_chat">OpenAI Chat（官方）</option>
              <option value="openai_completions">OpenAI Completions（兼容）</option>
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
            <span>支持图片输入(直传)</span>
          </label>
          <div class="admin-form-actions">
            <button class="ghost-button admin-form-button" type="button" @click="closeModelDialog">取消</button>
            <button class="primary-button admin-form-button" type="submit" :disabled="modelSaving === 'model-create' || modelSaving === 'model-update'">
              {{ selectedModel ? '保存模型' : '创建模型' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.profile-dialog-overlay {
  position: fixed;
  inset: 0;
  z-index: 900;
  background: rgba(15, 32, 38, 0.48);
  display: flex;
  align-items: stretch;
  justify-content: flex-end;
}

.profile-dialog-shell {
  width: min(680px, 100vw);
  height: 100%;
  background: #f5f0eb;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: -8px 0 32px rgba(15, 32, 38, 0.18);
}

.profile-dialog-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: 1.4rem 1.5rem 1rem;
  border-bottom: 1px solid rgba(15, 32, 38, 0.08);
  flex: 0 0 auto;
}

.profile-dialog-header h1 {
  font-size: 1.35rem;
  margin: 0;
}

.profile-dialog-header .eyebrow {
  margin: 0 0 0.15rem;
}

.profile-dialog-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding: 1rem 1.5rem 2rem;
  display: flex;
  flex-direction: column;
  gap: 0;
}

:deep(.admin-section) {
  margin: 2px 0;
}

.profile-action-button {
  padding: 0.38rem 0.75rem;
  font-size: 0.88rem;
}

.profile-model-head {
  font-weight: 700;
  color: #36535c;
}

.profile-model-table {
  overflow-x: auto;
}

.profile-model-row {
  grid-template-columns: minmax(100px, 1fr) minmax(50px, 70px) minmax(130px, auto);
}

.profile-model-enabled {
  color: #2e6658;
  font-size: 0.82rem;
  font-weight: 600;
}

.profile-model-disabled {
  color: #946b2d;
  font-size: 0.82rem;
  font-weight: 600;
}

.profile-model-name-cell {
  font-weight: 700;
  color: #203840;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.profile-model-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

.ghost-button.small {
  min-height: 2rem;
  padding: 0.38rem 0.58rem;
}

.profile-model-dialog-overlay {
  position: fixed;
  inset: 0;
  z-index: 950;
  display: grid;
  place-items: center;
  background: rgba(25, 50, 59, 0.35);
  padding: 1rem;
}

.profile-dialog-fade-enter-active,
.profile-dialog-fade-leave-active {
  transition: opacity 0.18s ease;
}

.profile-dialog-fade-enter-active .profile-dialog-shell,
.profile-dialog-fade-leave-active .profile-dialog-shell {
  transition: transform 0.22s cubic-bezier(0.4, 0, 0.2, 1);
}

.profile-dialog-fade-enter-from,
.profile-dialog-fade-leave-to {
  opacity: 0;
}

.profile-dialog-fade-enter-from .profile-dialog-shell,
.profile-dialog-fade-leave-to .profile-dialog-shell {
  transform: translateX(40px);
}

html.theme-teal .profile-dialog-shell {
  background: #eafaf8;
}

html.theme-teal-dark .profile-dialog-overlay {
  background: rgba(1, 12, 12, 0.72);
}

html.theme-teal-dark .profile-dialog-shell {
  background: rgba(8, 36, 34, 0.96);
  border-left: 1px solid rgba(125, 232, 221, 0.16);
  box-shadow: -12px 0 36px rgba(0, 0, 0, 0.36);
}

html.theme-teal-dark .profile-dialog-header {
  border-bottom-color: rgba(125, 232, 221, 0.12);
}

html.theme-teal-dark .profile-model-head,
html.theme-teal-dark .profile-model-name-cell {
  color: #f4fffd;
}

html.theme-teal-dark .profile-model-enabled {
  color: #8ffff0;
}

html.theme-teal-dark .profile-model-disabled {
  color: #ffd08f;
}

html.theme-teal-dark .profile-model-dialog-overlay {
  background: rgba(1, 12, 12, 0.62);
}

html.theme-teal-dark .profile-model-dialog-overlay .admin-dialog {
  background: rgba(9, 43, 40, 0.98);
  border-color: rgba(125, 232, 221, 0.16);
  color: #dffbf7;
}
</style>
