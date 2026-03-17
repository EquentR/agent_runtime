<script setup lang="ts">
import { computed, ref, watch } from 'vue'

const props = defineProps<{
  disabled: boolean
}>()

const emit = defineEmits<{
  send: [message: string]
}>()

const draft = ref('')

const canSend = computed(() => !props.disabled && draft.value.trim().length > 0)

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
    <textarea
      v-model="draft"
      class="composer-input"
      rows="4"
      placeholder="Ask something about your runtime..."
    />
    <div class="composer-footer">
      <p>Messages are sent to the existing `agent.run` task API.</p>
      <button class="primary-button" type="submit" :disabled="!canSend">
        {{ disabled ? 'Sending...' : 'Send message' }}
      </button>
    </div>
  </form>
</template>
