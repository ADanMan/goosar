package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server/internal/middleware"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func chatPendingCtxAs(t *testing.T, req *http.Request, userID string) *http.Request {
	t.Helper()
	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(userID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member row for %s: %v", userID, err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
}

func insertChatSessionAs(t *testing.T, agentID, creatorID string) string {
	t.Helper()
	var sessionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		VALUES ($1, $2, $3, 'pending-tasks-test', 'active')
		RETURNING id
	`, testWorkspaceID, agentID, creatorID).Scan(&sessionID); err != nil {
		t.Fatalf("insert chat session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})
	return sessionID
}

func insertPendingChatTask(t *testing.T, agentID, sessionID, status string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, chat_session_id)
		VALUES ($1, $2, $3, 0, $4)
		RETURNING id
	`, agentID, handlerTestRuntimeID(t), status, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("insert pending chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func decodePendingTasks(t *testing.T, w *httptest.ResponseRecorder) PendingChatTasksResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp PendingChatTasksResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode pending tasks: %v", err)
	}
	return resp
}

func decodeHasPending(t *testing.T, w *httptest.ResponseRecorder) bool {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp HasPendingChatTasksResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode has-any: %v", err)
	}
	return resp.HasPending
}

func containsPendingTask(tasks []PendingChatTaskItem, taskID string) bool {
	for _, it := range tasks {
		if it.TaskID == taskID {
			return true
		}
	}
	return false
}

func TestListPendingChatTasks_HidesPrivateAgentFromLostAccessCreator(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateAgentID, _, memberID := privateAgentTestFixture(t)
	publicAgentID := createHandlerTestAgent(t, "PendingPublicAgent", []byte("[]"))

	privateSession := insertChatSessionAs(t, privateAgentID, memberID)
	publicSession := insertChatSessionAs(t, publicAgentID, memberID)
	privateTask := insertPendingChatTask(t, privateAgentID, privateSession, "running")
	publicTask := insertPendingChatTask(t, publicAgentID, publicSession, "queued")

	w := httptest.NewRecorder()
	testHandler.ListPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(memberID, "GET", "/api/chat/pending-tasks", nil), memberID))
	resp := decodePendingTasks(t, w)

	if containsPendingTask(resp.Tasks, privateTask) {
		t.Fatalf("private-agent task %s leaked to plain member: %+v", privateTask, resp.Tasks)
	}
	if !containsPendingTask(resp.Tasks, publicTask) {
		t.Fatalf("public-agent task %s missing from plain member's pending list: %+v", publicTask, resp.Tasks)
	}
}

func TestListPendingChatTasks_OwnerSeesPrivateAgentTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateAgentID, ownerID, _ := privateAgentTestFixture(t)
	session := insertChatSessionAs(t, privateAgentID, ownerID)
	task := insertPendingChatTask(t, privateAgentID, session, "running")

	w := httptest.NewRecorder()
	testHandler.ListPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(ownerID, "GET", "/api/chat/pending-tasks", nil), ownerID))
	resp := decodePendingTasks(t, w)

	if !containsPendingTask(resp.Tasks, task) {
		t.Fatalf("agent owner did not see their own private-agent task %s: %+v", task, resp.Tasks)
	}
}

func TestHasPendingChatTasks_FalseWhenOnlyInaccessiblePrivateAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateAgentID, _, memberID := privateAgentTestFixture(t)
	session := insertChatSessionAs(t, privateAgentID, memberID)
	insertPendingChatTask(t, privateAgentID, session, "running")

	w := httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(memberID, "GET", "/api/chat/pending-tasks/has-any", nil), memberID))
	if decodeHasPending(t, w) {
		t.Fatalf("has-any returned true for a task on a private agent the member cannot access")
	}
}

func TestHasPendingChatTasks_TrueWhenAccessiblePublicAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateAgentID, _, memberID := privateAgentTestFixture(t)
	publicAgentID := createHandlerTestAgent(t, "PendingPublicAgentHasAny", []byte("[]"))

	privateSession := insertChatSessionAs(t, privateAgentID, memberID)
	publicSession := insertChatSessionAs(t, publicAgentID, memberID)
	insertPendingChatTask(t, privateAgentID, privateSession, "running")
	insertPendingChatTask(t, publicAgentID, publicSession, "waiting_local_directory")

	w := httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(memberID, "GET", "/api/chat/pending-tasks/has-any", nil), memberID))
	if !decodeHasPending(t, w) {
		t.Fatalf("has-any returned false despite an in-flight task on an accessible public agent")
	}
}

func TestHasPendingChatTasks_OwnerOfPrivateAgentSeesTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateAgentID, ownerID, _ := privateAgentTestFixture(t)
	session := insertChatSessionAs(t, privateAgentID, ownerID)
	insertPendingChatTask(t, privateAgentID, session, "dispatched")

	w := httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(ownerID, "GET", "/api/chat/pending-tasks/has-any", nil), ownerID))
	if !decodeHasPending(t, w) {
		t.Fatalf("has-any returned false for the private agent's own owner")
	}
}

func TestHasPendingChatTasks_IgnoresTerminalTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	_, _, memberID := privateAgentTestFixture(t)
	publicAgentID := createHandlerTestAgent(t, "PendingTerminalAgent", []byte("[]"))
	session := insertChatSessionAs(t, publicAgentID, memberID)
	insertPendingChatTask(t, publicAgentID, session, "completed")

	w := httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(memberID, "GET", "/api/chat/pending-tasks/has-any", nil), memberID))
	if decodeHasPending(t, w) {
		t.Fatalf("has-any returned true for a terminal (completed) task")
	}
}

func TestHasPendingChatTasks_HidesOtherCreatorsTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	_, creatorA, otherB := privateAgentTestFixture(t)
	publicAgentID := createHandlerTestAgent(t, "PendingCrossCreatorAgent", []byte("[]"))
	session := insertChatSessionAs(t, publicAgentID, creatorA)
	insertPendingChatTask(t, publicAgentID, session, "running")

	w := httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(otherB, "GET", "/api/chat/pending-tasks/has-any", nil), otherB))
	if decodeHasPending(t, w) {
		t.Fatalf("has-any leaked user A's task to user B (cs.creator_id gate not enforced)")
	}

	w = httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(creatorA, "GET", "/api/chat/pending-tasks/has-any", nil), creatorA))
	if !decodeHasPending(t, w) {
		t.Fatalf("has-any returned false for the task's own creator")
	}

	w = httptest.NewRecorder()
	testHandler.ListPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(otherB, "GET", "/api/chat/pending-tasks", nil), otherB))
	if resp := decodePendingTasks(t, w); len(resp.Tasks) != 0 {
		t.Fatalf("list leaked user A's task to user B: %+v", resp.Tasks)
	}
}
