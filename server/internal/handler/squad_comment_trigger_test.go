package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestCommentMentionsAnyone(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "empty", content: "", want: false},
		{name: "plain text", content: "please take a look", want: false},
		{name: "literal at sign only", content: "ping @alice", want: false},
		{name: "agent mention", content: "[@A](mention://agent/11111111-1111-1111-1111-111111111111) handle this", want: true},
		{name: "member mention", content: "[@Bob](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
		{name: "squad mention", content: "[@Squad](mention://squad/44444444-4444-4444-4444-444444444444)", want: true},
		{name: "mention all", content: "[@all](mention://all/all)", want: true},
		{name: "issue mention only", content: "see [MUL-1](mention://issue/33333333-3333-3333-3333-333333333333)", want: false},
		{name: "issue + plain text", content: "see [MUL-1](mention://issue/33333333-3333-3333-3333-333333333333) for context", want: false},
		{name: "agent plus member", content: "[@A](mention://agent/11111111-1111-1111-1111-111111111111) cc [@B](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
		{name: "issue plus member", content: "blocks [MUL-1](mention://issue/33333333-3333-3333-3333-333333333333) — [@Bob](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commentMentionsAnyone(tc.content); got != tc.want {
				t.Fatalf("commentMentionsAnyone(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func shouldEnqueueSquadLeaderOnCommentForTest(ctx context.Context, issue db.Issue, content, authorType, authorID string) bool {
	triggers, _ := testHandler.computeCommentAgentTriggers(ctx, issue, content, nil, authorType, authorID, commentTriggerComputeOptions{})
	return triggersContainIssueAssigneeSquadLeader(triggers)
}

func shouldEnqueueSquadLeaderOnReplyForTest(ctx context.Context, issue db.Issue, content string, parent *db.Comment, authorType, authorID string) bool {
	triggers, _ := testHandler.computeCommentAgentTriggers(ctx, issue, content, parent, authorType, authorID, commentTriggerComputeOptions{})
	return triggersContainIssueAssigneeSquadLeader(triggers)
}

func triggersContainIssueAssigneeSquadLeader(triggers []commentAgentTrigger) bool {
	for _, trigger := range triggers {
		if trigger.Source == commentTriggerSourceIssueAssignee && trigger.Squad != nil {
			return true
		}
	}
	return false
}

type squadCommentTriggerFixture struct {
	Issue    db.Issue
	SquadID  string
	LeaderID string
	OtherID  string
}

func newSquadCommentTriggerFixture(t *testing.T) squadCommentTriggerFixture {
	t.Helper()
	ctx := context.Background()

	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load leader agent: %v", err)
	}

	otherID := createHandlerTestAgent(t, "Squad Comment Other", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Squad Comment Trigger", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id)
		VALUES ($1, 'member', $2, $3, 'squad', $4)
		RETURNING id
	`, testWorkspaceID, testUserID, "squad comment trigger", squadID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	return squadCommentTriggerFixture{
		Issue:    issue,
		SquadID:  squadID,
		LeaderID: leaderID,
		OtherID:  otherID,
	}
}

func TestShouldEnqueueSquadLeaderOnComment_SkipsWhenMemberMentionsAnyone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSquadCommentTriggerFixture(t)
	ctx := context.Background()

	cases := []struct {
		name        string
		content     string
		authorType  string
		authorID    string
		want        bool
		description string
	}{
		{
			name:        "member plain comment triggers leader",
			content:     "what is the latest on this?",
			authorType:  "member",
			authorID:    testUserID,
			want:        true,
			description: "no @ in body → leader must coordinate as today",
		},
		{
			name:        "member issue cross-reference only triggers leader",
			content:     "blocked by [MUL-1](mention://issue/" + testUserID + ")",
			authorType:  "member",
			authorID:    testUserID,
			want:        true,
			description: "issue mentions are not routing — leader still owns dispatch",
		},
		{
			name:        "member mentions another member skips leader",
			content:     "[@self](mention://member/" + testUserID + ") please weigh in",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "user routed at a human — leader stays out (extended rule)",
		},
		{
			name:        "member mentions non-leader agent skips leader",
			content:     "[@Other](mention://agent/" + fx.OtherID + ") please take this",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "user routed at an agent — leader stays out",
		},
		{
			name:        "member mentions leader skips leader on comment path",
			content:     "[@Leader](mention://agent/" + fx.LeaderID + ") your call",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "even @leader is dispatched via the mention path; comment path must not double-enqueue",
		},
		{
			name:        "member mention all skips leader",
			content:     "[@all](mention://all/all) heads up",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "@all is a broadcast — leader does not need to wake to evaluate routing",
		},
		{
			name:        "member mentions a squad skips leader",
			content:     "handing to [@Other Squad](mention://squad/" + fx.SquadID + ")",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "@squad routes the issue to that squad's leader — current leader stays out",
		},
		{
			name:        "agent comment with @agent does not also trigger leader",
			content:     "delegating to [@Other](mention://agent/" + fx.OtherID + ")",
			authorType:  "agent",
			authorID:    fx.OtherID,
			want:        false,
			description: "explicit @agent routes only to the mentioned target; the assigned squad-leader fallback must not also fire (no double-enqueue)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Issue, tc.content, tc.authorType, tc.authorID)
			if got != tc.want {
				t.Fatalf("%s\n  content=%q author=%s/%s\n  got=%v want=%v",
					tc.description, tc.content, tc.authorType, tc.authorID, got, tc.want)
			}
		})
	}
}

func TestShouldEnqueueSquadLeaderOnComment_AgentAuthoredWorkerCommentsWakeLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSquadCommentTriggerFixture(t)
	ctx := context.Background()
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	clearTasks := func() {
		if _, err := testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID); err != nil {
			t.Fatalf("clear tasks: %v", err)
		}
	}

	insertLeaderTask := func(isLeader bool, status string) {
		t.Helper()
		var runtimeID string
		if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
			t.Fatalf("load runtime: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, squad_id)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, fx.LeaderID, runtimeID, issueID, status, isLeader, fx.SquadID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
	}

	t.Run("worker agent comment wakes squad leader", func(t *testing.T) {
		clearTasks()
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Issue, "pushed the fix, PR is up", "agent", fx.OtherID); !got {
			t.Fatalf("worker agent comment: expected leader to wake, got skip")
		}
	})

	t.Run("dual-role worker comment wakes leader when latest task is worker", func(t *testing.T) {
		clearTasks()
		insertLeaderTask(true, "completed")
		insertLeaderTask(false, "completed")
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Issue, "done with my worker slice", "agent", fx.LeaderID); !got {
			t.Fatalf("dual-role worker comment: expected leader to wake, got skip")
		}
	})

	t.Run("leader comment from latest leader task does not self-trigger", func(t *testing.T) {
		clearTasks()
		insertLeaderTask(false, "completed")
		insertLeaderTask(true, "completed")
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Issue, "coordinating next steps", "agent", fx.LeaderID); got {
			t.Fatalf("leader self-trigger: expected skip, got wake")
		}
	})

	t.Run("explicit mention does not double-enqueue assigned leader", func(t *testing.T) {
		clearTasks()
		content := "handing to [@Other](mention://agent/" + fx.OtherID + ")"
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Issue, content, "agent", fx.LeaderID); got {
			t.Fatalf("explicit mention: expected no assigned-leader fallback, got wake")
		}
	})
}

func TestCreateComment_SquadPlainReplyToMemberParentKeepsRootMentionOwner(t *testing.T) {
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

	countQueued := func(agentID string) int {
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
			issueID, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count tasks for %s: %v", agentID, err)
		}
		return n
	}

	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r = withURLParam(r, "id", issueID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode comment: %v", err)
		}
		return resp
	}

	parent := postMemberComment(map[string]any{
		"content": "[@Other](mention://agent/" + fx.OtherID + ") please take this",
	})
	if got := countQueued(fx.LeaderID); got != 0 {
		t.Fatalf("after parent (@OtherAgent): expected 0 leader tasks (skipped), got %d", got)
	}
	if got := countQueued(fx.OtherID); got != 1 {
		t.Fatalf("after parent (@OtherAgent): expected 1 OtherAgent task (mention path), got %d", got)
	}

	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET status = 'completed'
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, fx.OtherID); err != nil {
		t.Fatalf("complete OtherAgent parent task: %v", err)
	}

	postMemberComment(map[string]any{
		"content":   "any update?",
		"parent_id": parent.ID,
	})
	if got := countQueued(fx.LeaderID); got != 0 {
		t.Fatalf("after plain reply: expected 0 leader tasks, got %d", got)
	}
	if got := countQueued(fx.OtherID); got != 1 {
		t.Fatalf("after plain reply: expected 1 OtherAgent task, got %d", got)
	}
}

func TestCreateComment_DualRoleAgentWorkerCommentWakesLeader(t *testing.T) {
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

	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	var workerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, squad_id)
		VALUES ($1, $2, $3, 'running', FALSE, $4)
		RETURNING id
	`, fx.LeaderID, runtimeID, issueID, fx.SquadID).Scan(&workerTaskID); err != nil {
		t.Fatalf("seed worker task: %v", err)
	}

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "done — pushed the change",
	})
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", fx.LeaderID)
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
		t.Fatalf("after worker comment from dual-role agent: expected 1 queued leader task, got %d", leaderTasks)
	}
}

