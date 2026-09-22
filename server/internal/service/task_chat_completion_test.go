package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

type chatCompletionFixture struct {
	WorkspaceID   string
	UserID        string
	AgentID       string
	RuntimeID     string
	IssueID       string
	ChatSessionID string
	IssuePrefix   string
	IssueNumber   int32
}

func newChatCompletionFixture(t *testing.T, pool *pgxpool.Pool) chatCompletionFixture {
	t.Helper()
	ctx := context.Background()
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read seeded agent runtime: %v", err)
	}

	var issuePrefix string
	var issueNumber int32
	if err := pool.QueryRow(ctx, `SELECT w.issue_prefix, i.number FROM issue i JOIN workspace w ON w.id = i.workspace_id WHERE i.id = $1`,
		issueID).Scan(&issuePrefix, &issueNumber); err != nil {
		t.Fatalf("read issue identifier: %v", err)
	}

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID) })

	return chatCompletionFixture{
		WorkspaceID:   workspaceID,
		UserID:        userID,
		AgentID:       agentID,
		RuntimeID:     runtimeID,
		IssueID:       issueID,
		ChatSessionID: chatSessionID,
		IssuePrefix:   issuePrefix,
		IssueNumber:   issueNumber,
	}
}

func newChatTask(t *testing.T, pool *pgxpool.Pool, f chatCompletionFixture, originatorUserID pgtype.UUID) db.AgentTaskQueue {
	t.Helper()
	ctx := context.Background()

	var taskID string

	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, 'running', 0, $4, $4) RETURNING id`,
		f.AgentID, f.RuntimeID, f.ChatSessionID, originatorUserID).Scan(&taskID); err != nil {
		t.Fatalf("seed chat task: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`, taskID); err != nil {
		t.Fatalf("self-own chat input batch: %v", err)
	}

	task, err := db.New(pool).GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("reload chat task: %v", err)
	}
	return task
}

func taskCompletedResult(t *testing.T, output string) []byte {
	t.Helper()
	b, err := json.Marshal(protocol.TaskCompletedPayload{Output: output})
	if err != nil {
		t.Fatalf("marshal TaskCompletedPayload: %v", err)
	}
	return b
}

func TestWriteChatCompletionOutcomeEmptyNoAttachmentsWritesLocalizedNoResponse(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	f := newChatCompletionFixture(t, pool)
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	cases := []struct {
		name     string
		language string
		want     string
	}{
		{name: "unset language defaults to Russian", language: "", want: chatNoResponseFallbackByLang[EmailLangRU]},
		{name: "ru language", language: "ru", want: chatNoResponseFallbackByLang[EmailLangRU]},
		{name: "en language", language: "en", want: chatNoResponseFallbackByLang[EmailLangEN]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `UPDATE "user" SET language = $1 WHERE id = $2`, tc.language, f.UserID); err != nil {
				t.Fatalf("set user language: %v", err)
			}
			task := newChatTask(t, pool, f, util.MustParseUUID(f.UserID))

			msg, err := svc.writeChatCompletionOutcome(ctx, q, task, taskCompletedResult(t, "   \n\t  "))
			if err != nil {
				t.Fatalf("writeChatCompletionOutcome: %v", err)
			}
			if msg == nil {
				t.Fatal("writeChatCompletionOutcome returned no row, want a no_response row")
			}
			if msg.MessageKind != protocol.ChatMessageKindNoResponse {
				t.Errorf("message_kind = %q, want %q", msg.MessageKind, protocol.ChatMessageKindNoResponse)
			}
			if msg.Content != tc.want {
				t.Errorf("content = %q, want %q", msg.Content, tc.want)
			}

			const hardcodedEnglishLiteral = "The agent finished this turn without a text reply."
			if tc.language != "en" && msg.Content == hardcodedEnglishLiteral {
				t.Errorf("content is the old hardcoded English literal %q for language %q", hardcodedEnglishLiteral, tc.language)
			}
		})
	}
}

func TestWriteChatCompletionOutcomeEmptyWithAttachmentsWritesPlainMessage(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	f := newChatCompletionFixture(t, pool)
	task := newChatTask(t, pool, f, pgtype.UUID{})

	var attachmentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO attachment (workspace_id, task_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, $2, 'agent', $3, 'shot.png', 'https://cdn.test/shot.png', 'image/png', 100) RETURNING id`,
		f.WorkspaceID, task.ID, f.AgentID).Scan(&attachmentID); err != nil {
		t.Fatalf("seed attachment: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	msg, err := svc.writeChatCompletionOutcome(ctx, q, task, taskCompletedResult(t, ""))
	if err != nil {
		t.Fatalf("writeChatCompletionOutcome: %v", err)
	}
	if msg == nil {
		t.Fatal("writeChatCompletionOutcome returned no row, want an attachments-only message row")
	}
	if msg.MessageKind == protocol.ChatMessageKindNoResponse {
		t.Errorf("message_kind = %q, want a plain message (attachments ARE the reply)", msg.MessageKind)
	}
	if msg.Content != "" {
		t.Errorf("content = %q, want empty (attachment cards are the response)", msg.Content)
	}

	var boundMessageID pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT chat_message_id FROM attachment WHERE id = $1`, attachmentID).Scan(&boundMessageID); err != nil {
		t.Fatalf("read attachment binding: %v", err)
	}
	if !boundMessageID.Valid || boundMessageID.Bytes != msg.ID.Bytes {
		t.Errorf("attachment chat_message_id = %s, want %s", util.UUIDToString(boundMessageID), util.UUIDToString(msg.ID))
	}
}

