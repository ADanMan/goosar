package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateComment_TriggeredTaskRejectsTopLevelComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderTaskFixture(t)

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+fx.IssueID+"/comments", map[string]any{
		"content": "dispatching a squad from this task",
	})
	r = withURLParam(r, "id", fx.IssueID)
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Task-ID", fx.TaskID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateComment top-level: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if got := countAgentCommentsForIssue(t, fx.IssueID, fx.LeaderID); got != 0 {
		t.Fatalf("expected rejected top-level comment not to be stored, got %d", got)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	msg, _ := body["error"].(string)

	for _, want := range []string{
		"top-level comments",
		fx.TriggerCommentID,
		"parent_id (--parent)",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("409 message should contain %q, got %q", want, msg)
		}
	}
}

func TestCreateComment_TriggeredTaskAllowsReplyUnderTrigger(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderTaskFixture(t)

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+fx.IssueID+"/comments", map[string]any{
		"content":   "replying under the trigger comment",
		"parent_id": fx.TriggerCommentID,
	})
	r = withURLParam(r, "id", fx.IssueID)
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Task-ID", fx.TaskID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment reply-under-trigger: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}
