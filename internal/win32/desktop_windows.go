package win32

import (
	"fmt"
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

// EnumWindowsInfo describes every top-level window, without filtering. The
// caller decides what is eligible (see internal/layout).
func EnumWindowsInfo() ([]WindowInfo, error) {
	var out []WindowInfo
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		out = append(out, describeWindow(hwnd))
		return 1
	})
	if ret, _, err := procEnumWindows.Call(cb, 0); ret == 0 {
		return nil, fmt.Errorf("EnumWindows: %w", err)
	}
	return out, nil
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
	}
	return info
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

// EnumMonitors returns the current monitor arrangement in physical pixels.
func EnumMonitors() ([]store.Monitor, error) {
	var out []store.Monitor
	cb := syscall.NewCallback(func(hmon, hdc, lprc, data uintptr) uintptr {
		mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
		if ret, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); ret == 0 {
			return 1
		}
		out = append(out, store.Monitor{
			X:       mi.RcMonitor.Left,
			Y:       mi.RcMonitor.Top,
			W:       mi.RcMonitor.Right - mi.RcMonitor.Left,
			H:       mi.RcMonitor.Bottom - mi.RcMonitor.Top,
			Scale:   monitorScale(hmon),
			Primary: mi.DwFlags&1 != 0,
		})
		return 1
	})
	if ret, _, err := procEnumDisplayMonitors.Call(0, 0, cb, 0); ret == 0 {
		return nil, fmt.Errorf("EnumDisplayMonitors: %w", err)
	}
	return out, nil
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
func ApplyPlacement(handle uintptr, r store.Rect, state store.WindowState, topmost bool) error {
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
	// window, correct the position afterwards with screen coordinates.
	//
	// Coordinates are passed through uintptr(int32(...)) so that negative
	// values (windows on a monitor left of or above the primary) sign-extend
	// correctly instead of appearing as huge unsigned numbers.
	if state == store.StateNormal {
		flags := uintptr(swpNoZOrder | swpNoActivate)
		if ret, _, err := procSetWindowPos.Call(handle, 0, uintptr(int32(r.X)), uintptr(int32(r.Y)), uintptr(int32(r.W)), uintptr(int32(r.H)), flags); ret == 0 {
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
