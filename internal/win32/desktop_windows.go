package win32

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"fenster/internal/store"
)

// WindowInfo is the raw description of one top-level window.
type WindowInfo struct {
	Handle  uintptr
	Title   string
	Class   string
	Exe     string
	Rect    store.Rect
	Screen  store.Rect // actual on-screen rectangle from GetWindowRect; see store.WindowEntry.Screen
	State   store.WindowState
	Topmost bool
	Visible bool
	Cloaked bool
	Tool    bool
	PID     uint32
}

// EnableDPIAwareness declares the process per-monitor DPI aware (v2) so that
// all coordinates are physical pixels. Failure is not fatal: on an older
// Windows the process simply stays DPI-virtualized.
func EnableDPIAwareness() {
	if procSetProcessDpiAwarenessCtx.Find() == nil {
		procSetProcessDpiAwarenessCtx.Call(dpiAwarenessContextPerMonitorAwareV2)
	}
}

// CurrentPID returns the process id of this process.
func CurrentPID() uint32 {
	pid, _, _ := procGetCurrentProcessId.Call()
	return uint32(pid)
}

// enumWindowsMu serializes EnumWindowsInfo calls and guards
// enumWindowsCollector. fenster is a single-threaded UI application (tray
// icon + popup menus), so this never contends in practice; the mutex exists
// to make that assumption explicit and safe if it ever stops holding.
var (
	enumWindowsMu        sync.Mutex
	enumWindowsCollector *[]WindowInfo
)

// enumWindowsCallback is created exactly once for the lifetime of the
// process. syscall.NewCallback allocates from a small, fixed-size, never-freed
// table (a few thousand slots); a tray application that called EnumWindows on
// every menu open (as this one does, via Task 10) would exhaust that table
// and panic after enough uptime if a fresh callback were minted per call.
// Per-call state is instead reached through the package-level
// enumWindowsCollector, guarded by enumWindowsMu, rather than by threading a
// pointer through EnumWindows' lParam and converting it back with
// unsafe.Pointer(uintptr(...)) — that reverse conversion is exactly the
// pattern go vet's unsafeptr check flags as a possible misuse, since it
// cannot verify from the call site that the uintptr is still a valid,
// GC-tracked pointer.
var enumWindowsCallback = syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
	*enumWindowsCollector = append(*enumWindowsCollector, describeWindow(hwnd))
	return 1
})

// EnumWindowsInfo describes every top-level window, without filtering. The
// caller decides what is eligible (see internal/layout). Calls are
// serialized by enumWindowsMu, so repeated or concurrent calls never see
// each other's windows.
func EnumWindowsInfo() ([]WindowInfo, error) {
	enumWindowsMu.Lock()
	defer enumWindowsMu.Unlock()

	var windows []WindowInfo
	enumWindowsCollector = &windows
	defer func() { enumWindowsCollector = nil }()

	if ret, _, err := procEnumWindows.Call(enumWindowsCallback, 0); ret == 0 {
		return nil, fmt.Errorf("EnumWindows: %w", err)
	}
	return windows, nil
}

