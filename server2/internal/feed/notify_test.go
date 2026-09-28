package feed

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/adanman/goosar/server2/internal/store"
)

func insertTicket(t *testing.T, db *store.Store, workspaceID, creatorID, status string) string {
	t.Helper()
	var id string
	err := db.Pool.QueryRow(context.Background(), `
		INSERT INTO tickets (workspace_id, tk_seq_number, tk_display_key, tk_headline, tk_status, tk_creator_type, tk_creator_id)
		VALUES ($1, $2, $3, 'Test ticket', $4, 'member', $5) RETURNING id`,
		workspaceID, time.Now().UnixNano()%1000000, fmt.Sprintf("TST-%d", time.Now().UnixNano()%1000000), status, creatorID).Scan(&id)
	if err != nil {
		t.Fatalf("insert ticket: %v", err)
	}
	return id
}

func TestNotifyRespectsMutedPreference(t *testing.T) {
	db := newTestStore(t)
	wsID, acctID := seedWorkspace(t, db)
	s := NewStore(db)
	ctx := context.Background()

	if _, err := s.MergePrefs(ctx, wsID, acctID, map[string]string{"comments": "muted"}); err != nil {
		t.Fatalf("MergePrefs: %v", err)
	}

	_, created, err := Notify(ctx, s.Q(), NotifyParams{
		WorkspaceID: wsID, RecipientType: "member", RecipientID: acctID,
		Kind: "new_comment", Title: "New comment on TST-1",
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if created {
		t.Fatal("expected Notify to skip creating an alert for a muted group")
	}

	_, created2, err := Notify(ctx, s.Q(), NotifyParams{
		WorkspaceID: wsID, RecipientType: "member", RecipientID: acctID,
		Kind: "issue_assigned", Title: "You were assigned TST-1",
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if !created2 {
		t.Fatal("expected Notify to create an alert for a non-muted group")
	}

	list, err := s.ListActive(ctx, wsID, acctID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(list) != 1 || list[0].Type != "issue_assigned" || list[0].Severity != "action_required" {
		t.Fatalf("unexpected active list: %+v", list)
	}
}

func TestArchiveCollapsesByTicket(t *testing.T) {
	db := newTestStore(t)
	wsID, acctID := seedWorkspace(t, db)
	s := NewStore(db)
	ctx := context.Background()
	ticketID := insertTicket(t, db, wsID, acctID, "in_progress")

	a1, _, err := Notify(ctx, s.Q(), NotifyParams{WorkspaceID: wsID, RecipientType: "member", RecipientID: acctID,
		Kind: "new_comment", Title: "Comment 1", TicketID: &ticketID})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Notify(ctx, s.Q(), NotifyParams{WorkspaceID: wsID, RecipientType: "member", RecipientID: acctID,
		Kind: "new_comment", Title: "Comment 2", TicketID: &ticketID})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Archive(ctx, wsID, acctID, a1.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	active, err := s.ListActive(ctx, wsID, acctID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("expected both alerts collapsed into archive, got %d active", len(active))
	}
	archived, err := s.ListArchived(ctx, wsID, acctID)
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 2 {
		t.Fatalf("expected 2 archived alerts, got %d", len(archived))
	}

	if _, err := s.Unarchive(ctx, wsID, acctID, a1.ID); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	active2, err := s.ListActive(ctx, wsID, acctID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active2) != 2 {
		t.Fatalf("expected both alerts restored, got %d active", len(active2))
	}
}

func TestArchiveCompleted(t *testing.T) {
	db := newTestStore(t)
	wsID, acctID := seedWorkspace(t, db)
	s := NewStore(db)
	ctx := context.Background()
	openTicket := insertTicket(t, db, wsID, acctID, "in_progress")
	doneTicket := insertTicket(t, db, wsID, acctID, "done")

	if _, _, err := Notify(ctx, s.Q(), NotifyParams{WorkspaceID: wsID, RecipientType: "member", RecipientID: acctID,
		Kind: "status_changed", Title: "Open", TicketID: &openTicket}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Notify(ctx, s.Q(), NotifyParams{WorkspaceID: wsID, RecipientType: "member", RecipientID: acctID,
		Kind: "status_changed", Title: "Done", TicketID: &doneTicket}); err != nil {
		t.Fatal(err)
	}

	n, err := s.ArchiveCompleted(ctx, wsID, acctID)
	if err != nil {
		t.Fatalf("ArchiveCompleted: %v", err)
	}
	if n != 1 {
		t.Fatalf("ArchiveCompleted archived %d, want 1", n)
	}
	active, err := s.ListActive(ctx, wsID, acctID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Title != "Open" {
		t.Fatalf("unexpected remaining active: %+v", active)
	}
}

func TestNotificationPreferencesValidation(t *testing.T) {
	if validatePrefs(map[string]string{"comments": "muted"}) != true {
		t.Fatal("expected valid prefs to pass")
	}
	if validatePrefs(map[string]string{"comments": "loud"}) != false {
		t.Fatal("expected invalid value to fail")
	}
	if validatePrefs(map[string]string{"unknown_group": "all"}) != false {
		t.Fatal("expected unknown group to fail")
	}
}

func TestGroupForKnownAndUnknownKinds(t *testing.T) {
	if GroupFor("mentioned") != "comments" {
		t.Fatalf("GroupFor(mentioned) = %q, want comments", GroupFor("mentioned"))
	}
	if GroupFor("some_future_kind") != "updates" {
		t.Fatalf("GroupFor(unknown) = %q, want updates fallback", GroupFor("some_future_kind"))
	}
}
