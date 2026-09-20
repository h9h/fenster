package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleLayout(id, name string) Layout {
	return Layout{
		ID:      id,
		Name:    name,
		Created: time.Date(2026, 9, 18, 13, 40, 0, 0, time.UTC),
		Updated: time.Date(2026, 9, 18, 13, 40, 0, 0, time.UTC),
		Setup: Setup{
			Fingerprint: "a3f19c02",
			Label:       "2 Monitore · 5120×1440 + 1710×1073",
			Monitors:    []Monitor{{X: 0, Y: 0, W: 1710, H: 1073, Scale: 150, Primary: true}},
		},
		Windows: []WindowEntry{{
			Exe:     `C:\Program Files\Editor\editor.exe`,
			Class:   "EditorClass",
			Title:   "main.go - Editor",
			Ordinal: 0,
			Rect:    Rect{X: 227, Y: -682, W: 1129, H: 635},
			Screen:  Rect{X: -46, Y: -1440, W: 3425, H: 1398},
			State:   StateNormal,
			Include: true,
		}},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layouts.json")
	s := New(path)
	s.Add(sampleLayout("l1", "Docked"))
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := New(path)
	if _, err := got.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	layouts := got.Layouts()
	if len(layouts) != 1 {
		t.Fatalf("got %d layouts, want 1", len(layouts))
	}
	if layouts[0].Name != "Docked" || layouts[0].Windows[0].Rect.Y != -682 {
		t.Errorf("round trip mismatch: %+v", layouts[0])
	}
	if want := (Rect{X: -46, Y: -1440, W: 3425, H: 1398}); layouts[0].Windows[0].Screen != want {
		t.Errorf("Screen round trip mismatch: got %+v, want %+v", layouts[0].Windows[0].Screen, want)
	}
}

// TestLoadOldLayoutWithoutScreenDefaultsToZero pins backward compatibility:
// a layouts.json written before the Screen field existed has no "screen" key
// at all, and must still decode cleanly with a zero Screen, which callers
// treat as "absent, behave as before" rather than as a real rectangle at
// (0,0).
func TestLoadOldLayoutWithoutScreenDefaultsToZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layouts.json")
	oldFormat := `{
		"version": 1,
		"layouts": [{
			"id": "l1",
			"name": "Docked",
			"created": "2026-09-18T13:40:00Z",
			"updated": "2026-09-18T13:40:00Z",
			"setup": {"fingerprint": "a3f19c02", "label": "", "monitors": []},
			"windows": [{
				"exe": "C:\\a\\editor.exe",
				"class": "EditorClass",
				"title": "Editor",
				"ordinal": 0,
				"rect": {"x": 227, "y": -682, "w": 1129, "h": 635},
				"state": "normal",
				"topmost": false,
				"include": true
			}]
		}]
	}`
	if err := os.WriteFile(path, []byte(oldFormat), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(path)
	if _, err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	layouts := s.Layouts()
	if len(layouts) != 1 || len(layouts[0].Windows) != 1 {
		t.Fatalf("unexpected layouts: %+v", layouts)
	}
	if got := layouts[0].Windows[0].Screen; got != (Rect{}) {
		t.Errorf("Screen = %+v, want zero value for an old file without the field", got)
	}
	if got := layouts[0].Windows[0].Rect; got != (Rect{X: 227, Y: -682, W: 1129, H: 635}) {
		t.Errorf("Rect = %+v, want the value from the old file", got)
	}
}

func TestLoadMissingFileStartsEmpty(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "layouts.json"))
	recovered, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if recovered != "" {
		t.Errorf("recovered = %q, want empty", recovered)
	}
	if len(s.Layouts()) != 0 {
		t.Errorf("want empty store")
	}
}

func TestLoadCorruptFileIsRenamedAndReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "layouts.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(path)
	recovered, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(recovered, "broken") {
		t.Fatalf("recovered = %q, want a .broken- path", recovered)
	}
	if _, err := os.Stat(recovered); err != nil {
		t.Errorf("broken file not kept: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("corrupt file should have been moved away")
	}
	if len(s.Layouts()) != 0 {
		t.Errorf("want empty store after recovery")
	}
}

func TestLoadRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layouts.json")
	blob, _ := json.Marshal(File{Version: SchemaVersion + 1})
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := New(path).Load(); err == nil {
		t.Fatal("want error for newer schema version")
	}
}

func TestSaveIsAtomicAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "layouts.json")
	s := New(path)
	s.Add(sampleLayout("l1", "Docked"))
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "layouts.json" {
		t.Errorf("unexpected files in dir: %v", entries)
	}
}

