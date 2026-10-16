<script setup lang="ts">
// CodeMirror 6 binding. The editor owns its document; the parent owns the value.
//
// One instance per open file, which the parent arranges by keeping every open
// file's editor mounted and showing only the active one. Sharing one instance
// across tabs makes undo walk back through the tab switches themselves -- the
// document swap is an ordinary history entry -- so a reflex Ctrl-Z after
// switching tabs would pull the previous file's text into this one, and the next
// Save would write it there. Rebuilding the instance on each switch avoids that
// too, and loses something worse: the undo stack of every file the user switched
// away from.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { EditorView, basicSetup } from 'codemirror'

const props = defineProps<{ modelValue: string; active?: boolean }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

const host = ref<HTMLElement | null>(null)
let view: EditorView | null = null

// CodeMirror's own theme is a light one, which put a white gutter and a pale
// active line inside an otherwise dark page. This one is drawn from the page's
// tokens (style.css), so the editor reads as part of the same surface, and is
// marked dark so the editor picks its dark defaults for everything not listed.
const pageTheme = EditorView.theme(
  {
    '&': { color: 'var(--th-text-hi)', backgroundColor: 'var(--app-bg)' },
    '.cm-content': { caretColor: 'var(--th-text-hi)' },
    '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--th-text-hi)' },
    '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection':
      { backgroundColor: 'rgb(96 165 250 / 30%)' },
    '.cm-gutters': {
      backgroundColor: 'var(--th-surface)',
      color: 'var(--th-text-lo)',
      borderRight: '1px solid var(--th-border)',
    },
    '.cm-activeLine': { backgroundColor: 'rgb(255 255 255 / 4%)' },
    '.cm-activeLineGutter': { backgroundColor: 'rgb(255 255 255 / 6%)', color: 'var(--th-text-mid)' },
    '.cm-foldPlaceholder': { backgroundColor: 'var(--th-raised)', border: 'none', color: 'var(--th-text-mid)' },
    '.cm-searchMatch': { backgroundColor: 'rgb(250 204 21 / 25%)', outline: '1px solid rgb(250 204 21 / 45%)' },
    '.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: 'rgb(96 165 250 / 35%)' },
    '.cm-matchingBracket, .cm-nonmatchingBracket': { backgroundColor: 'rgb(255 255 255 / 10%)' },
    '.cm-panels': { backgroundColor: 'var(--th-surface)', color: 'var(--th-text-hi)' },
    '.cm-panels.cm-panels-bottom': { borderTop: '1px solid var(--th-border)' },
    '.cm-tooltip': { backgroundColor: 'var(--th-raised)', border: '1px solid var(--th-border)' },
  },
  { dark: true },
)

onMounted(() => {
  if (!host.value) return
  view = new EditorView({
    doc: props.modelValue,
    parent: host.value,
    extensions: [
      basicSetup,
      pageTheme,
      EditorView.updateListener.of((update) => {
        if (update.docChanged) emit('update:modelValue', update.state.doc.toString())
      }),
    ],
  })
})

watch(
  () => props.modelValue,
  (value) => {
    if (!view) return
    const current = view.state.doc.toString()
    // Only replace the document when the change came from somewhere other than
    // this editor. Writing back what a keystroke just produced would rebuild the
    // document on every character and lose the cursor.
    if (current === value) return
    view.dispatch({ changes: { from: 0, to: current.length, insert: value } })
  },
)

// An instance that is not on screen has no dimensions, so it cannot measure
// itself -- and CodeMirror's own sizing follows the element it is given. Showing
// one again is what asks it to measure, so that is where the request goes. The
// editor measures itself once when it is created, which covers the case of a
// file that is opened while it is the one being shown.
watch(
  () => props.active,
  (isActive) => {
    if (isActive) view?.requestMeasure()
  },
)

onBeforeUnmount(() => {
  view?.destroy()
  view = null
})
</script>

<template>
  <div ref="host" class="code-editor"></div>
</template>

<style scoped>
.code-editor {
  height: 100%;
  overflow: auto;
}

.code-editor :deep(.cm-editor) {
  height: 100%;
  font-size: 13px;
}

.code-editor :deep(.cm-scroller) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', monospace;
}
</style>
