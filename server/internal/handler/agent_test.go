package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestListWorkspaceAgentTaskSnapshot(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	agentA := createHandlerTestAgent(t, "snapshot-agent-a", []byte(`{}`))
	agentB := createHandlerTestAgent(t, "snapshot-agent-b", []byte(`{}`))
	agentC := createHandlerTestAgent(t, "snapshot-agent-c", []byte(`{}`))

	type taskFixture struct {
		agentID     string
		status      string
		completedAt string
		label       string
	}
	fixtures := []taskFixture{

		{agentA, "queued", "", "A.queued"},
		{agentA, "dispatched", "", "A.dispatched"},
		{agentA, "running", "", "A.running"},
		{agentA, "failed", "now() - interval '10 minutes'", "A.old_failed"},
		{agentA, "completed", "now() - interval '30 seconds'", "A.latest_completed"},

		{agentB, "failed", "now() - interval '5 minutes'", "B.stale_failed_kept"},

		{agentC, "failed", "now() - interval '5 minutes'", "C.failure"},
		{agentC, "cancelled", "now() - interval '30 seconds'", "C.newer_cancelled_must_be_ignored"},
	}

	insertedIDs := make([]string, 0, len(fixtures))
	for _, f := range fixtures {
		var id string
		var query string
		if f.completedAt == "" {
			query = `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
			         VALUES ($1, $2, $3, 0) RETURNING id`
		} else {
			query = `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, completed_at)
			         VALUES ($1, $2, $3, 0, ` + f.completedAt + `) RETURNING id`
		}
		if err := testPool.QueryRow(ctx, query, f.agentID, testRuntimeID, f.status).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", f.label, err)
		}
		insertedIDs = append(insertedIDs, id)
	}
	t.Cleanup(func() {
		for _, id := range insertedIDs {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, id)
		}
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/agent-task-snapshot", nil)
	testHandler.ListWorkspaceAgentTaskSnapshot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaceAgentTaskSnapshot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var tasks []AgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	type key struct{ agent, status string }
	counts := map[key]int{}
	for _, task := range tasks {
		if task.AgentID != agentA && task.AgentID != agentB && task.AgentID != agentC {
			continue
		}
		counts[key{task.AgentID, task.Status}]++
	}

	wantCounts := map[key]int{

		{agentA, "queued"}:     1,
		{agentA, "dispatched"}: 1,
		{agentA, "running"}:    1,
		{agentA, "completed"}:  1,

		{agentB, "failed"}: 1,

		{agentC, "failed"}: 1,
	}
	for k, expected := range wantCounts {
		if got := counts[k]; got != expected {
			t.Errorf("agent=%s status=%s: expected %d, got %d", k.agent, k.status, expected, got)
		}
	}

	if counts[key{agentA, "failed"}] != 0 {
		t.Errorf("agent A old failed must be superseded by newer completed; got %d", counts[key{agentA, "failed"}])
	}

	for _, agentID := range []string{agentA, agentB, agentC} {
		if counts[key{agentID, "cancelled"}] != 0 {
			t.Errorf("agent %s: cancelled rows must be excluded from snapshot; got %d",
				agentID, counts[key{agentID, "cancelled"}])
		}
	}
}

func TestListWorkspaceWorkingAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	workingAgentID := createHandlerTestAgent(t, "working-agents-running", []byte(`{}`))
	queuedAgentID := createHandlerTestAgent(t, "working-agents-queued", []byte(`{}`))

	var outsiderUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Working Agents Outsider', 'working-agents-outsider-' || gen_random_uuid()::text || '@goosar.test')
		RETURNING id
	`).Scan(&outsiderUserID); err != nil {
		t.Fatalf("insert outsider user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, outsiderUserID)
	})

	var outsiderAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES (
			$1, 'working-agents-outsider', '', 'cloud', '{}'::jsonb,
			$2, 'workspace', 'private', 1, $3,
			'', '{}'::jsonb, '[]'::jsonb, '{}'::jsonb
		)
		RETURNING id
	`, testWorkspaceID, testRuntimeID, outsiderUserID).Scan(&outsiderAgentID); err != nil {
		t.Fatalf("insert outsider agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, outsiderAgentID)
	})

	insertedSquadIDs := make([]string, 0, 3)
	insertSquad := func(name, leaderID string) string {
		t.Helper()
		var squadID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO squad (
				workspace_id, name, description, leader_id, creator_id
			)
			VALUES ($1, $2, '', $3, $4)
			RETURNING id
		`,
			testWorkspaceID,
			name,
			leaderID,
			testUserID,
		).Scan(&squadID); err != nil {
			t.Fatalf("insert squad %q: %v", name, err)
		}
		insertedSquadIDs = append(insertedSquadIDs, squadID)
		return squadID
	}
	directMemberSquadID := insertSquad(
		"working-agents-direct-member-squad",
		outsiderAgentID,
	)
	ownedLeaderSquadID := insertSquad(
		"working-agents-owned-leader-squad",
		workingAgentID,
	)
	ownedMemberSquadID := insertSquad(
		"working-agents-owned-member-squad",
		outsiderAgentID,
	)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad_member (squad_id, member_type, member_id)
		VALUES
			($1, 'member', $2),
			($3, 'agent', $4)
	`,
		directMemberSquadID,
		testUserID,
		ownedMemberSquadID,
		workingAgentID,
	); err != nil {
		t.Fatalf("insert squad involvement fixtures: %v", err)
	}
	t.Cleanup(func() {
		for _, squadID := range insertedSquadIDs {
			testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID)
		}
	})

	insertedIssueIDs := make([]string, 0, 6)
	insertIssue := func(
		title, creatorType, creatorID string,
		assigneeType, assigneeID any,
	) string {
		t.Helper()
		var issueID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, number, title, status, priority,
				assignee_type, assignee_id, creator_type, creator_id
			)
			SELECT $1, COALESCE(MIN(number), 0) - 1, $2, 'todo', 'none',
			       $3, $4, $5, $6
			FROM issue
			WHERE workspace_id = $1
			RETURNING id
		`,
			testWorkspaceID,
			title,
			assigneeType,
			assigneeID,
			creatorType,
			creatorID,
		).Scan(&issueID); err != nil {
			t.Fatalf("insert source issue %q: %v", title, err)
		}
		insertedIssueIDs = append(insertedIssueIDs, issueID)
		return issueID
	}
	assignedIssueID := insertIssue(
		"working-agent-assigned-to-me",
		"member",
		testUserID,
		"member",
		testUserID,
	)
	ownedAgentIssueID := insertIssue(
		"working-agent-owned-agent",
		"agent",
		outsiderAgentID,
		"agent",
		workingAgentID,
	)
	outsideIssueID := insertIssue(
		"working-agent-outside-mine",
		"agent",
		outsiderAgentID,
		nil,
		nil,
	)
	directMemberSquadIssueID := insertIssue(
		"working-agent-direct-member-squad",
		"agent",
		outsiderAgentID,
		"squad",
		directMemberSquadID,
	)
	ownedLeaderSquadIssueID := insertIssue(
		"working-agent-owned-leader-squad",
		"agent",
		outsiderAgentID,
		"squad",
		ownedLeaderSquadID,
	)
	ownedMemberSquadIssueID := insertIssue(
		"working-agent-owned-member-squad",
		"agent",
		outsiderAgentID,
		"squad",
		ownedMemberSquadID,
	)
	t.Cleanup(func() {
		for _, issueID := range insertedIssueIDs {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		}
	})
	chatSessionID := createHandlerTestChatSession(t, workingAgentID)
	autopilotID := insertListTestAutopilot(t, workingAgentID, "working-agent-source-filter")
	var autopilotRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status)
		VALUES ($1, 'manual', 'running')
		RETURNING id
	`, autopilotID).Scan(&autopilotRunID); err != nil {
		t.Fatalf("insert autopilot run: %v", err)
	}

	insertedTaskIDs := make([]string, 0, 10)
	for _, fixture := range []struct {
		agentID        string
		status         string
		issueID        any
		chatSessionID  any
		autopilotRunID any
	}{
		{workingAgentID, "running", assignedIssueID, nil, nil},
		{workingAgentID, "running", ownedAgentIssueID, nil, nil},
		{workingAgentID, "running", outsideIssueID, nil, nil},
		{workingAgentID, "running", directMemberSquadIssueID, nil, nil},
		{workingAgentID, "running", ownedLeaderSquadIssueID, nil, nil},
		{workingAgentID, "running", ownedMemberSquadIssueID, nil, nil},
		{workingAgentID, "running", nil, chatSessionID, nil},
		{workingAgentID, "running", assignedIssueID, nil, autopilotRunID},
		{workingAgentID, "running", nil, nil, nil},
		{queuedAgentID, "queued", nil, nil, nil},
	} {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, status, priority, issue_id,
				chat_session_id, autopilot_run_id, started_at
			)
			VALUES ($1, $2, $3, 0, $4, $5, $6, now())
			RETURNING id
		`,
			fixture.agentID,
			testRuntimeID,
			fixture.status,
			fixture.issueID,
			fixture.chatSessionID,
			fixture.autopilotRunID,
		).Scan(&taskID); err != nil {
			t.Fatalf("insert %s task: %v", fixture.status, err)
		}
		insertedTaskIDs = append(insertedTaskIDs, taskID)
	}
	t.Cleanup(func() {
		for _, taskID := range insertedTaskIDs {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		}
	})

	for _, tc := range []struct {
		name         string
		query        string
		wantCount    int32
		wantIssueIDs []string
	}{
		{
			name:      "all sources",
			wantCount: 9,
			wantIssueIDs: []string{
				assignedIssueID,
				ownedAgentIssueID,
				outsideIssueID,
				directMemberSquadIssueID,
				ownedLeaderSquadIssueID,
				ownedMemberSquadIssueID,
			},
		},
		{
			name:      "issue",
			query:     "?type=issue",
			wantCount: 6,
			wantIssueIDs: []string{
				assignedIssueID,
				ownedAgentIssueID,
				outsideIssueID,
				directMemberSquadIssueID,
				ownedLeaderSquadIssueID,
				ownedMemberSquadIssueID,
			},
		},
		{
			name:         "autopilot",
			query:        "?type=autopilot",
			wantCount:    1,
			wantIssueIDs: []string{assignedIssueID},
		},
		{name: "chat", query: "?type=chat", wantCount: 1, wantIssueIDs: []string{}},
		{
			name:      "mine defaults to any",
			query:     "?type=issue&scope=mine",
			wantCount: 5,
			wantIssueIDs: []string{
				assignedIssueID,
				ownedAgentIssueID,
				directMemberSquadIssueID,
				ownedLeaderSquadIssueID,
				ownedMemberSquadIssueID,
			},
		},
		{
			name:      "mine any",
			query:     "?type=issue&scope=mine&relation=any",
			wantCount: 5,
			wantIssueIDs: []string{
				assignedIssueID,
				ownedAgentIssueID,
				directMemberSquadIssueID,
				ownedLeaderSquadIssueID,
				ownedMemberSquadIssueID,
			},
		},
		{
			name:         "mine assigned",
			query:        "?type=issue&scope=mine&relation=assigned",
			wantCount:    1,
			wantIssueIDs: []string{assignedIssueID},
		},
		{
			name:         "mine created",
			query:        "?type=issue&scope=mine&relation=created",
			wantCount:    1,
			wantIssueIDs: []string{assignedIssueID},
		},
		{
			name:      "mine involved",
			query:     "?type=issue&scope=mine&relation=involved",
			wantCount: 4,
			wantIssueIDs: []string{
				ownedAgentIssueID,
				directMemberSquadIssueID,
				ownedLeaderSquadIssueID,
				ownedMemberSquadIssueID,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testHandler.ListWorkspaceWorkingAgents(
				w,
				newRequest(http.MethodGet, "/api/working-agents"+tc.query, nil),
			)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}

			var agents []WorkspaceWorkingAgent
			if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			var working *WorkspaceWorkingAgent
			for i := range agents {
				switch agents[i].ID {
				case workingAgentID:
					working = &agents[i]
				case queuedAgentID:
					t.Errorf("queued-only agent must not be returned")
				}
			}
			if working == nil {
				t.Fatalf("running agent %s was not returned", workingAgentID)
			}
			if working.Name != "working-agents-running" {
				t.Errorf("name = %q, want %q", working.Name, "working-agents-running")
			}
			if working.RunningTaskCount != tc.wantCount {
				t.Errorf("running_task_count = %d, want %d", working.RunningTaskCount, tc.wantCount)
			}
			if len(working.IssueIDs) != len(tc.wantIssueIDs) {
				t.Fatalf("issue_ids = %v, want %v", working.IssueIDs, tc.wantIssueIDs)
			}
			wantIssueIDs := make(map[string]struct{}, len(tc.wantIssueIDs))
			for _, issueID := range tc.wantIssueIDs {
				wantIssueIDs[issueID] = struct{}{}
			}
			for _, issueID := range working.IssueIDs {
				if _, ok := wantIssueIDs[issueID]; !ok {
					t.Errorf("unexpected issue_id %s; want %v", issueID, tc.wantIssueIDs)
				}
			}
		})
	}
}

func TestListWorkspaceWorkingAgentsRejectsInvalidFilters(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	for _, path := range []string{
		"/api/working-agents?type=quick_create",
		"/api/working-agents?scope=mine",
		"/api/working-agents?type=chat&scope=mine",
		"/api/working-agents?type=issue&scope=workspace",
		"/api/working-agents?type=issue&relation=assigned",
		"/api/working-agents?type=issue&scope=mine&relation=watching",
		"/api/working-agents?type=issue&parent=not-a-uuid",
		"/api/working-agents?type=chat&parent=00000000-0000-0000-0000-000000000000",
		"/api/working-agents?type=issue&scope=mine&parent=00000000-0000-0000-0000-000000000000",
	} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			testHandler.ListWorkspaceWorkingAgents(
				w,
				newRequest(http.MethodGet, path, nil),
			)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestListWorkspaceWorkingAgentsParentScope(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	childAgentID := createHandlerTestAgent(t, "working-agents-parent-child", []byte(`{}`))
	siblingAgentID := createHandlerTestAgent(t, "working-agents-parent-sibling", []byte(`{}`))

	insertedIssueIDs := make([]string, 0, 4)
	insertIssue := func(title string, parentIssueID any) string {
		t.Helper()
		var issueID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, number, title, status, priority,
				creator_type, creator_id, parent_issue_id
			)
			SELECT $1, COALESCE(MIN(number), 0) - 1, $2, 'todo', 'none',
			       'member', $3, $4
			FROM issue
			WHERE workspace_id = $1
			RETURNING id
		`, testWorkspaceID, title, testUserID, parentIssueID).Scan(&issueID); err != nil {
			t.Fatalf("insert issue %q: %v", title, err)
		}
		insertedIssueIDs = append(insertedIssueIDs, issueID)
		return issueID
	}
	parentIssueID := insertIssue("working-agents-parent", nil)
	firstChildID := insertIssue("working-agents-parent-first-child", parentIssueID)
	secondChildID := insertIssue("working-agents-parent-second-child", parentIssueID)

	unrelatedIssueID := insertIssue("working-agents-parent-unrelated", nil)
	t.Cleanup(func() {
		for _, issueID := range insertedIssueIDs {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		}
	})

	insertedTaskIDs := make([]string, 0, 3)
	for _, fixture := range []struct {
		agentID string
		issueID string
	}{
		{childAgentID, firstChildID},
		{siblingAgentID, secondChildID},
		{childAgentID, unrelatedIssueID},
	} {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, status, priority, issue_id, started_at
			)
			VALUES ($1, $2, 'running', 0, $3, now())
			RETURNING id
		`, fixture.agentID, testRuntimeID, fixture.issueID).Scan(&taskID); err != nil {
			t.Fatalf("insert running task: %v", err)
		}
		insertedTaskIDs = append(insertedTaskIDs, taskID)
	}
	t.Cleanup(func() {
		for _, taskID := range insertedTaskIDs {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		}
	})

	read := func(t *testing.T, query string) map[string]WorkspaceWorkingAgent {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.ListWorkspaceWorkingAgents(
			w,
			newRequest(http.MethodGet, "/api/working-agents"+query, nil),
		)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var agents []WorkspaceWorkingAgent
		if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		byID := make(map[string]WorkspaceWorkingAgent, len(agents))
		for _, agent := range agents {
			byID[agent.ID] = agent
		}
		return byID
	}

	t.Run("narrows to the parent's direct children", func(t *testing.T) {
		byID := read(t, "?type=issue&parent="+parentIssueID)

		child, ok := byID[childAgentID]
		if !ok {
			t.Fatalf("agent running on a child issue was not returned")
		}
		if child.RunningTaskCount != 1 {
			t.Errorf("running_task_count = %d, want 1", child.RunningTaskCount)
		}
		if len(child.IssueIDs) != 1 || child.IssueIDs[0] != firstChildID {
			t.Errorf("issue_ids = %v, want [%s]", child.IssueIDs, firstChildID)
		}
		if _, ok := byID[siblingAgentID]; !ok {
			t.Errorf("agent running on the second child was not returned")
		}
	})

	t.Run("an unrelated issue's parent yields nothing", func(t *testing.T) {
		byID := read(t, "?type=issue&parent="+unrelatedIssueID)

		if _, ok := byID[childAgentID]; ok {
			t.Errorf("childless issue must not return the child agent")
		}
		if _, ok := byID[siblingAgentID]; ok {
			t.Errorf("childless issue must not return the sibling agent")
		}
	})

	t.Run("omitting parent keeps the workspace projection", func(t *testing.T) {
		byID := read(t, "?type=issue")

		child, ok := byID[childAgentID]
		if !ok {
			t.Fatalf("agent was not returned by the unnarrowed read")
		}
		if child.RunningTaskCount != 2 {
			t.Errorf("running_task_count = %d, want 2", child.RunningTaskCount)
		}
		seen := make(map[string]struct{}, len(child.IssueIDs))
		for _, issueID := range child.IssueIDs {
			seen[issueID] = struct{}{}
		}
		for _, want := range []string{firstChildID, unrelatedIssueID} {
			if _, ok := seen[want]; !ok {
				t.Errorf("issue_ids = %v, missing %s", child.IssueIDs, want)
			}
		}
	})
}

func TestCreateAgent_RejectsDuplicateName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "duplicate-name-test-agent",
		)
	})

	body := map[string]any{
		"name":                 "duplicate-name-test-agent",
		"description":          "first description",
		"runtime_id":           testRuntimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}

	w1 := httptest.NewRecorder()
	testHandler.CreateAgent(w1, newRequest(http.MethodPost, "/api/agents", body))
	if w1.Code != http.StatusCreated {
		t.Fatalf("first CreateAgent: expected 201, got %d: %s", w1.Code, w1.Body.String())
	}
	var resp1 map[string]any
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	agentID1, _ := resp1["id"].(string)
	if agentID1 == "" {
		t.Fatalf("first CreateAgent: no id in response: %v", resp1)
	}

	body["description"] = "updated description"
	w2 := httptest.NewRecorder()
	testHandler.CreateAgent(w2, newRequest(http.MethodPost, "/api/agents", body))
	if w2.Code != http.StatusConflict {
		t.Fatalf("second CreateAgent with duplicate name: expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestUpdateAgent_RejectsRenameToArchivedName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const heldName = "rename-collision-archived-name"
	idA := createHandlerTestAgent(t, heldName, nil)
	archiveReq := withURLParam(newRequest(http.MethodPost, "/api/agents/"+idA+"/archive", nil), "id", idA)
	archiveW := httptest.NewRecorder()
	testHandler.ArchiveAgent(archiveW, archiveReq)
	if archiveW.Code != http.StatusOK {
		t.Fatalf("ArchiveAgent: expected 200, got %d: %s", archiveW.Code, archiveW.Body.String())
	}

	idB := createHandlerTestAgent(t, "rename-collision-source", nil)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(
		newRequest(http.MethodPatch, "/api/agents/"+idB, map[string]any{"name": heldName}), "id", idB))

	if w.Code != http.StatusConflict {
		t.Fatalf("rename to archived agent's name: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "agent_workspace_name_unique") {
		t.Fatalf("409 body leaked the raw constraint name: %s", w.Body.String())
	}
}

func TestCreateAgent_AssignsAvatarDefault(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name       string
		avatarURL  *string
		wantAvatar string
		wantEmoji  bool
	}{
		{name: "omitted", wantEmoji: true},
		{name: "empty", avatarURL: ptr(""), wantEmoji: true},
		{
			name:       "explicit",
			avatarURL:  ptr("https://cdn.example.com/avatars/agent.png"),
			wantAvatar: "https://cdn.example.com/avatars/agent.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentName := "avatar-default-test-" + tt.name
			t.Cleanup(func() {
				testPool.Exec(context.Background(),
					`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
					testWorkspaceID, agentName,
				)
			})

			body := map[string]any{
				"name":       agentName,
				"runtime_id": testRuntimeID,
			}
			if tt.avatarURL != nil {
				body["avatar_url"] = *tt.avatarURL
			}

			w := httptest.NewRecorder()
			testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
			if w.Code != http.StatusCreated {
				t.Fatalf("CreateAgent: expected 201, got %d: %s", w.Code, w.Body.String())
			}

			var response AgentResponse
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.AvatarURL == nil {
				t.Fatal("CreateAgent: avatar_url is nil")
			}
			if tt.wantEmoji {
				if !strings.HasPrefix(*response.AvatarURL, "emoji:") {
					t.Fatalf("CreateAgent: avatar_url = %q, want emoji avatar", *response.AvatarURL)
				}
				return
			}
			if *response.AvatarURL != tt.wantAvatar {
				t.Fatalf("CreateAgent: avatar_url = %q, want %q", *response.AvatarURL, tt.wantAvatar)
			}
		})
	}
}

