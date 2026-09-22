package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/attribution"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type agentCreateOriginScopeFixture struct {
	Issue        db.Issue
	ChatUserID   pgtype.UUID
	OriginTaskID pgtype.UUID
}

func seedAgentCreateOriginScopeFixture(t *testing.T, pool *pgxpool.Pool) agentCreateOriginScopeFixture {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Origin Scope User', $1) RETURNING id`,
		fmt.Sprintf("origin-scope-u-%d@goosar.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	cleanupChecked(t, pool, `DELETE FROM "user" WHERE id = $1`, userID)

	var workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('origin-scope-ws', $1) RETURNING id`,
		fmt.Sprintf("origin-scope-ws-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	cleanupChecked(t, pool, `DELETE FROM workspace WHERE id = $1`, workspaceID)

	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'origin-scope-runtime', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'origin-scope-agent', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	cleanupChecked(t, pool, `DELETE FROM chat_session WHERE id = $1`, chatSessionID)

	var originTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, 'running', 0, $4, $4)
		RETURNING id`, agentID, runtimeID, chatSessionID, userID).Scan(&originTaskID); err != nil {
		t.Fatalf("seed chat origin task: %v", err)
	}
	cleanupChecked(t, pool, `DELETE FROM agent_task_queue WHERE id = $1`, originTaskID)

	return agentCreateOriginScopeFixture{
		Issue: db.Issue{
			CreatorType: "agent",
			OriginType:  pgtype.Text{String: "agent_create", Valid: true},
			OriginID:    util.MustParseUUID(originTaskID),
			WorkspaceID: util.MustParseUUID(workspaceID),
		},
		ChatUserID:   util.MustParseUUID(userID),
		OriginTaskID: util.MustParseUUID(originTaskID),
	}
}

func TestAttributionForIssueTask_AgentCreateOriginFromChatTaskDoesNotInherit(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	fx := seedAgentCreateOriginScopeFixture(t, pool)
	svc := &TaskService{Queries: db.New(pool)}

	got := svc.attributionForIssueTask(context.Background(), fx.Issue, pgtype.UUID{}, attribution.SourceCommentSource, pgtype.UUID{})
	if got.UserID.Valid {
		t.Fatalf("originator = %s, want INVALID -- a chat-originated issue must not inherit the chat human as a scoped originator",
			util.UUIDToString(got.UserID))
	}
	if got.Source != attribution.SourceUnattributed {
		t.Errorf("source = %q, want unattributed", got.Source)
	}
}

func TestOriginatorForIssueTask_AgentCreateOriginFromChatTaskReturnsInvalid(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	fx := seedAgentCreateOriginScopeFixture(t, pool)
	svc := &TaskService{Queries: db.New(pool)}

	got := svc.OriginatorForIssueTask(context.Background(), fx.Issue, pgtype.UUID{})
	if got.Valid {
		t.Fatalf("gate originator = %s, want INVALID", util.UUIDToString(got))
	}
}
