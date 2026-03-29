<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Close, Promotion } from '@element-plus/icons-vue'

const props = defineProps<{
  disabled: boolean
  busy?: boolean
  stopDisabled?: boolean
}>()

const emit = defineEmits<{
  send: [message: string]
  stop: []
}>()

const draft = ref('')
const textareaRef = ref<HTMLTextAreaElement | null>(null)
const minComposerHeightPx = 64

const canSend = computed(() => !props.disabled && !props.busy && draft.value.trim().length > 0)
const isBusy = computed(() => Boolean(props.busy))
const canStop = computed(() => isBusy.value && !props.stopDisabled)

function syncTextareaHeight() {
  if (!textareaRef.value) {
    return
  }

  textareaRef.value.style.height = 'auto'
  textareaRef.value.style.height = `${Math.max(textareaRef.value.scrollHeight, minComposerHeightPx)}px`
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
  void nextTick(syncTextareaHeight)
}

function handleInput() {
  syncTextareaHeight()
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
</script>

<template>
  <form class="composer-panel" @submit.prevent="submit">
    <div class="composer-field">
      <textarea
        ref="textareaRef"
        v-model="draft"
        class="composer-input"
        rows="2"
        placeholder="输入消息..."
        @input="handleInput"
        @keydown="handleKeydown"
      />
      <button class="composer-submit" type="submit" :disabled="isBusy ? !canStop : !canSend" :aria-label="isBusy ? '停止' : '发送'">
        <Close v-if="isBusy" />
        <Promotion v-else />
      </button>
    </div>
  </form>
</template>
