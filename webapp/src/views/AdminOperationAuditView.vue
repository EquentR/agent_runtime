<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

import { fetchAdminAuditEvents } from '../lib/api'
import { formatCompactTimestamp } from '../lib/time'
import type { AdminAuditEvent, AdminAuditEventFilter } from '../types/api'

const events = ref<AdminAuditEvent[]>([])
const loading = ref(false)
const errorMessage = ref('')

const filters = reactive({
  action: '',
  targetKind: '',
  actorUsername: '',
  startDate: '',
  endDate: '',
})

function buildFilter(): AdminAuditEventFilter {
  const filter: AdminAuditEventFilter = { limit: 100 }
  if (filters.action.trim()) filter.action = filters.action.trim()
  if (filters.targetKind.trim()) filter.target_kind = filters.targetKind.trim()
  if (filters.actorUsername.trim()) filter.actor_username = filters.actorUsername.trim()
  if (filters.startDate) filter.created_after = filters.startDate
  if (filters.endDate) filter.created_before = filters.endDate
  return filter
}

function formatJSON(value: unknown) {
  if (value == null || value === '') return '-'
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

async function loadEvents() {
  loading.value = true
  errorMessage.value = ''
  try {
    events.value = await fetchAdminAuditEvents(buildFilter())
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载后台操作审计失败'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void loadEvents()
})
</script>

<template>
  <main class="admin-workbench">
    <header class="admin-workbench-header">
      <div>
        <p class="eyebrow">Operations</p>
        <h1>后台操作审计</h1>
      </div>
      <span class="status-pill" :class="{ loading }">{{ loading ? '加载中' : `${events.length} 事件` }}</span>
    </header>

    <p v-if="errorMessage" class="error-banner">{{ errorMessage }}</p>

    <section class="admin-section">
      <form class="admin-filter-bar wide" data-admin-audit-filter-form @submit.prevent="loadEvents">
        <input v-model="filters.action" class="text-input" data-admin-audit-action-input placeholder="Action">
        <input v-model="filters.targetKind" class="text-input" data-admin-audit-target-kind-input placeholder="Target kind">
        <input v-model="filters.actorUsername" class="text-input" data-admin-audit-actor-input placeholder="Actor">
        <input v-model="filters.startDate" class="text-input" type="date" data-admin-audit-start-date-input>
        <input v-model="filters.endDate" class="text-input" type="date" data-admin-audit-end-date-input>
        <button class="primary-button" type="submit">筛选</button>
      </form>
    </section>

    <section class="admin-section admin-table-panel">
      <div class="admin-section-heading">
        <h2>事件列表</h2>
      </div>
      <div class="admin-table operation-audit-table">
        <article
          v-for="event in events"
          :key="event.id"
          class="admin-table-row operation-audit-row"
          :data-admin-audit-row="event.id"
        >
          <span>{{ event.action }}</span>
          <span>{{ event.target_kind }}:{{ event.target_id }}</span>
          <span>{{ event.actor_username || event.actor_email || event.actor_id }}</span>
          <span>{{ event.ip_address || '-' }}</span>
          <span>{{ formatCompactTimestamp(event.created_at) }}</span>
          <details>
            <summary>变更</summary>
            <el-scrollbar class="operation-audit-json-scrollbar">
              <pre class="operation-audit-json-content">{{ formatJSON(event.before_json) }}</pre>
            </el-scrollbar>
            <el-scrollbar class="operation-audit-json-scrollbar">
              <pre class="operation-audit-json-content">{{ formatJSON(event.after_json) }}</pre>
            </el-scrollbar>
          </details>
        </article>
      </div>
      <p v-if="!loading && events.length === 0" class="admin-empty">没有匹配的审计事件。</p>
    </section>
  </main>
</template>
