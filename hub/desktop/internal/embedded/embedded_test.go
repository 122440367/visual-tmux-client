package embedded

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/AK12-Official/visual-tmux-client/hub/internal/app"
	"github.com/AK12-Official/visual-tmux-client/hub/internal/session"
	"github.com/AK12-Official/visual-tmux-client/hub/internal/terminal"
)

// The hub is started against doubles: these tests are about how the desktop
// application launches it, and no tmux is involved.
type noBackend struct{}

func (noBackend) List(context.Context) ([]session.Session, error) { return nil, nil }
func (noBackend) Create(_ context.Context, name string) (*session.Session, error) {
	return &session.Session{Name: name}, nil
}
func (noBackend) Rename(context.Context, string, string) error { return nil }
func (noBackend) Kill(context.Context, string) error           { return nil }

type noProcesses struct{}

func (noProcesses) NewProcess(context.Context, string, int, int) (terminal.Process, error) {
	return nil, nil
}

func options(t *testing.T, addrs ...string) Options {
	t.Helper()
	// An empty directory, so a developer's own configuration is not read, and
	// no inherited variables that would point the hub elsewhere.
	for _, key := range []string{"VISUAL_TMUX_CLIENT_CONFIG", "VISUAL_TMUX_CLIENT_ADDR", "VISUAL_TMUX_CLIENT_TOKEN",
		"VISUAL_TMUX_CLIENT_ORIGIN", "VISUAL_TMUX_CLIENT_TMUX_PATH"} {
		// Set and then unset: Setenv is what registers the restore.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	return Options{
		Version: "test",
		WorkDir: t.TempDir(),
		Static:  fstest.MapFS{"index.html": {Data: []byte("<html>page</html>")}},
		App:     app.Options{Backend: noBackend{}, ProcessFact: noProcesses{}},
		Addrs:   addrs,
	}
}

func get(t *testing.T, target, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // test cleanup
	body, _ := io.ReadAll(resp.Body)         //nolint:errcheck // compared by the caller
	return resp.StatusCode, string(body)
}

func TestStartFallsBackWhenThePreferredPortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }() //nolint:errcheck // test cleanup

	opts := options(t, taken.Addr().String(), "127.0.0.1:0")
	var wrappedWith string
	opts.Wrap = func(token string) func(http.Handler) http.Handler {
		wrappedWith = token
		return func(next http.Handler) http.Handler { return next }
	}
	h, err := Start(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := h.Shutdown(); err != nil {
			t.Error(err)
		}
	}()

	if h.Addr() == taken.Addr().String() || !strings.HasPrefix(h.Addr(), "127.0.0.1:") {
		t.Errorf("listening on %s", h.Addr())
	}
	if h.Token() == "" || wrappedWith != h.Token() {
		t.Errorf("the bridge was given %q for token %q", wrappedWith, h.Token())
	}

	u, err := url.Parse(h.URL())
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != h.Addr() || u.Fragment != LaunchFragment+h.Token() {
		t.Errorf("launch URL %s", h.URL())
	}

	if code, body := get(t, "http://"+h.Addr()+"/", ""); code != 200 || body != "<html>page</html>" {
		t.Errorf("page: %d %q", code, body)
	}
	if code, _ := get(t, "http://"+h.Addr()+"/api/hosts/local/sessions", h.Token()); code != 200 {
		t.Errorf("the launch token was refused: %d", code)
	}
}

// The command-line server takes its token from the environment; the desktop
// application must not, or a token exported for the server would also open
// the window's hub to whoever knows it.
func TestStartIgnoresAnEnvironmentToken(t *testing.T) {
	opts := options(t, "127.0.0.1:0")
	t.Setenv("VISUAL_TMUX_CLIENT_TOKEN", "from-the-environment")
	h, err := Start(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Shutdown() }() //nolint:errcheck // test cleanup
	if h.Token() == "from-the-environment" {
		t.Fatal("the environment token was used")
	}
	if code, _ := get(t, "http://"+h.Addr()+"/api/hosts/local/sessions", "from-the-environment"); code != 401 {
		t.Errorf("the environment token was accepted: %d", code)
	}
}

func TestStartReportsWhenNoAddressCanBeBound(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }() //nolint:errcheck // test cleanup
	if _, err := Start(context.Background(), options(t, taken.Addr().String())); err == nil {
		t.Fatal("expected an error")
	}
}
