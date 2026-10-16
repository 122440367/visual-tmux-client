// Package shortcuts is the one definition of the desktop shell's keyboard
// shortcuts. The menu bar is built from it, and the page is sent the same table
// as labels, so a tooltip can never name a key the menu does not answer.
//
// The two platforms deliberately differ. On macOS every shortcut carries
// Command, which no terminal program ever receives. On Linux the Control key
// belongs to the shell -- Ctrl+T, Ctrl+W and Ctrl+N all edit the command line --
// so the shell's shortcuts add Shift, as GNOME Terminal and Konsole do.
package shortcuts

import (
	"sort"
	"strings"
)

// Mod is a set of modifier keys.
type Mod uint8

// The modifiers. Cmd is the macOS Command key and is never used on Linux.
const (
	Cmd Mod = 1 << iota
	Ctrl
	Alt
	Shift
)

// Combo is one key with its modifiers. Key uses the menu toolkit's names:
// a single printable character, or a named key such as "f2" or "page down".
type Combo struct {
	Key  string
	Mods Mod
}

// Command names a menu command. The values are the commands the page accepts
// (desktop.ts), so they travel unchanged.
type Command string

// The commands that have shortcuts.
const (
	NewSession       Command = "new-session"
	ClosePanel       Command = "close-panel"
	RenameSession    Command = "rename-session"
	NextSession      Command = "next-session"
	PreviousSession  Command = "previous-session"
	ToggleSidebar    Command = "toggle-sidebar"
	ToggleFiles      Command = "toggle-files"
	ZoomIn           Command = "zoom-in"
	ZoomOut          Command = "zoom-out"
	ZoomReset        Command = "zoom-reset"
	ToggleFullscreen Command = "toggle-fullscreen"
	Help             Command = "help"
	Copy             Command = "copy"
	Paste            Command = "paste"
	SelectAll        Command = "select-all"
	ClearScrollback  Command = "clear-scrollback"
	// Reload and Quit are the shell's own; the page never receives them.
	Reload Command = "reload"
	Quit   Command = "quit"
)

// Darwin is the platform name runtime.GOOS reports for macOS.
const Darwin = "darwin"

// Named keys used more than once.
const (
	keyRight = "right"
	keyLeft  = "left"
	keyUp    = "up"
	keyDown  = "down"
)

// GoToSessionSlots is how many sessions have a direct shortcut (1 to 9).
const GoToSessionSlots = 9

// Table maps each command to its shortcut on one platform.
type Table map[Command]Combo

// For returns the shortcuts of a platform.
func For(goos string) Table {
	if goos == Darwin {
		return Table{
			NewSession:       {Key: "t", Mods: Cmd},
			ClosePanel:       {Key: "w", Mods: Cmd},
			RenameSession:    {Key: "f2"},
			NextSession:      {Key: keyRight, Mods: Cmd | Alt},
			PreviousSession:  {Key: keyLeft, Mods: Cmd | Alt},
			ToggleSidebar:    {Key: "b", Mods: Cmd},
			ToggleFiles:      {Key: "e", Mods: Cmd | Shift},
			ZoomIn:           {Key: "=", Mods: Cmd},
			ZoomOut:          {Key: "-", Mods: Cmd},
			ZoomReset:        {Key: "0", Mods: Cmd},
			ToggleFullscreen: {Key: "f", Mods: Cmd | Ctrl},
			Help:             {Key: "?", Mods: Cmd},
			// Copy, Paste and Select All are the standard Edit menu's on macOS;
			// they are listed so the page can show them.
			Copy:            {Key: "c", Mods: Cmd},
			Paste:           {Key: "v", Mods: Cmd},
			SelectAll:       {Key: "a", Mods: Cmd},
			ClearScrollback: {Key: "k", Mods: Cmd},
			Reload:          {Key: "r", Mods: Cmd},
			Quit:            {Key: "q", Mods: Cmd},
		}
	}
	return Table{
		NewSession:    {Key: "t", Mods: Ctrl | Shift},
		ClosePanel:    {Key: "w", Mods: Ctrl | Shift},
		RenameSession: {Key: "f2"},
		// Up and down, as the sessions are listed in the sidebar. Page Up and
		// Page Down would follow GNOME Terminal's tabs, but the toolkit binds
		// those names to the keypad's keys, which most keyboards lack.
		NextSession:      {Key: keyDown, Mods: Ctrl | Shift},
		PreviousSession:  {Key: keyUp, Mods: Ctrl | Shift},
		ToggleSidebar:    {Key: "b", Mods: Ctrl | Shift},
		ToggleFiles:      {Key: "e", Mods: Ctrl | Shift},
		ZoomIn:           {Key: "=", Mods: Ctrl},
		ZoomOut:          {Key: "-", Mods: Ctrl},
		ZoomReset:        {Key: "0", Mods: Ctrl},
		ToggleFullscreen: {Key: "f11"},
		Help:             {Key: "f1"},
		Copy:             {Key: "c", Mods: Ctrl | Shift},
		Paste:            {Key: "v", Mods: Ctrl | Shift},
		SelectAll:        {Key: "a", Mods: Ctrl | Shift},
		ClearScrollback:  {Key: "k", Mods: Ctrl | Shift},
		Reload:           {Key: "r", Mods: Ctrl | Shift},
		Quit:             {Key: "q", Mods: Ctrl | Shift},
	}
}

