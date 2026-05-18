<script setup lang="ts">
import { RouterLink, RouterView } from 'vue-router'

const navigation = [
  { label: '仪表盘', to: '/chat' },
  { label: '用户管理', to: '/admin/users' },
  { label: '系统设置', to: '/admin/settings' },
  { label: '提示词管理', to: '/admin/prompts' },
  { label: '审计会话', to: '/admin/audit' },
  { label: '后台操作审计', to: '/admin/audit-events' },
]
</script>

<template>
  <div class="admin-layout-shell">
    <aside class="admin-layout-sidebar">
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
    </aside>

    <section class="admin-layout-main">
      <RouterView />
    </section>
  </div>
</template>
