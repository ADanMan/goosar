package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func cursorQuery(recent int, before, beforeID string) string {
	v := url.Values{}
	if recent > 0 {
		v.Set("recent", strconv.Itoa(recent))
	}
	if before != "" {
		v.Set("before", before)
	}
	if beforeID != "" {
		v.Set("before_id", beforeID)
	}
	return v.Encode()
}

func nextThreadCursor(w *httptest.ResponseRecorder) (string, string) {
	return w.Header().Get("X-Goosar-Next-Before"), w.Header().Get("X-Goosar-Next-Before-Id")
}

type commentListFixture struct {
	IssueID string
	Root1   string
	R1a     string
	R1b     string
	R1b1    string
	Root2   string
	R2a     string
	R2b     string
	Base    time.Time
}

func newCommentListFixture(t *testing.T) commentListFixture {
	t.Helper()
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "comment list fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	base := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)

	insert := func(parent *string, offset time.Duration, body string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $6)
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, body, parent, base.Add(offset)).Scan(&id); err != nil {
			t.Fatalf("insert comment %q: %v", body, err)
		}
		return id
	}

	root1 := insert(nil, 0, "root1")
	r1a := insert(&root1, 1*time.Minute, "r1a")
	r1b := insert(&root1, 2*time.Minute, "r1b")
	r1b1 := insert(&r1b, 3*time.Minute, "r1b1")
	root2 := insert(nil, 10*time.Minute, "root2")
	r2a := insert(&root2, 11*time.Minute, "r2a")
	r2b := insert(&root2, 12*time.Minute, "r2b")

	return commentListFixture{
		IssueID: issueID,
		Root1:   root1, R1a: r1a, R1b: r1b, R1b1: r1b1,
		Root2: root2, R2a: r2a, R2b: r2b,
		Base: base,
	}
}

func decodeComments(t *testing.T, body []byte) []CommentResponse {
	t.Helper()
	var resp []CommentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode comments: %v", err)
	}
	return resp
}

func listComments(t *testing.T, issueID, query string) (*httptest.ResponseRecorder, []CommentResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	url := "/api/issues/" + issueID + "/comments"
	if query != "" {
		url += "?" + query
	}
	r := newRequest("GET", url, nil)
	r = withURLParam(r, "id", issueID)
	testHandler.ListComments(w, r)
	if w.Code != http.StatusOK {
		return w, nil
	}
	return w, decodeComments(t, w.Body.Bytes())
}

func ids(rows []CommentResponse) []string {
	out := make([]string, len(rows))
	for i, c := range rows {
		out[i] = c.ID
	}
	return out
}

func eqIDs(t *testing.T, got, want []string, ctx string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: ids len got=%d want=%d\ngot=%v\nwant=%v", ctx, len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: ids[%d] got=%s want=%s\ngot=%v\nwant=%v", ctx, i, got[i], want[i], got, want)
		}
	}
}

func TestListComments_DefaultPreservesChronologicalOrder(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	_, rows := listComments(t, fx.IssueID, "")
	want := []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1, fx.Root2, fx.R2a, fx.R2b}
	eqIDs(t, ids(rows), want, "default order")
}

func TestListComments_RootsOnlyReturnsTopLevelComments(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	for _, tc := range []struct {
		name  string
		query string
	}{
		{name: "underscore query", query: "roots_only=true"},
		{name: "hyphenated alias", query: "roots-only=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, rows := listComments(t, fx.IssueID, tc.query)
			eqIDs(t, ids(rows), []string{fx.Root1, fx.Root2}, tc.name)
			for _, row := range rows {
				if row.ParentID != nil {
					t.Fatalf("%s: expected root comment %s to have nil parent_id, got %q", tc.name, row.ID, *row.ParentID)
				}
			}
			nb, nbid := nextThreadCursor(w)
			if nb != "" || nbid != "" {
				t.Fatalf("%s: roots-only list should not emit cursor headers, got before=%q before_id=%q", tc.name, nb, nbid)
			}
		})
	}
}