func TestWorkspaceAlwaysRedactSecrets(t *testing.T) {
	tests := []struct {
		name     string
		settings []byte
		want     bool
	}{
		{"nil settings", nil, false},
		{"empty settings", []byte(`{}`), false},
		{"false", []byte(`{"always_redact_env": false}`), false},
		{"true", []byte(`{"always_redact_env": true}`), true},
		{"invalid json", []byte(`not json`), false},
		{"other fields only", []byte(`{"theme": "dark"}`), false},
		{"true among other fields", []byte(`{"theme": "dark", "always_redact_env": true}`), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workspaceAlwaysRedactSecrets(tt.settings); got != tt.want {
				t.Errorf("workspaceAlwaysRedactSecrets(%q) = %v, want %v", tt.settings, got, tt.want)
			}
		})
	}
}

func rawJSONResponse(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return out
}

func TestGetAgent_ResponseHasNoCustomEnv(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "noenv-get-agent", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"SECRET_KEY": "super-secret"}' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to set custom_env: %v", err)
	}

	req := newRequest("GET", "/agents/"+agentID, nil)
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	raw := rawJSONResponse(t, w.Body.Bytes())
	if _, ok := raw["custom_env"]; ok {
		t.Errorf("custom_env field must not appear in agent response, got %v", raw["custom_env"])
	}
	if _, ok := raw["custom_env_redacted"]; ok {
		t.Errorf("custom_env_redacted field must not appear in agent response (use has_custom_env)")
	}
	if got, _ := raw["has_custom_env"].(bool); !got {
		t.Errorf("has_custom_env expected true, got %v", raw["has_custom_env"])
	}
	if got, _ := raw["custom_env_key_count"].(float64); got != 1 {
		t.Errorf("custom_env_key_count expected 1, got %v", raw["custom_env_key_count"])
	}

	var typed AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &typed); err != nil {
		t.Fatalf("typed decode failed: %v", err)
	}
	if typed.HasCustomEnv != true {
		t.Errorf("typed.HasCustomEnv expected true")
	}
	if typed.CustomEnvKeyCount != 1 {
		t.Errorf("typed.CustomEnvKeyCount expected 1, got %d", typed.CustomEnvKeyCount)
	}
}

