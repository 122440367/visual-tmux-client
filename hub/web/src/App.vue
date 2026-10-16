<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import {
  AuthError,
  clearToken,
  createSession,
  getToken,
  isHeaderSafeToken,
  killSession,
  listSessions,
  renameSession,
  setToken,
  type Session,
} from './api'
import { notify } from './toasts'
import {
  clearTerminalScrollback,
  disposeTerminalSession,
  focusTerminal,
  insertIntoTerminal,
  pasteIntoTerminal,
  renameTerminalSession,
  selectAllInTerminal,
  terminalSelection,
  terminalTracksMouse,
  type ConnState,
} from './terminal'
import { getConfig, resolveFontSize } from './config'
import {
  isDesktop,
  onDesktopCommand,
  openExternal,
  readClipboard,
  reportUnsaved,
  setWindowTitle,
  shortcutFor,
  toggleWindowFullscreen,
  withShortcut,
  writeClipboard,
  type DesktopCommand,
} from './desktop'
import { askConfirm, isDialogOpen } from './dialogs'
import { applyOrder, loadOrder } from './ordering'
import { quoteForShell } from './files/pathUtils'
import SessionList from './components/SessionList.vue'
import TerminalView from './components/TerminalView.vue'
import ToastStack from './components/ToastStack.vue'
import HelpModal from './components/HelpModal.vue'
import AppDialog from './components/AppDialog.vue'
import ContextMenu, { type MenuItem } from './components/ContextMenu.vue'

// The file manager is loaded when it is first opened. It carries the editor,
// which is by far the largest dependency in the application and is of no use to
// a page that never opens the manager -- keeping it behind this boundary also
// keeps the Markdown parser out of the initial bundle, which is what HelpModal's
// own dynamic import was for.
const FileManagerOverlay = defineAsyncComponent(
  () => import('./components/files/FileManagerOverlay.vue'),
)

// Whether the page is running inside the desktop shell. Settled before the
// application mounts (see bootstrap.ts), so it never changes under a component.
const desktop = isDesktop()

const token = ref(getToken() ?? '')
const tokenInput = ref('')
const authError = ref('')
const authBusy = ref(false)

const sessions = ref<Session[]>([])
const listError = ref('')
const selected = ref<string | null>(null)

// Every map keyed by session name uses this, and a session name is arbitrary
// user text: the hub accepts anything that is valid UTF-8 without `:`, `.`,
// control characters or full-width lookalikes — which includes `constructor`,
// `toString`, `valueOf`, `__proto__` and `hasOwnProperty`. On a plain `{}`
// those names resolve through Object.prototype, so a lookup for a session that
// has no entry returns an inherited function instead of undefined: the
// `=== undefined` panel-identity guard never fires (so the panel key becomes a
// function and a killed session's successor reuses its dead KeepAlive entry),
// `!!activity[name]` is permanently true, the state pill renders a function
// body, and `delete` cannot remove what was never an own property. A
// null-prototype object inherits no names at all.
//
// These are paired with shallowRef, not ref: a deep ref wraps the object in a
// reactive proxy, and that proxy answers `hasOwnProperty` (plus its internal
// `__v_raw` / `__v_isReactive` / `__v_isReadonly` flags) even on a
// null-prototype target — re-opening the same hole for those names. Every
// update below replaces the whole map, so shallow tracking is all that is
// needed.
function nameMap<T>(...sources: Array<Record<string, T> | undefined>): Record<string, T> {
  return Object.assign(Object.create(null) as Record<string, T>, ...sources)
}

const connStates = shallowRef<Record<string, ConnState>>(nameMap())

const hasToken = computed(() => !!token.value)

// --- Terminal panel identity ---
// KeepAlive caches one TerminalView per key, and nothing ever evicts an entry.
// Keying by session name would therefore strand a panel whenever its session is
// renamed: the name changes, the session does not, so the still-live panel
// (xterm instance, WebSocket, and the hub-side tmux client, pty and
// `tmux attach-session` process behind it) stays cached under a name that no
// longer exists while a second attachment mounts beside it. Keying by a stable
// per-session id that renames carry over keeps one panel per session, so a
// rename reuses the existing attachment instead of leaking it.
const panelIds = shallowRef<Record<string, number>>(nameMap())
let nextPanelId = 1

function select(name: string): void {
  if (panelIds.value[name] === undefined) {
    panelIds.value = nameMap(panelIds.value, { [name]: nextPanelId++ })
  }
  selected.value = name
}

const selectedPanelId = computed(() =>
  selected.value === null ? null : panelIds.value[selected.value] ?? null,
)

// Whether the selected session still exists server-side, from the same polled
// list that renders the sidebar. Drives the terminal's ended overlay: a
// detached-but-alive session offers Reconnect; a gone one states its end.
const selectedAlive = computed(() =>
  selected.value !== null && sessions.value.some((s) => s.name === selected.value),
)

// --- Terminal header: font size (persisted), fullscreen, close ---

const FONT_SIZE_KEY = 'vtc:font-size'
const termConfig = computed(() => getConfig().web.terminal)

function loadFontSize(): number {
  const cfg = getConfig().web.terminal
  const raw = localStorage.getItem(FONT_SIZE_KEY)
  const resolved = resolveFontSize(raw, cfg)
  if (raw !== null && String(resolved) !== raw) {
    try {
      localStorage.setItem(FONT_SIZE_KEY, String(resolved))
    } catch {
      /* ignore storage quota */
    }
  }
  return resolved
}

const fontSize = ref(loadFontSize())

function setFontSize(px: number): void {
  const cfg = getConfig().web.terminal
  const clamped = Math.min(cfg.max_font_size, Math.max(cfg.min_font_size, px))
  fontSize.value = clamped
  try {
    localStorage.setItem(FONT_SIZE_KEY, String(clamped))
  } catch {
    /* ignore storage quota */
  }
}

const mainEl = ref<HTMLElement | null>(null)

// Fullscreen targets the main region (header + terminal) so the controls stay
// reachable. Size re-negotiation needs no handler here: the terminal's
// ResizeObserver fires when the region's box changes.
// Both calls return promises that reject when the browser refuses (no user
// activation, a permissions policy, or exiting when not fullscreen); an
// unhandled rejection would trip the global error banner in main.ts.
function toggleFullscreen(): void {
  // The shell switches the window itself, the way a native application's
  // fullscreen works; WKWebView offers no element fullscreen to an embedder.
  if (desktop) {
    toggleWindowFullscreen().catch(() => {
      notify('warning', 'The window could not switch to fullscreen.')
    })
    return
  }
  if (document.fullscreenElement) {
    document.exitFullscreen().catch(() => {
      /* already exited */
    })
  } else {
    mainEl.value?.requestFullscreen().catch(() => {
      notify('warning', 'The browser refused to enter fullscreen.')
    })
  }
}

