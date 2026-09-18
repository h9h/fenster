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
