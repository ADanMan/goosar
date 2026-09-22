//go:build windows

package agent

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHideAgentWindowSetsCreateNewConsole(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo", "hi")
	hideAgentWindow(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr should be initialized")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Error("HideWindow should be true")
	}
	if cmd.SysProcAttr.CreationFlags&createNewConsole == 0 {
		t.Errorf("CreationFlags should include CREATE_NEW_CONSOLE (0x%x), got 0x%x",
			createNewConsole, cmd.SysProcAttr.CreationFlags)
	}
	const createNoWindow = 0x08000000
	if cmd.SysProcAttr.CreationFlags&createNoWindow != 0 {
		t.Errorf("CreationFlags must NOT include CREATE_NO_WINDOW (0x%x), got 0x%x — "+
			"this causes grandchild popups",
			createNoWindow, cmd.SysProcAttr.CreationFlags)
	}
}

func TestHideAgentWindowPreservesExistingSysProcAttr(t *testing.T) {
	const createUnicodeEnvironment = 0x00000400
	cmd := exec.Command("cmd.exe", "/c", "echo", "hi")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags:    createUnicodeEnvironment,
		NoInheritHandles: true,
	}
	hideAgentWindow(cmd)

	if !cmd.SysProcAttr.NoInheritHandles {
		t.Error("NoInheritHandles set by caller should be preserved")
	}
	if cmd.SysProcAttr.CreationFlags&createUnicodeEnvironment == 0 {
		t.Error("existing CreationFlags bits (CREATE_UNICODE_ENVIRONMENT) should be preserved")
	}
	if cmd.SysProcAttr.CreationFlags&createNewConsole == 0 {
		t.Error("CREATE_NEW_CONSOLE should be OR'd into existing flags")
	}
}

func TestRuntimeEInitializeRetrySuppressedWithoutConfirmedTreeCleanup(t *testing.T) {
	if runtimeEInitializeRetrySupported() {
		t.Fatal("Codex initialize retry must remain disabled until Windows descendant cleanup is positively confirmed")
	}
}

func TestRuntimeEWindowsInheritedStdoutDescendantCleanupIsBounded(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "fake_codex.go")
	exePath := filepath.Join(tempDir, "fake_codex.exe")
	pidPath := filepath.Join(tempDir, "descendant.pid")
	const source = `package main
import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"time"
)
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" { fmt.Println("codex-cli windows-test"); return }
	if len(os.Args) > 1 && os.Args[1] == "descendant" { time.Sleep(30*time.Second); return }
	s := bufio.NewScanner(os.Stdin)
	if !s.Scan() { return }
	fmt.Println("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}")
	if !s.Scan() { return }
	if !s.Scan() { return }
	child := exec.Command(os.Args[0], "descendant")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil { panic(err) }
	_ = os.WriteFile(os.Getenv("DESCENDANT_PID_FILE"), []byte(fmt.Sprint(child.Process.Pid)), 0600)
	time.Sleep(time.Hour)
}`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", exePath, sourcePath)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Windows fake app-server: %v: %s", err, output)
	}

	runtimeEGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	runtimeEProcessWaitDelayNanos.Store(int64(200 * time.Millisecond))
	t.Cleanup(func() {
		runtimeEGracefulShutdownTimeoutNanos.Store(0)
		runtimeEProcessWaitDelayNanos.Store(0)
		if raw, err := os.ReadFile(pidPath); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
				if process, err := os.FindProcess(pid); err == nil {
					_ = process.Kill()
				}
			}
		}
	})

	var logs bytes.Buffer
	backend, err := New("runtime-e", Config{
		ExecutablePath: exePath,
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		Env:            map[string]string{"DESCENDANT_PID_FILE": pidPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{
		Timeout:          5 * time.Second,
		HandshakeTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cleanup exceeded bound: %s", elapsed)
	}
	if result.Status != "failed" || !strings.Contains(result.Error, "thread/start") {
		t.Fatalf("expected thread/start failure, got %+v", result)
	}
	entries := parseJSONLogEntries(t, logs.String())
	failure := findRuntimeELifecyclePhase(t, entries, "thread_start_failure")
	if failure["cleanup_confirmed"] != false || failure["reaped"] != false {
		t.Fatalf("Windows tree cleanup must remain unconfirmed: %v", failure)
	}
	if phaseCount(entries, "thread_start_response") != 0 {
		t.Fatalf("unexpected thread_start_response: %v", entries)
	}
}

func phaseCount(entries []map[string]any, phase string) int {
	count := 0
	for _, entry := range entries {
		if entry["phase"] == phase {
			count++
		}
	}
	return count
}
