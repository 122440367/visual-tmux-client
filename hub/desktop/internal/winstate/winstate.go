// Package winstate remembers the window's size between launches, the way a
// desktop application is expected to reopen as it was left.
//
// Only the size and whether the window was maximised are kept. The toolkit
// places a window relative to the monitor it is on rather than to the desktop,
// so a remembered position cannot be restored reliably across a change of
// displays -- and a window restored off every screen is worse than one centred.
package winstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// The size bounds. The minimum is what the sidebar and a usable terminal need
// side by side; the maximum only rejects nonsense from a damaged file.
const (
	MinWidth      = 720
	MinHeight     = 460
	DefaultWidth  = 1200
	DefaultHeight = 780
	maxDimension  = 16384
)

// The state is the user's own: readable by nobody else.
const (
	dirMode  = 0o700
	fileMode = 0o600
)

// State is the remembered window.
type State struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised"`
}

// Default is the window a first launch opens.
func Default() State {
	return State{Width: DefaultWidth, Height: DefaultHeight}
}

func clamp(v, lo, hi, fallback int) int {
	if v <= 0 || v > maxDimension {
		return fallback
	}
	return max(lo, min(v, hi))
}

// Normalised returns the state with its size inside the bounds.
func (s State) Normalised() State {
	return State{
		Width:     clamp(s.Width, MinWidth, maxDimension, DefaultWidth),
		Height:    clamp(s.Height, MinHeight, maxDimension, DefaultHeight),
		Maximised: s.Maximised,
	}
}

// Load reads the state saved at path, or the default when there is none or it
// cannot be read: a lost window size is not a reason to refuse to start.
func Load(path string) State {
	data, err := os.ReadFile(path)
	if err != nil {
		return Default()
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return Default()
	}
	return s.Normalised()
}

// Save writes the state to path, through a temporary file so a crash while
// writing cannot leave a truncated one behind.
func Save(path string, s State) error {
	data, err := json.MarshalIndent(s.Normalised(), "", "  ")
	if err != nil {
		return fmt.Errorf("encode window state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, fileMode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
