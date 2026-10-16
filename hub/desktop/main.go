// Command visual-tmux-client-desktop is Visual Tmux Client as a desktop
// application: the hub runs in this process, on loopback, and a native window
// shows the same page the browser client is. See internal/embedded for how the
// hub is started and internal/bridge for what the page can ask of the window.
package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/AK12-Official/visual-tmux-client/hub/desktop/internal/bridge"
	"github.com/AK12-Official/visual-tmux-client/hub/desktop/internal/embedded"
	"github.com/AK12-Official/visual-tmux-client/hub/desktop/internal/environ"
	"github.com/AK12-Official/visual-tmux-client/hub/desktop/internal/shortcuts"
	"github.com/AK12-Official/visual-tmux-client/hub/desktop/internal/winstate"
	"github.com/AK12-Official/visual-tmux-client/hub/web"
)

// version is replaced at build time via -X main.version for release builds.
var version = "dev"

//go:embed build/appicon.png
var appIcon []byte

const (
	appTitle    = "Visual Tmux Client"
	programName = "visual-tmux-client"
	stateFile   = "window.json"
	// configDirMode keeps the configuration directory the user's own: the
	// configuration can carry file roots, and nothing in it is for others.
	configDirMode = 0o700
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", programName, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print version and exit")
	configPath := fs.String("config", "", "path to a YAML configuration file "+
		"(default: visual-tmux-client.yaml in the per-user configuration directory)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		_, err := fmt.Fprintln(stdout, version)
		return err
	}

	home, _ := os.UserHomeDir() //nolint:errcheck // no home only means fewer directories to search
	if err := environ.Fix(environ.OS{}, goruntime.GOOS, home, environ.DirExists); err != nil {
		return fmt.Errorf("prepare environment: %w", err)
	}

	dir := embedded.ConfigDir()
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	table := shortcuts.For(goruntime.GOOS)
	shell := &wailsShell{}
	info := bridge.Info{Platform: goruntime.GOOS, Version: version, Shortcuts: table.Labels(goruntime.GOOS)}

	d := &desktop{
		shell:     shell,
		table:     table,
		configDir: dir,
		statePath: filepath.Join(dir, stateFile),
	}
	hub, err := embedded.Start(context.Background(), embedded.Options{
		Version:    version,
		ConfigFile: *configPath,
		WorkDir:    dir,
		Static:     web.FS(),
		Wrap: func(token string) func(http.Handler) http.Handler {
			d.bridge = bridge.New(shell, info, token)
			return d.bridge.Wrap
		},
	})
	if err != nil {
		// The window still opens, to say what went wrong: an application
		// launched from the Dock has no terminal to print to.
		d.startErr = err
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", programName, err) //nolint:errcheck // also shown in the window
	} else {
		d.hub = hub
		defer func() {
			if err := hub.Shutdown(); err != nil {
				_, _ = fmt.Fprintf(stderr, "%s: %v\n", programName, err) //nolint:errcheck // exiting anyway
			}
		}()
	}

	return d.run(winstate.Load(d.statePath))
}
