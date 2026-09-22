package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/cli"
)

func TestPatternsFromEnv_DefaultsWhenUnset(t *testing.T) {
	t.Setenv("GOOSAR_GC_ARTIFACT_PATTERNS", "")
	defaults := []string{"node_modules", ".next", ".turbo"}
	got := patternsFromEnv("GOOSAR_GC_ARTIFACT_PATTERNS", defaults)
	if !reflect.DeepEqual(got, defaults) {
		t.Fatalf("expected defaults %v, got %v", defaults, got)
	}

	got[0] = "mutated"
	if defaults[0] == "mutated" {
		t.Fatal("patternsFromEnv must not return a slice aliased with defaults")
	}
}

func TestDefaultGCIntervalIsTwoHours(t *testing.T) {
	if DefaultGCInterval != 2*time.Hour {
		t.Fatalf("DefaultGCInterval = %s, want 2h", DefaultGCInterval)
	}
}

func TestPatternsFromEnv_DropsSeparatorBearingEntries(t *testing.T) {
	t.Setenv("GOOSAR_GC_ARTIFACT_PATTERNS", "node_modules, .next ,foo/bar, ../etc, ,target")
	got := patternsFromEnv("GOOSAR_GC_ARTIFACT_PATTERNS", nil)
	want := []string{"node_modules", ".next", "target"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestIsSafeAgentName(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"claude", true},
		{"cursor-agent", true},
		{"kiro_cli", true},
		{"v1.2", true},
		{"Claude2", true},
		{"", false},
		{"a b", false},
		{"a/b", false},
		{"a;b", false},
		{"a$b", false},
		{"a`b", false},
		{"a'b", false},
		{`a"b`, false},
	} {
		if got := isSafeAgentName(tc.in); got != tc.want {
			t.Errorf("isSafeAgentName(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestBuildLoginShellResolveScript_ShapeAndContent(t *testing.T) {
	got := buildLoginShellResolveScript([]string{"claude", "cursor-agent"})

	if !strings.Contains(got, "for n in claude cursor-agent;") {
		t.Errorf("script missing expected for-loop header:\n%s", got)
	}

	idxUnalias := strings.Index(got, `unalias "$n" 2>/dev/null`)
	idxUnsetFn := strings.Index(got, `unset -f "$n" 2>/dev/null`)
	idxLookup := strings.Index(got, `command -v "$n"`)
	if idxUnalias < 0 || idxUnsetFn < 0 || idxLookup < 0 {
		t.Fatalf("script missing unalias/unset -f/command -v steps:\n%s", got)
	}
	if !(idxUnalias < idxLookup && idxUnsetFn < idxLookup) {
		t.Errorf("unalias/unset -f must precede command -v:\n%s", got)
	}

	if !strings.Contains(got, "pwd -P") {
		t.Errorf("script missing pwd -P canonicalisation:\n%s", got)
	}

	if !strings.Contains(got, `printf '%s\t%s\n'`) {
		t.Errorf("script missing tab-separated printf:\n%s", got)
	}
}

func TestResolveAgentsViaLoginShell_ResolvesViaInteractiveShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}
	sh := "/bin/sh"
	if _, err := os.Stat(sh); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "fakeclaude")

	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	t.Setenv("PATH", "/usr/bin:/bin")
	if _, err := lookPathInPath("fakeclaude"); err == nil {
		t.Skip("PATH leak — test environment already exposes fakeclaude without shell help")
	}

	rc := filepath.Join(t.TempDir(), "sh.rc")
	if err := os.WriteFile(rc, []byte("export PATH=\""+binDir+":$PATH\"\n"), 0o644); err != nil {
		t.Fatalf("write rc: %v", err)
	}
	t.Setenv("SHELL", sh)
	t.Setenv("ENV", rc)

	got := resolveAgentsViaLoginShell([]string{"fakeclaude", "kiro-cli"})
	resolved, ok := got["fakeclaude"]
	if !ok {
		t.Fatalf("expected fakeclaude in resolved map, got %v", got)
	}

	if !filepath.IsAbs(resolved) {
		t.Errorf("expected absolute path, got %q", resolved)
	}
	wantCanonical, err := filepath.EvalSymlinks(binPath)
	if err != nil {
		t.Fatalf("eval symlinks for expected path: %v", err)
	}
	if resolved != wantCanonical {
		t.Errorf("resolved = %q, want canonical %q", resolved, wantCanonical)
	}
}

