package shortcuts

import (
	"fmt"
	"testing"
	"unicode"
)

var platforms = []string{Darwin, "linux"}

func TestNoTwoCommandsShareAShortcut(t *testing.T) {
	for _, goos := range platforms {
		seen := map[Combo]string{}
		table := For(goos)
		for _, command := range table.Commands() {
			combo := table[command]
			if other, ok := seen[combo]; ok {
				t.Errorf("%s: %s and %s share %s", goos, other, command, combo.Label(goos))
			}
			seen[combo] = string(command)
		}
		for n := 1; n <= GoToSessionSlots; n++ {
			combo := GoToSession(goos, n)
			if other, ok := seen[combo]; ok {
				t.Errorf("%s: go-to-session %d shares %s with %s", goos, n, combo.Label(goos), other)
			}
			seen[combo] = fmt.Sprintf("go-to-session %d", n)
		}
	}
}

// Control plus a letter is the shell's: readline and every editor in a
// terminal use them. Taking one for the menu would make it unreachable in
// every session, so on Linux a letter always comes with Shift as well.
func TestLinuxLeavesControlLettersToTheTerminal(t *testing.T) {
	table := For("linux")
	for _, command := range table.Commands() {
		combo := table[command]
		if combo.Mods&Cmd != 0 {
			t.Errorf("%s uses Command, which Linux keyboards do not have", command)
		}
		r := []rune(combo.Key)
		if len(r) == 1 && unicode.IsLetter(r[0]) && combo.Mods&Ctrl != 0 && combo.Mods&Shift == 0 {
			t.Errorf("%s takes %s from the terminal", command, combo.Label("linux"))
		}
	}
}

// On macOS a shortcut either carries Command or is a function key, so none of
// them can be a keystroke a program in the terminal expects to receive.
func TestMacShortcutsCarryCommand(t *testing.T) {
	table := For(Darwin)
	for _, command := range table.Commands() {
		combo := table[command]
		if combo.Mods&Cmd == 0 && combo.Key[0] != 'f' {
			t.Errorf("%s (%s) does not carry Command", command, combo.Label(Darwin))
		}
	}
}

func TestLabels(t *testing.T) {
	cases := []struct {
		goos  string
		combo Combo
		want  string
	}{
		{Darwin, Combo{Key: "t", Mods: Cmd}, "⌘T"},
		{Darwin, Combo{Key: "right", Mods: Cmd | Alt}, "⌥⌘→"},
		{Darwin, Combo{Key: "f", Mods: Cmd | Ctrl}, "⌃⌘F"},
		{Darwin, Combo{Key: "e", Mods: Cmd | Shift}, "⇧⌘E"},
		{Darwin, Combo{Key: "f2"}, "F2"},
		{"linux", Combo{Key: "t", Mods: Ctrl | Shift}, "Ctrl+Shift+T"},
		{"linux", Combo{Key: "page down", Mods: Ctrl}, "Ctrl+PgDn"},
		{"linux", Combo{Key: "down", Mods: Ctrl | Shift}, "Ctrl+Shift+Down"},
		{"linux", Combo{Key: "1", Mods: Alt}, "Alt+1"},
		{"linux", Combo{Key: "f11"}, "F11"},
		{"linux", Combo{Key: "=", Mods: Ctrl}, "Ctrl+="},
	}
	for _, c := range cases {
		if got := c.combo.Label(c.goos); got != c.want {
			t.Errorf("%s %+v: got %q, want %q", c.goos, c.combo, got, c.want)
		}
	}
}

func TestPageLabelsLeaveOutTheShellsOwnCommands(t *testing.T) {
	labels := For("linux").Labels("linux")
	if _, ok := labels[string(Quit)]; ok {
		t.Error("quit is the shell's, not the page's")
	}
	if _, ok := labels[string(Reload)]; ok {
		t.Error("reload is the shell's, not the page's")
	}
	if labels[string(NewSession)] != "Ctrl+Shift+T" {
		t.Errorf("new-session: got %q", labels[string(NewSession)])
	}
}
