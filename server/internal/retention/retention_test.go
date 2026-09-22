package retention

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

var (
	testPool    *pgxpool.Pool
	testQueries *db.Queries
	runID       = fmt.Sprintf("%d", time.Now().UnixNano())
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil || pool.Ping(ctx) != nil {
		if os.Getenv("GOOSAR_REQUIRE_TEST_DB") == "1" || os.Getenv("CI") != "" {
			fmt.Printf("FATAL: retention tests need a database: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Skipping retention tests: no database")
		os.Exit(0)
	}
	testPool = pool
	testQueries = db.New(pool)
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

type fakeStore struct{ deleted []string }

func (f *fakeStore) KeyFromURL(rawURL string) string { return rawURL }
func (f *fakeStore) DeleteKeys(_ context.Context, keys []string) {
	f.deleted = append(f.deleted, keys...)
}

type scope struct {
	workspaceID string
	userID      string
	agentID     string
	runtimeID   string
}

func newScope(t *testing.T, label string) scope {
	t.Helper()
	ctx := context.Background()
	var s scope
	unique := label + "-" + runID

	must := func(err error, what string) {
		t.Helper()
		if err != nil {
			t.Fatalf("fixture %s: %v", what, err)
		}
	}
	must(testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Retention "+label, "retention-"+unique+"@goosar.ru").Scan(&s.userID), "user")
	must(testPool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id`,
		"Retention "+label, "retention-ws-"+unique).Scan(&s.workspaceID), "workspace")
	must(testPool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider) VALUES ($1, $2, 'local', 'claude') RETURNING id`,
		s.workspaceID, "rt-"+unique).Scan(&s.runtimeID), "agent_runtime")
	must(testPool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id) VALUES ($1, $2, 'local', $3) RETURNING id`,
		s.workspaceID, "agent-"+unique, s.runtimeID).Scan(&s.agentID), "agent")

	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM attachment_tombstone WHERE workspace_id = $1`, s.workspaceID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, s.workspaceID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, s.userID)
	})
	return s
}

func (s scope) chatSession(t *testing.T, age time.Duration) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, created_at, updated_at)
		 VALUES ($1, $2, $3, 'session', now() - $4::interval, now() - $4::interval) RETURNING id`,
		s.workspaceID, s.agentID, s.userID, age.String()).Scan(&id); err != nil {
		t.Fatalf("insert chat session: %v", err)
	}
	return id
}

func rowExists(t *testing.T, table, id string) bool {
	t.Helper()
	var exists bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = $1)`, id).Scan(&exists); err != nil {
		t.Fatalf("exists %s: %v", table, err)
	}
	return exists
}

