package win32

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// dialogState carries one InputBox invocation's mutable result across its
// window procedure and its local message loop.
type dialogState struct {
	editHwnd uintptr
	text     string
	ok       bool
	done     bool
}

// dialogMu guards dialogCurrent. InputBox runs its own nested modal message
// loop on the calling goroutine (this is a single-threaded UI application,
// like the rest of internal/win32), so at most one InputBox is ever
// in-flight at a time; the mutex documents that rather than protecting
// against real concurrency.
var (
	dialogMu      sync.Mutex
	dialogCurrent *dialogState
)

// inputBoxWndProc is a single, permanent callback (see messageWindowProc's
// comment in tray_windows.go for why: syscall.NewCallback's table is small
// and never freed). It deliberately does NOT call PostQuitMessage on
// WM_DESTROY: this window's message loop is a nested loop running on the
// same thread as the application's main MessageWindow.Run loop, and posting
// WM_QUIT here would incorrectly terminate that outer loop too (WM_QUIT is
// a thread-wide message, not tied to this window). The loop in InputBox
// instead ends by polling dialogCurrent.done.
var inputBoxWndProc = syscall.NewCallback(func(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmCommand:
		id := wparam & 0xFFFF
		dialogMu.Lock()
		st := dialogCurrent
		dialogMu.Unlock()
		if st != nil {
			switch id {
			case 1: // IDOK
				st.text = getWindowText(st.editHwnd)
				st.ok = true
				st.done = true
				procDestroyWindow.Call(hwnd)
			case 2: // IDCANCEL
				st.ok = false
				st.done = true
				procDestroyWindow.Call(hwnd)
			}
		}
		return 0
	case wmClose:
		dialogMu.Lock()
		if dialogCurrent != nil {
			dialogCurrent.ok = false
			dialogCurrent.done = true
		}
		dialogMu.Unlock()
		procDestroyWindow.Call(hwnd)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return ret
})

// inputBoxSeq makes each InputBox window class name unique so that repeated
// calls never collide with a not-yet-unregistered previous class.
var inputBoxSeq uint64

