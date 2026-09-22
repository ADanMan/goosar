package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareManagedIssueEnvWritesProvenance(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-prov-001",
		TaskID:         "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName:      "Prov Agent",
		Task: TaskContextForEnv{
			IssueID: "issue-prov-1",
			AgentID: "agent-prov-1",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	prov, err := ReadManagedEnvProvenance(env.RootDir)
	if err != nil {
		t.Fatalf("read managed env provenance: %v", err)
	}
	if prov.ManagedBy != ManagedEnvProvenanceManagedBy {
		t.Fatalf("managed_by = %q, want %q", prov.ManagedBy, ManagedEnvProvenanceManagedBy)
	}
	if prov.WorkspaceID != "ws-prov-001" || prov.IssueID != "issue-prov-1" || prov.AgentID != "agent-prov-1" {
		t.Fatalf("provenance owner mismatch: %+v", prov)
	}
}

func TestPrepareLocalDirectoryWritesNoProvenance(t *testing.T) {
	root := t.TempDir()
	localDir := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-prov-local",
		TaskID:         "bbbbbbbb-cccc-dddd-eeee-ffffffffffff",
		AgentName:      "Local Agent",
		LocalWorkDir:   localDir,
		Task: TaskContextForEnv{
			IssueID: "issue-prov-local",
			AgentID: "agent-prov-local",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if _, err := ReadManagedEnvProvenance(env.RootDir); !os.IsNotExist(err) {
		t.Fatalf("local_directory Prepare must not write managed env provenance; got err = %v", err)
	}

	if _, err := os.Stat(filepath.Join(localDir, managedEnvProvenanceFile)); !os.IsNotExist(err) {
		t.Fatal("managed env provenance leaked into the user's local directory")
	}
}

func TestPrepareNonIssueEnvWritesNoProvenance(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-prov-chat",
		TaskID:         "cccccccc-dddd-eeee-ffff-000000000000",
		AgentName:      "Chat Agent",
		Task: TaskContextForEnv{
			ChatSessionID: "chat-1",
			AgentID:       "agent-chat",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if _, err := ReadManagedEnvProvenance(env.RootDir); !os.IsNotExist(err) {
		t.Fatalf("non-issue Prepare must not write managed env provenance; got err = %v", err)
	}
}
