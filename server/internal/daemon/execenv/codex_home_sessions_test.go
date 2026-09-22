package execenv

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func seedFakeRollout(t *testing.T, sharedSessions, y, m, d, sessionID string, size int) string {
	t.Helper()
	dir := filepath.Join(sharedSessions, y, m, d)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir rollout dir: %v", err)
	}
	path := filepath.Join(dir, "rollout-"+y+"-"+m+"-"+d+"T00-00-00-"+sessionID+".jsonl")
	body := make([]byte, size)
	for i := range body {
		body[i] = 'x'
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	return path
}

func seedLegacySessionsSymlink(t *testing.T, codexHome, sharedSessions string) {
	t.Helper()
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("mkdir codex-home: %v", err)
	}
	if err := os.MkdirAll(sharedSessions, 0o755); err != nil {
		t.Fatalf("mkdir shared sessions: %v", err)
	}
	if err := os.Symlink(sharedSessions, filepath.Join(codexHome, "sessions")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("directory symlink unavailable on this Windows session: %v", err)
		}
		t.Fatalf("seed legacy sessions symlink: %v", err)
	}
}

func TestPrepareCodexSessionsDir_FreshCreatesEmptyLocalDir(t *testing.T) {
	t.Parallel()
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("mkdir codex-home: %v", err)
	}
	sharedHome := t.TempDir()

	if err := prepareCodexSessionsDir(codexHome, sharedHome, CodexHomeOptions{}, testLogger()); err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}

	sessions := filepath.Join(codexHome, "sessions")
	fi, err := os.Lstat(sessions)
	if err != nil {
		t.Fatalf("sessions not created: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("fresh sessions must be a real dir, not a symlink")
	}
	if !fi.IsDir() {
		t.Error("fresh sessions must be a directory")
	}
	entries, _ := os.ReadDir(sessions)
	if len(entries) != 0 {
		t.Errorf("fresh sessions must be empty, has %d entries", len(entries))
	}
}

func TestPrepareCodexSessionsDir_RealDirIsAuthoritative(t *testing.T) {
	t.Parallel()
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	sessions := filepath.Join(codexHome, "sessions", "2026", "07", "13")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	own := filepath.Join(sessions, "rollout-2026-07-13T00-00-00-own-session.jsonl")
	if err := os.WriteFile(own, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write own rollout: %v", err)
	}

	if err := prepareCodexSessionsDir(codexHome, t.TempDir(), CodexHomeOptions{}, testLogger()); err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}

	if data, err := os.ReadFile(own); err != nil || string(data) != "keep me" {
		t.Errorf("task-local rollout must be preserved, got err=%v data=%q", err, data)
	}
	fi, _ := os.Lstat(filepath.Join(codexHome, "sessions"))
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("authoritative sessions must remain a real dir")
	}
}

func TestPrepareCodexSessionsDir_MigratesLegacySymlinkNoResume(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	sharedSessions := filepath.Join(root, "shared", "sessions")

	seedFakeRollout(t, sharedSessions, "2026", "07", "13", "other-session-a", 16)
	seedFakeRollout(t, sharedSessions, "2026", "07", "12", "other-session-b", 16)
	seedLegacySessionsSymlink(t, codexHome, sharedSessions)

	writeFile(t, filepath.Join(codexHome, "state_5.sqlite"), "stale")
	writeFile(t, filepath.Join(codexHome, "state_5.sqlite-wal"), "stale")
	writeFile(t, filepath.Join(codexHome, "session_index.jsonl"), "names")
	writeFile(t, filepath.Join(codexHome, "goals_1.sqlite"), "keep")

	if err := prepareCodexSessionsDir(codexHome, filepath.Dir(sharedSessions), CodexHomeOptions{}, testLogger()); err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}

	sessions := filepath.Join(codexHome, "sessions")
	fi, err := os.Lstat(sessions)
	if err != nil {
		t.Fatalf("sessions missing after migration: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("legacy symlink must be replaced with a real dir")
	}

	entries, _ := os.ReadDir(sessions)
	if len(entries) != 0 {
		t.Errorf("non-resume migration must yield an empty sessions dir, has %d entries", len(entries))
	}

	if _, err := os.Stat(filepath.Join(sharedSessions, "2026", "07", "13", "rollout-2026-07-13T00-00-00-other-session-a.jsonl")); err != nil {
		t.Errorf("shared history must not be deleted: %v", err)
	}

	assertAbsent(t, filepath.Join(codexHome, "state_5.sqlite"))
	assertAbsent(t, filepath.Join(codexHome, "state_5.sqlite-wal"))
	assertPresent(t, filepath.Join(codexHome, "session_index.jsonl"))
	assertPresent(t, filepath.Join(codexHome, "goals_1.sqlite"))
}

