package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/leader"
	"github.com/adanman/goosar/server/internal/retention"
	"github.com/adanman/goosar/server/internal/uploadgc"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestCronExecutionRetentionPurge(t *testing.T) {
	queries := db.New(testPool)
	ctx := context.Background()

	clean := func() {
		testPool.Exec(context.Background(), `DELETE FROM sys_cron_executions WHERE job_name = 'gc_retention_test'`)
	}
	clean()
	t.Cleanup(clean)

	insert := func(planAgo time.Duration, status string) {
		t.Helper()
		finished := "now() - $2::interval"
		if status == "RUNNING" {
			finished = "NULL"
		}
		_, err := testPool.Exec(ctx, `
			INSERT INTO sys_cron_executions (
				job_name, scope_kind, scope_id, plan_time,
				status, attempt, max_attempts, runner_id,
				heartbeat_at, stale_after, started_at, finished_at, updated_at
			) VALUES (
				'gc_retention_test', 'global', $1, now() - $2::interval,
				$3, 1, 3, 'test-runner',
				now(), now() + interval '1 minute', now(), `+finished+`, now()
			)`, status+"-"+planAgo.String(), planAgo.String(), status)
		if err != nil {
			t.Fatalf("insert %s/%s: %v", planAgo, status, err)
		}
	}

	insert(90*24*time.Hour, "SUCCESS")
	insert(60*24*time.Hour, "FAILED")
	insert(90*24*time.Hour, "RUNNING")
	insert(1*time.Hour, "SUCCESS")

	deleted, err := queries.PurgeCronExecutions(ctx, db.PurgeCronExecutionsParams{
		RetentionSecs: (30 * 24 * time.Hour).Seconds(),
		MaxRows:       cronPurgeBatchSize,
	})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 purged rows, got %d", deleted)
	}

	var remaining []string
	rows, err := testPool.Query(ctx, `SELECT status FROM sys_cron_executions WHERE job_name = 'gc_retention_test' ORDER BY status`)
	if err != nil {
		t.Fatalf("select remaining: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		remaining = append(remaining, s)
	}
	if len(remaining) != 2 || remaining[0] != "RUNNING" || remaining[1] != "SUCCESS" {
		t.Fatalf("expected the RUNNING lease and the in-window SUCCESS to survive, got %v", remaining)
	}
}

type attachmentGCFixture struct {
	boundIDs  []string
	boundURLs []string
	orphanID  string
	orphanURL string
	issueID   string
}

func containsURL(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

func newAttachmentGCFixture(t *testing.T) *attachmentGCFixture {
	t.Helper()
	ctx := context.Background()

	var agentID, userID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, m.user_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		WHERE a.workspace_id = $1
		LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &userID); err != nil {
		t.Fatalf("find test agent/member: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, number, title, creator_type, creator_id)
		VALUES ($1, (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1),
		        'Upload GC fixture', 'member', $2)
		RETURNING id`, testWorkspaceID, userID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'fixture')
		RETURNING id`, testWorkspaceID, issueID, userID).Scan(&commentID); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	var sessionID, messageID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'Upload GC fixture')
		RETURNING id`, testWorkspaceID, agentID, userID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'fixture')
		RETURNING id`, sessionID).Scan(&messageID); err != nil {
		t.Fatalf("create chat message: %v", err)
	}

	fx := &attachmentGCFixture{}
	insert := func(url string, ownerCol string, ownerID any) string {
		t.Helper()
		var id string
		var err error
		if ownerCol == "" {
			err = testPool.QueryRow(ctx, `
				INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
				VALUES ($1, 'member', $2, 'f.png', $3, 'image/png', 10)
				RETURNING id`, testWorkspaceID, userID, url).Scan(&id)
		} else {
			err = testPool.QueryRow(ctx, `
				INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes, `+ownerCol+`)
				VALUES ($1, 'member', $2, 'f.png', $3, 'image/png', 10, $4)
				RETURNING id`, testWorkspaceID, userID, url, ownerID).Scan(&id)
		}
		if err != nil {
			t.Fatalf("create attachment (%s): %v", ownerCol, err)
		}
		return id
	}

	suffix := issueID
	fx.boundURLs = []string{
		"https://cdn.test/bound-issue-" + suffix,
		"https://cdn.test/bound-comment-" + suffix,
		"https://cdn.test/bound-chat-" + suffix,
	}
	fx.boundIDs = []string{
		insert(fx.boundURLs[0], "issue_id", issueID),
		insert(fx.boundURLs[1], "comment_id", commentID),
		insert(fx.boundURLs[2], "chat_message_id", messageID),
	}
	fx.orphanURL = "https://cdn.test/orphan-" + suffix
	fx.orphanID = insert(fx.orphanURL, "", nil)
	fx.issueID = issueID

	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM attachment WHERE url = ANY($1)`, append([]string{fx.orphanURL}, fx.boundURLs...))
		testPool.Exec(bg, `DELETE FROM chat_session WHERE id = $1`, sessionID)
		testPool.Exec(bg, `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return fx
}