func TestResolveAgentsViaLoginShell_SkipsUnsupportedShell(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/fish")
	got := resolveAgentsViaLoginShell([]string{"claude"})
	if len(got) != 0 {
		t.Errorf("expected empty map for unsupported shell, got %v", got)
	}
}

func TestResolveAgentsViaLoginShell_EmptyShellNoCrash(t *testing.T) {
	t.Setenv("SHELL", "")
	got := resolveAgentsViaLoginShell([]string{"claude"})
	if len(got) != 0 {
		t.Errorf("expected empty map when SHELL unset, got %v", got)
	}
}

func TestResolveAgentsViaLoginShell_EmptyInput(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	got := resolveAgentsViaLoginShell(nil)
	if len(got) != 0 {
		t.Errorf("expected empty map for nil input, got %v", got)
	}
}

func lookPathInPath(name string) (string, error) {
	return exec.LookPath(name)
}

func TestIsOfficialCloudServer(t *testing.T) {
	oldHost := officialCloudHost
	officialCloudHost = "goosar.ru"
	t.Cleanup(func() { officialCloudHost = oldHost })
	for _, tc := range []struct {
		name string
		url  string
		want bool
	}{
		{"canonical cloud https", "https://goosar.ru", true},
		{"canonical cloud with trailing slash stripped", "https://goosar.ru/", true},
		{"canonical cloud case-insensitive", "https://GOOSAR.RU", true},
		{"cloud over plain http (unusual but match host)", "http://goosar.ru", true},
		{"localhost is self-host", "http://localhost:8080", false},
		{"loopback ip is self-host", "http://127.0.0.1:8080", false},
		{"lan ip is self-host", "http://192.168.0.28:8080", false},
		{"third-party host is self-host", "https://goosar.example.com", false},

		{"api subdomain is not the single-origin cloud host", "https://api.goosar.ru", false},
		{"staging subdomain is self-host", "https://staging.goosar.ru", false},
		{"preview subdomain is self-host", "https://api-preview.goosar.ru", false},

		{"empty string is self-host", "", false},
		{"garbage string is self-host", "::not a url::", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOfficialCloudServer(tc.url); got != tc.want {
				t.Errorf("isOfficialCloudServer(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func stageFakeAgent(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("GOOSAR_DAEMON_ID", "11111111-1111-1111-1111-111111111111")

	t.Setenv("GOOSAR_DAEMON_AUTO_UPDATE", "")
	return binDir
}

func TestLoadConfig_DiscoversQwenCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture is unavailable on Windows")
	}
	binDir := stageFakeAgent(t)
	qwen := filepath.Join(binDir, "qwen")
	if err := os.WriteFile(qwen, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake qwen: %v", err)
	}

	t.Setenv("SHELL", "/usr/bin/fish")
	t.Setenv("GOOSAR_RUNTIME_Q_MODEL", "qwen3.8-max-preview")
	t.Setenv("GOOSAR_RUNTIME_Q_ARGS", "--verbose --foo=bar")

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	entry, ok := cfg.Agents["runtime-q"]
	if !ok {
		t.Fatalf("runtime-q was not discovered: %v", cfg.Agents)
	}
	wantPath, err := filepath.EvalSymlinks(qwen)
	if err != nil {
		t.Fatalf("eval symlinks for qwen: %v", err)
	}
	if entry.Path != wantPath || entry.Command != "qwen" || entry.Model != "qwen3.8-max-preview" {
		t.Fatalf("runtime-q entry = %+v, want path=%q command=qwen model=qwen3.8-max-preview", entry, wantPath)
	}
	if got, want := strings.Join(cfg.RuntimeQArgs, " "), "--verbose --foo=bar"; got != want {
		t.Fatalf("RuntimeQArgs = %q, want %q", got, want)
	}
}

func TestLoadConfig_SkipsGoosarHooksShadowingAgentBinaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	hooksDir := filepath.Join(home, ".goosar", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	realBinDir := t.TempDir()

	for _, name := range []string{"claude", "codex", "hermes"} {
		hookPath := filepath.Join(hooksDir, name)
		hookBody := "#!/bin/sh\nexec " + name + " \"$@\"\n"
		if err := os.WriteFile(hookPath, []byte(hookBody), 0o755); err != nil {
			t.Fatalf("write hook wrapper %s: %v", name, err)
		}
		realPath := filepath.Join(realBinDir, name)
		if err := os.WriteFile(realPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write real binary %s: %v", name, err)
		}
	}

	t.Setenv("PATH", hooksDir+string(os.PathListSeparator)+realBinDir)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	t.Setenv("GOOSAR_DAEMON_ID", "11111111-1111-1111-1111-111111111111")

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	for provider, binary := range map[string]string{
		"runtime-c": "claude",
		"runtime-e": "codex",
		"runtime-j": "hermes",
	} {
		got, ok := cfg.Agents[provider]
		if !ok {
			t.Fatalf("expected %s agent in config, got %#v", provider, cfg.Agents)
		}
		want := canonicalExecutablePath(filepath.Join(realBinDir, binary))
		if got.Path != want {
			t.Errorf("%s path = %q, want unshadowed real binary %q", provider, got.Path, want)
		}
		if strings.HasPrefix(got.Path, hooksDir) {
			t.Errorf("%s path still points into hooks dir: %q", provider, got.Path)
		}
	}
}

func TestLoadConfig_SkipsGoosarHooksFromLoginShellFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}
	sh := "/bin/sh"
	if _, err := os.Stat(sh); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	hooksDir := filepath.Join(home, ".goosar", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	realBinDir := t.TempDir()
	hookPath := filepath.Join(hooksDir, "codex")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nexec codex \"$@\"\n"), 0o755); err != nil {
		t.Fatalf("write hook wrapper: %v", err)
	}
	realPath := filepath.Join(realBinDir, "codex")
	if err := os.WriteFile(realPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write real codex: %v", err)
	}

	t.Setenv("PATH", "/usr/bin:/bin")
	if _, err := exec.LookPath("codex"); err == nil {
		t.Skip("PATH leak - codex already visible to daemon without shell fallback")
	}
	rc := filepath.Join(t.TempDir(), "sh.rc")
	rcBody := "export PATH=\"" + hooksDir + string(os.PathListSeparator) + realBinDir + ":$PATH\"\n"
	if err := os.WriteFile(rc, []byte(rcBody), 0o644); err != nil {
		t.Fatalf("write shell rc: %v", err)
	}
	t.Setenv("SHELL", sh)
	t.Setenv("ENV", rc)
	t.Setenv("GOOSAR_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	pinNonCodexAgentsToMissingPaths(t)
	stubBundledExecutablePaths(t, nil)

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	got, ok := cfg.Agents["runtime-e"]
	if !ok {
		t.Fatalf("expected runtime-e from login-shell fallback, got %#v", cfg.Agents)
	}
	want := canonicalExecutablePath(realPath)
	if got.Path != want {
		t.Fatalf("codex path = %q, want unshadowed real binary %q", got.Path, want)
	}
	if strings.HasPrefix(got.Path, hooksDir) {
		t.Fatalf("codex path still points into hooks dir: %q", got.Path)
	}
}

