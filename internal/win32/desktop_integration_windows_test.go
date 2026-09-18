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
		// The work area must be a plausible sub-rectangle of the monitor: a
		// real desktop always reports a non-zero rcWork, contained within
		// rcMonitor and no larger than it (a taskbar, if any, only ever
		// shrinks the work area relative to the full monitor rectangle).
		if m.Work.W <= 0 || m.Work.H <= 0 {
			t.Errorf("implausible work area %+v for monitor %+v", m.Work, m)
		}
		if m.Work.X < m.X || m.Work.Y < m.Y ||
			m.Work.X+m.Work.W > m.X+m.W || m.Work.Y+m.Work.H > m.Y+m.H {
			t.Errorf("work area %+v not contained within monitor %+v", m.Work, m)
		}
		if m.Work.W > m.W || m.Work.H > m.H {
			t.Errorf("work area %+v larger than monitor %+v", m.Work, m)
		}
	}
	if primaries != 1 {
		t.Errorf("got %d primary monitors, want exactly 1", primaries)
	}
}

// TestEnumWindowsInfoCallsAreIndependent is the regression pin for hoisting
// enumWindowsCallback to a package-level var (see desktop_windows.go): each
// call must see only its own windows, not an accumulation of every previous
// call's windows. Before the fix, a shared, never-reset collector would have
// made later calls report roughly double (or triple) the true window count.
func TestEnumWindowsInfoCallsAreIndependent(t *testing.T) {
	EnableDPIAwareness()

	counts := make([]int, 3)
	for i := range counts {
		windows, err := EnumWindowsInfo()
		if err != nil {
			t.Fatalf("EnumWindowsInfo (call %d): %v", i+1, err)
		}
		if len(windows) == 0 {
			t.Fatalf("EnumWindowsInfo (call %d) returned no windows", i+1)
		}
		counts[i] = len(windows)
	}

	// The live window count can drift by a window or two between calls, but
	// a shared, un-reset collector would make it grow roughly linearly
	// (2x, 3x, ...) across calls. Guard against that without being flaky
	// about small, legitimate fluctuations.
	for i := 1; i < len(counts); i++ {
		if counts[i] >= 2*counts[0] {
			t.Errorf("call %d returned %d windows, call 1 returned %d; "+
				"results look accumulated rather than independent",
				i+1, counts[i], counts[0])
		}
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
	if err := ApplyPlacement(hwnd, want, store.Rect{}, store.StateNormal, false); err != nil {
		t.Fatalf("ApplyPlacement: %v", err)
	}

	got := describeWindow(hwnd)
	if got.Rect != want {
		t.Errorf("rect = %+v, want %+v", got.Rect, want)
	}

	if err := ApplyPlacement(hwnd, want, store.Rect{}, store.StateMaximized, false); err != nil {
		t.Fatalf("ApplyPlacement(maximized): %v", err)
	}
	if got := describeWindow(hwnd); got.State != store.StateMaximized {
		t.Errorf("state = %q, want maximized", got.State)
	}
}

// TestApplyPlacementUsesScreenRectForNormalWindows is the real proof of the
// snapped-window fix: when r (the restored/pre-snap rectangle) and screen
// (the actual on-screen rectangle) deliberately differ, a normal-state
// window must end up on screen at screen, not at r — i.e. ApplyPlacement
// actually uses screen rather than silently falling back to r whenever both
// are present and valid.
//
// It does NOT also assert that rcNormalPosition still reads back as r after
// the call returns, even though that is the persistent split a genuinely
// Windows-Snapped window shows on a live desktop (see the confirmed
// GetWindowRect-vs-rcNormalPosition table in the bug report). A dedicated
// probe against this window (SetWindowPlacement(r) then SetWindowPos(screen),
// and the reverse order, on a freshly created, never-snapped window) showed
// that whichever of the two public calls runs last determines BOTH
// GetWindowRect and GetWindowPlacement's rcNormalPosition — Windows
// resyncs the "normal" rect to match the window's actual bounds for an
// ordinary restored-state window. The persistent divergence the bug report
// measured is maintained by Windows' own Snap engine through some internal
// path that is not reachable via SetWindowPlacement/SetWindowPos, and the
// task explicitly rules out synthesizing Win+arrow to invoke that engine.
// Calling SetWindowPlacement(r) before SetWindowPos(screen) — the order
// used below and in ApplyPlacement — is still the right and only choice
// between the two orders: reversing it would leave the window sitting at r
// instead of screen, undoing the fix entirely (verified with the same
// probe). So immediately after ApplyPlacement returns, rcNormalPosition is
// expected to read back as screen, not r; that is a Windows-enforced
// limitation, not a defect in this function, and is consistent with the
// documented, accepted limitation that a fenster-restored window is not
// treated as snapped by Windows afterwards.
func TestApplyPlacementUsesScreenRectForNormalWindows(t *testing.T) {
	EnableDPIAwareness()

	hwnd, cleanup, err := createTestWindow()
	if err != nil {
		t.Fatalf("createTestWindow: %v", err)
	}
	defer cleanup()

	restored := store.Rect{X: 100, Y: 120, W: 500, H: 400}
	screen := store.Rect{X: 250, Y: 60, W: 900, H: 700}
	if restored == screen {
		t.Fatal("test setup: restored and screen must differ to prove anything")
	}

	if err := ApplyPlacement(hwnd, restored, screen, store.StateNormal, false); err != nil {
		t.Fatalf("ApplyPlacement: %v", err)
	}

	gotScreen, ok := windowRect(hwnd)
	if !ok {
		t.Fatal("windowRect: failed")
	}
	if gotScreen != screen {
		t.Errorf("GetWindowRect = %+v, want the screen rect %+v", gotScreen, screen)
	}

	got := describeWindow(hwnd)
	if got.State != store.StateNormal {
		t.Errorf("state = %q, want normal", got.State)
	}
}

// TestApplyPlacementIgnoresScreenRectWhenMinimizedOrMaximized pins that a
// differing screen rect has no effect on the two states where using it would
// be actively harmful: a minimized window's GetWindowRect legitimately
// reports (-32000,-32000), and a maximized window is positioned by
// SW_SHOWMAXIMIZED itself, not by screen. In both cases rcNormalPosition
// must still come from r, and ApplyPlacement must not attempt to place the
// window at the (deliberately nonsensical) screen rect.
func TestApplyPlacementIgnoresScreenRectWhenMinimizedOrMaximized(t *testing.T) {
	EnableDPIAwareness()

	restored := store.Rect{X: 100, Y: 120, W: 500, H: 400}
	bogusScreen := store.Rect{X: -32000, Y: -32000, W: 10, H: 10}

	t.Run("minimized", func(t *testing.T) {
		hwnd, cleanup, err := createTestWindow()
		if err != nil {
			t.Fatalf("createTestWindow: %v", err)
		}
		defer cleanup()

		if err := ApplyPlacement(hwnd, restored, bogusScreen, store.StateMinimized, false); err != nil {
			t.Fatalf("ApplyPlacement: %v", err)
		}
		got := describeWindow(hwnd)
		if got.State != store.StateMinimized {
			t.Errorf("state = %q, want minimized", got.State)
		}
		if got.Rect != restored {
			t.Errorf("rcNormalPosition (Rect) = %+v, want the restored rect %+v", got.Rect, restored)
		}
	})

	t.Run("maximized", func(t *testing.T) {
		hwnd, cleanup, err := createTestWindow()
		if err != nil {
			t.Fatalf("createTestWindow: %v", err)
		}
		defer cleanup()

		if err := ApplyPlacement(hwnd, restored, bogusScreen, store.StateMaximized, false); err != nil {
			t.Fatalf("ApplyPlacement: %v", err)
		}
		got := describeWindow(hwnd)
		if got.State != store.StateMaximized {
			t.Errorf("state = %q, want maximized", got.State)
		}
		if got.Rect != restored {
			t.Errorf("rcNormalPosition (Rect) = %+v, want the restored rect %+v", got.Rect, restored)
		}
	})
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

	if err := ApplyPlacement(hwnd, want, store.Rect{}, store.StateNormal, false); err != nil {
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