func TestListAgents_ResponseHasNoCustomEnv(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentName := "noenv-list-agent"
	agentID := createHandlerTestAgent(t, agentName, nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"SECRET_KEY": "super-secret", "OTHER": "y"}' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to set custom_env: %v", err)
	}

	req := newRequest("GET", "/agents", nil)
	w := httptest.NewRecorder()
	testHandler.ListAgents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rawAgents []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rawAgents); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	var found map[string]any
	for _, a := range rawAgents {
		if name, _ := a["name"].(string); name == agentName {
			found = a
			break
		}
	}
	if found == nil {
		t.Fatal("agent not found in list response")
	}
	if _, ok := found["custom_env"]; ok {
		t.Errorf("custom_env must not appear in list response")
	}
	if got, _ := found["custom_env_key_count"].(float64); got != 2 {
		t.Errorf("custom_env_key_count expected 2, got %v", found["custom_env_key_count"])
	}
	if got, _ := found["has_custom_env"].(bool); !got {
		t.Errorf("has_custom_env expected true")
	}
}

func TestGetAgentEnv_OwnerSucceedsAndAudits(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "env-reveal-owner-agent", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"KEY_ONE": "v1", "KEY_TWO": "v2"}' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to set custom_env: %v", err)
	}

	req := newRequest("GET", "/api/agents/"+agentID+"/env", nil)
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgentEnv(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetAgentEnv: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AgentEnvResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AgentID != agentID {
		t.Errorf("agent_id mismatch: got %q", resp.AgentID)
	}
	expected := map[string]string{"KEY_ONE": "v1", "KEY_TWO": "v2"}
	if !reflect.DeepEqual(resp.CustomEnv, expected) {
		t.Errorf("CustomEnv mismatch: got %v, want %v", resp.CustomEnv, expected)
	}

	var revealedKeysJSON string
	if err := testPool.QueryRow(ctx, `
		SELECT details::text FROM activity_log
		WHERE workspace_id = $1 AND action = 'agent_env_revealed'
		  AND details->>'agent_id' = $2
		ORDER BY created_at DESC LIMIT 1
	`, testWorkspaceID, agentID).Scan(&revealedKeysJSON); err != nil {
		t.Fatalf("no agent_env_revealed activity row found: %v", err)
	}
	if !strings.Contains(revealedKeysJSON, `"KEY_ONE"`) || !strings.Contains(revealedKeysJSON, `"KEY_TWO"`) {
		t.Errorf("expected revealed_keys to contain KEY_ONE and KEY_TWO, got: %s", revealedKeysJSON)
	}
	if strings.Contains(revealedKeysJSON, `"v1"`) || strings.Contains(revealedKeysJSON, `"v2"`) {
		t.Errorf("activity details must NOT contain env values, got: %s", revealedKeysJSON)
	}
}

