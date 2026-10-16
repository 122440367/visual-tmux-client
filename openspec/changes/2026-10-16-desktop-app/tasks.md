## 1. Hub hook

- [x] 1.1 Add `app.Options.Wrap` so an embedding host can mount routes in front of the router. Verify: `TestWrapMountsRoutesBesideTheRouter` asserts a wrapped route answers and an unclaimed path still reaches the hub.

## 2. Web client

- [x] 2.1 `desktop.ts`: adopt the launch token from the fragment and clear it, identify the shell through `/api/desktop/info`, expose the bridge calls and the command receiver. Verify: `desktop.test.ts` covers adoption, a browser page left alone, no request from an unmarked page, shortcut filtering, fallback when the shell does not answer, command filtering, and the save call.
- [x] 2.2 `dialogs.ts` and `AppDialog.vue`: promise-based confirm/prompt, browser dialogs in a browser, one queued in-app dialog at a time in the shell, stem selection on rename, focus restored, no activation by the opening keystroke. Verify: `dialogs.test.ts` and `AppDialog.dom.test.ts`.
- [x] 2.3 File manager: await the dialogs, save downloads and copy paths through the shell. Verify: the existing `FileManagerOverlay.dom.test.ts` passes unchanged.
- [x] 2.4 Terminal helpers for selection, bracketed paste, select all, clear scrollback and mouse tracking. Verify: `terminal.test.ts`.
- [x] 2.5 Desktop layout and commands in `App.vue` and `SessionList.vue`: no sign-in or top bar, window title, menu commands, right-click menus, resizable sidebar, welcome screen, external links, file drops. Verify: manual run under Xvfb (section 5).
- [x] 2.6 Dark editor theme drawn from the page tokens.

## 3. Desktop module

- [x] 3.1 `internal/embedded`: load configuration, force a loopback address and a per-launch token, prefer the fixed port with fallback. Verify: `embedded_test.go` against doubles (no tmux).
- [x] 3.2 `internal/bridge`: the routes, behind the hub's `RequireAuth`, with saves streamed through the hub's download route into a temporary file. Verify: `bridge_test.go`.
- [x] 3.3 `internal/shortcuts`: one table per platform, labels for the page. Verify: tests for collisions, terminal keys left alone, ⌘ on macOS, and labels.
- [x] 3.4 `internal/winstate` and `internal/environ`. Verify: their tests.
- [x] 3.5 Window, menu, loader, quit guard, single instance, file drop, GTK dark preference. Verify: `main_test.go` (accelerators parse, loader escapes a start error) and section 5.

## 4. Build and documentation

- [x] 4.1 Makefile targets, `.gitignore`, `wails.json`, macOS `Info.plist`, Linux `.desktop` entry, icon.
- [x] 4.2 CI desktop job on Ubuntu and macOS; release desktop packages.
- [x] 4.3 README (English and Chinese), CONTRIBUTING, AGENTS.

## 5. Verification

- [x] 5.1 `make test`, `make lint`, `make desktop-test`, `make desktop-lint`. Verify: all pass with 0 lint issues.
- [x] 5.2 Linux build driven under Xvfb with a private `TMUX_TMPDIR` and private XDG directories: launch, create sessions by button and accelerator, CJK output, title sync, session and terminal right-click menus (tmux keeps a plain right click), kill dialog via menu and keyboard, next/previous session, file manager create prompt, clipboard copy/paste, quit guard, F1 guide, zoom, native save dialog writing the file, sidebar resize, sessions surviving a restart, window size saved on quit.
- [ ] 5.3 Interactive check on macOS (menu key equivalents, Edit menu copy/paste in xterm, loopback load under App Transport Security).
