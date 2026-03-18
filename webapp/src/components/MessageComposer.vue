<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Promotion } from '@element-plus/icons-vue'

const props = defineProps<{
  disabled: boolean
}>()

const emit = defineEmits<{
  send: [message: string]
}>()

const draft = ref('')

const canSend = computed(() => !props.disabled && draft.value.trim().length > 0)

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
}

watch(
  () => props.disabled,
  (disabled) => {
    if (disabled) {
      draft.value = draft.value.trimStart()
    }
  },
)
</script>

<template>
  <form class="composer-panel" @submit.prevent="submit">
    <div class="composer-field">
      <textarea
        v-model="draft"
        class="composer-input"
        rows="4"
        placeholder="输入消息..."
        @keydown="handleKeydown"
      />
      <button class="composer-submit" type="submit" :disabled="!canSend" aria-label="发送">
        <Promotion />
      </button>
    </div>
  </form>
</template>