func TestPrepareCodexSessionsDir_MigrateWithResumeRoutesThroughStore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sharedHome := filepath.Join(root, "shared")
	codexHome := filepath.Join(root, "codex-home")
	sharedSessions := filepath.Join(sharedHome, "sessions")
	resumeID := "019f59d9-a6aa-7a53-b173-1eccc4b4c873"
	resumeSrc := seedFakeRollout(t, sharedSessions, "2026", "07", "13", resumeID, 32)
	seedFakeRollout(t, sharedSessions, "2026", "07", "11", "unrelated-session", 32)
	seedLegacySessionsSymlink(t, codexHome, sharedSessions)
	writeFile(t, filepath.Join(codexHome, "state_5.sqlite"), "stale")

	key := filepath.Join("agent-1", "issue-1")
	err := prepareCodexSessionsDir(codexHome, sharedHome, CodexHomeOptions{ResumeSessionID: resumeID, SessionStoreKey: key}, testLogger())
	if err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}

	sessions := filepath.Join(codexHome, "sessions")
	assertSessionsLinkedToStore(t, sessions, codexSessionStoreDir(sharedHome, key))

	if !CodexResumeRolloutPresent(codexHome, resumeID) {
		t.Fatal("resume rollout must be visible in the task CODEX_HOME after migration")
	}
	stored := filepath.Join(codexSessionStoreDir(sharedHome, key), "2026", "07", "13", filepath.Base(resumeSrc))
	assertZeroCopyLink(t, resumeSrc, stored)

	if CodexResumeRolloutPresent(codexHome, "unrelated-session") {
		t.Error("unrelated shared history must not be exposed to the task")
	}

	assertAbsent(t, filepath.Join(codexHome, "state_5.sqlite"))
}

func TestExposeResumeRollout_NoMatchReturnsError(t *testing.T) {
	t.Parallel()
	shared := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	local := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := exposeResumeRollout(shared, local, "missing-session", testLogger()); err == nil {
		t.Error("expected error when no rollout matches the resume session ID")
	}
}

func TestExposeResumeRollout_LinksLargeFileWithoutCopying(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	shared := filepath.Join(root, "shared", "sessions")
	local := filepath.Join(root, "local", "sessions")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}

	const big = 4 << 20
	src := seedFakeRollout(t, shared, "2026", "07", "13", "big-session", big)

	if err := exposeResumeRollout(shared, local, "big-session", testLogger()); err != nil {
		t.Fatalf("exposeResumeRollout: %v", err)
	}

	dst := filepath.Join(local, "2026", "07", "13", "rollout-2026-07-13T00-00-00-big-session.jsonl")
	if _, err := os.Lstat(dst); err != nil {
		t.Fatalf("exposed rollout missing: %v", err)
	}
	assertZeroCopyLink(t, src, dst)
}

