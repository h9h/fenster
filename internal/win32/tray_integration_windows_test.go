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
	mw, err := NewMessageWindow(className, func() {})
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
	menu.AddSeparator()
	menu.AddSubmenu("Submenu", sub, false)

	if count, _, _ := procGetMenuItemCount.Call(menu.handle); int32(count) != 4 {
		t.Fatalf("menu item count = %d, want 4 (item, item, separator, submenu)", int32(count))
	}
	if len(menu.subs) != 1 || menu.subs[0] != sub {
		t.Fatalf("menu.subs = %v, want [sub]", menu.subs)
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