// --- File manager ---
// The overlay is rendered inside the terminal region rather than over the whole
// application, so it cannot cover the sidebar or the toasts -- the notification
// containment fix (#6) established that rule for this app. Its state lives in
// the component and is discarded when it closes, which is why closing the panel
// warns first.
const fileManagerOpen = ref(false)
const filesDirty = ref(false)

// The hub publishes whether it offers a file manager at all. A button that
// always answers "not found" invites a support question, so the entry point is
// left out rather than offered and refused.
const filesAvailable = computed(() => getConfig().files.enabled)

/** closeFileManager drops the overlay and the flag it reported.
 *
 * The flag is cleared here rather than left to the overlay: the watcher that
 * reports it stops when the component unmounts, so a manager closed while dirty
 * would leave `true` behind forever and the next panel close would warn about
 * unsaved changes that no longer exist. */
function closeFileManager(): void {
  fileManagerOpen.value = false
  filesDirty.value = false
}

/** toggleFileManager warns before discarding unsaved edits.
 *
 * The overlay's own close button asks for the same confirmation, so this is not
 * the only guard -- but the header button is a second way out, and closing
 * through it used to unmount the manager without a word. */
async function toggleFileManager(): Promise<void> {
  if (!filesAvailable.value) return
  if (!fileManagerOpen.value) {
    fileManagerOpen.value = true
    return
  }
  if (
    filesDirty.value &&
    !(await askConfirm('The file manager has unsaved changes. Close it anyway?', {
      title: 'Unsaved changes',
      confirmLabel: 'Discard changes',
      danger: true,
    }))
  ) {
    return
  }
  closeFileManager()
}

/** insertPathIntoTerminal writes text the file manager prepared. The manager has
 * already quoted it and made sure it carries no line terminator; all that is left
 * is to find a live attachment, and to say so when there is none rather than
 * letting the insertion look like it worked. */
function insertPathIntoTerminal(text: string): void {
  if (selected.value === null || !insertIntoTerminal(selected.value, text)) {
    notify('warning', 'No live terminal to insert into.')
  }
}

// Close detaches the view, never the session: the terminal keeps streaming in
// the KeepAlive cache, so the session stays alive and can still show activity.
async function closePanel(): Promise<void> {
  if (filesDirty.value) {
    const ok = await askConfirm('The file manager has unsaved changes. Close the panel anyway?', {
      title: 'Unsaved changes',
      confirmLabel: 'Discard changes',
      danger: true,
    })
    if (!ok) return
  }
  if (document.fullscreenElement) {
    document.exitFullscreen().catch(() => {
      /* already exited */
    })
  }
  closeFileManager()
  selected.value = null
}

// --- Session activity indication ---
// Background terminals keep streaming (KeepAlive), so "which session just
// produced output" is observable for free: each activity event lights the
// session's card (or, for the viewed session, the header dot and the tab
// title) and a decay timer dims it ~2 s after output stops.

const activity = shallowRef<Record<string, boolean>>(nameMap())
const activityTimers = new Map<string, ReturnType<typeof setTimeout>>()

function onActivity(name: string): void {
  if (!activity.value[name]) {
    activity.value = nameMap(activity.value, { [name]: true })
  }
  const timer = activityTimers.get(name)
  if (timer) clearTimeout(timer)
  activityTimers.set(
    name,
    setTimeout(() => {
      activityTimers.delete(name)
      const next = nameMap(activity.value)
      delete next[name]
      activity.value = next
    }, getConfig().web.activity_decay),
  )
}

const selectedActive = computed(
  () => selected.value !== null && !!activity.value[selected.value],
)

// The tab-title mark is the only signal visible when the browser tab itself
// is backgrounded — the whole point of the prefix. A desktop window has no tab
// strip beside it, so its title also names the session, the way a terminal
// application's window does.
const BASE_TITLE = 'Visual Tmux Client'
const windowTitle = computed(() => {
  const mark = selectedActive.value ? '● ' : ''
  if (!desktop) return `${mark}${BASE_TITLE}`
  return selected.value === null ? BASE_TITLE : `${mark}${selected.value} — ${BASE_TITLE}`
})
watch(
  windowTitle,
  (title) => {
    document.title = title
    if (desktop) {
      setWindowTitle(title).catch(() => {
        /* the title is cosmetic; a failed update leaves the previous one */
      })
    }
  },
  { immediate: desktop },
)

// The shell asks before quitting with edits the file manager has not saved.
watch(filesDirty, (unsaved) => {
  if (!desktop) return
  reportUnsaved(unsaved).catch(() => {
    /* the shell then quits without asking, as a browser tab closes */
  })
})

// --- Sidebar collapse (persisted) ---

const SIDEBAR_KEY = 'vtc:sidebar-collapsed'
const sidebarCollapsed = ref(localStorage.getItem(SIDEBAR_KEY) === '1')

function toggleSidebar(): void {
  sidebarCollapsed.value = !sidebarCollapsed.value
  try {
    localStorage.setItem(SIDEBAR_KEY, sidebarCollapsed.value ? '1' : '0')
  } catch {
    /* ignore storage quota */
  }
}

// --- Sidebar width (desktop shell, persisted) ---
// A desktop window is resized far more freely than a browser tab, so the
// sidebar is resized with it: its edge is a splitter, as in a native sidebar.

const SIDEBAR_WIDTH_KEY = 'vtc:sidebar-width'
const SIDEBAR_DEFAULT_WIDTH = 260
const SIDEBAR_MIN_WIDTH = 200
const SIDEBAR_MAX_WIDTH = 480

function clampSidebarWidth(px: number): number {
  return Math.round(Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, px)))
}

function loadSidebarWidth(): number {
  const raw = Number.parseInt(localStorage.getItem(SIDEBAR_WIDTH_KEY) ?? '', 10)
  return Number.isNaN(raw) ? SIDEBAR_DEFAULT_WIDTH : clampSidebarWidth(raw)
}

const sidebarWidth = ref(desktop ? loadSidebarWidth() : SIDEBAR_DEFAULT_WIDTH)
const sidebarResizing = ref(false)
const sidebarEl = ref<HTMLElement | null>(null)

function saveSidebarWidth(): void {
  try {
    localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth.value))
  } catch {
    /* ignore storage quota */
  }
}

function startSidebarResize(ev: PointerEvent): void {
  if (ev.button !== 0 || !sidebarEl.value) return
  ev.preventDefault()
  const handle = ev.currentTarget as HTMLElement
  const left = sidebarEl.value.getBoundingClientRect().left
  handle.setPointerCapture(ev.pointerId)
  sidebarResizing.value = true
  const move = (e: PointerEvent) => {
    sidebarWidth.value = clampSidebarWidth(e.clientX - left)
  }
  const end = () => {
    sidebarResizing.value = false
    handle.removeEventListener('pointermove', move)
    handle.removeEventListener('pointerup', end)
    handle.removeEventListener('pointercancel', end)
    saveSidebarWidth()
  }
  handle.addEventListener('pointermove', move)
  handle.addEventListener('pointerup', end)
  handle.addEventListener('pointercancel', end)
}

