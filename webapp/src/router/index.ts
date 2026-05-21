import { createMemoryHistory, createRouter, createWebHashHistory } from 'vue-router'

import { formatDocumentTitle } from '../lib/chat'
import { syncSession } from '../lib/session'
import { getRequiredActionProfileTarget } from '../lib/user-state'
import AdminLayout from '../components/AdminLayout.vue'
import AdminDashboardView from '../views/AdminDashboardView.vue'
import AdminAuditView from '../views/AdminAuditView.vue'
import AdminModelsView from '../views/AdminModelsView.vue'
import AdminOperationAuditView from '../views/AdminOperationAuditView.vue'
import AdminPromptView from '../views/AdminPromptView.vue'
import AdminSettingsView from '../views/AdminSettingsView.vue'
import AdminUsersView from '../views/AdminUsersView.vue'
import ChatView from '../views/ChatView.vue'
import LoginView from '../views/LoginView.vue'
import ProfileView from '../views/ProfileView.vue'

const routes = [
  {
    path: '/',
    redirect: '/chat',
  },
  {
    path: '/login',
    name: 'login',
    component: LoginView,
    meta: {
      title: '登录',
    },
  },
  {
    path: '/chat/:conversationId?',
    name: 'chat',
    component: ChatView,
    meta: {
      requiresSession: true,
      title: '聊天',
    },
  },
  {
    path: '/profile',
    name: 'profile',
    component: ProfileView,
    meta: {
      requiresSession: true,
      title: '个人设置',
    },
  },
  {
    path: '/admin',
    component: AdminLayout,
    meta: {
      requiresSession: true,
      requiresAdmin: true,
    },
    children: [
      {
        path: '',
        redirect: '/admin/dashboard',
      },
      {
        path: 'dashboard',
        name: 'admin-dashboard',
        component: AdminDashboardView,
        meta: {
          title: '仪表盘',
        },
      },
      {
        path: 'users',
        name: 'admin-users',
        component: AdminUsersView,
        meta: {
          title: '用户管理',
        },
      },
      {
        path: 'models',
        name: 'admin-models',
        component: AdminModelsView,
        meta: {
          title: '模型管理',
        },
      },
      {
        path: 'settings',
        name: 'admin-settings',
        component: AdminSettingsView,
        meta: {
          title: '系统设置',
        },
      },
      {
        path: 'audit-events',
        name: 'admin-audit-events',
        component: AdminOperationAuditView,
        meta: {
          title: '后台操作审计',
        },
      },
      {
        path: 'audit',
        name: 'admin-audit',
        component: AdminAuditView,
        meta: {
          title: '审计会话',
        },
      },
      {
        path: 'prompts',
        name: 'admin-prompts',
        component: AdminPromptView,
        meta: {
          title: '提示词管理',
        },
      },
    ],
  },
]

export function createAppRouter(memory = false) {
  const router = createRouter({
    history: memory ? createMemoryHistory() : createWebHashHistory(),
    routes,
  })

  router.beforeEach(async (to) => {
    const session = to.meta.requiresSession ? await syncSession(true) : await syncSession()
    const active = Boolean(session?.username && session.status !== 'disabled')

    if (to.meta.requiresSession && !active) {
      return { path: '/login' }
    }

    const requiredActionTarget = getRequiredActionProfileTarget(session)
    if (active && requiredActionTarget && to.path !== '/profile') {
      return requiredActionTarget
    }

    if (to.meta.requiresAdmin && session?.role !== 'admin') {
      return { path: '/chat' }
    }

    if (to.path === '/login' && active) {
      return requiredActionTarget ?? { path: '/chat' }
    }

    return true
  })

  router.afterEach((to) => {
    document.title = formatDocumentTitle(typeof to.meta.title === 'string' ? to.meta.title : undefined)
  })

  return router
}

export const router = createAppRouter()