func TestListComments_RootsOnlyWithSinceReturnsNewTopLevelComments(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("roots_only", "true")
	v.Set("since", fx.Base.Add(5*time.Minute).UTC().Format(time.RFC3339Nano))
	_, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.Root2}, "roots_only + since")
	for _, row := range rows {
		if row.ParentID != nil {
			t.Fatalf("roots_only + since: expected root comment %s to have nil parent_id, got %q", row.ID, *row.ParentID)
		}
	}
}

func TestListComments_RootsOnlyReturnsThreadStats(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	_, rows := listComments(t, fx.IssueID, "roots_only=true")
	eqIDs(t, ids(rows), []string{fx.Root1, fx.Root2}, "roots stats order")

	byID := map[string]CommentResponse{}
	for _, r := range rows {
		byID[r.ID] = r
	}

	assertRootStat(t, byID[fx.Root1], "root1", 3, fx.Base.Add(3*time.Minute))

	assertRootStat(t, byID[fx.Root2], "root2", 2, fx.Base.Add(12*time.Minute))

	_, all := listComments(t, fx.IssueID, "")
	for _, r := range all {
		if r.ReplyCount != nil || r.LastActivityAt != nil {
			t.Fatalf("default list leaked roots-only stats on %s (reply_count=%v last_activity=%v)", r.ID, r.ReplyCount, r.LastActivityAt)
		}
	}
}

func assertRootStat(t *testing.T, c CommentResponse, label string, wantReplies int, wantLastActivity time.Time) {
	t.Helper()
	if c.ReplyCount == nil {
		t.Fatalf("%s: reply_count missing", label)
	}
	if *c.ReplyCount != wantReplies {
		t.Fatalf("%s: reply_count got=%d want=%d", label, *c.ReplyCount, wantReplies)
	}
	if c.LastActivityAt == nil {
		t.Fatalf("%s: last_activity_at missing", label)
	}
	got, err := time.Parse(time.RFC3339, *c.LastActivityAt)
	if err != nil {
		t.Fatalf("%s: last_activity_at parse %q: %v", label, *c.LastActivityAt, err)
	}
	if !got.Equal(wantLastActivity) {
		t.Fatalf("%s: last_activity_at got=%s want=%s", label, got.UTC(), wantLastActivity.UTC())
	}
}

func TestListComments_SummaryClipsContent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "summary fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	insert := func(body string, offset time.Duration) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5)
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, body, base.Add(offset)).Scan(&id); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
		return id
	}
	longID := insert(strings.Repeat("x", 500), 0)
	shortID := insert("short", time.Minute)

	cjkID := insert(strings.Repeat("你", 300), 2*time.Minute)

	_, full := listComments(t, issueID, "")
	for _, c := range full {
		if c.ContentTruncated != nil {
			t.Fatalf("baseline: content_truncated should be nil, got %v on %s", *c.ContentTruncated, c.ID)
		}
		if c.ID == longID && utf8.RuneCountInString(c.Content) != 500 {
			t.Fatalf("baseline: long content len got=%d want=500", utf8.RuneCountInString(c.Content))
		}
	}

	_, sum := listComments(t, issueID, "summary=true")
	byID := map[string]CommentResponse{}
	for _, c := range sum {
		byID[c.ID] = c
	}
	long := byID[longID]
	if long.ContentTruncated == nil || !*long.ContentTruncated {
		t.Fatalf("summary: long comment should be truncated, got %v", long.ContentTruncated)
	}
	if rc := utf8.RuneCountInString(long.Content); rc != summaryContentRunes+1 {
		t.Fatalf("summary: long content rune count got=%d want=%d", rc, summaryContentRunes+1)
	}
	if !strings.HasSuffix(long.Content, "…") {
		t.Fatalf("summary: long content should end with ellipsis, got %q", long.Content)
	}
	short := byID[shortID]
	if short.ContentTruncated == nil || *short.ContentTruncated {
		t.Fatalf("summary: short comment should be untruncated (false), got %v", short.ContentTruncated)
	}
	if short.Content != "short" {
		t.Fatalf("summary: short content got=%q want=short", short.Content)
	}
	cjk := byID[cjkID]
	if cjk.ContentTruncated == nil || !*cjk.ContentTruncated {
		t.Fatalf("summary: cjk comment should be truncated, got %v", cjk.ContentTruncated)
	}
	if !utf8.ValidString(cjk.Content) {
		t.Fatalf("summary: cjk content was clipped mid-rune (invalid UTF-8): %q", cjk.Content)
	}
	if rc := utf8.RuneCountInString(cjk.Content); rc != summaryContentRunes+1 {
		t.Fatalf("summary: cjk content rune count got=%d want=%d", rc, summaryContentRunes+1)
	}
	if !strings.HasSuffix(cjk.Content, "你…") {
		t.Fatalf("summary: cjk content should be whole 你-runes then ellipsis, got %q", cjk.Content)
	}
}