func TestAgentEnv_AgentActorRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	targetID := createHandlerTestAgent(t, "env-target-agent", nil)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET custom_env = '{"K":"v"}' WHERE id = $1`, targetID); err != nil {
		t.Fatalf("failed to set custom_env: %v", err)
	}

	hostAgentID := createHandlerTestAgent(t, "env-host-agent", nil)
	hostTaskID := createHandlerTestTaskForAgent(t, hostAgentID)

	cases := []struct {
		name string
		fn   func(http.ResponseWriter, *http.Request)
		body any
	}{
		{"reveal", testHandler.GetAgentEnv, nil},
		{"update", testHandler.UpdateAgentEnv, map[string]any{"custom_env": map[string]string{"K": "v2"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method := http.MethodGet
			if tc.body != nil {
				method = http.MethodPut
			}
			req := newRequest(method, "/api/agents/"+targetID+"/env", tc.body)
			req = withURLParam(req, "id", targetID)
			req.Header.Set("X-Actor-Source", "task_token")
			req.Header.Set("X-Agent-ID", hostAgentID)
			req.Header.Set("X-Task-ID", hostTaskID)
			w := httptest.NewRecorder()
			tc.fn(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403 from agent actor, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAgentEnv_TaskTokenActorSource(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	targetID := createHandlerTestAgent(t, "env-tt-target-agent", nil)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET custom_env = '{"K":"v"}' WHERE id = $1`, targetID); err != nil {
		t.Fatalf("failed to set custom_env: %v", err)
	}

	req := newRequest(http.MethodGet, "/api/agents/"+targetID+"/env", nil)
	req = withURLParam(req, "id", targetID)

	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Del("X-Agent-ID")
	req.Header.Del("X-Task-ID")
	w := httptest.NewRecorder()
	testHandler.GetAgentEnv(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when X-Actor-Source=task_token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentEnv_PreservesSentinelValues(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "env-sentinel-agent", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"KEEP_ME":"real-secret","ALSO":"another-secret"}' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to seed custom_env: %v", err)
	}

	body := map[string]any{
		"custom_env": map[string]string{
			"KEEP_ME":   "****",
			"ALSO":      "rotated",
			"BRAND_NEW": "fresh",
		},
	}
	req := newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", body)
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentEnv(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentEnv: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(ctx, `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("failed to read back custom_env: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(stored), &got); err != nil {
		t.Fatalf("failed to decode stored custom_env: %v", err)
	}
	want := map[string]string{
		"KEEP_ME":   "real-secret",
		"ALSO":      "rotated",
		"BRAND_NEW": "fresh",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored custom_env mismatch:\n got:  %v\n want: %v", got, want)
	}

	var details string
	if err := testPool.QueryRow(ctx, `
		SELECT details::text FROM activity_log
		WHERE workspace_id = $1 AND action = 'agent_env_updated' AND details->>'agent_id' = $2
		ORDER BY created_at DESC LIMIT 1
	`, testWorkspaceID, agentID).Scan(&details); err != nil {
		t.Fatalf("expected agent_env_updated activity row: %v", err)
	}
	var auditFields struct {
		AddedKeys     []string `json:"added_keys"`
		ChangedKeys   []string `json:"changed_keys"`
		PreservedKeys []string `json:"preserved_keys"`
	}
	if err := json.Unmarshal([]byte(details), &auditFields); err != nil {
		t.Fatalf("failed to decode audit details: %v (raw=%s)", err, details)
	}
	if !reflect.DeepEqual(auditFields.AddedKeys, []string{"BRAND_NEW"}) {
		t.Errorf("added_keys: got %v, want [BRAND_NEW]; raw=%s", auditFields.AddedKeys, details)
	}
	if !reflect.DeepEqual(auditFields.ChangedKeys, []string{"ALSO"}) {
		t.Errorf("changed_keys: got %v, want [ALSO]; raw=%s", auditFields.ChangedKeys, details)
	}
	if !reflect.DeepEqual(auditFields.PreservedKeys, []string{"KEEP_ME"}) {
		t.Errorf("preserved_keys: got %v, want [KEEP_ME]; raw=%s", auditFields.PreservedKeys, details)
	}

	for _, leak := range []string{"real-secret", "another-secret", "rotated", "fresh"} {
		if strings.Contains(details, leak) {
			t.Errorf("audit details leaked value %q: %s", leak, details)
		}
	}
}

func TestUpdateAgent_RejectsCustomEnvInBody(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "update-no-env-agent", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"PRE":"existing"}' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to seed custom_env: %v", err)
	}

	body := map[string]any{
		"description": "still updating description",
		"custom_env":  map[string]string{"INJECTED": "should-not-stick"},
	}
	req := newRequest(http.MethodPut, "/api/agents/"+agentID, body)
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAgent: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "custom_env") || !strings.Contains(w.Body.String(), "/env") {
		t.Errorf("error body should mention custom_env and the env endpoint; got %s", w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(ctx, `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("failed to read custom_env: %v", err)
	}
	if !strings.Contains(stored, `"PRE": "existing"`) && !strings.Contains(stored, `"PRE":"existing"`) {
		t.Errorf("UpdateAgent must NOT touch custom_env; got %q", stored)
	}
	if strings.Contains(stored, "INJECTED") {
		t.Errorf("UpdateAgent should have rejected custom_env in body; got %q", stored)
	}
}

