import { createMemoryHistory, createRouter, createWebHistory } from 'vue-router'

import { syncSession } from '../lib/session'
import AdminAuditView from '../views/AdminAuditView.vue'
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
  },
  {
    path: '/chat',
    name: 'chat',
    component: ChatView,
    meta: {
      requiresSession: true,
    },
  },
  {
    path: '/admin/audit',
    name: 'admin-audit',
    component: AdminAuditView,
    meta: {
      requiresSession: true,
      requiresAdmin: true,
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

  return router
}

export const router = createAppRouter()
