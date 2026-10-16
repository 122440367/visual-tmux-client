// Package bridge is the page's way into the desktop shell.
//
// The page is served by the hub over loopback HTTP, not by the window toolkit,
// so the toolkit's own JavaScript bindings are not on it. What the page needs
// from the native side is small and request-shaped -- set the title, read the
// clipboard, save a file -- so it travels the way everything else the page asks
// for travels: as an authenticated request to the hub's listener. These routes
// are mounted beside the hub's through app.Options.Wrap and carry the same bearer
// token, so nothing that does not hold the hub's credential can drive the window.
package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"unicode"

	thttp "github.com/AK12-Official/visual-tmux-client/hub/internal/transport/http"
)

// Prefix is where the bridge's routes live.
const Prefix = "/api/desktop/"

const (
	// maxRequestBytes bounds every request except a clipboard write.
	maxRequestBytes = 64 << 10
	// maxClipboardBytes bounds a clipboard write: a large selection is real,
	// a clipboard of hundreds of megabytes is not.
	maxClipboardBytes = 16 << 20
	// maxTitleRunes bounds a window title. Session names are at most 64
	// characters; the rest is decoration.
	maxTitleRunes = 200
	// downloadRoute is the hub's file download route, which a save goes
	// through so the hub's checks apply to it.
	downloadRoute = "/api/hosts/local/files/download?path="
)

// Shell is the native window, as far as the page may reach it.
type Shell interface {
	SetTitle(title string)
	// ToggleFullscreen switches the window and reports whether it is now
	// fullscreen.
	ToggleFullscreen() bool
	ClipboardText() (string, error)
	SetClipboardText(text string) error
	// SaveFileDialog asks where to save a file, and answers "" when the user
	// cancels.
	SaveFileDialog(defaultName string) (string, error)
	OpenURL(url string)
}

// Info is what the page is told about the shell.
type Info struct {
	Platform  string            `json:"platform"`
	Version   string            `json:"version"`
	Shortcuts map[string]string `json:"shortcuts"`
}

// Bridge serves the page's requests to the shell.
type Bridge struct {
	shell   Shell
	info    Info
	token   string
	unsaved atomic.Bool
}

// New returns a bridge for a shell. token is the hub's bearer token: the
// bridge requires it on every request, and presents it to the hub when it
// fetches a file on the page's behalf.
func New(shell Shell, info Info, token string) *Bridge {
	return &Bridge{shell: shell, info: info, token: token}
}

// Unsaved reports whether the page last said it holds edits that quitting
// would lose.
func (b *Bridge) Unsaved() bool {
	return b.unsaved.Load()
}

// Wrap mounts the bridge's routes in front of the hub's handler. It has the
// shape app.Options.Wrap takes.
func (b *Bridge) Wrap(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+Prefix+"info", b.handleInfo)
	mux.HandleFunc("POST "+Prefix+"title", b.handleTitle)
	mux.HandleFunc("POST "+Prefix+"fullscreen", b.handleFullscreen)
	mux.HandleFunc("POST "+Prefix+"clipboard/read", b.handleClipboardRead)
	mux.HandleFunc("POST "+Prefix+"clipboard/write", b.handleClipboardWrite)
	mux.HandleFunc("POST "+Prefix+"open", b.handleOpen)
	mux.HandleFunc("POST "+Prefix+"unsaved", b.handleUnsaved)
	mux.Handle("POST "+Prefix+"save", b.saveHandler(next))
	mux.HandleFunc(Prefix, func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found")
	})

	// The hub's own check, so the bridge accepts exactly the credential the
	// rest of the API does.
	authed := thttp.RequireAuth(b.token)(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, Prefix) {
			authed.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (b *Bridge) handleInfo(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, b.info)
}

func (b *Bridge) handleTitle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if !decode(w, r, maxRequestBytes, &req) {
		return
	}
	b.shell.SetTitle(cleanTitle(req.Title))
	w.WriteHeader(http.StatusNoContent)
}

// cleanTitle keeps a title to one printable line of bounded length. It names
// a session, and a session name is user text.
func cleanTitle(title string) string {
	var out []rune
	for _, r := range title {
		if len(out) == maxTitleRunes {
			break
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		out = append(out, r)
	}
	return strings.TrimSpace(string(out))
}

func (b *Bridge) handleFullscreen(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"fullscreen": b.shell.ToggleFullscreen()})
}

