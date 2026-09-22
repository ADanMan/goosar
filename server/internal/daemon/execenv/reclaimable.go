package execenv

import "path/filepath"

const codexHomeDirName = "codex-home"

const sandboxBinDirName = ".sandbox-bin"

func ManagedReclaimableArtifactSubpaths() []string {
	reclaimable := make([]string, 0, 1)
	reclaimable = append(reclaimable, filepath.Join(codexHomeDirName, sandboxBinDirName))
	return reclaimable
}
