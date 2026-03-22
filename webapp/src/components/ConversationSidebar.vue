<script setup lang="ts">
import { Teleport, computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import {
  Close,
  DataAnalysis,
  Delete,
  Fold,
  Menu,
  Plus,
  Remove
} from '@element-plus/icons-vue'

import { formatConversationTitle } from '../lib/chat'
import type { Conversation } from '../types/api'

const props = defineProps<{
  activeConversationId: string
  collapsed?: boolean
  conversations: Conversation[]
  desktopHidden?: boolean
  loading: boolean
  mobile?: boolean
  open?: boolean
  username: string
  isAdmin?: boolean
}>()

const emit = defineEmits<{
  select: [conversationId: string]
  create: []
  close: []
  delete: [conversationId: string]
  logout: []
  'toggle-collapse': []
}>()

type ConfirmState =
  | { kind: 'delete'; conversationId: string; title: string; message: string; confirmLabel: string }
  | { kind: 'logout'; title: string; message: string; confirmLabel: string }

const items = computed(() => props.conversations)
const compact = computed(() => Boolean(props.collapsed && !props.mobile))
const confirmState = ref<ConfirmState | null>(null)
const userMenuOpen = ref(false)
const userMenuAnchor = ref<HTMLElement | null>(null)
const userMenuPanel = ref<HTMLElement | null>(null)
const userMenuStyle = ref<Record<string, string>>({})

function requestDelete(conversationId: string) {
  userMenuOpen.value = false
  confirmState.value = {
    kind: 'delete',
    conversationId,
    title: '删除对话',
    message: '确认删除这个对话？',
    confirmLabel: '删除',
  }
}

function closeUserMenu() {
	userMenuOpen.value = false
}

function requestLogout() {
  closeUserMenu()
  confirmState.value = {
    kind: 'logout',
    title: '退出登录',
    message: '确认退出登录？',
    confirmLabel: '退出',
  }
}

function cancelConfirm() {
	confirmState.value = null
}

function confirmAction() {
	if (!confirmState.value) {
		return
	}
	if (confirmState.value.kind === 'delete') {
		emit('delete', confirmState.value.conversationId)
	} else {
		emit('logout')
	}
	confirmState.value = null
}

function toggleUserMenu() {
	userMenuOpen.value = !userMenuOpen.value
	if (userMenuOpen.value) {
		void nextTick().then(syncUserMenuPosition)
	}
}

function syncUserMenuPosition() {
	const anchor = userMenuAnchor.value
	if (!anchor || !userMenuOpen.value) {
		return
	}
	const rect = anchor.getBoundingClientRect()
	userMenuStyle.value = {
		position: 'fixed',
		right: `${Math.max(window.innerWidth - rect.right, 16)}px`,
		bottom: `${Math.max(window.innerHeight - rect.top + 8, 16)}px`,
	}
}

function handleDocumentMouseDown(event: MouseEvent) {
	if (!userMenuOpen.value) {
		return
	}
	const target = event.target
	if (!(target instanceof Node)) {
		return
	}
	if (userMenuAnchor.value?.contains(target) || userMenuPanel.value?.contains(target)) {
		return
	}
	closeUserMenu()
}

function handleViewportChange() {
	if (!userMenuOpen.value) {
		return
	}
	syncUserMenuPosition()
}

onMounted(() => {
	document.addEventListener('mousedown', handleDocumentMouseDown)
	window.addEventListener('resize', handleViewportChange)
	window.addEventListener('scroll', handleViewportChange, true)
})

onBeforeUnmount(() => {
	document.removeEventListener('mousedown', handleDocumentMouseDown)
	window.removeEventListener('resize', handleViewportChange)
	window.removeEventListener('scroll', handleViewportChange, true)
})
</script>

<template>
  <aside
    class="sidebar-panel"
    :class="{ collapsed: compact, mobile: mobile, open: open }"
    :aria-hidden="desktopHidden ? 'true' : undefined"
    :inert="desktopHidden || undefined"
  >
    <div class="sidebar-header">
      <div v-if="!compact || mobile">
        <h2>对话列表</h2>
      </div>
      <div class="sidebar-header-actions">
        <button
          v-if="!mobile"
          class="ghost-button icon-button sidebar-toggle"
          type="button"
          :aria-label="compact ? '展开侧栏' : '收起侧栏'"
          @click="emit('toggle-collapse')"
        >
          <Fold />
        </button>
        <button class="ghost-button icon-button" type="button" aria-label="新建对话" @click="emit('create')">
          <Plus />
        </button>
        <button v-if="mobile" class="ghost-button icon-button" type="button" aria-label="关闭侧栏" @click="emit('close')">
          <Close />
        </button>
      </div>
    </div>

    <div class="sidebar-list">
      <p v-if="loading" class="sidebar-empty">正在加载对话...</p>

      <div v-else-if="items.length === 0" class="sidebar-empty">
        还没有对话呢~。
      </div>

      <template v-for="conversation in items" :key="conversation.id">
        <button
          class="conversation-card"
          :class="{ active: conversation.id === activeConversationId }"
          type="button"
          @click="emit('select', conversation.id)"
        >
          <span v-if="compact" class="conversation-compact-dot" aria-hidden="true"></span>
          <span
            v-if="!compact"
            class="conversation-title truncate-text"
            :title="formatConversationTitle(conversation.title, '未命名对话')"
          >
            {{ formatConversationTitle(conversation.title, '未命名对话') }}
          </span>
          <span v-else class="conversation-compact-label" :title="formatConversationTitle(conversation.title, '未命名对话')">
            {{ formatConversationTitle(conversation.title, '未命名对话').slice(0, 1).toUpperCase() }}
          </span>
          <button
            v-if="!compact"
            class="ghost-button icon-button conversation-delete-button"
            type="button"
            aria-label="删除对话"
            @click.stop="requestDelete(conversation.id)"
          >
            <Delete />
          </button>
        </button>
      </template>
    </div>

    <div class="sidebar-account" :class="{ collapsed: compact }">
      <div v-if="!compact || mobile" class="sidebar-account-copy">
        <span class="sidebar-account-label">当前账号</span>
        <strong class="sidebar-account-name">{{ username }}</strong>
      </div>
      <div ref="userMenuAnchor" class="sidebar-user-menu-anchor">
        <button
          v-if="compact && !mobile"
          class="ghost-button icon-button sidebar-user-menu-trigger compact"
          type="button"
          aria-label="打开用户菜单"
          @click="toggleUserMenu"
        >
          <Menu />
        </button>
        <button v-else class="ghost-button icon-button sidebar-user-menu-trigger" type="button" aria-label="打开用户菜单" @click="toggleUserMenu">
          <Menu />
        </button>

      </div>
    </div>

    <Teleport to="body">
      <transition name="model-menu-fade">
        <div v-if="userMenuOpen" ref="userMenuPanel" class="sidebar-user-menu-panel upward" :class="{ compact }" :style="userMenuStyle">
          <RouterLink v-if="isAdmin" class="sidebar-user-menu-option sidebar-admin-link" to="/admin/audit" @click="closeUserMenu">
            <span class="sidebar-user-menu-option-check" aria-hidden="true"></span>
            <DataAnalysis />
            <span class="sidebar-user-menu-option-label">审计</span>
          </RouterLink>
          <button class="sidebar-user-menu-option sidebar-user-menu-logout" type="button" @click="requestLogout">
            <span class="sidebar-user-menu-option-check" aria-hidden="true"></span>
            <Remove />
            <span class="sidebar-user-menu-option-label">退出登录</span>
          </button>
        </div>
      </transition>
    </Teleport>

    <div v-if="confirmState" class="sidebar-confirm-overlay" @click.self="cancelConfirm">
      <div class="sidebar-confirm-dialog">
        <h3>{{ confirmState.title }}</h3>
        <p>{{ confirmState.message }}</p>
        <div class="sidebar-confirm-actions">
          <button class="ghost-button sidebar-confirm-cancel" type="button" @click="cancelConfirm">取消</button>
          <button class="ghost-button sidebar-confirm-confirm" type="button" @click="confirmAction">{{ confirmState.confirmLabel }}</button>
        </div>
      </div>
    </div>
  </aside>
</template>