func TestLoadConfig_AutoUpdateDefault_SelfHostOff(t *testing.T) {
	stageFakeAgent(t)
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = true for self-host (localhost) server, want false")
	}
}

func TestLoadConfig_RuntimeEHandshakeTimeout(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("GOOSAR_RUNTIME_E_HANDSHAKE_TIMEOUT", "")

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with default: %v", err)
	}
	if cfg.RuntimeEHandshakeTimeout != DefaultRuntimeEHandshakeTimeout {
		t.Fatalf("RuntimeEHandshakeTimeout = %s, want default %s", cfg.RuntimeEHandshakeTimeout, DefaultRuntimeEHandshakeTimeout)
	}

	t.Setenv("GOOSAR_RUNTIME_E_HANDSHAKE_TIMEOUT", "47s")

	cfg, err = LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with env: %v", err)
	}
	if cfg.RuntimeEHandshakeTimeout != 47*time.Second {
		t.Fatalf("RuntimeEHandshakeTimeout = %s, want 47s from env", cfg.RuntimeEHandshakeTimeout)
	}

	t.Setenv("GOOSAR_RUNTIME_E_HANDSHAKE_TIMEOUT", "0")
	cfg, err = LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with zero env: %v", err)
	}
	if cfg.RuntimeEHandshakeTimeout != DefaultRuntimeEHandshakeTimeout {
		t.Fatalf("RuntimeEHandshakeTimeout = %s, want default %s for zero env", cfg.RuntimeEHandshakeTimeout, DefaultRuntimeEHandshakeTimeout)
	}

	cfg, err = LoadConfig(Overrides{
		ServerURL:                "http://localhost:8080",
		WorkspacesRoot:           t.TempDir(),
		RuntimeEHandshakeTimeout: 12 * time.Second,
	})
	if err != nil {
		t.Fatalf("LoadConfig with override: %v", err)
	}
	if cfg.RuntimeEHandshakeTimeout != 12*time.Second {
		t.Fatalf("RuntimeEHandshakeTimeout = %s, want 12s from override", cfg.RuntimeEHandshakeTimeout)
	}
}

