package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func seedTaskWithOriginator(t *testing.T, agentID, originatorUserID string) string {
	t.Helper()
	var originatorArg any
	if originatorUserID != "" {
		originatorArg = originatorUserID
	}
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, originator_user_id, accountable_user_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2, $2)
		RETURNING id
	`, agentID, originatorArg).Scan(&taskID); err != nil {
		t.Fatalf("seed task for agent %s: %v", agentID, err)
	}
	cleanupCheckedExec(t, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	return taskID
}

func TestCreateIssue_H1_IgnoresBodyOriginDerivesFromXTaskID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	victimV := createPermissionTestMember(t, "h1-victim-v@goosar.test")
	victimAgent := createHandlerTestAgent(t, "h1-victim-agent", nil)
	foreignTaskF := seedTaskWithOriginator(t, victimAgent, victimV)

	actingAgent := createHandlerTestAgent(t, "h1-acting-agent", nil)
	actingTask := seedTaskWithOriginator(t, actingAgent, "")

	w := httptest.NewRecorder()

	r := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "H1 forged-origin attack",
		"origin_type": "quick_create",
		"origin_id":   foreignTaskF,
	})
	r.Header.Set("X-Agent-ID", actingAgent)
	r.Header.Set("X-Task-ID", actingTask)
	r.Header.Set("X-Actor-Source", "task_token")
	testHandler.CreateIssue(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	var originType, originID string
	if err := testPool.QueryRow(ctx,
		`SELECT COALESCE(origin_type, ''), COALESCE(origin_id::text, '') FROM issue WHERE id = $1`, created.ID,
	).Scan(&originType, &originID); err != nil {
		t.Fatalf("load issue origin: %v", err)
	}
	if originID == foreignTaskF {
		t.Fatalf("issue origin_id = %s (the FORGED foreign task); the body origin was trusted (#144 regression)", originID)
	}
	if originID != actingTask {
		t.Fatalf("issue origin_id = %s, want the acting task %s (derived from X-Task-ID)", originID, actingTask)
	}

	if originType != "agent_create" {
		t.Fatalf("issue origin_type = %q, want agent_create (derived from the acting task, not the body)", originType)
	}
}

func TestCreateIssue_H1_LegitDaemonQuickCreateStillStamps(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "h1-quickcreate-agent", nil)

	qcContext := fmt.Sprintf(`{"type":"quick_create","prompt":"make an issue","requester_id":%q,"workspace_id":%q}`,
		testUserID, testWorkspaceID)
	var quickCreateTask string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2::jsonb)
		RETURNING id
	`, agentID, qcContext).Scan(&quickCreateTask); err != nil {
		t.Fatalf("seed quick-create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, quickCreateTask)
	})

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Quick-created issue",
	})
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", quickCreateTask)
	r.Header.Set("X-Actor-Source", "task_token")
	testHandler.CreateIssue(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID) })

	var originType, originID string
	if err := testPool.QueryRow(ctx,
		`SELECT COALESCE(origin_type, ''), COALESCE(origin_id::text, '') FROM issue WHERE id = $1`, created.ID,
	).Scan(&originType, &originID); err != nil {
		t.Fatalf("load issue origin: %v", err)
	}
	if originType != "quick_create" || originID != quickCreateTask {
		t.Fatalf("issue origin = (%q,%q), want (quick_create,%s)", originType, originID, quickCreateTask)
	}

	found, err := testHandler.Queries.GetIssueByOrigin(ctx, db.GetIssueByOriginParams{
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
		OriginType:  pgtype.Text{String: "quick_create", Valid: true},
		OriginID:    util.MustParseUUID(quickCreateTask),
	})
	if err != nil {
		t.Fatalf("GetIssueByOrigin failed to locate the quick-created issue: %v", err)
	}
	if util.UUIDToString(found.ID) != created.ID {
		t.Fatalf("GetIssueByOrigin found %s, want %s", util.UUIDToString(found.ID), created.ID)
	}
}

