package win32

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// notifyIconData mirrors the Win32 NOTIFYICONDATAW structure used by
// Shell_NotifyIconW. CbSize must be set from unsafe.Sizeof by every caller.
type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

// putUTF16 copies s into dst as a NUL-terminated UTF-16 string, truncating if
// necessary. dst must have room for at least one NUL.
func putUTF16(dst []uint16, s string) {
	src, err := syscall.UTF16FromString(s)
	if err != nil {
		// s contained an embedded NUL; fall back to an empty string rather
		// than propagating an error from what is otherwise a void helper.
		src = []uint16{0}
	}
	n := len(src)
	if n > len(dst) {
		n = len(dst)
	}
	copy(dst, src[:n])
	dst[len(dst)-1] = 0
}

// Callbacks are the events a MessageWindow reports to its owner. Every one
// is invoked from inside Run's DispatchMessageW — that is, on the owner's
// own goroutine, the one locked to this thread — and a nil field means the
// message is ignored. A named struct rather than positional parameters
// because a reader of the call site should not have to count func()
// arguments to work out which is which.
type Callbacks struct {
	// TrayClick fires on a left- or right-button-up on the tray icon.
	TrayClick func()
	// TaskbarRecreated fires when Explorer restarts and rebuilds the
	// notification area; the owner must re-add its tray icon.
	TaskbarRecreated func()
	// DisplayChange fires when the display resolution or arrangement
	// changes, which can change which saved layouts match the current setup.
	DisplayChange func()
	// Hotkey fires with the registration id of a pressed global hotkey.
	Hotkey func(id int32)
}

// MessageWindow is a hidden, never-shown top-level window used to receive
// tray icon callbacks, global hotkey presses, display changes and the
// WM_DESTROY that ends the application's message loop. NewMessageWindow
// creates it with hWndParent = 0, a genuine top-level window, not
// HWND_MESSAGE (a true message-only window): Windows broadcasts
// WM_DISPLAYCHANGE only to top-level windows, so a message-only window
// would never receive it, silently killing the display-change re-sync
// while hotkeys kept working, with no error and no log line. Do not
// "fix" this into HWND_MESSAGE.
type MessageWindow struct {
	hwnd      uintptr
	hInstance uintptr
	classPtr  *uint16
	cb        Callbacks
}

// lookupMessageWindow resolves the instance that owns hwnd. Every branch of
// messageWindowProc goes through it rather than repeating the mutex dance.
func lookupMessageWindow(hwnd uintptr) *MessageWindow {
	msgWndMu.Lock()
	defer msgWndMu.Unlock()
	return msgWndState[hwnd]
}

// taskbarCreatedMsg is the id Windows assigns to the "TaskbarCreated"
// message, registered once at package init (RegisterWindowMessageW always
// returns the same id for the same string within a session, so registering
// it eagerly here rather than lazily on first use changes nothing
// observable). Explorer broadcasts this message to every top-level window
// when it restarts — a Windows update, an Explorer crash, or a manual
// restart all trigger it — and rebuilds the notification area from
// scratch: every icon that was there before is gone until its owner calls
// Shell_NotifyIconW(NIM_ADD) again. messageWindowProc watches for this id
// and calls back into the owning MessageWindow's cb.TaskbarRecreated so that
// re-add happens automatically instead of leaving the process running with
// no visible icon and no way to reach it short of Task Manager.
var taskbarCreatedMsg = registerTaskbarCreatedMsg()

func registerTaskbarCreatedMsg() uintptr {
	namePtr, err := syscall.UTF16PtrFromString("TaskbarCreated")
	if err != nil {
		return 0
	}
	ret, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(namePtr)))
	return ret
}

// msgWndMu guards msgWndState, which routes messages arriving at
// messageWindowProc back to the *MessageWindow instance that owns the
// window, keyed by hwnd. A package-level map (rather than a closure per
// call to NewMessageWindow) keeps a single, permanent syscall.NewCallback
// registration regardless of how many message windows are ever created, for
// the same reason enumWindowsCallback and enumMonitorsCallback in
// desktop_windows.go are package-level: syscall.NewCallback's backing table
// is small and never freed.
var (
	msgWndMu    sync.Mutex
	msgWndState = map[uintptr]*MessageWindow{}
)