func TestListComments_RootsOnlySummaryComposes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "roots+summary fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	insert := func(parent *string, body string, offset time.Duration) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $6)
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, body, parent, base.Add(offset)).Scan(&id); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
		return id
	}
	rootID := insert(nil, strings.Repeat("x", 500), 0)
	insert(&rootID, "a reply", 5*time.Minute)

	_, rows := listComments(t, issueID, "roots_only=true&summary=true")
	eqIDs(t, ids(rows), []string{rootID}, "roots_only+summary returns only the root")

	root := rows[0]

	if root.ContentTruncated == nil || !*root.ContentTruncated {
		t.Fatalf("roots+summary: root should be truncated, got %v", root.ContentTruncated)
	}
	if rc := utf8.RuneCountInString(root.Content); rc != summaryContentRunes+1 {
		t.Fatalf("roots+summary: clipped content rune count got=%d want=%d", rc, summaryContentRunes+1)
	}

	assertRootStat(t, root, "root", 1, base.Add(5*time.Minute))
}

func TestListComments_ThreadResolvesFromAnyAnchor(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	wantThread1 := []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1}

	t.Run("anchor is root", func(t *testing.T) {
		_, rows := listComments(t, fx.IssueID, "thread="+fx.Root1)
		eqIDs(t, ids(rows), wantThread1, "anchor=root1")
	})

	t.Run("anchor is direct reply", func(t *testing.T) {
		_, rows := listComments(t, fx.IssueID, "thread="+fx.R1a)
		eqIDs(t, ids(rows), wantThread1, "anchor=r1a (direct reply)")
	})

	t.Run("anchor is nested reply", func(t *testing.T) {

		_, rows := listComments(t, fx.IssueID, "thread="+fx.R1b1)
		eqIDs(t, ids(rows), wantThread1, "anchor=r1b1 (nested reply)")
	})

	t.Run("anchor in other thread returns only that thread", func(t *testing.T) {
		_, rows := listComments(t, fx.IssueID, "thread="+fx.R2a)
		eqIDs(t, ids(rows), []string{fx.Root2, fx.R2a, fx.R2b}, "anchor=r2a")
	})
}