func TestWriteChatCompletionOutcomeNonEmptyWritesPlainMessage(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	f := newChatCompletionFixture(t, pool)
	task := newChatTask(t, pool, f, pgtype.UUID{})

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	msg, err := svc.writeChatCompletionOutcome(ctx, q, task, taskCompletedResult(t, "Done, see the PR."))
	if err != nil {
		t.Fatalf("writeChatCompletionOutcome: %v", err)
	}
	if msg == nil {
		t.Fatal("writeChatCompletionOutcome returned no row, want a message row")
	}
	if msg.MessageKind == protocol.ChatMessageKindNoResponse {
		t.Errorf("message_kind = %q, want a plain message", msg.MessageKind)
	}
	if msg.Content != "Done, see the PR." {
		t.Errorf("content = %q, want the agent's own text unchanged", msg.Content)
	}
}

func TestWriteChatCompletionOutcomeChannelOrLegacyEmptyWritesNoRow(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	f := newChatCompletionFixture(t, pool)
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	t.Run("legacy task with no chat_input_task_id", func(t *testing.T) {
		var taskID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority)
			VALUES ($1, $2, $3, 'running', 0) RETURNING id`, f.AgentID, f.RuntimeID, f.ChatSessionID).Scan(&taskID); err != nil {
			t.Fatalf("seed legacy chat task: %v", err)
		}
		t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
		task, err := q.GetAgentTask(ctx, util.MustParseUUID(taskID))
		if err != nil {
			t.Fatalf("reload legacy chat task: %v", err)
		}

		msg, err := svc.writeChatCompletionOutcome(ctx, q, task, taskCompletedResult(t, ""))
		if err != nil {
			t.Fatalf("writeChatCompletionOutcome: %v", err)
		}
		if msg != nil {
			t.Errorf("got a row for a legacy empty completion, want nil: %+v", msg)
		}
	})

	t.Run("channel-ingested task", func(t *testing.T) {
		task := newChatTask(t, pool, f, pgtype.UUID{})
		if _, err := pool.Exec(ctx, `
			INSERT INTO chat_message (chat_session_id, role, content, task_id, channel_ingested)
			VALUES ($1, 'user', 'hi from slack', $2, true)`, f.ChatSessionID, task.ID); err != nil {
			t.Fatalf("seed channel-ingested user message: %v", err)
		}

		msg, err := svc.writeChatCompletionOutcome(ctx, q, task, taskCompletedResult(t, ""))
		if err != nil {
			t.Fatalf("writeChatCompletionOutcome: %v", err)
		}
		if msg != nil {
			t.Errorf("got a row for a channel-ingested empty completion, want nil (silent-drop): %+v", msg)
		}
	})
}

func TestWriteChatCompletionOutcomeEmptyWithIssueCommentReferencesIt(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	f := newChatCompletionFixture(t, pool)
	task := newChatTask(t, pool, f, util.MustParseUUID(f.UserID))

	var commentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, source_task_id)
		VALUES ($1, $2, 'agent', $3, 'Fixed it, see the diff.', $4) RETURNING id`,
		f.WorkspaceID, f.IssueID, f.AgentID, task.ID).Scan(&commentID); err != nil {
		t.Fatalf("seed source-task comment: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	msg, err := svc.writeChatCompletionOutcome(ctx, q, task, taskCompletedResult(t, ""))
	if err != nil {
		t.Fatalf("writeChatCompletionOutcome: %v", err)
	}
	if msg == nil {
		t.Fatal("writeChatCompletionOutcome returned no row, want an informative message row")
	}
	if msg.MessageKind == protocol.ChatMessageKindNoResponse {
		t.Errorf("message_kind = %q, want a plain message (a discoverable side effect is a real outcome)", msg.MessageKind)
	}
	wantIdentifier := f.IssuePrefix + "-" + strconv.Itoa(int(f.IssueNumber))
	if !strings.Contains(msg.Content, wantIdentifier) {
		t.Errorf("content = %q, want it to reference %q", msg.Content, wantIdentifier)
	}
	if msg.Content == chatNoResponseFallbackByLang[EmailLangRU] || msg.Content == chatNoResponseFallbackByLang[EmailLangEN] {
		t.Errorf("content = %q, want the informative note, not the generic fallback", msg.Content)
	}
}
