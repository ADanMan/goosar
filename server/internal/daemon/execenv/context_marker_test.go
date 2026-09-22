package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureWorkspacesRootMarker(t *testing.T) {
	t.Run("writes a valid marker into an empty root", func(t *testing.T) {
		root := t.TempDir()
		if err := EnsureWorkspacesRootMarker(root); err != nil {
			t.Fatalf("EnsureWorkspacesRootMarker: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(root, TaskContextMarkerRelPath))
		if err != nil {
			t.Fatalf("read root marker: %v", err)
		}
		var marker struct {
			ManagedBy string `json:"managed_by"`
		}
		if err := json.Unmarshal(data, &marker); err != nil {
			t.Fatalf("unmarshal root marker: %v\n%s", err, string(data))
		}
		if marker.ManagedBy != TaskContextMarkerManagedBy {
			t.Fatalf("managed_by = %q, want %q", marker.ManagedBy, TaskContextMarkerManagedBy)
		}
	})

	t.Run("is idempotent when a matching marker exists", func(t *testing.T) {
		root := t.TempDir()
		if err := EnsureWorkspacesRootMarker(root); err != nil {
			t.Fatalf("first EnsureWorkspacesRootMarker: %v", err)
		}
		if err := EnsureWorkspacesRootMarker(root); err != nil {
			t.Fatalf("second EnsureWorkspacesRootMarker: %v", err)
		}
	})

	t.Run("refuses to overwrite a foreign file", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, TaskContextMarkerRelPath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		foreign := []byte(`{"managed_by":"someone-else"}`)
		if err := os.WriteFile(path, foreign, 0o644); err != nil {
			t.Fatalf("write foreign file: %v", err)
		}
		if err := EnsureWorkspacesRootMarker(root); err == nil {
			t.Fatal("expected error for foreign file at marker path, got nil")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("re-read foreign file: %v", err)
		}
		if string(data) != string(foreign) {
			t.Fatalf("foreign file was clobbered: %s", string(data))
		}
	})

	t.Run("self-heals a corrupt marker from a torn write", func(t *testing.T) {

		for _, corrupt := range []string{"", "{", "not-json", `{"managed_by":`} {
			root := t.TempDir()
			path := filepath.Join(root, TaskContextMarkerRelPath)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, []byte(corrupt), 0o644); err != nil {
				t.Fatalf("seed corrupt marker %q: %v", corrupt, err)
			}
			if err := EnsureWorkspacesRootMarker(root); err != nil {
				t.Fatalf("reclaim corrupt marker %q: unexpected error %v", corrupt, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read reclaimed marker: %v", err)
			}
			var marker struct {
				ManagedBy string `json:"managed_by"`
			}
			if err := json.Unmarshal(data, &marker); err != nil {
				t.Fatalf("reclaimed marker not valid JSON for input %q: %v\n%s", corrupt, err, string(data))
			}
			if marker.ManagedBy != TaskContextMarkerManagedBy {
				t.Fatalf("managed_by = %q, want %q (input %q)", marker.ManagedBy, TaskContextMarkerManagedBy, corrupt)
			}
		}
	})

	t.Run("rejects empty root", func(t *testing.T) {
		if err := EnsureWorkspacesRootMarker(""); err == nil {
			t.Fatal("expected error for empty workspaces root, got nil")
		}
	})
}

func TestPrepare_WritesWorkspacesRootMarker(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-test-001",
		TaskID:         "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		AgentName:      "Test Agent",
		Task: TaskContextForEnv{
			IssueID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	data, err := os.ReadFile(filepath.Join(root, TaskContextMarkerRelPath))
	if err != nil {
		t.Fatalf("read root marker after Prepare: %v", err)
	}
	if !strings.Contains(string(data), TaskContextMarkerManagedBy) {
		t.Fatalf("root marker missing managed_by discriminator: %s", string(data))
	}
}

func TestReuse_SelfHealsWorkspacesRootMarker(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-reuse-001",
		TaskID:         "b2c3d4e5-f6a7-8901-bcde-f23456789012",
		AgentName:      "Reuse Agent",
		Task:           TaskContextForEnv{IssueID: "b2c3d4e5-f6a7-8901-bcde-f23456789012"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	rootMarker := filepath.Join(root, TaskContextMarkerRelPath)
	if err := os.Remove(rootMarker); err != nil {
		t.Fatalf("remove root marker: %v", err)
	}

	reused := Reuse(ReuseParams{
		WorkspacesRoot: root,
		WorkDir:        env.WorkDir,
		Task:           TaskContextForEnv{IssueID: "b2c3d4e5-f6a7-8901-bcde-f23456789012"},
	}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil for an existing workdir")
	}
	if _, err := os.Stat(rootMarker); err != nil {
		t.Fatalf("Reuse did not restore the root marker: %v", err)
	}
}
