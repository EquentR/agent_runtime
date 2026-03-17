<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'

import { saveSession } from '../lib/session'

const router = useRouter()
const username = ref('')
const canLogin = computed(() => username.value.trim().length > 0)

async function handleLogin() {
  if (!canLogin.value) {
    return
  }

  saveSession(username.value)
  await router.push('/chat')
}
</script>

<template>
  <main class="login-shell">
    <section class="login-card">
      <p class="eyebrow">Demo Access</p>
      <h1>Step into the runtime</h1>
      <p class="login-copy">
        This first pass skips auth on purpose. Enter any name and go straight into the chat UI.
      </p>

      <label class="field-label" for="username">Display name</label>
      <input id="username" v-model="username" class="text-input" placeholder="e.g. Equent" />

      <button class="primary-button wide" type="button" :disabled="!canLogin" @click="handleLogin">
        Enter chat
      </button>
    </section>
  </main>
</template>
