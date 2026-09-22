//go:build windows

package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlatformRuntimeFInvocation_RewritesCmdLauncherToPowerShellFile(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "copilot.cmd")
	ps1Path := filepath.Join(dir, "copilot.ps1")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0copilot.ps1\" %*\r\n")
	writeFile(t, ps1Path, "# fake copilot.ps1\r\n")

	fakePS := filepath.Join(dir, "powershell.exe")
	writeFile(t, fakePS, "")
	stubPowerShell(t, fakePS, true)

	multiLinePrompt := "You are running as a local coding agent.\n\n# Context\nDo the task.\n"
	args := []string{
		"-p", multiLinePrompt,
		"--output-format", "json",
		"--allow-all",
		"--no-ask-user",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gotExec, gotArgs, ok := platformRuntimeFInvocation(cmdPath, args, logger)
	if !ok {
		t.Fatalf("expected platform rewrite to be applied, got ok=false")
	}
	if gotExec != fakePS {
		t.Errorf("argv0: got %q want %q", gotExec, fakePS)
	}

	wantArgs := append([]string{
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", ps1Path,
	}, args...)
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Errorf("argv mismatch:\n got  %#v\n want %#v", gotArgs, wantArgs)
	}

	promptIdx := -1
	for i, a := range gotArgs {
		if a == "-p" {
			promptIdx = i + 1
			break
		}
	}
	if promptIdx < 0 || promptIdx >= len(gotArgs) {
		t.Fatalf("could not find -p flag in gotArgs: %#v", gotArgs)
	}
	if gotArgs[promptIdx] != multiLinePrompt {
		t.Errorf("multi-line prompt was mangled:\n got  %q\n want %q", gotArgs[promptIdx], multiLinePrompt)
	}
}

func TestPlatformRuntimeFInvocation_SkipsWhenNotCmdOrBat(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "copilot.exe")
	writeFile(t, exePath, "")

	writeFile(t, filepath.Join(dir, "copilot.ps1"), "")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformRuntimeFInvocation(exePath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false for non-.cmd/.bat launcher")
	}
}

func TestPlatformRuntimeFInvocation_SkipsWhenPS1Missing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "copilot.cmd")
	writeFile(t, cmdPath, "@echo off\r\n")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformRuntimeFInvocation(cmdPath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false when copilot.ps1 is missing")
	}
}

func TestPlatformRuntimeFInvocation_SkipsWhenPowerShellMissing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "copilot.cmd")
	ps1Path := filepath.Join(dir, "copilot.ps1")
	writeFile(t, cmdPath, "@echo off\r\n")
	writeFile(t, ps1Path, "# fake\r\n")

	stubPowerShell(t, "", false)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformRuntimeFInvocation(cmdPath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false when no powershell host is available")
	}
}
