package pin

import (
	"context"
	"testing"
)

func TestPinCreateListReorderDelete(t *testing.T) {
	db := newTestDB(t)
	wsID, acctID := seedWorkspace(t, db)
	issueID := seedTicket(t, db, wsID, acctID, "TST-1")
	projectID := seedProject(t, db, wsID)
	s := NewStore(db)
	ctx := context.Background()

	p1, err := s.Create(ctx, wsID, acctID, "issue", issueID)
	if err != nil {
		t.Fatalf("Create (issue pin): %v", err)
	}
	if p1.Position != 1 {
		t.Errorf("first pin position = %v, want 1", p1.Position)
	}
	p2, err := s.Create(ctx, wsID, acctID, "project", projectID)
	if err != nil {
		t.Fatalf("Create (project pin): %v", err)
	}
	if p2.Position != 2 {
		t.Errorf("second pin position = %v, want 2 (единая последовательность позиций для обоих типов)", p2.Position)
	}

	if _, err := s.Create(ctx, wsID, acctID, "issue", issueID); err != ErrAlreadyExist {
		t.Errorf("pinning the same issue twice: got %v, want ErrAlreadyExist", err)
	}

	list, err := s.List(ctx, wsID, acctID)
	if err != nil || len(list) != 2 {
		t.Fatalf("List: got %d, err %v", len(list), err)
	}
	if list[0].ItemType != "issue" || list[1].ItemType != "project" {
		t.Errorf("List order by position: got %+v", list)
	}

	if err := s.UpdatePosition(ctx, acctID, p2.ID, 0.5); err != nil {
		t.Fatalf("UpdatePosition (project pin): %v", err)
	}
	list, err = s.List(ctx, wsID, acctID)
	if err != nil || len(list) != 2 || list[0].ItemType != "project" {
		t.Fatalf("List after reorder: got %+v, err %v", list, err)
	}

	if err := s.Delete(ctx, wsID, acctID, "issue", issueID); err != nil {
		t.Fatalf("Delete (issue pin): %v", err)
	}
	list, err = s.List(ctx, wsID, acctID)
	if err != nil || len(list) != 1 {
		t.Fatalf("List after delete: got %d, err %v", len(list), err)
	}
	if err := s.Delete(ctx, wsID, acctID, "issue", issueID); err != ErrNotFound {
		t.Errorf("Delete already-removed pin: got %v, want ErrNotFound", err)
	}
}

func TestPinExistenceChecks(t *testing.T) {
	db := newTestDB(t)
	wsID, acctID := seedWorkspace(t, db)
	issueID := seedTicket(t, db, wsID, acctID, "TST-1")
	projectID := seedProject(t, db, wsID)
	s := NewStore(db)
	ctx := context.Background()

	if ok, err := s.IssueExists(ctx, wsID, issueID); err != nil || !ok {
		t.Fatalf("IssueExists: %v, err %v", ok, err)
	}
	if ok, err := s.IssueExists(ctx, wsID, "00000000-0000-0000-0000-000000000000"); err != nil || ok {
		t.Fatalf("IssueExists (missing): %v, err %v", ok, err)
	}
	if ok, err := s.ProjectExists(ctx, wsID, projectID); err != nil || !ok {
		t.Fatalf("ProjectExists: %v, err %v", ok, err)
	}
}