// getWindowText reads an edit control's text via WM_GETTEXT, sized first via
// GetWindowTextLengthW (reused from Task 6; not redeclared here).
func getWindowText(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if length == 0 {
		return ""
	}
	buf := make([]uint16, length+1)
	procSendMessageW.Call(hwnd, uintptr(wmGetText), uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	return syscall.UTF16ToString(buf)
}

// centeredPosition returns the top-left corner that centers a w x h window
// on the primary monitor's full screen rectangle.
func centeredPosition(w, h int32) (int32, int32) {
	sw, _, _ := procGetSystemMetrics.Call(uintptr(smCxScreen))
	sh, _, _ := procGetSystemMetrics.Call(uintptr(smCyScreen))
	x := (int32(sw) - w) / 2
	y := (int32(sh) - h) / 2
	return x, y
}

// InputBox shows a small modal-style dialog with a label, a pre-filled edit
// field, and OK/Abbrechen buttons (ids 1/2, i.e. IDOK/IDCANCEL, so that
// IsDialogMessage's built-in Enter-confirms/Escape-cancels behavior applies
// without extra plumbing). It returns the edited text and true on OK, or ""
// and false on Abbrechen, Escape or closing the window.
func InputBox(title, prompt, initial string) (string, bool) {
	hInstance, _, _ := procGetModuleHandleW.Call(0)

	seq := atomic.AddUint64(&inputBoxSeq, 1)
	classPtr, err := syscall.UTF16PtrFromString(fmt.Sprintf("fensterInputBox%d", seq))
	if err != nil {
		return "", false
	}

	hCursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   inputBoxWndProc,
		HInstance:     hInstance,
		HCursor:       hCursor,
		HbrBackground: uintptr(colorBtnFace + 1),
		LpszClassName: classPtr,
	}
	if atom, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return "", false
	}
	defer procUnregisterClassW.Call(uintptr(unsafe.Pointer(classPtr)), hInstance)

	const width, height int32 = 420, 150
	x, y := centeredPosition(width, height)

	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return "", false
	}

	// WS_VISIBLE is added on top of the brief's WS_OVERLAPPED|WS_CAPTION|
	// WS_SYSMENU: without it the window is created hidden and would never
	// be shown, which cannot be the intent of a dialog a human types into.
	dwStyle := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsVisible)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		dwStyle,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return "", false
	}

	st := &dialogState{}
	dialogMu.Lock()
	dialogCurrent = st
	dialogMu.Unlock()
	defer func() {
		dialogMu.Lock()
		dialogCurrent = nil
		dialogMu.Unlock()
	}()

	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	editClass, _ := syscall.UTF16PtrFromString("EDIT")
	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")

	promptPtr, _ := syscall.UTF16PtrFromString(prompt)
	staticHwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(promptPtr)),
		uintptr(wsChild|wsVisible),
		20, 15, 380, 20,
		hwnd, 0, hInstance, 0,
	)

	// CreateWindowExW's first argument is dwExStyle, not dwStyle. Fix round 1
	// caught this control passing wsBorder (a dwStyle bit, WS_BORDER) into
	// that dwExStyle slot: the border never rendered, and a reserved WS_EX_
	// bit was set instead. The fix uses wsExClientEdge (WS_EX_CLIENTEDGE) in
	// the dwExStyle slot alone, giving the conventional single sunken
	// edit-box edge; WS_BORDER is not also added to dwStyle, since combining
	// both draws a redundant second, flat border around the sunken one.
	initialPtr, _ := syscall.UTF16PtrFromString(initial)
	editHwnd, _, _ := procCreateWindowExW.Call(
		uintptr(wsExClientEdge),
		uintptr(unsafe.Pointer(editClass)), uintptr(unsafe.Pointer(initialPtr)),
		uintptr(wsChild|wsVisible|wsTabStop|esAutoHScroll),
		20, 40, 380, 24,
		hwnd, 0, hInstance, 0,
	)
	st.editHwnd = editHwnd

	okTitlePtr, _ := syscall.UTF16PtrFromString("OK")
	okHwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(okTitlePtr)),
		uintptr(wsChild|wsVisible|wsTabStop|bsDefPushButton),
		220, 80, 80, 26,
		hwnd, 1, hInstance, 0,
	)

	cancelTitlePtr, _ := syscall.UTF16PtrFromString("Abbrechen")
	cancelHwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(cancelTitlePtr)),
		uintptr(wsChild|wsVisible|wsTabStop),
		310, 80, 90, 26,
		hwnd, 2, hInstance, 0,
	)

	if hFont, _, _ := procGetStockObject.Call(defaultGuiFont); hFont != 0 {
		for _, ctrl := range []uintptr{staticHwnd, editHwnd, okHwnd, cancelHwnd} {
			procSendMessageW.Call(ctrl, uintptr(wmSetFont), hFont, 1)
		}
	}

	procSetFocus.Call(editHwnd)

	var msg msgT
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 {
			// WM_QUIT arrived. This nested loop runs on the same thread as
			// the application's main MessageWindow.Run loop, so this WM_QUIT
			// was not necessarily meant for this dialog: it may be the
			// application's own shutdown request (e.g. Quit() from the tray
			// menu, called while this dialog happened to be open). Destroy
			// this dialog so it does not leak, then repost the WM_QUIT so
			// the outer Run loop still observes it and the application
			// actually exits, instead of this loop silently swallowing it.
			procDestroyWindow.Call(hwnd)
			procPostQuitMessage.Call(msg.WParam)
			return "", false
		}
		if handled, _, _ := procIsDialogMessageW.Call(hwnd, uintptr(unsafe.Pointer(&msg))); handled == 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}
		if st.done {
			break
		}
	}

	if st.ok {
		return st.text, true
	}
	return "", false
}

// Confirm shows a Ja/Nein message box and reports whether the user chose Ja.
func Confirm(title, text string) bool {
	textPtr, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return false
	}
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return false
	}
	ret, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(titlePtr)), uintptr(mbYesNo|mbIconWarning))
	return ret == idYes
}

// OpenInExplorer opens Windows Explorer with path selected. If path does not
// exist yet, its containing directory is opened instead.
func OpenInExplorer(path string) error {
	verbPtr, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return fmt.Errorf("verb: %w", err)
	}
	exePtr, err := syscall.UTF16PtrFromString("explorer.exe")
	if err != nil {
		return fmt.Errorf("exe: %w", err)
	}

	// Quoted manually rather than with fmt's %q, which would escape the
	// path's backslashes as a Go string literal would (\\), corrupting the
	// Windows path passed on to explorer.exe.
	var params string
	if _, statErr := os.Stat(path); statErr != nil {
		params = "\"" + filepath.Dir(path) + "\""
	} else {
		params = "/select,\"" + path + "\""
	}
	paramsPtr, err := syscall.UTF16PtrFromString(params)
	if err != nil {
		return fmt.Errorf("params: %w", err)
	}

	ret, _, err := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verbPtr)), uintptr(unsafe.Pointer(exePtr)), uintptr(unsafe.Pointer(paramsPtr)), 0, uintptr(swShowNormal))
	// ShellExecuteW returns a value > 32 on success, an error code <= 32
	// otherwise (it is not a real HINSTANCE despite the historical type).
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW: %w", err)
	}
	return nil
}