function onSplitterKey(ev: KeyboardEvent): void {
  const step = ev.shiftKey ? 40 : 10
  if (ev.key === 'ArrowLeft') sidebarWidth.value = clampSidebarWidth(sidebarWidth.value - step)
  else if (ev.key === 'ArrowRight') sidebarWidth.value = clampSidebarWidth(sidebarWidth.value + step)
  else if (ev.key === 'Home') sidebarWidth.value = SIDEBAR_MIN_WIDTH
  else if (ev.key === 'End') sidebarWidth.value = SIDEBAR_MAX_WIDTH
  else return
  ev.preventDefault()
  saveSidebarWidth()
}

function resetSidebarWidth(): void {
  sidebarWidth.value = SIDEBAR_DEFAULT_WIDTH
  saveSidebarWidth()
}

const sidebarStyle = computed(() =>
  desktop && !sidebarCollapsed.value
    ? { width: `${sidebarWidth.value}px`, flexBasis: `${sidebarWidth.value}px` }
    : undefined,
)

// --- In-app help ---

const helpOpen = ref(false)

let pollTimer: ReturnType<typeof setInterval> | null = null

function startPolling() {
  if (pollTimer) return
  pollTimer = setInterval(() => void refresh(), getConfig().web.session_poll_interval)
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

let refreshSeq = 0

async function refresh() {
  if (!hasToken.value) return
  const seq = ++refreshSeq
  try {
    const list = await listSessions()
    if (seq !== refreshSeq) return
    sessions.value = list
    listError.value = ''
    startPolling()

    // Prune dead panels for sessions that disappeared externally (e.g. killed via CLI).
    const liveNames = new Set(list.map((s) => s.name))
    const deadNames = new Set<string>()
    for (const name of Object.keys(panelIds.value)) {
      if (!liveNames.has(name) && selected.value !== name) deadNames.add(name)
    }
    for (const name of Object.keys(connStates.value)) {
      if (!liveNames.has(name) && selected.value !== name) deadNames.add(name)
    }
    for (const name of Object.keys(activity.value)) {
      if (!liveNames.has(name) && selected.value !== name) deadNames.add(name)
    }
    for (const name of deadNames) {
      dropPanel(name)
    }
  } catch (err) {
    if (seq !== refreshSeq) return
    if (err instanceof AuthError) {
      handleAuthFailure()
    } else {
      listError.value = err instanceof Error ? err.message : String(err)
    }
  }
}

function cleanupSessionState() {
  for (const name of Object.keys(panelIds.value)) {
    disposeTerminalSession(name)
  }
  panelIds.value = nameMap()
  connStates.value = nameMap()
  activity.value = nameMap()
  for (const timer of activityTimers.values()) clearTimeout(timer)
  activityTimers.clear()
}

function handleAuthFailure() {
  stopPolling()
  clearToken()
  token.value = ''
  // The credential is gone, so nothing could be saved anyway; asking about
  // unsaved edits here would be a question with only one answer.
  closeFileManager()
  selected.value = null
  sessions.value = []
  cleanupSessionState()
  authError.value = 'Authentication failed. Re-enter the token.'
}

// The main view opens only after the server has accepted the credential:
// submitting stores the token (authFetch reads it from storage), probes with
// a session listing, and bounces back to this prompt on any failure. A token
// that cannot be sent in an HTTP header (full-width IME characters look
// identical in the password box) is rejected without a round trip.
async function submitToken() {
  const t = tokenInput.value.trim()
  if (!t || authBusy.value) return
  if (!isHeaderSafeToken(t)) {
    authError.value =
      'The token contains characters that cannot be sent in an HTTP header — ' +
      'check for full-width characters (e.g. from an IME), which look identical but are not.'
    return
  }
  authBusy.value = true
  authError.value = ''
  try {
    setToken(t)
    const list = await listSessions()
    token.value = t
    tokenInput.value = ''
    sessions.value = list
    listError.value = ''
    startPolling()
  } catch (err) {
    clearToken()
    if (err instanceof AuthError) {
      authError.value = 'Authentication failed. Check the token and try again.'
    } else {
      authError.value = err instanceof Error ? err.message : String(err)
    }
  } finally {
    authBusy.value = false
  }
}

function logout() {
  stopPolling()
  clearToken()
  token.value = ''
  // As with an auth failure: the token is gone, so there is nothing to ask.
  closeFileManager()
  selected.value = null
  sessions.value = []
  cleanupSessionState()
}

// One-click creation: the hub assigns the default name (made unique by its
// suffix retry); renaming is how the user personalizes it afterwards.
const creating = ref(false)
async function onCreate() {
  if (creating.value) return
  creating.value = true
  try {
    const sess = await createSession()
    await refresh()
    select(sess.name)
  } catch (err) {
    if (err instanceof AuthError) {
      handleAuthFailure()
      return
    }
    notify('error', err instanceof Error ? err.message : String(err))
  } finally {
    creating.value = false
  }
}

async function onRename(oldName: string, newName: string) {
  try {
    await renameSession(oldName, newName)
    // Carry the panel identity across the rename, so the live attachment for
    // this session is reused rather than stranded in the KeepAlive cache under
    // a name that no longer exists (see panelIds).
    const id = panelIds.value[oldName]
    if (id !== undefined) {
      const next = nameMap(panelIds.value, { [newName]: id })
      delete next[oldName]
      panelIds.value = next
    }
    if (connStates.value[oldName] !== undefined) {
      const nextStates = nameMap(connStates.value, { [newName]: connStates.value[oldName] })
      delete nextStates[oldName]
      connStates.value = nextStates
    }
    if (activity.value[oldName] !== undefined) {
      const nextAct = nameMap(activity.value, { [newName]: activity.value[oldName] })
      delete nextAct[oldName]
      activity.value = nextAct
    }
    const timer = activityTimers.get(oldName)
    if (timer) {
      clearTimeout(timer)
      activityTimers.delete(oldName)
      activityTimers.set(
        newName,
        setTimeout(() => {
          activityTimers.delete(newName)
          const next = nameMap(activity.value)
          delete next[newName]
          activity.value = next
        }, getConfig().web.activity_decay),
      )
    }
    // Follow the rename: `selected` is the viewed terminal's identity — it
    // names the ws ticket and the header. Left on the old name, the polled
    // list no longer contains it, so a live session reads as ended and every
    // reconnect asks for a ticket the hub answers with session_not_found.
    if (selected.value === oldName) selected.value = newName
    // Retarget the attachment itself. For the viewed panel the `session` prop
    // change would reach it, but a panel parked in the KeepAlive cache never
    // re-renders, so no prop can: without this its terminal keeps the old name
    // and its next reconnect asks the hub for a session that no longer exists.
    renameTerminalSession(oldName, newName)
    await refresh()
  } catch (err) {
    if (err instanceof AuthError) {
      handleAuthFailure()
      return
    }
    notify('error', err instanceof Error ? err.message : String(err))
  }
}

// Killing a session retires its panel identity, so a later session that
// happens to reuse the name gets a fresh panel rather than the dead one still
// sitting in the KeepAlive cache (which would greet a brand-new session with an
// "ended" overlay). Retiring the key makes the cached panel unreachable but not
// gone — KeepAlive only unmounts on eviction, and nothing evicts here — so its
// terminal is disposed explicitly, or its xterm instance, scrollback and
// ResizeObserver would be retained for the life of the page.
function dropPanel(name: string): void {
  if (panelIds.value[name] !== undefined) {
    const next = nameMap(panelIds.value)
    delete next[name]
    panelIds.value = next
  }
  if (connStates.value[name] !== undefined) {
    const nextStates = nameMap(connStates.value)
    delete nextStates[name]
    connStates.value = nextStates
  }
  if (activity.value[name] !== undefined) {
    const nextAct = nameMap(activity.value)
    delete nextAct[name]
    activity.value = nextAct
  }
  const timer = activityTimers.get(name)
  if (timer) {
    clearTimeout(timer)
    activityTimers.delete(name)
  }
  disposeTerminalSession(name)
}

async function onKill(name: string) {
  try {
    await killSession(name)
    if (selected.value === name) selected.value = null
    dropPanel(name)
    await refresh()
  } catch (err) {
    if (err instanceof AuthError) {
      handleAuthFailure()
      return
    }
    notify('error', err instanceof Error ? err.message : String(err))
  }
}

// Bulk kill issues one DELETE per session sequentially and names the sessions
// that failed, so a mid-batch failure never hides behind the successes.
async function onBulkKill(names: string[]) {
  const failed: string[] = []
  for (const name of names) {
    try {
      await killSession(name)
      if (selected.value === name) selected.value = null
      dropPanel(name)
    } catch (err) {
      if (err instanceof AuthError) {
        handleAuthFailure()
        return
      }
      failed.push(name)
    }
  }
  if (failed.length > 0) {
    notify('error', `Failed to kill: ${failed.join(', ')}`)
  }
  await refresh()
}

function onState(session: string, state: ConnState) {
  connStates.value = nameMap(connStates.value, { [session]: state })
}

function onNotice(message: string, level: 'error' | 'warning' = 'error') {
  notify(level, message)
}

function onOnline() {
  if (hasToken.value) {
    void refresh()
  }
}

function onOffline() {
  notify('warning', 'Network connection offline')
}

// --- Desktop shell: menu commands ---
// Every command does what one of the page's own controls does, so the menu bar
// is another way to reach the application rather than a second implementation
// of it.

const sessionListRef = ref<InstanceType<typeof SessionList> | null>(null)

/** orderedNames is the sidebar's order, which is what "next" means to someone
 * looking at it. Read from the stored order rather than from the list, because
 * a collapsed sidebar has no list mounted to ask. */
function orderedNames(): string[] {
  return applyOrder(sessions.value, loadOrder()).map((s) => s.name)
}

function cycleSession(delta: 1 | -1): void {
  const names = orderedNames()
  if (names.length === 0) return
  const at = selected.value === null ? -1 : names.indexOf(selected.value)
  const next =
    at < 0 ? (delta > 0 ? 0 : names.length - 1) : (at + delta + names.length) % names.length
  select(names[next])
}

function goToSession(position: unknown): void {
  const names = orderedNames()
  if (typeof position !== 'number' || !Number.isInteger(position)) return
  if (position >= 1 && position <= names.length) select(names[position - 1])
}

async function renameSelected(): Promise<void> {
  const name = selected.value
  if (name === null) return
  if (sidebarCollapsed.value) toggleSidebar()
  await nextTick()
  sessionListRef.value?.startRename(name)
}

async function killSelected(): Promise<void> {
  const name = selected.value
  if (name === null) return
  const ok = await askConfirm(
    `Kill session “${name}”? Every program running in it will be terminated.`,
    { title: 'Kill session', confirmLabel: 'Kill', danger: true },
  )
  if (ok && sessions.value.some((s) => s.name === name)) await onKill(name)
}

/** editableFocus is the text field the keyboard is in, if any. xterm's own
 * input element is excluded: it is a textarea, but what it holds is not the
 * terminal's text. */
function editableFocus(): HTMLElement | null {
  const el = document.activeElement
  if (!(el instanceof HTMLElement) || el.classList.contains('xterm-helper-textarea')) return null
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el.isContentEditable) {
    return el
  }
  return null
}

