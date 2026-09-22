package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/daemon/execenv"
)

func TestRunTaskSquadLeaderReusesWorkdirBeforeGCMetaWritten(t *testing.T) {
	t.Parallel()

	d, argsFile, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()

	first := leaderReuseTestTask("task-first")
	firstResult, err := d.runTask(context.Background(), first, "runtime-c", 0, d.logger)
	if err != nil {
		t.Fatalf("first runTask: %v", err)
	}
	if firstResult.SessionID == "" || firstResult.WorkDir == "" {
		t.Fatalf("first result missing resume state: %+v", firstResult)
	}

	if _, err := os.Stat(filepath.Join(firstResult.EnvRoot, ".gc_meta.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no .gc_meta.json before the completion handler runs; stat err = %v", err)
	}

	second := leaderReuseTestTask("task-second")
	second.PriorSessionID = firstResult.SessionID
	second.PriorWorkDir = firstResult.WorkDir
	secondResult, err := d.runTask(context.Background(), second, "runtime-c", 0, d.logger)
	if err != nil {
		t.Fatalf("second runTask: %v", err)
	}
	if secondResult.WorkDir != firstResult.WorkDir {
		t.Fatalf("second WorkDir = %q, want reused leader workdir %q", secondResult.WorkDir, firstResult.WorkDir)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read claude args: %v", err)
	}
	if !strings.Contains(string(args), "--resume\nsession-leader-reuse\n") {
		t.Fatalf("second claude invocation did not resume prior session; args:\n%s", args)
	}
}

func TestRunTaskSquadLeaderDoesNotReuseExternalPriorWorkdir(t *testing.T) {
	t.Parallel()

	d, _, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()

	externalWorkDir := t.TempDir()
	task := leaderReuseTestTask("task-external")
	task.PriorSessionID = "session-leader-reuse"
	task.PriorWorkDir = externalWorkDir

	result, err := d.runTask(context.Background(), task, "runtime-c", 0, d.logger)
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if result.WorkDir == externalWorkDir {
		t.Fatalf("leader reused external workdir %q without a local-directory lock", externalWorkDir)
	}
}

func TestShouldReusePriorWorkdirNonLeaderReusesUnchanged(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	task := leaderReuseTestTask("task-non-leader")
	task.IsLeaderTask = false
	task.PriorWorkDir = filepath.Join(root, "anything", "workdir")
	if !shouldReusePriorWorkdir(task, nil, root) {
		t.Fatal("non-leader task must reuse its prior workdir without any provenance requirement")
	}
}

func TestShouldReusePriorWorkdirSquadLeaderAcceptsManagedProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")

	task := leaderReuseTestTask("task-accept")
	task.PriorWorkDir = workDir
	if !shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader did not reuse a fully-provenanced managed workdir %q", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsNonManagedPathUnderRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userDir := filepath.Join(root, "ws-leader", "user-project")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user dir: %v", err)
	}

	task := leaderReuseTestTask("task-contained-user-dir")
	task.PriorWorkDir = userDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused non-managed path %q merely because it is under WorkspacesRoot", userDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsManagedShapeWithoutProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")

	task := leaderReuseTestTask("task-without-provenance")
	task.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused marked workdir %q without managed-env provenance", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsMismatchedProvenanceOwner(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "other-agent")

	task := leaderReuseTestTask("task-mismatched-provenance")
	task.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused workdir %q with provenance owned by another agent", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsMismatchedTaskMarker(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "other-agent", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")

	task := leaderReuseTestTask("task-mismatched-marker")
	task.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused workdir %q with a marker for another agent", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsRegularFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	if err := os.MkdirAll(filepath.Dir(workDir), 0o755); err != nil {
		t.Fatalf("mkdir workdir parent: %v", err)
	}
	if err := os.WriteFile(workDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write workdir file: %v", err)
	}

	task := leaderReuseTestTask("task-file-workdir")
	task.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused regular file %q as a workdir", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsEmptyAgentID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")

	task := leaderReuseTestTask("task-empty-agent")
	task.AgentID = ""
	task.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatal("leader with an empty AgentID must not reuse a prior workdir")
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	external := t.TempDir()
	parent := filepath.Join(root, "ws-leader", "12345678")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}

	workDir := filepath.Join(parent, "workdir")
	if err := os.Symlink(external, workDir); err != nil {
		t.Fatalf("symlink workdir -> external: %v", err)
	}

	task := leaderReuseTestTask("task-symlink-escape")
	task.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused a workdir symlinked outside WorkspacesRoot (%q -> %q)", workDir, external)
	}
}

