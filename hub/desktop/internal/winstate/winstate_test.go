package winstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "window.json")
	want := State{Width: 1440, Height: 900, Maximised: true}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temporary file was left behind")
	}
}

func TestLoadFallsBackToTheDefault(t *testing.T) {
	dir := t.TempDir()
	if got := Load(filepath.Join(dir, "missing.json")); got != Default() {
		t.Errorf("missing file: %+v", got)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(bad); got != Default() {
		t.Errorf("damaged file: %+v", got)
	}
}

func TestNormalisedKeepsTheWindowUsable(t *testing.T) {
	cases := map[State]State{
		{Width: 100, Height: 100}:   {Width: MinWidth, Height: MinHeight},
		{Width: -5, Height: 0}:      {Width: DefaultWidth, Height: DefaultHeight},
		{Width: 99999, Height: 800}: {Width: DefaultWidth, Height: 800},
		{Width: 1300, Height: 850}:  {Width: 1300, Height: 850},
	}
	for in, want := range cases {
		if got := in.Normalised(); got != want {
			t.Errorf("%+v: got %+v, want %+v", in, got, want)
		}
	}
}
