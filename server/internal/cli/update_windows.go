//go:build windows

package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/adanman/goosar/server/internal/selfexec"
)

const oldBinarySuffix = ".old"

func replaceBinary(tmpPath, exePath string) error {
	oldPath := exePath + oldBinarySuffix

	_ = os.Remove(oldPath)

	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("move running binary aside: %w", err)
	}

	if err := os.Rename(tmpPath, exePath); err != nil {

		if rerr := os.Rename(oldPath, exePath); rerr != nil {
			return fmt.Errorf("install new binary: %w (and failed to restore: %v)", err, rerr)
		}
		return fmt.Errorf("install new binary: %w", err)
	}

	return nil
}

func CleanupStaleUpdateArtifacts() {
	exePath, err := selfexec.Resolve()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	_ = os.Remove(exePath + oldBinarySuffix)
}
