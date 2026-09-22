package ghsnapshot

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func testDBPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		if os.Getenv("GOOSAR_REQUIRE_TEST_DB") != "" || os.Getenv("CI") != "" {
			t.Fatalf("could not connect to database (GOOSAR_REQUIRE_TEST_DB/CI set, refusing to skip): %v", err)
		}
		t.Skipf("skipping DB test: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		if os.Getenv("GOOSAR_REQUIRE_TEST_DB") != "" || os.Getenv("CI") != "" {
			t.Fatalf("database not reachable (GOOSAR_REQUIRE_TEST_DB/CI set, refusing to skip): %v", err)
		}
		t.Skipf("skipping DB test: database not reachable: %v", err)
	}
	return pool
}

func seedWorkspace(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	slug := "ghsnap-" + randHex(t)
	var wsID pgtype.UUID
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1,$2,$3,$4) RETURNING id`,
		"ghsnap test", slug, "ghsnap test workspace", "GHS").Scan(&wsID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, wsID)
	})
	return wsID
}

func randHex(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 16)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}

func uniqueInstallationID(t *testing.T) int64 {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	v := int64(binary.BigEndian.Uint64(b[:]) &^ (1 << 63))
	if v == 0 {
		v = 1
	}
	return v
}

func seedPR(t *testing.T, pool *pgxpool.Pool, q *db.Queries, headSHA string) db.GithubPullRequest {
	return seedPRAt(t, pool, q, 987654, "r", 4242, headSHA)
}

func seedPRAt(t *testing.T, pool *pgxpool.Pool, q *db.Queries, installationID int64, repoName string, prNumber int32, headSHA string) db.GithubPullRequest {
	t.Helper()
	ts := pgtype.Timestamptz{Time: time.Unix(1_700_000_000, 0), Valid: true}

	if _, err := pool.Exec(context.Background(),
		`DELETE FROM github_pull_request WHERE installation_id=$1 AND repo_owner='o' AND repo_name=$2 AND pr_number=$3`,
		installationID, repoName, prNumber); err != nil {
		t.Fatalf("pre-clean natural key: %v", err)
	}
	pr, err := q.UpsertGitHubPullRequest(context.Background(), db.UpsertGitHubPullRequestParams{
		WorkspaceID:    seedWorkspace(t, pool),
		InstallationID: installationID,
		RepoOwner:      "o",
		RepoName:       repoName,
		PrNumber:       prNumber,
		Title:          "t",
		State:          "open",
		HtmlUrl:        "http://x",
		HeadSha:        headSHA,
		PrCreatedAt:    ts,
		PrUpdatedAt:    ts,
	})
	if err != nil {
		t.Fatalf("seed PR: %v", err)
	}
	return pr
}

func checkRunCount(t *testing.T, pool *pgxpool.Pool, prID pgtype.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM github_pull_request_check_run WHERE pr_id=$1`, prID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestListStaleUndecidedGitHubPRsExcludesDecidedAndRotatesCursor(t *testing.T) {
	pool := testDBPool(t)
	t.Cleanup(pool.Close)
	q := db.New(pool)
	ctx := context.Background()
	now := time.Unix(1_700_010_000, 0)

	instA := uniqueInstallationID(t)
	instB := uniqueInstallationID(t)
	if instA >= instB {

		instA, instB = instB, instA+1
	}

	settled := seedPRAt(t, pool, q, instA, "settled", 1, "S")
	oldest := seedPRAt(t, pool, q, instA, "oldest", 2, "O")
	running := seedPRAt(t, pool, q, instB, "running", 3, "R")
	newer := seedPRAt(t, pool, q, instB, "newer", 4, "N")
	prs := []db.GithubPullRequest{settled, oldest, running, newer}
	t.Cleanup(func() {
		for _, pr := range prs {
			_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request_check_run WHERE pr_id=$1`, pr.ID)
			_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id=$1`, pr.ID)
		}
	})

	setSnapshot := func(pr db.GithubPullRequest, fetchedAt time.Time, mergeable, rollup string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			UPDATE github_pull_request
			SET snapshot_head_sha=head_sha, snapshot_fetched_at=$2,
			    api_mergeable=$3, checks_rollup_state=$4
			WHERE id=$1`,
			pr.ID, fetchedAt, mergeable, rollup); err != nil {
			t.Fatal(err)
		}
	}
	setSnapshot(settled, now.Add(-time.Hour), "MERGEABLE", "SUCCESS")
	setSnapshot(oldest, now.Add(-40*time.Minute), "UNKNOWN", "PENDING")
	setSnapshot(running, now.Add(-30*time.Minute), "MERGEABLE", "SUCCESS")
	setSnapshot(newer, now.Add(-20*time.Minute), "MERGEABLE", "PENDING")
	if _, err := pool.Exec(ctx, `
		INSERT INTO github_pull_request_check_run
		    (pr_id, head_sha, ordinal, name, status, is_status_context)
		VALUES ($1, 'R', 0, 'backend', 'in_progress', false)`,
		running.ID); err != nil {
		t.Fatal(err)
	}

	own := func(row db.ListStaleUndecidedGitHubPRsRow) bool {
		return row.InstallationID == instA || row.InstallationID == instB
	}
	ownRows := func(rows []db.ListStaleUndecidedGitHubPRsRow) []db.ListStaleUndecidedGitHubPRsRow {
		out := make([]db.ListStaleUndecidedGitHubPRsRow, 0, len(rows))
		for _, row := range rows {
			if own(row) {
				out = append(out, row)
			}
		}
		return out
	}

	rows, err := q.ListStaleUndecidedGitHubPRs(ctx, db.ListStaleUndecidedGitHubPRsParams{
		OlderThan:           tsFromTime(now.Add(-10 * time.Minute)),
		AfterInstallationID: 0,
		AfterRepoOwner:      "",
		AfterRepoName:       "",
		AfterPrNumber:       0,
		MaxRows:             1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	sweep := ownRows(rows)
	gotRepos := make(map[string]bool, len(sweep))
	for _, row := range sweep {
		gotRepos[row.RepoName] = true
	}
	if gotRepos["settled"] {
		t.Fatal("decided snapshot remained in the periodic TTL sweep")
	}
	wantOrder := []string{"oldest", "newer", "running"}
	for _, repo := range wantOrder {
		if !gotRepos[repo] {
			t.Fatalf("undecided repo %q missing from TTL sweep: %+v", repo, sweep)
		}
	}
	if len(sweep) != len(wantOrder) {
		t.Fatalf("own rows in TTL sweep = %+v, want exactly %v", sweep, wantOrder)
	}
	for i, repo := range wantOrder {
		if sweep[i].RepoName != repo {
			t.Fatalf("own rows in TTL sweep = %+v, want order %v (installation/owner/repo/pr tuple order)", sweep, wantOrder)
		}
	}

	bounded, err := q.ListStaleUndecidedGitHubPRs(ctx, db.ListStaleUndecidedGitHubPRsParams{
		OlderThan: tsFromTime(now.Add(-10 * time.Minute)),
		MaxRows:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded) != 1 {
		t.Fatalf("bounded sweep returned %d rows, want 1", len(bounded))
	}

	afterOldest, err := q.ListStaleUndecidedGitHubPRs(ctx, db.ListStaleUndecidedGitHubPRsParams{
		OlderThan:           tsFromTime(now.Add(-10 * time.Minute)),
		AfterInstallationID: oldest.InstallationID,
		AfterRepoOwner:      oldest.RepoOwner,
		AfterRepoName:       oldest.RepoName,
		AfterPrNumber:       oldest.PrNumber,
		MaxRows:             1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	nextOwn := ownRows(afterOldest)
	if len(nextOwn) == 0 || nextOwn[0].RepoName != "newer" {
		t.Fatalf("next own row after %q = %+v, want %q first", oldest.RepoName, nextOwn, "newer")
	}
}

func TestApplySnapshotHeadSHAGuard(t *testing.T) {
	pool := testDBPool(t)
	t.Cleanup(pool.Close)
	q := db.New(pool)
	ctx := context.Background()
	now := time.Unix(1_700_000_100, 0)

	pr := seedPR(t, pool, q, "B")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request_check_run WHERE pr_id=$1`, pr.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id=$1`, pr.ID)
	})

	m := &Manager{queries: q, pool: pool, now: func() time.Time { return now }}

	applied, err := m.applySnapshot(ctx, pr.ID, &PRSnapshot{HeadSHA: "A", Mergeable: "CONFLICTING", MergeStateStatus: "DIRTY"})
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("mismatched-head snapshot must be discarded")
	}
	got, _ := q.GetGitHubPullRequestByID(ctx, pr.ID)
	if got.SnapshotHeadSha != "" || got.ApiMergeable.Valid {
		t.Fatalf("discarded write leaked into row: %+v", got)
	}

	applied, err = m.applySnapshot(ctx, pr.ID, &PRSnapshot{
		HeadSHA: "B", Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN", HasChecks: true, RollupState: "FAILURE",
		Contexts: []CheckContext{
			{Name: "backend", Status: "completed", Conclusion: "failure"},
			{Name: "vercel", Status: "completed", Conclusion: "success", IsStatusContext: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("matching-head snapshot must apply")
	}
	got, _ = q.GetGitHubPullRequestByID(ctx, pr.ID)
	if got.SnapshotHeadSha != "B" || got.ApiMergeable.String != "MERGEABLE" || got.ChecksRollupState.String != "FAILURE" {
		t.Fatalf("snapshot not written: %+v", got)
	}
	if n := checkRunCount(t, pool, pr.ID); n != 2 {
		t.Fatalf("check runs = %d, want 2", n)
	}

	if _, err := pool.Exec(ctx, `UPDATE github_pull_request SET head_sha='C' WHERE id=$1`, pr.ID); err != nil {
		t.Fatal(err)
	}
	applied, err = m.applySnapshot(ctx, pr.ID, &PRSnapshot{HeadSHA: "B", Mergeable: "CONFLICTING", MergeStateStatus: "DIRTY"})
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("late response for the old head must be discarded once head advanced")
	}
	got, _ = q.GetGitHubPullRequestByID(ctx, pr.ID)
	if got.SnapshotHeadSha != "B" || got.ApiMergeable.String != "MERGEABLE" {
		t.Fatalf("stale late write corrupted the snapshot: %+v", got)
	}
	if n := checkRunCount(t, pool, pr.ID); n != 2 {
		t.Fatalf("check runs after stale late write = %d, want 2 (unchanged)", n)
	}
}

func TestInFlightOldHeadKeepsTrailingRefresh(t *testing.T) {
	pool := testDBPool(t)
	t.Cleanup(pool.Close)
	q := db.New(pool)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	pr := seedPR(t, pool, q, "A")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request_check_run WHERE pr_id=$1`, pr.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id=$1`, pr.ID)
	})

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondFetched := make(chan struct{})
	applied := make(chan struct{}, 1)
	fetchCalls := 0

	m := NewManager(enabledClient(t), q, pool, func(context.Context, pgtype.UUID) {
		select {
		case applied <- struct{}{}:
		default:
		}
	})
	m.concurrency = 2
	m.sweepInterval = time.Hour
	m.jitter = func() time.Duration { return 0 }
	m.fetch = func(context.Context, *Client, int64, string, string, int32) (*PRSnapshot, error) {
		fetchCalls++
		if fetchCalls == 1 {
			close(firstStarted)
			<-releaseFirst
			return &PRSnapshot{
				HeadSHA: "A", Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN",
				HasChecks: true, RollupState: "SUCCESS",
			}, nil
		}
		close(secondFetched)
		return &PRSnapshot{
			HeadSHA: "B", Mergeable: "CONFLICTING", MergeStateStatus: "DIRTY",
			HasChecks: true, RollupState: "FAILURE",
			Contexts: []CheckContext{{Name: "backend", Status: "completed", Conclusion: "failure"}},
		}, nil
	}

	m.Start(ctx)
	m.Enqueue(pr.InstallationID, pr.RepoOwner, pr.RepoName, pr.PrNumber)
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first head fetch did not start")
	}

	if _, err := pool.Exec(ctx, `UPDATE github_pull_request SET head_sha='B' WHERE id=$1`, pr.ID); err != nil {
		t.Fatal(err)
	}
	m.Enqueue(pr.InstallationID, pr.RepoOwner, pr.RepoName, pr.PrNumber)

	select {
	case <-secondFetched:
		t.Fatal("second fetch started concurrently; single-PR in-flight guard failed")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)

	select {
	case <-secondFetched:
	case <-time.After(2 * time.Second):
		t.Fatal("new-head trailing refresh was swallowed")
	}
	select {
	case <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("new-head snapshot was not applied")
	}

	got, err := q.GetGitHubPullRequestByID(context.Background(), pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotHeadSha != "B" || got.ApiMergeable.String != "CONFLICTING" {
		t.Fatalf("trailing refresh did not replace old snapshot: %+v", got)
	}
	if n := checkRunCount(t, pool, pr.ID); n != 1 {
		t.Fatalf("new-head check runs = %d, want 1", n)
	}
}

func TestApplySnapshotReplacesRuns(t *testing.T) {
	pool := testDBPool(t)
	t.Cleanup(pool.Close)
	q := db.New(pool)
	ctx := context.Background()
	now := time.Unix(1_700_000_200, 0)

	pr := seedPR(t, pool, q, "H")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request_check_run WHERE pr_id=$1`, pr.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id=$1`, pr.ID)
	})
	m := &Manager{queries: q, pool: pool, now: func() time.Time { return now }}

	three := &PRSnapshot{
		HeadSHA: "H", Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN", HasChecks: true, RollupState: "PENDING",
		Contexts: []CheckContext{{Name: "a", Status: "in_progress"}, {Name: "b", Status: "in_progress"}, {Name: "c", Status: "in_progress"}},
	}
	if _, err := m.applySnapshot(ctx, pr.ID, three); err != nil {
		t.Fatal(err)
	}
	if n := checkRunCount(t, pool, pr.ID); n != 3 {
		t.Fatalf("after first apply: %d runs, want 3", n)
	}
	one := &PRSnapshot{
		HeadSHA: "H", Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN", HasChecks: true, RollupState: "SUCCESS",
		Contexts: []CheckContext{{Name: "a", Status: "completed", Conclusion: "success"}},
	}
	if _, err := m.applySnapshot(ctx, pr.ID, one); err != nil {
		t.Fatal(err)
	}
	if n := checkRunCount(t, pool, pr.ID); n != 1 {
		t.Fatalf("after replace: %d runs, want 1 (old runs deleted)", n)
	}
}
