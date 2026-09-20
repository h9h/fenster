package main

import (
	"errors"
	"log"

	"fenster/internal/hotkey"
	"fenster/internal/store"
	"fenster/internal/win32"
)

// hotkeyProbeID is the registration id used to test a combination the user
// just typed, before anything is written to the store. The manager itself
// allocates ids from 1 upwards, so 0 can never collide with a live
// registration. It is always unregistered again immediately.
const hotkeyProbeID int32 = 0

// hotkeyBackend is the Win32 surface the manager needs, extracted as an
// interface so the whole sync algorithm — id allocation, failure
// bookkeeping, releasing what is gone — is exercised by unit tests with a
// fake, on any machine, without a desktop or a real message window.
type hotkeyBackend interface {
	Register(id int32, mods, vk uint32) error
	Unregister(id int32) error
}

// windowHotkeys is the real backend: registrations owned by the
// application's hidden message window, which is also where WM_HOTKEY
// arrives.
type windowHotkeys struct{ hwnd uintptr }

func (w windowHotkeys) Register(id int32, mods, vk uint32) error {
	return win32.RegisterHotKey(w.hwnd, id, mods, vk)
}

func (w windowHotkeys) Unregister(id int32) error {
	return win32.UnregisterHotKey(w.hwnd, id)
}

// win32Mods translates the hotkey package's platform-free modifier bits
// into the Win32 MOD_* values RegisterHotKey expects. It is the single
// place the two vocabularies meet; the bit values happen to coincide today,
// but relying on that would make internal/hotkey silently
// platform-dependent.
func win32Mods(m hotkey.Mod) uint32 {
	var out uint32
	if m&hotkey.ModAlt != 0 {
		out |= win32.ModAlt
	}
	if m&hotkey.ModCtrl != 0 {
		out |= win32.ModControl
	}
	if m&hotkey.ModShift != 0 {
		out |= win32.ModShift
	}
	if m&hotkey.ModWin != 0 {
		out |= win32.ModWin
	}
	return out
}

// hotkeyManager keeps the set of registered global hotkeys in step with the
// store and the current monitor setup.
type hotkeyManager struct {
	backend     hotkeyBackend
	registered  map[int32]string // registration id -> layout ID, successes only
	unavailable map[string]bool  // layout IDs whose registration is currently refused

	// probeReleasePending records that a probe registration (see probe)
	// could not be released again. It must not be forgotten about: id 0 is
	// never part of registered, so nothing in sync's normal unregister loop
	// would ever retry it, and the combination would stay registered to
	// this process for the rest of its lifetime otherwise.
	probeReleasePending bool
}

func newHotkeyManager(b hotkeyBackend) *hotkeyManager {
	return &hotkeyManager{
		backend:     b,
		registered:  map[int32]string{},
		unavailable: map[string]bool{},
	}
}

// layoutFor resolves the layout a WM_HOTKEY id belongs to.
func (m *hotkeyManager) layoutFor(id int32) (string, bool) {
	layoutID, ok := m.registered[id]
	return layoutID, ok
}

// unavailableIDs is the set of layouts whose hotkey could not be
// registered, for the menu's "(belegt)" marker.
func (m *hotkeyManager) unavailableIDs() map[string]bool { return m.unavailable }

// probe tests whether Windows will grant mods+vk, without persisting
// anything: it registers the combination under hotkeyProbeID and
// immediately unregisters it again. A failure to register is returned
// as-is, so the caller can tell ErrHotkeyInUse (another application owns
// it) apart from any other failure with errors.Is, exactly as sync does.
//
// A failure to *release* the probe is not returned — the probe already
// answered the only question actionHotkey asked it — but it is not
// forgotten either: it is logged and recorded in probeReleasePending, and
// the top of sync retries it on every subsequent sync until it succeeds.
// Without that retry, a failed release here would hold hotkeyProbeID for
// the rest of the process's lifetime, and every later probe of a different
// combination would spuriously see it as still granted or, worse, silently
// clobber its registration.
func (m *hotkeyManager) probe(mods, vk uint32) error {
	if err := m.backend.Register(hotkeyProbeID, mods, vk); err != nil {
		return err
	}
	if err := m.backend.Unregister(hotkeyProbeID); err != nil {
		log.Printf("UnregisterHotKey(probe): %v", err)
		m.probeReleasePending = true
	}
	return nil
}