func TestListComments_ThreadAnchorErrors(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("non-uuid thread returns 400", func(t *testing.T) {
		w, _ := listComments(t, fx.IssueID, "thread=not-a-uuid")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unknown thread anchor returns 404", func(t *testing.T) {
		w, _ := listComments(t, fx.IssueID, "thread=00000000-0000-0000-0000-000000000001")
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestListComments_RecentReturnsMostRecentlyActiveThreads(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("recent=1 returns the freshest-active thread fully", func(t *testing.T) {
		_, rows := listComments(t, fx.IssueID, "recent=1")
		eqIDs(t, ids(rows), []string{fx.Root2, fx.R2a, fx.R2b}, "recent=1")
	})

	t.Run("recent=2 returns both threads, older-active first", func(t *testing.T) {
		_, rows := listComments(t, fx.IssueID, "recent=2")

		want := []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1, fx.Root2, fx.R2a, fx.R2b}
		eqIDs(t, ids(rows), want, "recent=2")
	})
}

func TestListComments_RecentRanksStaleThreadAheadIfRecentlyReplied(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3) RETURNING id
	`, testWorkspaceID, testUserID, "stale-but-fresh fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	base := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	insert := func(parent *string, offset time.Duration, body string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $6) RETURNING id
		`, issueID, testWorkspaceID, testUserID, body, parent, base.Add(offset)).Scan(&id); err != nil {
			t.Fatalf("insert: %v", err)
		}
		return id
	}

	oldRoot := insert(nil, 0, "oldRoot")
	quietRoot := insert(nil, 15*time.Minute, "quietRoot")
	freshReply := insert(&oldRoot, 30*time.Minute, "freshReply")

	_, rows := listComments(t, issueID, "recent=1")

	eqIDs(t, ids(rows), []string{oldRoot, freshReply}, "recent=1 picks freshly replied stale thread")
	_ = quietRoot
}

func TestListComments_RecentEmitsThreadCursorWhenPageFull(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("underfilled page emits no cursor", func(t *testing.T) {

		w, _ := listComments(t, fx.IssueID, "recent=5")
		nb, nbid := nextThreadCursor(w)
		if nb != "" || nbid != "" {
			t.Fatalf("expected no cursor, got before=%q before_id=%q", nb, nbid)
		}
	})

	t.Run("full page emits cursor pointing at oldest thread in page", func(t *testing.T) {

		w, _ := listComments(t, fx.IssueID, "recent=1")
		nb, nbid := nextThreadCursor(w)
		if nbid != fx.Root2 {
			t.Fatalf("cursor before_id = %q, want %q (root2 — newest thread)", nbid, fx.Root2)
		}
		if nb == "" {
			t.Fatalf("cursor before is empty; expected RFC3339Nano timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, nb); err != nil {
			t.Fatalf("cursor before = %q is not RFC3339Nano: %v", nb, err)
		}
	})
}

func TestListComments_RecentWithThreadCursorScrollsOlderThreads(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	w1, page1 := listComments(t, fx.IssueID, "recent=1")
	eqIDs(t, ids(page1), []string{fx.Root2, fx.R2a, fx.R2b}, "page1 = root2 thread")
	nb, nbid := nextThreadCursor(w1)
	if nb == "" || nbid != fx.Root2 {
		t.Fatalf("page1 cursor = (%q, %q), want (non-empty, %q)", nb, nbid, fx.Root2)
	}

	w2, page2 := listComments(t, fx.IssueID, cursorQuery(1, nb, nbid))
	eqIDs(t, ids(page2), []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1}, "page2 = root1 thread")

	nb2, nbid2 := nextThreadCursor(w2)
	if nb2 == "" || nbid2 != fx.Root1 {
		t.Fatalf("page2 cursor = (%q, %q), want (non-empty, %q)", nb2, nbid2, fx.Root1)
	}
	w3, page3 := listComments(t, fx.IssueID, cursorQuery(1, nb2, nbid2))
	if len(page3) != 0 {
		t.Fatalf("page3: expected empty (no older threads), got %d rows: %v", len(page3), ids(page3))
	}
	nb3, nbid3 := nextThreadCursor(w3)
	if nb3 != "" || nbid3 != "" {
		t.Fatalf("page3 cursor = (%q, %q), want both empty (end-of-list)", nb3, nbid3)
	}
}