func TestLoadConfig_OpenCodeIdleWatchdog(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("GOOSAR_RUNTIME_M_IDLE_WATCHDOG", "")

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with default: %v", err)
	}
	if cfg.OpenCodeIdleWatchdog != DefaultRuntimeMIdleWatchdog {
		t.Fatalf("OpenCodeIdleWatchdog = %s, want default %s", cfg.OpenCodeIdleWatchdog, DefaultRuntimeMIdleWatchdog)
	}

	t.Setenv("GOOSAR_RUNTIME_M_IDLE_WATCHDOG", "7m")
	cfg, err = LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with env: %v", err)
	}
	if cfg.OpenCodeIdleWatchdog != 7*time.Minute {
		t.Fatalf("OpenCodeIdleWatchdog = %s, want 7m from env", cfg.OpenCodeIdleWatchdog)
	}

	t.Setenv("GOOSAR_RUNTIME_M_IDLE_WATCHDOG", "0")
	cfg, err = LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with zero env: %v", err)
	}
	if cfg.OpenCodeIdleWatchdog != 0 {
		t.Fatalf("OpenCodeIdleWatchdog = %s, want zero from env", cfg.OpenCodeIdleWatchdog)
	}
}

func TestLoadConfig_AutoUpdateDefault_CloudOn(t *testing.T) {
	stageFakeAgent(t)
	oldHost := officialCloudHost
	officialCloudHost = "goosar.ru"
	t.Cleanup(func() { officialCloudHost = oldHost })
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "wss://goosar.ru/ws",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = false for Goosar Cloud server, want true")
	}
}

func TestLoadConfig_AutoUpdateEnv_ForcesOnForSelfHost(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("GOOSAR_DAEMON_AUTO_UPDATE", "true")
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = false after explicit GOOSAR_DAEMON_AUTO_UPDATE=true, want true")
	}
}

func TestLoadConfig_AutoUpdateEnv_ForcesOffForCloud(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("GOOSAR_DAEMON_AUTO_UPDATE", "false")
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "https://goosar.ru",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = true after explicit GOOSAR_DAEMON_AUTO_UPDATE=false, want false")
	}
}

func TestLoadConfig_AutoUpdate_NoFlagWinsOverCloudDefault(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("GOOSAR_DAEMON_AUTO_UPDATE", "true")
	cfg, err := LoadConfig(Overrides{
		ServerURL:         "https://goosar.ru",
		WorkspacesRoot:    t.TempDir(),
		DisableAutoUpdate: true,
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = true with --no-auto-update set; flag must win")
	}
}

func TestResolveAgentsViaLoginShell_StripsAliasShadowing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}
	sh := "/bin/sh"
	if _, err := os.Stat(sh); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "fakeclaude")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	rc := filepath.Join(t.TempDir(), "sh.rc")
	rcBody := "export PATH=\"" + binDir + ":$PATH\"\n" +
		"alias fakeclaude=\"/nonexistent/wrapper-from-rc\"\n"
	if err := os.WriteFile(rc, []byte(rcBody), 0o644); err != nil {
		t.Fatalf("write rc: %v", err)
	}

	t.Setenv("PATH", "/usr/bin:/bin")
	if _, err := lookPathInPath("fakeclaude"); err == nil {
		t.Skip("PATH leak — fakeclaude already visible to the daemon without shell help")
	}

	t.Setenv("SHELL", sh)
	t.Setenv("ENV", rc)
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer probeCancel()
	probeCmd := exec.CommandContext(probeCtx, sh, "-ilc", "alias fakeclaude 2>/dev/null")
	probeCmd.Stdin = strings.NewReader("")
	probe, err := probeCmd.Output()
	if err != nil || !strings.Contains(string(probe), "fakeclaude") {
		t.Skipf("test host's /bin/sh did not load alias from $ENV; cannot simulate shadowing (probe=%q err=%v)", string(probe), err)
	}

	got := resolveAgentsViaLoginShell([]string{"fakeclaude"})
	resolved, ok := got["fakeclaude"]
	if !ok {
		t.Fatalf("expected fakeclaude in resolved map despite alias shadowing, got %v", got)
	}
	wantCanonical, err := filepath.EvalSymlinks(binPath)
	if err != nil {
		t.Fatalf("eval symlinks for expected path: %v", err)
	}
	if resolved != wantCanonical {
		t.Errorf("resolved = %q, want canonical %q (got the alias instead of the PATH binary?)", resolved, wantCanonical)
	}
}

