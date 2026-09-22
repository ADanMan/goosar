//go:build windows

package agent

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	shimHelperEnv      = "GOOSAR_CURSOR_SHIM_HELPER"
	shimHelperArgvFile = "GOOSAR_CURSOR_SHIM_ARGV_FILE"
	shimHelperInFile   = "GOOSAR_CURSOR_SHIM_STDIN_FILE"
)

func TestRuntimeGShimHelperProcess(t *testing.T) {
	if os.Getenv(shimHelperEnv) != "1" {
		t.Skip("helper process; only runs when re-executed by the shim")
	}

	var forwarded []string
	for i, a := range os.Args {
		if a == "--" {
			forwarded = os.Args[i+1:]
			break
		}
	}
	if err := os.WriteFile(os.Getenv(shimHelperArgvFile), []byte(strings.Join(forwarded, "\n")), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "helper: write argv: %v\n", err)
		os.Exit(1)
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: read stdin: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv(shimHelperInFile), stdin, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "helper: write stdin: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"ok"}`)

	os.Exit(0)
}

func TestRuntimeGExecutePromptSurvivesPowerShellShim(t *testing.T) {
	hosts := availablePowerShellHosts()
	if len(hosts) == 0 {
		t.Skip("no PowerShell host available")
	}
	for _, host := range hosts {
		t.Run(filepath.Base(host), func(t *testing.T) {
			stubPowerShell(t, host, true)
			assertPromptSurvivesShim(t)
		})
	}
}

func availablePowerShellHosts() []string {
	var found []string
	for _, name := range []string{"powershell.exe", "pwsh.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			found = append(found, p)
		}
	}
	return found
}

func assertPromptSurvivesShim(t *testing.T) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary to use as native child: %v", err)
	}

	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")

	cmdPath := filepath.Join(dir, "cursor-agent.cmd")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0cursor-agent.ps1\" %*\r\n")

	ps1 := fmt.Sprintf(""+
		"$env:%s = '1'\r\n"+
		"$env:%s = '%s'\r\n"+
		"$env:%s = '%s'\r\n"+
		"& '%s' '-test.run=^TestCursorShimHelperProcess$' '--' $args\r\n"+
		"exit $LASTEXITCODE\r\n",
		shimHelperEnv,
		shimHelperArgvFile, argvPath,
		shimHelperInFile, stdinPath,
		self)
	writeFile(t, filepath.Join(dir, "cursor-agent.ps1"), ps1)

	prompt := "Please fix the build.\n" +
		`go build -ldflags "-X main.version=foo -X main.commit=bar" -o bin/server ./cmd/server` + "\n" +
		"Thanks."

	backend, err := New("runtime-g", Config{ExecutablePath: cmdPath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}

	session, err := backend.Execute(t.Context(), prompt, ExecOptions{Timeout: 5 * time.Minute})
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
		t.Fatalf("native child never recorded argv (did the shim reach it?): %v; result=%+v", err, result)
	}
	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("native child never recorded stdin: %v; result=%+v", err, result)
	}

	for _, a := range strings.Split(strings.TrimSuffix(string(argvRaw), "\n"), "\n") {
		for _, needle := range []string{"-X", "ldflags", "main.version", "Please fix"} {
			if strings.Contains(a, needle) {
				t.Errorf("prompt fragment %q leaked into native child argv element %q", needle, a)
			}
		}
	}

	gotArgv := string(argvRaw)
	for _, want := range []string{"-p", "--output-format", "stream-json", "--yolo"} {
		if !strings.Contains(gotArgv, want) {
			t.Errorf("expected %q to reach the native child; argv was %q", want, gotArgv)
		}
	}

	if string(stdinRaw) != prompt {
		t.Errorf("prompt did not survive Go → PowerShell → native child:\n got  %q\n want %q", string(stdinRaw), prompt)
	}

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
}
