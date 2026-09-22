package execenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type openclawCLIStub struct {
	t         *testing.T
	bin       string
	responses map[string]openclawResponse
	calls     []openclawCall
}

type openclawCall struct {
	bin  string
	args []string
}

type openclawResponse struct {
	stdout string
	err    error
}

func installOpenclawStub(t *testing.T, responses map[string]openclawResponse) *openclawCLIStub {
	t.Helper()
	stub := &openclawCLIStub{
		t:         t,
		bin:       "/test/stub/openclaw",
		responses: responses,
	}
	prev := openclawExec
	openclawExec = stub.exec
	t.Cleanup(func() { openclawExec = prev })
	return stub
}

func (s *openclawCLIStub) exec(_ context.Context, bin string, args ...string) (string, error) {
	s.calls = append(s.calls, openclawCall{bin: bin, args: append([]string(nil), args...)})
	key := strings.Join(args, " ")
	resp, ok := s.responses[key]
	if !ok {
		return "", fmt.Errorf("openclawCLIStub: unexpected args %q", key)
	}
	return resp.stdout, resp.err
}

func mustReadJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read synthesized cfg: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse synthesized cfg: %v", err)
	}
	return got
}

func TestPrepareOpenclawConfigDelegatesParsingToCLI(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userConfigDir := t.TempDir()
	userConfigPath := filepath.Join(userConfigDir, "openclaw.json")
	json5Body := `// User config with JSON5 features the old parser couldn't read
{
  agents: {
    defaults: {
      workspace: "/Users/alice/.openclaw/workspace",
      model: { primary: "anthropic/claude-sonnet-4-6" },
    },
    list: [
      { id: "scout", workspace: "/Users/alice/projects/scout", },
      { id: "coder", model: "openai/gpt-5", },
    ],
  },
  gateway: { port: 18789 }, // trailing comma
}
`
	if err := os.WriteFile(userConfigPath, []byte(json5Body), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: userConfigPath + "\n"},
		"config get agents.list --json": {stdout: `[
			{ "id": "scout", "workspace": "/Users/alice/projects/scout" },
			{ "id": "coder", "model": "openai/gpt-5" }
		]`},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	cfgPath := result.ConfigPath
	if cfgPath != filepath.Join(envRoot, openclawConfigFile) {
		t.Errorf("cfgPath = %q, want %q", cfgPath, filepath.Join(envRoot, openclawConfigFile))
	}

	got := mustReadJSON(t, cfgPath)

	include, ok := got["$include"].([]any)
	if !ok || len(include) != 1 || include[0] != userConfigPath {
		t.Errorf("$include = %v, want [%q]", got["$include"], userConfigPath)
	}

	if result.IncludeRoot != userConfigDir {
		t.Errorf("IncludeRoot = %q, want %q (dirname of active config so wrapper can $include across dirs)", result.IncludeRoot, userConfigDir)
	}

	agents := got["agents"].(map[string]any)
	defaults := agents["defaults"].(map[string]any)
	if defaults["workspace"] != workDir {
		t.Errorf("agents.defaults.workspace = %v, want %q", defaults["workspace"], workDir)
	}

	list := agents["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("agents.list length = %d, want 2", len(list))
	}
	for i, item := range list {
		entry := item.(map[string]any)
		if entry["workspace"] != workDir {
			t.Errorf("agents.list[%d].workspace = %v, want %q (per-agent overrides must be rewritten so they don't beat defaults)", i, entry["workspace"], workDir)
		}
	}

	if list[0].(map[string]any)["id"] != "scout" {
		t.Errorf("agents.list[0].id lost in carryover: %v", list[0])
	}
	if list[1].(map[string]any)["model"] != "openai/gpt-5" {
		t.Errorf("agents.list[1].model lost in carryover: %v", list[1])
	}
}

func TestPrepareOpenclawConfigFailsClosedOnCLIError(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: errors.New("exec: openclaw: no such file or directory")},
	})

	_, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err == nil {
		t.Fatal("prepareOpenclawConfig succeeded on CLI failure; expected fail closed")
	}
	if !strings.Contains(err.Error(), "locate openclaw active config") {
		t.Errorf("error message %q does not name the failed step", err.Error())
	}

	if _, err := os.Stat(filepath.Join(envRoot, openclawConfigFile)); !os.IsNotExist(err) {
		t.Errorf("wrapper config should not exist after fail-closed; got err = %v", err)
	}
}

