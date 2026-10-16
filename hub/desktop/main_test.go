package main

import (
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu/keys"

	"github.com/AK12-Official/visual-tmux-client/hub/desktop/internal/shortcuts"
)

func TestAcceleratorKeepsCommandAndControlApart(t *testing.T) {
	const cmd, ctrl, alt, shift = shortcuts.Cmd, shortcuts.Ctrl, shortcuts.Alt, shortcuts.Shift
	cases := []struct {
		combo shortcuts.Combo
		want  []keys.Modifier
	}{
		{shortcuts.Combo{Key: "t", Mods: cmd}, []keys.Modifier{keys.CmdOrCtrlKey}},
		{shortcuts.Combo{Key: "f", Mods: cmd | ctrl}, []keys.Modifier{keys.CmdOrCtrlKey, keys.ControlKey}},
		{shortcuts.Combo{Key: "t", Mods: ctrl | shift}, []keys.Modifier{keys.ControlKey, keys.ShiftKey}},
		{shortcuts.Combo{Key: "1", Mods: alt}, []keys.Modifier{keys.OptionOrAltKey}},
		{shortcuts.Combo{Key: "f2"}, nil},
	}
	for _, c := range cases {
		got := accelerator(c.combo)
		if got.Key != c.combo.Key || !reflect.DeepEqual(got.Modifiers, c.want) {
			t.Errorf("%+v: got %+v", c.combo, got)
		}
	}
}

func TestEveryShortcutIsAKeyTheMenuAccepts(t *testing.T) {
	for _, goos := range []string{shortcuts.Darwin, "linux"} {
		table := shortcuts.For(goos)
		for _, command := range table.Commands() {
			a := accelerator(table[command])
			parts := make([]string, 0, len(a.Modifiers)+1)
			for _, m := range a.Modifiers {
				parts = append(parts, string(m))
			}
			if _, err := keys.Parse(strings.Join(append(parts, a.Key), "+")); err != nil {
				t.Errorf("%s %s: %v", goos, command, err)
			}
		}
	}
}

// The loader is what a window shows when the hub cannot start, and the error
// can quote the user's configuration: it is text on the page, never markup.
func TestTheLoaderShowsAStartErrorAsText(t *testing.T) {
	d := &desktop{
		shell:     &wailsShell{},
		startErr:  errors.New(`bad <script>alert(1)</script> value`),
		configDir: "/home/u/.config/x",
	}
	rec := httptest.NewRecorder()
	d.serveLoader(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if strings.Contains(body, "<script>alert") {
		t.Fatalf("the error was rendered as markup:\n%s", body)
	}
	escaped := "&lt;script&gt;alert(1)&lt;/script&gt;"
	if !strings.Contains(body, escaped) || !strings.Contains(body, "/home/u/.config/x") {
		t.Errorf("the page does not say what went wrong:\n%s", body)
	}
	if strings.Contains(body, "location.replace") {
		t.Error("a failed start must not forward the window anywhere")
	}
}