func TestResetCodexSessionState_OnlyRemovesSessionDerived(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	sessionDerived := []string{
		"state_5.sqlite", "state_5.sqlite-shm", "state_5.sqlite-wal",
		"state_6.sqlite",
	}
	preserved := []string{

		"session_index.jsonl",
		"goals_1.sqlite", "logs_2.sqlite", "memories_1.sqlite",
		"config.toml", "auth.json", "models_cache.json",
	}
	for _, n := range append(append([]string{}, sessionDerived...), preserved...) {
		writeFile(t, filepath.Join(home, n), "x")
	}

	resetCodexSessionState(home, testLogger())

	for _, n := range sessionDerived {
		assertAbsent(t, filepath.Join(home, n))
	}
	for _, n := range preserved {
		assertPresent(t, filepath.Join(home, n))
	}
}

func TestPrepareCodexSessionsDir_MigratePreservesSessionIndex(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	sharedSessions := filepath.Join(root, "shared", "sessions")
	seedFakeRollout(t, sharedSessions, "2026", "07", "13", "some-session", 16)
	seedLegacySessionsSymlink(t, codexHome, sharedSessions)
	writeFile(t, filepath.Join(codexHome, "state_5.sqlite"), "stale")
	writeFile(t, filepath.Join(codexHome, "session_index.jsonl"), `{"id":"x","thread_name":"My thread"}`)

	if err := prepareCodexSessionsDir(codexHome, filepath.Dir(sharedSessions), CodexHomeOptions{}, testLogger()); err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}

	assertAbsent(t, filepath.Join(codexHome, "state_5.sqlite"))
	assertPresent(t, filepath.Join(codexHome, "session_index.jsonl"))
	if data, _ := os.ReadFile(filepath.Join(codexHome, "session_index.jsonl")); string(data) != `{"id":"x","thread_name":"My thread"}` {
		t.Errorf("session_index.jsonl content changed: %q", data)
	}
}

func TestExposeResumeRollout_FindsCompressedAndFlatLayouts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	shared := filepath.Join(root, "shared", "sessions")
	local := filepath.Join(root, "local", "sessions")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}

	compressed := seedRolloutAt(t, filepath.Join(shared, "2026", "07", "13", "rollout-2026-07-13T00-00-00-zst-session.jsonl.zst"), 8)
	flat := seedRolloutAt(t, filepath.Join(shared, "rollout-2026-07-13T00-00-00-flat-session.jsonl"), 8)

	for _, tc := range []struct {
		id, src, rel string
	}{
		{"zst-session", compressed, filepath.Join("2026", "07", "13", "rollout-2026-07-13T00-00-00-zst-session.jsonl.zst")},
		{"flat-session", flat, "rollout-2026-07-13T00-00-00-flat-session.jsonl"},
	} {
		if err := exposeResumeRollout(shared, local, tc.id, testLogger()); err != nil {
			t.Fatalf("expose %s: %v", tc.id, err)
		}
		assertZeroCopyLink(t, tc.src, filepath.Join(local, tc.rel))
	}
}

func TestCodexResumeRolloutPresent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	sessions := filepath.Join(codexHome, "sessions")
	seedRolloutAt(t, filepath.Join(sessions, "2026", "07", "13", "rollout-2026-07-13T00-00-00-nested.jsonl"), 8)
	seedRolloutAt(t, filepath.Join(sessions, "rollout-2026-07-13T00-00-00-flat.jsonl.zst"), 8)

	for _, id := range []string{"nested", "flat"} {
		if !CodexResumeRolloutPresent(codexHome, id) {
			t.Errorf("expected rollout %q to be found", id)
		}
	}
	if CodexResumeRolloutPresent(codexHome, "absent") {
		t.Error("absent session must not be reported present")
	}
	if CodexResumeRolloutPresent("", "nested") || CodexResumeRolloutPresent(codexHome, "") {
		t.Error("empty codexHome/sessionID must be reported absent")
	}
}