func TestPrepareOpenclawConfigFallsBackWhenConfigFileUnsupported(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userConfigDir := t.TempDir()
	userConfigPath := filepath.Join(userConfigDir, "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}
	t.Setenv("OPENCLAW_CONFIG_PATH", userConfigPath)

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: errors.New("openclaw config file: exit status 1 (stderr: error: too many arguments for 'config'. Expected 0 arguments but got 1.)")},
		"config get agents.list --json": {stdout: `[
			{ "id": "coder", "model": "openai/gpt-5" }
		]`},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	got := mustReadJSON(t, result.ConfigPath)
	include, ok := got["$include"].([]any)
	if !ok || len(include) != 1 || include[0] != userConfigPath {
		t.Errorf("$include = %v, want fallback OPENCLAW_CONFIG_PATH %q", got["$include"], userConfigPath)
	}
	if result.IncludeRoot != userConfigDir {
		t.Errorf("IncludeRoot = %q, want %q", result.IncludeRoot, userConfigDir)
	}
	agents := got["agents"].(map[string]any)
	list := agents["list"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["workspace"] != workDir {
		t.Errorf("agents.list workspace rewrite after fallback = %v, want workDir %q", list, workDir)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("openclaw calls = %d, want 2: %+v", len(stub.calls), stub.calls)
	}
	if strings.Join(stub.calls[1].args, " ") != "config get agents.list --json" {
		t.Errorf("second openclaw call = %q, want config get agents.list --json", strings.Join(stub.calls[1].args, " "))
	}
}

func TestOpenclawActiveConfigPathFallbackSources(t *testing.T) {
	cases := map[string]struct {
		setup func(t *testing.T) string
	}{
		"config_path": {
			setup: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "custom-openclaw.json")
				t.Setenv("OPENCLAW_CONFIG_PATH", path)
				return path
			},
		},
		"legacy_config_path": {
			setup: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "custom-clawdbot.json")
				t.Setenv("CLAWDBOT_CONFIG_PATH", path)
				return path
			},
		},
		"state_dir": {
			setup: func(t *testing.T) string {
				stateDir := t.TempDir()
				path := filepath.Join(stateDir, "openclaw.json")
				t.Setenv("OPENCLAW_STATE_DIR", stateDir)
				return path
			},
		},
		"legacy_state_dir": {
			setup: func(t *testing.T) string {
				stateDir := t.TempDir()
				path := filepath.Join(stateDir, "clawdbot.json")
				t.Setenv("CLAWDBOT_STATE_DIR", stateDir)
				return path
			},
		},
		"openclaw_home": {
			setup: func(t *testing.T) string {
				home := t.TempDir()
				path := filepath.Join(home, ".openclaw", "openclaw.json")
				t.Setenv("OPENCLAW_HOME", home)
				return path
			},
		},
		"default_home": {
			setup: func(t *testing.T) string {
				home := t.TempDir()
				path := filepath.Join(home, ".openclaw", "openclaw.json")
				t.Setenv("HOME", home)
				return path
			},
		},
		"legacy_default_clawdbot": {
			setup: func(t *testing.T) string {
				home := t.TempDir()
				path := filepath.Join(home, ".clawdbot", "clawdbot.json")
				t.Setenv("HOME", home)
				return path
			},
		},
		"legacy_default_moltbot": {
			setup: func(t *testing.T) string {
				home := t.TempDir()
				path := filepath.Join(home, ".moltbot", "moltbot.json")
				t.Setenv("HOME", home)
				return path
			},
		},
		"legacy_default_moldbot": {
			setup: func(t *testing.T) string {
				home := t.TempDir()
				path := filepath.Join(home, ".moldbot", "moldbot.json")
				t.Setenv("HOME", home)
				return path
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			clearOpenclawPathEnv(t)
			want := tc.setup(t)
			if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
				t.Fatalf("mkdir config dir: %v", err)
			}
			if err := os.WriteFile(want, []byte(`{}`), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file": {err: openclawConfigFileUnsupportedErr()},
			})

			got, exists, err := openclawActiveConfigPath(stub.bin, openclawCLITimeout)
			if err != nil {
				t.Fatalf("openclawActiveConfigPath: %v", err)
			}
			if !exists {
				t.Fatal("exists = false, want true")
			}
			if got != want {
				t.Errorf("path = %q, want %q", got, want)
			}
		})
	}
}

func TestOpenclawActiveConfigPathFallbackFreshInstallUsesCanonicalPath(t *testing.T) {
	clearOpenclawPathEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: openclawConfigFileUnsupportedErr()},
	})

	got, exists, err := openclawActiveConfigPath(stub.bin, openclawCLITimeout)
	if err != nil {
		t.Fatalf("openclawActiveConfigPath: %v", err)
	}
	want := filepath.Join(home, ".openclaw", "openclaw.json")
	if got != want {
		t.Errorf("path = %q, want canonical fresh-install path %q", got, want)
	}
	if exists {
		t.Fatal("exists = true, want false for fresh install")
	}
}