func TestListComments_ThreadCursorStableUnderSameLastActivity(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3) RETURNING id
	`, testWorkspaceID, testUserID, "thread tie-break fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	ts := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Millisecond)
	insertRoot := func(body string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5) RETURNING id
		`, issueID, testWorkspaceID, testUserID, body, ts).Scan(&id); err != nil {
			t.Fatalf("insert: %v", err)
		}
		return id
	}
	a := insertRoot("a")
	b := insertRoot("b")
	c := insertRoot("c")

	sorted := []string{a, b, c}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	wantOrder := []string{sorted[2], sorted[1], sorted[0]}

	var got []string
	w, page := listComments(t, issueID, "recent=1")
	if len(page) != 1 {
		t.Fatalf("page1: expected 1 thread (1 row), got %d", len(page))
	}
	got = append(got, page[0].ID)

	for i := 0; i < 2; i++ {
		nb, nbid := nextThreadCursor(w)
		if nb == "" || nbid == "" {
			t.Fatalf("page %d: missing cursor headers", i+1)
		}
		w, page = listComments(t, issueID, cursorQuery(1, nb, nbid))
		if len(page) != 1 {
			t.Fatalf("page %d: expected 1 thread (1 row), got %d", i+2, len(page))
		}
		got = append(got, page[0].ID)
	}

	eqIDs(t, got, wantOrder, "paginated walk")
}

func TestListComments_FlagCombinationRules(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	cases := []struct {
		name   string
		query  string
		status int
	}{
		{
			name:   "thread + recent rejected",
			query:  "thread=" + fx.Root1 + "&recent=5",
			status: http.StatusBadRequest,
		},
		{
			name: "thread + before rejected",
			query: (func() string {
				v := url.Values{}
				v.Set("thread", fx.Root1)
				v.Set("before", time.Now().UTC().Format(time.RFC3339))
				v.Set("before_id", uuid.NewString())
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
		{
			name: "before without before_id rejected",
			query: (func() string {
				v := url.Values{}
				v.Set("recent", "5")
				v.Set("before", time.Now().UTC().Format(time.RFC3339))
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
		{
			name: "before_id without before rejected",
			query: (func() string {
				v := url.Values{}
				v.Set("recent", "5")
				v.Set("before_id", uuid.NewString())
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
		{
			name: "before + before_id without recent rejected",

			query: (func() string {
				v := url.Values{}
				v.Set("before", time.Now().UTC().Format(time.RFC3339))
				v.Set("before_id", uuid.NewString())
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
		{
			name:   "zero recent rejected",
			query:  "recent=0",
			status: http.StatusBadRequest,
		},
		{
			name:   "negative recent rejected",
			query:  "recent=-3",
			status: http.StatusBadRequest,
		},
		{
			name:   "non-numeric recent rejected",
			query:  "recent=lots",
			status: http.StatusBadRequest,
		},
		{
			name:   "non-boolean roots_only rejected",
			query:  "roots_only=yes",
			status: http.StatusBadRequest,
		},
		{
			name:   "non-boolean summary rejected",
			query:  "summary=yes",
			status: http.StatusBadRequest,
		},
		{
			name:   "roots_only + thread rejected",
			query:  "roots_only=true&thread=" + fx.Root1,
			status: http.StatusBadRequest,
		},
		{
			name:   "roots_only + recent rejected",
			query:  "roots_only=true&recent=1",
			status: http.StatusBadRequest,
		},
		{
			name:   "roots_only + tail rejected",
			query:  "roots_only=true&tail=1",
			status: http.StatusBadRequest,
		},
		{
			name: "roots_only + cursor rejected",
			query: (func() string {
				v := url.Values{}
				v.Set("roots_only", "true")
				v.Set("before", time.Now().UTC().Format(time.RFC3339))
				v.Set("before_id", uuid.NewString())
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := listComments(t, fx.IssueID, tc.query)
			if w.Code != tc.status {
				t.Fatalf("query=%q\n  got=%d want=%d body=%s", tc.query, w.Code, tc.status, w.Body.String())
			}
		})
	}
}

func TestListComments_RecentWithSinceFilteredEmptySuppressesCursor(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("recent", "1")
	v.Set("since", fx.Base.Add(1*time.Hour).UTC().Format(time.RFC3339Nano))
	w, rows := listComments(t, fx.IssueID, v.Encode())
	if len(rows) != 0 {
		t.Fatalf("expected empty page after since-filter, got %d rows: %v", len(rows), ids(rows))
	}
	nb, nbid := nextThreadCursor(w)
	if nb != "" || nbid != "" {
		t.Fatalf("recent+since empty page must NOT emit cursor; got before=%q before_id=%q (this walks the caller into a guaranteed-empty pagination loop)", nb, nbid)
	}
}

func TestListComments_RecentWithSinceKeepsCursorWhenPageHasRows(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("recent", "1")
	v.Set("since", fx.Base.Add(11*time.Minute+30*time.Second).UTC().Format(time.RFC3339Nano))
	w, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.R2b}, "recent=1 + since keeps r2b")
	nb, nbid := nextThreadCursor(w)
	if nb == "" || nbid != fx.Root2 {
		t.Fatalf("non-empty recent+since page must keep cursor; got before=%q before_id=%q want root_id=%q", nb, nbid, fx.Root2)
	}
}

func TestListComments_RecentWithSinceSuppressesCursorWhenHeadPastSince(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("recent", "2")
	v.Set("since", fx.Base.Add(5*time.Minute).UTC().Format(time.RFC3339Nano))
	w, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.Root2, fx.R2a, fx.R2b}, "recent=2 + since keeps root2 thread")
	nb, nbid := nextThreadCursor(w)
	if nb != "" || nbid != "" {
		t.Fatalf("head thread (root1, last_activity = base+3m) is already <= since (base+5m); older pages can't beat it. cursor must be suppressed, got before=%q before_id=%q", nb, nbid)
	}
}

func TestListComments_ThreadWithSinceFiltersWithinThread(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("thread", fx.Root1)
	v.Set("since", fx.Base.Add(90*time.Second).UTC().Format(time.RFC3339Nano))
	_, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.R1b, fx.R1b1}, "thread+since")
}

func nextReplyCursor(w *httptest.ResponseRecorder) (string, string) {
	return w.Header().Get("X-Goosar-Next-Before"), w.Header().Get("X-Goosar-Next-Before-Id")
}

func TestListComments_ThreadTailReturnsRootPlusNewestReplies(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("tail=2 keeps newest 2 replies + root", func(t *testing.T) {
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "2")

		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.R1b1}, "tail=2")
	})

	t.Run("tail larger than reply count returns full thread", func(t *testing.T) {
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "99")
		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1}, "tail=99 (oversized)")
	})

	t.Run("tail=0 returns root only", func(t *testing.T) {

		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "0")
		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1}, "tail=0 (root only)")
	})

	t.Run("anchor on a nested reply still walks up to the root", func(t *testing.T) {

		v := url.Values{}
		v.Set("thread", fx.R1b1)
		v.Set("tail", "1")
		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b1}, "tail=1 anchored at nested reply")
	})
}