func (fx *attachmentGCFixture) age(t *testing.T) {
	t.Helper()
	urls := append([]string{fx.orphanURL}, fx.boundURLs...)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE attachment SET created_at = now() - interval '48 hours' WHERE url = ANY($1)`,
		urls); err != nil {
		t.Fatalf("age attachments: %v", err)
	}
}

type fakeGCStorage struct {
	deleted []string
}

func (f *fakeGCStorage) KeyFromURL(rawURL string) string { return "key:" + rawURL }
func (f *fakeGCStorage) DeleteKeys(_ context.Context, keys []string) {
	f.deleted = append(f.deleted, keys...)
}

func TestOrphanUploadSweepSparesBoundAttachments(t *testing.T) {
	ctx := context.Background()
	fx := newAttachmentGCFixture(t)
	fx.age(t)

	store := &fakeGCStorage{}
	res, err := uploadgc.Sweep(ctx, db.New(testPool), store, uploadgc.Options{Grace: 24 * time.Hour})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if !containsURL(res.URLs, fx.orphanURL) {
		t.Fatalf("the unbound attachment was not reclaimed, swept urls=%v", res.URLs)
	}
	if !containsURL(store.deleted, "key:"+fx.orphanURL) {
		t.Fatalf("the orphan object was not deleted from storage, got %v", store.deleted)
	}
	for _, bound := range fx.boundURLs {
		if containsURL(res.URLs, bound) {
			t.Fatalf("bound attachment %s was selected by the orphan sweep", bound)
		}
		if containsURL(store.deleted, "key:"+bound) {
			t.Fatalf("bound attachment object %s was deleted from storage", bound)
		}
	}

	for _, id := range fx.boundIDs {
		var n int
		if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM attachment WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatalf("count bound %s: %v", id, err)
		}
		if n != 1 {
			t.Fatalf("bound attachment %s was deleted by the orphan sweep", id)
		}
	}
}

func TestOrphanUploadSweepSparesReferencedUploads(t *testing.T) {
	ctx := context.Background()
	fx := newAttachmentGCFixture(t)

	var agentID, userID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, m.user_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		WHERE a.workspace_id = $1
		LIMIT 1`, testWorkspaceID).Scan(&agentID, &userID); err != nil {
		t.Fatalf("find test agent/member: %v", err)
	}

	kinds := []string{"user-avatar", "agent-avatar", "squad-avatar", "workspace-avatar", "draft-restore", "feedback"}
	urls := map[string]string{}
	ids := map[string]string{}
	for _, kind := range kinds {
		url := "https://cdn.test/referenced-" + kind + "-" + fx.orphanID
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes, created_at)
			VALUES ($1, 'member', $2, 'f.png', $3, 'image/png', 10, now() - interval '48 hours')
			RETURNING id`, testWorkspaceID, userID, url).Scan(&id); err != nil {
			t.Fatalf("create %s attachment: %v", kind, err)
		}
		urls[kind], ids[kind] = url, id
	}
	t.Cleanup(func() {
		bg := context.Background()
		for _, u := range urls {
			testPool.Exec(bg, `DELETE FROM attachment WHERE url = $1`, u)
		}
	})

	var avatarUser string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email, avatar_url) VALUES ('GC Avatar', 'gc-avatar@goosar.test', $1)
		ON CONFLICT (email) DO UPDATE SET avatar_url = EXCLUDED.avatar_url
		RETURNING id`, urls["user-avatar"]).Scan(&avatarUser); err != nil {
		t.Fatalf("create avatar user: %v", err)
	}
	var prevAgentAvatar, prevWsAvatar *string
	if err := testPool.QueryRow(ctx, `SELECT avatar_url FROM agent WHERE id = $1`, agentID).Scan(&prevAgentAvatar); err != nil {
		t.Fatalf("read agent avatar: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT avatar_url FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&prevWsAvatar); err != nil {
		t.Fatalf("read workspace avatar: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET avatar_url = $2 WHERE id = $1`, agentID, urls["agent-avatar"]); err != nil {
		t.Fatalf("set agent avatar: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET avatar_url = $2 WHERE id = $1`, testWorkspaceID, urls["workspace-avatar"]); err != nil {
		t.Fatalf("set workspace avatar: %v", err)
	}
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id, avatar_url)
		VALUES ($1, 'GC Fixture Squad ' || $2, $3, $4, $5)
		RETURNING id`, testWorkspaceID, fx.orphanID, agentID, userID, urls["squad-avatar"]).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'GC draft restore')
		RETURNING id`, testWorkspaceID, agentID, userID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_draft_restore (id, chat_session_id, task_id, content, attachment_ids)
		VALUES (gen_random_uuid(), $1, gen_random_uuid(), 'draft', ARRAY[$2::uuid])`,
		sessionID, ids["draft-restore"]); err != nil {
		t.Fatalf("create draft restore: %v", err)
	}

	var feedbackID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO feedback (user_id, workspace_id, message)
		VALUES ($1, $2, 'see ![](/api/attachments/' || $3 || '/download)')
		RETURNING id`, userID, testWorkspaceID, ids["feedback"]).Scan(&feedbackID); err != nil {
		t.Fatalf("create feedback: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM feedback WHERE id = $1`, feedbackID)
		testPool.Exec(bg, `DELETE FROM chat_session WHERE id = $1`, sessionID)
		testPool.Exec(bg, `DELETE FROM squad WHERE id = $1`, squadID)
		testPool.Exec(bg, `UPDATE agent SET avatar_url = $2 WHERE id = $1`, agentID, prevAgentAvatar)
		testPool.Exec(bg, `UPDATE workspace SET avatar_url = $2 WHERE id = $1`, testWorkspaceID, prevWsAvatar)
		testPool.Exec(bg, `DELETE FROM "user" WHERE id = $1`, avatarUser)
	})

	fx.age(t)
	store := &fakeGCStorage{}
	res, err := uploadgc.Sweep(ctx, db.New(testPool), store, uploadgc.Options{Grace: 24 * time.Hour})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !containsURL(res.URLs, fx.orphanURL) {
		t.Fatalf("control orphan was not reclaimed, swept urls=%v", res.URLs)
	}
	for _, kind := range kinds {
		if containsURL(res.URLs, urls[kind]) {
			t.Errorf("%s upload was selected by the orphan sweep", kind)
		}
		if containsURL(store.deleted, "key:"+urls[kind]) {
			t.Errorf("%s object was deleted from storage", kind)
		}
		var n int
		if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM attachment WHERE id = $1`, ids[kind]).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", kind, err)
		}
		if n != 1 {
			t.Errorf("%s attachment row was deleted by the orphan sweep", kind)
		}
	}
}

func TestOrphanUploadSweepRespectsGrace(t *testing.T) {
	orphanURL := newAttachmentGCFixture(t).orphanURL

	store := &fakeGCStorage{}
	res, err := uploadgc.Sweep(context.Background(), db.New(testPool), store, uploadgc.Options{Grace: 24 * time.Hour})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if containsURL(res.URLs, orphanURL) {
		t.Fatalf("a just-uploaded orphan must survive the grace window, swept urls=%v", res.URLs)
	}
	if containsURL(store.deleted, "key:"+orphanURL) {
		t.Fatalf("a just-uploaded orphan object must survive the grace window, got %v", store.deleted)
	}
}

func TestOrphanUploadSweepDryRun(t *testing.T) {
	ctx := context.Background()
	fx := newAttachmentGCFixture(t)
	fx.age(t)

	store := &fakeGCStorage{}
	res, err := uploadgc.Sweep(ctx, db.New(testPool), store, uploadgc.Options{Grace: 24 * time.Hour, DryRun: true})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !containsURL(res.URLs, fx.orphanURL) {
		t.Fatalf("dry run must report the orphan it would remove, swept urls=%v", res.URLs)
	}
	if res.Deleted != 0 || len(store.deleted) != 0 {
		t.Fatalf("dry run must delete nothing, got deleted=%d storage=%v", res.Deleted, store.deleted)
	}
	var n int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM attachment WHERE id = $1`, fx.orphanID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatal("dry run deleted the orphan row")
	}
}

