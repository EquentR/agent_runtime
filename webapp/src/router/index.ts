import { createMemoryHistory, createRouter, createWebHistory } from 'vue-router'

import { formatDocumentTitle } from '../lib/chat'
import { syncSession } from '../lib/session'
import AdminAuditView from '../views/AdminAuditView.vue'
import AdminPromptView from '../views/AdminPromptView.vue'
import ApprovalView from '../views/ApprovalView.vue'
import ChatView from '../views/ChatView.vue'
import LoginView from '../views/LoginView.vue'

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
    path: '/chat',
    name: 'chat',
    component: ChatView,
    meta: {
      requiresSession: true,
      title: '聊天',
    },
  },
  {
    path: '/approvals/:taskId',
    name: 'approvals',
    component: ApprovalView,
    meta: {
      requiresSession: true,
      title: '审批管理',
    },
  },
  {
    path: '/admin/audit',
    name: 'admin-audit',
    component: AdminAuditView,
    meta: {
      requiresSession: true,
      requiresAdmin: true,
      title: '审计会话',
    },
  },
  {
    path: '/admin/prompts',
    name: 'admin-prompts',
    component: AdminPromptView,
    meta: {
      requiresSession: true,
      requiresAdmin: true,
      title: '提示词管理',
    },
  },
]

export function createAppRouter(memory = false) {
  const router = createRouter({
    history: memory ? createMemoryHistory() : createWebHistory(),
    routes,
  })

  router.beforeEach(async (to) => {
    const session = to.meta.requiresSession ? await syncSession(true) : await syncSession()
    const active = Boolean(session?.username)

    if (to.meta.requiresSession && !active) {
      return { path: '/login' }
    }

    if (to.meta.requiresAdmin && session?.role !== 'admin') {
      return { path: '/chat' }
    }

    if (to.path === '/login' && active) {
      return { path: '/chat' }
    }

    return true
  })

  router.afterEach((to) => {
    document.title = formatDocumentTitle(typeof to.meta.title === 'string' ? to.meta.title : undefined)
  })

  return router
}

export const router = createAppRouter()
