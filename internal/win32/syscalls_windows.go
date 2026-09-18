// Package win32 wraps the Windows API calls the application needs. It is the
// only package that uses unsafe; everything above it works on plain data.
package win32

import "syscall"

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procEnumWindows                = user32.NewProc("EnumWindows")
	procGetWindowTextW             = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW       = user32.NewProc("GetWindowTextLengthW")
	procGetClassNameW              = user32.NewProc("GetClassNameW")
	procIsWindowVisible            = user32.NewProc("IsWindowVisible")
	procGetWindowLongPtrW          = user32.NewProc("GetWindowLongPtrW")
	procGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowPlacement         = user32.NewProc("GetWindowPlacement")
	procGetWindowRect              = user32.NewProc("GetWindowRect")
	procSetWindowPlacement         = user32.NewProc("SetWindowPlacement")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procEnumDisplayMonitors        = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW            = user32.NewProc("GetMonitorInfoW")
	procSetProcessDpiAwarenessCtx  = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForMonitor           = shcore.NewProc("GetDpiForMonitor")
	procDwmGetWindowAttribute      = dwmapi.NewProc("DwmGetWindowAttribute")
	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetCurrentProcessId        = kernel32.NewProc("GetCurrentProcessId")
	procGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")

	// Window-class and window-lifecycle procs. Owned by this package; Task 9
	// reuses these same vars and must not redeclare them.
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procUnregisterClassW = user32.NewProc("UnregisterClassW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
)

// Window styles, show commands and flags used by this package.
const (
	gwlExStyle     = -20
	wsExToolWindow = 0x00000080
	wsExTopmost    = 0x00000008

	swShowNormal      = 1
	swShowMinimized   = 2
	swShowMaximized   = 3
	swShowNoActivate  = 4
	swShowMinNoActive = 7

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	hwndTopmost   = ^uintptr(0) // (HWND)-1
	hwndNoTopmost = ^uintptr(1) // (HWND)-2

	dwmwaCloaked = 14

	processQueryLimitedInformation = 0x1000

	mdtEffectiveDPI = 0

	dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3) // (HANDLE)-4

	// Window-class and window-creation constants for the win32integration
	// test helper (createTestWindow) and, later, Task 9's own windows.
	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	cwUseDefault       = -2147483648 // (int)0x80000000, sign-extended
)

type rect struct{ Left, Top, Right, Bottom int32 }

type point struct{ X, Y int32 }

type windowPlacement struct {
	Length           uint32
	Flags            uint32
	ShowCmd          uint32
	PtMinPosition    point
	PtMaxPosition    point
	RcNormalPosition rect
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}

// wndClassEx mirrors the Win32 WNDCLASSEXW structure.
type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}
