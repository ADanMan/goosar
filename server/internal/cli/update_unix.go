//go:build !windows

package cli

import "os"

func replaceBinary(tmpPath, exePath string) error {
	return os.Rename(tmpPath, exePath)
}

func CleanupStaleUpdateArtifacts() {}
