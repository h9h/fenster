package main

import (
	"errors"
	"testing"

	"fenster/internal/hotkey"
	"fenster/internal/store"
	"fenster/internal/win32"
)

// fakeHotkeys records what the manager asked Windows to do and can be told
// to refuse a specific combination, standing in for another application
// that already owns it, or to fail the next Unregister of a given id,
// standing in for the rare case where UnregisterHotKey itself errors (wrong
// id or wrong thread).
type fakeHotkeys struct {
	registered  map[int32]uint64 // id -> mods<<32|vk
	registers   int
	unregisters int
	refuse      map[uint64]bool // mods<<32|vk that must fail with ErrHotkeyInUse
	failRelease map[int32]bool  // id -> Unregister(id) fails once, then clears itself
}

func newFakeHotkeys() *fakeHotkeys {
	return &fakeHotkeys{
		registered:  map[int32]uint64{},
		refuse:      map[uint64]bool{},
		failRelease: map[int32]bool{},
	}
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
	if f.failRelease[id] {
		delete(f.failRelease, id)
		return errors.New("simulated UnregisterHotKey failure")
	}
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

func TestProbeGrantsAndReleases(t *testing.T) {
	f := newFakeHotkeys()
	m := newHotkeyManager(f)

	if err := m.probe(win32.ModControl|win32.ModAlt, 0x31); err != nil {
		t.Fatalf("probe: unexpected error %v", err)
	}
	if m.probeReleasePending {
		t.Error("probeReleasePending set after a clean probe")
	}
	if _, held := f.registered[hotkeyProbeID]; held {
		t.Error("backend still holds the probe registration after a clean probe")
	}
}

func TestProbeReportsRefusal(t *testing.T) {
	f := newFakeHotkeys()
	f.refuse[combo(win32.ModControl|win32.ModAlt, 0x31)] = true
	m := newHotkeyManager(f)

	err := m.probe(win32.ModControl|win32.ModAlt, 0x31)
	if !errors.Is(err, win32.ErrHotkeyInUse) {
		t.Errorf("probe error = %v, want ErrHotkeyInUse", err)
	}
}

// TestProbeRetriesFailedRelease covers the log-and-continue path for a
// failed Unregister, which previously had no test at all: if the release
// of a probe registration fails, the combination must not be leaked for
// the rest of the process's life. The manager must retry the release on
// the next sync instead. Without the fix (probe releasing directly via
// win32.RegisterHotKey/UnregisterHotKey in actionHotkey, with no retry
// bookkeeping), there is no probeReleasePending field or retry to observe
// here — this test would not even compile against that version, and a
// hand-written equivalent against it would show the probe registration
// still held after sync, forever.
func TestProbeRetriesFailedRelease(t *testing.T) {
	f := newFakeHotkeys()
	m := newHotkeyManager(f)

	f.failRelease[hotkeyProbeID] = true
	if err := m.probe(win32.ModControl|win32.ModAlt, 0x31); err != nil {
		t.Fatalf("probe: unexpected error %v", err)
	}
	if !m.probeReleasePending {
		t.Fatal("probeReleasePending not set after a failed Unregister")
	}
	if _, held := f.registered[hotkeyProbeID]; !held {
		t.Fatal("backend should still hold the probe registration after a failed Unregister")
	}

	// A sync with nothing to register still must retry the pending release.
	m.sync(nil, "")

	if m.probeReleasePending {
		t.Error("sync did not clear the pending probe release once it succeeded")
	}
	if _, held := f.registered[hotkeyProbeID]; held {
		t.Error("sync did not actually release the probe registration")
	}
}

// TestHotkeyModsRoundTrip pins the two translations against each other. They
// are written out branch by branch on purpose, which is exactly the shape of
// code where a single mistyped pair goes unnoticed.
func TestHotkeyModsRoundTrip(t *testing.T) {
	all := hotkey.ModAlt | hotkey.ModCtrl | hotkey.ModShift | hotkey.ModWin
	for m := hotkey.Mod(0); m <= all; m++ {
		if m&^all != 0 {
			continue
		}
		if got := hotkeyMods(win32Mods(m)); got != m {
			t.Errorf("hotkeyMods(win32Mods(%#x)) = %#x, want %#x", m, got, m)
		}
	}
}

func TestDescribeCaptureAcceptsAValidCombination(t *testing.T) {
	display, problem := describeCapture(win32.ModControl|win32.ModShift, 0x31)
	if problem != "" {
		t.Errorf("problem = %q, want empty", problem)
	}
	if want := "Strg+Umschalt+1"; display != want {
		t.Errorf("display = %q, want %q", display, want)
	}
}

// TestDescribeCaptureRejectsAKeyWithNoSignal is the regression guard for the
// bug that prompted the capture dialog: a Mac keyboard's Option+1 arrives as
// vk 0xFF with no Alt modifier, and used to be storable as a hotkey that
// could never fire.
func TestDescribeCaptureRejectsAKeyWithNoSignal(t *testing.T) {
	display, problem := describeCapture(win32.ModControl, 0xFF)
	if problem == "" {
		t.Fatal("problem is empty; a key with no virtual-key code must be refused")
	}
	if want := "Strg"; display != want {
		t.Errorf("display = %q, want %q — the echo must still follow the held modifiers", display, want)
	}
}

func TestDescribeCaptureEchoesModifiersWhileIncomplete(t *testing.T) {
	display, problem := describeCapture(win32.ModControl|win32.ModAlt, 0)
	if problem == "" {
		t.Error("holding only modifiers must not count as a complete combination")
	}
	if want := "Strg+Alt"; display != want {
		t.Errorf("display = %q, want %q", display, want)
	}
}

func TestDescribeCaptureRejectsAModifierlessKey(t *testing.T) {
	if _, problem := describeCapture(0, 0x31); problem == "" {
		t.Error("a key with no modifier must be refused")
	}
}
