package win32

import (
	"fmt"
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

// MessageWindow is a hidden, message-only top-level window used to receive
// tray icon callbacks and the WM_DESTROY that ends the application's message
// loop.
type MessageWindow struct {
	hwnd        uintptr
	hInstance   uintptr
	classPtr    *uint16
	onTrayClick func()
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
	switch msg {
	case wmTrayIcon:
		if lparam == wmRightButtonUp || lparam == wmLeftButtonUp {
			msgWndMu.Lock()
			mw := msgWndState[hwnd]
			msgWndMu.Unlock()
			if mw != nil && mw.onTrayClick != nil {
				mw.onTrayClick()
			}
		}
		return 0
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
// never-shown top-level window of that class. onTrayClick is invoked (from
// inside Run's DispatchMessageW, i.e. on the caller's own goroutine) whenever
// the tray icon receives a left- or right-button-up click.
func NewMessageWindow(className string, onTrayClick func()) (*MessageWindow, error) {
	hInstance, _, _ := procGetModuleHandleW.Call(0)

	classPtr, err := syscall.UTF16PtrFromString(className)
	if err != nil {
		return nil, fmt.Errorf("class name: %w", err)
	}

	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   messageWindowProc,
		HInstance:     hInstance,
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

	mw := &MessageWindow{hwnd: hwnd, hInstance: hInstance, classPtr: classPtr, onTrayClick: onTrayClick}
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
}

// trayIconUID identifies the icon within its owning window. fenster shows
// exactly one tray icon per process, so a fixed id is sufficient.
const trayIconUID uint32 = 1

// NewTrayIcon adds a tray icon owned by hwnd. Clicks on it arrive at hwnd as
// wmTrayIcon messages (see MessageWindow).
func NewTrayIcon(hwnd uintptr, icon uintptr, tip string) (*TrayIcon, error) {
	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             hwnd,
		UID:              trayIconUID,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: uint32(wmTrayIcon),
		HIcon:            icon,
	}
	putUTF16(nid.SzTip[:], tip)

	ret, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if ret == 0 {
		return nil, fmt.Errorf("Shell_NotifyIconW(NIM_ADD): %w", err)
	}
	return &TrayIcon{hwnd: hwnd, uid: trayIconUID}, nil
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

// AddItem appends a command item.
func (m *Menu) AddItem(id uint32, label string, checked, disabled bool) {
	flags := uintptr(mfString)
	if checked {
		flags |= mfChecked
	}
	if disabled {
		flags |= mfGrayed
	}
	labelPtr, err := syscall.UTF16PtrFromString(label)
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
	labelPtr, err := syscall.UTF16PtrFromString(label)
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

// Destroy destroys the menu and all submenus attached via AddSubmenu.
func (m *Menu) Destroy() {
	for _, sub := range m.subs {
		sub.Destroy()
	}
	m.subs = nil
	procDestroyMenu.Call(m.handle)
}
