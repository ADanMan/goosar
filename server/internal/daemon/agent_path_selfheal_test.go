package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newSelfHealTestDaemon() *Daemon {
	return &Daemon{
		logger:        slog.Default(),
		resolvedPaths: make(map[string]healedAgent),
		agentVersions: make(map[string]string),
	}
}

func stubDetectVersionFromPath(t *testing.T) {
	t.Helper()
	orig := detectAgentVersion
	detectAgentVersion = func(_ context.Context, path string) (string, error) {
		return filepath.Base(filepath.Dir(filepath.Dir(path))), nil
	}
	t.Cleanup(func() { detectAgentVersion = orig })
}

func writeExecStub(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for stub %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", path, err)
	}
}

func installVersionedCodex(t *testing.T, root, version, stableBin string) string {
	t.Helper()
	versioned := filepath.Join(root, "Caskroom", "codex", version, "bin", "codex")
	writeExecStub(t, versioned)
	link := filepath.Join(stableBin, "codex")
	_ = os.Remove(link)
	if err := os.MkdirAll(stableBin, 0o755); err != nil {
		t.Fatalf("mkdir stable bin: %v", err)
	}
	if err := os.Symlink(versioned, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, versioned, err)
	}
	return canonicalExecutablePath(link)
}

func TestResolveAgentEntry_SelfHealsAfterInPlaceUpgrade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/exec-bit layout is POSIX-specific")
	}
	stubDetectVersionFromPath(t)

	root := t.TempDir()
	stableBin := filepath.Join(root, "bin")
	t.Setenv("PATH", stableBin)

	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))

	v1 := installVersionedCodex(t, root, "0.144.1", stableBin)
	if !strings.Contains(v1, "0.144.1") {
		t.Fatalf("pinned path %q does not point into the v1 versioned dir", v1)
	}

	d := newSelfHealTestDaemon()
	d.setAgentVersion("runtime-e", "0.144.1")
	entry := AgentEntry{Path: v1, Command: "codex"}
	ctx := context.Background()

	if got, ver := d.resolveAgentEntry(ctx, "runtime-e", entry); got.Path != v1 || ver != "0.144.1" {
		t.Fatalf("live pinned path/version rewritten: got (%q, %q), want (%q, %q)", got.Path, ver, v1, "0.144.1")
	}

	if err := os.RemoveAll(filepath.Join(root, "Caskroom", "codex", "0.144.1")); err != nil {
		t.Fatalf("remove v1 tree: %v", err)
	}
	if agentExecutablePresent(v1) {
		t.Fatalf("v1 path still present after removing its tree: %q", v1)
	}
	v2 := installVersionedCodex(t, root, "0.144.3", stableBin)

	got, ver := d.resolveAgentEntry(ctx, "runtime-e", entry)
	if got.Path != v2 {
		t.Fatalf("self-heal resolved %q, want re-resolved v2 %q", got.Path, v2)
	}
	if !agentExecutablePresent(got.Path) {
		t.Fatalf("self-healed path is not runnable: %q", got.Path)
	}

	if ver != "0.144.3" {
		t.Fatalf("returned version not paired with healed path: got %q, want %q", ver, "0.144.3")
	}
	if v := d.agentVersion("runtime-e"); v != "0.144.3" {
		t.Fatalf("version cache not updated in lockstep: got %q, want %q", v, "0.144.3")
	}

	t.Setenv("PATH", filepath.Join(root, "empty"))
	if got, ver := d.resolveAgentEntry(ctx, "runtime-e", entry); got.Path != v2 || ver != "0.144.3" {
		t.Fatalf("cached self-heal not reused: got (%q, %q), want (%q, %q)", got.Path, ver, v2, "0.144.3")
	}
}

func TestResolveAgentEntry_RejectsBelowMinVersionAfterUpgrade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/exec-bit layout is POSIX-specific")
	}
	stubDetectVersionFromPath(t)

	root := t.TempDir()
	stableBin := filepath.Join(root, "bin")
	t.Setenv("PATH", stableBin)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))

	v1 := installVersionedCodex(t, root, "0.144.1", stableBin)

	d := newSelfHealTestDaemon()
	d.setAgentVersion("runtime-e", "0.144.1")
	entry := AgentEntry{Path: v1, Command: "codex"}
	ctx := context.Background()

	if err := os.RemoveAll(filepath.Join(root, "Caskroom", "codex", "0.144.1")); err != nil {
		t.Fatalf("remove v1 tree: %v", err)
	}
	installVersionedCodex(t, root, "0.9.0", stableBin)

	got, ver := d.resolveAgentEntry(ctx, "runtime-e", entry)
	if got.Path != v1 {
		t.Fatalf("below-min build was adopted: got %q, want stale pinned %q", got.Path, v1)
	}

	if ver != "0.144.1" {
		t.Fatalf("returned version corrupted to below-min: got %q, want %q", ver, "0.144.1")
	}
	d.resolvedPathsMu.RLock()
	_, cached := d.resolvedPaths["runtime-e"]
	d.resolvedPathsMu.RUnlock()
	if cached {
		t.Fatalf("below-min build was cached as a healed path")
	}
	if v := d.agentVersion("runtime-e"); v != "0.144.1" {
		t.Fatalf("cached version was corrupted to a below-min value: got %q, want %q", v, "0.144.1")
	}
}