var messageWindowProc = syscall.NewCallback(func(hwnd, msg, wparam, lparam uintptr) uintptr {
	// taskbarCreatedMsg is a runtime-registered id, not a WM_ constant, so it
	// is checked separately rather than as a switch case: if registration
	// ever failed and left it 0, a bare `case 0:` in the switch below would
	// wrongly fire on WM_NULL, which Menu.Track posts to this very window on
	// every close.
	if taskbarCreatedMsg != 0 && msg == taskbarCreatedMsg {
		if mw := lookupMessageWindow(hwnd); mw != nil && mw.cb.TaskbarRecreated != nil {
			mw.cb.TaskbarRecreated()
		}
		return 0
	}
	switch msg {
	case wmTrayIcon:
		if lparam == wmRightButtonUp || lparam == wmLeftButtonUp {
			if mw := lookupMessageWindow(hwnd); mw != nil && mw.cb.TrayClick != nil {
				mw.cb.TrayClick()
			}
		}
		return 0
	case wmHotkey:
		// The registration id is the low word of wParam. The modifiers and
		// virtual-key code live in lParam (LOWORD = modifiers, HIWORD =
		// virtual key), not in wParam's high word; this handler does not
		// need either since the id alone identifies the registration.
		if mw := lookupMessageWindow(hwnd); mw != nil && mw.cb.Hotkey != nil {
			mw.cb.Hotkey(int32(wparam & 0xFFFF))
		}
		return 0
	case wmDisplayChange:
		if mw := lookupMessageWindow(hwnd); mw != nil && mw.cb.DisplayChange != nil {
			mw.cb.DisplayChange()
		}
		// No return: WM_DISPLAYCHANGE is passed on to DefWindowProcW below,
		// as Windows expects for a message it broadcasts to every top-level
		// window.
	case wmDestroy:
		msgWndMu.Lock()
		delete(msgWndState, hwnd)
		msgWndMu.Unlock()
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return ret
})

// NewMessageWindow registers className as a window class and creates a
// never-shown top-level window of that class. The events it reports are
// described on Callbacks; all of them arrive from inside Run's
// DispatchMessageW, i.e. on the caller's own goroutine. TaskbarRecreated
// exists because MessageWindow is created before the TrayIcon that will
// need re-adding, so re-adding is the caller's job, not this type's.
func NewMessageWindow(className string, cb Callbacks) (*MessageWindow, error) {
	hInstance, _, _ := procGetModuleHandleW.Call(0)

	classPtr, err := syscall.UTF16PtrFromString(className)
	if err != nil {
		return nil, fmt.Errorf("class name: %w", err)
	}

	hCursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   messageWindowProc,
		HInstance:     hInstance,
		HCursor:       hCursor,
		HbrBackground: uintptr(colorBtnFace + 1),
		LpszClassName: classPtr,
	}
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return nil, fmt.Errorf("RegisterClassExW: %w", err)
	}

	titlePtr, err := syscall.UTF16PtrFromString("fenster")
	if err != nil {
		procUnregisterClassW.Call(uintptr(unsafe.Pointer(classPtr)), hInstance)
		return nil, fmt.Errorf("title: %w", err)
	}

	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		0,
		0, 0, 0, 0,
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		procUnregisterClassW.Call(uintptr(unsafe.Pointer(classPtr)), hInstance)
		return nil, fmt.Errorf("CreateWindowExW: %w", err)
	}

	mw := &MessageWindow{
		hwnd:      hwnd,
		hInstance: hInstance,
		classPtr:  classPtr,
		cb:        cb,
	}
	msgWndMu.Lock()
	msgWndState[hwnd] = mw
	msgWndMu.Unlock()
	return mw, nil
}

// Handle returns the window's HWND.
func (mw *MessageWindow) Handle() uintptr { return mw.hwnd }

