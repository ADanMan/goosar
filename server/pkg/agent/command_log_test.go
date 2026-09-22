package agent

import (
	"bytes"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
)

func TestRedactAgentCommandArgsPreservesOnlySafeFlagNames(t *testing.T) {
	t.Parallel()

	overlongFlag := "--" + strings.Repeat("a", maxLoggedAgentCommandFlagLen)
	args := []string{
		"--api-key", "api-key-secret",
		"--dash-prefixed-secret", "-sTk9xQZ-secretvalue",
		"--token=token-secret",
		"--header", "Authorization: Bearer header-secret",
		"-c", `model_providers.example.api_key="config-secret"`,
		"--future-secret", "future-value-secret",
		"prompt-secret",
		"--verbose",
		"-not-a-short-flag",
		overlongFlag,
	}
	want := []string{
		"--api-key", redactedAgentCommandArg,
		"--dash-prefixed-secret", redactedAgentCommandArg,
		"--token",
		"--header", redactedAgentCommandArg,
		"-c", redactedAgentCommandArg,
		"--future-secret", redactedAgentCommandArg,
		redactedAgentCommandArg,
		"--verbose",
		redactedAgentCommandArg,
		redactedAgentCommandArg,
	}

	got := redactAgentCommandArgs(args, nil)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("redactAgentCommandArgs = %v, want %v", got, want)
	}
}

func TestTrustedAgentCommandPositionalsFollowSourceIndexes(t *testing.T) {
	t.Parallel()

	invocationArgs := []string{"acp", "--api-key", "-sTk9xQZ-secretvalue", "acp"}
	finalArgs := append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "wrapper.ps1"}, invocationArgs...)
	trusted := trustedAgentCommandPositionals(finalArgs,
		newAgentCommandLogArgs(invocationArgs, trustAgentCommandPositional(0, "acp")))

	got := redactAgentCommandArgs(finalArgs, trusted)
	want := []string{
		redactedAgentCommandArg, redactedAgentCommandArg, redactedAgentCommandArg, redactedAgentCommandArg, redactedAgentCommandArg,
		"acp", "--api-key", redactedAgentCommandArg, redactedAgentCommandArg,
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("source-aware redaction = %v, want %v", got, want)
	}

	if trusted := trustedAgentCommandPositionals([]string{"acp", "other"}, newAgentCommandLogArgs(invocationArgs, trustAgentCommandPositional(0, "acp"))); len(trusted) != 0 {
		t.Fatalf("mismatched argv must trust nothing, got %v", trusted)
	}
}

func TestLogAgentCommandRedactsTextAndJSON(t *testing.T) {
	t.Parallel()

	args := []string{
		"--api-key", "api-key-secret",
		"--dash-prefixed-secret", "-sTk9xQZ-secretvalue",
		"--token=token-secret",
		"--header", "Authorization: Bearer header-secret",
		"-c", `model_providers.example.api_key="config-secret"`,
		"--future-secret", "future-value-secret",
		"prompt-secret",
	}
	secrets := []string{
		"api-key-secret",
		"-sTk9xQZ-secretvalue",
		"token-secret",
		"Authorization: Bearer header-secret",
		"config-secret",
		"future-value-secret",
		"prompt-secret",
	}

	for _, tc := range []struct {
		name    string
		handler func(*bytes.Buffer) slog.Handler
	}{
		{"text", func(buf *bytes.Buffer) slog.Handler { return slog.NewTextHandler(buf, nil) }},
		{"json", func(buf *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(buf, nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cfg := Config{Logger: slog.New(tc.handler(&buf)), provider: "codex"}
			cmd := &exec.Cmd{Path: "/opt/goosar/bin/codex", Args: append([]string{"codex"}, args...)}
			cfg.logAgentCommandWithPrompt(cmd, newAgentCommandLogArgs(args), 123)

			output := buf.String()
			for _, секрет := range secrets {
				if strings.Contains(output, секрет) {
					t.Errorf("%s log exposed %q: %s", tc.name, секрет, output)
				}
			}
			for _, diagnostic := range []string{
				"agent command", "provider", "codex", "/opt/goosar/bin/codex",
				"--api-key", "--token", "--header", "-c", "--future-secret",
				redactedAgentCommandArg, "arg_count", "prompt_bytes",
			} {
				if !strings.Contains(output, diagnostic) {
					t.Errorf("%s log omitted diagnostic %q: %s", tc.name, diagnostic, output)
				}
			}
		})
	}
}

func TestBackendFactoriesSetCommandLogProvider(t *testing.T) {
	t.Parallel()

	runtimeC, err := New("runtime-c", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(runtime-c): %v", err)
	}
	if got := runtimeC.(*runtimeCBackend).cfg.provider; got != "runtime-c" {
		t.Fatalf("runtime-c log provider = %q, want runtime-c", got)
	}
}

func TestNoBackendLogsRawAgentCommandArgs(t *testing.T) {
	t.Parallel()
	out, err := exec.Command("grep", "-rln", `Info("agent command"`, "--include=*.go", "--exclude=*_test.go", ".").Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		files := strings.Fields(string(out))
		for _, f := range files {
			if f != "./command_log.go" {
				t.Errorf("%s logs \"agent command\" directly; use cfg.logAgentCommand", f)
			}
		}
	}
}
