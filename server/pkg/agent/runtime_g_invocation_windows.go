//go:build windows

package agent

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var powerShellLookup = defaultPowerShellLookup

var psFileFlags = []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass"}

func isBatchLauncher(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
		return true
	default:
		return false
	}
}

func rewriteCmdToPS1(toolName, lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	if !isBatchLauncher(lookedUp) {
		return "", nil, false
	}

	ps1 := filepath.Join(filepath.Dir(lookedUp), toolName+".ps1")
	info, err := os.Stat(ps1)
	if err != nil || info.IsDir() {
		return "", nil, false
	}

	psExe, ok := powerShellLookup()
	if !ok {
		return "", nil, false
	}

	full := make([]string, 0, len(psFileFlags)+2+len(args))
	full = append(full, psFileFlags...)
	full = append(full, "-File", ps1)
	full = append(full, args...)

	if logger != nil {
		logger.Info(toolName+": routing through powershell -File to preserve argv tokens",
			"powershell", psExe,
			"ps1", ps1,
			"original", lookedUp,
		)
	}
	return psExe, full, true
}

func platformRuntimeGInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	return rewriteCmdToPS1("cursor-agent", lookedUp, args, logger)
}

func defaultPowerShellLookup() (string, bool) {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, true
		}
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	candidate := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, true
	}
	return "", false
}