// sync makes the registered set match the layouts of the given setup: it
// unregisters everything and registers the desired set from scratch. A full
// re-sync rather than an incremental diff because registering a couple of
// dozen hotkeys costs microseconds, whereas a diff would have to carry
// per-id failure bookkeeping across syncs — state that can drift from the
// truth.
//
// Failures are collected, never fatal: one combination taken by another
// application must not cost the user the other nine. The return value is
// how many layouts became unavailable that were not already, so the caller
// can balloon once instead of once per hotkey, and not again on every
// later sync.
func (m *hotkeyManager) sync(layouts []store.Layout, fingerprint string) int {
	if m.probeReleasePending {
		if err := m.backend.Unregister(hotkeyProbeID); err != nil {
			log.Printf("UnregisterHotKey(probe retry): %v", err)
		} else {
			m.probeReleasePending = false
		}
	}

	for id := range m.registered {
		if err := m.backend.Unregister(id); err != nil {
			log.Printf("UnregisterHotKey(%d): %v", id, err)
		}
	}
	m.registered = map[int32]string{}

	bindings, errs := hotkey.Active(layouts, fingerprint)
	for _, err := range errs {
		// These describe the state of layouts.json, not of this sync, and
		// there is nothing a user could act on in a balloon.
		log.Printf("hotkey: %v", err)
	}

	was := m.unavailable
	m.unavailable = map[string]bool{}
	newly := 0

	var id int32 = 1
	for _, b := range bindings {
		err := m.backend.Register(id, win32Mods(b.Hotkey.Mods), b.Hotkey.Key)
		if err == nil {
			m.registered[id] = b.LayoutID
			id++
			continue
		}
		m.unavailable[b.LayoutID] = true
		if !was[b.LayoutID] {
			newly++
		}
		if errors.Is(err, win32.ErrHotkeyInUse) {
			log.Printf("hotkey %s for layout %s is already used by another application", b.Hotkey.Canonical(), b.LayoutID)
		} else {
			log.Printf("registering hotkey %s for layout %s: %v", b.Hotkey.Canonical(), b.LayoutID, err)
		}
	}
	return newly
}

// hotkeyMods is the inverse of win32Mods: it turns the modifier bits a
// captured keystroke carries back into the hotkey package's own vocabulary.
// Written out branch by branch for the same reason win32Mods is — the two
// bit sets coincide today, and depending on that would make internal/hotkey
// quietly platform-dependent.
func hotkeyMods(mods uint32) hotkey.Mod {
	var out hotkey.Mod
	if mods&win32.ModAlt != 0 {
		out |= hotkey.ModAlt
	}
	if mods&win32.ModControl != 0 {
		out |= hotkey.ModCtrl
	}
	if mods&win32.ModShift != 0 {
		out |= hotkey.ModShift
	}
	if mods&win32.ModWin != 0 {
		out |= hotkey.ModWin
	}
	return out
}

// describeCapture tells the capture dialog what to show for the keystroke it
// just saw: the combination in German, and the reason it is not acceptable
// if it is not. It is the only place the dialog's behaviour and the hotkey
// rules meet, which is what keeps internal/win32 free of any opinion about
// what a valid hotkey is.
func describeCapture(mods, vk uint32) (display, problem string) {
	m := hotkeyMods(mods)
	hk, err := hotkey.FromKeys(m, vk)
	if err != nil {
		// Still echo the modifiers being held, so the dialog follows the
		// user's fingers while it explains what is missing.
		return hotkey.ModLabel(m), err.Error()
	}
	return hk.Label(), ""
}
