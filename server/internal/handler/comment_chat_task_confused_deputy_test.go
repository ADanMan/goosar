package handler

import (
	"context"
	"testing"

	"github.com/adanman/goosar/server/internal/util"
)

func cleanupCheckedExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), sql, args...); err != nil {
			t.Logf("cleanup failed (sql=%q args=%v): %v", sql, args, err)
		}
	})
}

type chatTaskConfusedDeputyFixture struct {
	WorkspaceID string
	IssueID     string
	AgentAID    string
	AgentBID    string
	UserID      string
	ChatTaskID  string
	CommentID   string
}

func seedChatTaskConfusedDeputyFixture(t *testing.T) chatTaskConfusedDeputyFixture {
	t.Helper()
	ctx := context.Background()

	agentAID := createHandlerTestAgent(t, "Confused Deputy Agent A", nil)

	var agentBID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'Confused Deputy Target B', '', 'cloud', '{}'::jsonb,
			$2, 'private', 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, handlerTestRuntimeID(t), testUserID).Scan(&agentBID); err != nil {
		t.Fatalf("seed agent B: %v", err)
	}
	cleanupCheckedExec(t, `DELETE FROM agent WHERE id = $1`, agentBID)

	chatSessionID := seedChatSession(t, agentAID)
	chatTaskID := seedRunningChatTask(t, agentAID, chatSessionID)

	issueID := createCommentTriggerPreviewIssue(t, "confused deputy target issue X", "", "")

	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, source_task_id)
		VALUES ($1, $2, 'agent', $3, $4, $5)
		RETURNING id
	`, issueID, testWorkspaceID, agentAID,
		"[@B](mention://agent/"+agentBID+") please help", chatTaskID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	cleanupCheckedExec(t, `DELETE FROM comment WHERE id = $1`, commentID)

	return chatTaskConfusedDeputyFixture{
		WorkspaceID: testWorkspaceID,
		IssueID:     issueID,
		AgentAID:    agentAID,
		AgentBID:    agentBID,
		UserID:      testUserID,
		ChatTaskID:  chatTaskID,
		CommentID:   commentID,
	}
}

func TestChatTaskSourcedComment_ResolvedOriginatorFailsClosedForA2A(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := seedChatTaskConfusedDeputyFixture(t)

	originator := testHandler.TaskService.ResolveOriginatorFromTriggerComment(ctx, util.MustParseUUID(fx.WorkspaceID), util.MustParseUUID(fx.CommentID))
	if originator.Valid {
		t.Fatalf("resolved originator = %s, want invalid (U never touched issue X; must not leak from the unrelated chat task)",
			util.UUIDToString(originator))
	}

	agentB, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(fx.AgentBID))
	if err != nil {
		t.Fatalf("load agent B: %v", err)
	}
	if testHandler.canInvokeAgent(ctx, agentB, "agent", fx.AgentAID, scopedInvokeAuthority(uuidToString(originator)), fx.WorkspaceID) {
		t.Fatal("canInvokeAgent admitted the A2A invocation — confused-deputy hole: U's ownership of B leaked from an unrelated chat task")
	}
}

func TestChatTaskSourcedComment_MentionTriggerDoesNotEnqueueTarget(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := seedChatTaskConfusedDeputyFixture(t)

	comment, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(fx.CommentID))
	if err != nil {
		t.Fatalf("load comment: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(fx.IssueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	originator := testHandler.TaskService.ResolveOriginatorFromTriggerComment(ctx, util.MustParseUUID(fx.WorkspaceID), comment.ID)
	triggers, _ := testHandler.computeCommentAgentTriggers(ctx, issue, comment.Content, nil, "agent", fx.AgentAID,
		commentTriggerComputeOptions{
			ExcludeTriggerCommentID: comment.ID,
			Originator:              scopedInvokeAuthority(uuidToString(originator)),
		})
	for _, trigger := range triggers {
		if uuidToString(trigger.Agent.ID) == fx.AgentBID {
			t.Fatalf("mention trigger admitted target B via the unrelated chat task's originator: %+v", trigger)
		}
	}
}