// Run pumps the message loop until the window is destroyed (WM_QUIT).
func (mw *MessageWindow) Run() {
	var msg msgT
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// Quit posts WM_CLOSE to the window, which DefWindowProcW's default handling
// turns into a DestroyWindow, which in turn sends WM_DESTROY, ending Run's
// loop via PostQuitMessage.
func (mw *MessageWindow) Quit() {
	procPostMessageW.Call(mw.hwnd, uintptr(wmClose), 0, 0)
}

// TrayIcon is a single Shell_NotifyIconW-managed notification area icon.
type TrayIcon struct {
	hwnd uintptr
	uid  uint32
	icon uintptr
	tip  string
}

// trayIconUID identifies the icon within its owning window. fenster shows
// exactly one tray icon per process, so a fixed id is sufficient.
const trayIconUID uint32 = 1

// NewTrayIcon adds a tray icon owned by hwnd. Clicks on it arrive at hwnd as
// wmTrayIcon messages (see MessageWindow).
func NewTrayIcon(hwnd uintptr, icon uintptr, tip string) (*TrayIcon, error) {
	t := &TrayIcon{hwnd: hwnd, uid: trayIconUID, icon: icon, tip: tip}
	if err := t.add(); err != nil {
		return nil, err
	}
	return t, nil
}

// add issues Shell_NotifyIconW(NIM_ADD) for this icon's current hwnd, uid,
// icon and tip. Both NewTrayIcon and Readd (called after a TaskbarCreated
// notification) go through this single place so they cannot drift apart.
func (t *TrayIcon) add() error {
	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             t.hwnd,
		UID:              t.uid,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: uint32(wmTrayIcon),
		HIcon:            t.icon,
	}
	putUTF16(nid.SzTip[:], t.tip)

	ret, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if ret == 0 {
		return fmt.Errorf("Shell_NotifyIconW(NIM_ADD): %w", err)
	}
	return nil
}

// Readd re-issues NIM_ADD for this icon with its original icon handle and
// tip. Call it after a TaskbarCreated notification: Explorer has thrown away
// every icon in the notification area and this is the only way to get it
// back without restarting the process.
func (t *TrayIcon) Readd() error {
	return t.add()
}

// Balloon shows a Windows notification balloon from the tray icon.
func (t *TrayIcon) Balloon(title, text string) error {
	nid := notifyIconData{
		CbSize: uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:   t.hwnd,
		UID:    t.uid,
		UFlags: nifInfo,
	}
	putUTF16(nid.SzInfo[:], text)
	putUTF16(nid.SzInfoTitle[:], title)

	ret, _, err := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
	if ret == 0 {
		return fmt.Errorf("Shell_NotifyIconW(NIM_MODIFY): %w", err)
	}
	return nil
}

// Remove deletes the tray icon.
func (t *TrayIcon) Remove() error {
	nid := notifyIconData{
		CbSize: uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:   t.hwnd,
		UID:    t.uid,
	}
	ret, _, err := procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	if ret == 0 {
		return fmt.Errorf("Shell_NotifyIconW(NIM_DELETE): %w", err)
	}
	return nil
}

// LoadIconFromFile loads an icon from an .ico (or icon-bearing) file.
func LoadIconFromFile(path string) (uintptr, error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("path: %w", err)
	}
	h, _, err := procLoadImageW.Call(0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, 0, 0, uintptr(lrLoadFromFile|lrDefaultSize))
	if h == 0 {
		return 0, fmt.Errorf("LoadImageW: %w", err)
	}
	return h, nil
}

// StockIcon returns the shared system application icon (IDI_APPLICATION).
// The returned handle is owned by the system and must not be destroyed.
func StockIcon() uintptr {
	h, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
	return h
}

// Menu wraps a Win32 popup menu (HMENU). Submenus attached with AddSubmenu
// are kept alive on the parent so that Destroy can tear the whole tree down.
type Menu struct {
	handle uintptr
	subs   []*Menu
}

// NewMenu creates an empty popup menu.
func NewMenu() *Menu {
	h, _, _ := procCreatePopupMenu.Call()
	return &Menu{handle: h}
}

// escapeMenuAmpersand doubles "&" so a literal ampersand in a label (e.g. a
// window titled "Tom & Jerry") renders as-is instead of Win32 interpreting a
// single "&" as marking the following character as a keyboard accelerator,
// which would render as "Tom Jerry" with a stray underline.
func escapeMenuAmpersand(label string) string {
	return strings.ReplaceAll(label, "&", "&&")
}

// AddItem appends a command item.
func (m *Menu) AddItem(id uint32, label string, checked, disabled bool) {
	flags := uintptr(mfString)
	if checked {
		flags |= mfChecked
	}
	if disabled {
		flags |= mfGrayed
	}
	labelPtr, err := syscall.UTF16PtrFromString(escapeMenuAmpersand(label))
	if err != nil {
		return
	}
	procAppendMenuW.Call(m.handle, flags, uintptr(id), uintptr(unsafe.Pointer(labelPtr)))
}