func TestMergeAgentEnv_PureFunction(t *testing.T) {
	cases := []struct {
		name      string
		existing  map[string]string
		request   map[string]string
		want      map[string]string
		audit     envAudit
		wantError bool
	}{
		{
			name:     "preserve sentinel",
			existing: map[string]string{"A": "real"},
			request:  map[string]string{"A": "****"},
			want:     map[string]string{"A": "real"},
			audit:    envAudit{preserved: []string{"A"}},
		},
		{

			name:      "reject sentinel for missing key",
			existing:  map[string]string{},
			request:   map[string]string{"A": "****"},
			wantError: true,
		},
		{

			name:      "reject value typed onto the sentinel",
			existing:  map[string]string{"A": "real"},
			request:   map[string]string{"A": "****newsecret"},
			wantError: true,
		},
		{
			name:     "add new key",
			existing: map[string]string{},
			request:  map[string]string{"B": "v"},
			want:     map[string]string{"B": "v"},
			audit:    envAudit{added: []string{"B"}},
		},
		{
			name:     "change existing value",
			existing: map[string]string{"B": "old"},
			request:  map[string]string{"B": "new"},
			want:     map[string]string{"B": "new"},
			audit:    envAudit{changed: []string{"B"}},
		},
		{
			name:     "remove key absent from request",
			existing: map[string]string{"B": "v"},
			request:  map[string]string{},
			want:     map[string]string{},
			audit:    envAudit{removed: []string{"B"}},
		},
		{
			name:     "noop when value unchanged",
			existing: map[string]string{"B": "same"},
			request:  map[string]string{"B": "same"},
			want:     map[string]string{"B": "same"},
			audit:    envAudit{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, audit, err := mergeAgentEnv(tc.existing, tc.request)
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected an error, got merged map %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("merged map: got %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(audit, tc.audit) {
				t.Errorf("audit: got %+v, want %+v", audit, tc.audit)
			}
		})
	}
}

