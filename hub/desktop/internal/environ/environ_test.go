package environ

import (
	"os"
	"strings"
	"testing"
)

type mapEnv map[string]string

func (m mapEnv) LookupEnv(key string) (string, bool) {
	v, ok := m[key]
	return v, ok
}

func (m mapEnv) Setenv(key, value string) error {
	m[key] = value
	return nil
}

func existing(dirs ...string) func(string) bool {
	set := map[string]bool{}
	for _, d := range dirs {
		set[d] = true
	}
	return func(p string) bool { return set[p] }
}

func TestFixAddsExistingToolDirectoriesAfterThePath(t *testing.T) {
	env := mapEnv{"PATH": "/usr/bin:/bin:/usr/local/bin"}
	exists := existing("/opt/homebrew/bin", "/usr/local/bin", "/Users/u/bin")
	if err := Fix(env, "darwin", "/Users/u", exists); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{"/usr/bin", "/bin", "/usr/local/bin", "/opt/homebrew/bin", "/Users/u/bin"},
		string(os.PathListSeparator))
	if env["PATH"] != want {
		t.Errorf("PATH = %q, want %q", env["PATH"], want)
	}
}

func TestFixSuppliesAUTF8LocaleOnlyWhenNoneIsSet(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "en_US.UTF-8", "linux": "C.UTF-8"} {
		env := mapEnv{}
		if err := Fix(env, goos, "", existing()); err != nil {
			t.Fatal(err)
		}
		if env["LANG"] != want {
			t.Errorf("%s: LANG = %q, want %q", goos, env["LANG"], want)
		}
	}
	for _, key := range localeVars {
		env := mapEnv{key: "fr_FR.ISO-8859-1"}
		if err := Fix(env, "linux", "", existing()); err != nil {
			t.Fatal(err)
		}
		if _, set := env["LANG"]; set && key != "LANG" {
			t.Errorf("a chosen %s was overridden with LANG=%q", key, env["LANG"])
		}
		if env[key] != "fr_FR.ISO-8859-1" {
			t.Errorf("%s was changed to %q", key, env[key])
		}
	}
}
