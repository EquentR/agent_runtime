<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import {
  createAdminCustomModel,
  deleteAdminCustomModel,
  fetchAdminCustomModels,
  fetchAdminModels,
  testAdminCustomModel,
  updateAdminCustomModel,
  updateAdminYAMLModel,
} from '../lib/api'
import type { CustomLLMModel, CustomLLMModelInput, ModelScope, YAMLModel, YAMLModelCatalog } from '../types/api'

type YAMLModelRow = {
  providerId: string
  providerName: string
  model: YAMLModel
}

const providerTypes = [
  { value: 'openai_responses', label: 'OpenAI Responses' },
  { value: 'openai_chat', label: 'OpenAI Chat（官方）' },
  { value: 'openai_completions', label: 'OpenAI Completions（兼容）' },
  { value: 'google', label: 'Google' },
]

const scopeOptions: Array<{ value: ModelScope; label: string }> = [
  { value: 'owner', label: '个人' },
  { value: 'admin', label: '管理员' },
  { value: 'global', label: '全局' },
]

const yamlCatalog = ref<YAMLModelCatalog | null>(null)
const customModels = ref<CustomLLMModel[]>([])
const selectedCustomId = ref('')
const loading = ref(false)
const saving = ref('')
const errorMessage = ref('')
const statusMessage = ref('')
const auditWarning = ref('')

const customDraft = reactive({
  ownerUserId: '',
  providerType: 'openai_responses',
  providerId: '',
  modelId: '',
  displayName: '',
  baseURL: '',
  apiKey: '',
  scope: 'admin' as ModelScope,
  enabled: true,
  contextMaxTokens: 32768,
  attachments: false,
})

const yamlRows = computed<YAMLModelRow[]>(() =>
  yamlCatalog.value?.providers.flatMap((provider) =>
    provider.models.map((model) => ({
      providerId: provider.id,
      providerName: provider.name,
      model,
    })),
  ) ?? [],
)

const showDialog = ref(false)
const selectedCustom = computed(() => customModels.value.find((model) => model.id === selectedCustomId.value) ?? null)
const customFormTitle = computed(() => selectedCustom.value ? `编辑模型：${selectedCustom.value.display_name}` : '新增自定义模型')

function formatNumber(value: number | undefined) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return '-'
  }
  return value.toLocaleString('en-US')
}

function formatYAMLContextMax(model: YAMLModel) {
  const max = model.context?.max
  if (typeof max !== 'number' || !Number.isFinite(max) || max <= 0) {
    return '使用系统默认上下文'
  }
  return formatNumber(max)
}

function syncCustomDraft(model: CustomLLMModel) {
  selectedCustomId.value = model.id
  customDraft.ownerUserId = model.owner_user_id ? String(model.owner_user_id) : ''
  customDraft.providerType = model.provider_type || 'openai_responses'
  customDraft.providerId = model.provider_id
  customDraft.modelId = model.model_id
  customDraft.displayName = model.display_name
  customDraft.baseURL = model.base_url
  customDraft.apiKey = ''
  customDraft.scope = model.scope
  customDraft.enabled = model.enabled
  customDraft.contextMaxTokens = model.context_max_tokens || 32768
  customDraft.attachments = model.capabilities.attachments
}

function openCreateDialog() {
  resetCustomDraft()
  errorMessage.value = ''
  statusMessage.value = ''
  showDialog.value = true
}

function openEditDialog(model: CustomLLMModel) {
  syncCustomDraft(model)
  errorMessage.value = ''
  statusMessage.value = ''
  showDialog.value = true
}

function closeDialog() {
  showDialog.value = false
  resetCustomDraft()
}

function resetCustomDraft() {
  selectedCustomId.value = ''
  customDraft.ownerUserId = ''
  customDraft.providerType = 'openai_responses'
  customDraft.providerId = ''
  customDraft.modelId = ''
  customDraft.displayName = ''
  customDraft.baseURL = ''
  customDraft.apiKey = ''
  customDraft.scope = 'admin'
  customDraft.enabled = true
  customDraft.contextMaxTokens = 32768
  customDraft.attachments = false
}

function replaceYAMLModel(providerId: string, nextModel: YAMLModel) {
  if (!yamlCatalog.value) {
    return
  }
  yamlCatalog.value = {
    ...yamlCatalog.value,
    providers: yamlCatalog.value.providers.map((provider) => {
      if (provider.id !== providerId) {
        return provider
      }
      return {
        ...provider,
        models: provider.models.map((model) => model.id === nextModel.id ? nextModel : model),
      }
    }),
  }
}

