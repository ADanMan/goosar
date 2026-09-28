package project

import (
	"context"
	"encoding/json"
	"testing"
)

// mustCreateProject — общий помощник для подтестов ниже: создаёт проект и
// падает через t.Fatal при ошибке, чтобы каждый сценарий не повторял
// одинаковую проверку err != nil.
func mustCreateProject(t *testing.T, ctx context.Context, s *Store, p CreateParams) Project {
	t.Helper()
	proj, _, err := s.CreateProject(ctx, p)
	if err != nil {
		t.Fatalf("CreateProject(%q): %v", p.Title, err)
	}
	return proj
}

func TestProjectLifecycle(t *testing.T) {
	db := newTestStore(t)
	wsID, acctID := seedWorkspace(t, db)
	s := NewStore(db)
	ctx := context.Background()

	t.Run("create with resources and read back", func(t *testing.T) {
		repoRef, _ := json.Marshal(map[string]any{"url": "https://github.com/acme/repo"})
		proj := mustCreateProject(t, ctx, s, CreateParams{
			WorkspaceID: wsID, Title: "Roadmap", Status: "planned", Priority: "high",
			Resources: []CreateResourceParams{{Type: "github_repo", Ref: repoRef, CreatedBy: &acctID}},
		})

		switch {
		case proj.Status != "planned":
			t.Errorf("status = %q, want planned", proj.Status)
		case proj.Priority != "high":
			t.Errorf("priority = %q, want high", proj.Priority)
		case proj.ResourceCount != 1:
			t.Errorf("resource_count = %d, want 1", proj.ResourceCount)
		}

		fetched, resources, err := s.GetProject(ctx, wsID, proj.ID)
		if err != nil {
			t.Fatalf("GetProject: %v", err)
		}
		if fetched.ID != proj.ID {
			t.Errorf("fetched id %q, want %q", fetched.ID, proj.ID)
		}
		if len(resources) != 1 || resources[0].Type != "github_repo" {
			t.Errorf("resources after fetch: %+v", resources)
		}

		if _, _, err := s.GetProject(ctx, wsID, "00000000-0000-0000-0000-000000000000"); err != ErrNotFound {
			t.Errorf("GetProject on missing id: got %v, want ErrNotFound", err)
		}
	})

	t.Run("local_directory rejects a second resource on the same daemon", func(t *testing.T) {
		proj := mustCreateProject(t, ctx, s, CreateParams{WorkspaceID: wsID, Title: "P1", Status: "planned", Priority: "none"})

		first, _ := json.Marshal(map[string]any{"daemon_id": "shared-daemon", "local_path": "/tmp/one"})
		if _, err := s.CreateResource(ctx, wsID, proj.ID, CreateResourceParams{Type: "local_directory", Ref: first, CreatedBy: &acctID}); err != nil {
			t.Fatalf("first local_directory: %v", err)
		}

		second, _ := json.Marshal(map[string]any{"daemon_id": "shared-daemon", "local_path": "/tmp/two"})
		_, err := s.CreateResource(ctx, wsID, proj.ID, CreateResourceParams{Type: "local_directory", Ref: second, CreatedBy: &acctID})
		if err != ErrResourceConflict {
			t.Fatalf("second local_directory on the same daemon: got %v, want ErrResourceConflict", err)
		}
	})

	t.Run("rename then delete", func(t *testing.T) {
		proj := mustCreateProject(t, ctx, s, CreateParams{WorkspaceID: wsID, Title: "Draft", Status: "planned", Priority: "none"})

		renamed := "Final Name"
		updated, _, err := s.UpdateProject(ctx, wsID, proj.ID, UpdatePatch{Title: &renamed})
		if err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
		if updated.Title != renamed {
			t.Errorf("title after rename = %q, want %q", updated.Title, renamed)
		}

		if err := s.DeleteProject(ctx, wsID, proj.ID); err != nil {
			t.Fatalf("DeleteProject: %v", err)
		}
		if _, _, err := s.GetProject(ctx, wsID, proj.ID); err != ErrNotFound {
			t.Errorf("GetProject after delete: got %v, want ErrNotFound", err)
		}
	})
}

func TestSearchProjectsExcludesClosedByDefault(t *testing.T) {
	db := newTestStore(t)
	wsID, _ := seedWorkspace(t, db)
	s := NewStore(db)
	ctx := context.Background()

	mustCreateProject(t, ctx, s, CreateParams{WorkspaceID: wsID, Title: "Alpha Launch", Status: "planned", Priority: "none"})
	closedDesc := "mentions alpha in its description"
	mustCreateProject(t, ctx, s, CreateParams{WorkspaceID: wsID, Title: "Beta", Description: &closedDesc, Status: "completed", Priority: "none"})

	openOnly, openTotal, err := s.SearchProjects(ctx, wsID, "alpha", 20, 0, false)
	if err != nil {
		t.Fatalf("SearchProjects(include_closed=false): %v", err)
	}
	if openTotal != 1 || len(openOnly) != 1 || openOnly[0].MatchSource != "title" {
		t.Fatalf("open-only search: total=%d results=%+v", openTotal, openOnly)
	}

	_, everythingTotal, err := s.SearchProjects(ctx, wsID, "alpha", 20, 0, true)
	if err != nil {
		t.Fatalf("SearchProjects(include_closed=true): %v", err)
	}
	if everythingTotal != 2 {
		t.Fatalf("everythingTotal = %d, want 2", everythingTotal)
	}
}
