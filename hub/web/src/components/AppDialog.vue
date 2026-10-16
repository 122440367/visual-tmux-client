<script setup lang="ts">
// The in-app confirm/prompt dialog the desktop shell uses in place of the web
// view's own (see dialogs.ts). It is modal the way a native sheet is: the rest
// of the window cannot be reached while it is open, Enter answers yes, Escape
// answers no, and the focus goes back where it was when it closes.
import { computed, nextTick, ref, watch } from 'vue'
import { openDialog, settleDialog } from '../dialogs'

const panel = ref<HTMLElement | null>(null)
const input = ref<HTMLInputElement | null>(null)
const draft = ref('')
let previousFocus: HTMLElement | null = null

const dialog = computed(() => openDialog.value)

const title = computed(() => {
  const d = dialog.value
  if (!d) return ''
  return d.title ?? (d.kind === 'confirm' ? 'Confirm' : 'Enter a name')
})

const confirmLabel = computed(() => {
  const d = dialog.value
  if (!d) return ''
  return d.confirmLabel ?? 'OK'
})

const danger = computed(() => dialog.value?.kind === 'confirm' && dialog.value.danger === true)

// A prompt cannot be answered with nothing: the callers treat an empty answer as
// a cancel anyway, and a primary button that silently cancels is a trap.
const canConfirm = computed(() => dialog.value?.kind !== 'prompt' || draft.value.trim() !== '')

watch(
  dialog,
  async (d, previous) => {
    if (d === null) {
      // Only when the last one closes: a queued dialog opening straight after
      // keeps the focus inside the dialog.
      const target = previousFocus
      previousFocus = null
      if (target && target.isConnected) target.focus()
      return
    }
    if (previous === null || previous === undefined) {
      previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    }
    draft.value = d.kind === 'prompt' ? (d.initial ?? '') : ''
    await nextTick()
    if (d.kind === 'prompt' && input.value) {
      input.value.focus()
      // A rename keeps the extension: selecting only the stem is what a file
      // manager does, because the stem is what is almost always being changed.
      const dot = draft.value.lastIndexOf('.')
      input.value.setSelectionRange(0, dot > 0 ? dot : draft.value.length)
    } else {
      // The panel, not a button. The keystroke that asked the question --
      // Enter on a menu item -- is still being delivered when the dialog
      // appears, and WebKit activates a focused button on that key's keypress,
      // so a focused Kill button would be pressed by the very key that opened
      // it. The panel answers Enter from a keydown of its own (onKeydown).
      panel.value?.focus()
    }
  },
  { immediate: true },
)

function accept(): void {
  const d = dialog.value
  if (!d || !canConfirm.value) return
  settleDialog(d.kind === 'prompt' ? draft.value.trim() : true)
}

function cancel(): void {
  const d = dialog.value
  if (!d) return
  settleDialog(d.kind === 'prompt' ? null : false)
}

function onKeydown(ev: KeyboardEvent): void {
  if (ev.key === 'Escape') {
    ev.preventDefault()
    ev.stopPropagation()
    cancel()
    return
  }
  if (
    ev.key === 'Enter' &&
    !ev.repeat &&
    !ev.isComposing &&
    (ev.target as HTMLElement | null)?.tagName !== 'BUTTON'
  ) {
    // An IME composition commits with Enter; answering then would submit a
    // half-typed name. A focused button answers for itself, so Enter on Cancel
    // cancels.
    ev.preventDefault()
    accept()
    return
  }
  if (ev.key !== 'Tab' || !panel.value) return
  const focusable = Array.from(
    panel.value.querySelectorAll<HTMLElement>('input, button:not([disabled])'),
  )
  if (focusable.length === 0) return
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (ev.shiftKey && document.activeElement === first) {
    ev.preventDefault()
    last.focus()
  } else if (!ev.shiftKey && document.activeElement === last) {
    ev.preventDefault()
    first.focus()
  }
}
</script>

