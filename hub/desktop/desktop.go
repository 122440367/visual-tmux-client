package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os/exec"
	goruntime "runtime"
	"sync/atomic"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/AK12-Official/visual-tmux-client/hub/desktop/internal/bridge"
	"github.com/AK12-Official/visual-tmux-client/hub/desktop/internal/embedded"
	"github.com/AK12-Official/visual-tmux-client/hub/desktop/internal/shortcuts"
	"github.com/AK12-Official/visual-tmux-client/hub/desktop/internal/winstate"
)

// singleInstanceID names the lock that keeps one window per user. A second
// launch brings the first window forward instead: two windows would be two
// hubs, and the second could not have the first one's port, so its page would
// open with none of the preferences the first one keeps.
const singleInstanceID = "io.github.visual-tmux-client.desktop"

// The window's background before the page paints, matching --app-bg, so the
// window does not flash white while it loads.
const (
	backgroundRed   = 10
	backgroundGreen = 10
	backgroundBlue  = 10
	opaque          = 255
)

// The quit question's answers: the labels macOS shows, and the Yes GTK
// shows in place of Quit.
const (
	quitButton   = "Quit"
	cancelButton = "Cancel"
	gtkYes       = "Yes"
)

// desktop is the application: the window, the hub it shows, and the bridge
// between them.
type desktop struct {
	// shell holds the window's context once the window exists; every
	// callback reads it from there, because menu items, drops and a second
	// launch arrive on goroutines of their own.
	shell     *wailsShell
	hub       *embedded.Hub
	startErr  error
	bridge    *bridge.Bridge
	table     shortcuts.Table
	configDir string
	statePath string
	state     winstate.State
	// confirmedQuit records that the user already answered the unsaved-changes
	// question for this quit, so the close that follows is not asked twice.
	confirmedQuit atomic.Bool
}

func (d *desktop) run(state winstate.State) error {
	d.state = state
	preferDarkTheme()
	startState := options.Normal
	if state.Maximised {
		startState = options.Maximised
	}
	err := wails.Run(&options.App{
		Title:            appTitle,
		Width:            state.Width,
		Height:           state.Height,
		MinWidth:         winstate.MinWidth,
		MinHeight:        winstate.MinHeight,
		WindowStartState: startState,
		BackgroundColour: &options.RGBA{R: backgroundRed, G: backgroundGreen, B: backgroundBlue, A: opaque},
		AssetServer:      &assetserver.Options{Handler: http.HandlerFunc(d.serveLoader)},
		Menu:             d.buildMenu(),
		OnStartup:        d.startup,
		OnBeforeClose:    d.beforeClose,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               singleInstanceID,
			OnSecondInstanceLaunch: d.secondInstance,
		},
		// The drop is reported to Go, which types the paths into the terminal;
		// the page cancels the web view's own handling of it, which would
		// otherwise open the dropped file in place of the application.
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarDefault(),
			Appearance: mac.NSAppearanceNameDarkAqua,
			// Zooming the page would scale the terminal's cell grid without
			// telling it; View → Zoom changes the font size instead.
			DisableZoom: true,
			About: &mac.AboutInfo{
				Title:   appTitle,
				Message: aboutText(),
				Icon:    appIcon,
			},
		},
		Linux: &linux.Options{
			Icon:             appIcon,
			ProgramName:      programName,
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		return fmt.Errorf("run window: %w", err)
	}
	return nil
}

func aboutText() string {
	return fmt.Sprintf("Version %s\n\nManage and use tmux sessions in a native window.\n"+
		"Sessions keep running when the window closes.", version)
}

func (d *desktop) startup(ctx context.Context) {
	d.shell.attach(ctx)
	wruntime.OnFileDrop(ctx, d.onFileDrop)
}

// beforeClose runs when the window is about to close, by the close button or
// by Quit. Closing stops the hub, not the sessions, so the only thing a close
// can lose is an unsaved edit in the file manager.
func (d *desktop) beforeClose(ctx context.Context) bool {
	if d.bridge != nil && d.bridge.Unsaved() && !d.confirmedQuit.Load() {
		answer, err := wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{
			Type:  wruntime.QuestionDialog,
			Title: "Quit with unsaved changes?",
			// GTK labels the buttons Yes and No whatever they are asked to say,
			// so the message itself has to be the question.
			Message: "The file manager has changes that have not been saved. Quit anyway?\n\n" +
				"The changes will be lost. Your tmux sessions keep running either way.",
			Buttons:       []string{quitButton, cancelButton},
			DefaultButton: cancelButton,
			CancelButton:  cancelButton,
		})
		// macOS answers with the button's label; GTK has only Yes and No.
		if err != nil || (answer != quitButton && answer != gtkYes) {
			return true
		}
		d.confirmedQuit.Store(true)
	}
	d.saveWindowState(ctx)
	return false
}