function upsertCustomModel(model: CustomLLMModel) {
  const exists = customModels.value.some((item) => item.id === model.id)
  customModels.value = exists
    ? customModels.value.map((item) => item.id === model.id ? model : item)
    : [model, ...customModels.value]
  syncCustomDraft(model)
}

async function loadModels() {
  loading.value = true
  errorMessage.value = ''
  try {
    const [yaml, custom] = await Promise.all([
      fetchAdminModels(),
      fetchAdminCustomModels(),
    ])
    yamlCatalog.value = yaml
    customModels.value = custom
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载模型配置失败'
  } finally {
    loading.value = false
  }
}

async function updateYAML(row: YAMLModelRow, patch: { enabled?: boolean; scope?: ModelScope }) {
  saving.value = `yaml:${row.providerId}:${row.model.id}`
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    const updated = await updateAdminYAMLModel(row.providerId, row.model.id, patch)
    replaceYAMLModel(row.providerId, updated)
    statusMessage.value = 'YAML 模型配置已更新'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '更新 YAML 模型失败'
  } finally {
    saving.value = ''
  }
}

function buildCustomInput() {
  const input: CustomLLMModelInput = {
    provider_type: customDraft.providerType.trim(),
    provider_id: customDraft.providerId.trim(),
    model_id: customDraft.modelId.trim(),
    display_name: customDraft.displayName.trim(),
    base_url: customDraft.baseURL.trim(),
    clear_base_url: Boolean(selectedCustom.value && customDraft.baseURL.trim() === ''),
    api_key: customDraft.apiKey,
    scope: customDraft.scope,
    enabled: customDraft.enabled,
    context_max_tokens: Number(customDraft.contextMaxTokens) || 0,
    capabilities: {
      attachments: customDraft.attachments,
    },
  }
  const ownerUserID = Number(customDraft.ownerUserId)
  if (!selectedCustom.value || customDraft.ownerUserId.trim()) {
    input.owner_user_id = Number.isInteger(ownerUserID) && ownerUserID > 0 ? ownerUserID : 0
  }
  return input
}

function validateCustomInput(input: ReturnType<typeof buildCustomInput>) {
  if (!input.provider_type || !input.provider_id || !input.model_id || !input.display_name) {
    return 'Provider Type、Provider ID、Model ID 和显示名称必填'
  }
  if (!selectedCustom.value && !input.api_key?.trim()) {
    return '创建模型时必须填写 API Key'
  }
  const ownerUserID = Number(customDraft.ownerUserId)
  if (customDraft.ownerUserId.trim() && (!Number.isInteger(ownerUserID) || ownerUserID <= 0)) {
    return 'Owner User ID 必须是正整数'
  }
  if ((input.context_max_tokens ?? 0) < 4) {
    return '上下文上限不能小于 4 tokens'
  }
  return ''
}

function currentSessionUserID() {
  try {
    const raw = localStorage.getItem('agent-runtime.user')
    if (!raw) {
      return 0
    }
    const parsed = JSON.parse(raw) as { id?: unknown }
    const id = typeof parsed.id === 'number' && Number.isFinite(parsed.id) ? parsed.id : 0
    return id
  } catch {
    return 0
  }
}

async function submitCustomModel() {
  const input = buildCustomInput()
  const validationError = validateCustomInput(input)
  if (validationError) {
    errorMessage.value = validationError
    return
  }

  saving.value = selectedCustom.value ? 'custom-update' : 'custom-create'
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    const selected = selectedCustom.value
    const saved = selected
      ? await updateAdminCustomModel(selected.id, input)
      : await createAdminCustomModel(input)
    upsertCustomModel(saved)
    statusMessage.value = selected ? '自定义模型已更新' : '自定义模型已创建'
    closeDialog()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '保存自定义模型失败'
  } finally {
    saving.value = ''
  }
}

async function removeCustomModel(model: CustomLLMModel) {
  saving.value = `delete:${model.id}`
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    await deleteAdminCustomModel(model.id)
    customModels.value = customModels.value.filter((item) => item.id !== model.id)
    if (selectedCustomId.value === model.id) {
      resetCustomDraft()
    }
    statusMessage.value = '自定义模型已删除'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '删除自定义模型失败'
  } finally {
    saving.value = ''
  }
}