func TestPrepareCodexSessionsDir_LocalDirectoryUsesPerIssueStore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("mkdir codex-home: %v", err)
	}
	sharedHome := filepath.Join(root, "shared")

	seedFakeRollout(t, filepath.Join(sharedHome, "sessions"), "2026", "07", "13", "other-a", 16)
	seedFakeRollout(t, filepath.Join(sharedHome, "sessions"), "2026", "07", "12", "other-b", 16)

	key := filepath.Join("agent-1", "issue-1")
	if err := prepareCodexSessionsDir(codexHome, sharedHome, CodexHomeOptions{IsLocalDirectory: true, SessionStoreKey: key}, testLogger()); err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}

	sessions := filepath.Join(codexHome, "sessions")
	assertSessionsLinkedToStore(t, sessions, codexSessionStoreDir(sharedHome, key))

	entries, _ := os.ReadDir(sessions)
	if len(entries) != 0 {
		t.Errorf("per-issue store must start empty, has %d entries (whole-history leak?)", len(entries))
	}
	if CodexResumeRolloutPresent(codexHome, "other-a") {
		t.Error("machine-global history must not be visible to a local_directory task")
	}
}

func TestPrepareCodexSessionsDir_LocalDirectoryNoKeyFallsBackToEmptyDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("mkdir codex-home: %v", err)
	}
	sharedHome := filepath.Join(root, "shared")
	seedFakeRollout(t, filepath.Join(sharedHome, "sessions"), "2026", "07", "13", "other", 16)

	if err := prepareCodexSessionsDir(codexHome, sharedHome, CodexHomeOptions{IsLocalDirectory: true}, testLogger()); err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}
	sessions := filepath.Join(codexHome, "sessions")
	fi, err := os.Lstat(sessions)
	if err != nil {
		t.Fatalf("sessions not created: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("no-key local_directory must not link the shared history")
	}
	if CodexResumeRolloutPresent(codexHome, "other") {
		t.Error("machine-global history must not be visible")
	}
}

func TestPrepareCodexSessionsDir_LocalDirectoryResumeAcrossTaskIDs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sharedHome := filepath.Join(root, "shared")
	sessionID := "019f59d9-a6aa-7a53-b173-1eccc4b4c873"
	key := filepath.Join("agent-1", "issue-1")

	home1 := filepath.Join(root, "task-1", "codex-home")
	if err := os.MkdirAll(home1, 0o755); err != nil {
		t.Fatalf("mkdir home1: %v", err)
	}
	if err := prepareCodexSessionsDir(home1, sharedHome, CodexHomeOptions{IsLocalDirectory: true, SessionStoreKey: key}, testLogger()); err != nil {
		t.Fatalf("round 1 prepare: %v", err)
	}

	seedRolloutAt(t, filepath.Join(home1, "sessions", "2026", "07", "13", "rollout-2026-07-13T00-00-00-"+sessionID+".jsonl"), 32)

	home2 := filepath.Join(root, "task-2", "codex-home")
	if err := os.MkdirAll(home2, 0o755); err != nil {
		t.Fatalf("mkdir home2: %v", err)
	}
	if err := prepareCodexSessionsDir(home2, sharedHome, CodexHomeOptions{IsLocalDirectory: true, SessionStoreKey: key, ResumeSessionID: sessionID}, testLogger()); err != nil {
		t.Fatalf("round 2 prepare: %v", err)
	}

	if !CodexResumeRolloutPresent(home2, sessionID) {
		t.Fatal("round-2 local_directory task cannot see round-1 rollout — context would be silently lost")
	}
}

