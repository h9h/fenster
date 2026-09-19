package main

import (
	"testing"

	"fenster/internal/hotkey"
	"fenster/internal/store"
	"fenster/internal/win32"
)

// fakeHotkeys records what the manager asked Windows to do and can be told
// to refuse a specific combination, standing in for another application
// that already owns it.
type fakeHotkeys struct {
	registered map[int32]uint64 // id -> mods<<32|vk
	registers  int
	unregisters int
	refuse     map[uint64]bool // mods<<32|vk that must fail with ErrHotkeyInUse
}

func newFakeHotkeys() *fakeHotkeys {
	return &fakeHotkeys{registered: map[int32]uint64{}, refuse: map[uint64]bool{}}
}

func combo(mods, vk uint32) uint64 { return uint64(mods)<<32 | uint64(vk) }

func (f *fakeHotkeys) Register(id int32, mods, vk uint32) error {
	f.registers++
	if f.refuse[combo(mods, vk)] {
		return win32.ErrHotkeyInUse
	}
	f.registered[id] = combo(mods, vk)
	return nil
}

func (f *fakeHotkeys) Unregister(id int32) error {
	f.unregisters++
	delete(f.registered, id)
	return nil
}

func hotkeyLayout(id, fingerprint, hk string) store.Layout {
	return store.Layout{ID: id, Name: id, Setup: store.Setup{Fingerprint: fingerprint}, Hotkey: hk}
}

func TestWin32Mods(t *testing.T) {
	got := win32Mods(hotkey.ModCtrl | hotkey.ModAlt | hotkey.ModShift | hotkey.ModWin)
	want := win32.ModControl | win32.ModAlt | win32.ModShift | win32.ModWin
	if got != want {
		t.Errorf("win32Mods(all) = %#x, want %#x", got, want)
	}
	if got := win32Mods(hotkey.ModCtrl); got != win32.ModControl {
		t.Errorf("win32Mods(ModCtrl) = %#x, want %#x", got, win32.ModControl)
	}
	if got := win32Mods(0); got != 0 {
		t.Errorf("win32Mods(0) = %#x, want 0", got)
	}
}

func TestSyncRegistersCurrentSetupOnly(t *testing.T) {
	f := newFakeHotkeys()
	m := newHotkeyManager(f)
	layouts := []store.Layout{
		hotkeyLayout("a", "fp1", "Ctrl+Alt+1"),
		hotkeyLayout("b", "fp2", "Ctrl+Alt+2"),
		hotkeyLayout("c", "fp1", ""),
	}

	if newly := m.sync(layouts, "fp1"); newly != 0 {
		t.Errorf("sync reported %d newly unavailable, want 0", newly)
	}
	if len(f.registered) != 1 {
		t.Fatalf("backend holds %d registrations, want 1", len(f.registered))
	}
	if got, ok := m.layoutFor(1); !ok || got != "a" {
		t.Errorf("layoutFor(1) = (%q, %v), want (\"a\", true)", got, ok)
	}
}

func TestSyncUnregistersEverythingFirst(t *testing.T) {
	f := newFakeHotkeys()
	m := newHotkeyManager(f)
	m.sync([]store.Layout{hotkeyLayout("a", "fp1", "Ctrl+Alt+1")}, "fp1")

	// The display changed: fp1's layouts must go, fp2's must come.
	m.sync([]store.Layout{
		hotkeyLayout("a", "fp1", "Ctrl+Alt+1"),
		hotkeyLayout("b", "fp2", "Ctrl+Alt+2"),
	}, "fp2")

	if f.unregisters != 1 {
		t.Errorf("backend saw %d unregisters, want 1", f.unregisters)
	}
	if len(f.registered) != 1 {
		t.Fatalf("backend holds %d registrations, want 1", len(f.registered))
	}
	if got, ok := m.layoutFor(1); !ok || got != "b" {
		t.Errorf("layoutFor(1) = (%q, %v), want (\"b\", true)", got, ok)
	}
}

func TestSyncSurvivesARefusedRegistration(t *testing.T) {
	f := newFakeHotkeys()
	f.refuse[combo(win32.ModControl|win32.ModAlt, 0x31)] = true // Ctrl+Alt+1
	m := newHotkeyManager(f)
	layouts := []store.Layout{
		hotkeyLayout("a", "fp1", "Ctrl+Alt+1"),
		hotkeyLayout("b", "fp1", "Ctrl+Alt+2"),
	}

	newly := m.sync(layouts, "fp1")
	if newly != 1 {
		t.Errorf("sync reported %d newly unavailable, want 1", newly)
	}
	if len(f.registered) != 1 {
		t.Errorf("backend holds %d registrations, want 1 — one refusal must not cost the other hotkey", len(f.registered))
	}
	if !m.unavailableIDs()["a"] {
		t.Error("layout a should be marked unavailable")
	}
	if m.unavailableIDs()["b"] {
		t.Error("layout b should not be marked unavailable")
	}

	// A second sync with the same refusal is not "newly" unavailable: the
	// user has already been told once.
	if newly := m.sync(layouts, "fp1"); newly != 0 {
		t.Errorf("second sync reported %d newly unavailable, want 0", newly)
	}

	// The other application closed; the next sync picks the hotkey up again.
	delete(f.refuse, combo(win32.ModControl|win32.ModAlt, 0x31))
	if newly := m.sync(layouts, "fp1"); newly != 0 {
		t.Errorf("recovering sync reported %d newly unavailable, want 0", newly)
	}
	if len(m.unavailableIDs()) != 0 {
		t.Errorf("unavailable = %v, want empty after recovery", m.unavailableIDs())
	}
	if len(f.registered) != 2 {
		t.Errorf("backend holds %d registrations, want 2", len(f.registered))
	}
}

func TestSyncWithNoLayoutsClearsEverything(t *testing.T) {
	f := newFakeHotkeys()
	m := newHotkeyManager(f)
	m.sync([]store.Layout{hotkeyLayout("a", "fp1", "Ctrl+Alt+1")}, "fp1")

	m.sync(nil, "")

	if len(f.registered) != 0 {
		t.Errorf("backend holds %d registrations, want 0", len(f.registered))
	}
	if _, ok := m.layoutFor(1); ok {
		t.Error("layoutFor(1) still resolves after a clearing sync")
	}
}

func TestLayoutForUnknownID(t *testing.T) {
	m := newHotkeyManager(newFakeHotkeys())
	if _, ok := m.layoutFor(42); ok {
		t.Error("layoutFor(42) resolved on an empty manager")
	}
}
