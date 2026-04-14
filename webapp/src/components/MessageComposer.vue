<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Close, Promotion, FullScreen, UploadFilled } from '@element-plus/icons-vue'

import type { WorkspaceSkillListItem } from '../types/api'

interface DraftAttachmentItem {
  local_id: string
  id?: string
  file_name: string
  upload_state: 'uploading' | 'uploaded' | 'failed'
  error_message?: string
}

const props = withDefaults(defineProps<{
  disabled: boolean
  busy?: boolean
  stopDisabled?: boolean
  skills?: WorkspaceSkillListItem[]
  selectedSkillNames?: string[]
  attachmentsEnabled?: boolean
  attachmentsUploading?: boolean
  attachments?: DraftAttachmentItem[]
}>(), {
  attachmentsEnabled: true,
  attachmentsUploading: false,
  attachments: () => [],
})

const emit = defineEmits<{
  send: [message: string]
  stop: []
  'update:selectedSkillNames': [names: string[]]
  'add-attachments': [files: File[]]
  'remove-attachment': [localId: string]
}>()

const draft = ref('')
const textareaRef = ref<HTMLTextAreaElement | null>(null)
const fullscreenTextareaRef = ref<HTMLTextAreaElement | null>(null)
const fullscreenOpen = ref(false)
const fileInputRef = ref<HTMLInputElement | null>(null)

/* One line height ~24px (font-size 0.92rem * line-height 1.5 ≈ 22px + minor padding). */
const singleLineHeight = 24
const maxVisibleLines = 4
const maxTextareaHeight = singleLineHeight * maxVisibleLines

const canSend = computed(() => !props.disabled && !props.busy && !props.attachmentsUploading && draft.value.trim().length > 0)
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
  fileInputRef.value?.click()
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

function emitFiles(files: File[]) {
  if (files.length === 0) {
    return
  }
  emit('add-attachments', files)
}

function handleFileInputChange(event: Event) {
  const input = event.target as HTMLInputElement | null
  const files = input?.files ? Array.from(input.files) : []
  emitFiles(files)
  if (input) {
    input.value = ''
  }
}

function handleDrop(event: DragEvent) {
  if (!props.attachmentsEnabled) {
    return
  }
  const files = event.dataTransfer?.files ? Array.from(event.dataTransfer.files) : []
  emitFiles(files)
}

function handlePaste(event: ClipboardEvent) {
  if (!props.attachmentsEnabled) {
    return
  }
  const items = Array.from(event.clipboardData?.items ?? [])
  const files = items
    .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
    .map((item) => item.getAsFile())
    .filter((file): file is File => file instanceof File)
  if (files.length === 0) {
    return
  }
  event.preventDefault()
  emitFiles(files)
}

function removeAttachment(localId: string) {
  emit('remove-attachment', localId)
}

defineExpose({ focus })
</script>

<template>
  <form class="composer-panel" @submit.prevent="submit">
    <div class="composer-card">
      <input
        v-if="attachmentsEnabled"
        ref="fileInputRef"
        class="composer-file-input"
        type="file"
        multiple
        @change="handleFileInputChange"
      />

      <div
        v-if="attachmentsEnabled"
        class="composer-upload-dropzone"
        @dragover.prevent
        @drop.prevent="handleDrop"
      >
        <span class="composer-upload-dropzone-label">拖拽文件到这里，或点击“附件”上传</span>
      </div>

      <div v-if="attachments.length > 0" class="composer-attachment-list">
        <div
          v-for="attachment in attachments"
          :key="attachment.local_id"
          class="composer-attachment-chip"
          :data-upload-state="attachment.upload_state"
        >
          <span class="composer-attachment-name">{{ attachment.file_name }}</span>
          <span class="composer-attachment-state">
            {{
              attachment.upload_state === 'uploading'
                ? '上传中'
                : attachment.upload_state === 'failed'
                  ? attachment.error_message || '上传失败'
                  : '已上传'
            }}
          </span>
          <button
            type="button"
            class="composer-attachment-remove"
            :aria-label="`移除附件 ${attachment.file_name}`"
            @click="removeAttachment(attachment.local_id)"
          >
            <Close />
          </button>
        </div>
      </div>

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
          @paste="handlePaste"
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
            v-if="attachmentsEnabled"
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