// GoToSession is the shortcut for the n-th session (1-based).
func GoToSession(goos string, n int) Combo {
	key := string(rune('0' + n))
	if goos == Darwin {
		return Combo{Key: key, Mods: Cmd}
	}
	return Combo{Key: key, Mods: Alt}
}

var macKeyGlyphs = map[string]string{
	keyRight: "→", keyLeft: "←", keyUp: "↑", keyDown: "↓",
	"page up": "⇞", "page down": "⇟", "return": "↩", "enter": "↩",
	"escape": "⎋", "delete": "⌫", "tab": "⇥", "space": "Space",
}

var linuxKeyNames = map[string]string{
	keyRight: "Right", keyLeft: "Left", keyUp: "Up", keyDown: "Down",
	"page up": "PgUp", "page down": "PgDn", "return": "Enter", "enter": "Enter",
	"escape": "Esc", "delete": "Del", "tab": "Tab", "space": "Space",
}

func keyLabel(key string, names map[string]string) string {
	if name, ok := names[key]; ok {
		return name
	}
	// Letters and function keys are shown in capitals, as every menu shows them.
	return strings.ToUpper(key)
}

// Label renders a combo in the platform's own notation: ⌥⌘→ on macOS, in the
// order Apple's menus use, and Ctrl+Shift+T on Linux.
func (c Combo) Label(goos string) string {
	if goos == Darwin {
		var b strings.Builder
		for _, m := range []struct {
			mod   Mod
			glyph string
		}{{Ctrl, "⌃"}, {Alt, "⌥"}, {Shift, "⇧"}, {Cmd, "⌘"}} {
			if c.Mods&m.mod != 0 {
				b.WriteString(m.glyph)
			}
		}
		b.WriteString(keyLabel(c.Key, macKeyGlyphs))
		return b.String()
	}
	var parts []string
	for _, m := range []struct {
		mod  Mod
		name string
	}{{Ctrl, "Ctrl"}, {Alt, "Alt"}, {Shift, "Shift"}} {
		if c.Mods&m.mod != 0 {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, keyLabel(c.Key, linuxKeyNames)), "+")
}

// Labels renders a table for the page, leaving out the shell's own commands.
func (t Table) Labels(goos string) map[string]string {
	out := make(map[string]string, len(t))
	for command, combo := range t {
		if command == Reload || command == Quit {
			continue
		}
		out[string(command)] = combo.Label(goos)
	}
	return out
}

// Commands lists a table's commands in a stable order.
func (t Table) Commands() []Command {
	out := make([]Command, 0, len(t))
	for command := range t {
		out = append(out, command)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