func (b *Bridge) handleClipboardRead(w http.ResponseWriter, _ *http.Request) {
	text, err := b.shell.ClipboardText()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "clipboard_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (b *Bridge) handleClipboardWrite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !decode(w, r, maxClipboardBytes, &req) {
		return
	}
	if err := b.shell.SetClipboardText(req.Text); err != nil {
		writeError(w, http.StatusInternalServerError, "clipboard_unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleOpen opens a link in the user's browser. Only web and mail links are
// passed on: anything else -- file:, a custom scheme -- would hand the page a
// way to launch programs on the machine.
func (b *Bridge) handleOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if !decode(w, r, maxRequestBytes, &req) {
		return
	}
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") ||
		(u.Scheme != "mailto" && u.Host == "") {
		writeError(w, http.StatusBadRequest, "invalid_url")
		return
	}
	b.shell.OpenURL(u.String())
	w.WriteHeader(http.StatusNoContent)
}

func (b *Bridge) handleUnsaved(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Unsaved bool `json:"unsaved"`
	}
	if !decode(w, r, maxRequestBytes, &req) {
		return
	}
	b.unsaved.Store(req.Unsaved)
	w.WriteHeader(http.StatusNoContent)
}

// saveHandler saves a file from the hub to a place the user picks.
//
// The file is fetched through the hub's own download route rather than read
// here: that route is where the file manager's boundary is enforced -- the
// configured roots, the size limit, refusing what is not a regular file -- and
// a second way to read a file would be a second boundary to keep in step. The
// request never leaves the process; it is served straight into the file.
func (b *Bridge) saveHandler(hub http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		if !decode(w, r, maxRequestBytes, &req) {
			return
		}
		if req.Path == "" {
			writeError(w, http.StatusBadRequest, "invalid_path")
			return
		}
		target, err := b.shell.SaveFileDialog(defaultName(req.Name, req.Path))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "dialog_failed")
			return
		}
		if target == "" {
			writeJSON(w, http.StatusOK, map[string]any{"saved": false})
			return
		}
		code, err := b.fetchInto(r.Context(), hub, req.Path, target)
		if err != nil {
			status := http.StatusInternalServerError
			if code != "" {
				status = http.StatusBadGateway
			} else {
				code = "save_failed"
			}
			writeError(w, status, code)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"saved": true, "target": target})
	})
}

// defaultName is the name the save dialog proposes: the file's own, reduced to
// a single path element so a crafted name cannot point the dialog elsewhere.
func defaultName(name, path string) string {
	for _, candidate := range []string{name, path} {
		base := filepath.Base(strings.ReplaceAll(candidate, "\\", "/"))
		if base != "." && base != "/" && base != ".." && base != "" {
			return base
		}
	}
	return "download"
}

// errHubRefused carries the hub's own refusal code out of fetchInto.
var errHubRefused = errors.New("hub refused the download")

// fetchInto serves the hub's download of path into target. The bytes go to a
// temporary file beside the target first and are renamed over it only once the
// hub has finished, so a refused or interrupted download never leaves a
// truncated file under the chosen name -- or destroys the one that was there.
func (b *Bridge) fetchInto(ctx context.Context, hub http.Handler, path, target string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadRoute+url.QueryEscape(path), nil)
	if err != nil {
		return "", fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.token)

	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.part")
	if err != nil {
		return "", fmt.Errorf("create temporary file: %w", err)
	}
	defer func() {
		_ = os.Remove(tmp.Name()) //nolint:errcheck // gone already once renamed
	}()

	sink := &fileResponse{file: tmp, header: http.Header{}}
	hub.ServeHTTP(sink, req)
	closeErr := tmp.Close()

	switch {
	case sink.status != 0 && sink.status != http.StatusOK:
		return refusalCode(sink.body.String()), errHubRefused
	case sink.err != nil:
		return "", fmt.Errorf("write %s: %w", target, sink.err)
	case closeErr != nil:
		return "", fmt.Errorf("close %s: %w", target, closeErr)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return "", fmt.Errorf("move into %s: %w", target, err)
	}
	return "", nil
}

// refusalCode reads the hub's {"error": code} body, or names the refusal
// generically when there is none.
func refusalCode(body string) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(body), &e) == nil && e.Error != "" {
		return e.Error
	}
	return "download_failed"
}

// fileResponse is a ResponseWriter that streams a successful body into a file
// and keeps an unsuccessful one, which is the hub's error, in memory.
type fileResponse struct {
	file   *os.File
	header http.Header
	status int
	body   strings.Builder
	err    error
}

func (f *fileResponse) Header() http.Header { return f.header }

func (f *fileResponse) WriteHeader(status int) {
	if f.status == 0 {
		f.status = status
	}
}

func (f *fileResponse) Write(p []byte) (int, error) {
	if f.status == 0 {
		f.status = http.StatusOK
	}
	if f.status != http.StatusOK {
		if f.body.Len() < maxRequestBytes {
			f.body.Write(p)
		}
		return len(p), nil
	}
	if f.err != nil {
		return 0, f.err
	}
	n, err := f.file.Write(p)
	if err != nil {
		f.err = err
	}
	return n, err
}

func decode(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) //nolint:errcheck // writing response
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
