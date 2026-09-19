//go:build win32integration

package win32

import (
	"errors"
	"fmt"
	"testing"
	"unsafe"
)

// TestRegisterHotKeySmoke registers a real global hotkey on a real message
// window and asserts the three behaviours the manager depends on:
// registration succeeds, a second registration of the same combination is
// reported as ErrHotkeyInUse rather than a generic failure, and
// unregistering frees the combination again.
//
// Ctrl+Alt+Shift+Win+F24 is used deliberately: nothing binds it, so the
// test does not fight the user's own shortcuts or another running
// application for it.
func TestRegisterHotKeySmoke(t *testing.T) {
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className := fmt.Sprintf("fensterHotkeyTest%d", uintptr(unsafe.Pointer(&hInstance)))

	mw, err := NewMessageWindow(className, func() {}, func() {})
	if err != nil {
		t.Fatalf("NewMessageWindow: %v", err)
	}
	defer mw.Quit()

	const id int32 = 1
	mods := ModControl | ModAlt | ModShift | ModWin
	const vkF24 = 0x87

	if err := RegisterHotKey(mw.Handle(), id, mods, vkF24); err != nil {
		t.Fatalf("RegisterHotKey: %v", err)
	}

	if err := RegisterHotKey(mw.Handle(), id+1, mods, vkF24); !errors.Is(err, ErrHotkeyInUse) {
		t.Errorf("second RegisterHotKey = %v, want ErrHotkeyInUse", err)
	}

	if err := UnregisterHotKey(mw.Handle(), id); err != nil {
		t.Fatalf("UnregisterHotKey: %v", err)
	}

	if err := RegisterHotKey(mw.Handle(), id, mods, vkF24); err != nil {
		t.Fatalf("re-registering after unregister: %v", err)
	}
	if err := UnregisterHotKey(mw.Handle(), id); err != nil {
		t.Fatalf("final UnregisterHotKey: %v", err)
	}
}
