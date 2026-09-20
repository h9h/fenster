package win32

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Describer turns a captured modifier set and virtual-key code into what the
// dialog should show: display is the combination in the user's language
// (e.g. "Strg+Umschalt+1"), problem is empty when the combination is
// acceptable and otherwise the reason it is not.
//
// It is a callback rather than logic in this package because this package
// deliberately knows nothing about what makes a hotkey valid — the rules and
// their German wording live in internal/hotkey, which is pure and tested.
// CaptureHotkey only needs to know whether to enable its OK button.
type Describer func(mods, vk uint32) (display, problem string)

// captureState carries one CaptureHotkey invocation's mutable result across
// its window procedure and its local message loop.
type captureState struct {
	hwnd        uintptr
	echoHwnd    uintptr
	problemHwnd uintptr
	okHwnd      uintptr

	mods  uint32
	vk    uint32
	valid bool

	done    bool
	ok      bool
	cleared bool
}

// captureMu guards captureCurrent, for the same reason dialogMu guards
// dialogCurrent: CaptureHotkey runs its own nested modal message loop on the
// calling goroutine, so at most one is ever in flight. The mutex documents
// that rather than protecting against real concurrency.
var (
	captureMu      sync.Mutex
	captureCurrent *captureState
)

// captureSeq makes each dialog's window class name unique so repeated calls
// never collide with a not-yet-unregistered previous class.
var captureSeq uint64

// Button ids. 1 and 2 are IDOK/IDCANCEL so IsDialogMessageW's built-in
// Enter-confirms/Escape-cancels handling applies without extra plumbing;
// 3 is the "clear this binding" button, which has no keyboard equivalent
// because every key that is not Enter or Escape is being captured.
const (
	captureIDOK     = 1
	captureIDCancel = 2
	captureIDClear  = 3
)

var captureWndProc = syscall.NewCallback(func(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmCommand:
		id := wparam & 0xFFFF
		captureMu.Lock()
		st := captureCurrent
		captureMu.Unlock()
		if st != nil {
			switch id {
			case captureIDOK:
				// Ignored unless a valid combination has been captured; the
				// button is disabled in that state anyway, but Enter reaches
				// IDOK through IsDialogMessageW regardless of the button's
				// enabled state, so the guard is load-bearing.
				if st.valid {
					st.ok = true
					st.done = true
					procDestroyWindow.Call(hwnd)
				}
			case captureIDCancel:
				st.ok = false
				st.done = true
				procDestroyWindow.Call(hwnd)
			case captureIDClear:
				st.ok = true
				st.cleared = true
				st.done = true
				procDestroyWindow.Call(hwnd)
			}
		}
		return 0
	case wmClose:
		captureMu.Lock()
		if captureCurrent != nil {
			captureCurrent.ok = false
			captureCurrent.done = true
		}
		captureMu.Unlock()
		procDestroyWindow.Call(hwnd)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return ret
})

// currentMods reads the modifier keys held right now. GetKeyState is asked at
// the moment the key message is handled rather than decoded from the message
// itself, because a key message carries no modifier state of its own.
func currentMods() uint32 {
	var mods uint32
	pressed := func(vk uintptr) bool {
		ret, _, _ := procGetKeyState.Call(vk)
		return ret&keyPressedMask != 0
	}
	if pressed(vkControl) {
		mods |= ModControl
	}
	if pressed(vkMenu) {
		mods |= ModAlt
	}
	if pressed(vkShift) {
		mods |= ModShift
	}
	if pressed(vkLWin) || pressed(vkRWin) {
		mods |= ModWin
	}
	return mods
}

// isModifierKey reports whether vk is a modifier pressed on its own, which is
// an incomplete combination rather than a key to capture.
func isModifierKey(vk uint32) bool {
	switch vk {
	case vkShift, vkControl, vkMenu, vkLWin, vkRWin:
		return true
	}
	return false
}

// setText replaces a control's caption.
func setText(hwnd uintptr, s string) {
	ptr, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return
	}
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(ptr)))
}