func TestResolveActor_H3_ForgedPairFallsBackToMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentA, ownerV, memberM := privateAgentTestFixture(t)

	taskT := seedTaskWithOriginator(t, agentA, ownerV)

	forged := newRequestAs(memberM, "GET", "/x", nil)
	forged.Header.Set("X-Agent-ID", agentA)
	forged.Header.Set("X-Task-ID", taskT)
	if aType, aID := testHandler.resolveActor(forged, memberM, testWorkspaceID); aType != "member" || aID != memberM {
		t.Fatalf("forged pair resolved to (%q,%q); want (member,%s) — #167 impersonation not closed", aType, aID, memberM)
	}

	asOwner := newRequestAs(ownerV, "GET", "/x", nil)
	asOwner.Header.Set("X-Agent-ID", agentA)
	asOwner.Header.Set("X-Task-ID", taskT)
	if aType, aID := testHandler.resolveActor(asOwner, ownerV, testWorkspaceID); aType != "member" || aID != ownerV {
		t.Fatalf("owner resolved to (%q,%q); want (member,%s) — header fallback should be fully retired", aType, aID, ownerV)
	}

	asTaskToken := newRequestAs(memberM, "GET", "/x", nil)
	asTaskToken.Header.Set("X-Agent-ID", agentA)
	asTaskToken.Header.Set("X-Task-ID", taskT)
	asTaskToken.Header.Set("X-Actor-Source", "task_token")
	if aType, aID := testHandler.resolveActor(asTaskToken, memberM, testWorkspaceID); aType != "agent" || aID != agentA {
		t.Fatalf("task-token request resolved to (%q,%q); want (agent,%s) — daemon path broke", aType, aID, agentA)
	}
}

func TestTaskFromRequestHeader_H5_ScopedAndAgentBound(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentA := createHandlerTestAgent(t, "h5-agent-a", nil)
	agentB := createHandlerTestAgent(t, "h5-agent-b", nil)

	humanU := createPermissionTestMember(t, "h5-human-u@goosar.test")
	taskOfB := seedTaskWithOriginator(t, agentB, humanU)

	rWrongAgent := newRequest("GET", "/x?workspace_id="+testWorkspaceID, nil)
	rWrongAgent.Header.Set("X-Task-ID", taskOfB)
	if auth := testHandler.invokeAuthorityFromRequest(rWrongAgent, "agent", agentA, issueInvokeScope(pgtype.UUID{})); auth.UserID != "" || auth.Scoped {
		t.Fatalf("invokeAuthority for A naming B's task = %+v; want zero authority (agent binding, H5)", auth)
	}

	var foreignWS string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('H5 Foreign WS', 'h5-foreign-ws-' || gen_random_uuid(), 'x', 'H5F') RETURNING id
	`).Scan(&foreignWS); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWS) })
	var foreignRuntime, foreignAgent, foreignTask string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'h5-foreign-runtime', 'cloud', 'runtime-e', 'online', '', '{}'::jsonb, $2) RETURNING id
	`, foreignWS, humanU).Scan(&foreignRuntime); err != nil {
		t.Fatalf("seed foreign runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'h5-foreign-agent', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb) RETURNING id
	`, foreignWS, foreignRuntime, humanU).Scan(&foreignAgent); err != nil {
		t.Fatalf("seed foreign agent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, originator_user_id, accountable_user_id)
		VALUES ($1, $2, 'running', 0, $3, $3) RETURNING id
	`, foreignAgent, foreignRuntime, humanU).Scan(&foreignTask); err != nil {
		t.Fatalf("seed foreign task: %v", err)
	}

	rCrossWS := newRequest("GET", "/x?workspace_id="+testWorkspaceID, nil)
	rCrossWS.Header.Set("X-Task-ID", foreignTask)
	if auth := testHandler.invokeAuthorityFromRequest(rCrossWS, "agent", foreignAgent, issueInvokeScope(pgtype.UUID{})); auth.UserID != "" || auth.Scoped {
		t.Fatalf("invokeAuthority for a cross-workspace task = %+v; want zero authority (workspace scoping, H5)", auth)
	}
}

