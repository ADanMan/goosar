package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const localDirectoryResourceType = "local_directory"

type localDirectoryRef struct {
	LocalPath string `json:"local_path"`
	DaemonID  string `json:"daemon_id"`
	Label     string `json:"label,omitempty"`
}

type localDirectoryAssignment struct {
	Ref      localDirectoryRef
	AbsPath  string
	RealPath string
}

func localDirectoryAssignmentForTask(task Task, daemonID string) (*localDirectoryAssignment, error) {
	if task.IsLeaderTask {
		return nil, nil
	}
	return findLocalDirectoryAssignment(task.ProjectResources, daemonID)
}

func findLocalDirectoryAssignment(resources []ProjectResourceData, daemonID string) (*localDirectoryAssignment, error) {
	var match *localDirectoryAssignment
	for _, r := range resources {
		if r.ResourceType != localDirectoryResourceType {
			continue
		}
		var ref localDirectoryRef
		if err := json.Unmarshal(r.ResourceRef, &ref); err != nil {
			return nil, fmt.Errorf("local_directory: parse resource_ref: %w", err)
		}
		ref.DaemonID = strings.TrimSpace(ref.DaemonID)
		if ref.DaemonID == "" {
			return nil, errors.New("local_directory: resource_ref missing daemon_id")
		}
		if ref.DaemonID != daemonID {

			continue
		}
		if match != nil {

			return nil, fmt.Errorf(
				"local_directory: project has multiple local_directory resources for this daemon (%q and %q); remove the extra in project settings",
				match.AbsPath,
				strings.TrimSpace(ref.LocalPath),
			)
		}
		absPath, err := normalizeLocalPath(ref.LocalPath)
		if err != nil {
			return nil, err
		}
		realPath, err := resolveRealPath(absPath)
		if err != nil {
			return nil, err
		}
		match = &localDirectoryAssignment{
			Ref:      ref,
			AbsPath:  absPath,
			RealPath: realPath,
		}
	}
	return match, nil
}

func normalizeLocalPath(p string) (string, error) {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return "", errors.New("local_directory: local_path is empty")
	}
	if !filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("local_directory: local_path must be absolute, got %q", trimmed)
	}
	return filepath.Clean(trimmed), nil
}

func resolveRealPath(absPath string) (string, error) {
	real, err := filepath.EvalSymlinks(absPath)
	if err != nil {

		return absPath, nil
	}
	return real, nil
}

func validateLocalPath(absPath string) error {
	if absPath == "" {
		return errors.New("local_directory: local_path is empty")
	}
	if !filepath.IsAbs(absPath) {
		return fmt.Errorf("local_directory: local_path must be absolute, got %q", absPath)
	}
	if reason, blocked := isBlacklistedLocalPath(absPath); blocked {
		return fmt.Errorf("local_directory: %s (%q)", reason, absPath)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("local_directory: path does not exist: %q", absPath)
		}
		return fmt.Errorf("local_directory: stat %q: %w", absPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("local_directory: path is not a directory: %q", absPath)
	}

	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return fmt.Errorf("local_directory: resolve symlinks for %q: %w", absPath, err)
	}
	realPath = filepath.Clean(realPath)
	if reason, blocked := isBlacklistedRealPath(realPath); blocked {
		if realPath != filepath.Clean(absPath) {
			return fmt.Errorf("local_directory: %s (symlink target of %q is %q)", reason, absPath, realPath)
		}
		return fmt.Errorf("local_directory: %s (canonical path %q)", reason, absPath)
	}
	if err := checkConfinementRoots(absPath, realPath); err != nil {
		return err
	}
	if err := checkDirReadWrite(absPath); err != nil {
		return fmt.Errorf("local_directory: %w", err)
	}
	return nil
}

const EnvAllowedWorkdirRoots = "GOOSAR_ALLOWED_WORKDIR_ROOTS"

func checkConfinementRoots(absPath, realPath string) error {
	roots, configured := configuredWorkdirRoots()
	if !configured {
		return nil
	}
	for _, root := range roots {
		if pathWithinRoot(realPath, root) {
			return nil
		}
	}
	return fmt.Errorf("local_directory: %q resolves outside every directory this deployment confines agents to (%s=%s)",
		absPath, EnvAllowedWorkdirRoots, strings.Join(roots, string(os.PathListSeparator)))
}

func configuredWorkdirRoots() (roots []string, configured bool) {
	raw := strings.TrimSpace(os.Getenv(EnvAllowedWorkdirRoots))
	if raw == "" {
		return nil, false
	}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == os.PathListSeparator || r == ','
	}) {
		entry := strings.TrimSpace(part)
		if entry == "" || !filepath.IsAbs(entry) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(entry)
		if err != nil {
			continue
		}
		roots = append(roots, filepath.Clean(resolved))
	}
	return roots, true
}

func pathWithinRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || filepath.IsLocal(rel)
}

func isBlacklistedLocalPath(absPath string) (reason string, blocked bool) {
	cleaned := filepath.Clean(absPath)
	if isDriveRoot(cleaned) {
		return fmt.Sprintf("path is a drive root %q", cleaned), true
	}
	for _, banned := range systemRootBlacklist() {
		if cleaned == banned {
			return fmt.Sprintf("path is a protected system root %q", banned), true
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if cleaned == filepath.Clean(home) {
			return "path is the user's home directory", true
		}
	}
	return "", false
}

func isBlacklistedRealPath(realPath string) (reason string, blocked bool) {
	realClean := filepath.Clean(realPath)
	if isDriveRoot(realClean) {
		return fmt.Sprintf("path is a drive root %q", realClean), true
	}
	for _, banned := range systemRootBlacklist() {
		bannedClean := filepath.Clean(banned)
		if realClean == bannedClean {
			return fmt.Sprintf("path is a protected system root %q", banned), true
		}
		if r, err := filepath.EvalSymlinks(banned); err == nil {
			if filepath.Clean(r) == realClean {
				return fmt.Sprintf("path is a protected system root %q", banned), true
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		homeClean := filepath.Clean(home)
		if realClean == homeClean {
			return "path is the user's home directory", true
		}
		if r, err := filepath.EvalSymlinks(home); err == nil {
			if filepath.Clean(r) == realClean {
				return "path is the user's home directory", true
			}
		}
	}
	return "", false
}

func isDriveRoot(absPath string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	vol := filepath.VolumeName(absPath)
	if vol == "" {
		return false
	}

	rest := absPath[len(vol):]
	return rest == "" || rest == `\` || rest == "/"
}

func systemRootBlacklist() []string {
	if runtime.GOOS == "windows" {
		return []string{`C:\Users`, `C:\ProgramData`, `C:\Program Files`, `C:\Program Files (x86)`, `C:\Windows`}
	}
	return []string{"/", "/Users", "/Users/Shared", "/home", "/root", "/var", "/etc", "/tmp", "/usr", "/opt"}
}

func checkDirReadWrite(dir string) error {
	if _, err := os.ReadDir(dir); err != nil {
		return fmt.Errorf("read %q: %w", dir, err)
	}
	probe, err := os.CreateTemp(dir, ".goosar-rwcheck-*")
	if err != nil {
		return fmt.Errorf("write %q: %w", dir, err)
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)
	return nil
}

func isGitWorkTree(ctx context.Context, path string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

type LocalPathLocker struct {
	mu    sync.Mutex
	locks map[string]*pathLockEntry
}

type pathLockEntry struct {
	mu       sync.Mutex
	mu2      sync.Mutex
	holderID string
}

func NewLocalPathLocker() *LocalPathLocker {
	return &LocalPathLocker{locks: make(map[string]*pathLockEntry)}
}

func (l *LocalPathLocker) Holder(realPath string) string {
	l.mu.Lock()
	entry, ok := l.locks[realPath]
	l.mu.Unlock()
	if !ok {
		return ""
	}
	entry.mu2.Lock()
	defer entry.mu2.Unlock()
	return entry.holderID
}

func (l *LocalPathLocker) Acquire(ctx context.Context, realPath, taskID string, onWait func(holder string)) (func(), error) {
	if realPath == "" {
		return nil, errors.New("local_directory: realpath required for lock")
	}
	if taskID == "" {
		return nil, errors.New("local_directory: taskID required for lock")
	}

	l.mu.Lock()
	entry, ok := l.locks[realPath]
	if !ok {
		entry = &pathLockEntry{}
		l.locks[realPath] = entry
	}
	l.mu.Unlock()

	if entry.mu.TryLock() {
		entry.mu2.Lock()
		entry.holderID = taskID
		entry.mu2.Unlock()
		return l.releaser(realPath, entry), nil
	}

	if onWait != nil {
		entry.mu2.Lock()
		holder := entry.holderID
		entry.mu2.Unlock()
		onWait(holder)
	}

	acquired := make(chan struct{})
	go func() {
		entry.mu.Lock()
		close(acquired)
	}()

	select {
	case <-acquired:
		entry.mu2.Lock()
		entry.holderID = taskID
		entry.mu2.Unlock()
		return l.releaser(realPath, entry), nil
	case <-ctx.Done():

		go func() {
			<-acquired
			entry.mu.Unlock()
		}()
		return nil, ctx.Err()
	}
}

func (l *LocalPathLocker) releaser(realPath string, entry *pathLockEntry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			entry.mu2.Lock()
			entry.holderID = ""
			entry.mu2.Unlock()
			entry.mu.Unlock()

			_ = realPath
		})
	}
}
