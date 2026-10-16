## Context

See `proposal.md` — Why. Constraints that shape this approach:

- The hub is a Go server with the Vue client embedded (`hub/web`), and every feature of the client is a request to that server: REST for sessions and files, a WebSocket per attached terminal.
- WKWebView implements `confirm()`, `prompt()` and `<a download>` only through delegate methods its host provides, and Wails v2 provides none of them. WebKitGTK shows its own dialogs but also ignores downloads.
- tmux exists only on macOS and Linux, so those are the platforms the hub supports, and the only ones a desktop build is useful on.
- `make test` and the server's CI run `go test ./...` and `golangci-lint ./...` in `hub/` on machines with no C toolchain or web view headers.

## Goals / Non-Goals

**Goals:**
- Every browser-client feature in the desktop application, from the same code.
- Desktop conventions: menu bar, platform shortcuts that never take a key from the terminal, native save, clipboard, right-click menus, window state, single instance, quit guard.
- No change in behaviour for the browser client, and no new requirement on the server's build.

**Non-Goals:**
- Windows support: the hub needs a local tmux.
- A system tray, multiple windows, or remote-host aggregation.
- Code signing and notarization of the macOS bundle.

## Decisions

### 1. The window loads the hub's page; it does not host it

The desktop process starts the hub on loopback and the window navigates to `http://127.0.0.1:<port>/`. The page and its API and WebSockets are then same-origin, exactly as in a browser.

*Alternatives considered:* serving the page from Wails' `wails://` asset server and proxying the API through its handler. That keeps the Wails JavaScript runtime available, but a custom scheme cannot carry WebSockets, so the terminal would connect cross-origin to the loopback port — a mixed-content question each platform's web view answers differently, an origin the hub's same-host policy would have to be loosened for, and a second transport for the one feature that matters most.

### 2. A bridge over the hub's own listener

Because the page is not on Wails' origin, Wails' bindings are not on it. What the page needs from the window is small and request-shaped, so it is a set of authenticated routes, `/api/desktop/*`, mounted in front of the hub's router through a new `app.Options.Wrap` and guarded by the hub's own `RequireAuth`. The shell → page direction is `WindowExecJS` calling `window.__vtcDesktop.dispatch(command, arg)`, which works on any origin. Commands only trigger what the page's own controls already do.

A save goes through the hub's download route rather than reading the file in the bridge: that route enforces the file manager's boundary (roots, size, regular files only), and a second reader would be a second boundary to keep in step. The response is streamed into a temporary file beside the target and renamed over it only on success.

### 3. Credentials and port

Each launch generates a token and passes it in the URL fragment, which is never sent to a server and is removed with `history.replaceState` as soon as it is read; environment and configuration tokens are ignored so a token exported for the server cannot open the desktop hub. The hub listens on `127.0.0.1:47690` when it is free and on any free loopback port otherwise. The fixed port matters because the page keeps its preferences in `localStorage`, which is per origin; a port that changed every launch would start from an empty store every time.

### 4. Dialogs become promises

The file manager's `confirm`/`prompt` call sites now `await askConfirm`/`askText`. In a browser those resolve from `window.confirm`/`window.prompt` exactly as before; in the shell they open `AppDialog`. A confirmation focuses the dialog panel rather than its primary button, because WebKit activates a focused button on the keypress of the Enter that opened the dialog from a menu, which would answer a Kill or Delete question with the key that asked it.

### 5. Shortcuts that leave the terminal alone

One table (`hub/desktop/internal/shortcuts`) defines every accelerator and is sent to the page as labels. macOS shortcuts all carry ⌘, which no terminal program receives. On Linux, Ctrl plus a letter is the shell's, so the shell's shortcuts add Shift, as GNOME Terminal does; a test enforces both rules. Next/previous session use Ctrl+Shift+Down/Up on Linux because Wails maps the `page up`/`page down` names to the keypad keys.

On macOS the Edit menu is the system's, whose Copy and Paste reach xterm through its own clipboard events; on Linux they are page commands that go through the shell's clipboard.

### 6. A separate Go module

`hub/desktop` has its own `go.mod` with a `replace` to `../`, which keeps Wails and cgo out of the server module entirely while still letting it import `hub/internal/...` (the internal rule is by import path).

### 7. Launch environment

A GUI launch on macOS has `PATH=/usr/bin:/bin:/usr/sbin:/sbin` and no locale. The hub resolves tmux on `PATH` when it is constructed, and tmux replaces every non-ASCII character with `_` when its client's locale is not UTF-8, so the shell appends existing package-manager directories to `PATH` and sets `LANG` to a UTF-8 locale only when none of `LC_ALL`, `LC_CTYPE`, `LANG` is set.

## Risks / Trade-offs

- The macOS paths (WKWebView key equivalents, the system Edit menu reaching xterm, App Transport Security for loopback) are covered by CI builds but were not exercised interactively in this change; the Linux build was driven end to end under Xvfb.
- `prefer-dark-theme` only takes effect with a GTK theme that ships a dark variant; the user's theme is never replaced.
- Files dropped on the window are typed into the terminal only; uploading them through the file manager is out of scope.
