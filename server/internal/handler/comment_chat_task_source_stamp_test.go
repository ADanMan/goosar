package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server/internal/util"
)

func seedChatSession(t *testing.T, agentID string) string {
	t.Helper()
	ctx := context.Background()

	var chatSessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	cleanupCheckedExec(t, `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	return chatSessionID
}

func TestCreateComment_ChatTaskStampsSourceTaskIDOnAnyIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Chat Task Commenter", nil)
	chatTaskID := seedRunningChatTask(t, agentID, seedChatSession(t, agentID))

	issueID := createCommentTriggerPreviewIssue(t, "chat task side-effect comment", "", "")

	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": "Fixed it, see the diff."}), "id", issueID)
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", chatTaskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}

	if resp.SourceTaskID != nil {
		t.Errorf("regular agent comment response source_task_id = %s, want nil (redacted, H4)", *resp.SourceTaskID)
	}

	comment, err := testHandler.Queries.GetComment(context.Background(), util.MustParseUUID(resp.ID))
	if err != nil {
		t.Fatalf("reload comment: %v", err)
	}
	if !comment.SourceTaskID.Valid || util.UUIDToString(comment.SourceTaskID) != chatTaskID {
		t.Errorf("persisted source_task_id = %s, want %s", util.UUIDToString(comment.SourceTaskID), chatTaskID)
	}
}

func TestCreateComment_MemberActorDoesNotStampSourceTaskID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Bystander Agent", nil)
	chatTaskID := seedRunningChatTask(t, agentID, seedChatSession(t, agentID))
	issueID := createCommentTriggerPreviewIssue(t, "member comment with stray task header", "", "")

	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": "Just a member comment."}), "id", issueID)

	r.Header.Set("X-Task-ID", chatTaskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if resp.SourceTaskID != nil {
		t.Errorf("member-authored comment source_task_id = %s, want nil", *resp.SourceTaskID)
	}
}

func TestUpdateComment_ChatTaskSelfEditPreservesSourceTaskID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Chat Task Self-Editor", nil)
	chatTaskID := seedRunningChatTask(t, agentID, seedChatSession(t, agentID))
	issueID := createCommentTriggerPreviewIssue(t, "chat task self-edit comment", "", "")

	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": "First draft."}), "id", issueID)
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", chatTaskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created comment: %v", err)
	}

	if createdRow, err := testHandler.Queries.GetComment(context.Background(), util.MustParseUUID(created.ID)); err != nil {
		t.Fatalf("reload created comment: %v", err)
	} else if !createdRow.SourceTaskID.Valid || util.UUIDToString(createdRow.SourceTaskID) != chatTaskID {
		t.Fatalf("precondition failed: created comment source_task_id not stamped to %s", chatTaskID)
	}

	w = httptest.NewRecorder()
	r = withURLParam(newRequest(http.MethodPut, "/api/comments/"+created.ID, map[string]any{"content": "Corrected draft."}), "commentId", created.ID)
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", chatTaskID)
	testHandler.UpdateComment(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateComment: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	comment, err := testHandler.Queries.GetComment(context.Background(), util.MustParseUUID(created.ID))
	if err != nil {
		t.Fatalf("reload comment: %v", err)
	}
	if comment.Content != "Corrected draft." {
		t.Fatalf("content = %q, want the edited text", comment.Content)
	}
	if !comment.SourceTaskID.Valid || util.UUIDToString(comment.SourceTaskID) != chatTaskID {
		t.Errorf("source_task_id after self-edit = %s, want it preserved as %s (not cleared)",
			util.UUIDToString(comment.SourceTaskID), chatTaskID)
	}
}