func TestPrepareCodexSessionsDir_ReusedStoreLinkIsAuthoritative(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sharedHome := filepath.Join(root, "shared")
	codexHome := filepath.Join(root, "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("mkdir codex-home: %v", err)
	}
	key := filepath.Join("agent-1", "issue-1")
	storeDir := codexSessionStoreDir(sharedHome, key)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	if err := os.Symlink(storeDir, filepath.Join(codexHome, "sessions")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("directory symlink unavailable on this Windows session: %v", err)
		}
		t.Fatalf("seed store link: %v", err)
	}
	sessionID := "019f59d9-a6aa-7a53-b173-1eccc4b4c873"
	seedRolloutAt(t, filepath.Join(storeDir, "2026", "07", "13", "rollout-2026-07-13T00-00-00-"+sessionID+".jsonl"), 16)

	if err := prepareCodexSessionsDir(codexHome, sharedHome, CodexHomeOptions{SessionStoreKey: key, ResumeSessionID: sessionID}, testLogger()); err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}

	assertSessionsLinkedToStore(t, filepath.Join(codexHome, "sessions"), storeDir)
	if !CodexResumeRolloutPresent(codexHome, sessionID) {
		t.Error("existing store rollout must remain resumable after reuse")
	}
}

func TestPruneCodexSessionStores(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	storeRoot := filepath.Join(home, codexSessionStoreRoot, codexSessionStoreNamespace(""))

	freshStore := filepath.Join(storeRoot, "agent-1", "issue-fresh")
	staleStore := filepath.Join(storeRoot, "agent-1", "issue-stale")
	otherStore := filepath.Join(storeRoot, "agent-2", "issue-x")
	seedRolloutAt(t, filepath.Join(freshStore, "2026", "07", "14", "rollout-2026-07-14T00-00-00-a.jsonl"), 16)
	seedRolloutAt(t, filepath.Join(staleStore, "2026", "06", "01", "rollout-2026-06-01T00-00-00-b.jsonl"), 16)
	seedRolloutAt(t, filepath.Join(otherStore, "2026", "07", "14", "rollout-2026-07-14T00-00-00-c.jsonl"), 16)

	now := time.Now()
	retention := 14 * 24 * time.Hour

	chtimesTree(t, staleStore, now.Add(-30*24*time.Hour))

	removed, bytes := PruneCodexSessionStores("", retention, now, nil, testLogger())
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (only the store idle past retention)", removed)
	}
	if bytes <= 0 {
		t.Errorf("bytesFreed = %d, want > 0", bytes)
	}

	assertAbsent(t, staleStore)
	assertPresent(t, freshStore)
	assertPresent(t, otherStore)
	assertPresent(t, filepath.Join(storeRoot, "agent-1"))

	chtimesTree(t, freshStore, now.Add(-30*24*time.Hour))
	if removed, _ := PruneCodexSessionStores("", 0, now, nil, testLogger()); removed != 0 {
		t.Errorf("retention<=0 must disable pruning, removed=%d", removed)
	}
	assertPresent(t, freshStore)
}

func TestPruneCodexSessionStores_ReopenedStoreNotReclaimed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	storeDir := codexSessionStoreDir(home, codexSessionStoreKey("", "agent-1", "issue-old"))
	sessionID := "019f59d9-a6aa-7a53-b173-1eccc4b4c873"

	seedRolloutAt(t, filepath.Join(storeDir, "2026", "06", "01", "rollout-2026-06-01T00-00-00-"+sessionID+".jsonl"), 16)
	chtimesTree(t, storeDir, time.Now().Add(-30*24*time.Hour))

	codexHome := filepath.Join(home, "task", "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("mkdir codex-home: %v", err)
	}
	if err := linkCodexSessionsToStore(filepath.Join(codexHome, "sessions"), storeDir, filepath.Join(home, "sessions"), sessionID, testLogger()); err != nil {
		t.Fatalf("linkCodexSessionsToStore: %v", err)
	}
	if !CodexResumeRolloutPresent(codexHome, sessionID) {
		t.Fatal("resume rollout must be visible after remount")
	}

	if removed, _ := PruneCodexSessionStores("", 14*24*time.Hour, time.Now(), nil, testLogger()); removed != 0 {
		t.Fatalf("removed = %d, want 0 (a just-reopened store must survive GC)", removed)
	}
	assertPresent(t, storeDir)
	if !CodexResumeRolloutPresent(codexHome, sessionID) {
		t.Error("resume rollout must still be present after GC")
	}
}

