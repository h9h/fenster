package hotkey

import (
	"testing"

	"fenster/internal/store"
)

func layoutWith(id, name, fingerprint, hk string) store.Layout {
	return store.Layout{
		ID:     id,
		Name:   name,
		Setup:  store.Setup{Fingerprint: fingerprint},
		Hotkey: hk,
	}
}

func TestActiveSelectsCurrentSetupInStoreOrder(t *testing.T) {
	layouts := []store.Layout{
		layoutWith("a", "Dock", "fp1", "Ctrl+Alt+1"),
		layoutWith("b", "Laptop", "fp2", "Ctrl+Alt+1"), // other setup, same combination
		layoutWith("c", "Besprechung", "fp1", ""),      // no hotkey
		layoutWith("d", "Zweitmonitor", "fp1", "Ctrl+Alt+2"),
	}

	got, errs := Active(layouts, "fp1")
	if len(errs) != 0 {
		t.Fatalf("Active returned errors: %v", errs)
	}
	want := []Binding{
		{LayoutID: "a", Hotkey: Hotkey{Mods: ModCtrl | ModAlt, Key: 0x31}},
		{LayoutID: "d", Hotkey: Hotkey{Mods: ModCtrl | ModAlt, Key: 0x32}},
	}
	if len(got) != len(want) {
		t.Fatalf("Active returned %d bindings (%+v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("binding %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestActiveSkipsUnparseableAndReportsIt(t *testing.T) {
	layouts := []store.Layout{
		layoutWith("a", "Kaputt", "fp1", "Strg+Zwiebel"),
		layoutWith("b", "Heil", "fp1", "Ctrl+Alt+1"),
	}

	got, errs := Active(layouts, "fp1")
	if len(got) != 1 || got[0].LayoutID != "b" {
		t.Fatalf("Active = %+v, want only layout b", got)
	}
	if len(errs) != 1 {
		t.Fatalf("Active returned %d errors, want 1", len(errs))
	}
}

func TestActiveKeepsFirstOfDuplicateCombinations(t *testing.T) {
	layouts := []store.Layout{
		layoutWith("a", "Erste", "fp1", "Ctrl+Alt+1"),
		layoutWith("b", "Zweite", "fp1", "Alt+Ctrl+1"), // same combination, other spelling
	}

	got, errs := Active(layouts, "fp1")
	if len(got) != 1 || got[0].LayoutID != "a" {
		t.Fatalf("Active = %+v, want only layout a", got)
	}
	if len(errs) != 1 {
		t.Fatalf("Active returned %d errors, want 1 naming the duplicate", len(errs))
	}
}

func TestConflict(t *testing.T) {
	layouts := []store.Layout{
		layoutWith("a", "Dock", "fp1", "Ctrl+Alt+1"),
		layoutWith("b", "Laptop", "fp2", "Ctrl+Alt+2"),
	}
	hk := Hotkey{Mods: ModCtrl | ModAlt, Key: 0x31}

	if other, ok := Conflict(layouts, "fp1", hk, ""); !ok || other.ID != "a" {
		t.Errorf("Conflict in the same setup = (%+v, %v), want layout a", other, ok)
	}
	if _, ok := Conflict(layouts, "fp1", hk, "a"); ok {
		t.Error("Conflict reported a layout against itself; exceptID should exclude it")
	}
	if _, ok := Conflict(layouts, "fp2", hk, ""); ok {
		t.Error("Conflict reported across setups; a combination may repeat in another setup")
	}
	if _, ok := Conflict(layouts, "fp1", Hotkey{}, ""); ok {
		t.Error("the zero Hotkey conflicts with nothing")
	}
}
