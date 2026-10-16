package bridge

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const token = "secret"

type fakeShell struct {
	title      string
	fullscreen bool
	clipboard  string
	clipErr    error
	saveTo     string
	saveErr    error
	proposed   string
	opened     []string
}

func (f *fakeShell) SetTitle(title string) { f.title = title }
func (f *fakeShell) ToggleFullscreen() bool {
	f.fullscreen = !f.fullscreen
	return f.fullscreen
}
func (f *fakeShell) ClipboardText() (string, error)     { return f.clipboard, f.clipErr }
func (f *fakeShell) SetClipboardText(text string) error { f.clipboard = text; return f.clipErr }
func (f *fakeShell) SaveFileDialog(name string) (string, error) {
	f.proposed = name
	return f.saveTo, f.saveErr
}
func (f *fakeShell) OpenURL(url string) { f.opened = append(f.opened, url) }

// fakeHub stands in for the hub's router: it serves the download route the way
// the hub does, behind the same token, and records anything else it is asked.
type fakeHub struct {
	files  map[string]string
	others []string
}

func (h *fakeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/hosts/local/files/download" {
		h.others = append(h.others, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, ok := h.files[r.URL.Query().Get("path")]
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not_found"}`) //nolint:errcheck // test response
		return
	}
	_, _ = io.WriteString(w, body) //nolint:errcheck // test response
}

func setup(t *testing.T) (*fakeShell, *fakeHub, *Bridge, http.Handler) {
	t.Helper()
	shell := &fakeShell{}
	hub := &fakeHub{files: map[string]string{"/srv/notes.md": "# notes\n"}}
	b := New(shell, Info{Platform: "linux", Version: "1.0.0", Shortcuts: map[string]string{"help": "F1"}}, token)
	return shell, hub, b, b.Wrap(hub)
}

