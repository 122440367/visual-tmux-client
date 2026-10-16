package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// wailsShell is the bridge's view of the native window. The hub is serving
// before the window exists, so a request that arrives first finds no context
// and is answered with the window's absence rather than a crash.
type wailsShell struct {
	ctx atomic.Pointer[context.Context]
}

var errNoWindow = fmt.Errorf("the window is not open yet")

func (s *wailsShell) attach(ctx context.Context) {
	s.ctx.Store(&ctx)
}

func (s *wailsShell) context() (context.Context, bool) {
	p := s.ctx.Load()
	if p == nil {
		return nil, false
	}
	return *p, true
}

func (s *wailsShell) SetTitle(title string) {
	if ctx, ok := s.context(); ok {
		wruntime.WindowSetTitle(ctx, title)
	}
}

func (s *wailsShell) ToggleFullscreen() bool {
	ctx, ok := s.context()
	if !ok {
		return false
	}
	if wruntime.WindowIsFullscreen(ctx) {
		wruntime.WindowUnfullscreen(ctx)
		return false
	}
	wruntime.WindowFullscreen(ctx)
	return true
}

func (s *wailsShell) ClipboardText() (string, error) {
	ctx, ok := s.context()
	if !ok {
		return "", errNoWindow
	}
	text, err := wruntime.ClipboardGetText(ctx)
	if err != nil {
		return "", fmt.Errorf("read clipboard: %w", err)
	}
	return text, nil
}

func (s *wailsShell) SetClipboardText(text string) error {
	ctx, ok := s.context()
	if !ok {
		return errNoWindow
	}
	if err := wruntime.ClipboardSetText(ctx, text); err != nil {
		return fmt.Errorf("write clipboard: %w", err)
	}
	return nil
}

func (s *wailsShell) SaveFileDialog(defaultName string) (string, error) {
	ctx, ok := s.context()
	if !ok {
		return "", errNoWindow
	}
	target, err := wruntime.SaveFileDialog(ctx, wruntime.SaveDialogOptions{
		Title:                "Save " + defaultName,
		DefaultDirectory:     downloadsDir(),
		DefaultFilename:      defaultName,
		CanCreateDirectories: true,
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	return target, nil
}

func (s *wailsShell) OpenURL(url string) {
	if ctx, ok := s.context(); ok {
		wruntime.BrowserOpenURL(ctx, url)
	}
}

// downloadsDir is where a save dialog opens: the user's Downloads folder, as a
// browser's download would land, or the home directory when there is none.
// Left to itself the dialog opens in the process's working directory, which for
// an application started from a launcher is wherever the launcher was.
func downloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if dir := os.Getenv("XDG_DOWNLOAD_DIR"); dir != "" && isDir(dir) {
		return dir
	}
	if dir := filepath.Join(home, "Downloads"); isDir(dir) {
		return dir
	}
	return home
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
