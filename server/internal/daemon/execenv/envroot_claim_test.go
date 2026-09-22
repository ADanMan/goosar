package execenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPredictRootDirDistinctForSharedUUIDv7Prefix(t *testing.T) {
	t.Parallel()
	ids := []string{
		"01a01ec0-e69d-7000-8000-000000000001",
		"01a01ec0-f014-7000-8000-000000000002",
		"01a01ec0-f927-7000-8000-000000000003",
	}
	seen := make(map[string]string, len(ids))
	for _, id := range ids {
		root := PredictRootDir("/root", "ws-uuid", id)
		if prev, dup := seen[root]; dup {
			t.Fatalf("tasks %s and %s share env root %q — a truncated task id is back", prev, id, root)
		}
		seen[root] = id
	}
}

func TestTaskKeyReadsTheRandomTail(t *testing.T) {
	t.Parallel()
	const id = "01a01ec0-e69d-7000-8000-0123456789ab"
	got := taskKey(id)
	if len(got) != taskKeyLen {
		t.Fatalf("taskKey(%q) = %q (len %d), want len %d", id, got, len(got), taskKeyLen)
	}
	if want := "0123456789ab"; got != want {
		t.Fatalf("taskKey(%q) = %q, want %q — the segment must come from the random tail", id, got, want)
	}
	if short := taskKey("abc"); short != "abc" {
		t.Fatalf("taskKey on a sub-length input = %q, want it returned as-is", short)
	}
	a := fmt.Sprintf("agent/%s/%s", sanitizeName("Reviewer"), taskKey("01a01ec0-e69d-7000-8000-000000000001"))
	b := fmt.Sprintf("agent/%s/%s", sanitizeName("Reviewer"), taskKey("01a01ec0-f014-7000-8000-000000000002"))
	if a == b {
		t.Fatalf("both tasks resolved to branch %q", a)
	}
}

func prepareClaimTest(t *testing.T, workspacesRoot, workspaceID, taskID, agent string) (*Environment, error) {
	t.Helper()
	return Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    workspaceID,
		TaskID:         taskID,
		AgentName:      agent,
		Task:           TaskContextForEnv{IssueID: taskID},
	}, testLogger())
}

func TestPrepareDoesNotDeleteConcurrentTaskEnv(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()
	const (
		taskA = "01a01ec0-e69d-7000-8000-000000000001"
		taskB = "01a01ec0-f014-7000-8000-000000000002"
	)
	envA, err := prepareClaimTest(t, workspacesRoot, "ws-collision", taskA, "Agent A")
	if err != nil {
		t.Fatalf("Prepare task A: %v", err)
	}
	defer envA.Cleanup(true)
	marker := filepath.Join(envA.WorkDir, "task-a-work.txt")
	if err := os.WriteFile(marker, []byte("A"), 0o644); err != nil {
		t.Fatalf("seed task A work: %v", err)
	}
	envB, err := prepareClaimTest(t, workspacesRoot, "ws-collision", taskB, "Agent B")
	if err != nil {
		t.Fatalf("Prepare task B: %v", err)
	}
	defer envB.Cleanup(true)
	if envA.RootDir == envB.RootDir {
		t.Fatalf("both tasks share env root %q", envA.RootDir)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("task B's Prepare destroyed task A's live env root: %v", err)
	}
}

func TestPrepareRefusesEnvRootOwnedByAnotherTask(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()
	const taskID = "01a01ec0-e69d-7000-8000-0123456789ab"
	envRoot := PredictRootDir(workspacesRoot, "ws-owned", taskID)
	if err := os.MkdirAll(filepath.Join(envRoot, "workdir"), 0o755); err != nil {
		t.Fatalf("seed env root: %v", err)
	}
	if err := writeEnvRootOwner(envRoot, "11111111-2222-3333-4444-555555555555"); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	survivor := filepath.Join(envRoot, "workdir", "other-task-work.txt")
	if err := os.WriteFile(survivor, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("seed work: %v", err)
	}
	_, err := prepareClaimTest(t, workspacesRoot, "ws-owned", taskID, "Intruder")
	if err == nil {
		t.Fatal("Prepare accepted an env root owned by another task")
	}
	if !strings.Contains(err.Error(), "belongs to task") {
		t.Fatalf("error = %v, want it to name the owning task", err)
	}
	if _, statErr := os.Stat(survivor); statErr != nil {
		t.Fatalf("Prepare deleted the other task's work despite failing: %v", statErr)
	}
}