func TestListComments_H4_RedactsSourceTaskIDExceptAgentSystem(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "h4-agent", nil)
	taskID := seedTaskWithOriginator(t, agentID, testUserID)
	issueID := createCommentTriggerPreviewIssue(t, "h4 comment redaction", "", "")

	var regularID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, source_task_id)
		VALUES ($1, $2, 'agent', $3, 'a normal agent reply', 'comment', $4) RETURNING id
	`, issueID, testWorkspaceID, agentID, taskID).Scan(&regularID); err != nil {
		t.Fatalf("seed regular agent comment: %v", err)
	}

	var systemID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, source_task_id)
		VALUES ($1, $2, 'agent', $3, 'API Error: run failed', 'system', $4) RETURNING id
	`, issueID, testWorkspaceID, agentID, taskID).Scan(&systemID); err != nil {
		t.Fatalf("seed agent system comment: %v", err)
	}

	w := httptest.NewRecorder()
	r := withURLParam(newRequest("GET", "/api/issues/"+issueID+"/comments", nil), "id", issueID)
	testHandler.ListComments(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("ListComments: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var comments []CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&comments); err != nil {
		t.Fatalf("decode comments: %v", err)
	}

	var sawRegular, sawSystem bool
	for _, c := range comments {
		switch c.ID {
		case regularID:
			sawRegular = true
			if c.SourceTaskID != nil {
				t.Errorf("regular agent comment exposed source_task_id = %s; want omitted (H4)", *c.SourceTaskID)
			}
		case systemID:
			sawSystem = true
			if c.SourceTaskID == nil || *c.SourceTaskID != taskID {
				got := "<nil>"
				if c.SourceTaskID != nil {
					got = *c.SourceTaskID
				}
				t.Errorf("agent system comment source_task_id = %s; want %s (retry affordance must keep it)", got, taskID)
			}
		}
	}
	if !sawRegular || !sawSystem {
		t.Fatalf("did not see both seeded comments (regular=%v system=%v)", sawRegular, sawSystem)
	}
}

func TestSubscribe_H7_RejectsForeignTargetForNonAdmin(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	memberA := createPermissionTestMember(t, "h7-member-a@goosar.test")
	memberB := createPermissionTestMember(t, "h7-member-b@goosar.test")
	issueID := createCommentTriggerPreviewIssue(t, "h7 subscription target", "", "")

	subscribeAs := func(callerUserID string, body map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := withURLParam(newRequestAs(callerUserID, "POST", "/api/issues/"+issueID+"/subscribe", body), "id", issueID)
		testHandler.SubscribeToIssue(w, r)
		return w
	}
	isSubscribed := func(userID string) bool {
		ok, err := testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  util.MustParseUUID(issueID),
			UserType: "member",
			UserID:   util.MustParseUUID(userID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber: %v", err)
		}
		return ok
	}

	if w := subscribeAs(memberA, map[string]any{"user_type": "member", "user_id": memberB}); w.Code != http.StatusForbidden {
		t.Fatalf("member A subscribing B: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if isSubscribed(memberB) {
		t.Fatalf("member B was subscribed by a non-admin (#168 not closed)")
	}

	if w := subscribeAs(memberA, nil); w.Code != http.StatusOK {
		t.Fatalf("member A self-subscribe: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !isSubscribed(memberA) {
		t.Fatalf("member A self-subscribe did not persist")
	}

	if w := subscribeAs(testUserID, map[string]any{"user_type": "member", "user_id": memberB}); w.Code != http.StatusOK {
		t.Fatalf("owner subscribing B: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !isSubscribed(memberB) {
		t.Fatalf("owner-initiated subscribe of B did not persist")
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_subscriber WHERE issue_id = $1`, issueID)
	})
}