func TestOpenclawActiveConfigPathFallbackOpenclawConfigPathHardOverride(t *testing.T) {
	clearOpenclawPathEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	explicitPath := filepath.Join(t.TempDir(), "missing-openclaw.json")
	t.Setenv("OPENCLAW_CONFIG_PATH", explicitPath)
	legacyPath := filepath.Join(home, ".clawdbot", "clawdbot.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy config dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: openclawConfigFileUnsupportedErr()},
	})

	got, exists, err := openclawActiveConfigPath(stub.bin, openclawCLITimeout)
	if err != nil {
		t.Fatalf("openclawActiveConfigPath: %v", err)
	}
	if got != explicitPath {
		t.Errorf("path = %q, want OPENCLAW_CONFIG_PATH hard override %q", got, explicitPath)
	}
	if exists {
		t.Fatal("exists = true, want false when explicit OPENCLAW_CONFIG_PATH is missing")
	}
}

func TestOpenclawActiveConfigPathFallbackErrorIncludesOriginalCLIError(t *testing.T) {
	clearOpenclawPathEnv(t)
	badPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.MkdirAll(badPath, 0o755); err != nil {
		t.Fatalf("mkdir bad config path: %v", err)
	}
	t.Setenv("OPENCLAW_CONFIG_PATH", badPath)
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: openclawConfigFileUnsupportedErr()},
	})

	_, _, err := openclawActiveConfigPath(stub.bin, openclawCLITimeout)
	if err == nil {
		t.Fatal("openclawActiveConfigPath succeeded with directory config path; expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "too many arguments for 'config'") {
		t.Errorf("error %q lost original unsupported CLI stderr", msg)
	}
	if !strings.Contains(msg, "is a directory") {
		t.Errorf("error %q lost fallback failure detail", msg)
	}
}

func TestIsOpenclawConfigFileUnsupportedMatchesKnownShapes(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"reported_too_many_arguments": {
			err:  errors.New("openclaw config file: exit status 1 (stderr: error: too many arguments for 'config')"),
			want: true,
		},
		"reported_expected_zero_args": {
			err:  errors.New("Expected 0 arguments but got 1."),
			want: true,
		},
		"unknown_config_file": {
			err:  errors.New("unknown subcommand `file` for `openclaw config`"),
			want: true,
		},
		"real_config_validation_error": {
			err:  errors.New("openclaw config validation failed: missing gateway.auth.token"),
			want: false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := isOpenclawConfigFileUnsupported(tc.err); got != tc.want {
				t.Errorf("isOpenclawConfigFileUnsupported(%q) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

func clearOpenclawPathEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENCLAW_CONFIG_PATH", "")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	t.Setenv("OPENCLAW_HOME", "")
	t.Setenv("CLAWDBOT_CONFIG_PATH", "")
	t.Setenv("CLAWDBOT_STATE_DIR", "")
}

func openclawConfigFileUnsupportedErr() error {
	return errors.New("openclaw config file: exit status 1 (stderr: error: too many arguments for 'config'. Expected 0 arguments but got 1.)")
}

func TestPrepareOpenclawConfigFailsClosedOnMalformedAgentsList(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userConfigPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userConfigPath},
		"config get agents.list --json": {stdout: "<<<garbage>>>"},
	})

	_, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err == nil {
		t.Fatal("prepareOpenclawConfig succeeded on malformed agents.list output; expected fail closed")
	}
	if !strings.Contains(err.Error(), "agents.list") {
		t.Errorf("error message %q does not name the failed step", err.Error())
	}
}

func TestPrepareOpenclawConfigKeyMissingTreatedAsEmpty(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userConfigPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userConfigPath},
		"config get agents.list --json": {err: errors.New("openclaw: No value at agents.list")},

		"agents list --json": {stdout: "null"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	cfgPath := result.ConfigPath
	got := mustReadJSON(t, cfgPath)
	if _, present := got["agents"].(map[string]any)["list"]; present {
		t.Errorf("agents.list should be omitted when user has none, got %v", got["agents"])
	}
	if got["agents"].(map[string]any)["defaults"].(map[string]any)["workspace"] != workDir {
		t.Errorf("defaults.workspace not set when agents.list missing")
	}
}

func TestPrepareOpenclawConfigFreshInstallNoOnDiskConfig(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	missingPath := filepath.Join(t.TempDir(), "openclaw.json")

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: missingPath},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	cfgPath := result.ConfigPath
	got := mustReadJSON(t, cfgPath)
	if _, present := got["$include"]; present {
		t.Errorf("$include should be absent for fresh install, got %v", got["$include"])
	}
	if got["agents"].(map[string]any)["defaults"].(map[string]any)["workspace"] != workDir {
		t.Errorf("defaults.workspace not set on fresh-install wrapper")
	}

	if result.IncludeRoot != "" {
		t.Errorf("IncludeRoot = %q on fresh install, want empty (no $include emitted)", result.IncludeRoot)
	}
}

