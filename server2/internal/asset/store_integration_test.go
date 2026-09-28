package asset

import (
	"context"
	"testing"
)

func TestAssetCreateGetListDelete(t *testing.T) {
	db := newTestDB(t)
	wsID, acctID := seedWorkspace(t, db)
	ticketID := seedTicket(t, db, wsID, acctID)
	s := NewStore(db)
	ctx := context.Background()

	att, err := s.Create(ctx, CreateParams{
		WorkspaceID: wsID, IssueID: &ticketID, UploaderType: "member", UploaderID: acctID,
		Filename: "notes.txt", StorageURL: "local://workspaces/" + wsID + "/abc-notes.txt",
		ContentType: "text/plain", SizeBytes: 42,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if att.DownloadURL != "/api/attachments/"+att.ID+"/download" {
		t.Errorf("DownloadURL = %q, want the self-referencing /api/attachments/{id}/download path", att.DownloadURL)
	}
	if att.StorageKey != "workspaces/"+wsID+"/abc-notes.txt" {
		t.Errorf("StorageKey = %q, unexpected", att.StorageKey)
	}

	got, err := s.Get(ctx, wsID, att.ID)
	if err != nil || got.Filename != "notes.txt" {
		t.Fatalf("Get: %+v, err %v", got, err)
	}
	byID, err := s.GetByID(ctx, att.ID)
	if err != nil || byID.ID != att.ID {
		t.Fatalf("GetByID: %+v, err %v", byID, err)
	}

	list, err := s.ListForIssue(ctx, wsID, ticketID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListForIssue: got %d, err %v", len(list), err)
	}

	if err := s.Delete(ctx, wsID, att.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, wsID, att.ID); err != ErrNotFound {
		t.Errorf("Get after delete: got %v, want ErrNotFound", err)
	}
}

func TestAttachToCommentReplacesSet(t *testing.T) {
	db := newTestDB(t)
	wsID, acctID := seedWorkspace(t, db)
	ticketID := seedTicket(t, db, wsID, acctID)
	s := NewStore(db)
	ctx := context.Background()

	var commentID string
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO ticket_notes (ticket_id, tn_author_type, tn_author_id, tn_body)
		VALUES ($1, 'member', $2, 'a comment') RETURNING id`, ticketID, acctID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	a1, err := s.Create(ctx, CreateParams{WorkspaceID: wsID, UploaderType: "member", UploaderID: acctID,
		Filename: "a.txt", StorageURL: "local://x/a.txt", ContentType: "text/plain", SizeBytes: 1})
	if err != nil {
		t.Fatalf("Create a1: %v", err)
	}
	a2, err := s.Create(ctx, CreateParams{WorkspaceID: wsID, UploaderType: "member", UploaderID: acctID,
		Filename: "b.txt", StorageURL: "local://x/b.txt", ContentType: "text/plain", SizeBytes: 1})
	if err != nil {
		t.Fatalf("Create a2: %v", err)
	}

	if err := s.AttachToComment(ctx, wsID, ticketID, commentID, []string{a1.ID, a2.ID}); err != nil {
		t.Fatalf("AttachToComment: %v", err)
	}
	list, err := s.ListForComment(ctx, commentID)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListForComment: got %d, err %v", len(list), err)
	}

	// Замена набора: остаётся только a1.
	if err := s.AttachToComment(ctx, wsID, ticketID, commentID, []string{a1.ID}); err != nil {
		t.Fatalf("AttachToComment (replace): %v", err)
	}
	list, err = s.ListForComment(ctx, commentID)
	if err != nil || len(list) != 1 || list[0].ID != a1.ID {
		t.Fatalf("ListForComment after replace: %+v, err %v", list, err)
	}

	byComments, err := s.ListForComments(ctx, []string{commentID})
	if err != nil || len(byComments[commentID]) != 1 {
		t.Fatalf("ListForComments: %+v, err %v", byComments, err)
	}
}