func TestResolveAgentEntry_ReturnsVersionPairedWithCachedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/exec-bit layout is POSIX-specific")
	}
	stubDetectVersionFromPath(t)

	root := t.TempDir()
	stableBin := filepath.Join(root, "bin")
	t.Setenv("PATH", stableBin)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))

	v1 := installVersionedCodex(t, root, "0.144.1", stableBin)
	d := newSelfHealTestDaemon()
	d.setAgentVersion("runtime-e", "0.144.1")
	entry := AgentEntry{Path: v1, Command: "codex"}
	ctx := context.Background()

	if err := os.RemoveAll(filepath.Join(root, "Caskroom", "codex", "0.144.1")); err != nil {
		t.Fatalf("remove v1 tree: %v", err)
	}
	v2 := installVersionedCodex(t, root, "0.144.3", stableBin)

	if got, ver := d.resolveAgentEntry(ctx, "runtime-e", entry); got.Path != v2 || ver != "0.144.3" {
		t.Fatalf("initial heal wrong: got (%q, %q)", got.Path, ver)
	}

	d.setAgentVersion("runtime-e", "0.0.1-stale")

	got, ver := d.resolveAgentEntry(ctx, "runtime-e", entry)
	if got.Path != v2 {
		t.Fatalf("cached path not reused: got %q, want %q", got.Path, v2)
	}
	if ver != "0.144.3" {
		t.Fatalf("returned version came from the skewed shared cache, not the cached path pairing: got %q, want %q", ver, "0.144.3")
	}
}

func TestResolveAgentEntry_HealedPathWinsOverReappearingPinnedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/exec-bit layout is POSIX-specific")
	}
	stubDetectVersionFromPath(t)

	root := t.TempDir()
	stableBin := filepath.Join(root, "bin")
	t.Setenv("PATH", stableBin)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))

	v1 := installVersionedCodex(t, root, "0.144.1", stableBin)
	d := newSelfHealTestDaemon()
	d.setAgentVersion("runtime-e", "0.144.1")
	entry := AgentEntry{Path: v1, Command: "codex"}
	ctx := context.Background()

	if err := os.RemoveAll(filepath.Join(root, "Caskroom", "codex", "0.144.1")); err != nil {
		t.Fatalf("remove v1 tree: %v", err)
	}
	v2 := installVersionedCodex(t, root, "0.144.3", stableBin)
	if got, ver := d.resolveAgentEntry(ctx, "runtime-e", entry); got.Path != v2 || ver != "0.144.3" {
		t.Fatalf("initial heal wrong: got (%q, %q), want (%q, %q)", got.Path, ver, v2, "0.144.3")
	}

	writeExecStub(t, v1)
	if !agentExecutablePresent(v1) {
		t.Fatalf("v1 path should be runnable again after reinstall: %q", v1)
	}

	got, ver := d.resolveAgentEntry(ctx, "runtime-e", entry)
	if got.Path != v2 {
		t.Fatalf("reappearing pinned path hijacked the launch: got %q, want healed %q", got.Path, v2)
	}
	if ver != "0.144.3" {
		t.Fatalf("returned version not paired with healed path: got %q, want %q", ver, "0.144.3")
	}
}

func TestResolveAgentEntry_UninstalledLeavesEntryUnchanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-specific layout")
	}

	root := t.TempDir()
	stableBin := filepath.Join(root, "bin")
	t.Setenv("PATH", stableBin)

	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	pinned := installVersionedCodex(t, root, "0.144.1", stableBin)

	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove install root: %v", err)
	}

	d := newSelfHealTestDaemon()
	entry := AgentEntry{Path: pinned, Command: "codex"}
	got, _ := d.resolveAgentEntry(context.Background(), "runtime-e", entry)
	if got.Path != pinned {
		t.Fatalf("expected entry unchanged when binary is gone, got %q want %q", got.Path, pinned)
	}
}

func TestResolveAgentEntry_NoCommandNoHeal(t *testing.T) {
	d := newSelfHealTestDaemon()
	entry := AgentEntry{Path: filepath.Join(t.TempDir(), "does-not-exist"), Command: ""}
	if got, _ := d.resolveAgentEntry(context.Background(), "runtime-e", entry); got.Path != entry.Path {
		t.Fatalf("entry with empty Command was rewritten: got %q, want %q", got.Path, entry.Path)
	}
}
