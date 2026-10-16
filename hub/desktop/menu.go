package main

import (
	"fmt"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/AK12-Official/visual-tmux-client/hub/desktop/internal/shortcuts"
)

const projectURL = "https://github.com/AK12-Official/visual-tmux-client"

// accelerator converts a combo to the toolkit's form. Command and Control are
// distinct on macOS; on Linux only Control exists, and the table never uses
// Command there.
func accelerator(c shortcuts.Combo) *keys.Accelerator {
	a := &keys.Accelerator{Key: c.Key}
	if c.Mods&shortcuts.Cmd != 0 {
		a.Modifiers = append(a.Modifiers, keys.CmdOrCtrlKey)
	}
	if c.Mods&shortcuts.Ctrl != 0 {
		a.Modifiers = append(a.Modifiers, keys.ControlKey)
	}
	if c.Mods&shortcuts.Alt != 0 {
		a.Modifiers = append(a.Modifiers, keys.OptionOrAltKey)
	}
	if c.Mods&shortcuts.Shift != 0 {
		a.Modifiers = append(a.Modifiers, keys.ShiftKey)
	}
	return a
}

// key is the accelerator for a command, or nil when it has none.
func (d *desktop) key(command shortcuts.Command) *keys.Accelerator {
	combo, ok := d.table[command]
	if !ok {
		return nil
	}
	return accelerator(combo)
}

// send is a menu callback that forwards a page command.
func (d *desktop) send(command shortcuts.Command) menu.Callback {
	return func(*menu.CallbackData) { d.dispatch(string(command), nil) }
}

// buildMenu lays the menu bar out the way each platform's applications do:
// on macOS the application menu and the standard Edit and Window menus come
// from the system, and on Linux File and Edit carry Quit and the clipboard.
func (d *desktop) buildMenu() *menu.Menu {
	mac := goruntime.GOOS == shortcuts.Darwin
	bar := menu.NewMenu()
	if mac {
		bar.Append(menu.AppMenu())
	}
	d.fileMenu(bar.AddSubmenu("File"), mac)
	d.editMenu(bar, mac)
	d.viewMenu(bar.AddSubmenu("View"))
	d.sessionMenu(bar.AddSubmenu("Session"))
	if mac {
		bar.Append(menu.WindowMenu())
	}
	d.helpMenu(bar.AddSubmenu("Help"), mac)
	return bar
}

func (d *desktop) fileMenu(m *menu.Menu, mac bool) {
	m.AddText("New Session", d.key(shortcuts.NewSession), d.send(shortcuts.NewSession))
	m.AddSeparator()
	m.AddText("Close Panel", d.key(shortcuts.ClosePanel), d.send(shortcuts.ClosePanel))
	if mac {
		return
	}
	m.AddSeparator()
	m.AddText("Open Configuration Folder", nil, func(*menu.CallbackData) { d.openConfigDir() })
	m.AddSeparator()
	m.AddText("Quit", d.key(shortcuts.Quit), func(*menu.CallbackData) {
		if ctx, ok := d.shell.context(); ok {
			wruntime.Quit(ctx)
		}
	})
}

// editMenu is the system's on macOS: its Copy and Paste reach the focused
// element, which xterm answers through its own clipboard events, and the
// system menu is also what makes Cmd+C work in every text field. Linux gets
// the same items as page commands, under Ctrl+Shift so that Ctrl+C stays an
// interrupt.
func (d *desktop) editMenu(bar *menu.Menu, mac bool) {
	if mac {
		bar.Append(menu.EditMenu())
		return
	}
	m := bar.AddSubmenu("Edit")
	m.AddText("Copy", d.key(shortcuts.Copy), d.send(shortcuts.Copy))
	m.AddText("Paste", d.key(shortcuts.Paste), d.send(shortcuts.Paste))
	m.AddText("Select All", d.key(shortcuts.SelectAll), d.send(shortcuts.SelectAll))
}

func (d *desktop) viewMenu(m *menu.Menu) {
	m.AddText("Toggle Sidebar", d.key(shortcuts.ToggleSidebar), d.send(shortcuts.ToggleSidebar))
	m.AddText("Toggle Files", d.key(shortcuts.ToggleFiles), d.send(shortcuts.ToggleFiles))
	m.AddSeparator()
	m.AddText("Increase Font Size", d.key(shortcuts.ZoomIn), d.send(shortcuts.ZoomIn))
	m.AddText("Decrease Font Size", d.key(shortcuts.ZoomOut), d.send(shortcuts.ZoomOut))
	m.AddText("Actual Size", d.key(shortcuts.ZoomReset), d.send(shortcuts.ZoomReset))
	m.AddSeparator()
	m.AddText("Toggle Full Screen", d.key(shortcuts.ToggleFullscreen), d.send(shortcuts.ToggleFullscreen))
	m.AddSeparator()
	m.AddText("Reload", d.key(shortcuts.Reload), func(*menu.CallbackData) { d.reload() })
}

func (d *desktop) sessionMenu(m *menu.Menu) {
	m.AddText("Rename Session…", d.key(shortcuts.RenameSession), d.send(shortcuts.RenameSession))
	m.AddText("Kill Session…", nil, func(*menu.CallbackData) { d.dispatch("kill-session", nil) })
	m.AddSeparator()
	m.AddText("Next Session", d.key(shortcuts.NextSession), d.send(shortcuts.NextSession))
	m.AddText("Previous Session", d.key(shortcuts.PreviousSession), d.send(shortcuts.PreviousSession))
	goTo := m.AddSubmenu("Go to Session")
	for n := 1; n <= shortcuts.GoToSessionSlots; n++ {
		position := n
		goTo.AddText(fmt.Sprintf("Session %d", n), accelerator(shortcuts.GoToSession(goruntime.GOOS, n)),
			func(*menu.CallbackData) { d.dispatch("go-to-session", position) })
	}
	m.AddSeparator()
	m.AddText("Clear Scrollback", d.key(shortcuts.ClearScrollback), d.send(shortcuts.ClearScrollback))
}

func (d *desktop) helpMenu(m *menu.Menu, mac bool) {
	m.AddText("tmux Guide", d.key(shortcuts.Help), d.send(shortcuts.Help))
	m.AddText("Project Website", nil, func(*menu.CallbackData) {
		if ctx, ok := d.shell.context(); ok {
			wruntime.BrowserOpenURL(ctx, projectURL)
		}
	})
	if mac {
		m.AddText("Open Configuration Folder", nil, func(*menu.CallbackData) { d.openConfigDir() })
		return
	}
	m.AddSeparator()
	m.AddText("About Visual Tmux Client", nil, func(*menu.CallbackData) {
		ctx, ok := d.shell.context()
		if !ok {
			return
		}
		_, _ = wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{ //nolint:errcheck // informational
			Type:    wruntime.InfoDialog,
			Title:   "About " + appTitle,
			Message: aboutText(),
			Icon:    appIcon,
		})
	})
}