func TestListComments_ThreadTailEmitsReplyCursorWhenPageFull(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("underfilled page emits no cursor", func(t *testing.T) {

		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "5")
		w, _ := listComments(t, fx.IssueID, v.Encode())
		nb, nbid := nextReplyCursor(w)
		if nb != "" || nbid != "" {
			t.Fatalf("expected no cursor, got before=%q before_id=%q", nb, nbid)
		}
	})

	t.Run("tail=0 emits no cursor (no replies were requested)", func(t *testing.T) {

		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "0")
		w, _ := listComments(t, fx.IssueID, v.Encode())
		nb, nbid := nextReplyCursor(w)
		if nb != "" || nbid != "" {
			t.Fatalf("tail=0 must not emit cursor, got before=%q before_id=%q", nb, nbid)
		}
	})

	t.Run("full page emits cursor pointing at oldest reply", func(t *testing.T) {

		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "2")
		w, _ := listComments(t, fx.IssueID, v.Encode())
		nb, nbid := nextReplyCursor(w)
		if nbid != fx.R1b {
			t.Fatalf("cursor before_id = %q, want %q (r1b — oldest reply on page)", nbid, fx.R1b)
		}
		if nb == "" {
			t.Fatalf("cursor before is empty; expected RFC3339Nano timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, nb); err != nil {
			t.Fatalf("cursor before = %q is not RFC3339Nano: %v", nb, err)
		}
	})

	t.Run("exact-boundary page emits no cursor", func(t *testing.T) {

		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "3")
		w, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1}, "tail==replyCount returns full thread")
		nb, nbid := nextReplyCursor(w)
		if nb != "" || nbid != "" {
			t.Fatalf("exact-boundary page must not emit cursor, got before=%q before_id=%q", nb, nbid)
		}
	})
}

