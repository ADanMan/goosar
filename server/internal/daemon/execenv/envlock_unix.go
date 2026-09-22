//go:build !windows

package execenv

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openEnvRootLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
}

func lockFileExclusiveNonBlocking(f *os.File) (ok bool, err error) {
	switch flockErr := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); {
	case flockErr == nil:
		return true, nil
	case errors.Is(flockErr, unix.EWOULDBLOCK):
		return false, nil
	default:
		return false, flockErr
	}
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
