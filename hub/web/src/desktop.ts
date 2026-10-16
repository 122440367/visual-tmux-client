// Desktop shell integration.
//
// The desktop application runs the hub in its own process and opens this same
// page in a native window, so everything the browser client does works there
// unchanged. What a native window adds -- a menu bar, real dialogs, saving to a
// chosen file, the system clipboard, window fullscreen -- is reached through
// this module, and nothing here is active in a plain browser.
//
// The two directions use different channels on purpose:
//
// - page → shell is an authenticated request to /api/desktop/*, routes the
//   shell mounts beside the hub's own. They carry the same bearer token as the
//   rest of the API, so a page that is not this one cannot drive them.
// - shell → page is a call to window.__vtcDesktop.dispatch, which the shell
//   evaluates in the page when a menu item fires or files are dropped on the
//   window. Commands only ask the page to do what its own controls already do.

import { authFetch, errorText, setToken } from './api'

/** The platforms the desktop shell is built for. tmux is the reason there are
 * only two: the hub needs a local tmux, and Windows has none. */
export type DesktopPlatform = 'darwin' | 'linux'

/** DesktopInfo is what the shell reports about itself once the page has loaded. */
export interface DesktopInfo {
  platform: DesktopPlatform
  version: string
  /** Accelerator labels for menu commands, keyed by command, in the platform's
   * own notation (⌘T, Ctrl+Shift+T). The shell builds the menu, so it is the one
   * place the shortcuts are defined; the page only shows them. */
  shortcuts: Partial<Record<DesktopCommand, string>>
}

/** The commands a native menu item (or a drop) can send to the page. */
export type DesktopCommand =
  | 'new-session'
  | 'close-panel'
  | 'rename-session'
  | 'kill-session'
  | 'next-session'
  | 'previous-session'
  | 'go-to-session'
  | 'toggle-sidebar'
  | 'toggle-files'
  | 'zoom-in'
  | 'zoom-out'
  | 'zoom-reset'
  | 'toggle-fullscreen'
  | 'help'
  | 'copy'
  | 'paste'
  | 'select-all'
  | 'clear-scrollback'
  | 'insert-paths'

const COMMANDS: ReadonlySet<string> = new Set<DesktopCommand>([
  'new-session',
  'close-panel',
  'rename-session',
  'kill-session',
  'next-session',
  'previous-session',
  'go-to-session',
  'toggle-sidebar',
  'toggle-files',
  'zoom-in',
  'zoom-out',
  'zoom-reset',
  'toggle-fullscreen',
  'help',
  'copy',
  'paste',
  'select-all',
  'clear-scrollback',
  'insert-paths',
])

// The launch token travels in the fragment, which is never sent to a server and
// is removed from the address as soon as it has been read. sessionStorage then
// remembers that this browsing context belongs to the shell, so a reload from
// the View menu stays a desktop page.
const LAUNCH_PREFIX = '#vtc-desktop='
const FLAG_KEY = 'vtc:desktop'

/**
 * adoptDesktopLaunch takes the credential the shell put in the address and
 * reports whether this page was opened by the shell.
 *
 * It must run before the application reads the stored token. The shell starts a
 * new hub with a new token on every launch, so a token left in storage by the
 * previous launch is always stale, and the one in the address always wins.
 */
export function adoptDesktopLaunch(
  loc: Pick<Location, 'hash' | 'pathname' | 'search'> = window.location,
  hist: Pick<History, 'replaceState'> = window.history,
  storage: Pick<Storage, 'getItem' | 'setItem'> = sessionStorage,
): boolean {
  if (loc.hash.startsWith(LAUNCH_PREFIX)) {
    const token = decodeURIComponent(loc.hash.slice(LAUNCH_PREFIX.length))
    if (token) {
      setToken(token)
      try {
        storage.setItem(FLAG_KEY, '1')
      } catch {
        /* ignore storage quota */
      }
    }
    hist.replaceState(null, '', loc.pathname + loc.search)
    return token !== ''
  }
  try {
    return storage.getItem(FLAG_KEY) === '1'
  } catch {
    return false
  }
}

let info: DesktopInfo | null = null

/** isDesktop reports whether the page is running inside the desktop shell, as
 * established by initDesktop. Every desktop behaviour branches on this, so a
 * page that never reached the shell behaves exactly like the browser client. */
export function isDesktop(): boolean {
  return info !== null
}

/** desktopInfo is the shell's report, or null in a browser. */
export function desktopInfo(): DesktopInfo | null {
  return info
}

/** shortcutFor returns the accelerator label for a command, or '' when the
 * command has none (or this is not the desktop shell). */
export function shortcutFor(command: DesktopCommand): string {
  return info?.shortcuts[command] ?? ''
}

/** withShortcut appends a command's accelerator to a tooltip, in the form the
 * platform's own menus use. */
export function withShortcut(label: string, command: DesktopCommand): string {
  const keys = shortcutFor(command)
  return keys ? `${label} (${keys})` : label
}

