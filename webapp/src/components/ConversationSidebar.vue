<script setup lang="ts">
import { computed, ref } from 'vue'
import { Close, Delete, Fold, Plus, SwitchButton } from '@element-plus/icons-vue'

import { formatConversationTitle } from '../lib/chat'
import type { Conversation } from '../types/api'

const props = defineProps<{
  activeConversationId: string
  collapsed?: boolean
  conversations: Conversation[]
  loading: boolean
  mobile?: boolean
  open?: boolean
  username: string
}>()

const emit = defineEmits<{
  select: [conversationId: string]
  create: []
  close: []
  delete: [conversationId: string]
  logout: []
  'toggle-collapse': []
}>()

const items = computed(() => props.conversations)
const confirmingConversationId = ref('')
const confirmingLogout = ref(false)

function requestDelete(conversationId: string) {
  confirmingConversationId.value = conversationId
}

function cancelDelete() {
  confirmingConversationId.value = ''
}

function confirmDelete(conversationId: string) {
  confirmingConversationId.value = ''
  emit('delete', conversationId)
}

function requestLogout() {
  confirmingLogout.value = true
}

function cancelLogout() {
  confirmingLogout.value = false
}

function confirmLogout() {
  confirmingLogout.value = false
  emit('logout')
}
</script>

<template>
  <aside class="sidebar-panel" :class="{ collapsed: collapsed && !mobile, mobile: mobile, open: open }">
    <div class="sidebar-header">
      <div v-if="!collapsed || mobile">
        <h2>对话列表</h2>
      </div>
      <div class="sidebar-header-actions">
        <button
          v-if="!mobile"
          class="ghost-button icon-button sidebar-toggle"
          type="button"
          :aria-label="collapsed ? '展开侧栏' : '收起侧栏'"
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
          <span v-if="collapsed" class="conversation-compact-dot" aria-hidden="true"></span>
          <span
            v-if="!collapsed"
            class="conversation-title truncate-text"
            :title="formatConversationTitle(conversation.title, '未命名对话')"
          >
            {{ formatConversationTitle(conversation.title, '未命名对话') }}
          </span>
          <span v-else class="conversation-compact-label" :title="formatConversationTitle(conversation.title, '未命名对话')">
            {{ formatConversationTitle(conversation.title, '未命名对话').slice(0, 1).toUpperCase() }}
          </span>
          <button
            v-if="!collapsed"
            class="ghost-button icon-button conversation-delete-button"
            type="button"
            aria-label="删除对话"
            @click.stop="requestDelete(conversation.id)"
          >
            <Delete />
          </button>
        </button>
        <div v-if="confirmingConversationId === conversation.id" class="conversation-delete-confirm compact-confirm">
          <span class="compact-confirm-text">确认删除这个对话？</span>
          <div class="compact-confirm-actions">
            <button class="ghost-button compact-confirm-button conversation-delete-cancel" type="button" @click="cancelDelete">取消</button>
            <button class="ghost-button compact-confirm-button conversation-delete-confirm-button" type="button" @click="confirmDelete(conversation.id)">
              删除
            </button>
          </div>
        </div>
      </template>
    </div>

    <div class="sidebar-account" :class="{ collapsed: collapsed && !mobile }">
      <div v-if="!collapsed || mobile" class="sidebar-account-copy">
        <span class="sidebar-account-label">当前账号</span>
        <strong class="sidebar-account-name">{{ username }}</strong>
      </div>
      <button
        class="ghost-button icon-button sidebar-account-logout"
        type="button"
        aria-label="退出登录"
        @click="requestLogout"
      >
        <SwitchButton />
      </button>
    </div>
    <div v-if="confirmingLogout" class="sidebar-logout-confirm compact-confirm">
      <span class="compact-confirm-text">确认退出？</span>
      <div class="compact-confirm-actions">
        <button class="ghost-button compact-confirm-button sidebar-logout-cancel" type="button" @click="cancelLogout">取消</button>
        <button class="ghost-button compact-confirm-button sidebar-logout-confirm-button" type="button" @click="confirmLogout">退出</button>
      </div>
    </div>
  </aside>
</template>
