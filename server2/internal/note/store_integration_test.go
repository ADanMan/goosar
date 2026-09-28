package note

import (
	"context"
	"testing"
)

func TestCommentCRUDResolveAndReactions(t *testing.T) {
	db := newTestDB(t)
	wsID, acctID := seedWorkspace(t, db)
	ticketID := seedTicket(t, db, wsID, acctID)
	s := NewStore(db)
	ctx := context.Background()

	root, err := s.CreateComment(ctx, CreateCommentParams{
		TicketID: ticketID, AuthorType: "member", AuthorID: acctID, Content: "hello", Kind: "comment",
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if root.SourceTaskID != nil {
		t.Error("source_task_id should be hidden for a member-authored comment")
	}

	reply, err := s.CreateComment(ctx, CreateCommentParams{
		TicketID: ticketID, AuthorType: "member", AuthorID: acctID, Content: "a reply", Kind: "comment",
		ParentID: &root.ID,
	})
	if err != nil {
		t.Fatalf("CreateComment (reply): %v", err)
	}

	list, err := s.ListAll(ctx, ticketID, nil)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListAll: got %d comments, err %v", len(list), err)
	}

	rootID, err := s.RootOf(ctx, reply.ID)
	if err != nil || rootID != root.ID {
		t.Fatalf("RootOf(reply) = %q, err %v, want %q", rootID, err, root.ID)
	}

	roots, err := s.ListRoots(ctx, ticketID)
	if err != nil || len(roots) != 1 || roots[0].ReplyCount == nil || *roots[0].ReplyCount != 1 {
		t.Fatalf("ListRoots: %+v, err %v", roots, err)
	}

	thread, err := s.ListThread(ctx, root.ID, 0)
	if err != nil || len(thread) != 2 {
		t.Fatalf("ListThread: got %d, err %v", len(thread), err)
	}

	updated, err := s.UpdateCommentContent(ctx, root.ID, "hello (edited)")
	if err != nil || updated.Content != "hello (edited)" {
		t.Fatalf("UpdateCommentContent: %+v, err %v", updated, err)
	}

	resolved, unresolvedPrev, err := s.ResolveThread(ctx, root.ID, reply.ID, "member", acctID)
	if err != nil {
		t.Fatalf("ResolveThread: %v", err)
	}
	if resolved.ResolvedAt == nil {
		t.Error("resolved comment should have ResolvedAt set")
	}
	if unresolvedPrev != nil {
		t.Errorf("no prior resolution existed, unresolvedPrev should be nil, got %v", *unresolvedPrev)
	}
	// Резолюция другого комментария того же треда должна снять первую.
	secondResolve, prev, err := s.ResolveThread(ctx, root.ID, root.ID, "member", acctID)
	if err != nil {
		t.Fatalf("ResolveThread (second): %v", err)
	}
	if prev == nil || *prev != reply.ID {
		t.Fatalf("expected previous resolution %q to be unresolved, got %v", reply.ID, prev)
	}
	if secondResolve.ResolvedAt == nil {
		t.Error("second resolved comment should have ResolvedAt set")
	}

	_, wasResolved, err := s.UnresolveComment(ctx, root.ID)
	if err != nil || !wasResolved {
		t.Fatalf("UnresolveComment: wasResolved=%v, err %v", wasResolved, err)
	}
	_, wasResolved, err = s.UnresolveComment(ctx, root.ID)
	if err != nil || wasResolved {
		t.Fatalf("UnresolveComment (already unresolved): wasResolved=%v, err %v", wasResolved, err)
	}

	re, err := s.AddCommentReaction(ctx, root.ID, "member", acctID, "👍")
	if err != nil || re.Emoji != "👍" {
		t.Fatalf("AddCommentReaction: %+v, err %v", re, err)
	}
	reactions, err := s.ListCommentReactions(ctx, root.ID)
	if err != nil || len(reactions) != 1 {
		t.Fatalf("ListCommentReactions: got %d, err %v", len(reactions), err)
	}
	if err := s.RemoveCommentReaction(ctx, root.ID, "member", acctID, "👍"); err != nil {
		t.Fatalf("RemoveCommentReaction: %v", err)
	}
	reactions, err = s.ListCommentReactions(ctx, root.ID)
	if err != nil || len(reactions) != 0 {
		t.Fatalf("ListCommentReactions after remove: got %d, err %v", len(reactions), err)
	}

	if err := s.DeleteComment(ctx, reply.ID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	if _, err := s.GetComment(ctx, reply.ID); err != ErrNotFound {
		t.Errorf("GetComment after delete: got %v, want ErrNotFound", err)
	}
}

func TestIssueReactions(t *testing.T) {
	db := newTestDB(t)
	wsID, acctID := seedWorkspace(t, db)
	ticketID := seedTicket(t, db, wsID, acctID)
	s := NewStore(db)
	ctx := context.Background()

	re, err := s.AddIssueReaction(ctx, ticketID, "member", acctID, "🎉")
	if err != nil || re.Emoji != "🎉" {
		t.Fatalf("AddIssueReaction: %+v, err %v", re, err)
	}
	// Тот же (actor, emoji) второй раз — не ошибка (ON CONFLICT DO UPDATE),
	// не дублирует строку (contract §1.11: ключ уникальности — комбинация
	// объект+автор+эмодзи).
	if _, err := s.AddIssueReaction(ctx, ticketID, "member", acctID, "🎉"); err != nil {
		t.Fatalf("AddIssueReaction (repeat): %v", err)
	}
	if err := s.RemoveIssueReaction(ctx, ticketID, "member", acctID, "🎉"); err != nil {
		t.Fatalf("RemoveIssueReaction: %v", err)
	}
}

func TestGetTicketInfoNotFound(t *testing.T) {
	db := newTestDB(t)
	wsID, _ := seedWorkspace(t, db)
	s := NewStore(db)
	if _, err := s.GetTicketInfo(context.Background(), wsID, "00000000-0000-0000-0000-000000000000"); err != ErrNotFound {
		t.Errorf("GetTicketInfo for missing ticket: got %v, want ErrNotFound", err)
	}
}
