package win32

import (
	"fmt"
	"syscall"
	"unsafe"
)

// AcquireSingleInstance creates (or opens) a named mutex identifying one
// running instance of the application. If the mutex already existed —
// CreateMutexW leaves ERROR_ALREADY_EXISTS as the last error in that case —
// alreadyRunning is true and the caller must not proceed to act as if it
// owned the instance; release is a no-op in that case, the handle having
// already been closed. Otherwise release closes the mutex handle and should
// be deferred by the caller for the remaining lifetime of the process. The
// caller never sees the raw handle.
func AcquireSingleInstance(name string) (release func(), alreadyRunning bool, err error) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return func() {}, false, fmt.Errorf("mutex name: %w", err)
	}

	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(namePtr)))
	if h == 0 {
		return func() {}, false, fmt.Errorf("CreateMutexW: %w", callErr)
	}

	if errno, ok := callErr.(syscall.Errno); ok && errno == errorAlreadyExists {
		procCloseHandle.Call(h)
		return func() {}, true, nil
	}

	return func() { procCloseHandle.Call(h) }, false, nil
}
