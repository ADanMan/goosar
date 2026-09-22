package execenv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

var codexSymlinkedFiles = []string{
	"auth.json",
}

var codexCopiedFiles = []string{
	"config.json",
	"config.toml",
	"instructions.md",
}

const (
	codexModelsCacheFile        = "models_cache.json"
	codexModelsCacheBindingFile = ".models_cache_config.sha256"
)

var codexModelsCacheConfigFiles = []string{
	"config.json",
	"config.toml",
}

type CodexHomeOptions struct {
	CodexVersion string

	GOOS string

	ResumeSessionID string

	IsLocalDirectory bool

	SessionStoreKey string

	WritableRoots []string

	CodexCustomArgs []string
}

func prepareCodexHome(codexHome string, logger *slog.Logger) error {
	return prepareCodexHomeWithOpts(codexHome, CodexHomeOptions{GOOS: "linux"}, logger)
}

type sharedConfigPresence int

const (
	sharedConfigAbsent sharedConfigPresence = iota

	sharedConfigPresent

	sharedConfigUndecidable
)

func statSharedCodexConfig(sharedHome string) sharedConfigPresence {
	if sharedHome == "" {
		return sharedConfigAbsent
	}
	_, err := os.Stat(filepath.Join(sharedHome, "config.toml"))
	switch {
	case err == nil:
		return sharedConfigPresent
	case os.IsNotExist(err):
		return sharedConfigAbsent
	default:
		return sharedConfigUndecidable
	}
}

func resolveWindowsSandboxState(configFile string, configSyncErr error, sharedPresence sharedConfigPresence, customArgs []string, logger *slog.Logger) windowsSandboxConfig {
	configState := classifyPerTaskWindowsSandbox(configFile, configSyncErr, sharedPresence)
	state := resolveWindowsSandbox(configState, windowsSandboxFromCustomArgs(customArgs))
	if state == windowsSandboxUndecidable && logger != nil {
		logger.Error("codex sandbox: cannot determine Windows native sandbox config; keeping workspace-write and refusing to loosen to danger-full-access",
			"config_file", configFile)
	}
	return state
}

func classifyPerTaskWindowsSandbox(configFile string, configSyncErr error, sharedPresence sharedConfigPresence) windowsSandboxConfig {

	if configSyncErr != nil {
		return windowsSandboxUndecidable
	}
	data, err := os.ReadFile(configFile)
	switch {
	case err == nil:
		return windowsSandboxFromConfig(string(data))
	case os.IsNotExist(err):

		if sharedPresence == sharedConfigAbsent {
			return windowsSandboxAbsent
		}
		return windowsSandboxUndecidable
	default:

		return windowsSandboxUndecidable
	}
}

func prepareCodexHomeWithOpts(codexHome string, opts CodexHomeOptions, logger *slog.Logger) error {
	sharedHome := resolveSharedCodexHome()
	freshHome := false
	if _, err := os.Lstat(codexHome); os.IsNotExist(err) {
		freshHome = true
	}

	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		return fmt.Errorf("create codex-home dir: %w", err)
	}

	if err := prepareCodexSessionsDir(codexHome, sharedHome, opts, logger); err != nil {
		logger.Warn("execenv: codex-home sessions dir prepare failed", "error", err)
	}

	for _, name := range codexSymlinkedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := ensureSymlink(src, dst); err != nil {
			logger.Warn("execenv: codex-home symlink failed", "file", name, "error", err)
		}
	}

	logCodexAuthState(filepath.Join(codexHome, "auth.json"), logger)

	var configSyncErr error
	for _, name := range codexCopiedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := syncCopiedFile(src, dst); err != nil {
			logger.Warn("execenv: codex-home sync failed", "file", name, "error", err)
			if name == "config.toml" {
				configSyncErr = err
			}
		}
	}

	if err := sanitizeCopiedCodexConfig(filepath.Join(codexHome, "config.toml")); err != nil {
		logger.Warn("execenv: codex-home sanitize config failed", "error", err)
	}

	if err := syncCodexModelCatalog(codexHome, sharedHome); err != nil {
		return fmt.Errorf("sync codex model_catalog_json: %w", err)
	}

	if err := syncCodexModelsCache(codexHome, sharedHome, freshHome); err != nil {
		logger.Warn("execenv: codex-home models cache sync failed; discarding cache", "error", err)
		if removeErr := os.RemoveAll(filepath.Join(codexHome, codexModelsCacheFile)); removeErr != nil {
			return fmt.Errorf("sync codex models cache: %v; discard unsafe cache: %w", err, removeErr)
		}
	}

	if err := exposeSharedCodexPluginCache(codexHome, sharedHome); err != nil {
		logger.Warn("execenv: codex-home plugin cache exposure failed", "error", err)
	}

	configFile := filepath.Join(codexHome, "config.toml")
	winState := windowsSandboxAbsent
	if resolveGOOS(opts.GOOS) == "windows" {
		winState = resolveWindowsSandboxState(configFile, configSyncErr, statSharedCodexConfig(sharedHome), opts.CodexCustomArgs, logger)
	}
	policy := codexSandboxPolicyForConfig(opts.GOOS, opts.CodexVersion, winState)
	policy.WritableRoots = opts.WritableRoots
	if err := ensureCodexSandboxConfig(configFile, policy, opts.CodexVersion, logger); err != nil {

		return fmt.Errorf("ensure codex sandbox config: %w", err)
	}

	if err := ensureCodexMultiAgentConfig(filepath.Join(codexHome, "config.toml"), logger); err != nil {
		logger.Warn("execenv: codex-home ensure multi-agent config failed", "error", err)
	}

	if err := ensureCodexMemoryConfig(filepath.Join(codexHome, "config.toml"), logger); err != nil {
		logger.Warn("execenv: codex-home ensure memory config failed", "error", err)
	}

	return nil
}

