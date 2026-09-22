//go:build windows

package execenv

import (
	"fmt"
	"os"
	"os/exec"
)

func createDirLink(src, dst string) error {
	if err := os.Symlink(src, dst); err == nil {
		return nil
	}
	return createJunction(src, dst)
}

func createJunction(src, dst string) error {
	output, err := exec.Command("cmd", "/c", "mklink", "/J", dst, src).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %s %s: %s: %w", dst, src, output, err)
	}
	return nil
}

func createFileLink(src, dst string) error {
	if err := os.Symlink(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}