<template>
  <Transition name="app-dialog">
    <div v-if="dialog" class="app-dialog" @mousedown.self.prevent>
      <div
        ref="panel"
        class="app-dialog__panel"
        tabindex="-1"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="app-dialog-title"
        aria-describedby="app-dialog-message"
        @keydown="onKeydown"
      >
        <h2 id="app-dialog-title" class="app-dialog__title">{{ title }}</h2>
        <p id="app-dialog-message" class="app-dialog__message">{{ dialog.message }}</p>
        <input
          v-if="dialog.kind === 'prompt'"
          ref="input"
          v-model="draft"
          class="app-dialog__input"
          type="text"
          spellcheck="false"
          autocomplete="off"
          :aria-label="dialog.message"
        />
        <div class="app-dialog__actions">
          <button class="app-dialog__btn" type="button" @click="cancel">Cancel</button>
          <button
            class="app-dialog__btn app-dialog__btn--primary"
            :class="{ 'app-dialog__btn--danger': danger }"
            type="button"
            :disabled="!canConfirm"
            @click="accept"
          >{{ confirmLabel }}</button>
        </div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.app-dialog {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 14vh;
  background: rgba(0, 0, 0, 0.45);
  backdrop-filter: blur(2px);
}
.app-dialog__panel {
  width: min(420px, calc(100vw - 2rem));
  box-sizing: border-box;
  padding: 1.1rem 1.2rem 1rem;
  background: var(--th-surface);
  border: 1px solid var(--th-border-strong, var(--th-border));
  border-radius: 12px;
  box-shadow:
    0 24px 48px rgba(0, 0, 0, 0.45),
    0 2px 6px rgba(0, 0, 0, 0.3);
}
/* The panel holds the focus while no control has it; the primary button's
   colour already says what Enter does. */
.app-dialog__panel:focus {
  outline: none;
}
.app-dialog__title {
  margin: 0 0 0.4rem;
  font-size: 0.95rem;
  font-weight: 600;
  color: var(--th-text-hi);
}
.app-dialog__message {
  margin: 0;
  font-size: 0.85rem;
  line-height: 1.5;
  color: var(--th-text-mid);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.app-dialog__input {
  display: block;
  width: 100%;
  box-sizing: border-box;
  margin-top: 0.75rem;
  padding: 0.45rem 0.6rem;
  background: var(--app-bg);
  color: var(--th-text-hi);
  border: 1px solid var(--th-border-strong, var(--th-border));
  border-radius: 6px;
  font-family: var(--app-code-font);
  font-size: 0.85rem;
}
.app-dialog__input:focus {
  outline: none;
  border-color: var(--th-accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--th-accent) 25%, transparent);
}
.app-dialog__actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
  margin-top: 1.1rem;
}
.app-dialog__btn {
  min-width: 5.5rem;
  padding: 0.4rem 0.9rem;
  background: var(--th-raised);
  color: var(--th-text-hi);
  border: 1px solid var(--th-border-strong, var(--th-border));
  border-radius: 6px;
  font-size: 0.82rem;
  cursor: pointer;
}
.app-dialog__btn:hover {
  border-color: var(--th-text-lo);
}
.app-dialog__btn--primary {
  background: var(--th-accent);
  border-color: var(--th-accent);
  color: #0a0a0a;
  font-weight: 600;
}
.app-dialog__btn--primary:hover {
  filter: brightness(1.08);
  border-color: var(--th-accent);
}
.app-dialog__btn--danger {
  background: var(--th-danger);
  border-color: var(--th-danger);
}
.app-dialog__btn--danger:hover {
  border-color: var(--th-danger);
}
.app-dialog__btn:disabled {
  opacity: 0.45;
  cursor: default;
  filter: none;
}
.app-dialog-enter-active,
.app-dialog-leave-active {
  transition: opacity 0.12s ease;
}
.app-dialog-enter-active .app-dialog__panel,
.app-dialog-leave-active .app-dialog__panel {
  transition: transform 0.12s ease;
}
.app-dialog-enter-from,
.app-dialog-leave-to {
  opacity: 0;
}
.app-dialog-enter-from .app-dialog__panel,
.app-dialog-leave-to .app-dialog__panel {
  transform: translateY(-6px) scale(0.98);
}
@media (prefers-reduced-motion: reduce) {
  .app-dialog-enter-active,
  .app-dialog-leave-active,
  .app-dialog-enter-active .app-dialog__panel,
  .app-dialog-leave-active .app-dialog__panel {
    transition: none;
  }
}
</style>
