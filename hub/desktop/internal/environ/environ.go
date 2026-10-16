// Package environ repairs the environment a desktop launch starts with.
//
// A program started from a terminal inherits the user's shell environment. One
// started from the Dock, Finder or a desktop launcher does not: on macOS its
// PATH is the system's /usr/bin:/bin:/usr/sbin:/sbin and it has no locale at
// all. Both matter here, because the hub finds tmux on PATH, and tmux decides
// from the locale whether its client may print UTF-8 -- without one it shows
// every non-ASCII character, CJK session names and file names included, as an
// underscore. Everything tmux starts inherits this environment too.
package environ

import (
	"os"
	"path/filepath"
	"strings"
)

// Env is the slice of the process environment this package reads and writes.
type Env interface {
	LookupEnv(key string) (string, bool)
	Setenv(key, value string) error
}

// OS is the process environment.
type OS struct{}

// LookupEnv implements Env.
func (OS) LookupEnv(key string) (string, bool) { return os.LookupEnv(key) }

// Setenv implements Env.
func (OS) Setenv(key, value string) error { return os.Setenv(key, value) }

// searchDirs are where package managers put tmux, ahead of the system
// directories a desktop launch already has. Only directories that exist are
// added.
func searchDirs(home string) []string {
	dirs := []string{
		"/opt/homebrew/bin",              // Homebrew on Apple silicon
		"/usr/local/bin",                 // Homebrew on Intel, manual installs
		"/opt/local/bin",                 // MacPorts
		"/home/linuxbrew/.linuxbrew/bin", // Homebrew on Linux
		"/run/current-system/sw/bin",     // NixOS
		"/nix/var/nix/profiles/default/bin",
		"/snap/bin",
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".nix-profile", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, "bin"),
		)
	}
	return dirs
}

// localeVars are the variables tmux consults, in its own order of precedence.
var localeVars = []string{"LC_ALL", "LC_CTYPE", "LANG"}

// Fix adds the usual tool directories to PATH and supplies a UTF-8 locale when
// none is set. It only ever adds: a directory already on PATH stays where it is,
// and a locale the user chose -- UTF-8 or not -- is left alone.
func Fix(env Env, goos, home string, exists func(string) bool) error {
	path, _ := env.LookupEnv("PATH")
	have := map[string]bool{}
	var parts []string
	for _, p := range filepath.SplitList(path) {
		if p != "" {
			have[p] = true
			parts = append(parts, p)
		}
	}
	for _, dir := range searchDirs(home) {
		if !have[dir] && exists(dir) {
			have[dir] = true
			parts = append(parts, dir)
		}
	}
	if err := env.Setenv("PATH", strings.Join(parts, string(os.PathListSeparator))); err != nil {
		return err
	}

	for _, key := range localeVars {
		if v, ok := env.LookupEnv(key); ok && v != "" {
			return nil
		}
	}
	// C.UTF-8 exists on every current Linux distribution; macOS has no C.UTF-8
	// but always has en_US.UTF-8.
	locale := "C.UTF-8"
	if goos == "darwin" {
		locale = "en_US.UTF-8"
	}
	return env.Setenv("LANG", locale)
}

// DirExists reports whether path is a directory.
func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
