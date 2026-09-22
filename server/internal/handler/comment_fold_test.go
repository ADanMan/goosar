package handler

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func setCommentResolvedAt(t *testing.T, id string, at time.Time) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE comment SET resolved_at = $2, resolved_by_type = 'member', resolved_by_id = $3 WHERE id = $1`,
		id, at, testUserID,
	); err != nil {
		t.Fatalf("set resolved_at for %s: %v", id, err)
	}
}

func byID(rows []CommentResponse) map[string]CommentResponse {
	m := make(map[string]CommentResponse, len(rows))
	for _, r := range rows {
		m[r.ID] = r
	}
	return m
}

func assertFolded(t *testing.T, c CommentResponse, wantCount int, ctx string) {
	t.Helper()
	if c.ThreadResolved == nil || !*c.ThreadResolved {
		t.Fatalf("%s: expected thread_resolved=true on %s, got %v", ctx, c.ID, c.ThreadResolved)
	}
	if c.FoldedCount == nil {
		t.Fatalf("%s: expected folded_count on %s, got nil", ctx, c.ID)
	}
	if *c.FoldedCount != wantCount {
		t.Fatalf("%s: folded_count on %s got %d want %d", ctx, c.ID, *c.FoldedCount, wantCount)
	}
}

func assertNotFolded(t *testing.T, c CommentResponse, ctx string) {
	t.Helper()
	if c.ThreadResolved != nil {
		t.Fatalf("%s: expected no thread_resolved on %s, got %v", ctx, c.ID, *c.ThreadResolved)
	}
	if c.FoldedCount != nil {
		t.Fatalf("%s: expected no folded_count on %s, got %v", ctx, c.ID, *c.FoldedCount)
	}
}

func TestListComments_FoldReplyResolvedKeepsRootAndConclusion(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.R1b, fx.Base.Add(20*time.Minute))

	_, rows := listComments(t, fx.IssueID, "fold=true")

	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.Root2, fx.R2a, fx.R2b}, "reply-resolved fold")

	m := byID(rows)
	assertFolded(t, m[fx.Root1], 2, "reply-resolved")
	assertNotFolded(t, m[fx.R1b], "conclusion reply is not annotated")
	assertNotFolded(t, m[fx.Root2], "unresolved thread root")
	assertNotFolded(t, m[fx.R2a], "unresolved reply")
}

func TestListComments_FoldRootResolvedKeepsRootOnly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.Root2, fx.Base.Add(20*time.Minute))

	_, rows := listComments(t, fx.IssueID, "fold=true")
	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1, fx.Root2}, "root-resolved fold")

	m := byID(rows)
	assertFolded(t, m[fx.Root2], 2, "root-resolved")
	for _, id := range []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1} {
		assertNotFolded(t, m[id], "unresolved thread1 comment")
	}
}

func TestListComments_FoldNoOpWhenNothingResolved(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	_, rows := listComments(t, fx.IssueID, "fold=true")
	eqIDs(t, ids(rows),
		[]string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1, fx.Root2, fx.R2a, fx.R2b},
		"fold no-op")
	for _, r := range rows {
		assertNotFolded(t, r, "no resolutions present")
	}
}

func TestListComments_FoldLatestReplyWins(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.R1a, fx.Base.Add(5*time.Minute))
	setCommentResolvedAt(t, fx.R1b, fx.Base.Add(20*time.Minute))

	_, rows := listComments(t, fx.IssueID, "fold=true")
	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.Root2, fx.R2a, fx.R2b}, "latest reply wins")
	assertFolded(t, byID(rows)[fx.Root1], 2, "latest-wins fold")
}

func TestListComments_FoldComposesWithRecent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.R1b, fx.Base.Add(20*time.Minute))

	v := url.Values{}
	v.Set("fold", "true")
	v.Set("recent", "10")
	_, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.Root2, fx.R2a, fx.R2b}, "fold + recent")
	assertFolded(t, byID(rows)[fx.Root1], 2, "fold + recent")
}

func TestListComments_FoldComposesWithSummary(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.R1b, fx.Base.Add(20*time.Minute))

	v := url.Values{}
	v.Set("fold", "true")
	v.Set("summary", "true")
	_, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.Root2, fx.R2a, fx.R2b}, "fold + summary")

	root1 := byID(rows)[fx.Root1]
	assertFolded(t, root1, 2, "fold + summary")
	if root1.ContentTruncated == nil {
		t.Fatalf("fold + summary: expected content_truncated marker on root1, got nil")
	}
}

func TestListComments_FoldRejectsPartialThreadModes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	since := fx.Base.Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	for _, tc := range []struct {
		name  string
		query func() string
	}{
		{name: "fold + since", query: func() string {
			v := url.Values{}
			v.Set("fold", "true")
			v.Set("since", since)
			return v.Encode()
		}},
		{name: "fold + tail", query: func() string {
			v := url.Values{}
			v.Set("fold", "true")
			v.Set("thread", fx.Root1)
			v.Set("tail", "2")
			return v.Encode()
		}},
		{name: "fold + roots_only", query: func() string {
			v := url.Values{}
			v.Set("fold", "true")
			v.Set("roots_only", "true")
			return v.Encode()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := listComments(t, fx.IssueID, tc.query())
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400, got %d: %s", tc.name, w.Code, w.Body.String())
			}
		})
	}
}

func TestListComments_FoldComposesWithThread(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.R1b, fx.Base.Add(20*time.Minute))

	v := url.Values{}
	v.Set("fold", "true")
	v.Set("thread", fx.R1a)
	_, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b}, "fold + thread")
	assertFolded(t, byID(rows)[fx.Root1], 2, "fold + thread")
}