func resolveSharedCodexHome() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		abs, err := filepath.Abs(v)
		if err == nil {
			return abs
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".codex")
	}
	return filepath.Join(home, ".codex")
}

var codexSessionStateGlobs = []string{
	"state_*.sqlite",
	"state_*.sqlite-shm",
	"state_*.sqlite-wal",
}

const codexSessionStoreRoot = "goosar-sessions"

func codexSessionStoreDir(sharedHome, key string) string {
	if key == "" {
		return ""
	}
	return filepath.Join(sharedHome, codexSessionStoreRoot, key)
}

func codexSessionStoreNamespace(profile string) string {
	if profile == "" {
		return "default"
	}
	sum := sha256.Sum256([]byte(profile))
	return "p_" + hex.EncodeToString(sum[:])
}

func codexSessionStoreKey(profile, agentID, issueID string) string {
	issue := sanitizeCodexPathSegment(issueID)
	if issue == "" {
		return ""
	}
	agent := sanitizeCodexPathSegment(agentID)
	if agent == "" {
		agent = "_"
	}
	return filepath.Join(codexSessionStoreNamespace(profile), agent, issue)
}

func sanitizeCodexPathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func PruneCodexSessionStores(profile string, retention time.Duration, now time.Time, reserve func(storeDir string) (commit func(), ok bool), logger *slog.Logger) (removed int, bytesFreed int64) {
	if retention <= 0 {
		return 0, 0
	}
	root := filepath.Join(resolveSharedCodexHome(), codexSessionStoreRoot, codexSessionStoreNamespace(profile))
	agents, err := os.ReadDir(root)
	if err != nil {
		return 0, 0
	}
	for _, a := range agents {
		if !a.IsDir() {
			continue
		}
		agentDir := filepath.Join(root, a.Name())
		issues, err := os.ReadDir(agentDir)
		if err != nil {
			continue
		}
		kept := 0
		for _, is := range issues {
			if !is.IsDir() {
				continue
			}
			storeDir := filepath.Join(agentDir, is.Name())
			newest, size := codexStoreStat(storeDir)
			if newest.IsZero() || now.Sub(newest) <= retention {
				kept++
				continue
			}

			var commit func()
			if reserve != nil {
				c, ok := reserve(storeDir)
				if !ok {
					kept++
					continue
				}
				commit = c
			}
			err := os.RemoveAll(storeDir)
			if commit != nil {
				commit()
			}
			if err != nil {
				logger.Warn("execenv: prune codex session store failed", "store", storeDir, "error", err)
				kept++
				continue
			}
			removed++
			bytesFreed += size
		}

		if kept == 0 {
			_ = os.Remove(agentDir)
		}
	}
	return removed, bytesFreed
}

func codexStoreStat(dir string) (newest time.Time, size int64) {
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		if !d.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return newest, size
}