func TestPrepareOpenclawConfigExpandsTilde(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	if err := os.MkdirAll(filepath.Join(fakeHome, ".openclaw"), 0o755); err != nil {
		t.Fatalf("mkdir home/.openclaw: %v", err)
	}
	realPath := filepath.Join(fakeHome, ".openclaw", "openclaw.json")
	if err := os.WriteFile(realPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: "~/.openclaw/openclaw.json\n"},
		"config get agents.list --json": {stdout: "null"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	cfgPath := result.ConfigPath
	got := mustReadJSON(t, cfgPath)
	include := got["$include"].([]any)
	if include[0] != realPath {
		t.Errorf("$include[0] = %v, want %q (tilde must be expanded to absolute)", include[0], realPath)
	}

	wantRoot := filepath.Join(fakeHome, ".openclaw")
	if result.IncludeRoot != wantRoot {
		t.Errorf("IncludeRoot = %q, want %q (must be expanded absolute dirname)", result.IncludeRoot, wantRoot)
	}
}

func TestPrepareOpenclawConfigParsesPathFromUITerminalOutput(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userConfigDir := t.TempDir()
	userConfigPath := filepath.Join(userConfigDir, "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stdoutWithUI := `│
◇  Doctor warnings ──────────────────────────────────────────────────────╮
│                                                                        │
│  - Left plugin install index in place because shared SQLite state has  │
│    conflicting plugin install metadata for: qqbot                      │
│                                                                        │
├────────────────────────────────────────────────────────────────────────╯
[state-migrations] Legacy state migration warnings:
- Left plugin install index in place because shared SQLite state has conflicting plugin install metadata for: qqbot
│
◇  Doctor warnings ──────────────────────────────────────────────────────╮
│                                                                        │
│  - Left plugin install index in place because shared SQLite state has  │
│    conflicting plugin install metadata for: qqbot                      │
│                                                                        │
├────────────────────────────────────────────────────────────────────────╯
` + userConfigPath + "\n"

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: stdoutWithUI},
		"config get agents.list --json": {stdout: "null"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	got := mustReadJSON(t, result.ConfigPath)
	include := got["$include"].([]any)
	if include[0] != userConfigPath {
		t.Errorf("$include[0] = %v, want %q (path must be extracted from last non-empty line)", include[0], userConfigPath)
	}
}

func TestPrepareOpenclawConfigWrapperLoadableUnderIncludeConfinement(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userConfigDir := t.TempDir()
	userConfigPath := filepath.Join(userConfigDir, "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userConfigPath},
		"config get agents.list --json": {stdout: "null"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	got := mustReadJSON(t, result.ConfigPath)
	rawIncludes, ok := got["$include"].([]any)
	if !ok || len(rawIncludes) == 0 {
		t.Fatalf("wrapper has no $include entries, but a user config is present: %v", got)
	}

	wrapperDir := filepath.Dir(result.ConfigPath)
	granted := []string{wrapperDir}
	if result.IncludeRoot != "" {
		granted = append(granted, result.IncludeRoot)
	}
	for _, raw := range rawIncludes {
		target, ok := raw.(string)
		if !ok {
			t.Fatalf("$include entry is not a string: %T %v", raw, raw)
		}
		targetDir := filepath.Dir(target)
		allowed := false
		for _, g := range granted {
			if targetDir == g {
				allowed = true
				break
			}
		}
		if !allowed {
			t.Errorf("$include target %q has dirname %q which is not in granted include roots %v — OpenClaw would refuse to load it",
				target, targetDir, granted)
		}
	}
}

func TestPrepareOpenclawConfigStrictReplacesUserMcpServers(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	userCfg := `{"mcp": {"servers": {"global_one": {"command": "/bin/echo"}}}, "providers": {"anthropic": {"apiKey": "sk-user-secret"}}}`
	if err := os.WriteFile(userCfgPath, []byte(userCfg), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userCfgPath},
		"config get agents.list --json": {stdout: "null"},
	})

	mcpConfig := json.RawMessage(`{
		"mcpServers": {
			"shared":       {"command": "/bin/new-version"},
			"managed_only": {"url": "https://mcp.example.com", "transport": "streamable-http"}
		}
	}`)

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: stub.bin,
		McpConfig:   mcpConfig,
	})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	got := mustReadJSON(t, result.ConfigPath)
	mcp, ok := got["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("wrapper missing mcp block: %v", got)
	}
	servers, ok := mcp["servers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.servers is not an object: %v", mcp)
	}
	if len(servers) != 2 {
		t.Errorf("mcp.servers has %d entries, want 2 (managed only): %v", len(servers), servers)
	}
	if _, leaked := servers["global_one"]; leaked {
		t.Errorf("mcp.servers.global_one leaked into wrapper from user config: %v", servers)
	}

	include, _ := got["$include"].([]any)
	if len(include) != 2 {
		t.Fatalf("wrapper $include has %d entries, want [user, reset]: %v", len(include), include)
	}
	if include[0] != userCfgPath {
		t.Errorf("$include[0] = %v, want live user config %q", include[0], userCfgPath)
	}
	resetPath := filepath.Join(envRoot, openclawMcpResetFile)
	if include[1] != resetPath {
		t.Errorf("$include[1] = %v, want reset stage %q", include[1], resetPath)
	}
	raw, err := os.ReadFile(resetPath)
	if err != nil {
		t.Fatalf("read reset stage: %v", err)
	}
	if string(raw) != openclawMcpResetBody {
		t.Errorf("reset stage body = %q, want %q", raw, openclawMcpResetBody)
	}

	if strings.Contains(string(raw), "sk-user-secret") || strings.Contains(string(raw), "global_one") {
		t.Errorf("reset stage carries user config bytes: %s", raw)
	}

	if result.IncludeRoot != filepath.Dir(userCfgPath) {
		t.Errorf("IncludeRoot = %q, want %q", result.IncludeRoot, filepath.Dir(userCfgPath))
	}
}

func TestPrepareOpenclawConfigStrictEmptyManagedSetDropsUserMcp(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{"mcp": {"servers": {"global_one": {"command": "/bin/echo"}}}}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	cases := map[string]json.RawMessage{
		"object_empty":          json.RawMessage(`{}`),
		"mcp_servers_empty_map": json.RawMessage(`{"mcpServers": {}}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file":                   {stdout: userCfgPath},
				"config get agents.list --json": {stdout: "null"},
			})
			result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
				OpenclawBin: stub.bin,
				McpConfig:   raw,
			})
			if err != nil {
				t.Fatalf("prepareOpenclawConfig: %v", err)
			}
			got := mustReadJSON(t, result.ConfigPath)
			mcp, ok := got["mcp"].(map[string]any)
			if !ok {
				t.Fatalf("wrapper missing mcp block (managed empty must still be present): %v", got)
			}
			servers, ok := mcp["servers"].(map[string]any)
			if !ok {
				t.Fatalf("mcp.servers is not an object: %v", mcp)
			}
			if len(servers) != 0 {
				t.Errorf("mcp.servers has %d entries on managed-empty, want 0: %v", len(servers), servers)
			}
			include, _ := got["$include"].([]any)
			if len(include) != 2 || include[1] != filepath.Join(envRoot, openclawMcpResetFile) {
				t.Errorf("$include = %v, want [user, reset] — managed-empty must still null the user's servers", include)
			}
		})
	}
}

