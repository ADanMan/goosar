//go:build windows

package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlatformRuntimeOInvocation_RewritesCmdLauncherToPowerShellFile(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "pi.cmd")
	ps1Path := filepath.Join(dir, "pi.ps1")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0pi.ps1\" %*\r\n")
	writeFile(t, ps1Path, "# fake pi.ps1\r\n")

	fakePS := filepath.Join(dir, "powershell.exe")
	writeFile(t, fakePS, "")
	stubPowerShell(t, fakePS, true)

	multiLinePrompt := "You are running as a chat assistant for a Goosar workspace.\n\nUser message:\n我需要创建一个issue\n"
	args := []string{
		"-p",
		"--mode", "json",
		"--session", `C:\Users\X\.goosar\pi-sessions\20260528T040000.jsonl`,
		multiLinePrompt,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gotExec, gotArgs, ok := platformRuntimeOInvocation(cmdPath, args, logger)
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

	if gotArgs[len(gotArgs)-1] != multiLinePrompt {
		t.Errorf("multi-line prompt was mangled:\n got  %q\n want %q", gotArgs[len(gotArgs)-1], multiLinePrompt)
	}
}

func TestPlatformRuntimeOInvocation_SkipsWhenNotCmdOrBat(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "pi.exe")
	writeFile(t, exePath, "")

	writeFile(t, filepath.Join(dir, "pi.ps1"), "")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformRuntimeOInvocation(exePath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false for non-.cmd/.bat launcher")
	}
}

func TestPlatformRuntimeOInvocation_SkipsWhenPS1Missing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "pi.cmd")
	writeFile(t, cmdPath, "@echo off\r\n")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformRuntimeOInvocation(cmdPath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false when pi.ps1 is missing")
	}
}

func TestPlatformRuntimeOInvocation_SkipsWhenPowerShellMissing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "pi.cmd")
	ps1Path := filepath.Join(dir, "pi.ps1")
	writeFile(t, cmdPath, "@echo off\r\n")
	writeFile(t, ps1Path, "# fake\r\n")

	stubPowerShell(t, "", false)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformRuntimeOInvocation(cmdPath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false when no powershell host is available")
	}
}
