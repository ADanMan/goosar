package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/attribution"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func cleanupChecked(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
			t.Logf("cleanup failed (sql=%q args=%v): %v", sql, args, err)
		}
	})
}

type chatSourcedCommentFixture struct {
	WorkspaceID  pgtype.UUID
	IssueID      pgtype.UUID
	AgentID      pgtype.UUID
	AgentOwnerID pgtype.UUID
	ChatTaskID   pgtype.UUID
	UserID       pgtype.UUID
	CommentID    pgtype.UUID
}

func seedChatSourcedCommentFixture(t *testing.T, pool *pgxpool.Pool) chatSourcedCommentFixture {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var userID, ownerID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Chat Originator U', $1) RETURNING id`,
		fmt.Sprintf("chat-orig-u-%d@goosar.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("seed user U: %v", err)
	}
	cleanupChecked(t, pool, `DELETE FROM "user" WHERE id = $1`, userID)
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Agent Owner', $1) RETURNING id`,
		fmt.Sprintf("chat-orig-owner-%d@goosar.test", suffix)).Scan(&ownerID); err != nil {
		t.Fatalf("seed agent owner: %v", err)
	}
	cleanupChecked(t, pool, `DELETE FROM "user" WHERE id = $1`, ownerID)

	var workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('chat-orig-ws', $1) RETURNING id`,
		fmt.Sprintf("chat-orig-ws-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	cleanupChecked(t, pool, `DELETE FROM workspace WHERE id = $1`, workspaceID)

	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'owner')`,
		workspaceID, userID, ownerID); err != nil {
		t.Fatalf("seed members: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'chat-orig-runtime', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id`, workspaceID, ownerID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'chat-orig-agent', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, workspaceID, runtimeID, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	var issueID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'unrelated issue X', 'member', $2)
		RETURNING id`, workspaceID, userID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	cleanupChecked(t, pool, `DELETE FROM chat_session WHERE id = $1`, chatSessionID)

	var chatTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, 'running', 0, $4, $4)
		RETURNING id`, agentID, runtimeID, chatSessionID, userID).Scan(&chatTaskID); err != nil {
		t.Fatalf("seed chat task: %v", err)
	}
	cleanupChecked(t, pool, `DELETE FROM agent_task_queue WHERE id = $1`, chatTaskID)

	var commentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, source_task_id)
		VALUES ($1, $2, 'agent', $3, '[@Target](mention://agent/does-not-matter) please help', $4)
		RETURNING id`, issueID, workspaceID, agentID, chatTaskID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	cleanupChecked(t, pool, `DELETE FROM comment WHERE id = $1`, commentID)

	return chatSourcedCommentFixture{
		WorkspaceID:  util.MustParseUUID(workspaceID),
		IssueID:      util.MustParseUUID(issueID),
		AgentID:      util.MustParseUUID(agentID),
		AgentOwnerID: util.MustParseUUID(ownerID),
		ChatTaskID:   util.MustParseUUID(chatTaskID),
		UserID:       util.MustParseUUID(userID),
		CommentID:    util.MustParseUUID(commentID),
	}
}

func TestResolveOriginatorFromTriggerComment_ChatTaskSourcedCommentDoesNotResolvePrecise(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	fx := seedChatSourcedCommentFixture(t, pool)
	svc := &TaskService{Queries: db.New(pool)}

	got := svc.resolveOriginatorFromTriggerComment(context.Background(), fx.WorkspaceID, fx.CommentID)
	if got.Valid {
		t.Fatalf("originator = %s, want invalid (chat task has no relation to this issue; must not leak %s)",
			util.UUIDToString(got), util.UUIDToString(fx.UserID))
	}
}

func TestAttributionForIssueTask_ChatTaskSourcedCommentDoesNotResolvePrecise(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	fx := seedChatSourcedCommentFixture(t, pool)
	svc := &TaskService{Queries: db.New(pool)}

	issue := db.Issue{ID: fx.IssueID, WorkspaceID: fx.WorkspaceID}
	got := svc.attributionForIssueTask(context.Background(), issue, fx.CommentID, attribution.SourceDelegation, pgtype.UUID{})
	if got.Source != attribution.SourceUnattributed {
		t.Fatalf("source = %q, want unattributed (chat task has no relation to this issue)", got.Source)
	}
	if got.UserID.Valid {
		t.Errorf("originator = %s, want invalid", util.UUIDToString(got.UserID))
	}
	if got.AccountableUserID.Valid {
		t.Errorf("accountable = %s, want invalid before applyAttributionFallback degrades it", util.UUIDToString(got.AccountableUserID))
	}
}

func TestApplyAttributionFallback_ChatTaskSourcedCommentFailsClosedWhenWorkspaceFailClosed(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	fx := seedChatSourcedCommentFixture(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `UPDATE workspace SET attribution_fail_closed = true WHERE id = $1`, util.UUIDToString(fx.WorkspaceID)); err != nil {
		t.Fatalf("set attribution_fail_closed: %v", err)
	}

	svc := &TaskService{Queries: db.New(pool)}
	issue := db.Issue{ID: fx.IssueID, WorkspaceID: fx.WorkspaceID}
	attr := svc.attributionForIssueTask(ctx, issue, fx.CommentID, attribution.SourceDelegation, pgtype.UUID{})

	agent, err := svc.Queries.GetAgent(ctx, fx.AgentID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	_, err = svc.applyAttributionFallback(ctx, attr, agent)
	if !errors.Is(err, ErrAttributionFailClosed) {
		t.Fatalf("applyAttributionFallback error = %v, want ErrAttributionFailClosed", err)
	}
}

func TestApplyAttributionFallback_ChatTaskSourcedCommentDegradesToOwnerFallback(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	fx := seedChatSourcedCommentFixture(t, pool)
	ctx := context.Background()

	svc := &TaskService{Queries: db.New(pool)}
	issue := db.Issue{ID: fx.IssueID, WorkspaceID: fx.WorkspaceID}
	attr := svc.attributionForIssueTask(ctx, issue, fx.CommentID, attribution.SourceDelegation, pgtype.UUID{})

	agent, err := svc.Queries.GetAgent(ctx, fx.AgentID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	fallback, err := svc.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		t.Fatalf("applyAttributionFallback: %v", err)
	}
	if fallback.Source != attribution.SourceOwnerFallback {
		t.Fatalf("source = %q, want owner_fallback", fallback.Source)
	}
	if fallback.UserID.Valid {
		t.Errorf("originator = %s, want invalid (owner_fallback carries no authorizing human)", util.UUIDToString(fallback.UserID))
	}
	if !fallback.AccountableUserID.Valid || fallback.AccountableUserID.Bytes != fx.AgentOwnerID.Bytes {
		t.Errorf("accountable = %s, want the agent's owner %s (never %s, the chat's human)",
			util.UUIDToString(fallback.AccountableUserID), util.UUIDToString(fx.AgentOwnerID), util.UUIDToString(fx.UserID))
	}
}
