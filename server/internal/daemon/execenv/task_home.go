package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

var taskHomeSeedEntries = []string{
	".gitconfig",
	".npmrc",
	".netrc",
	".git-credentials",
	".ssh",
}

var taskHomeConfigSeedEntries = []string{
	"gh",
	"git",
}

func seedSymlinks(srcDir, dstDir string, names []string, logger *slog.Logger) {
	for _, name := range names {
		src := filepath.Join(srcDir, name)
		if _, err := os.Lstat(src); err != nil {
			continue
		}
		if err := ensureSymlink(src, filepath.Join(dstDir, name)); err != nil && logger != nil {
			logger.Warn("execenv: task home: seed symlink failed", "entry", name, "error", err)
		}
	}
}

func prepareTaskHome(taskHome string, logger *slog.Logger) error {
	if err := os.MkdirAll(taskHome, 0o700); err != nil {
		return fmt.Errorf("create task home: %w", err)
	}

	sharedHome, err := os.UserHomeDir()
	if err != nil || sharedHome == "" {

		if logger != nil {
			logger.Warn("execenv: task home: no user home to seed credentials from", "error", err)
		}
		return nil
	}
	seedSymlinks(sharedHome, taskHome, taskHomeSeedEntries, logger)

	configDir := filepath.Join(taskHome, ".config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		if logger != nil {
			logger.Warn("execenv: task home: create .config failed", "error", err)
		}
		return nil
	}
	seedSymlinks(filepath.Join(sharedHome, ".config"), configDir, taskHomeConfigSeedEntries, logger)

	return nil
}

func TaskHomeEnv(taskHome string) map[string]string {
	return map[string]string{
		"HOME":             taskHome,
		"XDG_CACHE_HOME":   filepath.Join(taskHome, ".cache"),
		"XDG_CONFIG_HOME":  filepath.Join(taskHome, ".config"),
		"XDG_DATA_HOME":    filepath.Join(taskHome, ".local", "share"),
		"XDG_STATE_HOME":   filepath.Join(taskHome, ".local", "state"),
		"npm_config_cache": filepath.Join(taskHome, ".npm"),
	}
}

func needsRedirectedHome(goos, codexVersion string) bool {
	return goos == "linux" && codexSandboxPolicyFor(goos, codexVersion).Mode == "workspace-write"
}

func prepareCodexSandboxHome(envRoot, goos, codexVersion string, logger *slog.Logger) (taskHome string, writableRoots []string, err error) {
	if envRoot == "" {

		return "", nil, nil
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	if !needsRedirectedHome(goos, codexVersion) {
		return "", nil, nil
	}

	taskHome = filepath.Join(envRoot, "home")
	if err := prepareTaskHome(taskHome, logger); err != nil {
		return "", nil, err
	}
	return taskHome, []string{taskHome}, nil
}