function parseInfo(raw: unknown): DesktopInfo | null {
  if (!raw || typeof raw !== 'object') return null
  const body = raw as Partial<DesktopInfo>
  if (body.platform !== 'darwin' && body.platform !== 'linux') return null
  const shortcuts: Partial<Record<DesktopCommand, string>> = {}
  if (body.shortcuts && typeof body.shortcuts === 'object') {
    for (const [command, label] of Object.entries(body.shortcuts)) {
      if (COMMANDS.has(command) && typeof label === 'string') {
        shortcuts[command as DesktopCommand] = label
      }
    }
  }
  return {
    platform: body.platform,
    version: typeof body.version === 'string' ? body.version : '',
    shortcuts,
  }
}

/**
 * initDesktop asks the shell to identify itself. A page that adoptDesktopLaunch
 * did not mark is never asked: the browser client has no shell, and its hub has
 * no such route. A marked page whose question goes unanswered falls back to the
 * browser behaviour rather than failing to start -- the desktop additions are
 * conveniences, and the client is complete without them.
 */
export async function initDesktop(launched: boolean): Promise<DesktopInfo | null> {
  info = null
  if (!launched) return null
  try {
    const res = await authFetch('/api/desktop/info')
    if (!res.ok) return null
    info = parseInfo(await res.json())
  } catch {
    info = null
  }
  if (info) document.documentElement.dataset.desktop = info.platform
  return info
}

/** resetDesktopForTest forgets the shell's report. */
export function resetDesktopForTest(): void {
  info = null
  handlers.clear()
}

async function call<T>(route: string, body?: unknown): Promise<T> {
  const res = await authFetch(`/api/desktop/${route}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body ?? {}),
  })
  if (!res.ok) throw new Error(await errorText(res))
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

/** setWindowTitle names the native window. document.title is not reflected in
 * the window's title bar by every platform's web view. */
export async function setWindowTitle(title: string): Promise<void> {
  await call('title', { title })
}

/** toggleWindowFullscreen switches the native window, rather than an element,
 * into fullscreen: WKWebView does not implement the element Fullscreen API for
 * an embedded view. Resolves to whether the window is now fullscreen. */
export async function toggleWindowFullscreen(): Promise<boolean> {
  const res = await call<{ fullscreen?: unknown }>('fullscreen')
  return res?.fullscreen === true
}

/** readClipboard returns the system clipboard's text through the shell, which
 * reaches it without the permission prompts a web view puts in front of
 * navigator.clipboard.readText. */
export async function readClipboard(): Promise<string> {
  const res = await call<{ text?: unknown }>('clipboard/read')
  return typeof res?.text === 'string' ? res.text : ''
}

/** writeClipboard places text on the system clipboard through the shell. */
export async function writeClipboard(text: string): Promise<void> {
  await call('clipboard/write', { text })
}

/** SaveResult says where a download went, or that the user cancelled. */
export interface SaveResult {
  saved: boolean
  target?: string
}

/**
 * saveDownload asks the shell to save a file from the hub through a native save
 * dialog. A web view does not act on <a download>, and fetching the bytes into
 * the page only to pass them back to the shell would hold a whole file in
 * memory twice; the shell fetches the hub's download route itself instead,
 * which keeps every check that route makes.
 */
export async function saveDownload(path: string, name: string): Promise<SaveResult> {
  const res = await call<{ saved?: unknown; target?: unknown }>('save', { path, name })
  return {
    saved: res?.saved === true,
    target: typeof res?.target === 'string' ? res.target : undefined,
  }
}

/** openExternal opens a link in the user's browser. Followed inside the web
 * view, a link would navigate the application itself away. */
export async function openExternal(url: string): Promise<void> {
  await call('open', { url })
}

/** reportUnsaved tells the shell whether quitting now would lose edits, so it
 * can ask before the window closes. */
export async function reportUnsaved(unsaved: boolean): Promise<void> {
  await call('unsaved', { unsaved })
}

// --- Commands from the shell ---

export type CommandHandler = (command: DesktopCommand, arg: unknown) => void

const handlers = new Set<CommandHandler>()

/** onDesktopCommand subscribes to shell commands and returns the unsubscribe. */
export function onDesktopCommand(handler: CommandHandler): () => void {
  handlers.add(handler)
  return () => handlers.delete(handler)
}

/** dispatchDesktopCommand delivers a command to every subscriber. Unknown
 * commands are dropped: the shell and the page ship together, so one is a bug,
 * and a no-op is a better failure than an exception in evaluated script. */
export function dispatchDesktopCommand(command: string, arg?: unknown): boolean {
  if (!isDesktop() || !COMMANDS.has(command)) return false
  for (const handler of handlers) handler(command as DesktopCommand, arg)
  return true
}

declare global {
  interface Window {
    __vtcDesktop?: { dispatch: (command: string, arg?: unknown) => boolean }
  }
}

/** installCommandReceiver exposes the entry point the shell evaluates. */
export function installCommandReceiver(target: Window = window): void {
  target.__vtcDesktop = { dispatch: dispatchDesktopCommand }
}