func describeWindow(hwnd uintptr) WindowInfo {
	info := WindowInfo{Handle: hwnd, Title: windowText(hwnd), Class: className(hwnd)}

	visible, _, _ := procIsWindowVisible.Call(hwnd)
	info.Visible = visible != 0

	// gwlExStyle is a negative constant (GWL_EXSTYLE == -20). Go disallows
	// converting a negative *constant* to an unsigned type at compile time, so
	// it is routed through a plain int32 variable first, whose conversion to
	// uintptr sign-extends at run time instead.
	exStyleIndex := int32(gwlExStyle)
	exStyle, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(exStyleIndex))
	info.Tool = exStyle&wsExToolWindow != 0
	info.Topmost = exStyle&wsExTopmost != 0

	var cloaked uint32
	procDwmGetWindowAttribute.Call(hwnd, dwmwaCloaked, uintptr(unsafe.Pointer(&cloaked)), unsafe.Sizeof(cloaked))
	info.Cloaked = cloaked != 0

	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	info.PID = pid
	info.Exe = processPath(pid)

	// Default to a normal window before the placement lookup so that a
	// failure below can never leave info.State as the empty string, which is
	// not one of store.WindowState's three valid values.
	info.State = store.StateNormal

	// windowRect (GetWindowRect) is read once and reused both as info.Screen
	// and as the GetWindowPlacement fallback below, rather than calling it
	// twice.
	screenRect, screenOK := windowRect(hwnd)
	if screenOK {
		info.Screen = screenRect
	}

	wp := windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{}))}
	if ret, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp))); ret != 0 {
		r := wp.RcNormalPosition
		info.Rect = store.Rect{X: r.Left, Y: r.Top, W: r.Right - r.Left, H: r.Bottom - r.Top}
		switch wp.ShowCmd {
		case swShowMinimized, swShowMinNoActive:
			info.State = store.StateMinimized
		case swShowMaximized:
			info.State = store.StateMaximized
		default:
			info.State = store.StateNormal
		}
	} else if screenOK {
		// GetWindowPlacement failed; fall back to the current screen
		// rectangle so the window still gets a usable, non-zero rect. The
		// state stays store.StateNormal, set above.
		info.Rect = screenRect
	}
	// If both calls failed, info.Rect and info.Screen stay their zero value
	// (W == H == 0). internal/layout.Eligible rejects windows with a
	// non-positive Rect width or height, so such a window is dropped rather
	// than saved with a bogus rectangle; a zero Screen alone is legitimate
	// and treated as "absent" by ApplyPlacement.
	return info
}

// windowRect reads a window's current screen rectangle via GetWindowRect. It
// is the fallback used when GetWindowPlacement fails.
func windowRect(hwnd uintptr) (store.Rect, bool) {
	var r rect
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ret == 0 {
		return store.Rect{}, false
	}
	return store.Rect{X: r.Left, Y: r.Top, W: r.Right - r.Left, H: r.Bottom - r.Top}, true
}

func windowText(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if length == 0 {
		return ""
	}
	buf := make([]uint16, length+1)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func className(hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func processPath(pid uint32) string {
	if pid == 0 {
		return ""
	}
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)

	buf := make([]uint16, syscall.MAX_LONG_PATH)
	size := uint32(len(buf))
	ret, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ret == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

// enumMonitorsMu serializes EnumMonitors calls and guards
// enumMonitorsCollector, for the same single-threaded-in-practice reason as
// enumWindowsMu.
var (
	enumMonitorsMu        sync.Mutex
	enumMonitorsCollector *[]store.Monitor
)

// enumMonitorsCallback is created exactly once for the lifetime of the
// process, for the same reason as enumWindowsCallback: syscall.NewCallback's
// backing table is small and never freed, and Task 10 calls EnumMonitors on
// every tray-menu open. Per-call state is reached through the package-level
// enumMonitorsCollector, guarded by enumMonitorsMu, rather than through
// EnumDisplayMonitors' dwData parameter — see enumWindowsCallback's comment
// for why the dwData/lParam route is avoided (it requires converting a
// uintptr back to unsafe.Pointer, which go vet's unsafeptr check flags).
var enumMonitorsCallback = syscall.NewCallback(func(hmon, hdc, lprc, data uintptr) uintptr {
	mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ret, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); ret == 0 {
		return 1
	}
	*enumMonitorsCollector = append(*enumMonitorsCollector, store.Monitor{
		X:       mi.RcMonitor.Left,
		Y:       mi.RcMonitor.Top,
		W:       mi.RcMonitor.Right - mi.RcMonitor.Left,
		H:       mi.RcMonitor.Bottom - mi.RcMonitor.Top,
		Scale:   monitorScale(hmon),
		Primary: mi.DwFlags&1 != 0,
		Work: store.Rect{
			X: mi.RcWork.Left,
			Y: mi.RcWork.Top,
			W: mi.RcWork.Right - mi.RcWork.Left,
			H: mi.RcWork.Bottom - mi.RcWork.Top,
		},
	})
	return 1
})

// EnumMonitors returns the current monitor arrangement in physical pixels,
// including each monitor's work area (see store.Monitor.Work). Calls are
// serialized by enumMonitorsMu, so repeated or concurrent calls never see
// each other's monitors.
func EnumMonitors() ([]store.Monitor, error) {
	enumMonitorsMu.Lock()
	defer enumMonitorsMu.Unlock()

	var monitors []store.Monitor
	enumMonitorsCollector = &monitors
	defer func() { enumMonitorsCollector = nil }()

	if ret, _, err := procEnumDisplayMonitors.Call(0, 0, enumMonitorsCallback, 0); ret == 0 {
		return nil, fmt.Errorf("EnumDisplayMonitors: %w", err)
	}
	return monitors, nil
}

func monitorScale(hmon uintptr) int {
	var dpiX, dpiY uint32
	if procGetDpiForMonitor.Find() != nil {
		return 100
	}
	ret, _, _ := procGetDpiForMonitor.Call(hmon, mdtEffectiveDPI, uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY)))
	if ret != 0 || dpiX == 0 {
		return 100
	}
	return int(dpiX) * 100 / 96
}