func TestPrepareRefusesUnownedEnvRootWithWork(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()
	const taskID = "01a01ec0-e69d-7000-8000-0123456789ab"
	envRoot := PredictRootDir(workspacesRoot, "ws-unowned", taskID)
	if err := os.MkdirAll(filepath.Join(envRoot, "workdir"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := prepareClaimTest(t, workspacesRoot, "ws-unowned", taskID, "Agent"); err == nil || !strings.Contains(err.Error(), "names no owning task") {
		t.Fatalf("Prepare on an unowned non-empty root: err = %v, want fail-closed", err)
	}

	empty := PredictRootDir(workspacesRoot, "ws-empty", taskID)
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	lock, reset, err := claimEnvRoot(empty, taskID)
	if err != nil {
		t.Fatalf("claimEnvRoot on an empty unowned root: %v", err)
	}
	defer releaseEnvRootLock(lock)
	if reset {
		t.Fatal("adopting an empty directory should not ask for a reset")
	}
	if owner, _ := readEnvRootOwner(empty); owner != taskID {
		t.Fatalf("owner = %q, want %q", owner, taskID)
	}
}

func TestPrepareRefusesOverlappingExecutionOfSameTask(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()
	const taskID = "01a01ec0-e69d-7000-8000-0123456789ab"

	live, err := prepareClaimTest(t, workspacesRoot, "ws-redispatch", taskID, "Redispatch")
	if err != nil {
		t.Fatalf("first execution: %v", err)
	}
	defer live.Cleanup(true)
	inFlight := filepath.Join(live.WorkDir, "in-flight.txt")
	if err := os.WriteFile(inFlight, []byte("still running"), 0o644); err != nil {
		t.Fatalf("seed in-flight work: %v", err)
	}

	second, err := prepareClaimTest(t, workspacesRoot, "ws-redispatch", taskID, "Redispatch")
	if err == nil {
		second.Cleanup(true)
		t.Fatal("a second execution of the same task took over a live env root")
	}
	if !errors.Is(err, ErrEnvRootBusy) {
		t.Fatalf("error = %v, want ErrEnvRootBusy", err)
	}
	if _, statErr := os.Stat(inFlight); statErr != nil {
		t.Fatalf("the re-dispatched execution destroyed live work: %v", statErr)
	}

	live.ReleaseLock()
	live.ReleaseLock()
	third, err := prepareClaimTest(t, workspacesRoot, "ws-redispatch", taskID, "Redispatch")
	if err != nil {
		t.Fatalf("recovery execution refused a released env root: %v", err)
	}
	if _, statErr := os.Stat(inFlight); !os.IsNotExist(statErr) {
		t.Fatalf("recovery did not reset the env root; stale file still present (%v)", statErr)
	}
	if err := third.Cleanup(true); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

func TestClaimEnvRootRepairsTornOwnerMarker(t *testing.T) {
	t.Parallel()
	envRoot := filepath.Join(t.TempDir(), "ws", "0123456789ab")
	if err := os.MkdirAll(envRoot, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(envRoot, envRootOwnerFile), nil, 0o644); err != nil {
		t.Fatalf("seed torn marker: %v", err)
	}
	const id = "aaaaaaaa-1111-2222-3333-0123456789ab"
	lock, _, err := claimEnvRoot(envRoot, id)
	if err != nil {
		t.Fatalf("claimEnvRoot wedged on a torn marker: %v", err)
	}
	defer releaseEnvRootLock(lock)
	if owner, _ := readEnvRootOwner(envRoot); owner != id {
		t.Fatalf("owner = %q, want the repairing task %q", owner, id)
	}
}

func TestClaimEnvRootSurvivesPreparationHelper(t *testing.T) {
	workspacesRoot := t.TempDir()
	const taskID = "01a01ec0-e69d-7000-8000-0123456789ab"
	claim, err := ClaimEnvRoot(workspacesRoot, "ws-helper-claim", taskID)
	if err != nil {
		t.Fatalf("ClaimEnvRoot: %v", err)
	}
	defer claim.Release()

	params := PrepareParams{
		WorkspacesRoot:    workspacesRoot,
		WorkspaceID:       "ws-helper-claim",
		TaskID:            taskID,
		Provider:          "runtime-c",
		EnvRootPreclaimed: true,
		Task:              TaskContextForEnv{IssueID: "issue-helper-claim"},
	}
	env, err := PrepareIsolated(t.Context(), preparationHelperTestCommand(), params, testLogger())
	if err != nil {
		t.Fatalf("PrepareIsolated: %v", err)
	}
	if env.RootDir != claim.RootDir() {
		t.Fatalf("helper prepared %q, claim covers %q", env.RootDir, claim.RootDir())
	}
	if _, err := ClaimEnvRoot(workspacesRoot, "ws-helper-claim", taskID); !errors.Is(err, ErrEnvRootBusy) {
		t.Fatalf("a second execution claimed the env root of a task that just prepared: err = %v", err)
	}

	params.EnvRootPreclaimed = false
	if _, err := PrepareIsolated(t.Context(), preparationHelperTestCommand(), params, testLogger()); err == nil || !strings.Contains(err.Error(), "running execution") {
		t.Fatalf("Prepare without EnvRootPreclaimed under a held claim: err = %v, want busy", err)
	}
}

func TestLockEnvRootForReuseExcludesConcurrentContinuation(t *testing.T) {
	t.Parallel()
	priorRoot := filepath.Join(t.TempDir(), "ws", "0123456789ab")
	if err := os.MkdirAll(priorRoot, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	first, err := LockEnvRootForReuse(priorRoot)
	if err != nil || first == nil {
		t.Fatalf("first lock: claim=%v err=%v", first, err)
	}
	defer first.Release()
	if _, err := LockEnvRootForReuse(priorRoot); !errors.Is(err, ErrEnvRootBusy) {
		t.Fatalf("second continuation: err = %v, want ErrEnvRootBusy", err)
	}
	if owner, _ := readEnvRootOwner(priorRoot); owner != "" {
		t.Fatalf("reuse lock must not write an owner marker, got %q", owner)
	}
	if claim, err := LockEnvRootForReuse(filepath.Join(priorRoot, "missing")); claim != nil || err != nil {
		t.Fatalf("missing root: claim=%v err=%v, want nil/nil", claim, err)
	}
}
