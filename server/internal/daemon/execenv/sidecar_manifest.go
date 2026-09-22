package execenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const sidecarManifestFile = ".goosar_sidecar_manifest.json"

var errPathPreExists = errors.New("execenv: refuse to overwrite pre-existing path")

type sidecarManifest struct {
	Files []string `json:"files,omitempty"`
	Dirs  []string `json:"dirs,omitempty"`
}

func recordMkdirAll(path string, perm os.FileMode, m *sidecarManifest) error {
	if path == "" {
		return os.MkdirAll(path, perm)
	}
	if m == nil {
		return os.MkdirAll(path, perm)
	}

	var toCreate []string
	cur := filepath.Clean(path)
	for {
		if _, err := os.Lstat(cur); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("stat ancestor %s: %w", cur, err)
		}
		toCreate = append(toCreate, cur)
		parent := filepath.Dir(cur)
		if parent == cur || parent == "." {
			break
		}
		cur = parent
	}
	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}

	for i, j := 0, len(toCreate)-1; i < j; i, j = i+1, j-1 {
		toCreate[i], toCreate[j] = toCreate[j], toCreate[i]
	}
	m.Dirs = append(m.Dirs, toCreate...)
	return nil
}

func recordWriteFile(path string, data []byte, perm os.FileMode, m *sidecarManifest) error {
	if m == nil {
		return os.WriteFile(path, data, perm)
	}
	_, statErr := os.Lstat(path)
	if statErr == nil {

		return fmt.Errorf("%w: %s", errPathPreExists, path)
	}
	if !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("stat target %s: %w", path, statErr)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	m.Files = append(m.Files, path)
	return nil
}

func allocateCollisionFreeSkillDir(skillsParent, baseSlug string) (slug, dir string, err error) {
	const maxAttempts = 64
	for i := 0; i < maxAttempts; i++ {
		var candidate string
		switch {
		case i == 0:
			candidate = baseSlug
		case i == 1:
			candidate = baseSlug + "-goosar"
		default:
			candidate = fmt.Sprintf("%s-goosar-%d", baseSlug, i)
		}
		path := filepath.Join(skillsParent, candidate)
		if _, statErr := os.Lstat(path); statErr != nil {
			if errors.Is(statErr, fs.ErrNotExist) {
				return candidate, path, nil
			}
			return "", "", fmt.Errorf("stat candidate %s: %w", path, statErr)
		}
	}
	return "", "", fmt.Errorf("allocate collision-free skill dir under %s: exhausted %d attempts for base %q", skillsParent, maxAttempts, baseSlug)
}

func writeSidecarManifest(envRoot string, m *sidecarManifest) error {
	if envRoot == "" {
		return nil
	}
	if m == nil {
		m = &sidecarManifest{}
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal sidecar manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(envRoot, sidecarManifestFile), data, 0o644)
}

func CleanupSidecars(envRoot string) error {
	if envRoot == "" {
		return nil
	}
	manifestPath := filepath.Join(envRoot, sidecarManifestFile)
	data, err := os.ReadFile(manifestPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read sidecar manifest %s: %w", manifestPath, err)
	}
	var m sidecarManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parse sidecar manifest %s: %w", manifestPath, err)
	}

	return rollBackManifest(m, manifestPath)
}

func rollBackManifest(m sidecarManifest, manifestPath string) error {
	var firstErr error
	captureErr := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}

	for _, f := range m.Files {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			captureErr(fmt.Errorf("remove %s: %w", f, err))
		}
	}

	for i := len(m.Dirs) - 1; i >= 0; i-- {
		d := m.Dirs[i]
		err := os.Remove(d)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			continue
		}
		hasEntries, ok := dirHasEntries(d)
		switch {
		case !ok:

			captureErr(fmt.Errorf("rmdir %s: %w", d, err))
		case hasEntries:

		default:

			captureErr(fmt.Errorf("rmdir %s: %w", d, err))
		}
	}

	if manifestPath != "" {
		if err := os.Remove(manifestPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			captureErr(fmt.Errorf("remove manifest %s: %w", manifestPath, err))
		}
	}

	return firstErr
}

func rollBackPreparedSidecars(m sidecarManifest) error {
	return rollBackManifest(m, "")
}

func removeReusedManagedSkillDirs(envRoot, skillsParent string) error {
	if envRoot == "" || skillsParent == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(envRoot, sidecarManifestFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read sidecar manifest for reuse skill rollback: %w", err)
	}
	var m sidecarManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parse sidecar manifest for reuse skill rollback: %w", err)
	}

	cleanParent := filepath.Clean(skillsParent)
	var firstErr error
	for _, d := range m.Dirs {
		if filepath.Dir(filepath.Clean(d)) != cleanParent {
			continue
		}
		if err := os.RemoveAll(d); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("remove managed skill dir %s: %w", d, err)
		}
	}
	return firstErr
}

func dirHasEntries(dir string) (hasEntries bool, ok bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, true
		}
		return false, false
	}
	return len(entries) > 0, true
}
