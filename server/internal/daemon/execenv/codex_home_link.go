//go:build !windows

package execenv

import "os"

func symlink(src, dst string) error {
	return os.Symlink(src, dst)
}

func createDirLink(src, dst string) error {
	return symlink(src, dst)
}

func createFileLink(src, dst string) error {
	return symlink(src, dst)
}