async function copyCommand(): Promise<void> {
  const field = editableFocus()
  let text = ''
  if (field instanceof HTMLInputElement || field instanceof HTMLTextAreaElement) {
    text = field.value.slice(field.selectionStart ?? 0, field.selectionEnd ?? 0)
  } else if (field === null && selected.value !== null) {
    text = terminalSelection(selected.value)
  }
  if (!text) text = window.getSelection()?.toString() ?? ''
  if (!text) return
  try {
    await writeClipboard(text)
  } catch {
    notify('warning', 'Copy failed: the clipboard could not be written.')
  }
}

async function pasteCommand(): Promise<void> {
  let text: string
  try {
    text = await readClipboard()
  } catch {
    notify('warning', 'Paste failed: the clipboard could not be read.')
    return
  }
  if (!text) return
  const field = editableFocus()
  if (field) {
    // insertText goes through the field's own editing, so it lands at the
    // caret, replaces a selection and can be undone -- which assigning to the
    // value would not do, and which a CodeMirror editor would not even notice.
    document.execCommand('insertText', false, text)
    return
  }
  if (selected.value === null || !pasteIntoTerminal(selected.value, text)) {
    notify('warning', 'No live terminal to paste into.')
  }
}

function selectAllCommand(): void {
  const field = editableFocus()
  if (field instanceof HTMLInputElement || field instanceof HTMLTextAreaElement) {
    field.select()
  } else if (field) {
    document.execCommand('selectAll')
  } else if (selected.value !== null) {
    selectAllInTerminal(selected.value)
  }
}

/** insertPaths types the paths of files dropped on the window into the
 * terminal, quoted, the way a terminal application does. Nothing runs: the
 * quoting refuses a path with a control character, and the text ends with a
 * space rather than a newline. */
function insertPaths(paths: unknown): void {
  if (!Array.isArray(paths)) return
  const quoted: string[] = []
  for (const path of paths) {
    if (typeof path !== 'string') continue
    const text = quoteForShell(path)
    if (text === null) {
      notify('warning', 'A dropped path contains a control character, so nothing was inserted.')
      return
    }
    quoted.push(text)
  }
  if (quoted.length === 0) return
  if (selected.value === null || !insertIntoTerminal(selected.value, `${quoted.join(' ')} `)) {
    notify('warning', 'Open a session to drop files into its terminal.')
    return
  }
  focusTerminal(selected.value)
}

