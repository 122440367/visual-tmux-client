// Confirmation and text-entry dialogs.
//
// The browser client asks with window.confirm and window.prompt, and keeps
// doing so. The desktop shell cannot: WKWebView answers both with "no" unless
// its host implements them, so on macOS every delete, overwrite and rename would
// be refused without a word. In the shell these questions are asked by an
// in-app dialog instead (AppDialog.vue renders the one open here), which also
// matches the rest of the window rather than the web view's generic chrome.
//
// Either way the answer arrives as a promise, so every caller is written once
// for both.

import { shallowRef } from 'vue'
import { isDesktop } from './desktop'

export interface ConfirmOptions {
  title?: string
  /** Label of the affirmative button. */
  confirmLabel?: string
  /** Styles the affirmative button as destructive. */
  danger?: boolean
}

export interface PromptOptions {
  title?: string
  initial?: string
  confirmLabel?: string
}

/** OpenDialog is the dialog on screen; AppDialog renders it. */
export type OpenDialog =
  | ({ kind: 'confirm'; message: string; resolve: (ok: boolean) => void } & ConfirmOptions)
  | ({ kind: 'prompt'; message: string; resolve: (value: string | null) => void } & PromptOptions)

export const openDialog = shallowRef<OpenDialog | null>(null)

// A second question while one is on screen waits for the first to be answered.
// Replacing it would resolve the first caller with an answer nobody gave.
const queue: Array<() => void> = []

function show(dialog: OpenDialog): void {
  if (openDialog.value === null) {
    openDialog.value = dialog
  } else {
    queue.push(() => {
      openDialog.value = dialog
    })
  }
}

/** settleDialog closes the open dialog with an answer and shows the next one. */
export function settleDialog(answer: boolean | string | null): void {
  const current = openDialog.value
  if (current === null) return
  openDialog.value = null
  if (current.kind === 'confirm') current.resolve(answer === true)
  else current.resolve(typeof answer === 'string' ? answer : null)
  const next = queue.shift()
  if (next) next()
}

/** isDialogOpen reports whether a question is waiting for an answer. Shell
 * commands are held off while one is, as a native modal would hold them. */
export function isDialogOpen(): boolean {
  return openDialog.value !== null
}

/** askConfirm asks a yes/no question. */
export function askConfirm(message: string, options: ConfirmOptions = {}): Promise<boolean> {
  if (!isDesktop()) return Promise.resolve(window.confirm(message))
  return new Promise((resolve) => show({ kind: 'confirm', message, resolve, ...options }))
}

/** askText asks for a line of text; null means the user cancelled. */
export function askText(message: string, options: PromptOptions = {}): Promise<string | null> {
  if (!isDesktop()) return Promise.resolve(window.prompt(message, options.initial))
  return new Promise((resolve) => show({ kind: 'prompt', message, resolve, ...options }))
}

/** resetDialogsForTest drops any open or queued dialog. */
export function resetDialogsForTest(): void {
  openDialog.value = null
  queue.length = 0
}