// ApplyPlacement moves a window to r and applies its show state and topmost
// flag. The restored rectangle is set first so that a maximized window is
// maximized on the monitor it was saved on, and so that un-maximizing later
// yields the saved geometry.
//
// screen is the window's actual on-screen rectangle at save time (see
// store.WindowEntry.Screen). It is only used for a normal-state window, and
// only when present and valid (W>0 and H>0): that is the fix for a window
// that was snapped via Windows Snap, where r (rcNormalPosition) is
// deliberately the pre-snap rectangle Windows keeps around so the user can
// drag the window back out, while screen is where it actually sits.
// Minimized and maximized windows ignore screen entirely — a minimized
// window's GetWindowRect reports (-32000,-32000), which must never be used
// to position anything.
func ApplyPlacement(handle uintptr, r store.Rect, screen store.Rect, state store.WindowState, topmost bool) error {
	wp := windowPlacement{
		Length:           uint32(unsafe.Sizeof(windowPlacement{})),
		RcNormalPosition: rect{Left: r.X, Top: r.Y, Right: r.X + r.W, Bottom: r.Y + r.H},
	}
	switch state {
	case store.StateMinimized:
		wp.ShowCmd = swShowMinNoActive
	case store.StateMaximized:
		wp.ShowCmd = swShowMaximized
	default:
		wp.ShowCmd = swShowNoActivate
	}
	if ret, _, err := procSetWindowPlacement.Call(handle, uintptr(unsafe.Pointer(&wp))); ret == 0 {
		return fmt.Errorf("SetWindowPlacement: %w", err)
	}

	// SetWindowPlacement works in workspace coordinates, which differ from
	// screen coordinates when the primary monitor has a taskbar. For a normal
	// window, correct the position afterwards with screen coordinates: use
	// the actual on-screen rectangle when it is present and valid, falling
	// back to r for layouts saved before Screen existed or for a window that
	// reported no usable GetWindowRect at save time.
	//
	// Coordinates are passed through uintptr(int32(...)) so that negative
	// values (windows on a monitor left of or above the primary) sign-extend
	// correctly instead of appearing as huge unsigned numbers.
	if state == store.StateNormal {
		pos := r
		if screen.W > 0 && screen.H > 0 {
			pos = screen
		}
		flags := uintptr(swpNoZOrder | swpNoActivate)
		if ret, _, err := procSetWindowPos.Call(handle, 0, uintptr(int32(pos.X)), uintptr(int32(pos.Y)), uintptr(int32(pos.W)), uintptr(int32(pos.H)), flags); ret == 0 {
			return fmt.Errorf("SetWindowPos: %w", err)
		}
	}

	insertAfter := hwndNoTopmost
	if topmost {
		insertAfter = hwndTopmost
	}
	if ret, _, err := procSetWindowPos.Call(handle, insertAfter, 0, 0, 0, 0, uintptr(swpNoMove|swpNoSize|swpNoActivate)); ret == 0 {
		return fmt.Errorf("SetWindowPos(topmost): %w", err)
	}
	return nil
}

// MinimizeWindow minimizes hwnd without activating it and without touching
// its restored rectangle.
//
// SW_SHOWMINNOACTIVE rather than SW_MINIMIZE because a restore minimizes a
// whole set of extraneous windows in a row: activating each one in turn
// would hand focus around the desktop and leave it wherever the last
// minimize landed, instead of on the layout being restored.
//
// ShowWindow's return value reports the window's *previous* visibility, not
// success or failure, so it is deliberately not treated as an error. A
// window that vanished between enumeration and this call simply does
// nothing, which is the correct outcome.
func MinimizeWindow(hwnd uintptr) {
	procShowWindow.Call(hwnd, uintptr(swShowMinNoActive))
}