// While a dialog is up the window is modal, as it would be under a native
// sheet: only the editing commands, which can act on the dialog's own field,
// get through.
const MODAL_COMMANDS: ReadonlySet<DesktopCommand> = new Set(['copy', 'paste', 'select-all'])

function onDesktopCommandReceived(command: DesktopCommand, arg: unknown): void {
  if (!hasToken.value) return
  if (isDialogOpen() && !MODAL_COMMANDS.has(command)) return
  switch (command) {
    case 'new-session':
      void onCreate()
      break
    case 'close-panel':
      if (selected.value !== null) void closePanel()
      break
    case 'rename-session':
      void renameSelected()
      break
    case 'kill-session':
      void killSelected()
      break
    case 'next-session':
      cycleSession(1)
      break
    case 'previous-session':
      cycleSession(-1)
      break
    case 'go-to-session':
      goToSession(arg)
      break
    case 'toggle-sidebar':
      toggleSidebar()
      break
    case 'toggle-files':
      if (selected.value !== null) void toggleFileManager()
      break
    case 'zoom-in':
      setFontSize(fontSize.value + 1)
      break
    case 'zoom-out':
      setFontSize(fontSize.value - 1)
      break
    case 'zoom-reset':
      setFontSize(getConfig().web.terminal.font_size)
      break
    case 'toggle-fullscreen':
      toggleFullscreen()
      break
    case 'help':
      helpOpen.value = !helpOpen.value
      break
    case 'copy':
      void copyCommand()
      break
    case 'paste':
      void pasteCommand()
      break
    case 'select-all':
      selectAllCommand()
      break
    case 'clear-scrollback':
      if (selected.value !== null) clearTerminalScrollback(selected.value)
      break
    case 'insert-paths':
      insertPaths(arg)
      break
  }
}

// --- Desktop shell: the terminal's right-click menu ---

const terminalMenu = ref<{ x: number; y: number; items: MenuItem[] } | null>(null)

function onTerminalContextMenu(ev: MouseEvent): void {
  if (!desktop || selected.value === null) return
  // A program that asked for mouse reports gets the right click: tmux has a
  // menu of its own there. Shift reaches this one instead, which is also how
  // xterm lets a selection through mouse reporting.
  if (terminalTracksMouse(selected.value) && !ev.shiftKey) {
    ev.preventDefault()
    return
  }
  ev.preventDefault()
  const hasSelection = terminalSelection(selected.value) !== ''
  const items: MenuItem[] = [
    { id: 'copy', label: 'Copy', shortcut: shortcutFor('copy'), disabled: !hasSelection },
    { id: 'paste', label: 'Paste', shortcut: shortcutFor('paste') },
    { id: 'select-all', label: 'Select All', shortcut: shortcutFor('select-all') },
    {
      id: 'clear-scrollback',
      label: 'Clear Scrollback',
      shortcut: shortcutFor('clear-scrollback'),
      separated: true,
    },
  ]
  if (filesAvailable.value) {
    items.push({
      id: 'toggle-files',
      label: fileManagerOpen.value ? 'Hide Files' : 'Browse Files',
      shortcut: shortcutFor('toggle-files'),
      separated: true,
    })
  }
  items.push({
    id: 'close-panel',
    label: 'Close Panel',
    shortcut: shortcutFor('close-panel'),
    separated: !filesAvailable.value,
  })
  terminalMenu.value = { x: ev.clientX, y: ev.clientY, items }
}

function onTerminalMenuSelect(id: string): void {
  const name = selected.value
  if (name === null) return
  if (id === 'copy') {
    const text = terminalSelection(name)
    if (text) {
      writeClipboard(text).catch(() => notify('warning', 'Copy failed: the clipboard could not be written.'))
    }
  } else if (id === 'paste') {
    focusTerminal(name)
    void pasteCommand()
  } else if (id === 'select-all') {
    selectAllInTerminal(name)
  } else if (id === 'clear-scrollback') {
    clearTerminalScrollback(name)
  } else if (id === 'toggle-files') {
    void toggleFileManager()
  } else if (id === 'close-panel') {
    void closePanel()
  }
}

function closeTerminalMenu(): void {
  terminalMenu.value = null
  if (selected.value !== null && !fileManagerOpen.value) focusTerminal(selected.value)
}

// --- Desktop shell: what the web view would otherwise do on its own ---

/** onDocumentContextMenu keeps the web view's own menu (Reload, Back, Inspect
 * Element) out of the application. Text fields keep theirs, which is where Cut,
 * Copy and Paste are expected. */
function onDocumentContextMenu(ev: MouseEvent): void {
  const target = ev.target
  const editable =
    (target instanceof HTMLInputElement ||
      target instanceof HTMLTextAreaElement ||
      (target instanceof HTMLElement && target.isContentEditable)) &&
    !target.classList.contains('xterm-helper-textarea')
  if (!editable) ev.preventDefault()
}

/** onDocumentClick sends links to other sites to the user's browser. Followed
 * in place, a link would navigate the application window away from itself. */
function onDocumentClick(ev: MouseEvent): void {
  if (ev.defaultPrevented || ev.button !== 0) return
  const anchor = ev.target instanceof Element ? ev.target.closest('a[href]') : null
  if (!(anchor instanceof HTMLAnchorElement)) return
  let url: URL
  try {
    url = new URL(anchor.href, window.location.href)
  } catch {
    return
  }
  if (url.origin === window.location.origin && anchor.target !== '_blank') return
  ev.preventDefault()
  if (url.protocol === 'http:' || url.protocol === 'https:' || url.protocol === 'mailto:') {
    openExternal(url.href).catch(() => notify('warning', `Could not open ${url.href}`))
  }
}

/** A drag that carries files from outside the window. Reordering sessions
 * drags text, so it is never mistaken for one. */
function isFileDrag(ev: DragEvent): boolean {
  return Array.from(ev.dataTransfer?.types ?? []).includes('Files')
}

/** onDocumentDragOver accepts a file drag anywhere in the window, so the
 * pointer shows a copy rather than a refusal. The shell reports the drop to
 * the page as paths (insertPaths). */
function onDocumentDragOver(ev: DragEvent): void {
  if (!isFileDrag(ev)) return
  ev.preventDefault()
  if (ev.dataTransfer) ev.dataTransfer.dropEffect = 'copy'
}

/** onDocumentDrop cancels the web view's own handling of a dropped file,
 * which is to open it in place of the application. */
function onDocumentDrop(ev: DragEvent): void {
  if (isFileDrag(ev)) ev.preventDefault()
}

