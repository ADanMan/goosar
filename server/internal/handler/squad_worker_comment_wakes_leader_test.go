package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCreateComment_WorkerAgentCommentWakesSquadLeader_MUL4015(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})

	var workerRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.OtherID).Scan(&workerRuntimeID); err != nil {
		t.Fatalf("load worker runtime: %v", err)
	}
	var workerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, 'running', FALSE, $4, $4)
		RETURNING id
	`, fx.OtherID, workerRuntimeID, issueID, testUserID).Scan(&workerTaskID); err != nil {
		t.Fatalf("seed worker task: %v", err)
	}

	var leaderRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&leaderRuntimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, 'completed', TRUE, $4, $4)
	`, fx.LeaderID, leaderRuntimeID, issueID, testUserID); err != nil {
		t.Fatalf("seed leader task: %v", err)
	}

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "done — pushed the change, PR is up",
	})
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", fx.OtherID)
	r.Header.Set("X-Task-ID", workerTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var leaderTasks int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
	`, issueID, fx.LeaderID).Scan(&leaderTasks); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderTasks != 1 {
		t.Fatalf("after worker comment: expected 1 queued leader task for L, got %d", leaderTasks)
	}
}

func TestCreateComment_WorkerAgentCommentDoesNotWakeLeader_WhenLeaderTaskPending(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})

	var workerRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.OtherID).Scan(&workerRuntimeID); err != nil {
		t.Fatalf("load worker runtime: %v", err)
	}
	var workerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, 'running', FALSE, $4, $4)
		RETURNING id
	`, fx.OtherID, workerRuntimeID, issueID, testUserID).Scan(&workerTaskID); err != nil {
		t.Fatalf("seed worker task: %v", err)
	}

	var leaderRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&leaderRuntimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, 'queued', TRUE, $4, $4)
	`, fx.LeaderID, leaderRuntimeID, issueID, testUserID); err != nil {
		t.Fatalf("seed queued leader task: %v", err)
	}

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "done",
	})
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", fx.OtherID)
	r.Header.Set("X-Task-ID", workerTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var leaderTasks int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
	`, issueID, fx.LeaderID).Scan(&leaderTasks); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderTasks != 1 {
		t.Fatalf("expected 1 queued leader task (dedup), got %d", leaderTasks)
	}
}

func TestCreateComment_WorkerAgentCommentWakesPrivateSquadLeader_MUL4015(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	privateAgent := func(name string) string {
		var agentID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent (
				workspace_id, name, description, runtime_mode, runtime_config,
				runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
				instructions, custom_env, custom_args, mcp_config
			)
			VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb)
			RETURNING id
		`, testWorkspaceID, name, handlerTestRuntimeID(t), testUserID).Scan(&agentID); err != nil {
			t.Fatalf("failed to create private agent %q: %v", name, err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
		})
		return agentID
	}

	leaderID := privateAgent("MUL-4015 Private Leader")
	workerID := privateAgent("MUL-4015 Private Worker")

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'MUL-4015 Private Squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create private squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id)
		VALUES ($1, 'member', $2, 'private squad worker-comment MUL-4015', 'squad', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, squadID).Scan(&issueID); err != nil {
		t.Fatalf("create private squad issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var leaderRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, leaderID).Scan(&leaderRuntimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}
	var leaderTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, originator_user_id, accountable_user_id, squad_id)
		VALUES ($1, $2, $3, 'running', TRUE, $4, $4, $5)
		RETURNING id
	`, leaderID, leaderRuntimeID, issueID, testUserID, squadID).Scan(&leaderTaskID); err != nil {
		t.Fatalf("seed leader task: %v", err)
	}

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Worker](mention://agent/" + workerID + ") please handle",
	})
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", leaderID)
	r.Header.Set("X-Task-ID", leaderTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("leader mention CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var workerTaskID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, workerID).Scan(&workerTaskID); err != nil {
		t.Fatalf("worker task not enqueued from leader mention: %v", err)
	}

	var workerOriginator pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT originator_user_id FROM agent_task_queue WHERE id = $1`, workerTaskID).Scan(&workerOriginator); err != nil {
		t.Fatalf("read worker originator: %v", err)
	}
	if !workerOriginator.Valid || uuidToString(workerOriginator) != testUserID {
		t.Fatalf("worker task originator = %v (valid=%v), want %s — originator inheritance broken (comment.source_task_id not stamped)",
			uuidToString(workerOriginator), workerOriginator.Valid, testUserID)
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, workerTaskID); err != nil {
		t.Fatalf("advance worker task to running: %v", err)
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, leaderTaskID); err != nil {
		t.Fatalf("complete leader task: %v", err)
	}

	var workerTriggerCommentID string
	if err := testPool.QueryRow(ctx, `SELECT trigger_comment_id FROM agent_task_queue WHERE id = $1`, workerTaskID).Scan(&workerTriggerCommentID); err != nil {
		t.Fatalf("read worker trigger_comment_id: %v", err)
	}
	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content":   "done — pushed the change",
		"parent_id": workerTriggerCommentID,
	})
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", workerID)
	r.Header.Set("X-Task-ID", workerTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("worker done CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var leaderTasksQueued int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
	`, issueID, leaderID).Scan(&leaderTasksQueued); err != nil {
		t.Fatalf("count queued leader tasks: %v", err)
	}
	if leaderTasksQueued != 1 {
		t.Fatalf("after worker done: expected 1 queued leader task for private L, got %d — leader→worker→leader loop broken for private leader (MUL-4015)",
			leaderTasksQueued)
	}
}
