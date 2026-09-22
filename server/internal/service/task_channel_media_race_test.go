package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type raceInjectTxStarter struct {
	pool   *pgxpool.Pool
	inject func()
}

func (s *raceInjectTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &raceInjectTx{Tx: tx, inject: s.inject}, nil
}

type raceInjectTx struct {
	pgx.Tx
	inject func()
}

func (t *raceInjectTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "LinkUnownedChannelChatMessagesToTask") && t.inject != nil {
		t.inject()
		t.inject = nil
	}
	return t.Tx.Exec(ctx, sql, args...)
}

func TestEnqueueChatTaskDefersWhenMediaMessageCommitsDuringEnqueue(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'look at this')`, chatSessionID); err != nil {
		t.Fatalf("seed text message: %v", err)
	}

	deadline := time.Now().Add(time.Minute)
	var mediaMessageID string
	svc := &TaskService{
		Queries: q,
		TxStarter: &raceInjectTxStarter{pool: pool, inject: func() {

			if err := pool.QueryRow(ctx, `
				INSERT INTO chat_message (chat_session_id, role, content, channel_media_pending_until)
				VALUES ($1, 'user', '[Image]', $2) RETURNING id`, chatSessionID, deadline).Scan(&mediaMessageID); err != nil {
				t.Errorf("inject media message: %v", err)
			}
		}},
		Bus: events.New(),
	}

	task, err := svc.EnqueueChatTask(ctx, db.ChatSession{
		ID:      util.MustParseUUID(chatSessionID),
		AgentID: util.MustParseUUID(agentID),
	}, util.MustParseUUID(userID), false)
	if err != nil {
		t.Fatalf("EnqueueChatTask: %v", err)
	}
	if mediaMessageID == "" {
		t.Fatal("race injection did not run")
	}

	var linkedTaskID pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT task_id FROM chat_message WHERE id = $1`, mediaMessageID).Scan(&linkedTaskID); err != nil {
		t.Fatalf("load sealed media message: %v", err)
	}
	if !linkedTaskID.Valid || linkedTaskID.Bytes != task.ID.Bytes {
		t.Fatalf("media message task_id = %s, want %s", util.UUIDToString(linkedTaskID), util.UUIDToString(task.ID))
	}

	if task.Status != "deferred" || !task.FireAt.Valid {
		t.Fatalf("task = status %q fire_at %v, want deferred until the sealed media deadline", task.Status, task.FireAt)
	}
	if task.FireAt.Time.Before(deadline.Add(-time.Second)) || task.FireAt.Time.After(deadline.Add(time.Second)) {
		t.Fatalf("task fire_at = %v, want sealed media deadline %v", task.FireAt.Time, deadline)
	}

	if err := q.ClearChatMessageChannelMediaPending(ctx, db.ClearChatMessageChannelMediaPendingParams{
		ID:            util.MustParseUUID(mediaMessageID),
		ChatSessionID: util.MustParseUUID(chatSessionID),
	}); err != nil {
		t.Fatalf("clear pending media: %v", err)
	}
	if err := svc.PromoteChannelChatTasksIfMediaReady(ctx, util.MustParseUUID(chatSessionID)); err != nil {
		t.Fatalf("PromoteChannelChatTasksIfMediaReady: %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&status); err != nil {
		t.Fatalf("load promoted task: %v", err)
	}
	if status != "queued" {
		t.Fatalf("promoted task status = %q, want queued", status)
	}
}