func TestOrphanUploadDeleteRechecksOwnership(t *testing.T) {
	ctx := context.Background()
	fx := newAttachmentGCFixture(t)
	fx.age(t)
	queries := db.New(testPool)

	rows, err := queries.ListOrphanAttachments(ctx, db.ListOrphanAttachmentsParams{
		GraceSecs: (24 * time.Hour).Seconds(),
		MaxRows:   1000,
	})
	if err != nil {
		t.Fatalf("list orphans: %v", err)
	}
	var ids []pgtype.UUID
	var selected bool
	for _, row := range rows {
		if row.Url == fx.orphanURL {
			ids = append(ids, row.ID)
			selected = true
		}
	}
	if !selected {
		t.Fatal("fixture orphan was not selected, nothing to race")
	}

	if _, err := testPool.Exec(ctx,
		`UPDATE attachment SET issue_id = $2 WHERE id = $1`, fx.orphanID, fx.issueID); err != nil {
		t.Fatalf("bind orphan to issue: %v", err)
	}

	deleted, err := queries.DeleteAttachmentsByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("a row bound after selection must not be deleted, deleted=%d", deleted)
	}
	var n int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM attachment WHERE id = $1`, fx.orphanID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatal("the newly bound attachment was deleted by the sweep")
	}
}

func TestHygieneEnvDefaults(t *testing.T) {
	t.Setenv("GOOSAR_SCHEDULER_AUDIT_RETENTION", "")
	t.Setenv("GOOSAR_UPLOAD_GC_GRACE", "")
	if got := cronRetentionFromEnv(); got != defaultCronRetention {
		t.Fatalf("cron retention default = %s, want %s", got, defaultCronRetention)
	}
	if got := uploadGCGraceFromEnv(); got != uploadgc.DefaultGrace {
		t.Fatalf("upload grace default = %s, want %s", got, uploadgc.DefaultGrace)
	}
	t.Setenv("GOOSAR_SCHEDULER_AUDIT_RETENTION", "0")
	t.Setenv("GOOSAR_UPLOAD_GC_GRACE", "0")
	if got := cronRetentionFromEnv(); got != 0 {
		t.Fatalf("explicit 0 must disable the cron purge, got %s", got)
	}
	if got := uploadGCGraceFromEnv(); got != 0 {
		t.Fatalf("explicit 0 must disable the upload GC, got %s", got)
	}
}

func TestHygieneTickIsLeaderElected(t *testing.T) {
	queries := db.New(testPool)
	ctx := context.Background()

	holder, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire holder connection: %v", err)
	}
	defer holder.Release()
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_lock($1)", leader.KeyHygieneSweep); err != nil {
		t.Fatalf("hold the hygiene lock: %v", err)
	}
	defer holder.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", leader.KeyHygieneSweep)

	if won := hygieneTick(ctx, testPool, queries, nil, 0, 0, retention.Policy{}); won {
		t.Fatal("a second replica must skip the tick while another holds the hygiene lock")
	}

	if _, err := holder.Exec(ctx, "SELECT pg_advisory_unlock($1)", leader.KeyHygieneSweep); err != nil {
		t.Fatalf("release the hygiene lock: %v", err)
	}
	if won := hygieneTick(ctx, testPool, queries, nil, 0, 0, retention.Policy{}); !won {
		t.Fatal("the tick must run once the lock is free")
	}
}
