package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
)

type liveDeputyFixture struct {
	UserU string

	AgentA string

	AllowListed string
	Owned       string

	IssueX        string
	ChatSessionID string
	ChatTaskID    string
	IssueTaskX    string
}

func seedLiveDeputyFixture(t *testing.T) liveDeputyFixture {
	t.Helper()
	ctx := context.Background()

	userU := createPermissionTestMember(t, "live-deputy-u@goosar.test")
	agentA := createHandlerTestAgent(t, "Live Deputy Agent A", nil)

	allowListed := createPublicToAgentWithTargets(t, "live-deputy-allowlisted", []map[string]any{
		{"target_type": "member", "target_id": userU},
	})

	var owned string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'live-deputy-owned-by-u', '', 'cloud', '{}'::jsonb,
			$2, 'private', 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, handlerTestRuntimeID(t), userU).Scan(&owned); err != nil {
		t.Fatalf("seed owned agent: %v", err)
	}
	cleanupCheckedExec(t, `DELETE FROM agent_task_queue WHERE agent_id = $1`, owned)
	cleanupCheckedExec(t, `DELETE FROM agent WHERE id = $1`, owned)

	issueX := createCommentTriggerPreviewIssue(t, "live deputy unrelated issue X", "", "")
	chatSessionID := seedChatSession(t, agentA)

	return liveDeputyFixture{
		UserU:         userU,
		AgentA:        agentA,
		AllowListed:   allowListed,
		Owned:         owned,
		IssueX:        issueX,
		ChatSessionID: chatSessionID,
		ChatTaskID:    seedLiveDeputyTask(t, agentA, "chat_session_id", chatSessionID, userU),
		IssueTaskX:    seedLiveDeputyTask(t, agentA, "issue_id", issueX, userU),
	}
}

func seedLiveDeputyTask(t *testing.T, agentID, column, value, originatorUserID string) string {
	t.Helper()
	if column != "issue_id" && column != "chat_session_id" {
		t.Fatalf("seedLiveDeputyTask: unsupported column %q", column)
	}
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, `+column+`,
			originator_user_id, accountable_user_id, started_at
		)
		VALUES ($1, $2, 'running', 0, $3, $4, $4, now())
		RETURNING id
	`, agentID, handlerTestRuntimeID(t), value, originatorUserID).Scan(&taskID); err != nil {
		t.Fatalf("seed %s task: %v", column, err)
	}
	cleanupCheckedExec(t, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	return taskID
}

func commentAsAgentMentioning(t *testing.T, issueID, agentID, taskID, targetAgentID string) []CommentTriggerOutcome {
	t.Helper()
	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@target](mention://agent/" + targetAgentID + ") please take this",
	}), "id", issueID)
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", taskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment response: %v", err)
	}
	return resp.TriggerOutcomes
}

func outcomeFor(t *testing.T, outcomes []CommentTriggerOutcome, targetAgentID string) CommentTriggerOutcome {
	t.Helper()
	for _, o := range outcomes {
		if o.TargetType == "agent" && o.TargetID == targetAgentID {
			return o
		}
	}
	t.Fatalf("no trigger outcome for agent %s in %+v", targetAgentID, outcomes)
	return CommentTriggerOutcome{}
}

func agentTaskCount(t *testing.T, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`, agentID,
	).Scan(&n); err != nil {
		t.Fatalf("count tasks for agent %s: %v", agentID, err)
	}
	return n
}

func TestLivePath_ChatTaskCannotReachAllowListedAgentOnUnrelatedIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := seedLiveDeputyFixture(t)

	before := agentTaskCount(t, fx.AllowListed)
	outcomes := commentAsAgentMentioning(t, fx.IssueX, fx.AgentA, fx.ChatTaskID, fx.AllowListed)

	got := outcomeFor(t, outcomes, fx.AllowListed)
	if got.Status != DispatchBlocked {
		t.Fatalf("mention outcome = %q (reason %q), want %q — a chat task must not lend U's allow-list grant to an unrelated issue",
			got.Status, got.ReasonCode, DispatchBlocked)
	}
	if got.ReasonCode != ReasonInvocationNotAllowed {
		t.Errorf("blocked reason = %q, want %q", got.ReasonCode, ReasonInvocationNotAllowed)
	}
	if after := agentTaskCount(t, fx.AllowListed); after != before {
		t.Fatalf("task count for the allow-listed agent went %d -> %d; the refusal must enqueue nothing", before, after)
	}
}

