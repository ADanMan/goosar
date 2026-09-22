package daemon

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const managedArtifactPatternPrefix = "managed:"

type artifactMatcher struct {
	basenames  map[string]struct{}
	exactPaths map[string]string

	exactBasenames map[string]struct{}
}

func newArtifactMatcher(patterns, managedSubpaths []string) artifactMatcher {
	exactPaths := make(map[string]string, len(managedSubpaths))
	exactBasenames := make(map[string]struct{}, len(managedSubpaths))
	for _, raw := range managedSubpaths {
		rel, ok := cleanManagedSubpath(raw)
		if !ok {
			continue
		}
		exactPaths[rel] = managedArtifactPatternPrefix + filepath.ToSlash(rel)
		exactBasenames[filepath.Base(rel)] = struct{}{}
	}
	return artifactMatcher{
		basenames:      buildPatternSet(patterns),
		exactPaths:     exactPaths,
		exactBasenames: exactBasenames,
	}
}

func (m artifactMatcher) matchDirectory(absRoot, path string, entry os.DirEntry) (string, bool) {
	name := entry.Name()
	_, mayBeExact := m.exactBasenames[name]
	_, isBasenameMatch := m.basenames[name]
	if !mayBeExact && !isBasenameMatch {
		return "", false
	}

	rel, err := filepath.Rel(absRoot, path)
	if err != nil {
		return "", false
	}
	rel, ok := cleanManagedSubpath(rel)
	if !ok {
		return "", false
	}
	if label, isExact := m.exactPaths[rel]; isExact {
		return label, true
	}
	if isBasenameMatch {
		return name, true
	}
	return "", false
}

func (m artifactMatcher) managedSubpaths() []string {
	paths := make([]string, 0, len(m.exactPaths))
	for rel := range m.exactPaths {
		paths = append(paths, filepath.ToSlash(rel))
	}
	sort.Strings(paths)
	return paths
}

func cleanManagedSubpath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return "", false
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	return cleaned, true
}
