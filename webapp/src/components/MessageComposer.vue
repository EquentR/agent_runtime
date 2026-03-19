<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Promotion } from '@element-plus/icons-vue'

const props = defineProps<{
  disabled: boolean
}>()

const emit = defineEmits<{
  send: [message: string]
}>()

const draft = ref('')
const textareaRef = ref<HTMLTextAreaElement | null>(null)
const minComposerHeightPx = 64

const canSend = computed(() => !props.disabled && draft.value.trim().length > 0)

function syncTextareaHeight() {
  if (!textareaRef.value) {
    return
  }

  textareaRef.value.style.height = 'auto'
  textareaRef.value.style.height = `${Math.max(textareaRef.value.scrollHeight, minComposerHeightPx)}px`
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || event.shiftKey) {
    return
  }

  event.preventDefault()
  submit()
}

function submit() {
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
      <button class="composer-submit" type="submit" :disabled="!canSend" aria-label="发送">
        <Promotion />
      </button>
    </div>
  </form>
</template>
