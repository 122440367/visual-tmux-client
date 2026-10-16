// The desktop shell's in-app dialog, mounted in a document: what the keyboard
// answers, and where the focus goes.
import test from 'node:test'
import assert from 'node:assert/strict'

import '../../test/dom.mjs'

import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

import AppDialog from './AppDialog.vue'
import { askConfirm, askText, resetDialogsForTest } from '../dialogs'
import { initDesktop, resetDesktopForTest } from '../desktop'

async function enterDesktop(): Promise<void> {
  const original = globalThis.fetch
  globalThis.fetch = (async () =>
    new Response(JSON.stringify({ platform: 'darwin', version: '', shortcuts: {} }), {
      status: 200,
    })) as typeof fetch
  try {
    await initDesktop(true)
  } finally {
    globalThis.fetch = original
  }
}

async function settle(): Promise<void> {
  await nextTick()
  await new Promise((resolve) => setTimeout(resolve, 0))
  await nextTick()
}

test('a prompt opens with the stem selected and answers Enter with the trimmed text', { timeout: 5000 }, async () => {
  resetDialogsForTest()
  await enterDesktop()
  const opener = document.createElement('button')
  document.body.appendChild(opener)
  opener.focus()

  const wrapper = mount(AppDialog, { attachTo: document.body })
  const answer = askText('Rename to', { initial: 'notes.md', title: 'Rename notes.md' })
  await settle()

  const input = wrapper.find('input')
  assert.ok(input.exists())
  const el = input.element as HTMLInputElement
  assert.equal(document.activeElement, el)
  assert.equal(el.selectionStart, 0)
  assert.equal(el.selectionEnd, 'notes'.length, 'the extension is left unselected')
  assert.match(wrapper.text(), /Rename notes\.md/)

  await input.setValue('  todo.md  ')
  await input.trigger('keydown', { key: 'Enter' })
  assert.equal(await answer, 'todo.md')
  await settle()
  assert.equal(wrapper.find('input').exists(), false)
  assert.equal(document.activeElement, opener, 'the focus goes back where it was')

  wrapper.unmount()
  opener.remove()
  resetDesktopForTest()
})

test('Escape refuses, and an empty prompt cannot be confirmed', { timeout: 5000 }, async () => {
  resetDialogsForTest()
  await enterDesktop()
  const wrapper = mount(AppDialog, { attachTo: document.body })

  const confirmed = askConfirm('Delete it?', { danger: true, confirmLabel: 'Delete' })
  await settle()
  const primary = wrapper.find('.app-dialog__btn--primary')
  assert.equal(primary.text(), 'Delete')
  // Nothing that a key still being delivered could press.
  assert.equal(document.activeElement, wrapper.find('[role="alertdialog"]').element)
  assert.ok(primary.classes().includes('app-dialog__btn--danger'))
  await wrapper.find('[role="alertdialog"]').trigger('keydown', { key: 'Escape' })
  assert.equal(await confirmed, false)

  const named = askText('New file name')
  await settle()
  assert.equal(
    (wrapper.find('.app-dialog__btn--primary').element as HTMLButtonElement).disabled,
    true,
  )
  await wrapper.find('input').trigger('keydown', { key: 'Enter' })
  await wrapper.find('.app-dialog__btn:not(.app-dialog__btn--primary)').trigger('click')
  assert.equal(await named, null)

  wrapper.unmount()
  resetDesktopForTest()
})

test('Enter answers a confirmation from the panel, but not a held key', { timeout: 5000 }, async () => {
  resetDialogsForTest()
  await enterDesktop()
  const wrapper = mount(AppDialog, { attachTo: document.body })
  const answer = askConfirm('Overwrite?')
  await settle()
  const panel = wrapper.find('[role="alertdialog"]')
  await panel.trigger('keydown', { key: 'Enter', repeat: true })
  assert.ok(wrapper.find('[role="alertdialog"]').exists(), 'a repeating key is still the one that opened it')
  await panel.trigger('keydown', { key: 'Enter' })
  assert.equal(await answer, true)
  wrapper.unmount()
  resetDesktopForTest()
})
