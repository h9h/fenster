package win32

import (
	"errors"
	"fmt"
	"syscall"
)

// Modifier bits for RegisterHotKey, as Windows defines them. They are
// exported so a caller can build the mods argument without redeclaring
// magic numbers; MOD_NOREPEAT is deliberately not among them, because
// RegisterHotKey adds it unconditionally rather than leaving it optional.
const (
	ModAlt     uint32 = 0x0001
	ModControl uint32 = 0x0002
	ModShift   uint32 = 0x0004
	ModWin     uint32 = 0x0008
)

// ErrHotkeyInUse reports that Windows refused the registration because
// another window or process already owns the combination
// (ERROR_HOTKEY_ALREADY_REGISTERED). Callers distinguish it from a genuine
// failure to decide whether to blame the user's choice or the system, so it
// is a sentinel to test with errors.Is rather than something to match on
// message text.
var ErrHotkeyInUse = errors.New("hotkey already in use")

// RegisterHotKey registers a system-wide hotkey that posts WM_HOTKEY to
// hwnd with id in the low word of wParam. id must be in the documented
// 0x0000–0xBFFF range for a window-owned hotkey. MOD_NOREPEAT is always
// added: see the modNoRepeat comment in syscalls_windows.go.
func RegisterHotKey(hwnd uintptr, id int32, mods, vk uint32) error {
	ret, _, err := procRegisterHotKey.Call(
		hwnd,
		uintptr(id),
		uintptr(mods|modNoRepeat),
		uintptr(vk),
	)
	if ret == 0 {
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == errorHotkeyAlreadyRegistered {
			return ErrHotkeyInUse
		}
		return fmt.Errorf("RegisterHotKey: %w", err)
	}
	return nil
}

// UnregisterHotKey releases the hotkey with the given id from hwnd. It must
// run on the same thread that registered it.
func UnregisterHotKey(hwnd uintptr, id int32) error {
	ret, _, err := procUnregisterHotKey.Call(hwnd, uintptr(id))
	if ret == 0 {
		return fmt.Errorf("UnregisterHotKey: %w", err)
	}
	return nil
}