func prepareCodexSessionsDir(codexHome, sharedHome string, opts CodexHomeOptions, logger *slog.Logger) error {
	dst := filepath.Join(codexHome, "sessions")
	sharedSessions := filepath.Join(sharedHome, "sessions")
	storeDir := codexSessionStoreDir(sharedHome, opts.SessionStoreKey)

	if opts.IsLocalDirectory {
		if storeDir == "" {

			return os.MkdirAll(dst, 0o755)
		}
		return linkCodexSessionsToStore(dst, storeDir, sharedSessions, opts.ResumeSessionID, logger)
	}

	fi, err := os.Lstat(dst)
	switch {
	case os.IsNotExist(err):
		return os.MkdirAll(dst, 0o755)
	case err != nil:
		return fmt.Errorf("stat sessions dir %s: %w", dst, err)
	}

	if fi.Mode()&os.ModeSymlink == 0 {

		return os.MkdirAll(dst, 0o755)
	}

	if storeDir != "" {
		if target, rlErr := os.Readlink(dst); rlErr == nil && sameCodexPath(target, storeDir) {
			return linkCodexSessionsToStore(dst, storeDir, sharedSessions, opts.ResumeSessionID, logger)
		}
	}

	if err := os.Remove(dst); err != nil {
		return fmt.Errorf("remove legacy sessions symlink %s: %w", dst, err)
	}
	resetCodexSessionState(codexHome, logger)

	if opts.ResumeSessionID != "" && storeDir != "" {
		logger.Info("execenv: migrated codex-home sessions from shared symlink to per-issue store",
			"codex_home", codexHome, "resume_session", true)
		return linkCodexSessionsToStore(dst, storeDir, sharedSessions, opts.ResumeSessionID, logger)
	}
	logger.Info("execenv: migrated codex-home sessions from shared symlink to task-local dir",
		"codex_home", codexHome, "resume_session", false)
	return os.MkdirAll(dst, 0o755)
}

func linkCodexSessionsToStore(dst, storeDir, sharedSessions, resumeID string, logger *slog.Logger) error {
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return fmt.Errorf("create codex session store %s: %w", storeDir, err)
	}
	if resumeID != "" && len(findCodexRollouts(storeDir, resumeID)) == 0 {
		if err := exposeResumeRollout(sharedSessions, storeDir, resumeID, logger); err != nil {
			logger.Warn("execenv: bootstrap resume rollout into session store failed; task will fall back to a fresh thread",
				"session_id", resumeID, "error", err)
		}
	}
	if err := ensureCodexSessionsLink(dst, storeDir); err != nil {
		return err
	}

	touchCodexSessionStore(storeDir, logger)
	return nil
}

func touchCodexSessionStore(storeDir string, logger *slog.Logger) {
	now := time.Now()
	if err := os.Chtimes(storeDir, now, now); err != nil {
		logger.Warn("execenv: refresh codex session store activity failed", "store", storeDir, "error", err)
	}
}

func CodexSessionStorePath(profile, agentID, issueID string) string {
	key := codexSessionStoreKey(profile, agentID, issueID)
	if key == "" {
		return ""
	}
	return codexSessionStoreDir(resolveSharedCodexHome(), key)
}

func sameCodexPath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func resetCodexSessionState(codexHome string, logger *slog.Logger) {
	for _, pattern := range codexSessionStateGlobs {
		matches, err := filepath.Glob(filepath.Join(codexHome, pattern))
		if err != nil {
			continue
		}
		for _, m := range matches {
			if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
				logger.Warn("execenv: codex-home reset session state failed", "path", m, "error", err)
			}
		}
	}
}

func ensureCodexSessionsLink(dst, src string) error {
	if err := os.MkdirAll(src, 0o755); err != nil {
		return fmt.Errorf("create codex session store %s: %w", src, err)
	}
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, rlErr := os.Readlink(dst); rlErr == nil && sameCodexPath(target, src) {
				return nil
			}
		}
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("remove stale sessions path %s: %w", dst, err)
		}
	}
	return createDirLink(src, dst)
}

func codexRolloutGlobs(sessionsDir, sessionID string) []string {
	name := "rollout-*-" + sessionID + ".jsonl*"
	return []string{
		filepath.Join(sessionsDir, name),
		filepath.Join(sessionsDir, "*", "*", "*", name),
	}
}

