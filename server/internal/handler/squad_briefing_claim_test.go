package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func claimAgentInstructionsForTest(t *testing.T, runtimeID string) (taskID string, instructions string, raw string) {
	t.Helper()

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "squad-briefing-claim")
	req = withURLParam(req, "runtimeId", runtimeID)

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *struct {
			ID    string `json:"id"`
			Agent *struct {
				Instructions string `json:"instructions"`
			} `json:"agent"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil {
		return "", "", w.Body.String()
	}
	var instr string
	if resp.Task.Agent != nil {
		instr = resp.Task.Agent.Instructions
	}
	return resp.Task.ID, instr, w.Body.String()
}

type squadBriefingClaimFixture struct {
	RuntimeID string
	AgentID   string
	SquadID   string
	IssueID   string
}

func newSquadBriefingClaimFixture(t *testing.T, ctx context.Context, name string) squadBriefingClaimFixture {
	t.Helper()

	runtimeID := createClaimReclaimRuntime(t, ctx, name+" runtime")

	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name+" leader")

	if _, err := testPool.Exec(ctx, `UPDATE agent SET instructions = '' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("clear leader instructions: %v", err)
	}

	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, agentID); err != nil {
		t.Fatalf("set issue agent assignee: %v", err)
	}

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, name+" squad", agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	return squadBriefingClaimFixture{
		RuntimeID: runtimeID,
		AgentID:   agentID,
		SquadID:   squadID,
		IssueID:   issueID,
	}
}

func enqueueClaimTask(t *testing.T, ctx context.Context, fx squadBriefingClaimFixture, isLeader bool, withSquadID bool) string {
	t.Helper()
	var taskID string
	var squadArg any
	if withSquadID {
		squadArg = fx.SquadID
	} else {
		squadArg = nil
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, is_leader_task, squad_id)
		VALUES ($1, $2, $3, 'queued', 0, $4, $5)
		RETURNING id
	`, fx.AgentID, fx.RuntimeID, fx.IssueID, isLeader, squadArg).Scan(&taskID); err != nil {
		t.Fatalf("enqueue claim task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

func TestClaim_LeaderTaskFromCommentMention_InjectsBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadBriefingClaimFixture(t, ctx, "Briefing inject")
	want := enqueueClaimTask(t, ctx, fx, true, true)

	got, instr, raw := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if got != want {
		t.Fatalf("claimed task id = %q, want %q: %s", got, want, raw)
	}
	if !strings.Contains(instr, "## Squad Operating Protocol") || !strings.Contains(instr, "## Squad Roster") {
		t.Fatalf("expected squad-leader briefing in agent instructions, got:\n%s", instr)
	}
}

func TestClaim_NonLeaderTask_NoBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadBriefingClaimFixture(t, ctx, "Briefing nonleader")
	enqueueClaimTask(t, ctx, fx, false, true)

	_, instr, _ := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if strings.Contains(instr, "## Squad Operating Protocol") || strings.Contains(instr, "## Squad Roster") {
		t.Fatalf("non-leader task must NOT get squad briefing, got:\n%s", instr)
	}
}

func TestClaim_LeaderTaskWithDanglingSquadID_NoBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadBriefingClaimFixture(t, ctx, "Briefing dangling")
	want := enqueueClaimTask(t, ctx, fx, true, true)

	if _, err := testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, fx.SquadID); err != nil {
		t.Fatalf("delete squad: %v", err)
	}

	var stillSet bool
	if err := testPool.QueryRow(ctx,
		`SELECT squad_id = $2 FROM agent_task_queue WHERE id = $1`, want, fx.SquadID,
	).Scan(&stillSet); err != nil {
		t.Fatalf("reload task squad_id: %v", err)
	}
	if !stillSet {
		t.Fatalf("expected task.squad_id to remain the dangling UUID after squad delete (no FK)")
	}

	got, instr, raw := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if got != want {
		t.Fatalf("claimed task id = %q, want %q (claim must still succeed 200): %s", got, want, raw)
	}
	if strings.Contains(instr, "## Squad Operating Protocol") || strings.Contains(instr, "## Squad Roster") {
		t.Fatalf("dangling squad_id must NOT get squad briefing, got:\n%s", instr)
	}
}

func TestClaim_LeaderTaskWithoutSquadID_NoBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadBriefingClaimFixture(t, ctx, "Briefing nullsquad")
	enqueueClaimTask(t, ctx, fx, true, false)

	_, instr, _ := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if strings.Contains(instr, "## Squad Operating Protocol") || strings.Contains(instr, "## Squad Roster") {
		t.Fatalf("leader task with NULL squad_id must NOT get squad briefing, got:\n%s", instr)
	}
}