async function testCustom(model: CustomLLMModel) {
  const currentUserID = currentSessionUserID()
  auditWarning.value = model.owner_user_id > 0 && model.owner_user_id !== currentUserID
    ? '正在测试其他用户的模型，操作会写入后台审计'
    : ''
  saving.value = `test:${model.id}`
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    await testAdminCustomModel(model.id)
    statusMessage.value = '模型连通性测试通过'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '模型连通性测试失败'
  } finally {
    saving.value = ''
  }
}

onMounted(() => {
  void loadModels()
})
</script>

<template>
  <main class="admin-workbench">
    <header class="admin-workbench-header">
      <div>
        <p class="eyebrow">Models</p>
        <h1>模型管理</h1>
      </div>
      <span class="status-pill" :class="{ loading }">{{ loading ? '加载中' : `${customModels.length} 自定义模型` }}</span>
    </header>

    <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>
    <p v-if="statusMessage" class="admin-inline-success">{{ statusMessage }}</p>
    <p v-if="auditWarning" class="admin-warning-banner">{{ auditWarning }}</p>

    <section class="admin-section">
      <div class="admin-section-heading">
        <h2>YAML 配置模型</h2>
        <span class="admin-current-value">默认：{{ yamlCatalog?.default_provider_id || '-' }} / {{ yamlCatalog?.default_model_id || '-' }}</span>
      </div>
      <div class="admin-table admin-model-table">
        <div class="admin-table-row admin-model-yaml-row admin-table-head">
          <span>Provider</span>
          <span>Model</span>
          <span>Context Max</span>
          <span>Enabled</span>
          <span>Scope</span>
        </div>
        <div
          v-for="row in yamlRows"
          :key="`${row.providerId}:${row.model.id}`"
          class="admin-table-row admin-model-yaml-row"
        >
          <span>{{ row.providerName }}</span>
          <span>{{ row.model.name }}</span>
          <span>{{ formatYAMLContextMax(row.model) }}</span>
          <label class="admin-check-row compact-check">
            <input
              type="checkbox"
              :checked="row.model.enabled"
              :data-yaml-enabled="`${row.providerId}:${row.model.id}`"
              @change="updateYAML(row, { enabled: ($event.target as HTMLInputElement).checked })"
            >
            <span>{{ row.model.enabled ? '启用' : '停用' }}</span>
          </label>
          <select
            class="text-input"
            :value="row.model.scope"
            :data-yaml-scope="`${row.providerId}:${row.model.id}`"
            @change="updateYAML(row, { scope: ($event.target as HTMLSelectElement).value as ModelScope })"
          >
            <option v-for="scope in scopeOptions" :key="scope.value" :value="scope.value">{{ scope.label }}</option>
          </select>
        </div>
      </div>
    </section>

    <section class="admin-section">
      <div class="admin-section-heading">
        <h2>自定义模型</h2>
        <button class="ghost-button" type="button" data-admin-model-create @click="openCreateDialog">新增模型</button>
      </div>
      <div class="admin-table admin-model-table">
        <div class="admin-table-row admin-custom-model-row admin-table-head">
          <span>名称</span>
          <span>Owner</span>
          <span>Provider</span>
          <span>Scope</span>
          <span>Context Max</span>
          <span>API Key</span>
          <span>操作</span>
        </div>
        <div
          v-for="model in customModels"
          :key="model.id"
          class="admin-table-row admin-custom-model-row"
        >
          <span class="admin-model-name">{{ model.display_name }}</span>
          <span>{{ model.owner_user_id || '-' }}</span>
          <span>{{ model.provider_type }} · {{ model.provider_id }} / {{ model.model_id }}</span>
          <span>{{ model.scope }} · {{ model.enabled ? '启用' : '停用' }}</span>
          <span>{{ formatNumber(model.context_max_tokens) }}</span>
          <span>{{ model.api_key_masked || '未保存' }}</span>
          <span class="admin-row-actions">
            <button class="ghost-button small" type="button" :data-admin-custom-test="model.id" @click="testCustom(model)">测试</button>
            <button class="ghost-button small" type="button" :data-admin-custom-row="model.id" @click="openEditDialog(model)">编辑</button>
            <button class="ghost-button small" type="button" :disabled="saving === `delete:${model.id}`" @click="removeCustomModel(model)">删除</button>
          </span>
        </div>
      </div>
    </section>

    <div v-if="showDialog" class="admin-dialog-overlay" @click.self="closeDialog">
      <div class="admin-dialog" role="dialog" :aria-label="customFormTitle">
        <div class="admin-dialog-header">
          <h2>{{ customFormTitle }}</h2>
          <button class="admin-dialog-close" type="button" aria-label="关闭" @click="closeDialog">✕</button>
        </div>
        <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>
        <p class="admin-dialog-hint">输出预算默认不超过 8k，输入预算由 context 动态计算</p>
        <form class="admin-form-grid" data-admin-model-form @submit.prevent="submitCustomModel">
          <label>
            <span class="field-label">Owner User ID</span>
            <input v-model="customDraft.ownerUserId" class="text-input" data-admin-model-owner-user-id inputmode="numeric" placeholder="留空则使用当前管理员">
          </label>
          <label>
            <span class="field-label">Provider Type</span>
            <select v-model="customDraft.providerType" class="text-input" data-admin-model-provider-type required>
              <option v-for="providerType in providerTypes" :key="providerType.value" :value="providerType.value">
                {{ providerType.label }}
              </option>
            </select>
          </label>
          <label>
            <span class="field-label">Provider ID</span>
            <input v-model="customDraft.providerId" class="text-input" data-admin-model-provider-id required>
          </label>
          <label>
            <span class="field-label">Model ID</span>
            <input v-model="customDraft.modelId" class="text-input" data-admin-model-model-id required>
          </label>
          <label>
            <span class="field-label">显示名称</span>
            <input v-model="customDraft.displayName" class="text-input" data-admin-model-display-name required>
          </label>
          <label>
            <span class="field-label">Base URL</span>
            <input v-model="customDraft.baseURL" class="text-input" data-admin-model-base-url>
          </label>
          <label>
            <span class="field-label">API Key</span>
            <input v-model="customDraft.apiKey" class="text-input" type="password" data-admin-model-api-key :required="!selectedCustom">
          </label>
          <label>
            <span class="field-label">Scope</span>
            <select v-model="customDraft.scope" class="text-input" data-admin-model-scope>
              <option v-for="scope in scopeOptions" :key="scope.value" :value="scope.value">{{ scope.label }}</option>
            </select>
          </label>
          <label>
            <span class="field-label">Context Max Tokens</span>
            <input v-model.number="customDraft.contextMaxTokens" class="text-input" type="number" min="4" data-admin-model-context-max required>
          </label>
          <label class="admin-check-row">
            <input v-model="customDraft.enabled" type="checkbox">
            <span>启用</span>
          </label>
          <label class="admin-check-row">
            <input v-model="customDraft.attachments" type="checkbox">
            <span>支持附件</span>
          </label>
          <div class="admin-form-actions">
            <button class="ghost-button admin-form-button" type="button" @click="closeDialog">取消</button>
            <button class="primary-button admin-form-button" type="submit" :disabled="saving === 'custom-create' || saving === 'custom-update'">
              {{ selectedCustom ? '保存模型' : '创建模型' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </main>
</template>

<style scoped>
.admin-table-head {
  font-weight: 700;
  color: #36535c;
}

.admin-model-yaml-row {
  grid-template-columns: minmax(110px, 0.7fr) minmax(160px, 1.2fr) minmax(120px, 0.75fr) minmax(120px, 0.65fr) minmax(140px, 0.75fr);
}

.admin-custom-model-row {
  grid-template-columns: minmax(120px, 0.9fr) minmax(70px, 0.45fr) minmax(180px, 1.45fr) minmax(110px, 0.65fr) minmax(110px, 0.65fr) minmax(120px, 0.7fr) minmax(150px, 0.8fr);
}

.admin-custom-model-row.active {
  border-color: rgba(196, 88, 63, 0.24);
  background: linear-gradient(135deg, rgba(255, 238, 221, 0.94), rgba(238, 247, 249, 0.96));
}

.admin-model-name {
  font-weight: 700;
  color: #203840;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.admin-text-button {
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

.admin-row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

.ghost-button.small {
  min-height: 2rem;
  padding: 0.38rem 0.58rem;
}

.admin-dialog-hint {
  font-size: 0.82rem;
  color: #5a7a84;
  margin: 0 0 0.5rem;
}

.admin-warning-banner {
  border: 1px solid rgba(196, 88, 63, 0.22);
  background: rgba(255, 238, 221, 0.9);
  color: #7b3a2a;
  border-radius: 12px;
  padding: 0.68rem 0.82rem;
  font-weight: 700;
}
</style>