func findCodexRollouts(sessionsDir, sessionID string) []string {
	if sessionsDir == "" || sessionID == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, pattern := range codexRolloutGlobs(sessionsDir, sessionID) {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

func CodexResumeRolloutPresent(codexHome, sessionID string) bool {
	if codexHome == "" || sessionID == "" {
		return false
	}
	return len(findCodexRollouts(filepath.Join(codexHome, "sessions"), sessionID)) > 0
}

func exposeResumeRollout(sharedSessions, localSessions, sessionID string, logger *slog.Logger) error {
	matches := findCodexRollouts(sharedSessions, sessionID)
	if len(matches) == 0 {
		return fmt.Errorf("no rollout found for session %s under %s", sessionID, sharedSessions)
	}
	linked := 0
	for _, src := range matches {
		rel, err := filepath.Rel(sharedSessions, src)
		if err != nil {
			rel = filepath.Base(src)
		}
		dst := filepath.Join(localSessions, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create rollout dir %s: %w", filepath.Dir(dst), err)
		}
		if err := linkCodexRollout(src, dst); err != nil {
			return fmt.Errorf("link rollout %s: %w", src, err)
		}
		linked++
	}
	logger.Info("execenv: exposed resume rollout into task-local sessions", "session_id", sessionID, "files", linked)
	return nil
}

func linkCodexRollout(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return os.Symlink(src, dst)
}

func syncCodexModelCatalog(codexHome, sharedHome string) error {
	configPath := filepath.Join(codexHome, "config.toml")
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	var конфиг struct {
		ModelCatalogJSON string `toml:"model_catalog_json"`
	}
	if err := toml.Unmarshal(data, &конфиг); err != nil {
		return fmt.Errorf("parse %s: %w", configPath, err)
	}
	catalogPath := strings.TrimSpace(конфиг.ModelCatalogJSON)
	if catalogPath == "" {
		return nil
	}

	src, err := resolveCodexConfigPath(catalogPath, sharedHome)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("model_catalog_json %q resolved to missing file %s: %w", catalogPath, src, err)
	}

	if filepath.IsAbs(catalogPath) || strings.HasPrefix(catalogPath, "~") {
		return nil
	}
	cleanCatalogPath := filepath.Clean(catalogPath)
	if !filepath.IsLocal(cleanCatalogPath) {
		return fmt.Errorf("model_catalog_json %q must be a local relative path or an absolute path", catalogPath)
	}
	dst := filepath.Join(codexHome, cleanCatalogPath)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create model catalog directory %s: %w", filepath.Dir(dst), err)
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("remove stale model catalog %s: %w", dst, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat model catalog %s: %w", dst, err)
	}
	if err := copyFile(src, dst); err != nil {
		return fmt.Errorf("copy model_catalog_json %s to %s: %w", src, dst, err)
	}
	return nil
}

func syncCodexModelsCache(codexHome, sharedHome string, freshHome bool) error {
	fingerprint, err := codexModelsCacheConfigFingerprint(sharedHome)
	if err != nil {
		return err
	}

	bindingPath := filepath.Join(codexHome, codexModelsCacheBindingFile)
	previous, bound, err := readCodexModelsCacheBinding(bindingPath)
	if err != nil {
		return err
	}

	cachePath := filepath.Join(codexHome, codexModelsCacheFile)
	cacheInfo, cacheErr := os.Lstat(cachePath)
	cacheExists := cacheErr == nil
	if cacheErr != nil && !os.IsNotExist(cacheErr) {
		return fmt.Errorf("stat codex models cache %s: %w", cachePath, cacheErr)
	}

	if bound && previous == fingerprint {

		if cacheExists && !cacheInfo.Mode().IsRegular() {
			if err := os.RemoveAll(cachePath); err != nil {
				return fmt.Errorf("remove non-regular codex models cache %s: %w", cachePath, err)
			}
		}
		return nil
	}

	if cacheExists {
		if err := os.RemoveAll(cachePath); err != nil {
			return fmt.Errorf("remove unbound codex models cache %s: %w", cachePath, err)
		}
	}

	if freshHome && !bound && !cacheExists {

		if err := seedCopiedFile(filepath.Join(sharedHome, codexModelsCacheFile), cachePath); err != nil {
			return fmt.Errorf("seed codex models cache: %w", err)
		}
	}

	if err := writeCodexModelsCacheBinding(bindingPath, fingerprint); err != nil {
		return err
	}
	return nil
}

func codexModelsCacheConfigFingerprint(sharedHome string) (string, error) {
	h := sha256.New()
	var configTOML []byte

	for _, name := range codexModelsCacheConfigFiles {
		path := filepath.Join(sharedHome, name)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			fmt.Fprintf(h, "%s\x00missing\x00", name)
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read codex model cache config %s: %w", path, err)
		}
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(data))
		_, _ = h.Write(data)
		if name == "config.toml" {
			configTOML = data
		}
	}

	if len(configTOML) > 0 {
		var конфиг struct {
			ModelCatalogJSON string `toml:"model_catalog_json"`
		}
		if err := toml.Unmarshal(configTOML, &конфиг); err != nil {
			return "", fmt.Errorf("parse codex model cache config %s: %w", filepath.Join(sharedHome, "config.toml"), err)
		}
		catalogPath := strings.TrimSpace(конфиг.ModelCatalogJSON)
		if catalogPath != "" {
			resolved, err := resolveCodexConfigPath(catalogPath, sharedHome)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				return "", fmt.Errorf("read model_catalog_json %s: %w", resolved, err)
			}
			fmt.Fprintf(h, "model_catalog_json\x00%d\x00", len(data))
			_, _ = h.Write(data)
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func readCodexModelsCacheBinding(path string) (fingerprint string, bound bool, err error) {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("stat codex models cache binding %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		if err := os.RemoveAll(path); err != nil {
			return "", false, fmt.Errorf("remove non-regular codex models cache binding %s: %w", path, err)
		}
		return "", false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read codex models cache binding %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), true, nil
}

