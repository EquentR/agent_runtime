<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Download, Link, Refresh, RefreshLeft } from '@element-plus/icons-vue'
import MarkdownIt from 'markdown-it'

import {
  authorizeAdminUpdate,
  checkAdminUpdate,
  fetchAdminUpdateStatus,
  forceInstallAdminUpdate,
  installAdminUpdate,
  rollbackAdminUpdate,
} from '../lib/api'
import type { AdminUpdateStatus } from '../types/api'

const markdown = new MarkdownIt({ html: false, breaks: true, linkify: true })
const status = ref<AdminUpdateStatus | null>(null)
const loading = ref(false)
const busy = ref('')
const errorMessage = ref('')
const reconnecting = ref(false)
const releaseOpen = ref(false)
const installOpen = ref(false)
const rollbackOpen = ref(false)
const password = ref('')
const backupMode = ref<'compact' | 'full'>('compact')
const forceMode = ref(false)
const forceConfirmed = ref(false)
let pollTimer: number | undefined

const activePhases = new Set(['downloading', 'verifying', 'staged', 'draining', 'backing_up', 'replacing', 'restarting', 'health_check', 'rolling_back'])
const phaseLabels: Record<string, string> = {
  idle: '空闲', checking: '检查中', available: '有可用更新', downloading: '下载中', verifying: '校验中', staged: '已准备',
  draining: '等待任务完成', backing_up: '备份中', replacing: '替换程序', restarting: '重启中', health_check: '健康检查',
  succeeded: '升级成功', failed: '升级失败', rolling_back: '回滚中', rolled_back: '已回滚', recovery_required: '需要人工恢复',
}

const phaseLabel = computed(() => phaseLabels[status.value?.state.phase ?? 'idle'] ?? status.value?.state.phase ?? '未知')
const releaseNotes = computed(() => markdown.render(status.value?.latest?.body || '暂无 Release Notes。'))
const nativeInstall = computed(() => Boolean(status.value?.capable && status.value?.update_available))
const operationActive = computed(() => activePhases.has(status.value?.state.phase ?? ''))
const showRollback = computed(() => Boolean(status.value?.state.backup_id && ['succeeded', 'available', 'idle'].includes(status.value?.state.phase ?? '')))
const showForceInstall = computed(() => Boolean(status.value?.force_install_allowed))
const selectedBackupBytes = computed(() => status.value?.preflight
  ? backupMode.value === 'full' ? status.value.preflight.estimated_full_backup_bytes : status.value.preflight.estimated_compact_backup_bytes
  : 0)

function newOperationID() {
  return crypto.randomUUID().replaceAll('-', '')
}

function openInstall(force: boolean) {
  forceMode.value = force
  password.value = ''
  forceConfirmed.value = false
  installOpen.value = true
}

function openRollback() {
  password.value = ''
  rollbackOpen.value = true
}

function clearPassword() {
  password.value = ''
  forceConfirmed.value = false
}

