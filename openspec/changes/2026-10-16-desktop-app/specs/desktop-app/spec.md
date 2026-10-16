## ADDED Requirements

### Requirement: Embedded hub

The desktop application SHALL run the hub in its own process and show the hub's page in a native window, so that every feature of the browser client is available in the window without a separate server. The embedded hub SHALL listen on a loopback address only, SHALL generate a new bearer token for every launch, and SHALL ignore a token or listen address supplied for the command-line server. It SHALL read the same configuration file format as the server from its per-user configuration directory or from `--config`.

#### Scenario: Launch without sign-in

- **WHEN** the user opens the desktop application
- **THEN** the window shows the session manager without asking for a token

#### Scenario: A server token does not open the desktop hub

- **WHEN** `VISUAL_TMUX_CLIENT_TOKEN` is set in the environment the application starts with
- **THEN** the embedded hub rejects that token

#### Scenario: Preferred port taken

- **WHEN** the preferred loopback port is in use
- **THEN** the hub listens on another free loopback port and the window opens normally

#### Scenario: The hub cannot start

- **WHEN** the configuration is invalid
- **THEN** the window shows the error as text and where the configuration lives, and does not navigate anywhere

### Requirement: Window bridge

The page SHALL reach the window only through authenticated routes under `/api/desktop/`, which SHALL require the same bearer token as the rest of the API. A save SHALL fetch the file through the hub's download route so the file manager's boundary applies, and SHALL leave an existing file at the chosen path untouched unless the download completes. Links passed to the system browser SHALL be limited to `http`, `https` and `mailto`.

#### Scenario: Unauthenticated bridge request

- **WHEN** a request to `/api/desktop/*` carries no valid token
- **THEN** it is refused with 401 and the window is not affected

#### Scenario: Refused download

- **WHEN** the user saves a file the hub refuses to serve
- **THEN** the page reports the hub's reason and the chosen target is unchanged

#### Scenario: Non-web link

- **WHEN** the page asks to open a `file:` or other non-web URL
- **THEN** the request is refused

### Requirement: Desktop interaction

In the desktop application the client SHALL provide a native menu bar whose shortcuts on macOS all use Command and on Linux never use Control with a letter alone, so no shortcut takes a keystroke from the terminal. Confirmations and prompts SHALL be in-app dialogs that the keystroke opening them cannot answer. Session rows and the terminal SHALL answer a right click with a menu, except that a right click on a terminal whose program tracks the mouse SHALL go to that program and Shift+right-click SHALL open the menu. Files dropped on the window SHALL be inserted into the viewed terminal as shell-quoted paths followed by a space, never a line terminator. The browser client's behaviour SHALL be unchanged.

#### Scenario: Shortcut in the terminal

- **WHEN** the user presses Ctrl+C, Ctrl+W or Ctrl+T in a terminal on Linux
- **THEN** the keystroke reaches the program in the terminal

#### Scenario: Kill from a menu with the keyboard

- **WHEN** the user chooses Kill Session from a menu with Enter
- **THEN** the confirmation dialog stays open until the user answers it

#### Scenario: Right click with tmux mouse mode

- **WHEN** tmux has mouse mode on and the user right-clicks the terminal
- **THEN** tmux receives the click and the application shows no menu

### Requirement: Window lifecycle

The application SHALL remember the window's size and maximised state between launches, SHALL run as a single instance that brings its window forward when launched again, and SHALL ask before quitting while the file manager holds unsaved changes. Quitting SHALL stop the embedded hub and SHALL NOT terminate any tmux session.

#### Scenario: Quit with unsaved edits

- **WHEN** the user quits while a file has unsaved changes
- **THEN** the application asks, and stays open if the user declines

#### Scenario: Sessions survive quitting

- **WHEN** the user quits and opens the application again
- **THEN** the sessions that were running are still listed

### Requirement: Launch environment

When the application starts without a terminal's environment, it SHALL add existing package-manager directories to `PATH` without reordering the entries already present, and SHALL set a UTF-8 `LANG` only when none of `LC_ALL`, `LC_CTYPE` and `LANG` is set.

#### Scenario: Started from the Dock

- **WHEN** tmux is installed by Homebrew and the application is opened from the Dock
- **THEN** sessions can be created and non-ASCII text is displayed correctly
