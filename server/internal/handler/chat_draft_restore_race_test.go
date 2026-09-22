package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type draftRestoreRaceFixture struct {
	workspaceID   string
	chatSessionID string
	taskID        string
}

func seedDraftRestoreRaceFixture(t *testing.T, slug string) draftRestoreRaceFixture {
	t.Helper()
	ctx := context.Background()

	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, '')
		RETURNING id
	`, "Draft Restore Race", slug).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Draft Restore Race Runtime', 'cloud', 'isolated_test', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, wsID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Draft Restore Race Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, wsID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		VALUES ($1, $2, $3, 'Draft Restore Race Session', 'active')
		RETURNING id
	`, wsID, agentID, testUserID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, chat_session_id, status, priority, context, runtime_id,
			started_at, chat_finalize_deferred_at
		)
		VALUES ($1, $2, 'cancelled', 0, '{}'::jsonb, $3, now(), now())
		RETURNING id
	`, agentID, sessionID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create deferred chat task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', 'the prompt the cancel ate', $2)
	`, sessionID, taskID); err != nil {
		t.Fatalf("create user chat message: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		testPool.Exec(cleanupCtx, `DELETE FROM chat_draft_restore WHERE chat_session_id = $1`, sessionID)
		testPool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	return draftRestoreRaceFixture{workspaceID: wsID, chatSessionID: sessionID, taskID: taskID}
}

func waitForBlockedBackend(t *testing.T, done <-chan struct{}) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return false
		default:
		}
		var blocked int
		if err := testPool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'
		`).Scan(&blocked); err == nil && blocked > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestFinalizeDeferredCancelledChat_TakesTheChatSessionLockBeforeInserting(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := seedDraftRestoreRaceFixture(t, "handler-tests-draft-restore-race-writer")

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock holder tx: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT id FROM chat_session WHERE id = $1 FOR UPDATE`, f.chatSessionID); err != nil {
		t.Fatalf("lock chat session: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		testHandler.TaskService.FinalizeDeferredCancelledChat(context.Background(), parseUUID(f.taskID))
	}()

	if !waitForBlockedBackend(t, done) {
		t.Fatal("finalizer settled while the chat_session row was locked: it never took the lock, so nothing stops it from inserting a restore behind a deleter's sweep")
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("release chat session lock: %v", err)
	}
	<-done

	if n := countDraftRestores(t, f.chatSessionID); n != 1 {
		t.Errorf("expected the finalizer to write 1 restore once the lock cleared, got %d", n)
	}
}

func TestDeleteWorkspace_SweepsRestoreCommittedByAConcurrentFinalizer(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := seedDraftRestoreRaceFixture(t, "handler-tests-draft-restore-race-deleter")

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin finalizer tx: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT id FROM chat_session WHERE id = $1 FOR UPDATE`, f.chatSessionID); err != nil {
		t.Fatalf("lock chat session: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO chat_draft_restore (id, chat_session_id, task_id, content, attachment_ids)
		VALUES ($1, $2, $3, 'the prompt the cancel ate', '{}'::uuid[])
	`, uuid.NewString(), f.chatSessionID, f.taskID); err != nil {
		t.Fatalf("insert draft restore: %v", err)
	}

	code := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodDelete, fmt.Sprintf("/api/workspaces/%s", f.workspaceID), nil), "id", f.workspaceID)
		testHandler.DeleteWorkspace(w, req)
		code <- w.Code
	}()

	if !waitForBlockedBackend(t, done) {
		t.Fatal("DeleteWorkspace finished while a finalizer held the session lock: it never took the lock, so its sweep cannot see a restore committed behind it")
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit finalizer tx: %v", err)
	}
	<-done

	if got := <-code; got != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace: expected 204, got %d", got)
	}
	if n := countDraftRestores(t, f.chatSessionID); n != 0 {
		t.Errorf("a restore committed during the teardown survived it (%d row(s)) — orphaned, holding the user's prompt", n)
	}
}

func TestCreateChatSession_BlocksWhileTheWorkspaceDeleteLockIsHeld(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := seedDraftRestoreRaceFixture(t, "handler-tests-draft-restore-race-newsession")

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT agent_id FROM chat_session WHERE id = $1`, f.chatSessionID).Scan(&agentID); err != nil {
		t.Fatalf("read fixture agent: %v", err)
	}

	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(testUserID),
		WorkspaceID: parseUUID(f.workspaceID),
	})
	if err != nil {
		t.Fatalf("load fixture owner member: %v", err)
	}

	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workspace-lock holder tx: %v", err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `SELECT id FROM workspace WHERE id = $1 FOR UPDATE`, f.workspaceID); err != nil {
		t.Fatalf("lock workspace row: %v", err)
	}

	code := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := httptest.NewRecorder()
		req := newRequestAs(testUserID, http.MethodPost, "/api/chat/sessions", map[string]any{
			"agent_id": agentID,
			"title":    "Session racing the teardown",
		})
		req = req.WithContext(middleware.SetMemberContext(req.Context(), f.workspaceID, member))
		testHandler.CreateChatSession(w, req)
		code <- w.Code
	}()

	if !waitForBlockedBackend(t, done) {
		t.Fatal("CreateChatSession returned while the workspace delete lock was held: it never took LockWorkspaceForChatSessionCreate, so a session can be created into a workspace mid-delete and its restore can outlive the cascade")
	}

	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("release workspace lock: %v", err)
	}
	<-done
	if got := <-code; got != http.StatusCreated {
		t.Fatalf("CreateChatSession: expected 201 once the delete lock cleared, got %d", got)
	}
}

func TestFinalizeDeferredCancelledChat_SkipsTheInsertWhenTheSessionIsGone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := seedDraftRestoreRaceFixture(t, "handler-tests-draft-restore-race-gone")

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodDelete, fmt.Sprintf("/api/workspaces/%s", f.workspaceID), nil), "id", f.workspaceID)
	testHandler.DeleteWorkspace(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	testHandler.TaskService.FinalizeDeferredCancelledChat(ctx, parseUUID(f.taskID))

	if n := countDraftRestores(t, f.chatSessionID); n != 0 {
		t.Errorf("finalizer wrote %d restore(s) for a session that no longer exists", n)
	}
}
