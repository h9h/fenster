//go:build win32integration

package win32

import (
	"fmt"
	"syscall"
	"testing"
	"unsafe"

	"fenster/internal/store"
)

// TestEnumerationFindsOwnConsoleWindowAndMonitors is a smoke test: it proves
// the syscall wrappers return plausible data on a real desktop.
func TestEnumerationFindsWindowsAndMonitors(t *testing.T) {
	EnableDPIAwareness()

	windows, err := EnumWindowsInfo()
	if err != nil {
		t.Fatalf("EnumWindowsInfo: %v", err)
	}
	titled := 0
	for _, w := range windows {
		if w.Visible && w.Title != "" && w.Exe != "" {
			titled++
		}
	}
	if titled == 0 {
		t.Errorf("no visible titled window with an executable path found")
	}

	monitors, err := EnumMonitors()
	if err != nil {
		t.Fatalf("EnumMonitors: %v", err)
	}
	if len(monitors) == 0 {
		t.Fatal("no monitors reported")
	}
	primaries := 0
	for _, m := range monitors {
		if m.W <= 0 || m.H <= 0 {
			t.Errorf("implausible monitor %+v", m)
		}
		if m.Scale < 100 || m.Scale > 400 {
			t.Errorf("implausible scale %+v", m)
		}
		if m.Primary {
			primaries++
		}
	}
	if primaries != 1 {
		t.Errorf("got %d primary monitors, want exactly 1", primaries)
	}
}

// TestApplyPlacementMovesARealWindow creates a window, moves it, and reads the
// geometry back.
func TestApplyPlacementMovesARealWindow(t *testing.T) {
	EnableDPIAwareness()

	hwnd, cleanup, err := createTestWindow()
	if err != nil {
		t.Fatalf("createTestWindow: %v", err)
	}
	defer cleanup()

	want := store.Rect{X: 120, Y: 140, W: 640, H: 480}
	if err := ApplyPlacement(hwnd, want, store.StateNormal, false); err != nil {
		t.Fatalf("ApplyPlacement: %v", err)
	}

	got := describeWindow(hwnd)
	if got.Rect != want {
		t.Errorf("rect = %+v, want %+v", got.Rect, want)
	}

	if err := ApplyPlacement(hwnd, want, store.StateMaximized, false); err != nil {
		t.Fatalf("ApplyPlacement(maximized): %v", err)
	}
	if got := describeWindow(hwnd); got.State != store.StateMaximized {
		t.Errorf("state = %q, want maximized", got.State)
	}
}

// TestApplyPlacementHandlesNegativeCoordinates places a window at a negative
// X/Y, which is the normal case for a monitor left of or above the primary
// monitor in a multi-monitor setup. This exercises the explicit
// uintptr(int32(...)) sign extension in ApplyPlacement's SetWindowPos call.
//
// The test requires the current desktop to actually have such a monitor; if
// it does not, the test fails loudly rather than silently skipping, per the
// task instructions.
func TestApplyPlacementHandlesNegativeCoordinates(t *testing.T) {
	EnableDPIAwareness()

	monitors, err := EnumMonitors()
	if err != nil {
		t.Fatalf("EnumMonitors: %v", err)
	}

	var target *store.Monitor
	for i := range monitors {
		m := monitors[i]
		if m.X < 0 || m.Y < 0 {
			target = &m
			break
		}
	}
	if target == nil {
		t.Fatal("no monitor left of or above the primary was found on this desktop; " +
			"the negative-coordinate path cannot be exercised here")
	}

	hwnd, cleanup, err := createTestWindow()
	if err != nil {
		t.Fatalf("createTestWindow: %v", err)
	}
	defer cleanup()

	want := store.Rect{X: target.X + 20, Y: target.Y + 20, W: 640, H: 480}
	if want.X >= 0 && want.Y >= 0 {
		t.Fatalf("test setup produced a non-negative rect %+v from monitor %+v", want, target)
	}

	if err := ApplyPlacement(hwnd, want, store.StateNormal, false); err != nil {
		t.Fatalf("ApplyPlacement: %v", err)
	}

	got := describeWindow(hwnd)
	if got.Rect != want {
		t.Errorf("rect = %+v, want %+v", got.Rect, want)
	}
}

// createTestWindow registers a throwaway window class and creates a visible
// WS_OVERLAPPEDWINDOW for the integration tests to manipulate. The returned
// cleanup destroys the window and unregisters the class.
func createTestWindow() (uintptr, func(), error) {
	hInstance, _, _ := procGetModuleHandleW.Call(0)

	className, err := syscall.UTF16PtrFromString(fmt.Sprintf("fensterTestWindow%d", uintptr(unsafe.Pointer(&hInstance))))
	if err != nil {
		return 0, nil, fmt.Errorf("class name: %w", err)
	}

	wndProc := syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
		ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return ret
	})

	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   wndProc,
		HInstance:     hInstance,
		LpszClassName: className,
	}
	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return 0, nil, fmt.Errorf("RegisterClassExW: %w", err)
	}

	titlePtr, err := syscall.UTF16PtrFromString("fenster integration test window")
	if err != nil {
		return 0, nil, fmt.Errorf("title: %w", err)
	}

	// cwUseDefault is a negative constant. Go disallows converting a negative
	// *constant* to an unsigned type at compile time, so it is routed through
	// a plain int32 variable first, whose conversion to uintptr sign-extends
	// at run time instead.
	cwUseDefaultI32 := int32(cwUseDefault)
	useDefault := uintptr(cwUseDefaultI32)
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(wsOverlappedWindow|wsVisible),
		useDefault, useDefault,
		useDefault, useDefault,
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), hInstance)
		return 0, nil, fmt.Errorf("CreateWindowExW: %w", err)
	}

	cleanup := func() {
		procDestroyWindow.Call(hwnd)
		procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), hInstance)
	}
	return hwnd, cleanup, nil
}
