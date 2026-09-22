package handler

import (
	"context"
	"strings"
	"testing"
)

func TestAgentTaskQueue_ChatSessionAndIssueAreExclusive(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "Chat Issue Exclusive Agent", nil)
	runtimeID := handlerTestRuntimeID(t)
	issueID := createCommentTriggerPreviewIssue(t, "chat/issue exclusivity", "", "")
	sessionID := seedChatSession(t, agentID)

	insert := func(issueArg, sessionArg any) (string, error) {
		var taskID string
		err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, chat_session_id, started_at)
			VALUES ($1, $2, 'running', 0, $3, $4, now())
			RETURNING id
		`, agentID, runtimeID, issueArg, sessionArg).Scan(&taskID)
		if err == nil {
			cleanupCheckedExec(t, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		}
		return taskID, err
	}

	t.Run("both set is rejected", func(t *testing.T) {
		if _, err := insert(issueID, sessionID); err == nil {
			t.Fatal("insert with BOTH issue_id and chat_session_id succeeded; the CHECK constraint is missing (#134)")
		} else if !strings.Contains(err.Error(), "agent_task_queue_chat_issue_exclusive") {
			t.Fatalf("insert failed for the wrong reason: %v", err)
		}
	})

	t.Run("issue only is accepted", func(t *testing.T) {
		if _, err := insert(issueID, nil); err != nil {
			t.Fatalf("issue-only task must remain legal: %v", err)
		}
	})

	t.Run("chat session only is accepted", func(t *testing.T) {
		if _, err := insert(nil, sessionID); err != nil {
			t.Fatalf("chat-only task must remain legal: %v", err)
		}
	})

	t.Run("neither set is accepted", func(t *testing.T) {

		if _, err := insert(nil, nil); err != nil {
			t.Fatalf("task bound to neither an issue nor a chat must remain legal: %v", err)
		}
	})
}
