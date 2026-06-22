import * as monaco from 'monaco-editor/esm/vs/editor/editor.api.js'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import type { Environment } from 'monaco-editor/esm/vs/editor/editor.api.js'

type MonacoWorker = new () => Worker

let monacoReady = false

function ensureMonacoEnvironment() {
  const existing = globalThis.MonacoEnvironment ?? {}
  if (existing.getWorker) {
    globalThis.MonacoEnvironment = existing
    return
  }

  const editorWorker = EditorWorker as unknown as MonacoWorker
  const environment: Environment = {
    ...existing,
    getWorker(workerId: string, label: string) {
      void workerId
      if (label === 'editor') {
        return new editorWorker()
      }
      return new editorWorker()
    },
  }
  globalThis.MonacoEnvironment = environment
}

export function getMonaco() {
  if (!monacoReady) {
    ensureMonacoEnvironment()
    monacoReady = true
  }
  return monaco
}