let stopDesktopCommands: (() => void) | null = null

onMounted(() => {
  if (hasToken.value) void refresh()
  window.addEventListener('online', onOnline)
  window.addEventListener('offline', onOffline)
  if (desktop) {
    stopDesktopCommands = onDesktopCommand(onDesktopCommandReceived)
    document.addEventListener('contextmenu', onDocumentContextMenu)
    document.addEventListener('click', onDocumentClick)
    document.addEventListener('dragover', onDocumentDragOver)
    document.addEventListener('drop', onDocumentDrop)
  }
})

onBeforeUnmount(() => {
  stopPolling()
  window.removeEventListener('online', onOnline)
  window.removeEventListener('offline', onOffline)
  stopDesktopCommands?.()
  stopDesktopCommands = null
  document.removeEventListener('contextmenu', onDocumentContextMenu)
  document.removeEventListener('click', onDocumentClick)
  document.removeEventListener('dragover', onDocumentDragOver)
  document.removeEventListener('drop', onDocumentDrop)
  cleanupSessionState()
})
</script>

<template>
  <div class="app" :class="{ 'app--desktop': desktop, 'app--resizing': sidebarResizing }">
    <div v-if="!hasToken && desktop" class="app__auth">
      <!-- The shell hands the page its credential, so there is nothing to type:
           losing it means the embedded hub refused it, and only a fresh launch
           of the page carries a new one. -->
      <div class="auth-card" role="alert">
        <h1 class="auth-card__title">Visual Tmux Client</h1>
        <p class="auth-card__hint">
          The window lost its connection to the embedded hub. Choose View → Reload to reconnect.
        </p>
      </div>
      <ToastStack />
    </div>
    <div v-else-if="!hasToken" class="app__auth">
      <div class="auth-card">
        <h1 class="auth-card__title">Visual Tmux Client</h1>
        <p class="auth-card__hint">Enter the access token to connect.</p>
        <label class="sr-only" for="access-token">Access token</label>
        <input
          id="access-token"
          v-model="tokenInput"
          type="password"
          class="auth-card__input"
          placeholder="token"
          autocomplete="off"
          :disabled="authBusy"
          @keyup.enter="submitToken"
        />
        <button class="auth-card__btn" type="button" :disabled="authBusy" @click="submitToken">
          {{ authBusy ? 'Connecting…' : 'Connect' }}
        </button>
        <p v-if="authError" class="auth-card__error" role="alert" aria-live="assertive">{{ authError }}</p>
      </div>
      <ToastStack />
    </div>

    <template v-else>
      <header v-if="!desktop" class="app__bar">
        <span class="app__title">Visual Tmux Client</span>
        <button class="app__logout" type="button" aria-label="Disconnect and clear credentials" @click="logout">Disconnect</button>
      </header>

      <div class="app__body">
        <aside
          ref="sidebarEl"
          class="app__sidebar"
          :class="{ 'app__sidebar--collapsed': sidebarCollapsed }"
          :style="sidebarStyle"
        >
          <template v-if="!sidebarCollapsed">
            <button
              class="app__sidebar-toggle"
              type="button"
              :title="withShortcut('collapse sidebar', 'toggle-sidebar')"
              aria-label="Collapse session sidebar"
              :aria-expanded="!sidebarCollapsed"
              @click="toggleSidebar"
            ><span aria-hidden="true">«</span></button>
            <div class="app__sidebar-body">
              <SessionList
                ref="sessionListRef"
                :desktop="desktop"
                :sessions="sessions"
                :error="listError"
                :selected="selected"
                :active="activity"
                :creating="creating"
                @select="select($event)"
                @create="onCreate"
                @rename="onRename"
                @kill="onKill"
                @bulk-kill="onBulkKill"
                @retry="refresh"
              />
            </div>
            <button class="app__help-btn" type="button" :title="withShortcut('tmux 使用指南', 'help')" @click="helpOpen = true">
              ? Help
            </button>
          </template>
          <div v-else class="app__rail">
            <button
              class="app__rail-btn"
              type="button"
              :title="withShortcut('expand sidebar', 'toggle-sidebar')"
              aria-label="Expand session sidebar"
              :aria-expanded="!sidebarCollapsed"
              @click="toggleSidebar"
            ><span aria-hidden="true">»</span></button>
            <button class="app__rail-btn" type="button" :title="withShortcut('new session', 'new-session')" aria-label="Create new session" :disabled="creating" @click="onCreate">
              <span aria-hidden="true">+</span>
            </button>
            <button class="app__rail-btn" type="button" :title="withShortcut('help', 'help')" aria-label="Open tmux help" @click="helpOpen = true">
              <span aria-hidden="true">?</span>
            </button>
          </div>
        </aside>
        <div
          v-if="desktop && !sidebarCollapsed"
          class="app__splitter"
          role="separator"
          tabindex="0"
          aria-orientation="vertical"
          aria-label="Resize session sidebar"
          :aria-valuenow="sidebarWidth"
          :aria-valuemin="SIDEBAR_MIN_WIDTH"
          :aria-valuemax="SIDEBAR_MAX_WIDTH"
          title="drag to resize · double-click to reset"
          @pointerdown="startSidebarResize"
          @dblclick="resetSidebarWidth"
          @keydown="onSplitterKey"
        ></div>

        <main ref="mainEl" class="app__main">
          <header v-if="selected" class="app__term-header">
            <span
              class="app__term-dot"
              :class="{ 'app__term-dot--live': selectedActive }"
              :title="selectedActive ? 'producing output' : 'idle'"
              aria-hidden="true"
            ></span>
            <span class="app__term-title">{{ selected }}</span>
            <span
              class="app__term-state"
              :class="`app__term-state--${connStates[selected] ?? 'connecting'}`"
              role="status"
              aria-live="polite"
            >{{ connStates[selected] ?? 'connecting' }}</span>
            <span class="app__term-spacer"></span>
            <button
              class="app__term-btn"
              type="button"
              :title="withShortcut('decrease font size', 'zoom-out')"
              aria-label="Decrease terminal font size"
              :disabled="fontSize <= termConfig.min_font_size"
              @click="setFontSize(fontSize - 1)"
            >A−</button>
            <span class="app__term-fontsize" aria-live="polite" aria-label="Current font size">{{ fontSize }} px</span>
            <button
              class="app__term-btn"
              type="button"
              :title="withShortcut('increase font size', 'zoom-in')"
              aria-label="Increase terminal font size"
              :disabled="fontSize >= termConfig.max_font_size"
              @click="setFontSize(fontSize + 1)"
            >A+</button>
            <button
              class="app__term-btn"
              type="button"
              :title="withShortcut('toggle fullscreen', 'toggle-fullscreen')"
              aria-label="Toggle terminal fullscreen"
              @click="toggleFullscreen"
            ><span aria-hidden="true">⛶</span></button>
            <button
              v-if="filesAvailable"
              class="app__term-btn"
              type="button"
              :title="withShortcut('browse files in this session', 'toggle-files')"
              aria-label="Open the file manager"
              :aria-pressed="fileManagerOpen"
              @click="toggleFileManager"
            >Files</button>
            <button
              class="app__term-btn"
              type="button"
              :title="withShortcut('close panel (the session keeps running)', 'close-panel')"
              aria-label="Close terminal panel; session keeps running"
              @click="closePanel"
            ><span aria-hidden="true">✕</span></button>
          </header>
          <div v-if="!selected && desktop" class="app__welcome">
            <div class="app__welcome-card">
              <div class="app__welcome-mark" aria-hidden="true">
                <span></span><span></span><span></span>
              </div>
              <h2 class="app__welcome-title">
                {{ sessions.length === 0 ? 'No sessions yet' : 'No session open' }}
              </h2>
              <p class="app__welcome-hint">
                {{
                  sessions.length === 0
                    ? 'Create a tmux session to get started. Sessions keep running after the window closes.'
                    : 'Pick a session from the sidebar, or start a new one.'
                }}
              </p>
              <button class="app__welcome-btn" type="button" :disabled="creating" @click="onCreate">
                New Session
                <kbd v-if="shortcutFor('new-session')">{{ shortcutFor('new-session') }}</kbd>
              </button>
              <dl class="app__welcome-keys">
                <template v-for="row in [
                  ['next-session', 'Next session'],
                  ['previous-session', 'Previous session'],
                  ['toggle-sidebar', 'Toggle sidebar'],
                  ['help', 'tmux guide'],
                ] as const" :key="row[0]">
                  <template v-if="shortcutFor(row[0])">
                    <dt>{{ row[1] }}</dt>
                    <dd><kbd>{{ shortcutFor(row[0]) }}</kbd></dd>
                  </template>
                </template>
              </dl>
            </div>
          </div>
          <div v-else-if="!selected" class="app__placeholder">Select a session.</div>
          <KeepAlive>
            <TerminalView
              v-if="selected && selectedPanelId !== null"
              :key="selectedPanelId"
              :session="selected"
              :alive="selectedAlive"
              :font-size="fontSize"
              @contextmenu="onTerminalContextMenu"
              @state="onState"
              @notice="onNotice"
              @activity="onActivity"
            />
          </KeepAlive>
          <div v-if="fileManagerOpen" class="app__files">
            <FileManagerOverlay
              :session="selected"
              @close="closeFileManager"
              @notice="onNotice"
              @insert-path="insertPathIntoTerminal"
              @dirty-change="filesDirty = $event"
            />
          </div>
          <ToastStack />
        </main>
      </div>
    </template>

    <HelpModal :open="helpOpen" @close="helpOpen = false" />
    <ContextMenu
      v-if="terminalMenu"
      :x="terminalMenu.x"
      :y="terminalMenu.y"
      :items="terminalMenu.items"
      @select="onTerminalMenuSelect"
      @close="closeTerminalMenu"
    />
    <AppDialog />
  </div>