func TestCreateComment_SquadLeaderMentionTaskDoesNotSelfTriggerAssignedFallback(t *testing.T) {
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

	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r = withURLParam(r, "id", issueID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(member): expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode member comment: %v", err)
		}
		return resp
	}
	postAgentComment := func(taskID string, body map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r.Header.Set("X-Actor-Source", "task_token")
		r.Header.Set("X-Agent-ID", fx.LeaderID)
		r.Header.Set("X-Task-ID", taskID)
		r = withURLParam(r, "id", issueID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(agent): expected 201, got %d: %s", w.Code, w.Body.String())
		}
	}
	countQueuedLeaderTasks := func() int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
		`, issueID, fx.LeaderID).Scan(&n); err != nil {
			t.Fatalf("count queued leader tasks: %v", err)
		}
		return n
	}

	trigger := postMemberComment(map[string]any{
		"content": "[@Leader](mention://agent/" + fx.LeaderID + ") can you check this?",
	})

	var mentionTaskID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		  AND is_leader_task = FALSE AND squad_id IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, issueID, fx.LeaderID).Scan(&mentionTaskID); err != nil {
		t.Fatalf("load leader mention task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, mentionTaskID); err != nil {
		t.Fatalf("mark mention task running: %v", err)
	}

	postAgentComment(mentionTaskID, map[string]any{
		"content":   "checked, no action needed",
		"parent_id": trigger.ID,
	})

	if got := countQueuedLeaderTasks(); got != 0 {
		t.Fatalf("leader reply from generic mention task queued %d leader tasks, want 0", got)
	}
}

func TestCreateComment_SquadLeaderThreadParentTaskDoesNotSelfTriggerAssignedFallback(t *testing.T) {
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

	var leaderRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&leaderRuntimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}
	var leaderTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, squad_id)
		VALUES ($1, $2, $3, 'running', TRUE, $4)
		RETURNING id
	`, fx.LeaderID, leaderRuntimeID, issueID, fx.SquadID).Scan(&leaderTaskID); err != nil {
		t.Fatalf("seed leader task: %v", err)
	}

	postAgentComment := func(taskID string, body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r.Header.Set("X-Actor-Source", "task_token")
		r.Header.Set("X-Agent-ID", fx.LeaderID)
		r.Header.Set("X-Task-ID", taskID)
		r = withURLParam(r, "id", issueID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(agent): expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode agent comment: %v", err)
		}
		return resp
	}
	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r = withURLParam(r, "id", issueID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(member): expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode member comment: %v", err)
		}
		return resp
	}
	countQueuedLeaderTasks := func() int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
		`, issueID, fx.LeaderID).Scan(&n); err != nil {
			t.Fatalf("count queued leader tasks: %v", err)
		}
		return n
	}

	parent := postAgentComment(leaderTaskID, map[string]any{
		"content": "coordinating this issue",
	})
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, leaderTaskID); err != nil {
		t.Fatalf("complete leader task: %v", err)
	}

	trigger := postMemberComment(map[string]any{
		"content":   "any update?",
		"parent_id": parent.ID,
	})

	var threadParentTaskID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		  AND is_leader_task = FALSE AND squad_id IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, issueID, fx.LeaderID).Scan(&threadParentTaskID); err != nil {
		t.Fatalf("load leader thread-parent task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, threadParentTaskID); err != nil {
		t.Fatalf("mark thread-parent task running: %v", err)
	}

	postAgentComment(threadParentTaskID, map[string]any{
		"content":   "replying from the thread-parent task",
		"parent_id": trigger.ID,
	})

	if got := countQueuedLeaderTasks(); got != 0 {
		t.Fatalf("leader reply from generic thread-parent task queued %d leader tasks, want 0", got)
	}
}

func TestCreateRetryTask_InheritsIsLeaderTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}

	cases := []struct {
		name     string
		isLeader bool
	}{
		{name: "leader task retry stays leader", isLeader: true},
		{name: "worker task retry stays worker", isLeader: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var parentID string
			if err := testPool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, attempt, max_attempts, is_leader_task)
				VALUES ($1, $2, $3, 'failed', 1, 3, $4)
				RETURNING id
			`, fx.LeaderID, runtimeID, issueID, tc.isLeader).Scan(&parentID); err != nil {
				t.Fatalf("seed parent task: %v", err)
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1 OR parent_task_id = $1`, parentID)
			})

			child, err := testHandler.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: util.MustParseUUID(parentID)})
			if err != nil {
				t.Fatalf("CreateRetryTask: %v", err)
			}
			if child.IsLeaderTask != tc.isLeader {
				t.Fatalf("child.IsLeaderTask = %v, want %v (parent role must be inherited)", child.IsLeaderTask, tc.isLeader)
			}
		})
	}
}

func TestCreateComment_SquadMentionPrivateLeaderBlocksPlainMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, _, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'private leader squad mention test')
		RETURNING id
	`, testWorkspaceID, memberID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Squad](mention://squad/" + squadID + ") please handle",
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		issueID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("private leader got %d queued tasks from plain member squad mention; want 0 (access denied)", count)
	}
}

func TestCreateComment_SquadMentionTriggersLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load leader agent: %v", err)
	}

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Mention Trigger Squad", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'squad mention trigger test')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	countQueued := func(agentID string) int {
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
			issueID, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count tasks for %s: %v", agentID, err)
		}
		return n
	}

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Squad](mention://squad/" + squadID + ") please handle this",
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if got := countQueued(leaderID); got != 1 {
		t.Fatalf("after @squad mention: expected 1 leader task, got %d", got)
	}
}