func TestReplaceDeleteAndSetInclude(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "layouts.json"))
	s.Add(sampleLayout("l1", "Docked"))
	s.Add(sampleLayout("l2", "Mobile"))

	renamed := sampleLayout("l1", "Docked v2")
	if err := s.Replace("l1", renamed); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if got, _ := s.Get("l1"); got.Name != "Docked v2" {
		t.Errorf("Replace did not apply: %q", got.Name)
	}

	if err := s.SetInclude("l1", 0, false); err != nil {
		t.Fatalf("SetInclude: %v", err)
	}
	if got, _ := s.Get("l1"); got.Windows[0].Include {
		t.Errorf("SetInclude did not apply")
	}
	if err := s.SetInclude("l1", 7, false); err == nil {
		t.Errorf("want error for out-of-range window index")
	}

	if err := s.Delete("l2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get("l2"); ok {
		t.Errorf("Delete did not remove the layout")
	}
	if err := s.Delete("nope"); err == nil {
		t.Errorf("want error deleting unknown id")
	}
}

func TestNewIDIsUniqueAndShort(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewID()
		if len(id) != 6 {
			t.Fatalf("id %q has length %d, want 6", id, len(id))
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestLoadTreatsMissingHotkeyAsNone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "layouts.json")
	// A file as written before Layout.Hotkey existed.
	blob := `{"version":1,"layouts":[{"id":"a","name":"Alt","windows":[]}]}`
	if err := os.WriteFile(path, []byte(blob), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(path)
	if recovered, err := s.Load(); err != nil || recovered != "" {
		t.Fatalf("Load() = (%q, %v), want (\"\", nil)", recovered, err)
	}
	l, ok := s.Get("a")
	if !ok {
		t.Fatal("layout a missing after load")
	}
	if l.Hotkey != "" {
		t.Errorf("Hotkey = %q, want empty", l.Hotkey)
	}
}

func TestHotkeyRoundTripsThroughSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "layouts.json")

	s := New(path)
	s.Add(Layout{ID: "a", Name: "Dock", Hotkey: "Ctrl+Alt+1"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded := New(path)
	if _, err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	l, ok := reloaded.Get("a")
	if !ok {
		t.Fatal("layout a missing after reload")
	}
	if l.Hotkey != "Ctrl+Alt+1" {
		t.Errorf("Hotkey = %q, want %q", l.Hotkey, "Ctrl+Alt+1")
	}
}

// TestMinimizeOthersDefaultsOnWhenAbsent is the load-bearing case for the
// "default on" requirement: a layouts.json written before the setting
// existed has no such key, and must still come up enabled.
func TestMinimizeOthersDefaultsOnWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "layouts.json")
	blob := `{"version":1,"layouts":[]}`
	if err := os.WriteFile(path, []byte(blob), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(path)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if !s.MinimizeOthers() {
		t.Error("MinimizeOthers() = false for a file with no key, want true")
	}
}

func TestMinimizeOthersHonoursAnExplicitValue(t *testing.T) {
	for _, tc := range []struct {
		blob string
		want bool
	}{
		{`{"version":1,"minimizeOthers":false,"layouts":[]}`, false},
		{`{"version":1,"minimizeOthers":true,"layouts":[]}`, true},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "layouts.json")
		if err := os.WriteFile(path, []byte(tc.blob), 0o644); err != nil {
			t.Fatal(err)
		}
		s := New(path)
		if _, err := s.Load(); err != nil {
			t.Fatal(err)
		}
		if got := s.MinimizeOthers(); got != tc.want {
			t.Errorf("%s: MinimizeOthers() = %v, want %v", tc.blob, got, tc.want)
		}
	}
}

// TestMinimizeOthersRoundTrips pins that turning the option OFF survives a
// save/load cycle. A plain bool field with omitempty would drop the false
// back out of the file and silently re-enable the feature on next start.
func TestMinimizeOthersRoundTrips(t *testing.T) {
	for _, want := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, "layouts.json")

		s := New(path)
		s.SetMinimizeOthers(want)
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}

		reloaded := New(path)
		if _, err := reloaded.Load(); err != nil {
			t.Fatal(err)
		}
		if got := reloaded.MinimizeOthers(); got != want {
			t.Errorf("after saving %v, MinimizeOthers() = %v", want, got)
		}
	}
}

// TestNewStoreDefaultsMinimizeOthersOn covers a first run, where nothing has
// been loaded from disk at all.
func TestNewStoreDefaultsMinimizeOthersOn(t *testing.T) {
	if !New(filepath.Join(t.TempDir(), "layouts.json")).MinimizeOthers() {
		t.Error("a fresh store has MinimizeOthers() = false, want true")
	}
}
