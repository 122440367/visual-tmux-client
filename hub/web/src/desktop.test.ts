import test from 'node:test'
import assert from 'node:assert/strict'

import {
  adoptDesktopLaunch,
  desktopInfo,
  dispatchDesktopCommand,
  initDesktop,
  installCommandReceiver,
  isDesktop,
  onDesktopCommand,
  resetDesktopForTest,
  saveDownload,
  shortcutFor,
  withShortcut,
} from './desktop'
import { getToken } from './api'

function memoryStorage(): Storage {
  const store = new Map<string, string>()
  return {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, val: string) => void store.set(key, val),
    removeItem: (key: string) => void store.delete(key),
    clear: () => store.clear(),
    key: (i: number) => Array.from(store.keys())[i] ?? null,
    get length() {
      return store.size
    },
  } as Storage
}

interface Seen {
  url: string
  method: string
  auth: string | null
  body: string
}

/** withFetch answers requests with `respond` and records what was asked. */
async function withFetch(
  respond: (url: string) => Response,
  run: (seen: Seen[]) => Promise<void>,
): Promise<void> {
  const original = globalThis.fetch
  const seen: Seen[] = []
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    seen.push({
      url,
      method: init?.method ?? 'GET',
      auth: new Headers(init?.headers).get('Authorization'),
      body: typeof init?.body === 'string' ? init.body : '',
    })
    return respond(url)
  }) as typeof fetch
  try {
    await run(seen)
  } finally {
    globalThis.fetch = original
  }
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const document = { documentElement: { dataset: {} as Record<string, string> } }

function setup(): void {
  globalThis.localStorage = memoryStorage()
  ;(globalThis as { document?: unknown }).document = document
  document.documentElement.dataset = {}
  resetDesktopForTest()
}

test('adoptDesktopLaunch stores the launch token and clears it from the address', () => {
  setup()
  const session = memoryStorage()
  const replaced: string[] = []
  const launched = adoptDesktopLaunch(
    { hash: '#vtc-desktop=t%2Bk', pathname: '/', search: '' },
    { replaceState: (_d: unknown, _t: string, url?: string | URL | null) => void replaced.push(String(url)) },
    session,
  )
  assert.equal(launched, true)
  assert.equal(getToken(), 't+k')
  assert.deepEqual(replaced, ['/'])

  // A reload has no fragment any more, and is still the shell's page.
  const again = adoptDesktopLaunch(
    { hash: '', pathname: '/', search: '' },
    { replaceState: () => assert.fail('nothing to clear') },
    session,
  )
  assert.equal(again, true)
})

test('adoptDesktopLaunch leaves a browser page alone', () => {
  setup()
  const launched = adoptDesktopLaunch(
    { hash: '#section', pathname: '/', search: '' },
    { replaceState: () => assert.fail('a browser address is not rewritten') },
    memoryStorage(),
  )
  assert.equal(launched, false)
  assert.equal(getToken(), null)
})

test('initDesktop never asks a page the shell did not open', async () => {
  setup()
  await withFetch(
    () => assert.fail('no request'),
    async (seen) => {
      assert.equal(await initDesktop(false), null)
      assert.equal(seen.length, 0)
      assert.equal(isDesktop(), false)
    },
  )
})

test('initDesktop reads the shell report, keeping only known shortcuts', async () => {
  setup()
  localStorage.setItem('visual-tmux-client-token', 'tok')
  await withFetch(
    () =>
      json({
        platform: 'darwin',
        version: '1.2.3',
        shortcuts: { 'new-session': '⌘T', bogus: 'X', 'close-panel': 7 },
      }),
    async (seen) => {
      const info = await initDesktop(true)
      assert.equal(seen[0].url, '/api/desktop/info')
      assert.equal(seen[0].auth, 'Bearer tok')
      assert.deepEqual(info, { platform: 'darwin', version: '1.2.3', shortcuts: { 'new-session': '⌘T' } })
      assert.equal(isDesktop(), true)
      assert.equal(document.documentElement.dataset.desktop, 'darwin')
      assert.equal(shortcutFor('new-session'), '⌘T')
      assert.equal(shortcutFor('close-panel'), '')
      assert.equal(withShortcut('new session', 'new-session'), 'new session (⌘T)')
      assert.equal(withShortcut('close', 'close-panel'), 'close')
    },
  )
})

test('initDesktop falls back to the browser client when the shell does not answer', async () => {
  setup()
  await withFetch(
    () => json({ error: 'not_found' }, 404),
    async () => {
      assert.equal(await initDesktop(true), null)
      assert.equal(isDesktop(), false)
      assert.equal(desktopInfo(), null)
    },
  )
  await withFetch(
    () => json({ platform: 'windows' }),
    async () => {
      assert.equal(await initDesktop(true), null, 'an unknown platform is not a shell this page knows')
    },
  )
})

test('commands reach subscribers only inside the shell, and only known ones', async () => {
  setup()
  const received: Array<[string, unknown]> = []
  const stop = onDesktopCommand((command, arg) => received.push([command, arg]))

  assert.equal(dispatchDesktopCommand('new-session'), false, 'a browser page ignores commands')

  await withFetch(
    () => json({ platform: 'linux', version: '', shortcuts: {} }),
    async () => {
      await initDesktop(true)
    },
  )
  const target = {} as Window
  installCommandReceiver(target)
  assert.equal(target.__vtcDesktop?.dispatch('go-to-session', 3), true)
  assert.equal(target.__vtcDesktop?.dispatch('format-disk'), false)
  stop()
  assert.equal(dispatchDesktopCommand('help'), true)
  assert.deepEqual(received, [['go-to-session', 3]])
})

test('saveDownload posts the file to save and reports where it went', async () => {
  setup()
  await withFetch(
    () => json({ saved: true, target: '/home/u/a.txt' }),
    async (seen) => {
      const result = await saveDownload('/srv/a.txt', 'a.txt')
      assert.deepEqual(result, { saved: true, target: '/home/u/a.txt' })
      assert.equal(seen[0].url, '/api/desktop/save')
      assert.equal(seen[0].method, 'POST')
      assert.deepEqual(JSON.parse(seen[0].body), { path: '/srv/a.txt', name: 'a.txt' })
    },
  )
  await withFetch(
    () => json({ error: 'not_found' }, 404),
    async () => {
      await assert.rejects(saveDownload('/srv/b', 'b'), /not_found/)
    },
  )
})
