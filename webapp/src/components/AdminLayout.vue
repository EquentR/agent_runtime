<script setup lang="ts">
import { RouterLink, RouterView } from 'vue-router'

const navigation = [
  { label: '仪表盘', to: '/admin/dashboard' },
  { label: '用户管理', to: '/admin/users' },
  { label: '模型管理', to: '/admin/models' },
  { label: '系统设置', to: '/admin/settings' },
  { label: '提示词管理', to: '/admin/prompts' },
  { label: '审计会话', to: '/admin/audit' },
  { label: '后台操作审计', to: '/admin/audit-events' },
]
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
          {{ item.label }}
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
</style>
