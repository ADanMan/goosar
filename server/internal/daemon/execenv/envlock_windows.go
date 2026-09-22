//go:build windows

package execenv

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func openEnvRootLockFile(path string) (*os.File, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func lockFileExclusiveNonBlocking(f *os.File) (ok bool, err error) {
	overlapped := new(windows.Overlapped)
	lockErr := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, overlapped,
	)
	switch {
	case lockErr == nil:
		return true, nil
	case errors.Is(lockErr, windows.ERROR_LOCK_VIOLATION), errors.Is(lockErr, windows.ERROR_IO_PENDING):
		return false, nil
	default:
		return false, lockErr
	}
}

func unlockFile(f *os.File) error {
	overlapped := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, overlapped)
}