func TestAgentResponseShape_HasNoLegacyEnvFields(t *testing.T) {
	typ := reflect.TypeOf(AgentResponse{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		switch tag {
		case "custom_env", "custom_env_redacted", "custom_env_redacted_reason":
			t.Errorf("AgentResponse must not carry %q field (MUL-2600)", tag)
		}
	}
}

func TestUpdateAgent_RedactsMcpConfigForAgentActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	target := createHandlerTestAgent(t, "mut-mcp-target", []byte(`{"server":"secret-config"}`))

	caller := createHandlerTestAgent(t, "mut-mcp-caller", nil)
	taskID := insertHandlerTestTask(t, caller)

	desc := "trivial mutation that should NOT leak target mcp_config"
	req := newRequest(http.MethodPut, "/api/agents/"+target, map[string]any{
		"description": desc,
	})
	req = withURLParam(req, "id", target)

	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", caller)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateAgent: expected 403 for an agent actor, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret-config") {
		t.Errorf("refusal body leaked mcp_config to agent actor: %s", w.Body.String())
	}
}

func TestUpdateAgent_KeepsMcpConfigForMemberActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	target := createHandlerTestAgent(t, "mut-mcp-member", []byte(`{"server":"member-visible"}`))

	req := newRequest(http.MethodPut, "/api/agents/"+target, map[string]any{
		"description": "owner-visible mutation",
	})
	req = withURLParam(req, "id", target)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.McpConfig == nil {
		t.Errorf("UpdateAgent response should keep mcp_config for member actor; got nil")
	}
	if resp.McpConfigRedacted {
		t.Errorf("UpdateAgent response should NOT mark mcp_config redacted for member actor")
	}
}

