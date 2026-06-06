<script setup lang="ts">
import { ref } from 'vue'

import { openSourceDependencies, openSourceProjectSource } from '../lib/open-source'

const isOpen = ref(false)

function openLicenses() {
  isOpen.value = true
}

function closeLicenses() {
  isOpen.value = false
}
</script>

<template>
  <section class="open-source-panel">
    <button
      class="open-source-trigger"
      type="button"
      aria-haspopup="dialog"
      :aria-expanded="isOpen"
      data-open-source-licenses-toggle
      @click="openLicenses"
    >
      开放源代码许可
    </button>

    <Teleport to="body">
      <Transition name="open-source-fade">
        <div v-if="isOpen" class="open-source-overlay" @click.self="closeLicenses">
          <div
            class="admin-dialog open-source-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="open-source-dialog-title"
            data-open-source-licenses-dialog
          >
            <div class="admin-dialog-header">
              <div>
                <p class="eyebrow">Open Source</p>
                <h2 id="open-source-dialog-title">开放源代码许可</h2>
              </div>
              <button
                class="admin-dialog-close"
                type="button"
                aria-label="关闭开放源代码许可"
                data-open-source-licenses-close
                @click="closeLicenses"
              >
                ✕
              </button>
            </div>

            <div class="open-source-scroll">
              <section class="open-source-project" :data-open-source-group="openSourceProjectSource.title">
                <h3>{{ openSourceProjectSource.title }}</h3>
                <a
                  v-for="entry in openSourceProjectSource.entries"
                  :key="entry.label + entry.repositoryUrl"
                  class="open-source-project-link"
                  :href="entry.repositoryUrl"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  {{ entry.repositoryUrl }}
                </a>
              </section>

              <section class="open-source-group" data-open-source-group="all-direct-dependencies">
                <h3>Components</h3>
                <div class="open-source-table">
                  <div class="open-source-row open-source-row-head" aria-hidden="true">
                    <span>名称</span>
                    <span>源码地址</span>
                  </div>
                  <div
                    v-for="entry in openSourceDependencies"
                    :key="entry.label + entry.repositoryUrl"
                    class="open-source-row"
                  >
                    <span class="open-source-label">{{ entry.label }}</span>
                    <a class="open-source-link" :href="entry.repositoryUrl" target="_blank" rel="noopener noreferrer">
                      {{ entry.repositoryUrl }}
                    </a>
                  </div>
                </div>
              </section>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </section>
</template>

<style scoped>
.open-source-panel {
  display: flex;
  justify-content: flex-start;
  padding-top: 0.35rem;
}

.open-source-trigger {
  border: 0;
  background: transparent;
  padding: 0;
  color: #946b2d;
  font: inherit;
  font-size: 0.88rem;
  font-weight: 600;
  cursor: pointer;
  text-decoration: underline;
  text-decoration-thickness: 1px;
  text-underline-offset: 0.18em;
}

.open-source-trigger:hover {
  color: #7b5621;
}

.open-source-overlay {
  position: fixed;
  inset: 0;
  z-index: 980;
  padding: 1rem;
  background: rgba(15, 32, 38, 0.42);
  display: grid;
  place-items: center;
}

.open-source-dialog {
  width: min(920px, calc(100vw - 2rem));
  max-height: calc(100svh - 2rem);
  overflow: hidden;
  display: flex;
  flex-direction: column;
  gap: 0.85rem;
}

.open-source-scroll {
  display: grid;
  flex: 1 1 auto;
  gap: 1rem;
  min-height: 0;
  overflow-y: auto;
  margin-right: 2px;
  padding-right: 0.2rem;
}

.open-source-project {
  display: grid;
  gap: 0.4rem;
}

.open-source-project h3,
.open-source-group h3 {
  margin: 0;
  font-size: 0.93rem;
  line-height: 1.2;
  color: #203840;
}

.open-source-project-link {
  color: #2f5f6d;
  word-break: break-all;
}

.open-source-project-link:hover {
  color: #19323b;
}

.open-source-group {
  display: grid;
  gap: 0.55rem;
}

.open-source-table {
  display: grid;
  gap: 0.35rem;
}

.open-source-row {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) minmax(0, 1.4fr);
  gap: 0.75rem;
  align-items: start;
  padding: 0.65rem 0.75rem;
  border: 1px solid rgba(25, 50, 59, 0.08);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.82);
}

.open-source-row-head {
  padding: 0 0.15rem;
  border: 0;
  background: transparent;
  color: #5a7a84;
  font-size: 0.78rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.02em;
}

.open-source-label {
  color: #203840;
  font-weight: 700;
  min-width: 0;
  word-break: break-word;
}

.open-source-link {
  color: #2f5f6d;
  word-break: break-all;
}

.open-source-link:hover {
  color: #19323b;
}

.open-source-fade-enter-active,
.open-source-fade-leave-active {
  transition: opacity 0.18s ease;
}

.open-source-fade-enter-active .open-source-dialog,
.open-source-fade-leave-active .open-source-dialog {
  transition: transform 0.22s cubic-bezier(0.4, 0, 0.2, 1);
}

.open-source-fade-enter-from,
.open-source-fade-leave-to {
  opacity: 0;
}

.open-source-fade-enter-from .open-source-dialog,
.open-source-fade-leave-to .open-source-dialog {
  transform: translateY(12px) scale(0.985);
}

html.theme-teal-dark .open-source-overlay {
  background: rgba(1, 12, 12, 0.62);
}

html.theme-teal-dark .open-source-dialog {
  background: rgba(9, 43, 40, 0.96);
  border: 1px solid rgba(125, 232, 221, 0.16);
  box-shadow: 0 28px 64px rgba(0, 0, 0, 0.38);
}

html.theme-teal-dark .open-source-project h3,
html.theme-teal-dark .open-source-group h3,
html.theme-teal-dark .open-source-label {
  color: #f4fffd;
}

html.theme-teal-dark .open-source-project-link,
html.theme-teal-dark .open-source-link {
  color: #75e6ff;
}

html.theme-teal-dark .open-source-project-link:hover,
html.theme-teal-dark .open-source-link:hover {
  color: #8ffff0;
}

html.theme-teal-dark .open-source-row {
  border-color: rgba(125, 232, 221, 0.12);
  background: rgba(7, 31, 30, 0.82);
}

html.theme-teal-dark .open-source-row-head {
  color: #7fcac0;
}

@media (max-width: 760px) {
  .open-source-row {
    grid-template-columns: 1fr;
  }
}
</style>
