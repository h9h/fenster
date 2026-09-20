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
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

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

	// Task 10: single-instance guard (AcquireSingleInstance in
	// instance_windows.go). procCloseHandle above is reused to release the
	// mutex handle.
	procCreateMutexW = kernel32.NewProc("CreateMutexW")

	// Window-class and window-lifecycle procs. Owned by this package; Task 9
	// reuses these same vars and must not redeclare them.
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procUnregisterClassW = user32.NewProc("UnregisterClassW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")

	// Task 9: message window, tray icon, menus and dialogs. Task 6 already
	// declared procRegisterClassExW, procCreateWindowExW, procDestroyWindow,
	// procUnregisterClassW, procDefWindowProcW, procGetModuleHandleW and
	// procGetWindowTextLengthW above; those are reused as-is here rather than
	// redeclared.
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procLoadImageW          = user32.NewProc("LoadImageW")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procMessageBoxW         = user32.NewProc("MessageBoxW")
	procSendMessageW        = user32.NewProc("SendMessageW")
	procEnableWindow        = user32.NewProc("EnableWindow")
	procShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW       = shell32.NewProc("ShellExecuteW")

	// Not in the task brief's proc list, but needed for a correct
	// implementation of the InputBox dialog described in prose: centering
	// the window (procGetSystemMetrics), routing Enter/Escape to the
	// default/cancel button per BS_DEFPUSHBUTTON and control id 2 ==
	// IDCANCEL (procIsDialogMessageW), giving the edit control initial
	// keyboard focus (procSetFocus), and applying a readable font to the
	// controls via WM_SETFONT (procGetStockObject).
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procIsDialogMessageW = user32.NewProc("IsDialogMessageW")
	procSetFocus         = user32.NewProc("SetFocus")
	procGetStockObject   = gdi32.NewProc("GetStockObject")

	// procRegisterWindowMessageW registers the "TaskbarCreated" message id
	// used to re-add the tray icon after Explorer restarts (see
	// taskbarCreatedMsg in tray_windows.go).
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")

	// procShowWindow minimizes an extraneous window during a restore. Note
	// this is deliberately NOT SetWindowPlacement, which the restore path
	// uses: SetWindowPlacement also rewrites rcNormalPosition, and an
	// extraneous window is only being moved out of the way — where it
	// returns to when the user restores it must not change.
	procShowWindow = user32.NewProc("ShowWindow")

	// Hotkey capture dialog (capture_windows.go). procGetKeyState reads the
	// modifier state at the moment a key message arrives; procSetWindowTextW
	// updates the dialog's echo line as the user presses combinations.
	procGetKeyState    = user32.NewProc("GetKeyState")
	procSetWindowTextW = user32.NewProc("SetWindowTextW")

	// Global hotkeys. A hotkey registered by a thread can only be
	// unregistered by that same thread, which is why the application locks
	// its UI goroutine to an OS thread (runtime.LockOSThread in main).
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")

	// Fix round 1: window classes need a real cursor and background brush,
	// or a human tester sees no I-beam/arrow feedback and background paint
	// artifacts on the dialog.
	procLoadCursorW = user32.NewProc("LoadCursorW")

	// Test-only: used by the win32integration smoke test to assert that
	// Menu.AddItem/AddSeparator/AddSubmenu actually appended items and set
	// the expected checked/greyed state.
	procGetMenuItemCount = user32.NewProc("GetMenuItemCount")
	procGetMenuState     = user32.NewProc("GetMenuState")

	// Test-only: used by the win32integration smoke test to read back an
	// item's actual rendered label, to assert that AddItem escapes "&".
	procGetMenuStringW = user32.NewProc("GetMenuStringW")
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

	// Tray icon (Shell_NotifyIconW / NOTIFYICONDATAW).
	nimAdd          = 0x00000000
	nimModify       = 0x00000001
	nimDelete       = 0x00000002
	nifMessage      = 0x00000001
	nifIcon         = 0x00000002
	nifTip          = 0x00000004
	nifInfo         = 0x00000010
	wmRightButtonUp = 0x0205
	wmLeftButtonUp  = 0x0202
	wmDestroy       = 0x0002
	wmCommand       = 0x0111
	wmClose         = 0x0010
	wmSetFont       = 0x0030
	wmGetText       = 0x000D
	wmNull          = 0x0000

	// wmTrayIcon is the tray callback message, WM_APP+1. WM_APP is 0x8000;
	// the task brief's own proc listing mislabeled 0x0400 (which is
	// WM_USER, not WM_APP) as "WM_APP+1", contradicting its own "Details
	// that matter" section, which explicitly calls for WM_APP+1. The value
	// below follows the explicit prose, not the mislabeled arithmetic.
	wmTrayIcon = 0x8000 + 1

	// Menus
	mfString       = 0x00000000
	mfSeparator    = 0x00000800
	mfChecked      = 0x00000008
	mfGrayed       = 0x00000001
	mfPopup        = 0x00000010
	mfByCommand    = 0x00000000 // GetMenuState's uFlags: look up by item id
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	// MessageBox
	mbOK          = 0x00000000
	mbYesNo       = 0x00000004
	mbIconError   = 0x00000010
	mbIconWarning = 0x00000030
	idYes         = 6

	// errorAlreadyExists is ERROR_ALREADY_EXISTS, the last-error code
	// CreateMutexW leaves set when the named mutex already existed.
	errorAlreadyExists = 183

	// Key messages and virtual-key codes used by the hotkey capture dialog.
	// wmSysKeyDown is the one Alt combinations arrive as; capturing only
	// wmKeyDown would miss every Alt+<key> the user presses and would also
	// let Alt open the window's system menu.
	wmKeyDown    = 0x0100
	wmSysKeyDown = 0x0104

	vkReturn  = 0x0D
	vkEscape  = 0x1B
	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12 // Alt
	vkLWin    = 0x5B
	vkRWin    = 0x5C

	// keyPressedMask is the high bit GetKeyState sets while a key is down.
	keyPressedMask = 0x8000

	// ssCenter centers a STATIC control's text, used for the capture
	// dialog's echo line.
	ssCenter = 0x00000001

	// Hotkeys. modNoRepeat (MOD_NOREPEAT) makes Windows deliver one
	// WM_HOTKEY per press instead of repeating while the keys are held
	// down: a restore is not an operation that should run forty times a
	// second.
	modNoRepeat = 0x4000

	// errorHotkeyAlreadyRegistered is ERROR_HOTKEY_ALREADY_REGISTERED, the
	// last-error code RegisterHotKey leaves set when another window or
	// process already owns the combination.
	errorHotkeyAlreadyRegistered = 1409

	// wmHotkey (WM_HOTKEY) carries the registration id in the low word of
	// wParam. wmDisplayChange (WM_DISPLAYCHANGE) is broadcast when the
	// display resolution or arrangement changes, which is when the set of
	// layouts matching the current setup can change.
	wmHotkey        = 0x0312
	wmDisplayChange = 0x007E

	// Icons
	imageIcon      = 1
	lrLoadFromFile = 0x00000010
	lrDefaultSize  = 0x00000040
	idiApplication = 32512

	// Window styles and control styles used by dialog_windows.go's InputBox.
	// wsOverlapped/wsCaption/wsSysMenu/wsChild/wsTabStop are plain
	// (non-negative) style bits; wsVisible above is reused for both the
	// dialog window and its controls.
	wsOverlapped    = 0x00000000
	wsCaption       = 0x00C00000
	wsSysMenu       = 0x00080000
	wsChild         = 0x40000000
	wsTabStop       = 0x00010000
	esAutoHScroll   = 0x00000080
	bsDefPushButton = 0x00000001

	// wsExClientEdge (WS_EX_CLIENTEDGE) is an *extended* style
	// (CreateWindowExW's first argument). Fix round 1 found the edit control
	// had passed WS_BORDER (a plain dwStyle bit, not an extended style) into
	// the dwExStyle slot by mistake, which set a reserved WS_EX_ bit instead
	// of drawing a border. wsExClientEdge is used instead, alone, for the
	// conventional single sunken edit-box edge; there is no wsBorder
	// constant, since combining WS_BORDER with WS_EX_CLIENTEDGE would only
	// draw a redundant second, flat border, and nothing else in this
	// package needs it.
	wsExClientEdge = 0x00000200

	// GetSystemMetrics indices used to center the InputBox window.
	smCxScreen = 0
	smCyScreen = 1

	// GetStockObject argument used to give InputBox's controls a readable
	// font instead of the default bitmap font.
	defaultGuiFont = 17

	// LoadCursorW/window-class background brush constants (fix round 1
	// minor): idcArrow is IDC_ARROW as a MAKEINTRESOURCE id; colorBtnFace+1
	// is passed directly as HbrBackground, which is how WNDCLASSEXW expects
	// a system color to be given instead of an actual brush handle (see the
	// CbSize comment on wndClassEx / the Win32 docs for WNDCLASSEX.hbrBackground).
	idcArrow     = 32512
	colorBtnFace = 15
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

// msgT mirrors the Win32 MSG structure, used by the message loops in
// tray_windows.go and dialog_windows.go.
type msgT struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
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
