//go:build unix

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runtimeGStdinProbe(t *testing.T, prompt string) ([]string, string, Result) {
	t.Helper()

	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")

	script := fmt.Sprintf(`#!/bin/sh
: > %[1]q
for a in "$@"; do printf '%%s\n' "$a" >> %[1]q; done
cat > %[2]q
printf '%%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"ok"}'
`, argvPath, stdinPath)

	fakePath := filepath.Join(dir, "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("runtime-g", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	argvRaw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read recorded argv: %v", err)
	}
	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read recorded stdin: %v", err)
	}
	argv := strings.Split(strings.TrimSuffix(string(argvRaw), "\n"), "\n")
	return argv, string(stdinRaw), result
}

func TestRuntimeGExecuteSendsPromptOnStdinNotArgv(t *testing.T) {
	t.Parallel()

	prompt := "Please fix the build.\n" +
		"Log follows:\n" +
		`go build -ldflags "-X main.version=foo -X main.commit=bar" -o bin/server ./cmd/server` + "\n" +
		"Thanks."

	argv, stdinGot, result := runtimeGStdinProbe(t, prompt)

	if stdinGot != prompt {
		t.Errorf("prompt did not arrive on stdin intact:\n got  %q\n want %q", stdinGot, prompt)
	}

	for _, a := range argv {
		for _, needle := range []string{"-X", "ldflags", "main.version", "Please fix"} {
			if strings.Contains(a, needle) {
				t.Errorf("prompt fragment %q leaked into argv element %q; argv=%v", needle, a, argv)
			}
		}
	}

	joined := strings.Join(argv, " ")
	for _, want := range []string{"-p", "--output-format", "stream-json", "--yolo"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in argv, got %v", want, argv)
		}
	}

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
}

func TestRuntimeGExecuteLargePromptDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stdinPath := filepath.Join(dir, "stdin.txt")

	script := fmt.Sprintf(`#!/bin/sh
yes '{"type":"noise","pad":"0123456789012345678901234567890123456789"}' | head -n 8000
cat > %[1]q
printf '%%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"ok"}'
`, stdinPath)

	fakePath := filepath.Join(dir, "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	prompt := strings.Repeat("goosar cursor stdin payload 0123456789\n", 13_444)
	if len(prompt) < 512*1024 {
		t.Fatalf("test prompt too small: %d bytes", len(prompt))
	}

	backend, err := New("runtime-g", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}

	session, err := backend.Execute(t.Context(), prompt, ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read recorded stdin: %v", err)
	}
	if len(stdinRaw) != len(prompt) {
		t.Errorf("stdin truncated: got %d bytes, want %d", len(stdinRaw), len(prompt))
	}
	if string(stdinRaw) != prompt {
		t.Error("large prompt arrived corrupted on stdin")
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed (deadlock would show as timeout); error=%q", result.Status, result.Error)
	}
}

func TestRuntimeGExecuteCancelReleasesBlockedPromptWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	script := "#!/bin/sh\nsleep 120\n"
	fakePath := filepath.Join(dir, "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	prompt := strings.Repeat("blocked write payload 0123456789\n", 16_384)

	backend, err := New("runtime-g", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	session, err := backend.Execute(ctx, prompt, ExecOptions{Timeout: 120 * time.Second})
	if err != nil {
		cancel()
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case result := <-session.Result:
		if result.Status != "aborted" {
			t.Fatalf("status = %q, want aborted; error=%q", result.Status, result.Error)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("cancel did not release the blocked prompt write; Result never arrived")
	}
}

func TestRuntimeGExecuteWritesPromptVerbatim(t *testing.T) {
	t.Parallel()

	prompt := "\n  leading and trailing whitespace  \n\n"

	_, stdinGot, result := runtimeGStdinProbe(t, prompt)

	if stdinGot != prompt {
		t.Errorf("prompt was mutated before write:\n got  %q\n want %q", stdinGot, prompt)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
}