func TestPrepareOpenclawConfigResetStagePairsWithWrapperMcp(t *testing.T) {
	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}
	for name, raw := range map[string]json.RawMessage{
		"managed":   json.RawMessage(`{"mcpServers": {"m": {"command": "uvx"}}}`),
		"inherited": nil,
	} {
		t.Run(name, func(t *testing.T) {
			envRoot := t.TempDir()
			workDir := filepath.Join(envRoot, "workdir")
			if err := os.MkdirAll(workDir, 0o755); err != nil {
				t.Fatalf("mkdir workdir: %v", err)
			}
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file":                   {stdout: userCfgPath},
				"config get agents.list --json": {stdout: "null"},
			})
			result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin, McpConfig: raw})
			if err != nil {
				t.Fatalf("prepareOpenclawConfig: %v", err)
			}
			got := mustReadJSON(t, result.ConfigPath)
			_, wrapperHasMcp := got["mcp"]
			_, statErr := os.Stat(filepath.Join(envRoot, openclawMcpResetFile))
			hasReset := statErr == nil
			if wrapperHasMcp != hasReset {
				t.Fatalf("wrapper mcp=%v reset stage=%v: the two must always pair", wrapperHasMcp, hasReset)
			}
		})
	}
}

func TestPrepareOpenclawConfigNullMcpConfigKeepsUserInclude(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userCfgDir := t.TempDir()
	userCfgPath := filepath.Join(userCfgDir, "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	cases := map[string]json.RawMessage{
		"nil":   nil,
		"empty": json.RawMessage(""),
		"null":  json.RawMessage("null"),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file":                   {stdout: userCfgPath},
				"config get agents.list --json": {stdout: "null"},
			})
			result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
				OpenclawBin: stub.bin,
				McpConfig:   raw,
			})
			if err != nil {
				t.Fatalf("prepareOpenclawConfig: %v", err)
			}
			got := mustReadJSON(t, result.ConfigPath)
			if _, present := got["mcp"]; present {
				t.Errorf("wrapper has mcp block when mcp_config = %q: %v", name, got["mcp"])
			}
			include, _ := got["$include"].([]any)
			if len(include) != 1 || include[0] != userCfgPath {
				t.Errorf("$include = %v, want live user config %q on inherit path", got["$include"], userCfgPath)
			}
			if _, err := os.Stat(filepath.Join(envRoot, openclawMcpResetFile)); !os.IsNotExist(err) {
				t.Errorf("inherit path wrote a reset stage (should not): err=%v", err)
			}
			if result.IncludeRoot != userCfgDir {
				t.Errorf("IncludeRoot = %q, want %q (cross-dir hop for live $include)", result.IncludeRoot, userCfgDir)
			}
		})
	}
}

