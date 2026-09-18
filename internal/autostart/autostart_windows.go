// Package autostart toggles the HKCU Run entry that starts the application at
// logon. The standard library has no registry support, so advapi32 is called
// directly.
package autostart

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	procRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	procRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	procRegQueryValueEx = advapi32.NewProc("RegQueryValueExW")
	procRegDeleteValueW = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

const (
	hkeyCurrentUser = 0x80000001
	keyRead         = 0x20019
	keyWrite        = 0x20006
	regSZ           = 1

	errorSuccess      = 0
	errorFileNotFound = 2
)

// Entry identifies one autostart registry value.
type Entry struct {
	KeyPath   string // relative to HKEY_CURRENT_USER
	ValueName string
}

// Default is the real autostart entry of the application.
func Default() Entry {
	return Entry{KeyPath: `Software\Microsoft\Windows\CurrentVersion\Run`, ValueName: "fenster"}
}

// Enabled reports whether the entry exists and points at exePath.
func (e Entry) Enabled(exePath string) (bool, error) {
	key, err := e.open(keyRead)
	if err != nil {
		return false, err
	}
	if key == 0 {
		return false, nil // key does not exist yet
	}
	defer procRegCloseKey.Call(key)

	name, err := syscall.UTF16PtrFromString(e.ValueName)
	if err != nil {
		return false, err
	}
	var kind, size uint32
	ret, _, _ := procRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(name)), 0,
		uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&size)))
	if ret == errorFileNotFound {
		return false, nil
	}
	if ret != errorSuccess {
		return false, fmt.Errorf("RegQueryValueExW: error %d", ret)
	}

	buf := make([]uint16, size/2+1)
	ret, _, _ = procRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(name)), 0,
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ret != errorSuccess {
		return false, fmt.Errorf("RegQueryValueExW: error %d", ret)
	}
	return strings.EqualFold(strings.Trim(syscall.UTF16ToString(buf), `"`), exePath), nil
}

// Enable writes the entry, creating the key if necessary.
func (e Entry) Enable(exePath string) error {
	key, err := e.create()
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(key)

	name, err := syscall.UTF16PtrFromString(e.ValueName)
	if err != nil {
		return err
	}
	value, err := syscall.UTF16FromString(`"` + exePath + `"`)
	if err != nil {
		return err
	}
	ret, _, _ := procRegSetValueExW.Call(key, uintptr(unsafe.Pointer(name)), 0, regSZ,
		uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)*2))
	if ret != errorSuccess {
		return fmt.Errorf("RegSetValueExW: error %d", ret)
	}
	return nil
}

// Disable removes the entry. A missing entry is not an error.
func (e Entry) Disable() error {
	key, err := e.open(keyWrite)
	if err != nil || key == 0 {
		return err
	}
	defer procRegCloseKey.Call(key)

	name, err := syscall.UTF16PtrFromString(e.ValueName)
	if err != nil {
		return err
	}
	ret, _, _ := procRegDeleteValueW.Call(key, uintptr(unsafe.Pointer(name)))
	if ret != errorSuccess && ret != errorFileNotFound {
		return fmt.Errorf("RegDeleteValueW: error %d", ret)
	}
	return nil
}

// open returns 0 without error when the key does not exist.
func (e Entry) open(access uint32) (uintptr, error) {
	path, err := syscall.UTF16PtrFromString(e.KeyPath)
	if err != nil {
		return 0, err
	}
	var key uintptr
	ret, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(path)), 0,
		uintptr(access), uintptr(unsafe.Pointer(&key)))
	if ret == errorFileNotFound {
		return 0, nil
	}
	if ret != errorSuccess {
		return 0, fmt.Errorf("RegOpenKeyExW(%s): error %d", e.KeyPath, ret)
	}
	return key, nil
}

func (e Entry) create() (uintptr, error) {
	path, err := syscall.UTF16PtrFromString(e.KeyPath)
	if err != nil {
		return 0, err
	}
	var key uintptr
	var disposition uint32
	ret, _, _ := procRegCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(path)), 0, 0, 0,
		keyWrite|keyRead, 0, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&disposition)))
	if ret != errorSuccess {
		return 0, fmt.Errorf("RegCreateKeyExW(%s): error %d", e.KeyPath, ret)
	}
	return key, nil
}