func TestUpdateAgent_PreservesSkillsInResponse(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "update-preserves-skills-agent", nil)
	skillA := insertHandlerTestSkill(t, "update-preserve-a", "alpha body")
	skillB := insertHandlerTestSkill(t, "update-preserve-b", "beta body")
	for _, sid := range []string{skillA, skillB} {
		if _, err := testPool.Exec(ctx,
			`INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`,
			agentID, sid,
		); err != nil {
			t.Fatalf("attach skill %s: %v", sid, err)
		}
	}

	req := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"description": "metadata-only update",
	})
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	gotIDs := map[string]bool{}
	for _, s := range resp.Skills {
		gotIDs[s.ID] = true
	}
	for _, want := range []string{skillA, skillB} {
		if !gotIDs[want] {
			t.Errorf("UpdateAgent response missing skill %s; got %+v", want, resp.Skills)
		}
	}

	var rowCount int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM agent_skill WHERE agent_id = $1`,
		agentID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count agent_skill: %v", err)
	}
	if rowCount != 2 {
		t.Errorf("agent_skill row count: expected 2, got %d", rowCount)
	}

	getReq := newRequest(http.MethodGet, "/api/agents/"+agentID, nil)
	getReq = withURLParam(getReq, "id", agentID)
	getW := httptest.NewRecorder()
	testHandler.GetAgent(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GetAgent: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}
	var getResp AgentResponse
	if err := json.NewDecoder(getW.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode GetAgent: %v", err)
	}
	if len(getResp.Skills) != len(resp.Skills) {
		t.Errorf("GetAgent skill count %d != UpdateAgent skill count %d",
			len(getResp.Skills), len(resp.Skills))
	}
}

func TestArchiveRestoreAgent_PreservesSkillsInResponse(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "archive-preserves-skills-agent", nil)
	skillID := insertHandlerTestSkill(t, "archive-preserve", "body")
	if _, err := testPool.Exec(ctx,
		`INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`,
		agentID, skillID,
	); err != nil {
		t.Fatalf("attach skill: %v", err)
	}

	archiveReq := newRequest(http.MethodPost, "/api/agents/"+agentID+"/archive", nil)
	archiveReq = withURLParam(archiveReq, "id", agentID)
	archiveW := httptest.NewRecorder()
	testHandler.ArchiveAgent(archiveW, archiveReq)
	if archiveW.Code != http.StatusOK {
		t.Fatalf("ArchiveAgent: expected 200, got %d: %s", archiveW.Code, archiveW.Body.String())
	}
	var archived AgentResponse
	if err := json.NewDecoder(archiveW.Body).Decode(&archived); err != nil {
		t.Fatalf("decode archive: %v", err)
	}
	if len(archived.Skills) != 1 || archived.Skills[0].ID != skillID {
		t.Errorf("ArchiveAgent: expected 1 skill %s, got %+v", skillID, archived.Skills)
	}

	restoreReq := newRequest(http.MethodPost, "/api/agents/"+agentID+"/restore", nil)
	restoreReq = withURLParam(restoreReq, "id", agentID)
	restoreW := httptest.NewRecorder()
	testHandler.RestoreAgent(restoreW, restoreReq)
	if restoreW.Code != http.StatusOK {
		t.Fatalf("RestoreAgent: expected 200, got %d: %s", restoreW.Code, restoreW.Body.String())
	}
	var restored AgentResponse
	if err := json.NewDecoder(restoreW.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if len(restored.Skills) != 1 || restored.Skills[0].ID != skillID {
		t.Errorf("RestoreAgent: expected 1 skill %s, got %+v", skillID, restored.Skills)
	}
}

func insertHandlerTestTask(t *testing.T, agentID string) string {
	t.Helper()
	ctx := context.Background()
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'running', 0)
		RETURNING id
	`, agentID, handlerTestRuntimeID(t)).Scan(&taskID); err != nil {
		t.Fatalf("insert test task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

var _ = fmt.Sprintf
