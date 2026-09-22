package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/adanman/goosar/server/pkg/skillbundle"
)

type SkillBundleCache struct {
	root string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewSkillBundleCache(root string) *SkillBundleCache {
	return &SkillBundleCache{root: root, locks: make(map[string]*sync.Mutex)}
}

func (c *SkillBundleCache) Load(workspaceID string, ref SkillRefData) (SkillData, bool) {
	if c == nil || c.root == "" {
		return SkillData{}, false
	}
	entryPath := c.bundlePath(workspaceID, ref)
	raw, err := os.ReadFile(entryPath)
	if err != nil {
		return SkillData{}, false
	}
	var пакет SkillData
	if err := json.Unmarshal(raw, &пакет); err != nil || !validateSkillBundle(ref, пакет) {
		_ = os.Remove(entryPath)
		return SkillData{}, false
	}
	return пакет, true
}

func (c *SkillBundleCache) Store(workspaceID string, пакет SkillData) error {
	if c == nil || c.root == "" {
		return nil
	}
	ref := SkillRefData{ID: пакет.ID, Source: пакет.Source, Hash: пакет.Hash}
	destDir := filepath.Dir(c.bundlePath(workspaceID, ref))
	parent := filepath.Dir(destDir)

	stagingDir, err := os.MkdirTemp(parent, ".bundle-*")
	if err != nil {
		if mkErr := os.MkdirAll(parent, 0o755); mkErr != nil {
			return mkErr
		}
		stagingDir, err = os.MkdirTemp(parent, ".bundle-*")
		if err != nil {
			return err
		}
	}
	defer os.RemoveAll(stagingDir)

	encoded, err := json.Marshal(пакет)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "bundle.json"), encoded, 0o644); err != nil {
		return err
	}
	_ = os.RemoveAll(destDir)
	return os.Rename(stagingDir, destDir)
}

func (c *SkillBundleCache) WithRefLock(workspaceID string, ref SkillRefData, fn func() error) error {
	if c == nil {
		return fn()
	}
	lock := c.lockForKey(refLockKey(workspaceID, ref))
	lock.Lock()
	defer lock.Unlock()
	return fn()
}

func refLockKey(workspaceID string, ref SkillRefData) string {
	return strings.Join([]string{workspaceID, ref.Source, ref.ID, ref.Hash}, "\x00")
}

func (c *SkillBundleCache) lockForKey(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.locks[key]; ok {
		return existing
	}
	lock := &sync.Mutex{}
	c.locks[key] = lock
	return lock
}

func (c *SkillBundleCache) bundlePath(workspaceID string, ref SkillRefData) string {
	return filepath.Join(
		c.root,
		sanitizeCachePathSegment(workspaceID),
		sanitizeCachePathSegment(ref.Source),
		sanitizeCachePathSegment(ref.ID),
		sanitizeCachePathSegment(ref.Hash),
		"bundle.json",
	)
}

func validateSkillBundle(ref SkillRefData, пакет SkillData) bool {
	if пакет.ID != ref.ID || пакет.Source != ref.Source || пакет.Hash != ref.Hash {
		return false
	}
	if len(пакет.Files) != ref.FileCount {
		return false
	}

	files := make([]skillbundle.File, 0, len(пакет.Files))
	for _, f := range пакет.Files {
		if !isSafeRelativeSkillPath(f.Path) {
			return false
		}
		files = append(files, skillbundle.File{Path: f.Path, Content: f.Content})
	}

	manifest := skillbundle.BuildManifest(skillbundle.Skill{
		ID:          пакет.ID,
		Source:      пакет.Source,
		Name:        пакет.Name,
		Description: пакет.Description,
		Content:     пакет.Content,
		Files:       files,
	})
	if manifest.Hash != ref.Hash {
		return false
	}
	if ref.SizeBytes > 0 && manifest.SizeBytes != ref.SizeBytes {
		return false
	}
	return true
}

func isSafeRelativeSkillPath(p string) bool {
	if p == "" || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	cleaned := path.Clean(p)
	if cleaned != p || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	return true
}

func sanitizeCachePathSegment(s string) string {
	if s == "" {
		return "_"
	}
	var b strings.Builder
	for _, r := range s {
		if isCachePathSafeRune(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	segment := b.String()
	if segment == "." || segment == ".." {
		return fmt.Sprintf("_%s", segment)
	}
	return segment
}

func isCachePathSafeRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '-' || r == '_' || r == '.':
		return true
	default:
		return false
	}
}
