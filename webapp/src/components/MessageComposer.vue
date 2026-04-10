<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Close, Promotion, FullScreen, UploadFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

import type { WorkspaceSkillListItem } from '../types/api'

const props = defineProps<{
  disabled: boolean
  busy?: boolean
  stopDisabled?: boolean
  skills?: WorkspaceSkillListItem[]
  selectedSkillNames?: string[]
}>()

const emit = defineEmits<{
  send: [message: string]
  stop: []
  'update:selectedSkillNames': [names: string[]]
}>()

const draft = ref('')
const textareaRef = ref<HTMLTextAreaElement | null>(null)
const fullscreenTextareaRef = ref<HTMLTextAreaElement | null>(null)
const fullscreenOpen = ref(false)

/* One line height ~24px (font-size 0.92rem * line-height 1.5 ≈ 22px + minor padding). */
const singleLineHeight = 24
const maxVisibleLines = 4
const maxTextareaHeight = singleLineHeight * maxVisibleLines

const canSend = computed(() => !props.disabled && !props.busy && draft.value.trim().length > 0)
const isBusy = computed(() => Boolean(props.busy))
const canStop = computed(() => isBusy.value && !props.stopDisabled)

const showExpandButton = ref(false)

const skillOptions = computed(() =>
  (props.skills ?? []).map((s) => ({ label: s.name, value: s.name })),
)
const localSelectedSkills = computed({
  get: () => props.selectedSkillNames ?? [],
  set: (val: string[]) => emit('update:selectedSkillNames', val),
})

function syncTextareaHeight() {
  const el = textareaRef.value
  if (!el) {
    return
  }

  el.style.height = 'auto'
  const scrollH = el.scrollHeight

  if (scrollH > maxTextareaHeight) {
    el.style.height = `${maxTextareaHeight}px`
    el.style.overflowY = 'auto'
    showExpandButton.value = true
  } else {
    el.style.height = `${Math.max(scrollH, singleLineHeight)}px`
    el.style.overflowY = 'hidden'
    showExpandButton.value = draft.value.split('\n').length > maxVisibleLines
  }
}

function handleKeydown(event: KeyboardEvent) {
  if (isBusy.value) {
    return
  }
  if (event.key !== 'Enter' || event.shiftKey) {
    return
  }

  event.preventDefault()
  submit()
}

function submit() {
  if (isBusy.value) {
    emit('stop')
    return
  }
  if (!canSend.value) {
    return
  }

  emit('send', draft.value.trim())
  draft.value = ''
  showExpandButton.value = false
  void nextTick(syncTextareaHeight)
}

function handleInput() {
  syncTextareaHeight()
}

function handleAttachClick() {
  ElMessage.info('附件上传功能开发中')
}

function openFullscreen() {
  fullscreenOpen.value = true
  void nextTick(() => {
    fullscreenTextareaRef.value?.focus()
  })
}

function closeFullscreen() {
  fullscreenOpen.value = false
  void nextTick(() => {
    textareaRef.value?.focus()
    syncTextareaHeight()
  })
}

function handleFullscreenKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    closeFullscreen()
    return
  }
  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault()
    closeFullscreen()
    void nextTick(submit)
  }
}

watch(
  () => props.disabled,
  (disabled) => {
    if (disabled) {
      draft.value = draft.value.trimStart()
      void nextTick(syncTextareaHeight)
    }
  },
)

onMounted(() => {
  syncTextareaHeight()
})

function focus() {
  textareaRef.value?.focus()
}

defineExpose({ focus })
</script>

<template>
  <form class="composer-panel" @submit.prevent="submit">
    <div class="composer-card">
      <!-- Textarea area -->
      <div class="composer-textarea-wrapper">
        <textarea
          ref="textareaRef"
          v-model="draft"
          class="composer-input"
          rows="1"
          placeholder="输入消息..."
          @input="handleInput"
          @keydown="handleKeydown"
        />
        <button
          v-if="showExpandButton"
          class="composer-expand-btn"
          type="button"
          aria-label="全屏编辑"
          @click="openFullscreen"
        >
          <FullScreen />
        </button>
      </div>

      <!-- Bottom toolbar -->
      <div class="composer-toolbar">
        <div class="composer-toolbar-left">
          <!-- File attachment placeholder -->
          <button
            class="composer-attach-btn"
            type="button"
            aria-label="上传附件"
            @click="handleAttachClick"
          >
            <UploadFilled />
            <span>附件</span>
          </button>

          <!-- Skills multi-select -->
          <el-select
            v-if="skillOptions.length > 0"
            v-model="localSelectedSkills"
            class="composer-skill-select"
            popper-class="composer-skill-popper"
            multiple
            collapse-tags
            collapse-tags-tooltip
            placeholder="选择 Skills"
            size="small"
            clearable
          >
            <el-option
              v-for="opt in skillOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </div>

        <div class="composer-toolbar-right">
          <button
            class="composer-submit"
            type="submit"
            :disabled="isBusy ? !canStop : !canSend"
            :aria-label="isBusy ? '停止' : '发送'"
          >
            <Close v-if="isBusy" />
            <Promotion v-else />
          </button>
        </div>
      </div>
    </div>
  </form>

  <!-- Fullscreen editing modal -->
  <Teleport to="body">
    <div v-if="fullscreenOpen" class="composer-fullscreen-overlay" @click.self="closeFullscreen">
      <div class="composer-fullscreen-modal">
        <div class="composer-fullscreen-header">
          <span class="composer-fullscreen-title">编辑消息</span>
          <button
            class="composer-fullscreen-close"
            type="button"
            aria-label="关闭全屏"
            @click="closeFullscreen"
          >
            <Close />
          </button>
        </div>
        <textarea
          ref="fullscreenTextareaRef"
          v-model="draft"
          class="composer-fullscreen-input"
          placeholder="输入消息..."
          @keydown="handleFullscreenKeydown"
        />
        <div class="composer-fullscreen-footer">
          <span class="composer-fullscreen-hint">Enter 发送 · Shift+Enter 换行 · Esc 关闭</span>
          <button
            class="composer-submit"
            type="button"
            :disabled="!canSend"
            aria-label="发送"
            @click="closeFullscreen(); $nextTick(() => submit())"
          >
            <Promotion />
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