func TestResolveAgentsViaLoginShell_HardTimeoutOnBackgroundedStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}
	sh := "/bin/sh"
	if _, err := os.Stat(sh); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	rc := filepath.Join(t.TempDir(), "sh.rc")
	rcBody := "( sleep 60 ) &\n"
	if err := os.WriteFile(rc, []byte(rcBody), 0o644); err != nil {
		t.Fatalf("write rc: %v", err)
	}
	t.Setenv("SHELL", sh)
	t.Setenv("ENV", rc)

	cap := loginShellResolveTimeout + loginShellResolveWaitDelay + 3*time.Second
	start := time.Now()
	done := make(chan struct{})
	go func() {
		_ = resolveAgentsViaLoginShell([]string{"claude"})
		close(done)
	}()
	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > cap {
			t.Errorf("resolver took %v, expected <= %v (WaitDelay leak?)", elapsed, cap)
		}
	case <-time.After(cap):
		t.Fatalf("resolver did not return within %v — WaitDelay is not enforcing a hard ceiling", cap)
	}
}

func TestLoadConfig_SkipsLoginShellWhenLookPathSucceeds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell not available on Windows")
	}

	pathDir := t.TempDir()
	fakeClaude := filepath.Join(pathDir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	shellDir := t.TempDir()
	shellPath := filepath.Join(shellDir, "bash")
	marker := filepath.Join(shellDir, "invoked.marker")
	shellBody := "#!/bin/sh\ntouch \"" + marker + "\"\n"
	if err := os.WriteFile(shellPath, []byte(shellBody), 0o755); err != nil {
		t.Fatalf("write sentinel shell: %v", err)
	}

	t.Setenv("PATH", pathDir)
	t.Setenv("SHELL", shellPath)

	t.Setenv("GOOSAR_DAEMON_ID", "11111111-1111-1111-1111-111111111111")

	if _, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	}); err != nil {

		t.Logf("LoadConfig returned %v (non-fatal for this test)", err)
	}

	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("login shell was invoked even though exec.LookPath found every agent — laziness broken")
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error stat-ing marker file: %v", err)
	}
}