func TestPruneCodexSessionStores_ActiveStoreNotReclaimed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	storeDir := codexSessionStoreDir(home, codexSessionStoreKey("", "agent-1", "issue-active"))
	seedRolloutAt(t, filepath.Join(storeDir, "2026", "06", "01", "rollout-2026-06-01T00-00-00-x.jsonl"), 16)

	chtimesTree(t, storeDir, time.Now().Add(-30*24*time.Hour))

	held := func(p string) (func(), bool) {
		if sameCodexPath(p, storeDir) {
			return nil, false
		}
		return func() {}, true
	}
	if removed, _ := PruneCodexSessionStores("", 14*24*time.Hour, time.Now(), held, testLogger()); removed != 0 {
		t.Fatalf("removed = %d, want 0 (a reserved/in-use store must never be reclaimed)", removed)
	}
	assertPresent(t, storeDir)

	if removed, _ := PruneCodexSessionStores("", 14*24*time.Hour, time.Now(), nil, testLogger()); removed != 1 {
		t.Fatalf("removed = %d, want 1 (idle store reclaimed when reservable)", removed)
	}
	assertAbsent(t, storeDir)
}

func TestPruneCodexSessionStores_IsolatesProfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	stagingStore := codexSessionStoreDir(home, codexSessionStoreKey("staging", "agent-1", "issue-1"))
	seedRolloutAt(t, filepath.Join(stagingStore, "2026", "06", "01", "rollout-2026-06-01T00-00-00-s.jsonl"), 16)
	chtimesTree(t, stagingStore, time.Now().Add(-30*24*time.Hour))

	if removed, _ := PruneCodexSessionStores("", 14*24*time.Hour, time.Now(), nil, testLogger()); removed != 0 {
		t.Fatalf("removed = %d, want 0 — another profile's store must be out of scope", removed)
	}
	assertPresent(t, stagingStore)

	if removed, _ := PruneCodexSessionStores("staging", 14*24*time.Hour, time.Now(), nil, testLogger()); removed != 1 {
		t.Fatalf("removed = %d, want 1 — the owning profile reclaims its idle store", removed)
	}
	assertAbsent(t, stagingStore)
}

func TestCodexSessionStoreNamespaceInjective(t *testing.T) {
	t.Parallel()
	pairs := [][2]string{
		{"", "default"},
		{"staging.prod", "stagingprod"},
		{"a-b", "a_b"},
		{"prod", "Prod"},
		{"p_default", "default"},
	}
	for _, p := range pairs {
		if a, b := codexSessionStoreNamespace(p[0]), codexSessionStoreNamespace(p[1]); a == b {
			t.Errorf("profiles %q and %q collide in namespace %q", p[0], p[1], a)
		}
	}

	if codexSessionStoreNamespace("staging") != codexSessionStoreNamespace("staging") {
		t.Error("namespace must be deterministic for a fixed profile")
	}
}

func TestCodexSessionStoreNamespace_FitsDirectorySegment(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	for _, profile := range []string{"", "staging", strings.Repeat("a", 127), strings.Repeat("z", 255)} {
		ns := codexSessionStoreNamespace(profile)
		if len(ns) > 255 {
			t.Errorf("profile (len %d) -> namespace (len %d) exceeds the 255-byte single-segment limit", len(profile), len(ns))
		}
		if err := os.MkdirAll(filepath.Join(base, ns), 0o755); err != nil {
			t.Errorf("namespace for profile (len %d) could not be created: %v", len(profile), err)
		}
	}
}