func TestListComments_ThreadTailCursorScrollsOlderReplies(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("thread", fx.Root1)
	v.Set("tail", "1")
	w1, page1 := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(page1), []string{fx.Root1, fx.R1b1}, "page1 = root + r1b1")
	nb, nbid := nextReplyCursor(w1)
	if nb == "" || nbid != fx.R1b1 {
		t.Fatalf("page1 cursor = (%q, %q), want (non-empty, %q)", nb, nbid, fx.R1b1)
	}

	v.Set("before", nb)
	v.Set("before_id", nbid)
	w2, page2 := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(page2), []string{fx.Root1, fx.R1b}, "page2 = root + r1b")
	nb2, nbid2 := nextReplyCursor(w2)
	if nb2 == "" || nbid2 != fx.R1b {
		t.Fatalf("page2 cursor = (%q, %q), want (non-empty, %q)", nb2, nbid2, fx.R1b)
	}

	v.Set("before", nb2)
	v.Set("before_id", nbid2)
	w3, page3 := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(page3), []string{fx.Root1, fx.R1a}, "page3 = root + r1a (last reply)")
	nb3, nbid3 := nextReplyCursor(w3)
	if nb3 != "" || nbid3 != "" {
		t.Fatalf("page3 cursor = (%q, %q), want both empty (end-of-thread, no older replies after r1a)", nb3, nbid3)
	}
}

func TestListComments_ThreadTailWithSinceFiltersAfterTail(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("since drops older replies, keeps root + fresher", func(t *testing.T) {

		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "3")
		v.Set("since", fx.Base.Add(90*time.Second).UTC().Format(time.RFC3339Nano))
		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.R1b1}, "tail=3+since")
	})

	t.Run("since drops every reply but root stays", func(t *testing.T) {

		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "3")
		v.Set("since", fx.Base.Add(1*time.Hour).UTC().Format(time.RFC3339Nano))
		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1}, "since past everything keeps root")
	})

	t.Run("tail overflow with since past oldest retained reply suppresses cursor", func(t *testing.T) {

		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "2")
		v.Set("since", fx.Base.Add(150*time.Second).UTC().Format(time.RFC3339Nano))
		w, rows := listComments(t, fx.IssueID, v.Encode())

		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b1}, "body keeps root + fresher reply only")
		nb, nbid := nextReplyCursor(w)
		if nb != "" || nbid != "" {
			t.Fatalf("expected no cursor (older page is guaranteed-empty under since), got before=%q before_id=%q", nb, nbid)
		}
	})
}

func TestListComments_ThreadTailFlagCombinationRules(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	cases := []struct {
		name   string
		query  string
		status int
	}{
		{

			name:   "tail without thread rejected",
			query:  "tail=5",
			status: http.StatusBadRequest,
		},
		{

			name:   "negative tail rejected",
			query:  "thread=" + fx.Root1 + "&tail=-1",
			status: http.StatusBadRequest,
		},
		{
			name:   "non-numeric tail rejected",
			query:  "thread=" + fx.Root1 + "&tail=lots",
			status: http.StatusBadRequest,
		},
		{

			name: "thread + before without tail rejected",
			query: (func() string {
				v := url.Values{}
				v.Set("thread", fx.Root1)
				v.Set("before", time.Now().UTC().Format(time.RFC3339))
				v.Set("before_id", uuid.NewString())
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := listComments(t, fx.IssueID, tc.query)
			if w.Code != tc.status {
				t.Fatalf("query=%q\n  got=%d want=%d body=%s", tc.query, w.Code, tc.status, w.Body.String())
			}
		})
	}
}

func TestListComments_ThreadTailZeroReplyCountIsAllowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("thread", fx.Root1)
	v.Set("tail", "0")
	w, rows := listComments(t, fx.IssueID, v.Encode())
	if w.Code != http.StatusOK {
		t.Fatalf("tail=0 should succeed, got %d: %s", w.Code, w.Body.String())
	}
	eqIDs(t, ids(rows), []string{fx.Root1}, "tail=0 returns only root")
}