func TestLivePath_ChatTaskStillReachesOriginatorOwnAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := seedLiveDeputyFixture(t)

	outcomes := commentAsAgentMentioning(t, fx.IssueX, fx.AgentA, fx.ChatTaskID, fx.Owned)

	got := outcomeFor(t, outcomes, fx.Owned)
	if got.Status != DispatchQueued {
		t.Fatalf("mention outcome = %q (reason %q), want %q — the owner branch must survive an unscoped authority",
			got.Status, got.ReasonCode, DispatchQueued)
	}
}

func TestLivePath_IssueTaskOnItsOwnIssueUnchanged(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("allow-list branch still admits", func(t *testing.T) {
		fx := seedLiveDeputyFixture(t)
		outcomes := commentAsAgentMentioning(t, fx.IssueX, fx.AgentA, fx.IssueTaskX, fx.AllowListed)
		if got := outcomeFor(t, outcomes, fx.AllowListed); got.Status != DispatchQueued {
			t.Fatalf("mention outcome = %q (reason %q), want %q — an issue task acting on ITS OWN issue is unchanged",
				got.Status, got.ReasonCode, DispatchQueued)
		}
	})

	t.Run("owner branch still admits", func(t *testing.T) {
		fx := seedLiveDeputyFixture(t)
		outcomes := commentAsAgentMentioning(t, fx.IssueX, fx.AgentA, fx.IssueTaskX, fx.Owned)
		if got := outcomeFor(t, outcomes, fx.Owned); got.Status != DispatchQueued {
			t.Fatalf("mention outcome = %q (reason %q), want %q", got.Status, got.ReasonCode, DispatchQueued)
		}
	})
}