// saveWindowState remembers the window for the next launch. A maximised or
// fullscreen window keeps the size it had before, so un-maximising it next
// time goes back to something smaller than the screen.
func (d *desktop) saveWindowState(ctx context.Context) {
	if wruntime.WindowIsFullscreen(ctx) {
		return
	}
	state := d.state
	state.Maximised = wruntime.WindowIsMaximised(ctx)
	if !state.Maximised {
		state.Width, state.Height = wruntime.WindowGetSize(ctx)
	}
	if err := winstate.Save(d.statePath, state); err != nil {
		wruntime.LogWarningf(ctx, "save window state: %v", err)
	}
}

func (d *desktop) secondInstance(options.SecondInstanceData) {
	ctx, ok := d.shell.context()
	if !ok {
		return
	}
	wruntime.WindowUnminimise(ctx)
	wruntime.Show(ctx)
	wruntime.WindowShow(ctx)
}

func (d *desktop) onFileDrop(_, _ int, paths []string) {
	if len(paths) > 0 {
		d.dispatch("insert-paths", paths)
	}
}

// dispatch sends a command to the page. The page may not have loaded, or may
// be the loader, so the receiver is looked up rather than assumed.
func (d *desktop) dispatch(command string, arg any) {
	ctx, ok := d.shell.context()
	if !ok {
		return
	}
	name, err := json.Marshal(command)
	if err != nil {
		return
	}
	payload, err := json.Marshal(arg)
	if err != nil {
		return
	}
	wruntime.WindowExecJS(ctx, fmt.Sprintf(
		"window.__vtcDesktop && window.__vtcDesktop.dispatch(%s, %s)", name, payload))
}

// reload opens the page afresh with the launch credential. A plain reload
// would do in the ordinary case, because the page keeps its token; going back
// through the launch address also recovers a page that has lost it.
func (d *desktop) reload() {
	ctx, ok := d.shell.context()
	if !ok || d.hub == nil {
		return
	}
	target, err := json.Marshal(d.hub.URL())
	if err != nil {
		return
	}
	// replace() alone would only move to the fragment of the page that is
	// already loaded, so it is followed by a reload of the address it set.
	wruntime.WindowExecJS(ctx, fmt.Sprintf("window.location.replace(%s); window.location.reload()", target))
}

// openConfigDir shows the configuration directory in the file manager. The
// toolkit's own opener refuses file: addresses, so the platform's is used.
func (d *desktop) openConfigDir() {
	opener := "xdg-open"
	if goruntime.GOOS == shortcuts.Darwin {
		opener = "open"
	}
	cmd := exec.Command(opener, d.configDir)
	if err := cmd.Start(); err != nil {
		if ctx, ok := d.shell.context(); ok {
			wruntime.LogWarningf(ctx, "open %s: %v", d.configDir, err)
		}
		return
	}
	go func() { _ = cmd.Wait() }() //nolint:errcheck // reaping the opener; its status means nothing here
}

var loaderPage = template.Must(template.New("loader").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Visual Tmux Client</title>
<style>
  html, body { margin: 0; height: 100%; background: #0a0a0a; color: #a3a3a3;
    font: 13px ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif; }
  main { height: 100%; display: flex; flex-direction: column; align-items: center;
    justify-content: center; gap: 10px; padding: 24px; box-sizing: border-box; text-align: center; }
  h1 { margin: 0; font-size: 15px; font-weight: 600; color: #f5f5f5; }
  pre { margin: 0; max-width: 640px; white-space: pre-wrap; color: #f87171;
    font: 12px ui-monospace, SFMono-Regular, Menlo, monospace; }
</style>
</head>
<body>
<main>
{{if .Error}}
  <h1>Visual Tmux Client could not start</h1>
  <pre>{{.Error}}</pre>
  <p>Fix the configuration in {{.ConfigDir}} and open the application again.</p>
{{else}}
  <p>Starting…</p>
  <script>window.location.replace({{.URL}})</script>
{{end}}
</main>
</body>
</html>
`))

// serveLoader answers the window's first page. It never holds the
// application: it only forwards the window to the hub, or says why it cannot.
func (d *desktop) serveLoader(w http.ResponseWriter, _ *http.Request) {
	data := struct {
		URL       string
		Error     string
		ConfigDir string
	}{ConfigDir: d.configDir}
	if d.hub != nil {
		data.URL = d.hub.URL()
	} else {
		data.Error = fmt.Sprint(d.startErr)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := loaderPage.Execute(w, data); err != nil {
		if ctx, ok := d.shell.context(); ok {
			wruntime.LogWarningf(ctx, "loader page: %v", err)
		}
	}
}
