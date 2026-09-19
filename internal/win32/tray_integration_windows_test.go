//go:build win32integration

package win32

import (
	"fmt"
	"syscall"
	"testing"
	"unsafe"
)

// TestTrayMenuSmoke exercises the message window, tray icon and menu
// plumbing added in Task 9 end-to-end, without ever blocking on human input:
// it deliberately never calls Menu.Track or InputBox, both of which wait for
// a person and stay on the manual checklist instead.
func TestTrayMenuSmoke(t *testing.T) {
	hInstance, _, _ := procGetModuleHandleW.Call(0)

	className := fmt.Sprintf("fensterTrayTest%d", uintptr(unsafe.Pointer(&hInstance)))
	hotkeyIDs := make(chan int32, 1)
	displayChanges := make(chan struct{}, 1)
	mw, err := NewMessageWindow(className, Callbacks{
		TrayClick:        func() {},
		TaskbarRecreated: func() {},
		DisplayChange:    func() { displayChanges <- struct{}{} },
		Hotkey:           func(id int32) { hotkeyIDs <- id },
	})
	if err != nil {
		t.Fatalf("NewMessageWindow: %v", err)
	}
	if mw.Handle() == 0 {
		t.Fatal("MessageWindow.Handle() returned 0")
	}

	icon := StockIcon()
	if icon == 0 {
		t.Fatal("StockIcon() returned 0")
	}

	tray, err := NewTrayIcon(mw.Handle(), icon, "fenster smoke test")
	if err != nil {
		t.Fatalf("NewTrayIcon: %v", err)
	}
	if err := tray.Balloon("fenster", "smoke test balloon"); err != nil {
		t.Fatalf("Balloon: %v", err)
	}
	if err := tray.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	sub := NewMenu()
	if sub.handle == 0 {
		t.Fatal("submenu handle is 0")
	}
	sub.AddItem(100, "Sub item", false, false)
	if count, _, _ := procGetMenuItemCount.Call(sub.handle); int32(count) != 1 {
		t.Fatalf("submenu item count = %d, want 1", int32(count))
	}

	menu := NewMenu()
	if menu.handle == 0 {
		t.Fatal("menu handle is 0")
	}
	menu.AddItem(1, "Checked item", true, false)
	menu.AddItem(2, "Greyed item", false, true)
	menu.AddItem(3, "Plain item", false, false)
	menu.AddItem(4, "Tom & Jerry", false, false)
	menu.AddSeparator()
	menu.AddSubmenu("Submenu", sub, false)

	if count, _, _ := procGetMenuItemCount.Call(menu.handle); int32(count) != 6 {
		t.Fatalf("menu item count = %d, want 6 (item, item, item, item, separator, submenu)", int32(count))
	}
	if len(menu.subs) != 1 || menu.subs[0] != sub {
		t.Fatalf("menu.subs = %v, want [sub]", menu.subs)
	}

	// Regression coverage for AddItem's checked/greyed flag composition:
	// GetMenuState surfaces the actual MF_CHECKED/MF_GRAYED bits Windows
	// recorded for each item, so this fails if AddItem ever swaps or drops
	// either flag. A plain item with neither flag is included so the test
	// also catches a bug that spuriously sets state on unrelated items.
	menuState := func(id uint32) int32 {
		state, _, _ := procGetMenuState.Call(menu.handle, uintptr(id), uintptr(mfByCommand))
		if int32(state) == -1 {
			t.Fatalf("GetMenuState(id=%d): item not found", id)
		}
		return int32(state)
	}

	checkedState := menuState(1)
	if checkedState&mfChecked == 0 {
		t.Errorf("item 1 (checked): MF_CHECKED not set, state = 0x%x", checkedState)
	}
	if checkedState&mfGrayed != 0 {
		t.Errorf("item 1 (checked): MF_GRAYED unexpectedly set, state = 0x%x", checkedState)
	}

	greyedState := menuState(2)
	if greyedState&mfGrayed == 0 {
		t.Errorf("item 2 (greyed): MF_GRAYED not set, state = 0x%x", greyedState)
	}
	if greyedState&mfChecked != 0 {
		t.Errorf("item 2 (greyed): MF_CHECKED unexpectedly set, state = 0x%x", greyedState)
	}

	plainState := menuState(3)
	if plainState&(mfChecked|mfGrayed) != 0 {
		t.Errorf("item 3 (plain): expected neither MF_CHECKED nor MF_GRAYED, state = 0x%x", plainState)
	}

	// Regression coverage for AddItem escaping "&": a label like "Tom &
	// Jerry" must render with a literal ampersand, not be interpreted by
	// Win32 as marking the following character a keyboard accelerator
	// (which would render as "Tom Jerry" with a stray underline).
	if got, want := menuItemText(t, menu.handle, 4), "Tom && Jerry"; got != want {
		t.Errorf("item 4 label = %q, want %q (ampersand not escaped)", got, want)
	}

	menu.Destroy()

	classPtr, err := syscall.UTF16PtrFromString(className)
	if err != nil {
		t.Fatalf("class name: %v", err)
	}
	if ret, _, err := procDestroyWindow.Call(mw.Handle()); ret == 0 {
		t.Fatalf("DestroyWindow: %v", err)
	}
	if ret, _, err := procUnregisterClassW.Call(uintptr(unsafe.Pointer(classPtr)), hInstance); ret == 0 {
		t.Fatalf("UnregisterClassW: %v", err)
	}
}

// menuItemText reads back the raw label GetMenuStringW has stored for id,
// exactly as AppendMenuW recorded it (Win32 only interprets a lone "&" as an
// accelerator marker when painting the menu, not in what GetMenuStringW
// returns), so this is how the test asserts that AddItem actually doubled
// every "&" before handing the label to AppendMenuW.
func menuItemText(t *testing.T, hmenu uintptr, id uint32) string {
	t.Helper()
	buf := make([]uint16, 256)
	n, _, err := procGetMenuStringW.Call(hmenu, uintptr(id), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(mfByCommand))
	if n == 0 {
		t.Fatalf("GetMenuStringW(id=%d): %v", id, err)
	}
	return syscall.UTF16ToString(buf[:n])
}

// TestMessageWindowRoutesHotkeyAndDisplayChange sends the two new messages
// to the window procedure directly, rather than waiting for a real key
// press or a real monitor being unplugged, and asserts they reach the
// right callback with the right payload.
func TestMessageWindowRoutesHotkeyAndDisplayChange(t *testing.T) {
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className := fmt.Sprintf("fensterRouteTest%d", uintptr(unsafe.Pointer(&hInstance)))

	gotHotkey := make(chan int32, 1)
	gotDisplay := make(chan struct{}, 1)
	mw, err := NewMessageWindow(className, Callbacks{
		DisplayChange: func() { gotDisplay <- struct{}{} },
		Hotkey:        func(id int32) { gotHotkey <- id },
	})
	if err != nil {
		t.Fatalf("NewMessageWindow: %v", err)
	}
	defer mw.Quit()

	// SendMessageW dispatches synchronously to the window procedure on this
	// thread, so no message loop has to be running for this to arrive.
	procSendMessageW.Call(mw.Handle(), uintptr(wmHotkey), 7, 0)
	select {
	case id := <-gotHotkey:
		if id != 7 {
			t.Errorf("Hotkey callback got id %d, want 7", id)
		}
	default:
		t.Error("Hotkey callback was not invoked")
	}

	procSendMessageW.Call(mw.Handle(), uintptr(wmDisplayChange), 0, 0)
	select {
	case <-gotDisplay:
	default:
		t.Error("DisplayChange callback was not invoked")
	}
}
