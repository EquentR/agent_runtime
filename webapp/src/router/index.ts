import { createMemoryHistory, createRouter, createWebHistory } from 'vue-router'

import { hasActiveSession } from '../lib/session'
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

  router.beforeEach((to) => {
    if (to.meta.requiresSession && !hasActiveSession()) {
      return { path: '/login' }
    }

    if (to.path === '/login' && hasActiveSession()) {
      return { path: '/chat' }
    }

    return true
  })

  return router
}

export const router = createAppRouter()
