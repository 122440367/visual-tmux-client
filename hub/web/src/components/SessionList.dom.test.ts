// The session list's inline rename, mounted in a document: what is selected
// when it opens.
import test from 'node:test'
import assert from 'node:assert/strict'

import '../../test/dom.mjs'

import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

import SessionList from './SessionList.vue'

const sessions = [{ name: 'build-logs', windows: 1, attached: 0, created: 1 }]

async function openRename(desktop: boolean): Promise<HTMLInputElement> {
  const wrapper = mount(SessionList, {
    attachTo: document.body,
    props: { sessions, error: '', selected: null, active: {}, desktop },
  })
  ;(wrapper.vm as unknown as { startRename(name: string): void }).startRename('build-logs')
  await nextTick()
  await nextTick()
  const el = wrapper.find('input[aria-label="Rename session"]').element as HTMLInputElement
  assert.equal(el.value, 'build-logs')
  assert.equal(document.activeElement, el)
  wrapper.unmount()
  return el
}

test('in the desktop shell a rename opens with the whole name selected', async () => {
  const el = await openRename(true)
  assert.equal(el.selectionStart, 0)
  assert.equal(el.selectionEnd, 'build-logs'.length)
})

test('in a browser a rename opens with nothing selected, as before', async () => {
  const el = await openRename(false)
  assert.equal(el.selectionStart, el.selectionEnd)
})