// CaptureHotkey shows a modal-style dialog that records the key combination
// the user actually presses, rather than asking them to type its name.
//
// This exists because typing was worse in a way that only showed up on real
// hardware: a keyboard whose Option key composes characters (a Mac keyboard,
// among others) cannot produce Alt at all, so a user could type a perfectly
// valid "Ctrl+Alt+1", have it stored and registered successfully, and then
// find it never fired — with nothing anywhere reporting a fault. Capturing
// the keystroke makes that impossible: what cannot be pressed cannot be
// saved.
//
// Escape cancels and Enter confirms, so neither can itself be part of a
// hotkey; every other key is captured. current is shown as the starting
// echo so the user can see the existing binding before replacing it.
//
// It returns the captured modifiers and virtual-key code with ok true, or
// cleared true when the user chose to remove the binding.
func CaptureHotkey(title, prompt, current string, describe Describer) (mods, vk uint32, ok, cleared bool) {
	hInstance, _, _ := procGetModuleHandleW.Call(0)

	seq := atomic.AddUint64(&captureSeq, 1)
	classPtr, err := syscall.UTF16PtrFromString(fmt.Sprintf("fensterCaptureHotkey%d", seq))
	if err != nil {
		return 0, 0, false, false
	}

	hCursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   captureWndProc,
		HInstance:     hInstance,
		HCursor:       hCursor,
		HbrBackground: uintptr(colorBtnFace + 1),
		LpszClassName: classPtr,
	}
	if atom, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return 0, 0, false, false
	}
	defer procUnregisterClassW.Call(uintptr(unsafe.Pointer(classPtr)), hInstance)

	const width, height int32 = 440, 190
	x, y := centeredPosition(width, height)

	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0, 0, false, false
	}

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(wsOverlapped|wsCaption|wsSysMenu|wsVisible),
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return 0, 0, false, false
	}

	st := &captureState{hwnd: hwnd}
	captureMu.Lock()
	prev := captureCurrent
	captureCurrent = st
	captureMu.Unlock()
	defer func() {
		captureMu.Lock()
		captureCurrent = prev
		captureMu.Unlock()
	}()

	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")

	promptPtr, _ := syscall.UTF16PtrFromString(prompt)
	promptHwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(promptPtr)),
		uintptr(wsChild|wsVisible),
		20, 14, 400, 20,
		hwnd, 0, hInstance, 0,
	)

	echoPtr, _ := syscall.UTF16PtrFromString(current)
	echoHwnd, _, _ := procCreateWindowExW.Call(
		uintptr(wsExClientEdge), uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(echoPtr)),
		uintptr(wsChild|wsVisible|ssCenter),
		20, 42, 400, 28,
		hwnd, 0, hInstance, 0,
	)
	st.echoHwnd = echoHwnd

	problemHwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(staticClass)), 0,
		uintptr(wsChild|wsVisible|ssCenter),
		20, 78, 400, 20,
		hwnd, 0, hInstance, 0,
	)
	st.problemHwnd = problemHwnd

	okTitlePtr, _ := syscall.UTF16PtrFromString("OK")
	okHwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(okTitlePtr)),
		uintptr(wsChild|wsVisible|bsDefPushButton),
		130, 118, 80, 26,
		hwnd, captureIDOK, hInstance, 0,
	)
	st.okHwnd = okHwnd
	// Nothing is captured yet, so there is nothing to confirm.
	procEnableWindow.Call(okHwnd, 0)

	clearTitlePtr, _ := syscall.UTF16PtrFromString("Löschen")
	clearHwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(clearTitlePtr)),
		uintptr(wsChild|wsVisible),
		220, 118, 90, 26,
		hwnd, captureIDClear, hInstance, 0,
	)

	cancelTitlePtr, _ := syscall.UTF16PtrFromString("Abbrechen")
	cancelHwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(cancelTitlePtr)),
		uintptr(wsChild|wsVisible),
		320, 118, 90, 26,
		hwnd, captureIDCancel, hInstance, 0,
	)

	if hFont, _, _ := procGetStockObject.Call(defaultGuiFont); hFont != 0 {
		for _, ctrl := range []uintptr{promptHwnd, echoHwnd, problemHwnd, okHwnd, clearHwnd, cancelHwnd} {
			procSendMessageW.Call(ctrl, uintptr(wmSetFont), hFont, 1)
		}
	}

	var msg msgT
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 {
			// WM_QUIT arrived. This nested loop shares a thread with the
			// application's main Run loop, so the quit may be the
			// application's own shutdown rather than anything to do with
			// this dialog. Destroy the dialog so it does not leak, then
			// repost the quit so the outer loop still sees it.
			procDestroyWindow.Call(hwnd)
			procPostQuitMessage.Call(msg.WParam)
			return 0, 0, false, false
		}

		// Capture happens here, ahead of IsDialogMessageW, so it works no
		// matter which control has focus and so a captured key never
		// doubles as dialog navigation. Enter and Escape are deliberately
		// let through: they are what confirms and cancels, which is exactly
		// why they cannot themselves be part of a hotkey.
		if msg.Message == wmKeyDown || msg.Message == wmSysKeyDown {
			vkPressed := uint32(msg.WParam)
			if vkPressed != vkReturn && vkPressed != vkEscape {
				st.capture(vkPressed, describe)
				continue
			}
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
		return st.mods, st.vk, true, st.cleared
	}
	return 0, 0, false, false
}

// capture records one keystroke and updates the dialog to match. A modifier
// pressed on its own is shown but never accepted: it is an incomplete
// combination, not a wrong one, so the echo follows the user's fingers while
// OK stays disabled.
func (st *captureState) capture(vk uint32, describe Describer) {
	mods := currentMods()
	if isModifierKey(vk) {
		vk = 0
	}

	display, problem := describe(mods, vk)
	st.mods, st.vk = mods, vk
	st.valid = problem == ""

	setText(st.echoHwnd, display)
	setText(st.problemHwnd, problem)
	if st.valid {
		procEnableWindow.Call(st.okHwnd, 1)
	} else {
		procEnableWindow.Call(st.okHwnd, 0)
	}
}
