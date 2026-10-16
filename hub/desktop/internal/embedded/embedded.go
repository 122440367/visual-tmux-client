// Package embedded runs the hub inside the desktop application.
//
// The hub is the same one the command-line server runs -- the same
// configuration, API, file manager and terminal streaming -- so the window shows
// exactly the browser client. Three things are decided here rather than by the
// configuration, because the desktop application owns them:
//
//   - The hub listens on loopback only. The window is its one client; a
//     configured server.addr is for the command-line server's browsers.
//   - The token is generated for every launch and handed to the window, so
//     there is nothing to type and nothing to store. An environment token is
//     ignored for the same reason.
//   - The port is a fixed preferred one when it is free. A web view keeps its
//     storage per origin, and the page keeps its preferences there (sidebar
//     width, font size, session order); a port that changed on every launch
//     would give it an empty store every time. When the port is taken the hub
//     falls back to any free one and the preferences are simply not found.
package embedded

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/AK12-Official/visual-tmux-client/hub/internal/app"
	"github.com/AK12-Official/visual-tmux-client/hub/internal/auth"
	"github.com/AK12-Official/visual-tmux-client/hub/internal/config"
)

const (
	// PreferredAddr is where the hub listens when it can.
	PreferredAddr = "127.0.0.1:47690"
	// fallbackAddr asks the system for any free loopback port.
	fallbackAddr = "127.0.0.1:0"
	// shutdownSlack is added to the configured phase budgets, as the
	// command-line server does.
	shutdownSlack = time.Second
)

// LaunchFragment prefixes the launch token in the page address; desktop.ts
// reads it under the same name.
const LaunchFragment = "vtc-desktop="

// Options configures a launch.
type Options struct {
	Version string
	// ConfigFile is a YAML configuration to load, or "" to use the implicit
	// one in WorkDir. VISUAL_TMUX_CLIENT_CONFIG is honoured as by the server.
	ConfigFile string
	// WorkDir resolves relative paths in the environment and is where the
	// implicit visual-tmux-client.yaml is found; see ConfigDir.
	WorkDir string
	// Wrap receives the token and returns the handler wrapper to mount the
	// desktop bridge with.
	Wrap func(token string) func(http.Handler) http.Handler
	// Static is the page to serve.
	Static fs.FS
	// App carries test doubles for the tmux backend; zero in production.
	App app.Options
	// Addrs overrides the listen addresses tried, in order.
	Addrs []string
}

// Hub is a running embedded hub.
type Hub struct {
	app   *app.App
	cfg   *config.Config
	token string
	errCh chan error
}

// Token is the hub's bearer token for this launch.
func (h *Hub) Token() string { return h.token }

// Addr is the address the hub is listening on.
func (h *Hub) Addr() string { return h.app.Addr() }

// URL is the page address the window opens, carrying the launch token in its
// fragment, which is never sent to a server.
func (h *Hub) URL() string {
	return "http://" + h.app.Addr() + "/#" + LaunchFragment + url.QueryEscape(h.token)
}

// Done reports the hub's server stopping on its own.
func (h *Hub) Done() <-chan error { return h.errCh }

// ConfigDir is the desktop application's per-user directory: the platform's
// configuration directory (~/Library/Application Support on macOS,
// $XDG_CONFIG_HOME or ~/.config on Linux). It is the hub's working directory,
// so a visual-tmux-client.yaml placed there is loaded the way the command-line
// server loads one from its working directory, and the window state is kept
// beside it.
func ConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "."
		}
		return filepath.Join(home, ".visual-tmux-client")
	}
	return filepath.Join(dir, "visual-tmux-client")
}

// Start loads the configuration and starts the hub.
func Start(ctx context.Context, opts Options) (*Hub, error) {
	var args []string
	if opts.ConfigFile != "" {
		args = []string{"--config", opts.ConfigFile}
	}
	cfg, prov, err := config.Load(args, opts.WorkDir, os.LookupEnv, opts.Version)
	if err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}

	token, err := auth.GenerateBearerToken()
	if err != nil {
		return nil, fmt.Errorf("generate bearer token: %w", err)
	}
	cfg.Auth.Token = token
	prov.TokenSource = config.SourceGenerated
	// The page and the hub always share an origin here, which is what the
	// strict same-host policy an empty origin selects checks for.
	cfg.WebSocket.Origin = ""

	addrs := opts.Addrs
	if len(addrs) == 0 {
		addrs = []string{PreferredAddr, fallbackAddr}
	}

	appOpts := opts.App
	if opts.Wrap != nil {
		appOpts.Wrap = opts.Wrap(token)
	}

	var errs []error
	for _, addr := range addrs {
		cfg.Server.Addr = addr
		a, err := app.New(cfg, *prov, opts.Static, appOpts)
		if err != nil {
			return nil, fmt.Errorf("create hub: %w", err)
		}
		errCh, err := a.Start(ctx)
		if err == nil {
			return &Hub{app: a, cfg: cfg, token: token, errCh: errCh}, nil
		}
		errs = append(errs, err)
		// Nothing was served, but the hub holds its file roots open from
		// construction; Shutdown is what gives them back.
		_ = a.Shutdown(ctx) //nolint:errcheck // a hub that never started has nothing to drain
	}
	return nil, fmt.Errorf("start hub: %w", errors.Join(errs...))
}

// Shutdown stops the hub. The tmux sessions keep running: detaching the
// window's attachments is all a shutdown does to them.
func (h *Hub) Shutdown() error {
	budget := h.cfg.Shutdown.AttachmentTimeout.Duration() + h.cfg.Shutdown.HTTPTimeout.Duration() + shutdownSlack
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	if err := h.app.Shutdown(ctx); err != nil {
		return fmt.Errorf("shut down hub: %w", err)
	}
	return nil
}