func TestKeepForeverIsTheDefaultAndDeletesNothing(t *testing.T) {
	s := newScope(t, "default")
	old := s.chatSession(t, 400*24*time.Hour)

	policy := PolicyFromEnv()
	if policy.Chat != 0 || policy.Tasks != 0 || policy.ClosedIssues != 0 || policy.Activity != 0 {
		t.Fatalf("shipped content policy is not keep-forever: %+v", policy)
	}
	if _, err := Run(context.Background(), testQueries, &fakeStore{}, policy, false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rowExists(t, "chat_session", old) {
		t.Error("a year-old chat session was deleted under the shipped keep-forever policy")
	}
}

func TestConfiguredChatWindowDeletesOldSessionsAndKeepsFreshOnes(t *testing.T) {
	s := newScope(t, "chat")
	old := s.chatSession(t, 60*24*time.Hour)
	fresh := s.chatSession(t, time.Hour)

	report, err := Run(context.Background(), testQueries, &fakeStore{}, Policy{Chat: 30 * 24 * time.Hour}, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.ChatSessions == 0 {
		t.Error("the pass reported no chat sessions purged")
	}
	if rowExists(t, "chat_session", old) {
		t.Error("a session older than the window survived the purge")
	}
	if !rowExists(t, "chat_session", fresh) {
		t.Error("a session inside the window was purged")
	}
}

func TestChatPurgeTakesTheMessagesWithTheSession(t *testing.T) {
	s := newScope(t, "chatmsg")
	session := s.chatSession(t, 60*24*time.Hour)
	var messageID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', 'body') RETURNING id`,
		session).Scan(&messageID); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	if _, err := Run(context.Background(), testQueries, &fakeStore{}, Policy{Chat: 30 * 24 * time.Hour}, false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rowExists(t, "chat_message", messageID) {
		t.Error("the message outlived its purged session — a retention window that leaves the text behind is not a retention window")
	}
}

func TestTaskWindowSparesWorkInFlight(t *testing.T) {
	s := newScope(t, "tasks")
	ctx := context.Background()

	var finished, running string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, status, completed_at, created_at)
		 VALUES ($1, $2, 'completed', now() - interval '60 days', now() - interval '60 days') RETURNING id`,
		s.agentID, s.runtimeID).Scan(&finished); err != nil {
		t.Fatalf("insert finished task: %v", err)
	}

	if err := testPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, status, created_at)
		 VALUES ($1, $2, 'running', now() - interval '60 days') RETURNING id`,
		s.agentID, s.runtimeID).Scan(&running); err != nil {
		t.Fatalf("insert running task: %v", err)
	}

	if _, err := Run(ctx, testQueries, &fakeStore{}, Policy{Tasks: 30 * 24 * time.Hour}, false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rowExists(t, "agent_task_queue", finished) {
		t.Error("a finished task older than the window survived")
	}
	if !rowExists(t, "agent_task_queue", running) {
		t.Error("a RUNNING task was purged by a retention window")
	}
}

func TestExpiredVerificationCodesArePurgedWithoutASignIn(t *testing.T) {
	ctx := context.Background()
	email := "retention-code-" + runID + "@goosar.ru"
	if _, err := testPool.Exec(ctx,
		`INSERT INTO verification_code (email, code, expires_at) VALUES ($1, '000000', now() - interval '2 days')`,
		email); err != nil {
		t.Fatalf("insert code: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM verification_code WHERE email = $1`, email)
	})

	if _, err := Run(ctx, testQueries, &fakeStore{}, Policy{}, false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var left int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM verification_code WHERE email = $1`, email).Scan(&left); err != nil {
		t.Fatalf("count codes: %v", err)
	}
	if left != 0 {
		t.Error("an expired login code survived the scheduled purge — until now they were only cleared when somebody happened to sign in")
	}
}

func TestDeletingAnAttachmentLeavesItsObjectReclaimable(t *testing.T) {
	s := newScope(t, "tombstone")
	ctx := context.Background()

	var issueID, attachmentID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, creator_type, creator_id)
		 VALUES ($1, 'issue', 'todo', 'member', $2) RETURNING id`,
		s.workspaceID, s.userID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	url := "https://example.invalid/uploads/" + runID + "-tombstone.png"
	if err := testPool.QueryRow(ctx,
		`INSERT INTO attachment (workspace_id, issue_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		 VALUES ($1, $2, 'member', $3, 'f.png', $4, 'image/png', 1) RETURNING id`,
		s.workspaceID, issueID, s.userID, url).Scan(&attachmentID); err != nil {
		t.Fatalf("insert attachment: %v", err)
	}

	if _, err := testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID); err != nil {
		t.Fatalf("delete issue: %v", err)
	}

	var tombstoned int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM attachment_tombstone WHERE attachment_id = $1`, attachmentID).Scan(&tombstoned); err != nil {
		t.Fatalf("read tombstone: %v", err)
	}
	if tombstoned != 1 {
		t.Fatal("deleting an issue left no tombstone for its attachment, so the stored object is unreachable forever")
	}

	store := &fakeStore{}
	if _, err := Run(ctx, testQueries, store, Policy{AttachmentGrace: 24 * time.Hour}, false); err != nil {
		t.Fatalf("Run inside grace: %v", err)
	}
	for _, key := range store.deleted {
		if key == url {
			t.Fatal("the object was deleted inside its grace window")
		}
	}

	if _, err := testPool.Exec(ctx,
		`UPDATE attachment_tombstone SET deleted_at = now() - interval '30 days' WHERE attachment_id = $1`,
		attachmentID); err != nil {
		t.Fatalf("age tombstone: %v", err)
	}
	store = &fakeStore{}
	if _, err := Run(ctx, testQueries, store, Policy{AttachmentGrace: 24 * time.Hour}, false); err != nil {
		t.Fatalf("Run past grace: %v", err)
	}
	var deletedTheObject bool
	for _, key := range store.deleted {
		if key == url {
			deletedTheObject = true
		}
	}
	if !deletedTheObject {
		t.Errorf("the storage object was not reclaimed; sweep deleted %v", store.deleted)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM attachment_tombstone WHERE attachment_id = $1`, attachmentID).Scan(&tombstoned); err != nil {
		t.Fatalf("recount tombstone: %v", err)
	}
	if tombstoned != 0 {
		t.Error("the ledger row survived its own sweep, so the next pass would delete the object again")
	}
}