func TestLoadConfig_UsesCodexDesktopAppBundleFallback(t *testing.T) {
	pathDir := t.TempDir()
	fakeCodex := filepath.Join(pathDir, "Codex.app", "Contents", "Resources", "codex")
	if err := os.MkdirAll(filepath.Dir(fakeCodex), 0o755); err != nil {
		t.Fatalf("mkdir fake Codex bundle: %v", err)
	}
	if err := os.WriteFile(fakeCodex, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake Codex bundle CLI: %v", err)
	}

	stubBundledExecutablePaths(t, map[string][]string{"runtime-e": {fakeCodex}})

	t.Setenv("PATH", t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	t.Setenv("GOOSAR_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	t.Setenv("GOOSAR_RUNTIME_E_MODEL", "gpt-5")
	pinNonCodexAgentsToMissingPaths(t)

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	got, ok := cfg.Agents["runtime-e"]
	if !ok {
		t.Fatalf("expected runtime-e agent from Desktop app bundle fallback, got %#v", cfg.Agents)
	}
	if got.Path != fakeCodex {
		t.Fatalf("runtime-e path = %q, want %q", got.Path, fakeCodex)
	}
	if got.Model != "gpt-5" {
		t.Fatalf("runtime-e model = %q, want gpt-5", got.Model)
	}
}

func stubBundledExecutablePaths(t *testing.T, byCode map[string][]string) {
	t.Helper()
	old := runtimeBundledExecutablePaths
	runtimeBundledExecutablePaths = func(code string) []string { return byCode[code] }
	t.Cleanup(func() { runtimeBundledExecutablePaths = old })
}

func TestLoadConfig_UsesChatGPTAppBundleCodexPath(t *testing.T) {
	pathDir := t.TempDir()
	fakeChatGPT := filepath.Join(pathDir, "ChatGPT.app", "Contents", "Resources", "codex")
	fakeLegacy := filepath.Join(pathDir, "Codex.app", "Contents", "Resources", "codex")
	for _, p := range []string{fakeChatGPT, fakeLegacy} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write fake CLI: %v", err)
		}
	}

	stubBundledExecutablePaths(t, map[string][]string{"runtime-e": {fakeChatGPT, fakeLegacy}})

	t.Setenv("PATH", t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	t.Setenv("GOOSAR_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	pinNonCodexAgentsToMissingPaths(t)

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	got, ok := cfg.Agents["runtime-e"]
	if !ok {
		t.Fatalf("expected runtime-e agent from ChatGPT.app bundle, got %#v", cfg.Agents)
	}
	if got.Path != fakeChatGPT {
		t.Fatalf("runtime-e path = %q, want ChatGPT.app path %q", got.Path, fakeChatGPT)
	}
}

func TestCodexDesktopAppBundlePaths_IncludesChatGPTAndLegacy(t *testing.T) {
	paths := runtimeBundledExecutablePaths("runtime-e")
	var hasChatGPT, hasLegacy bool
	for _, p := range paths {
		if strings.Contains(p, "ChatGPT.app") && strings.HasSuffix(filepath.ToSlash(p), "Contents/Resources/codex") {
			hasChatGPT = true
		}
		if strings.Contains(p, "Codex.app") && strings.HasSuffix(filepath.ToSlash(p), "Contents/Resources/codex") {
			hasLegacy = true
		}
	}
	if !hasChatGPT {
		t.Fatalf("runtimeBundledExecutablePaths missing ChatGPT.app entry: %#v", paths)
	}
	if !hasLegacy {
		t.Fatalf("runtimeBundledExecutablePaths missing legacy Codex.app entry: %#v", paths)
	}

	chatgptIdx, legacyIdx := -1, -1
	for i, p := range paths {
		if chatgptIdx < 0 && strings.Contains(p, "ChatGPT.app") {
			chatgptIdx = i
		}
		if legacyIdx < 0 && strings.Contains(p, "Codex.app") {
			legacyIdx = i
		}
	}
	if chatgptIdx < 0 || legacyIdx < 0 || chatgptIdx > legacyIdx {
		t.Fatalf("expected ChatGPT.app before Codex.app, got indices chat=%d legacy=%d paths=%#v", chatgptIdx, legacyIdx, paths)
	}
}

func TestLoadConfig_CodexDesktopFallbackDoesNotOverrideExplicitPath(t *testing.T) {
	pathDir := t.TempDir()
	fakeCodex := filepath.Join(pathDir, "Codex.app", "Contents", "Resources", "codex")
	if err := os.MkdirAll(filepath.Dir(fakeCodex), 0o755); err != nil {
		t.Fatalf("mkdir fake Codex bundle: %v", err)
	}
	if err := os.WriteFile(fakeCodex, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake Codex bundle CLI: %v", err)
	}

	stubBundledExecutablePaths(t, map[string][]string{"runtime-e": {fakeCodex}})

	t.Setenv("PATH", t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	t.Setenv("GOOSAR_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	t.Setenv("GOOSAR_RUNTIME_E_PATH", filepath.Join(t.TempDir(), "missing-codex"))
	pinNonCodexAgentsToMissingPaths(t)
	fakeClaude := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("GOOSAR_RUNTIME_C_PATH", fakeClaude)

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got, ok := cfg.Agents["runtime-e"]; ok {
		t.Fatalf("explicit missing GOOSAR_RUNTIME_E_PATH should not fall back to Desktop bundle, got %#v", got)
	}
}

func pinNonCodexAgentsToMissingPaths(t *testing.T) {
	t.Helper()
	missingDir := t.TempDir()
	for _, code := range runtimeRegistry.Codes() {
		if code == "runtime-e" {
			continue
		}
		name := runtimeEnvPrefix(code) + "_PATH"
		t.Setenv(name, filepath.Join(missingDir, strings.ToLower(name)))
	}
}

func writeCLIConfigForProfile(t *testing.T, profile string, cfg cli.CLIConfig) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		t.Fatalf("write cli config: %v", err)
	}
}

func TestApplyOpenclawOverride_DoesNothingWhenNil(t *testing.T) {

	t.Setenv("GOOSAR_RUNTIME_N_PATH", "/before/openclaw")
	t.Setenv("OPENCLAW_STATE_DIR", "/before/state")

	applyOpenclawOverride(nil)

	if got := os.Getenv("GOOSAR_RUNTIME_N_PATH"); got != "/before/openclaw" {
		t.Errorf("GOOSAR_RUNTIME_N_PATH mutated: got %q, want /before/openclaw", got)
	}
	if got := os.Getenv("OPENCLAW_STATE_DIR"); got != "/before/state" {
		t.Errorf("OPENCLAW_STATE_DIR mutated: got %q, want /before/state", got)
	}
}

func TestApplyOpenclawOverride_SetsBothWhenEnvUnset(t *testing.T) {
	t.Setenv("GOOSAR_RUNTIME_N_PATH", "")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	os.Unsetenv("GOOSAR_RUNTIME_N_PATH")
	os.Unsetenv("OPENCLAW_STATE_DIR")
	t.Cleanup(func() {
		os.Unsetenv("GOOSAR_RUNTIME_N_PATH")
		os.Unsetenv("OPENCLAW_STATE_DIR")
	})

	applyOpenclawOverride(&cli.OpenClawOverride{
		BinaryPath: "/from/config/openclaw",
		StateDir:   "/from/config/state",
	})

	if got := os.Getenv("GOOSAR_RUNTIME_N_PATH"); got != "/from/config/openclaw" {
		t.Errorf("GOOSAR_RUNTIME_N_PATH: got %q, want /from/config/openclaw", got)
	}
	if got := os.Getenv("OPENCLAW_STATE_DIR"); got != "/from/config/state" {
		t.Errorf("OPENCLAW_STATE_DIR: got %q, want /from/config/state", got)
	}
}

func TestApplyOpenclawOverride_EnvWinsOverConfig(t *testing.T) {

	t.Setenv("GOOSAR_RUNTIME_N_PATH", "/from/env/openclaw")
	t.Setenv("OPENCLAW_STATE_DIR", "/from/env/state")

	applyOpenclawOverride(&cli.OpenClawOverride{
		BinaryPath: "/from/config/openclaw",
		StateDir:   "/from/config/state",
	})

	if got := os.Getenv("GOOSAR_RUNTIME_N_PATH"); got != "/from/env/openclaw" {
		t.Errorf("GOOSAR_RUNTIME_N_PATH: env should win, got %q want /from/env/openclaw", got)
	}
	if got := os.Getenv("OPENCLAW_STATE_DIR"); got != "/from/env/state" {
		t.Errorf("OPENCLAW_STATE_DIR: env should win, got %q want /from/env/state", got)
	}
}

func TestApplyOpenclawOverride_PartialFields_OnlySetsConfigured(t *testing.T) {
	os.Unsetenv("GOOSAR_RUNTIME_N_PATH")
	os.Unsetenv("OPENCLAW_STATE_DIR")
	t.Cleanup(func() {
		os.Unsetenv("GOOSAR_RUNTIME_N_PATH")
		os.Unsetenv("OPENCLAW_STATE_DIR")
	})

	applyOpenclawOverride(&cli.OpenClawOverride{
		StateDir: "/from/config/state",
	})

	if _, set := os.LookupEnv("GOOSAR_RUNTIME_N_PATH"); set {
		t.Errorf("GOOSAR_RUNTIME_N_PATH should remain unset when BinaryPath is empty; got %q", os.Getenv("GOOSAR_RUNTIME_N_PATH"))
	}
	if got := os.Getenv("OPENCLAW_STATE_DIR"); got != "/from/config/state" {
		t.Errorf("OPENCLAW_STATE_DIR: got %q, want /from/config/state", got)
	}
}

func TestOpenclawOverrideFrom_NavigationCases(t *testing.T) {
	if got := openclawOverrideFrom(cli.CLIConfig{}); got != nil {
		t.Errorf("nil Backends should produce nil override, got %+v", got)
	}
	if got := openclawOverrideFrom(cli.CLIConfig{Backends: &cli.BackendOverrides{}}); got != nil {
		t.Errorf("nil OpenClaw inside Backends should produce nil override, got %+v", got)
	}
	want := &cli.OpenClawOverride{StateDir: "/x"}
	got := openclawOverrideFrom(cli.CLIConfig{Backends: &cli.BackendOverrides{OpenClaw: want}})
	if got != want {
		t.Errorf("happy path should return inner pointer; got %p want %p", got, want)
	}
}

func TestLoadConfig_AppliesBackendOverridesFromConfigFile(t *testing.T) {
	stageFakeAgent(t)

	customDir := t.TempDir()
	customOpenclaw := filepath.Join(customDir, "non-default-openclaw")
	if err := os.WriteFile(customOpenclaw, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}

	os.Unsetenv("GOOSAR_RUNTIME_N_PATH")
	os.Unsetenv("OPENCLAW_STATE_DIR")
	t.Cleanup(func() {
		os.Unsetenv("GOOSAR_RUNTIME_N_PATH")
		os.Unsetenv("OPENCLAW_STATE_DIR")
	})

	homeForCLIConfig := t.TempDir()
	t.Setenv("HOME", homeForCLIConfig)
	cfg := cli.CLIConfig{
		ServerURL: "http://localhost:8080",
		Backends: &cli.BackendOverrides{
			OpenClaw: &cli.OpenClawOverride{
				BinaryPath: customOpenclaw,
				StateDir:   "/var/lib/openclaw-isolated",
			},
		},
	}
	if err := cli.SaveCLIConfig(cfg); err != nil {
		t.Fatalf("save cli config: %v", err)
	}

	loaded, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	openclaw, ok := loaded.Agents["runtime-n"]
	if !ok {
		t.Fatalf("agents map missing 'runtime-n' key; got keys=%v", agentKeys(loaded.Agents))
	}
	if openclaw.Path != customOpenclaw {
		t.Errorf("openclaw.Path: got %q, want %q (the binary configured in CLI config)", openclaw.Path, customOpenclaw)
	}
	if got := os.Getenv("OPENCLAW_STATE_DIR"); got != "/var/lib/openclaw-isolated" {
		t.Errorf("OPENCLAW_STATE_DIR: got %q, want injected from config", got)
	}
}

func TestLoadConfig_BackendOverrides_BackwardCompat_NoConfigFile(t *testing.T) {
	stageFakeAgent(t)

	t.Setenv("HOME", t.TempDir())
	os.Unsetenv("GOOSAR_RUNTIME_N_PATH")
	os.Unsetenv("OPENCLAW_STATE_DIR")
	t.Cleanup(func() {
		os.Unsetenv("GOOSAR_RUNTIME_N_PATH")
		os.Unsetenv("OPENCLAW_STATE_DIR")
	})

	_, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig with no config file should not fail: %v", err)
	}

	if _, set := os.LookupEnv("OPENCLAW_STATE_DIR"); set {
		t.Errorf("OPENCLAW_STATE_DIR should remain unset when no config file is present; got %q", os.Getenv("OPENCLAW_STATE_DIR"))
	}
}

func TestLoadConfig_BackendOverrides_MalformedConfigFileNonFatal(t *testing.T) {
	stageFakeAgent(t)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cfgDir := filepath.Join(homeDir, ".goosar")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig should not fail on malformed config.json: %v", err)
	}

}

