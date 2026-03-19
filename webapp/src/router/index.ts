import { createMemoryHistory, createRouter, createWebHistory } from 'vue-router'

import { syncSession } from '../lib/session'
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
]

export function createAppRouter(memory = false) {
  const router = createRouter({
    history: memory ? createMemoryHistory() : createWebHistory(),
    routes,
  })

  router.beforeEach(async (to) => {
    const username = to.meta.requiresSession ? await syncSession(true) : await syncSession()
    const active = username.length > 0

    if (to.meta.requiresSession && !active) {
      return { path: '/login' }
    }

    if (to.path === '/login' && active) {
      return { path: '/chat' }
    }

    return true
  })

  return router
}

export const router = createAppRouter()