func newLeaderReuseTestDaemon(t *testing.T) (*Daemon, string, func()) {
	t.Helper()

	testDir := t.TempDir()
	fakeBin := filepath.Join(testDir, "claude")
	argsFile := filepath.Join(testDir, "claude-args.txt")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "` + argsFile + `"
printf '%s\n' '--invocation-end--' >> "` + argsFile + `"
IFS= read -r _
printf '%s\n' '{"type":"system","session_id":"session-leader-reuse"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-leader-reuse","result":"done"}'
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         logger,
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-leader": {ID: "rt-leader", Provider: "runtime-c"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: t.TempDir(),
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents: map[string]AgentEntry{
				"runtime-c": {Path: fakeBin},
			},
		},
	}
	return d, argsFile, srv.Close
}

func writeLeaderTaskMarker(t *testing.T, workDir, agentID, issueID string) {
	t.Helper()

	markerPath := filepath.Join(workDir, execenv.TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	marker := []byte(`{"managed_by":"` + execenv.TaskContextMarkerManagedBy + `","agent_id":"` + agentID + `","issue_id":"` + issueID + `"}`)
	if err := os.WriteFile(markerPath, marker, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

func writeLeaderManagedEnvProvenance(t *testing.T, workDir, workspaceID, issueID, agentID string) {
	t.Helper()

	envRoot := filepath.Dir(workDir)
	if err := os.MkdirAll(envRoot, 0o755); err != nil {
		t.Fatalf("mkdir env root: %v", err)
	}
	if err := execenv.WriteManagedEnvProvenance(envRoot, execenv.ManagedEnvProvenance{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		AgentID:     agentID,
	}); err != nil {
		t.Fatalf("write managed env provenance: %v", err)
	}
}

func leaderReuseTestTask(id string) Task {
	return Task{
		ID:           id,
		WorkspaceID:  "ws-leader",
		RuntimeID:    "rt-leader",
		IssueID:      "issue-leader",
		AgentID:      "agent-leader",
		AuthToken:    "mat_leader_reuse",
		IsLeaderTask: true,
		Agent: &AgentData{
			ID:   "agent-leader",
			Name: "leader-agent",
		},
	}
}

func TestLockReusablePriorEnvRootDeclinesBusyRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "0123456789ab", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")

	d := &Daemon{cfg: Config{WorkspacesRoot: root}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	task := leaderReuseTestTask("task-busy")
	task.PriorWorkDir = workDir

	claim, ok := d.lockReusablePriorEnvRoot(task, nil, "")
	if !ok || claim == nil {
		t.Fatalf("first continuation: claim=%v ok=%v, want a held claim", claim, ok)
	}
	defer claim.Release()
	if _, err := os.Stat(filepath.Join(filepath.Dir(workDir), ".task_lock")); err != nil {
		t.Fatalf("lock file not in the prior env root: %v", err)
	}
	if _, ok := d.lockReusablePriorEnvRoot(task, nil, ""); ok {
		t.Fatal("second continuation reused a prior env root another execution holds")
	}

	outside := leaderReuseTestTask("task-outside")
	outside.IsLeaderTask = false
	outside.PriorWorkDir = filepath.Join(t.TempDir(), "user-dir")
	if err := os.MkdirAll(outside.PriorWorkDir, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if c, ok := d.lockReusablePriorEnvRoot(outside, nil, ""); !ok || c != nil {
		t.Fatalf("outside root: claim=%v ok=%v, want reuse without a lock", c, ok)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(outside.PriorWorkDir), ".task_lock")); !os.IsNotExist(err) {
		t.Fatal("lock file written outside the workspaces root")
	}
}