func agentKeys(m map[string]AgentEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestLoadConfig_AutoUpdateEnv_OffSpelling(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("GOOSAR_DAEMON_AUTO_UPDATE", "off")
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "https://goosar.ru",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = true after explicit GOOSAR_DAEMON_AUTO_UPDATE=off, want false")
	}
}

func TestLoadConfig_NoAgentCLIErrorListsEveryRuntime(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	t.Setenv("GOOSAR_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	missingDir := t.TempDir()
	for _, code := range runtimeRegistry.Codes() {
		name := runtimeEnvPrefix(code) + "_PATH"
		t.Setenv(name, filepath.Join(missingDir, strings.ToLower(name)))
	}

	_, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:0",
		WorkspacesRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatal("LoadConfig: expected an error with no agent CLI available, got nil")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "no agent CLI found: ") {
		t.Fatalf("error = %q, want it to start with %q", msg, "no agent CLI found: ")
	}
	for _, code := range runtimeRegistry.Codes() {
		if !strings.Contains(msg, code) {
			t.Fatalf("error = %q, want it to name runtime %q", msg, code)
		}
	}
	if !strings.Contains(msg, "GOOSAR_RUNTIME_<LETTER>_PATH") {
		t.Fatalf("error = %q, want it to point at the executable override", msg)
	}
}
