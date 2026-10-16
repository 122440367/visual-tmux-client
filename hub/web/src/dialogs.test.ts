import test from 'node:test'
import assert from 'node:assert/strict'

import { askConfirm, askText, isDialogOpen, openDialog, resetDialogsForTest, settleDialog } from './dialogs'
import { initDesktop, resetDesktopForTest } from './desktop'

async function enterDesktop(): Promise<void> {
  const original = globalThis.fetch
  ;(globalThis as { document?: unknown }).document = { documentElement: { dataset: {} } }
  globalThis.fetch = (async () =>
    new Response(JSON.stringify({ platform: 'linux', version: '', shortcuts: {} }), {
      status: 200,
    })) as typeof fetch
  try {
    await initDesktop(true)
  } finally {
    globalThis.fetch = original
  }
}

function stubWindow(confirmAnswer: boolean, promptAnswer: string | null) {
  const asked: string[] = []
  ;(globalThis as { window?: unknown }).window = {
    confirm: (message: string) => {
      asked.push(message)
      return confirmAnswer
    },
    prompt: (message: string, initial?: string) => {
      asked.push(`${message}|${initial ?? ''}`)
      return promptAnswer
    },
  }
  return asked
}

test('a browser page asks with the browser dialogs', async () => {
  resetDesktopForTest()
  resetDialogsForTest()
  const asked = stubWindow(true, 'name')
  assert.equal(await askConfirm('Delete?'), true)
  assert.equal(await askText('Rename to', { initial: 'old' }), 'name')
  assert.deepEqual(asked, ['Delete?', 'Rename to|old'])
  assert.equal(isDialogOpen(), false)
})

test('the desktop shell asks in the page, one question at a time', async () => {
  resetDialogsForTest()
  stubWindow(false, null)
  ;(globalThis as { localStorage?: unknown }).localStorage = {
    getItem: () => null,
    setItem: () => {},
    removeItem: () => {},
  }
  await enterDesktop()

  const first = askConfirm('Kill?', { danger: true, confirmLabel: 'Kill' })
  const second = askText('Name', { initial: 'a' })
  assert.equal(openDialog.value?.kind, 'confirm')
  assert.equal(openDialog.value?.message, 'Kill?')

  settleDialog(true)
  assert.equal(await first, true)
  // The queued question replaces the answered one only once it was answered.
  assert.equal(openDialog.value?.kind, 'prompt')
  settleDialog('renamed')
  assert.equal(await second, 'renamed')
  assert.equal(isDialogOpen(), false)

  const cancelled = askText('Name')
  settleDialog(null)
  assert.equal(await cancelled, null)

  const refused = askConfirm('Overwrite?')
  settleDialog(false)
  assert.equal(await refused, false)

  resetDesktopForTest()
})