</template>

<style scoped>
.app {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.app__auth {
  position: relative;
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}
.app__bar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.75rem;
  background: var(--th-surface);
  border-bottom: 1px solid var(--th-border);
}
.app__title {
  font-weight: 600;
  font-size: 0.95rem;
}
.app__logout {
  margin-left: auto;
  background: var(--th-raised);
  color: var(--th-text-hi);
  border: 1px solid var(--th-border);
  border-radius: 4px;
  padding: 0.3rem 0.6rem;
  font-size: 0.8rem;
  cursor: pointer;
}
.app__logout:hover {
  border-color: var(--th-text-lo);
}
/* Terminal header: session identity on the left, controls on the right. */
.app__term-header {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.35rem 0.6rem;
  background: var(--th-surface);
  border-bottom: 1px solid var(--th-border);
}
.app__term-dot {
  width: 8px;
  height: 8px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: var(--th-text-lo);
}
/* The viewed session's activity is a breathing dot, deliberately different
   from the background sessions' card-border highlight. */
.app__term-dot--live {
  background: var(--th-green);
  animation: app-term-dot-breathe 1.2s ease-in-out infinite;
}
@keyframes app-term-dot-breathe {
  0%,
  100% {
    opacity: 0.35;
  }
  50% {
    opacity: 1;
  }
}