func TestPrepareOpenclawConfigManagedSetFreshInstall(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	missingPath := filepath.Join(t.TempDir(), "openclaw.json")
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: missingPath},
	})
	mcpConfig := json.RawMessage(`{"mcpServers": {"context7": {"command": "uvx", "args": ["context7-mcp"]}}}`)

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: stub.bin,
		McpConfig:   mcpConfig,
	})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	got := mustReadJSON(t, result.ConfigPath)
	mcp, ok := got["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("wrapper missing mcp block: %v", got)
	}
	servers, ok := mcp["servers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.servers is not an object: %v", mcp)
	}
	entry, _ := servers["context7"].(map[string]any)
	if entry == nil || entry["command"] != "uvx" {
		t.Errorf("context7 entry missing/wrong on fresh install: %v", servers)
	}
	args, _ := entry["args"].([]any)
	if len(args) != 1 || args[0] != "context7-mcp" {
		t.Errorf("context7.args = %v", args)
	}
	if _, present := got["$include"]; present {
		t.Errorf("fresh install should not emit $include: %v", got["$include"])
	}
}

func TestPrepareOpenclawConfigFailsClosedOnMalformedMcpConfig(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userCfgPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	cases := map[string]json.RawMessage{
		"unparseable_json":      json.RawMessage(`{not-json}`),
		"entry_missing_command": json.RawMessage(`{"mcpServers": {"bad": {}}}`),
		"entry_wrong_shape":     json.RawMessage(`{"mcpServers": {"bad": "not-an-object"}}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			stub := installOpenclawStub(t, map[string]openclawResponse{
				"config file":                   {stdout: userCfgPath},
				"config get agents.list --json": {stdout: "null"},
			})
			_, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
				OpenclawBin: stub.bin,
				McpConfig:   raw,
			})
			if err == nil {
				t.Fatalf("prepareOpenclawConfig succeeded on %s; expected fail closed", name)
			}
			if !strings.Contains(err.Error(), "mcp_config") && !strings.Contains(err.Error(), "mcp_servers") {
				t.Errorf("error %q does not name the mcp_config step", err.Error())
			}
		})
	}
}

func TestPrepareOpenclawSkillWriteMatchesScanPath(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	for _, sub := range []string{workDir, filepath.Join(envRoot, "output"), filepath.Join(envRoot, "logs")} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{

		"config file": {stdout: filepath.Join(t.TempDir(), "absent-openclaw.json")},
	})

	skills := []SkillContextForEnv{
		{Name: "Issue Review", Content: "Review issues thoroughly."},
		{Name: "Local Dev", Content: "Spin up the local dev env."},
	}

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	cfgPath := result.ConfigPath
	if err := writeContextFiles(workDir, "runtime-n", TaskContextForEnv{
		IssueID:     "issue-1",
		AgentSkills: skills,
	}, nil); err != nil {
		t.Fatalf("writeContextFiles: %v", err)
	}

	конфиг := mustReadJSON(t, cfgPath)
	wsDir := конфиг["agents"].(map[string]any)["defaults"].(map[string]any)["workspace"].(string)
	for _, s := range skills {
		want := filepath.Join(wsDir, "skills", sanitizeSkillName(s.Name), "SKILL.md")
		if _, err := os.Stat(want); err != nil {
			t.Errorf("openclaw scan target %s missing — Goosar's write path and the openclaw scanner are out of sync: %v", want, err)
		}
	}
}

func TestPrepareEnvironmentOpenclawWiresConfigPath(t *testing.T) {
	wsRoot := t.TempDir()

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: filepath.Join(t.TempDir(), "absent.json")},
	})

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: wsRoot,
		WorkspaceID:    "ws-1",
		TaskID:         "11111111-2222-3333-4444-555555555555",
		AgentName:      "scout",
		Provider:       "runtime-n",
		OpenclawBin:    stub.bin,
		Task: TaskContextForEnv{
			IssueID: "issue-1",
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if env.OpenclawConfigPath == "" {
		t.Fatal("Prepare(openclaw) did not set OpenclawConfigPath")
	}
	got := mustReadJSON(t, env.OpenclawConfigPath)
	workspace := got["agents"].(map[string]any)["defaults"].(map[string]any)["workspace"]
	if workspace != env.WorkDir {
		t.Errorf("agents.defaults.workspace = %v, want %q", workspace, env.WorkDir)
	}

	if env.OpenclawIncludeRoot != "" {
		t.Errorf("OpenclawIncludeRoot = %q on fresh install, want empty", env.OpenclawIncludeRoot)
	}
}

func TestPrepareEnvironmentOpenclawWiresIncludeRoot(t *testing.T) {
	wsRoot := t.TempDir()

	userCfgDir := t.TempDir()
	userCfgPath := filepath.Join(userCfgDir, "openclaw.json")
	if err := os.WriteFile(userCfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userCfgPath},
		"config get agents.list --json": {stdout: "null"},
	})

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: wsRoot,
		WorkspaceID:    "ws-1",
		TaskID:         "33333333-2222-3333-4444-555555555555",
		AgentName:      "scout",
		Provider:       "runtime-n",
		OpenclawBin:    stub.bin,
		Task:           TaskContextForEnv{IssueID: "issue-1"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if env.OpenclawIncludeRoot != userCfgDir {
		t.Errorf("OpenclawIncludeRoot = %q, want %q (dirname of active config so daemon can grant OPENCLAW_INCLUDE_ROOTS)", env.OpenclawIncludeRoot, userCfgDir)
	}
}

func TestPrepareEnvironmentOpenclawFailsClosed(t *testing.T) {
	wsRoot := t.TempDir()

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: errors.New("openclaw config validation failed")},
	})

	_, err := Prepare(PrepareParams{
		WorkspacesRoot: wsRoot,
		WorkspaceID:    "ws-1",
		TaskID:         "22222222-2222-3333-4444-555555555555",
		AgentName:      "scout",
		Provider:       "runtime-n",
		OpenclawBin:    stub.bin,
		Task:           TaskContextForEnv{IssueID: "issue-1"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("Prepare(openclaw) succeeded when CLI errored; expected fail closed")
	}
	if !strings.Contains(err.Error(), "prepare openclaw config") {
		t.Errorf("error message %q does not name the openclaw config step", err.Error())
	}
}

func TestPrepareEnvironmentNonOpenclawSkipsConfig(t *testing.T) {
	wsRoot := t.TempDir()

	stub := installOpenclawStub(t, map[string]openclawResponse{})

	taskIDs := map[string]string{
		"runtime-c": "aaaaaaaa-1111-2222-3333-4444444444aa",
		"runtime-m": "aaaaaaaa-1111-2222-3333-4444444444bb",
		"runtime-j": "aaaaaaaa-1111-2222-3333-4444444444cc",
		"runtime-l": "aaaaaaaa-1111-2222-3333-4444444444dd",
	}
	for provider, taskID := range taskIDs {
		t.Run(provider, func(t *testing.T) {
			env, err := Prepare(PrepareParams{
				WorkspacesRoot: wsRoot,
				WorkspaceID:    "ws-1",
				TaskID:         taskID,
				AgentName:      "scout",
				Provider:       provider,
				Task:           TaskContextForEnv{IssueID: "issue-1"},
			}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatalf("Prepare(%s): %v", provider, err)
			}
			if env.OpenclawConfigPath != "" {
				t.Errorf("provider %s should not get an OpenclawConfigPath, got %q", provider, env.OpenclawConfigPath)
			}
			if _, err := os.Stat(filepath.Join(env.RootDir, openclawConfigFile)); !os.IsNotExist(err) {
				t.Errorf("provider %s left a stray openclaw-config.json", provider)
			}
		})
	}
	if len(stub.calls) != 0 {
		t.Errorf("non-openclaw providers shelled out to openclaw CLI %d times: %+v", len(stub.calls), stub.calls)
	}
}

func TestBuildPerTaskOpenclawConfigOmitsGatewayWhenZero(t *testing.T) {
	t.Parallel()

	конфиг := buildPerTaskOpenclawConfig(
		"", false, "", nil, false, "/workdir", nil, false,
		OpenclawGatewayPin{},
	)
	if _, present := конфиг["gateway"]; present {
		t.Errorf("zero gateway must not emit a gateway block, got %v", конфиг["gateway"])
	}
}

func TestBuildPerTaskOpenclawConfigWritesGatewayBlock(t *testing.T) {
	t.Parallel()

	pin := OpenclawGatewayPin{
		Host:  "gw.internal",
		Port:  18789,
		Token: "secret-token",
		TLS:   true,
	}
	конфиг := buildPerTaskOpenclawConfig(
		"", false, "", nil, false, "/workdir", nil, false,
		pin,
	)

	gw, ok := конфиг["gateway"].(map[string]any)
	if !ok {
		t.Fatalf("expected gateway map, got %T: %v", конфиг["gateway"], конфиг["gateway"])
	}
	if gw["host"] != "gw.internal" {
		t.Errorf("gateway.host = %v, want %q", gw["host"], "gw.internal")
	}
	if gw["port"] != 18789 {
		t.Errorf("gateway.port = %v, want %d", gw["port"], 18789)
	}

	auth, ok := gw["auth"].(map[string]any)
	if !ok {
		t.Fatalf("expected gateway.auth map, got %T: %v", gw["auth"], gw["auth"])
	}
	if auth["mode"] != "token" {
		t.Errorf("gateway.auth.mode = %v, want %q", auth["mode"], "token")
	}
	if auth["token"] != "secret-token" {
		t.Errorf("gateway.auth.token = %v, want %q", auth["token"], "secret-token")
	}
	if gw["tls"] != true {
		t.Errorf("gateway.tls = %v, want true", gw["tls"])
	}
}

func TestBuildPerTaskOpenclawConfigPartialGatewayOmitsZeroFields(t *testing.T) {
	t.Parallel()

	конфиг := buildPerTaskOpenclawConfig(
		"", false, "", nil, false, "/workdir", nil, false,
		OpenclawGatewayPin{Host: "gw.internal", Port: 18789},
	)
	gw := конфиг["gateway"].(map[string]any)
	if _, present := gw["auth"]; present {
		t.Errorf("auth block must be omitted when token is empty, got %v", gw["auth"])
	}
	if _, present := gw["tls"]; present {
		t.Errorf("tls field must be omitted when false, got %v", gw["tls"])
	}
}

func TestIsOpenclawKeyMissing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"pre-2026.6 No value at", errors.New("openclaw: No value at agents.list"), true},
		{"pre-2026.6 Path not found", errors.New("openclaw config get agents.list --json: Path not found"), true},
		{"not set", errors.New("agents.list is not set"), true},
		{"missing key", errors.New("missing key: agents.list"), true},
		{
			"2026.6.x Config path not found (verbatim #3028)",
			errors.New("openclaw config get agents.list --json: exit status 1 (stderr: Config path not found: agents.list. Run openclaw config validate to inspect config shape.)"),
			true,
		},
		{"real failure stays an error", errors.New("openclaw: failed to read config: permission denied"), false},
		{"malformed json is not a missing key", errors.New("parse output: invalid character 'x'"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOpenclawKeyMissing(tc.err); got != tc.want {
				t.Errorf("isOpenclawKeyMissing(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestPrepareOpenclawConfigNewSchemaOmitsAgentsList(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userConfigPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	registry := `[{"id":"main","identityName":"Beau","identitySource":"identity","workspace":"/Users/cob/.openclaw/workspace","agentDir":"/Users/cob/.openclaw/agents/main/agent","model":"anthropic/claude-sonnet-4-6","bindings":0,"isDefault":true}]`
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {stdout: userConfigPath},

		"config get agents.list --json": {err: errors.New("openclaw config get agents.list --json: exit status 1 (stderr: Config path not found: agents.list. Run openclaw config validate to inspect config shape.)")},

		"agents list --json": {stdout: registry},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	got := mustReadJSON(t, result.ConfigPath)
	agents := got["agents"].(map[string]any)
	if agents["defaults"].(map[string]any)["workspace"] != workDir {
		t.Errorf("defaults.workspace not pinned to workDir")
	}
	if _, present := agents["list"]; present {
		t.Fatalf("agents.list must be omitted for a registry-sourced (2026.6.x) host — OpenClaw rejects it; got %v", agents["list"])
	}
}

func TestPrepareOpenclawConfigNewSchemaEmptyRegistry(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userConfigPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: userConfigPath},
		"config get agents.list --json": {err: errors.New("Config path not found: agents.list")},
		"agents list --json":            {stdout: "[]"},
	})

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	got := mustReadJSON(t, result.ConfigPath)
	agents := got["agents"].(map[string]any)
	if _, present := agents["list"]; present {
		t.Errorf("agents.list should be omitted for empty registry, got %v", agents["list"])
	}
	if agents["defaults"].(map[string]any)["workspace"] != workDir {
		t.Errorf("defaults.workspace not set")
	}
}
