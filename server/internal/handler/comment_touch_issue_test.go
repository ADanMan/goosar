package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestCreateComment_BumpsIssueUpdatedAt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	issueID := createCommentTriggerPreviewIssue(t, "comment bumps updated_at", "", "")

	var before time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at FROM issue WHERE id = $1`, issueID).Scan(&before); err != nil {
		t.Fatalf("read updated_at before: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	w := httptest.NewRecorder()
	r := withURLParam(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": "a fresh comment"}),
		"id", issueID,
	)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var after time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at FROM issue WHERE id = $1`, issueID).Scan(&after); err != nil {
		t.Fatalf("read updated_at after: %v", err)
	}

	if !after.After(before) {
		t.Fatalf("issue updated_at was not bumped by a new comment: before=%s after=%s", before, after)
	}
}

func TestCreateComment_WorkspaceMismatchPersistsNothing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	issueID := createCommentTriggerPreviewIssue(t, "comment workspace guard", "", "")

	var before time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at FROM issue WHERE id = $1`, issueID).Scan(&before); err != nil {
		t.Fatalf("read updated_at before: %v", err)
	}

	wrongWorkspace := parseUUID("11111111-1111-1111-1111-111111111111")
	_, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     parseUUID(issueID),
		WorkspaceID: wrongWorkspace,
		AuthorType:  "member",
		AuthorID:    parseUUID(testUserID),
		Content:     "should never persist",
		Type:        "comment",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows on workspace mismatch, got %v", err)
	}

	var commentCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&commentCount); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	if commentCount != 0 {
		t.Fatalf("workspace mismatch must persist no comment, found %d", commentCount)
	}

	var after time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at FROM issue WHERE id = $1`, issueID).Scan(&after); err != nil {
		t.Fatalf("read updated_at after: %v", err)
	}
	if !after.Equal(before) {
		t.Fatalf("workspace mismatch must not bump updated_at: before=%s after=%s", before, after)
	}
}