func TestAReUploadedObjectKeepsItsBytes(t *testing.T) {
	s := newScope(t, "revived")
	ctx := context.Background()

	url := "https://example.invalid/uploads/" + runID + "-revived.png"

	if _, err := testPool.Exec(ctx,
		`INSERT INTO attachment_tombstone (attachment_id, workspace_id, url, deleted_at)
		 VALUES (gen_random_uuid(), $1, $2, now() - interval '30 days')`,
		s.workspaceID, url); err != nil {
		t.Fatalf("insert tombstone: %v", err)
	}

	if _, err := testPool.Exec(ctx,
		`INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		 VALUES ($1, 'member', $2, 'f.png', $3, 'image/png', 1)`,
		s.workspaceID, s.userID, url); err != nil {
		t.Fatalf("insert live attachment: %v", err)
	}

	store := &fakeStore{}
	report, err := Run(ctx, testQueries, store, Policy{AttachmentGrace: 24 * time.Hour}, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, key := range store.deleted {
		if key == url {
			t.Fatal("the sweep deleted the bytes of an object a live attachment is serving")
		}
	}
	if report.AttachmentRevived == 0 {
		t.Error("the sweep did not report the revived object it spared")
	}
	var left int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM attachment_tombstone WHERE url = $1`, url).Scan(&left); err != nil {
		t.Fatalf("count tombstones: %v", err)
	}
	if left != 0 {
		t.Error("the spared tombstone stayed in the ledger and would be re-examined forever")
	}
}

func TestDryRunReportsTheBacklogAndChangesNothing(t *testing.T) {
	s := newScope(t, "dryrun")
	old := s.chatSession(t, 60*24*time.Hour)

	report, err := Run(context.Background(), testQueries, &fakeStore{}, Policy{Chat: 30 * 24 * time.Hour}, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.DryRun {
		t.Error("the report does not say it was a dry run")
	}
	if report.ChatSessions == 0 {
		t.Error("a dry run over a matching backlog reported nothing")
	}
	if !rowExists(t, "chat_session", old) {
		t.Fatal("a DRY RUN deleted a chat session")
	}
}

func TestABadWindowFallsBackToTheDefaultInsteadOfDisablingRetention(t *testing.T) {
	t.Setenv(EnvAttachmentGrace, "not-a-duration")
	if got := PolicyFromEnv().AttachmentGrace; got != DefaultAttachmentGrace {
		t.Errorf("AttachmentGrace = %s, want the default %s — a typo must not silently disable a sweep", got, DefaultAttachmentGrace)
	}
	t.Setenv(EnvAttachmentGrace, "-5h")
	if got := PolicyFromEnv().AttachmentGrace; got != DefaultAttachmentGrace {
		t.Errorf("a negative window resolved to %s, want the default %s", got, DefaultAttachmentGrace)
	}
	t.Setenv(EnvChat, "720h")
	if got := PolicyFromEnv().Chat; got != 720*time.Hour {
		t.Errorf("Chat = %s, want 720h", got)
	}
}