// AddSeparator appends a visual separator line.
func (m *Menu) AddSeparator() {
	procAppendMenuW.Call(m.handle, uintptr(mfSeparator), 0, 0)
}

// AddSubmenu appends sub as a nested popup under label. sub is kept on m so
// that m.Destroy() also destroys sub.
func (m *Menu) AddSubmenu(label string, sub *Menu, disabled bool) {
	flags := uintptr(mfPopup | mfString)
	if disabled {
		flags |= mfGrayed
	}
	labelPtr, err := syscall.UTF16PtrFromString(escapeMenuAmpersand(label))
	if err != nil {
		return
	}
	procAppendMenuW.Call(m.handle, flags, sub.handle, uintptr(unsafe.Pointer(labelPtr)))
	m.subs = append(m.subs, sub)
}

// Track shows the popup menu at the current cursor position, modally, and
// returns the id of the chosen item (0 if dismissed without a choice).
//
// SetForegroundWindow before TrackPopupMenu, and posting WM_NULL to hwnd
// afterwards, are both required: without them the menu can fail to close
// when the user clicks outside it. This is a long-documented Win32 quirk,
// not an oversight.
func (m *Menu) Track(hwnd uintptr) uint32 {
	procSetForegroundWindow.Call(hwnd)

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	ret, _, _ := procTrackPopupMenu.Call(
		m.handle,
		uintptr(tpmReturnCmd|tpmRightButton),
		uintptr(pt.X), uintptr(pt.Y),
		0, hwnd, 0,
	)

	procPostMessageW.Call(hwnd, uintptr(wmNull), 0, 0)
	return uint32(ret)
}

// Destroy destroys the menu and, transitively, every submenu attached via
// AddSubmenu: DestroyMenu already recursively frees any popup menu it still
// holds a handle to, so a separate loop calling sub.Destroy() first would
// only ask USER32 to free handles it had already freed a moment earlier, on
// every single menu open. m.subs is dropped here purely to release fenster's
// own references to those *Menu wrappers, not because anything Win32-side
// still needs it.
func (m *Menu) Destroy() {
	m.subs = nil
	procDestroyMenu.Call(m.handle)
}

// MessageWindowClass is the window class name fenster's hidden message
// window is registered under. It is a constant rather than a string each
// caller repeats because SignalQuit has to name the *same* class the running
// instance created, and a typo in either place would fail silently: the
// quit would report "nothing running" while the instance kept running.
const MessageWindowClass = "fensterMessageWindow"

// SignalQuit asks an already-running fenster to shut down and reports
// whether one was found. It posts WM_CLOSE to the instance's hidden message
// window, which is the same message the tray menu's "Beenden" sends, so the
// instance exits through its own shutdown path rather than being killed.
//
// This exists for deployment: the binary cannot be overwritten while it is
// running, and terminating the process instead would leave its icon sitting
// in the notification area until the shell next reaps it.
//
// PostMessageW rather than SendMessageW: the target window belongs to
// another process, and this call must not block on that process's message
// loop getting around to it.
func SignalQuit() bool { return signalQuitClass(MessageWindowClass) }

// signalQuitClass is SignalQuit with the class name as a parameter, so the
// find-and-close mechanism can be tested against a throwaway class without
// closing a real fenster that happens to be running on the test machine.
func signalQuitClass(class string) bool {
	hwnd := findWindowByClass(class)
	if hwnd == 0 {
		return false
	}
	procPostMessageW.Call(hwnd, uintptr(wmClose), 0, 0)
	return true
}

// findWindowByClass locates a window by class name WITHOUT acting on it,
// returning 0 when there is none.
//
// Split out from signalQuitClass because the two are easy to confuse with
// costly results: a caller that only wants to know whether an instance is
// running must not use signalQuitClass, which closes the instance as a side
// effect of answering. That mistake has already been made once here, in a
// test guard meant to protect a live instance from the test.
func findWindowByClass(class string) uintptr {
	classPtr, err := syscall.UTF16PtrFromString(class)
	if err != nil {
		return 0
	}
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(classPtr)), 0)
	return hwnd
}