func writeCodexModelsCacheBinding(path, fingerprint string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove prior codex models cache binding %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create codex models cache binding %s: %w", path, err)
	}
	if _, err := io.WriteString(f, fingerprint+"\n"); err != nil {
		_ = f.Close()
		return fmt.Errorf("write codex models cache binding %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close codex models cache binding %s: %w", path, err)
	}
	return nil
}

func resolveCodexConfigPath(configPath, sharedHome string) (string, error) {
	if filepath.IsAbs(configPath) {
		return filepath.Clean(configPath), nil
	}
	if strings.HasPrefix(configPath, "~/") || strings.HasPrefix(configPath, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve model_catalog_json %q: user home: %w", configPath, err)
		}
		return filepath.Join(home, configPath[2:]), nil
	}
	if strings.HasPrefix(configPath, "~") {
		return "", fmt.Errorf("model_catalog_json %q uses unsupported ~user expansion", configPath)
	}
	return filepath.Join(sharedHome, filepath.Clean(configPath)), nil
}

func exposeSharedCodexPluginCache(codexHome, sharedHome string) error {
	src := filepath.Join(sharedHome, "plugins", "cache")
	dst := filepath.Join(codexHome, "plugins", "cache")
	if err := os.MkdirAll(src, 0o755); err != nil {
		return fmt.Errorf("create shared plugin cache dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create codex plugin dir: %w", err)
	}

	if fi, err := os.Lstat(dst); err == nil {
		isLink := fi.Mode()&os.ModeSymlink != 0
		if isLink {
			if target, readlinkErr := os.Readlink(dst); readlinkErr == nil && target == src {
				return nil
			}
			if err := os.Remove(dst); err != nil {
				return fmt.Errorf("remove stale plugin cache link: %w", err)
			}
		} else {
			if err := os.RemoveAll(dst); err != nil {
				return fmt.Errorf("remove stale plugin cache path: %w", err)
			}
		}
	}

	if err := createDirLink(src, dst); err != nil {
		return fmt.Errorf("expose shared plugin cache: %w", err)
	}
	return nil
}

func ensureSymlink(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}

	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(dst); err == nil && target == src {
				return nil
			}
		}

		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("remove stale dst %s: %w", dst, err)
		}
	}

	return createFileLink(src, dst)
}

func logCodexAuthState(authPath string, logger *slog.Logger) {
	fi, err := os.Lstat(authPath)
	if err != nil {
		logger.Info("execenv: codex auth.json absent", "path", authPath, "error", err)
		return
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(authPath)
		logger.Info("execenv: codex auth.json is symlink", "path", authPath, "target", target)
		return
	}
	logger.Info("execenv: codex auth.json is regular file",
		"path", authPath,
		"size", fi.Size(),
		"mtime", fi.ModTime().UTC(),
	)
}

func syncCopiedFile(src, dst string) error {
	_, srcErr := os.Stat(src)
	srcMissing := os.IsNotExist(srcErr)
	if srcErr != nil && !srcMissing {
		return fmt.Errorf("stat src %s: %w", src, srcErr)
	}

	if _, err := os.Lstat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("remove stale dst %s: %w", dst, err)
		}
	}

	if srcMissing {
		return nil
	}
	return copyFile(src, dst)
}

func seedCopiedFile(src, dst string) error {
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode().IsRegular() {
			return nil
		}
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("remove non-regular dst %s: %w", dst, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat dst %s: %w", dst, err)
	}

	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat src %s: %w", src, err)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s → %s: %w", src, dst, err)
	}
	return nil
}