func TestListComments_ThreadTailNotFoundReturns404(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("thread", "00000000-0000-0000-0000-000000000001")
	v.Set("tail", "5")
	w, _ := listComments(t, fx.IssueID, v.Encode())
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown anchor, got %d: %s", w.Code, w.Body.String())
	}
	_ = fx
}

func resolveCommentRow(t *testing.T, commentID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE comment SET resolved_at = now(), resolved_by_type = 'member', resolved_by_id = $2 WHERE id = $1`,
		commentID, testUserID,
	); err != nil {
		t.Fatalf("resolve comment row: %v", err)
	}
}

func TestCountNewCommentsSince_IssueWideExcludesAgentOwnAndTrigger(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	agentID := createHandlerTestAgent(t, "count-agent", []byte("[]"))

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, created_at)
		VALUES ($1, $2, 'agent', $3, 'agent reply', 'comment', $4, $5)
	`, fx.IssueID, testWorkspaceID, agentID, fx.Root1, fx.Base.Add(4*time.Minute)); err != nil {
		t.Fatalf("insert agent comment: %v", err)
	}

	ctx := context.Background()

	got, err := testHandler.Queries.CountNewCommentsSince(ctx, db.CountNewCommentsSinceParams{
		AnchorID:    parseUUID(fx.R1b1),
		IssueID:     parseUUID(fx.IssueID),
		WorkspaceID: parseUUID(testWorkspaceID),
		Since:       pgtype.Timestamptz{Time: fx.Base.Add(90 * time.Second), Valid: true},
		AuthorID:    parseUUID(agentID),
	})
	if err != nil {
		t.Fatalf("count new since anchor: %v", err)
	}
	if got != 4 {
		t.Fatalf("expected issue-wide count of 4 (r1b + root2 + r2a + r2b), got %d", got)
	}

	got, err = testHandler.Queries.CountNewCommentsSince(ctx, db.CountNewCommentsSinceParams{
		AnchorID:    parseUUID(fx.R1b1),
		IssueID:     parseUUID(fx.IssueID),
		WorkspaceID: parseUUID(testWorkspaceID),
		Since:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		AuthorID:    parseUUID(agentID),
	})
	if err != nil {
		t.Fatalf("count new since future: %v", err)
	}
	if got != 0 {
		t.Fatalf("expected 0 new comments after a future anchor, got %d", got)
	}
}

func TestCreateCommentPreservesDirectParent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires DB")
	}
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "direct parent fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	create := func(parentID, body string) CommentResponse {
		t.Helper()
		payload := map[string]any{"content": body}
		if parentID != "" {
			payload["parent_id"] = parentID
		}
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/comments", payload)
		req = withURLParam(req, "id", issueID)
		testHandler.CreateComment(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(%q): expected 201, got %d: %s", body, w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode created comment %q: %v", body, err)
		}
		return resp
	}

	root := create("", "root")
	if root.ParentID != nil {
		t.Fatalf("root comment must have nil parent_id, got %v", *root.ParentID)
	}

	reply := create(root.ID, "reply")
	if reply.ParentID == nil || *reply.ParentID != root.ID {
		t.Fatalf("direct reply parent_id: want root %s, got %v", root.ID, reply.ParentID)
	}

	nested := create(reply.ID, "nested")
	if nested.ParentID == nil || *nested.ParentID != reply.ID {
		t.Fatalf("reply-to-reply parent_id: want direct parent %s, got %v (root was %s)", reply.ID, nested.ParentID, root.ID)
	}
}