func TestPruneCodexSessionStores_NoCrossProfileCollision(t *testing.T) {
	for _, pair := range [][2]string{{"", "default"}, {"staging.prod", "stagingprod"}} {
		home := t.TempDir()
		t.Setenv("CODEX_HOME", home)
		a, b := pair[0], pair[1]
		storeA := codexSessionStoreDir(home, codexSessionStoreKey(a, "agent", "issue"))
		storeB := codexSessionStoreDir(home, codexSessionStoreKey(b, "agent", "issue"))
		seedRolloutAt(t, filepath.Join(storeA, "2026", "06", "01", "rollout-2026-06-01T00-00-00-a.jsonl"), 16)
		seedRolloutAt(t, filepath.Join(storeB, "2026", "06", "01", "rollout-2026-06-01T00-00-00-b.jsonl"), 16)
		chtimesTree(t, storeA, time.Now().Add(-30*24*time.Hour))
		chtimesTree(t, storeB, time.Now().Add(-30*24*time.Hour))

		if removed, _ := PruneCodexSessionStores(a, 14*24*time.Hour, time.Now(), nil, testLogger()); removed != 1 {
			t.Fatalf("prune %q removed=%d, want 1", a, removed)
		}
		assertAbsent(t, storeA)
		assertPresent(t, storeB)

		if removed, _ := PruneCodexSessionStores(b, 14*24*time.Hour, time.Now(), nil, testLogger()); removed != 1 {
			t.Fatalf("prune %q removed=%d, want 1", b, removed)
		}
		assertAbsent(t, storeB)
	}
}

func TestPruneCodexSessionStores_RemovesEmptyAgentDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	storeRoot := filepath.Join(home, codexSessionStoreRoot, codexSessionStoreNamespace(""))
	onlyStore := filepath.Join(storeRoot, "agent-lonely", "issue-1")
	seedRolloutAt(t, filepath.Join(onlyStore, "2026", "06", "01", "rollout-2026-06-01T00-00-00-x.jsonl"), 16)

	now := time.Now()
	chtimesTree(t, onlyStore, now.Add(-30*24*time.Hour))
	if removed, _ := PruneCodexSessionStores("", 14*24*time.Hour, now, nil, testLogger()); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	assertAbsent(t, filepath.Join(storeRoot, "agent-lonely"))
}

func chtimesTree(t *testing.T, root string, ts time.Time) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, ts, ts)
	})
	if err != nil {
		t.Fatalf("chtimes tree %s: %v", root, err)
	}
}

func seedRolloutAt(t *testing.T, path string, size int) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir rollout dir: %v", err)
	}
	body := make([]byte, size)
	for i := range body {
		body[i] = 'x'
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	return path
}

func assertSessionsLinkedToStore(t *testing.T, sessions, storeDir string) {
	t.Helper()
	fi, err := os.Lstat(sessions)
	if err != nil {
		t.Fatalf("sessions link missing: %v", err)
	}
	if runtime.GOOS != "windows" {
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("sessions must link the per-issue store, got mode %v", fi.Mode())
		}
		if target, _ := os.Readlink(sessions); filepath.Clean(target) != filepath.Clean(storeDir) {
			t.Errorf("sessions link target = %q, want store %q", target, storeDir)
		}
	}
	realSessions, err := filepath.EvalSymlinks(sessions)
	if err != nil {
		t.Fatalf("eval sessions link: %v", err)
	}
	realStore, err := filepath.EvalSymlinks(storeDir)
	if err != nil {
		t.Fatalf("eval store: %v", err)
	}
	if realSessions != realStore {
		t.Errorf("sessions resolves to %q, want store %q", realSessions, realStore)
	}
}

func assertZeroCopyLink(t *testing.T, src, dst string) {
	t.Helper()
	si, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat src %s: %v", src, err)
	}
	di, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst %s: %v", dst, err)
	}
	if !os.SameFile(si, di) {
		t.Errorf("%s is a copy of %s, expected a zero-copy hard/sym link", dst, src)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, err=%v", filepath.Base(path), err)
	}
}

func assertPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("expected %s to be preserved: %v", filepath.Base(path), err)
	}
}