@media (prefers-reduced-motion: reduce) {
  .app__term-dot--live {
    animation: none;
    opacity: 1;
  }

  .app__sidebar {
    transition: none;
  }
}
.app__term-title {
  font-family: var(--app-code-font);
  font-size: 0.85rem;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 40%;
}
.app__term-state {
  font-size: 0.75rem;
  padding: 0.1rem 0.5rem;
  border-radius: 999px;
  border: 1px solid var(--th-border);
  color: var(--th-text-mid);
  white-space: nowrap;
}
.app__term-state--connected {
  color: var(--th-green, #4ade80);
  border-color: var(--th-green, #4ade80);
}
.app__term-state--reconnecting {
  color: var(--th-accent);
  border-color: var(--th-accent);
}
.app__term-state--ended {
  color: var(--th-danger);
  border-color: var(--th-danger);
}
.app__term-spacer {
  flex: 1;
}
.app__term-btn {
  background: var(--th-raised);
  color: var(--th-text-mid);
  border: 1px solid var(--th-border);
  border-radius: 4px;
  padding: 0.25rem 0.5rem;
  font-size: 0.8rem;
  cursor: pointer;
  white-space: nowrap;
}
.app__term-btn:hover {
  color: var(--th-text-hi);
  border-color: var(--th-text-lo);
}
.app__term-btn:disabled {
  opacity: 0.35;
  cursor: default;
  border-color: var(--th-border);
  color: var(--th-text-lo);
}
.app__term-fontsize {
  font-size: 0.72rem;
  color: var(--th-text-lo);
  min-width: 3.2em;
  text-align: center;
  white-space: nowrap;
  user-select: none;
}
.app__body {
  flex: 1;
  display: flex;
  min-height: 0;
}
.app__sidebar {
  width: 260px;
  flex: 0 0 260px;
  background: var(--th-surface);
  border-right: 1px solid var(--th-border);
  padding: 0.6rem;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  min-height: 0;
  transition: width 0.15s ease, flex-basis 0.15s ease;
}
.app__sidebar--collapsed {
  width: 44px;
  flex-basis: 44px;
  padding: 0.5rem 0.25rem;
  gap: 0.4rem;
}
.app__sidebar-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.app__help-btn {
  background: var(--th-raised);
  color: var(--th-text-mid);
  border: 1px solid var(--th-border);
  border-radius: 4px;
  padding: 0.35rem 0.6rem;
  font-size: 0.8rem;
  cursor: pointer;
  white-space: nowrap;
}
.app__help-btn:hover {
  color: var(--th-text-hi);
  border-color: var(--th-accent);
}
.app__sidebar-toggle {
  align-self: flex-end;
  background: none;
  border: 1px solid var(--th-border);
  border-radius: 4px;
  color: var(--th-text-mid);
  font-size: 0.75rem;
  padding: 0.1rem 0.35rem;
  cursor: pointer;
}
.app__sidebar-toggle:hover {
  color: var(--th-text-hi);
  border-color: var(--th-text-lo);
}
.app__rail {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.app__rail-btn {
  background: var(--th-raised);
  color: var(--th-text-mid);
  border: 1px solid var(--th-border);
  border-radius: 4px;
  font-size: 0.9rem;
  padding: 0.35rem 0;
  cursor: pointer;
}
.app__rail-btn:hover {
  color: var(--th-text-hi);
  border-color: var(--th-text-lo);
}
.app__main {
  flex: 1;
  min-width: 0;
  position: relative;
  display: flex;
  flex-direction: column;
}
/* In fullscreen the main region covers the screen, so it needs its own
   background instead of showing through to the page. */
.app__main:fullscreen {
  background: var(--app-bg);
}
/* The file manager covers the terminal area only. `.app__main` is already a
   positioning context, so this cannot escape over the sidebar. */
.app__files {
  position: absolute;
  inset: 0;
  z-index: 20;
}
.app__placeholder {
  padding: 1rem;
  color: var(--th-text-lo);
  font-size: 0.9rem;
}

/* --- Desktop shell --------------------------------------------------------
   The window's title bar names the application, so the page drops its own
   bar and the sidebar runs to the top of the window. */
.app--desktop .app__sidebar {
  background: var(--th-sidebar, var(--th-surface));
  border-right: none;
  transition: none;
}
.app--desktop .app__sidebar--collapsed {
  border-right: 1px solid var(--th-border);
}
.app--desktop .app__term-header {
  min-height: 34px;
  box-sizing: border-box;
  padding: 0.3rem 0.6rem 0.3rem 0.85rem;
}
.app--desktop .app__term-btn {
  border-radius: 5px;
}
.app--resizing,
.app--resizing * {
  cursor: col-resize !important;
  user-select: none !important;
}
/* A wide grab area over a hairline, so the edge is easy to catch without
   looking heavy. */
.app__splitter {
  position: relative;
  flex: 0 0 1px;
  width: 1px;
  background: var(--th-border);
  cursor: col-resize;
  z-index: 5;
}
.app__splitter::before {
  content: '';
  position: absolute;
  inset: 0 -3px;
}
.app__splitter:hover,
.app--resizing .app__splitter,
.app__splitter:focus-visible {
  background: var(--th-accent);
  outline: none;
}
.app__welcome {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 2rem;
  background:
    radial-gradient(ellipse at 50% 35%, color-mix(in srgb, var(--th-accent) 7%, transparent), transparent 60%),
    var(--app-bg);
}
.app__welcome-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  max-width: 360px;
  text-align: center;
}
.app__welcome-mark {
  display: grid;
  grid-template-columns: 1fr 1fr;
  grid-template-rows: 1fr 1fr;
  gap: 4px;
  width: 52px;
  height: 52px;
  padding: 7px;
  box-sizing: border-box;
  margin-bottom: 1.1rem;
  border: 1px solid var(--th-border-strong, var(--th-border));
  border-radius: 13px;
  background: var(--th-surface);
  box-shadow: 0 10px 30px rgb(0 0 0 / 35%);
}
.app__welcome-mark span {
  border-radius: 3px;
  background: var(--th-raised);
}
.app__welcome-mark span:first-child {
  grid-row: span 2;
  background: color-mix(in srgb, var(--th-accent) 70%, var(--th-raised));
}
.app__welcome-mark span:last-child {
  background: color-mix(in srgb, var(--th-green) 55%, var(--th-raised));
}
.app__welcome-title {
  margin: 0 0 0.4rem;
  font-size: 1.05rem;
  font-weight: 600;
  color: var(--th-text-hi);
}
.app__welcome-hint {
  margin: 0 0 1.2rem;
  font-size: 0.85rem;
  line-height: 1.5;
  color: var(--th-text-mid);
}
.app__welcome-btn {
  display: inline-flex;
  align-items: center;
  gap: 0.6rem;
  padding: 0.5rem 1rem;
  background: var(--th-accent);
  color: #0a0a0a;
  border: none;
  border-radius: 7px;
  font-size: 0.85rem;
  font-weight: 600;
  cursor: pointer;
}
.app__welcome-btn:hover {
  filter: brightness(1.08);
}
.app__welcome-btn:disabled {
  opacity: 0.6;
  cursor: default;
}
.app__welcome-btn kbd {
  font-family: var(--app-ui-font);
  font-size: 0.72rem;
  font-weight: 500;
  padding: 0.05rem 0.35rem;
  border-radius: 4px;
  background: rgb(10 10 10 / 15%);
}
.app__welcome-keys {
  display: grid;
  grid-template-columns: auto auto;
  gap: 0.45rem 1.5rem;
  margin: 1.6rem 0 0;
  font-size: 0.78rem;
  color: var(--th-text-lo);
}
.app__welcome-keys dt {
  text-align: right;
}
.app__welcome-keys dd {
  margin: 0;
  text-align: left;
}
.app__welcome-keys kbd {
  font-family: var(--app-ui-font);
  font-size: 0.72rem;
  padding: 0.1rem 0.4rem;
  color: var(--th-text-mid);
  background: var(--th-surface);
  border: 1px solid var(--th-border-strong, var(--th-border));
  border-bottom-width: 2px;
  border-radius: 4px;
}
</style>

<style>
.auth-card {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  width: 280px;
  padding: 1.5rem;
  background: var(--th-surface);
  border: 1px solid var(--th-border);
  border-radius: 8px;
}
.auth-card__title {
  margin: 0;
  font-size: 1.2rem;
}
.auth-card__hint {
  margin: 0;
  color: var(--th-text-lo);
  font-size: 0.85rem;
}
.auth-card__input {
  background: var(--app-bg);
  color: var(--th-text-hi);
  border: 1px solid var(--th-border);
  border-radius: 4px;
  padding: 0.4rem 0.6rem;
  font-family: var(--app-code-font);
  font-size: 0.85rem;
}
.auth-card__btn {
  background: var(--th-raised);
  color: var(--th-text-hi);
  border: 1px solid var(--th-border);
  border-radius: 4px;
  padding: 0.4rem 0.6rem;
  font-size: 0.9rem;
  cursor: pointer;
}
.auth-card__btn:hover {
  border-color: var(--th-text-lo);
}
.auth-card__btn:disabled {
  opacity: 0.5;
  cursor: default;
}
.auth-card__error {
  margin: 0;
  color: var(--th-danger);
  font-size: 0.8rem;
}
</style>