function formatBytes(value?: number) {
  if (!value || value < 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let amount = value
  let index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index += 1
  }
  return `${amount.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function schedulePoll(delay = 2000) {
  window.clearTimeout(pollTimer)
  pollTimer = window.setTimeout(() => void loadStatus(true), delay)
}

function formatCheckedAt(value?: string) {
  if (!value) return '—'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) || parsed.getFullYear() < 2000 ? '—' : parsed.toLocaleString()
}

async function loadStatus(poll = false) {
  if (!poll) loading.value = true
  try {
    status.value = await fetchAdminUpdateStatus()
    reconnecting.value = false
    if (operationActive.value) schedulePoll()
  } catch (error) {
    if (poll || operationActive.value) {
      reconnecting.value = true
      schedulePoll()
    } else {
      errorMessage.value = error instanceof Error ? error.message : '加载升级状态失败'
    }
  } finally {
    loading.value = false
  }
}

async function checkNow() {
  busy.value = 'check'
  errorMessage.value = ''
  try {
    status.value = await checkAdminUpdate()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '检查更新失败'
  } finally {
    busy.value = ''
  }
}

async function install() {
  const target = status.value?.latest?.tag_name
  if (!target || !password.value) return
  busy.value = 'install'
  errorMessage.value = ''
  try {
    const action = forceMode.value ? 'force_install' : 'install'
    const authorization = await authorizeAdminUpdate({ password: password.value, action, target })
    const input = { authorization_token: authorization.authorization_token, operation_id: newOperationID(), target, backup_mode: backupMode.value }
    const operation = forceMode.value ? await forceInstallAdminUpdate(input) : await installAdminUpdate(input)
    if (status.value) status.value.state = operation
    password.value = ''
    installOpen.value = false
    reconnecting.value = true
    schedulePoll(750)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '启动升级失败'
    await loadStatus(true)
  } finally {
    busy.value = ''
  }
}

async function rollback() {
  const target = status.value?.state.backup_id
  if (!target || !password.value) return
  busy.value = 'rollback'
  errorMessage.value = ''
  try {
    const authorization = await authorizeAdminUpdate({ password: password.value, action: 'rollback', target })
    const operation = await rollbackAdminUpdate({ authorization_token: authorization.authorization_token, operation_id: newOperationID(), target })
    if (status.value) status.value.state = operation
    password.value = ''
    rollbackOpen.value = false
    reconnecting.value = true
    schedulePoll(750)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '启动回滚失败'
  } finally {
    busy.value = ''
  }
}

onMounted(() => void loadStatus())
onBeforeUnmount(() => window.clearTimeout(pollTimer))
</script>

<template>
  <main class="admin-workbench update-workbench">
    <header class="admin-workbench-header">
      <div><p class="eyebrow">Update</p><h1>系统升级</h1></div>
      <span class="status-pill" :class="{ loading: loading || operationActive }">{{ reconnecting ? '正在重连' : phaseLabel }}</span>
    </header>

    <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>
    <p v-if="reconnecting" class="update-reconnect">服务正在重启，连接恢复后将继续显示升级进度。</p>

    <section class="admin-section">
      <div class="admin-section-heading">
        <h2>版本状态</h2>
        <div class="update-actions">
          <button class="ghost-button icon-button" type="button" title="立即检查" aria-label="立即检查" :disabled="busy !== ''" @click="checkNow"><Refresh /></button>
          <button v-if="status?.latest" class="ghost-button" type="button" @click="releaseOpen = true">查看 Release</button>
          <button v-if="nativeInstall" class="primary-button update-command" type="button" :disabled="busy !== '' || operationActive" @click="openInstall(false)"><Download /><span>升级到 {{ status?.latest?.tag_name }}</span></button>
        </div>
      </div>
      <dl class="update-facts">
        <div><dt>当前版本</dt><dd>{{ status?.current.version || '—' }}</dd></div>
        <div><dt>Commit</dt><dd>{{ status?.current.commit || '—' }}</dd></div>
        <div><dt>发行方式</dt><dd>{{ status?.current.distribution || '—' }}</dd></div>
        <div><dt>运行平台</dt><dd>{{ status ? `${status.current.goos}/${status.current.goarch}` : '—' }}</dd></div>
        <div><dt>最新版本</dt><dd>{{ status?.latest?.tag_name || '—' }}</dd></div>
        <div><dt>上次检查</dt><dd>{{ formatCheckedAt(status?.checked_at) }}</dd></div>
        <div><dt>运行模式</dt><dd>{{ status?.runtime_mode || '—' }}</dd></div>
        <div><dt>签名状态</dt><dd>{{ status?.signature_status || '—' }}</dd></div>
      </dl>
      <p v-if="status && !status.capable" class="update-notice">{{ status.capability_reason || '当前部署仅支持更新提示。' }}</p>
      <p v-if="status?.cache_stale" class="update-notice">当前显示最近一次成功检查的缓存结果。</p>
    </section>

    <section class="admin-section">
      <div class="admin-section-heading">
        <h2>升级进度</h2>
        <button v-if="showRollback" class="ghost-button update-command" type="button" @click="openRollback"><RefreshLeft /><span>回滚最近升级</span></button>
        <button v-if="showForceInstall" class="primary-button danger-button update-command" type="button" @click="openInstall(true)"><Download /><span>Force continue</span></button>
      </div>
      <div class="update-progress"><strong>{{ phaseLabel }}</strong><span>{{ status?.state.target_version || status?.latest?.tag_name || '暂无目标版本' }}</span></div>
      <p v-if="status?.state.error" class="error-banner">{{ status.state.error }}</p>
    </section>

    <el-dialog v-model="releaseOpen" width="min(720px, 92vw)" title="Release Notes" append-to-body>
      <div class="release-notes" v-html="releaseNotes"></div>
      <template #footer><a v-if="status?.latest?.html_url" class="ghost-button update-command" :href="status.latest.html_url" target="_blank" rel="noopener noreferrer"><Link /><span>在 GitHub 查看</span></a></template>
    </el-dialog>

    <el-dialog v-model="installOpen" width="min(520px, 92vw)" :title="forceMode ? '强制继续升级' : '确认升级'" append-to-body @closed="clearPassword">
      <form class="admin-form-grid" @submit.prevent="install">
        <p>目标版本：<strong>{{ status?.latest?.tag_name }}</strong></p>
        <label><span class="field-label">备份模式</span><el-radio-group v-model="backupMode"><el-radio-button value="compact">精简备份</el-radio-button><el-radio-button value="full">完整备份</el-radio-button></el-radio-group></label>
        <label><span class="field-label">管理员密码</span><input v-model="password" class="text-input" type="password" autocomplete="current-password" required></label>
        <dl v-if="status?.preflight" class="update-preflight">
          <div><dt>预计下载</dt><dd>{{ formatBytes(status.preflight.estimated_download_bytes) }}</dd></div>
          <div><dt>本次备份</dt><dd>{{ formatBytes(selectedBackupBytes) }}</dd></div>
          <div><dt>可用空间</dt><dd>{{ formatBytes(status.preflight.available_bytes) }}</dd></div>
          <div><dt>运行任务</dt><dd>{{ status.preflight.active_task_count }}</dd></div>
        </dl>
        <p v-if="status?.preflight" class="update-dialog-note">升级期间将暂停写操作，等待运行任务结束后重启服务。</p>
        <p v-if="forceMode" class="update-force-warning">强制升级会中断仍在运行的任务，未提交的任务数据可能丢失。</p>
        <label v-if="forceMode" class="force-confirmation"><input v-model="forceConfirmed" type="checkbox">我确认继续并承担中断任务风险</label>
        <div class="admin-form-actions"><button class="primary-button" type="submit" :disabled="busy === 'install' || !password || (forceMode && !forceConfirmed)">{{ busy === 'install' ? '准备升级中' : '确认升级' }}</button></div>
      </form>
    </el-dialog>

    <el-dialog v-model="rollbackOpen" width="min(520px, 92vw)" title="回滚最近升级" append-to-body @closed="clearPassword">
      <form class="admin-form-grid" @submit.prevent="rollback">
        <p>将恢复备份 {{ status?.state.backup_id }}，备份之后的数据可能丢失。</p>
        <label><span class="field-label">管理员密码</span><input v-model="password" class="text-input" type="password" autocomplete="current-password" required></label>
        <div class="admin-form-actions"><button class="primary-button danger-button" type="submit" :disabled="busy === 'rollback' || !password">{{ busy === 'rollback' ? '准备回滚中' : '确认回滚' }}</button></div>
      </form>
    </el-dialog>
  </main>
</template>

<style scoped>
.update-actions, .update-command { display: flex; align-items: center; }
.update-workbench .admin-section { padding: 1.1rem 0; border: 0; border-top: 1px solid rgba(25, 50, 59, 0.1); border-radius: 0; background: transparent; box-shadow: none; }
.update-actions { flex-wrap: wrap; justify-content: flex-end; gap: 0.55rem; }
.update-command { gap: 0.42rem; }
.update-command svg { width: 1rem; height: 1rem; }
.update-facts { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0; margin: 0; border-top: 1px solid rgba(25, 50, 59, 0.1); }
.update-facts > div { min-width: 0; padding: 1rem 0; border-bottom: 1px solid rgba(25, 50, 59, 0.1); }
.update-facts dt { color: var(--muted-text); font-size: 0.78rem; }
.update-facts dd { margin: 0.3rem 0 0; overflow-wrap: anywhere; font-weight: 650; }
.update-notice, .update-reconnect { padding: 0.75rem 0; color: var(--muted-text); }
.update-progress { display: flex; justify-content: space-between; gap: 1rem; padding: 0.9rem 0; border-block: 1px solid rgba(25, 50, 59, 0.1); }
.release-notes { max-height: min(62vh, 620px); overflow: auto; line-height: 1.68; overflow-wrap: anywhere; }
.release-notes :deep(img) { max-width: 100%; }
.release-notes :deep(pre) { overflow: auto; padding: 0.75rem; background: rgba(25, 50, 59, 0.06); border-radius: 6px; }
.danger-button { background: #b42318; border-color: #b42318; }
.update-preflight { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.65rem; margin: 0; }
.update-preflight > div { padding: 0.65rem; background: rgba(25, 50, 59, 0.05); border-radius: 6px; }
.update-preflight dt { color: var(--muted-text); font-size: 0.78rem; }
.update-preflight dd { margin: 0.2rem 0 0; font-weight: 650; }
.update-dialog-note, .update-force-warning { color: var(--muted-text); line-height: 1.55; }
.update-force-warning { color: #b42318; font-weight: 650; }
.force-confirmation { display: flex; align-items: flex-start; gap: 0.5rem; line-height: 1.45; }
@media (max-width: 760px) {
  .admin-section-heading { align-items: flex-start; flex-direction: column; }
  .update-actions { justify-content: flex-start; width: 100%; }
  .update-facts { grid-template-columns: 1fr 1fr; }
}
@media (max-width: 480px) {
  .update-facts { grid-template-columns: 1fr; }
  .update-actions .primary-button { width: 100%; justify-content: center; }
}
</style>