func call(t *testing.T, h http.Handler, method, path, body string, authed bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if authed {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestBridgeRequiresTheHubToken(t *testing.T) {
	shell, hub, _, h := setup(t)
	for _, path := range []string{"info", "title", "clipboard/read", "save", "nowhere"} {
		method := http.MethodPost
		if path == "info" {
			method = http.MethodGet
		}
		rec := call(t, h, method, Prefix+path, `{"title":"x"}`, false)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token: got %d", path, rec.Code)
		}
	}
	if shell.title != "" {
		t.Error("an unauthenticated request reached the window")
	}

	// Everything outside the prefix is the hub's, credential or not.
	if rec := call(t, h, http.MethodGet, "/api/client-config", "", false); rec.Code != http.StatusTeapot {
		t.Errorf("a hub route was not passed through: %d", rec.Code)
	}
	if len(hub.others) != 1 {
		t.Errorf("hub saw %v", hub.others)
	}
}

func TestInfoAndUnknownRoutes(t *testing.T) {
	_, _, _, h := setup(t)
	rec := call(t, h, http.MethodGet, Prefix+"info", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("info: %d", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["platform"] != "linux" || body["version"] != "1.0.0" {
		t.Errorf("info: %v", body)
	}
	if rec := call(t, h, http.MethodPost, Prefix+"format-disk", "{}", true); rec.Code != http.StatusNotFound {
		t.Errorf("unknown route: %d", rec.Code)
	}
}

func TestTitleIsOneBoundedLine(t *testing.T) {
	shell, _, _, h := setup(t)
	long := strings.Repeat("界", maxTitleRunes+50)
	if rec := call(t, h, http.MethodPost, Prefix+"title", `{"title":"a\nb\u0007c"}`, true); rec.Code != 204 {
		t.Fatalf("title: %d", rec.Code)
	}
	if shell.title != "a b c" {
		t.Errorf("control characters survived: %q", shell.title)
	}
	call(t, h, http.MethodPost, Prefix+"title", `{"title":"`+long+`"}`, true)
	if n := len([]rune(shell.title)); n != maxTitleRunes {
		t.Errorf("title kept %d runes", n)
	}
}

func TestClipboardAndFullscreen(t *testing.T) {
	shell, _, _, h := setup(t)
	call(t, h, http.MethodPost, Prefix+"clipboard/write", `{"text":"copied"}`, true)
	if shell.clipboard != "copied" {
		t.Errorf("clipboard: %q", shell.clipboard)
	}
	rec := call(t, h, http.MethodPost, Prefix+"clipboard/read", "", true)
	if decodeBody(t, rec)["text"] != "copied" {
		t.Errorf("read: %s", rec.Body.String())
	}
	shell.clipErr = errors.New("no display")
	if rec := call(t, h, http.MethodPost, Prefix+"clipboard/read", "", true); rec.Code != 500 {
		t.Errorf("a failing clipboard answered %d", rec.Code)
	}

	rec = call(t, h, http.MethodPost, Prefix+"fullscreen", "", true)
	if decodeBody(t, rec)["fullscreen"] != true {
		t.Errorf("fullscreen: %s", rec.Body.String())
	}
}

func TestOpenPassesOnlyWebAndMailLinks(t *testing.T) {
	shell, _, _, h := setup(t)
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "vscode://x", "https://", "not a url"} {
		if rec := call(t, h, http.MethodPost, Prefix+"open", `{"url":"`+u+`"}`, true); rec.Code != 400 {
			t.Errorf("%q was answered %d", u, rec.Code)
		}
	}
	for _, u := range []string{"https://github.com/tmux/tmux", "http://example.com/a", "mailto:a@example.com"} {
		if rec := call(t, h, http.MethodPost, Prefix+"open", `{"url":"`+u+`"}`, true); rec.Code != 204 {
			t.Errorf("%q was answered %d", u, rec.Code)
		}
	}
	if len(shell.opened) != 3 {
		t.Errorf("opened %v", shell.opened)
	}
}

func TestUnsavedFlag(t *testing.T) {
	_, _, b, h := setup(t)
	call(t, h, http.MethodPost, Prefix+"unsaved", `{"unsaved":true}`, true)
	if !b.Unsaved() {
		t.Error("unsaved was not recorded")
	}
	call(t, h, http.MethodPost, Prefix+"unsaved", `{"unsaved":false}`, true)
	if b.Unsaved() {
		t.Error("unsaved was not cleared")
	}
}

func TestSaveWritesThroughTheHubsDownloadRoute(t *testing.T) {
	shell, _, _, h := setup(t)
	dir := t.TempDir()
	shell.saveTo = filepath.Join(dir, "saved.md")

	rec := call(t, h, http.MethodPost, Prefix+"save", `{"path":"/srv/notes.md","name":"../../notes.md"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	if body := decodeBody(t, rec); body["saved"] != true || body["target"] != shell.saveTo {
		t.Errorf("save answered %v", body)
	}
	if shell.proposed != "notes.md" {
		t.Errorf("the dialog proposed %q", shell.proposed)
	}
	got, err := os.ReadFile(shell.saveTo)
	if err != nil || string(got) != "# notes\n" {
		t.Errorf("saved %q, %v", got, err)
	}
	entries, _ := os.ReadDir(dir) //nolint:errcheck // checked through the length
	if len(entries) != 1 {
		t.Errorf("a temporary file was left behind: %v", entries)
	}
}

func TestSaveCancelledOrRefusedLeavesTheTargetAlone(t *testing.T) {
	shell, _, _, h := setup(t)
	dir := t.TempDir()

	rec := call(t, h, http.MethodPost, Prefix+"save", `{"path":"/srv/notes.md","name":"notes.md"}`, true)
	if decodeBody(t, rec)["saved"] != false {
		t.Errorf("a cancelled dialog answered %s", rec.Body.String())
	}

	shell.saveTo = filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(shell.saveTo, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec = call(t, h, http.MethodPost, Prefix+"save", `{"path":"/srv/missing","name":"missing"}`, true)
	if rec.Code != http.StatusBadGateway || decodeBody(t, rec)["error"] != "not_found" {
		t.Errorf("a refused download answered %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := os.ReadFile(shell.saveTo); string(got) != "original" { //nolint:errcheck // compared below
		t.Errorf("the existing file became %q", got)
	}
	entries, _ := os.ReadDir(dir) //nolint:errcheck // checked through the length
	if len(entries) != 1 {
		t.Errorf("a temporary file was left behind: %v", entries)
	}

	if rec := call(t, h, http.MethodPost, Prefix+"save", `{"name":"x"}`, true); rec.Code != 400 {
		t.Errorf("a save with no path answered %d", rec.Code)
	}
}

func TestDefaultName(t *testing.T) {
	cases := map[[2]string]string{
		{"report.pdf", "/a/report.pdf"}: "report.pdf",
		{"../../x", "/a/x"}:             "x",
		{"", "/srv/data.bin"}:           "data.bin",
		{"..", "/"}:                     "download",
		{`..\..\win.txt`, ""}:           "win.txt",
	}
	for in, want := range cases {
		if got := defaultName(in[0], in[1]); got != want {
			t.Errorf("defaultName(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