func TestInvokeAuthorityFromRequest_ScopePerSite(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := seedLiveDeputyFixture(t)
	otherIssue := createCommentTriggerPreviewIssue(t, "live deputy other issue", "", "")
	otherSession := seedChatSession(t, fx.AgentA)

	issueXUUID := util.MustParseUUID(fx.IssueX)
	otherIssueUUID := util.MustParseUUID(otherIssue)
	chatSessionUUID := util.MustParseUUID(fx.ChatSessionID)
	otherSessionUUID := util.MustParseUUID(otherSession)

	agentReq := func(taskID string) *http.Request {
		r := newRequest(http.MethodPost, "/api/issues/"+fx.IssueX+"/comments", nil)
		r.Header.Set("X-Agent-ID", fx.AgentA)
		r.Header.Set("X-Task-ID", taskID)
		return r
	}

	cases := []struct {
		name       string
		actorType  string
		actorID    string
		req        *http.Request
		scope      invokeScope
		wantUser   string
		wantScoped bool
	}{
		{
			name: "chat task, chat-session scope, its OWN session -> scoped",

			actorType: "agent", actorID: fx.AgentA, req: agentReq(fx.ChatTaskID),
			scope: chatSessionInvokeScope(chatSessionUUID), wantUser: fx.UserU, wantScoped: true,
		},
		{
			name:      "chat task, chat-session scope, a DIFFERENT session -> unscoped",
			actorType: "agent", actorID: fx.AgentA, req: agentReq(fx.ChatTaskID),
			scope: chatSessionInvokeScope(otherSessionUUID), wantUser: fx.UserU, wantScoped: false,
		},
		{
			name: "chat task, ISSUE scope -> unscoped (the reported attack)",

			actorType: "agent", actorID: fx.AgentA, req: agentReq(fx.ChatTaskID),
			scope: issueInvokeScope(issueXUUID), wantUser: fx.UserU, wantScoped: false,
		},
		{
			name:      "issue task, issue scope, its OWN issue -> scoped",
			actorType: "agent", actorID: fx.AgentA, req: agentReq(fx.IssueTaskX),
			scope: issueInvokeScope(issueXUUID), wantUser: fx.UserU, wantScoped: true,
		},
		{
			name:      "issue task, issue scope, a DIFFERENT issue -> unscoped",
			actorType: "agent", actorID: fx.AgentA, req: agentReq(fx.IssueTaskX),
			scope: issueInvokeScope(otherIssueUUID), wantUser: fx.UserU, wantScoped: false,
		},
		{
			name: "issue task, issue scope, no such issue yet -> unscoped",

			actorType: "agent", actorID: fx.AgentA, req: agentReq(fx.IssueTaskX),
			scope: issueInvokeScope(pgtype.UUID{}), wantUser: fx.UserU, wantScoped: false,
		},
		{
			name:      "issue task, no scope (autopilot / chat create) -> unscoped",
			actorType: "agent", actorID: fx.AgentA, req: agentReq(fx.IssueTaskX),
			scope: noInvokeScope, wantUser: fx.UserU, wantScoped: false,
		},
		{
			name: "member actor -> always their own scoped originator",

			actorType: "member", actorID: fx.UserU,
			req:   newRequest(http.MethodPost, "/api/issues/"+fx.IssueX+"/comments", nil),
			scope: noInvokeScope, wantUser: fx.UserU, wantScoped: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := testHandler.invokeAuthorityFromRequest(tc.req, tc.actorType, tc.actorID, tc.scope)
			if got.UserID != tc.wantUser {
				t.Errorf("UserID = %q, want %q", got.UserID, tc.wantUser)
			}
			if got.Scoped != tc.wantScoped {
				t.Errorf("Scoped = %v, want %v", got.Scoped, tc.wantScoped)
			}
		})
	}
}

func TestCanInvokeAgent_UnscopedAuthorityPolicy(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := seedLiveDeputyFixture(t)

	allowListed, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(fx.AllowListed))
	if err != nil {
		t.Fatalf("load allow-listed agent: %v", err)
	}
	owned, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(fx.Owned))
	if err != nil {
		t.Fatalf("load owned agent: %v", err)
	}

	workspaceAgent, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(fx.AgentA))
	if err != nil {
		t.Fatalf("load workspace agent: %v", err)
	}

	cases := []struct {
		name      string
		agentRow  string
		actorType string
		authority invokeAuthority
		want      bool
	}{
		{"unscoped human must NOT reach a member-target agent", "allowlisted", "agent", unscopedInvokeAuthority(fx.UserU), false},
		{"scoped human DOES reach a member-target agent", "allowlisted", "agent", scopedInvokeAuthority(fx.UserU), true},
		{"unscoped human still reaches an agent they OWN", "owned", "agent", unscopedInvokeAuthority(fx.UserU), true},
		{"scoped human reaches an agent they own", "owned", "agent", scopedInvokeAuthority(fx.UserU), true},

		{"unscoped human keeps the workspace-target exception", "workspace", "agent", unscopedInvokeAuthority(fx.UserU), true},
		{"no human at all keeps the workspace-target exception", "workspace", "agent", invokeAuthority{}, true},
		{"no human at all still fails closed on a member target", "allowlisted", "agent", invokeAuthority{}, false},

		{"member actor is unaffected by the scope flag", "allowlisted", "member", unscopedInvokeAuthority(fx.UserU), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := allowListed
			switch tc.agentRow {
			case "owned":
				target = owned
			case "workspace":
				target = workspaceAgent
			}
			actorID := fx.AgentA
			if tc.actorType == "member" {
				actorID = fx.UserU
			}
			got := testHandler.canInvokeAgent(ctx, target, tc.actorType, actorID, tc.authority, testWorkspaceID)
			if got != tc.want {
				t.Fatalf("canInvokeAgent = %v, want %v", got, tc.want)
			}
		})
	}
}
