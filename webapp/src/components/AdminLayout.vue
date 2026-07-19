<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink, RouterView } from 'vue-router'

import { fetchAdminUpdateStatus } from '../lib/api'

const updateAvailable = ref(false)

const navigation = [
  { label: '系统升级', to: '/admin/updates', update: true },
  { label: '仪表盘', to: '/admin/dashboard' },
  { label: '用户管理', to: '/admin/users' },
  { label: '模型管理', to: '/admin/models' },
  { label: '系统设置', to: '/admin/settings' },
  { label: '提示词管理', to: '/admin/prompts' },
  { label: '审计会话', to: '/admin/audit' },
  { label: '后台操作审计', to: '/admin/audit-events' },
]

onMounted(async () => {
  try {
    updateAvailable.value = (await fetchAdminUpdateStatus()).update_available
  } catch {
    updateAvailable.value = false
  }
})
</script>

<template>
  <div class="admin-layout-shell">
    <aside class="admin-layout-sidebar">
      <el-scrollbar class="admin-layout-sidebar-scrollbar" view-class="admin-layout-sidebar-content">
      <div class="admin-layout-brand">
        <p class="eyebrow">Admin</p>
        <h1>后台工作台</h1>
      </div>
      <nav class="admin-layout-nav" aria-label="后台导航">
        <RouterLink
          v-for="item in navigation"
          :key="item.to"
          class="admin-layout-nav-link"
          active-class="active"
          :to="item.to"
        >
          <span>{{ item.label }}</span>
          <span v-if="item.update && updateAvailable" class="admin-update-badge">新版本</span>
        </RouterLink>
      </nav>
      <div class="admin-layout-sidebar-footer">
        <RouterLink class="ghost-button admin-layout-home-button" to="/chat">返回首页</RouterLink>
      </div>
      </el-scrollbar>
    </aside>

    <section class="admin-layout-main">
      <el-scrollbar class="admin-layout-main-scrollbar" view-class="admin-layout-main-content">
        <RouterView />
      </el-scrollbar>
    </section>
  </div>
</template>

<style scoped>
.admin-layout-sidebar-footer {
  margin-top: auto;
  padding-top: 0.5rem;
  border-top: 1px solid rgba(25, 50, 59, 0.08);
}

.admin-layout-home-button {
  display: block;
  text-align: center;
  text-decoration: none;
  font-size: 0.88rem;
}

.admin-layout-nav-link {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.admin-update-badge {
  flex: 0 0 auto;
  padding: 0.08rem 0.38rem;
  border-radius: 999px;
  background: #b42318;
  color: #fff;
  font-size: 0.68rem;
  line-height: 1.4;
}
</style>
