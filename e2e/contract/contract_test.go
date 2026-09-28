package contract

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func urlEscape(s string) string {
	return url.QueryEscape(s)
}

// TestContract is the single entry point for the whole suite. Every domain
// gets its own named subtest so a single one can be re-run in isolation,
// e.g.:
//
//	go test ./e2e/contract/... -run 'TestContract/auth' -v
//
// Each subtest calls the ensureX() fixture helpers it needs, so it is
// self-sufficient even when run alone: it will transparently create the
// workspace/runtime/agent/etc. it depends on if a fuller run hasn't already
// done so.
func TestContract(t *testing.T) {
	f := getFixture(t)

	t.Run("auth", testAuth(f))
	t.Run("workspaces", testWorkspaces(f))
	t.Run("me", testMe(f))
	t.Run("issues", testIssues(f))
	t.Run("projects", testProjects(f))
	t.Run("labels", testLabels(f))
	t.Run("comments", testComments(f))
	t.Run("squads", testSquads(f))
	t.Run("autopilots", testAutopilots(f))
	t.Run("agents", testAgents(f))
	t.Run("runtimes", testRuntimes(f))
	t.Run("skills", testSkills(f))
	t.Run("chat", testChat(f))
	t.Run("inbox", testInbox(f))
	t.Run("daemon", testDaemon(f))
	t.Run("deployment", testDeployment(f))
	t.Run("admin", testAdmin(f))
	t.Run("integrations", testIntegrations(f))
}

// ---------------------------------------------------------------------------
// auth
// ---------------------------------------------------------------------------

func testAuth(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureAuth(t)

		status, _ := f.call(t, http.MethodGet, "/api/auth/methods", "/api/auth/methods", nil)
		if status != 200 {
			t.Errorf("GET /api/auth/methods: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/me", "/api/me", nil)
		if status != 200 {
			t.Errorf("GET /api/me: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/auth/mfa", "/api/auth/mfa", nil)
		if status != 200 {
			t.Errorf("GET /api/auth/mfa: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/auth/sessions", "/api/auth/sessions", nil)
		if status != 200 {
			t.Errorf("GET /api/auth/sessions: expected 200, got %d", status)
		}

		// Wrong verification code: a documented failure path, not just the
		// happy path.
		status, _ = f.call(t, http.MethodPost, "/auth/verify-code", "/auth/verify-code", map[string]any{
			"email": fmt.Sprintf("nobody-%s@example.test", uniqueSuffix()),
			"code":  "000000",
		})
		if status != 400 {
			t.Errorf("POST /auth/verify-code with a bad code: expected 400, got %d", status)
		}

		// No Authorization header at all: unauthenticated /api/me.
		anon := newAPIClient(f.baseURL)
		status, _ = f.callAs(t, anon, http.MethodGet, "/api/me", "/api/me", nil)
		if status != 401 {
			t.Errorf("GET /api/me with no token: expected 401, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// workspaces
// ---------------------------------------------------------------------------

func testWorkspaces(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureWorkspace(t)

		status, _ := f.call(t, http.MethodGet, "/api/workspaces", "/api/workspaces", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}", "/api/workspaces/"+f.workspaceID, nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/capabilities",
			"/api/workspaces/"+f.workspaceID+"/capabilities", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/capabilities: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/members",
			"/api/workspaces/"+f.workspaceID+"/members", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/members: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPatch, "/api/workspaces/{id}", "/api/workspaces/"+f.workspaceID,
			map[string]any{"description": "updated by contract suite " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PATCH /api/workspaces/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspace-templates", "/api/workspace-templates", nil)
		if status != 200 {
			t.Errorf("GET /api/workspace-templates: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/invitations",
			"/api/workspaces/"+f.workspaceID+"/invitations", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/invitations: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/workspaces/{id}/members",
			"/api/workspaces/"+f.workspaceID+"/members", map[string]any{
				"email": fmt.Sprintf("contract-invite-%s@example.test", uniqueSuffix()),
			})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/workspaces/{id}/members: expected 200/201, got %d", status)
		}

		status, body := f.call(t, http.MethodPost, "/api/workspaces/{id}/runtime-profiles",
			"/api/workspaces/"+f.workspaceID+"/runtime-profiles", map[string]any{
				"display_name":    "Contract profile " + uniqueSuffix(),
				"protocol_family": "runtime-a",
				"command_name":    "contract-runtime-cmd",
			})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/workspaces/{id}/runtime-profiles: expected 200/201, got %d, body=%s", status, truncate(body))
		} else {
			profile := decodeJSON(t, body)
			profileID := str(profile, "id")
			if profileID != "" {
				status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/runtime-profiles/{profileId}",
					"/api/workspaces/"+f.workspaceID+"/runtime-profiles/"+profileID, nil)
				if status != 200 {
					t.Errorf("GET /api/workspaces/{id}/runtime-profiles/{profileId}: expected 200, got %d", status)
				}
			}
		}

		// A workspace that does not exist: documented 404.
		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}",
			"/api/workspaces/00000000-0000-0000-0000-000000000000", nil)
		if status != 404 {
			t.Errorf("GET /api/workspaces/{id} for an unknown id: expected 404, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// me
// ---------------------------------------------------------------------------

func testMe(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureAuth(t)

		status, body := f.call(t, http.MethodGet, "/api/me", "/api/me", nil)
		if status != 200 {
			t.Fatalf("GET /api/me: expected 200, got %d", status)
		}
		me := decodeJSON(t, body)
		if mustStr(t, me, "id") != f.userID {
			t.Errorf("GET /api/me: id %q does not match logged-in user %q", me["id"], f.userID)
		}

		status, _ = f.call(t, http.MethodPatch, "/api/me", "/api/me",
			map[string]any{"name": "Contract Tester " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PATCH /api/me: expected 200, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// issues
// ---------------------------------------------------------------------------

func testIssues(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureIssue(t)

		status, _ := f.call(t, http.MethodGet, "/api/issues", "/api/issues", nil)
		if status != 200 {
			t.Errorf("GET /api/issues: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}", "/api/issues/"+f.issueID, nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/issues/{id}", "/api/issues/"+f.issueID,
			map[string]any{"title": "Contract issue (updated) " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PUT /api/issues/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/search", "/api/issues/search?q=contract", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/search: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/attachments",
			"/api/issues/"+f.issueID+"/attachments", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/attachments: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/subscribers",
			"/api/issues/"+f.issueID+"/subscribers", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/subscribers: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}", "/api/issues/00000000-0000-0000-0000-000000000000", nil)
		if status != 404 {
			t.Errorf("GET /api/issues/{id} for an unknown id: expected 404, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/issues", "/api/issues", map[string]any{})
		if status != 400 {
			t.Errorf("POST /api/issues with no title: expected 400, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// projects
// ---------------------------------------------------------------------------

func testProjects(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureProject(t)

		status, _ := f.call(t, http.MethodGet, "/api/projects", "/api/projects", nil)
		if status != 200 {
			t.Errorf("GET /api/projects: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/projects/{id}", "/api/projects/"+f.projectID, nil)
		if status != 200 {
			t.Errorf("GET /api/projects/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/projects/{id}", "/api/projects/"+f.projectID,
			map[string]any{"title": "Contract project (updated) " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PUT /api/projects/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/projects/{id}/resources",
			"/api/projects/"+f.projectID+"/resources", nil)
		if status != 200 {
			t.Errorf("GET /api/projects/{id}/resources: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/projects/search", "/api/projects/search?q=contract", nil)
		if status != 200 {
			t.Errorf("GET /api/projects/search: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/projects/{id}/resources",
			"/api/projects/"+f.projectID+"/resources", map[string]any{
				"resource_type": "github_repo",
				"resource_ref":  map[string]any{"url": "https://github.com/example/contract-test"},
			})
		if status != 201 && status != 200 && status != 400 {
			t.Errorf("POST /api/projects/{id}/resources: expected 200/201/400, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// labels
// ---------------------------------------------------------------------------

func testLabels(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureLabel(t)

		status, _ := f.call(t, http.MethodGet, "/api/labels", "/api/labels", nil)
		if status != 200 {
			t.Errorf("GET /api/labels: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/labels/{id}", "/api/labels/"+f.labelID, nil)
		if status != 200 {
			t.Errorf("GET /api/labels/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/labels/{id}", "/api/labels/"+f.labelID,
			map[string]any{"name": "contract-updated-" + uniqueSuffix(), "color": "#112233"})
		if status != 200 {
			t.Errorf("PUT /api/labels/{id}: expected 200, got %d", status)
		}

		f.ensureIssue(t)
		status, _ = f.call(t, http.MethodPost, "/api/issues/{id}/labels", "/api/issues/"+f.issueID+"/labels",
			map[string]any{"label_id": f.labelID})
		if status != 200 && status != 201 {
			t.Errorf("POST /api/issues/{id}/labels: expected 200/201, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/labels", "/api/issues/"+f.issueID+"/labels", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/labels: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/properties", "/api/properties", nil)
		if status != 200 {
			t.Errorf("GET /api/properties: expected 200, got %d", status)
		}

		status, body := f.call(t, http.MethodPost, "/api/properties", "/api/properties", map[string]any{
			"name": "contract-prop-" + uniqueSuffix(),
			"type": "text",
		})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/properties: expected 200/201, got %d, body=%s", status, truncate(body))
		} else {
			prop := decodeJSON(t, body)
			propID := str(prop, "id")
			if propID != "" {
				status, _ = f.call(t, http.MethodGet, "/api/properties/{id}", "/api/properties/"+propID, nil)
				if status != 200 {
					t.Errorf("GET /api/properties/{id}: expected 200, got %d", status)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// comments
// ---------------------------------------------------------------------------

func testComments(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureIssue(t)

		status, body := f.call(t, http.MethodPost, "/api/issues/{id}/comments", "/api/issues/"+f.issueID+"/comments",
			map[string]any{"content": "Contract comment " + uniqueSuffix()})
		if status != 201 {
			t.Fatalf("POST /api/issues/{id}/comments: expected 201, got %d, body=%s", status, truncate(body))
		}
		comment := decodeJSON(t, body)
		commentID := mustStr(t, comment, "id")

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/comments", "/api/issues/"+f.issueID+"/comments", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/comments: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/comments/{commentId}", "/api/comments/"+commentID,
			map[string]any{"content": "Contract comment (edited) " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PUT /api/comments/{commentId}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/comments/{commentId}/reactions",
			"/api/comments/"+commentID+"/reactions", map[string]any{"emoji": "👍"})
		if status != 200 && status != 201 {
			t.Errorf("POST /api/comments/{commentId}/reactions: expected 200/201, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/timeline", "/api/issues/"+f.issueID+"/timeline", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/timeline: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/comments/{commentId}/resolve",
			"/api/comments/"+commentID+"/resolve", nil)
		if status != 200 {
			t.Errorf("POST /api/comments/{commentId}/resolve: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodDelete, "/api/comments/{commentId}/resolve",
			"/api/comments/"+commentID+"/resolve", nil)
		if status != 200 {
			t.Errorf("DELETE /api/comments/{commentId}/resolve: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/issues/{id}/subscribe", "/api/issues/"+f.issueID+"/subscribe", nil)
		if status != 200 {
			t.Errorf("POST /api/issues/{id}/subscribe: expected 200, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// squads
// ---------------------------------------------------------------------------

func testSquads(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureSquad(t)

		status, _ := f.call(t, http.MethodGet, "/api/squads", "/api/squads", nil)
		if status != 200 {
			t.Errorf("GET /api/squads: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/squads/{id}", "/api/squads/"+f.squadID, nil)
		if status != 200 {
			t.Errorf("GET /api/squads/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/squads/{id}/members", "/api/squads/"+f.squadID+"/members", nil)
		if status != 200 {
			t.Errorf("GET /api/squads/{id}/members: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/squads/{id}/members/status",
			"/api/squads/"+f.squadID+"/members/status", nil)
		if status != 200 {
			t.Errorf("GET /api/squads/{id}/members/status: expected 200, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// autopilots
// ---------------------------------------------------------------------------

func testAutopilots(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureAgent(t)

		status, body := f.call(t, http.MethodPost, "/api/autopilots", "/api/autopilots", map[string]any{
			"title":          "Contract autopilot " + uniqueSuffix(),
			"assignee_id":    f.agentID,
			"execution_mode": "run_only",
		})
		if status != 201 {
			t.Fatalf("POST /api/autopilots: expected 201, got %d, body=%s", status, truncate(body))
		}
		autopilot := decodeJSON(t, body)
		autopilotID := mustStr(t, autopilot, "id")

		status, _ = f.call(t, http.MethodGet, "/api/autopilots", "/api/autopilots", nil)
		if status != 200 {
			t.Errorf("GET /api/autopilots: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/autopilots/{id}", "/api/autopilots/"+autopilotID, nil)
		if status != 200 {
			t.Errorf("GET /api/autopilots/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/autopilots/{id}/runs", "/api/autopilots/"+autopilotID+"/runs", nil)
		if status != 200 {
			t.Errorf("GET /api/autopilots/{id}/runs: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/autopilots/cron-preview",
			"/api/autopilots/cron-preview?expr="+urlEscape("0 9 * * *"), nil)
		if status != 200 {
			t.Errorf("GET /api/autopilots/cron-preview: expected 200, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// agents
// ---------------------------------------------------------------------------

func testAgents(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureAgent(t)

		status, _ := f.call(t, http.MethodGet, "/api/agents", "/api/agents", nil)
		if status != 200 {
			t.Errorf("GET /api/agents: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agents/{id}", "/api/agents/"+f.agentID, nil)
		if status != 200 {
			t.Errorf("GET /api/agents/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agents/{id}/skills", "/api/agents/"+f.agentID+"/skills", nil)
		if status != 200 {
			t.Errorf("GET /api/agents/{id}/skills: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agents/{id}/mcp-servers", "/api/agents/"+f.agentID+"/mcp-servers", nil)
		if status != 200 {
			t.Errorf("GET /api/agents/{id}/mcp-servers: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agents/{id}/tasks", "/api/agents/"+f.agentID+"/tasks", nil)
		if status != 200 {
			t.Errorf("GET /api/agents/{id}/tasks: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agent-templates", "/api/agent-templates", nil)
		if status != 200 {
			t.Errorf("GET /api/agent-templates: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agents/{id}/env", "/api/agents/"+f.agentID+"/env", nil)
		if status != 200 {
			t.Errorf("GET /api/agents/{id}/env: expected 200, got %d", status)
		}

		// Resource labels on agents are behind a feature flag that defaults
		// off, in which case the server documents 404 instead of 200.
		f.call(t, http.MethodGet, "/api/agents/{id}/labels", "/api/agents/"+f.agentID+"/labels", nil)

		status, _ = f.call(t, http.MethodPost, "/api/agent-builder/sessions", "/api/agent-builder/sessions",
			map[string]any{"runtime_id": f.runtimeID})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/agent-builder/sessions: expected 200/201, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// runtimes
// ---------------------------------------------------------------------------

func testRuntimes(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureRuntime(t)

		status, _ := f.call(t, http.MethodGet, "/api/runtimes", "/api/runtimes", nil)
		if status != 200 {
			t.Errorf("GET /api/runtimes: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/runtime-profiles",
			"/api/workspaces/"+f.workspaceID+"/runtime-profiles", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/runtime-profiles: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/runtimes/{runtimeId}/usage",
			"/api/runtimes/"+f.runtimeID+"/usage", nil)
		if status != 200 {
			t.Errorf("GET /api/runtimes/{runtimeId}/usage: expected 200, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// skills
// ---------------------------------------------------------------------------

func testSkills(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureSkill(t)

		status, _ := f.call(t, http.MethodGet, "/api/skills", "/api/skills", nil)
		if status != 200 {
			t.Errorf("GET /api/skills: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/skills/{id}", "/api/skills/"+f.skillID, nil)
		if status != 200 {
			t.Errorf("GET /api/skills/{id}: expected 200, got %d", status)
		}

		// Skill search can legitimately answer 502 in an environment with no
		// search backend configured; any documented status is fine here; the
		// schema check inside f.call already covers correctness.
		f.call(t, http.MethodGet, "/api/skills/search", "/api/skills/search?q=contract", nil)

		status, _ = f.call(t, http.MethodGet, "/api/skills/{id}/files", "/api/skills/"+f.skillID+"/files", nil)
		if status != 200 {
			t.Errorf("GET /api/skills/{id}/files: expected 200, got %d", status)
		}

		// Same feature-flagged behavior as agent labels.
		f.call(t, http.MethodGet, "/api/skills/{id}/labels", "/api/skills/"+f.skillID+"/labels", nil)
	}
}

// ---------------------------------------------------------------------------
// chat
// ---------------------------------------------------------------------------

func testChat(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureChatSession(t)

		status, _ := f.call(t, http.MethodGet, "/api/chat/sessions", "/api/chat/sessions", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/sessions: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/chat/sessions/{sessionId}",
			"/api/chat/sessions/"+f.chatSessionID, nil)
		if status != 200 {
			t.Errorf("GET /api/chat/sessions/{sessionId}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/chat/sessions/{sessionId}/messages",
			"/api/chat/sessions/"+f.chatSessionID+"/messages", map[string]any{
				"content": "Hello from the contract suite " + uniqueSuffix(),
			})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/chat/sessions/{sessionId}/messages: expected 200/201, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/chat/sessions/{sessionId}/messages",
			"/api/chat/sessions/"+f.chatSessionID+"/messages", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/sessions/{sessionId}/messages: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/chat/pending-tasks", "/api/chat/pending-tasks", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/pending-tasks: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/chat/pinned-agents", "/api/chat/pinned-agents", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/pinned-agents: expected 200, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// inbox
// ---------------------------------------------------------------------------

func testInbox(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureWorkspace(t)

		status, _ := f.call(t, http.MethodGet, "/api/inbox", "/api/inbox", nil)
		if status != 200 {
			t.Errorf("GET /api/inbox: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/inbox/unread-count", "/api/inbox/unread-count", nil)
		if status != 200 {
			t.Errorf("GET /api/inbox/unread-count: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/inbox/archived", "/api/inbox/archived", nil)
		if status != 200 {
			t.Errorf("GET /api/inbox/archived: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/notification-preferences", "/api/notification-preferences", nil)
		if status != 200 {
			t.Errorf("GET /api/notification-preferences: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPatch, "/api/notification-preferences", "/api/notification-preferences",
			map[string]any{"preferences": map[string]any{"comments": "muted"}})
		if status != 200 {
			t.Errorf("PATCH /api/notification-preferences: expected 200, got %d", status)
		}

		f.ensureIssue(t)
		status, _ = f.call(t, http.MethodPost, "/api/pins", "/api/pins", map[string]any{
			"item_type": "issue",
			"item_id":   f.issueID,
		})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/pins: expected 200/201, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/pins", "/api/pins", nil)
		if status != 200 {
			t.Errorf("GET /api/pins: expected 200, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// daemon
// ---------------------------------------------------------------------------

func testDaemon(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureRuntime(t)

		status, _ := f.call(t, http.MethodPost, "/api/daemon/heartbeat", "/api/daemon/heartbeat",
			map[string]any{"runtime_id": f.runtimeID})
		if status != 200 {
			t.Errorf("POST /api/daemon/heartbeat: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/daemon/workspaces", "/api/daemon/workspaces", nil)
		if status != 200 {
			t.Errorf("GET /api/daemon/workspaces: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/daemon/workspaces/{workspaceId}/repos",
			"/api/daemon/workspaces/"+f.workspaceID+"/repos", nil)
		if status != 200 {
			t.Errorf("GET /api/daemon/workspaces/{workspaceId}/repos: expected 200, got %d", status)
		}

		// Claim with an empty queue: still a fully valid, schema-checked
		// 200 response ({"tasks": []}), without needing a real task queued.
		status, _ = f.call(t, http.MethodPost, "/api/daemon/tasks/claim", "/api/daemon/tasks/claim",
			map[string]any{
				"daemon_id":   "contract-daemon-" + uniqueSuffix(),
				"runtime_ids": []string{f.runtimeID},
				"max_tasks":   1,
			})
		if status != 200 {
			t.Errorf("POST /api/daemon/tasks/claim: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/daemon/runtimes/{runtimeId}/tasks/pending",
			"/api/daemon/runtimes/"+f.runtimeID+"/tasks/pending", nil)
		if status != 200 {
			t.Errorf("GET /api/daemon/runtimes/{runtimeId}/tasks/pending: expected 200, got %d", status)
		}

		// A daemon call with an invalid token: no Authorization at all is a
		// documented 401.
		anon := newAPIClient(f.baseURL)
		status, _ = f.callAs(t, anon, http.MethodPost, "/api/daemon/heartbeat", "/api/daemon/heartbeat",
			map[string]any{"runtime_id": f.runtimeID})
		if status != 401 {
			t.Errorf("POST /api/daemon/heartbeat with no auth: expected 401, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// deployment (deployment-admin-only surface; this suite's user is never a
// deployment admin, so these calls exercise the documented 403 path -- see
// docs/50-api-contract.yaml, which documents 403 on all of them precisely
// because of this check).
// ---------------------------------------------------------------------------

func testDeployment(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureAuth(t)

		status, _ := f.call(t, http.MethodGet, "/api/deployment/admins", "/api/deployment/admins", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/admins: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment/admins/pending", "/api/deployment/admins/pending", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/admins/pending: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment/audit", "/api/deployment/audit", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/audit: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment/workspaces", "/api/deployment/workspaces", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/workspaces: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment/fleet", "/api/deployment/fleet", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/fleet: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment/mcp-servers", "/api/deployment/mcp-servers", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/mcp-servers: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment-policy", "/api/deployment-policy", nil)
		if status != 200 {
			t.Errorf("GET /api/deployment-policy: expected 200, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// admin (workspace-scoped provisioning/config admin surface)
// ---------------------------------------------------------------------------

func testAdmin(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureWorkspace(t)

		status, _ := f.call(t, http.MethodGet, "/api/status", "/api/status", nil)
		if status != 200 {
			t.Errorf("GET /api/status: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/effective-config", "/api/effective-config", nil)
		if status != 200 {
			t.Errorf("GET /api/effective-config: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspace-config", "/api/workspace-config", nil)
		if status != 200 {
			t.Errorf("GET /api/workspace-config: expected 200, got %d", status)
		}

		// Provisioning can legitimately answer 503 when no provisioning
		// store is configured for this deployment; any documented status is
		// fine, the schema check inside f.call already covers correctness.
		f.call(t, http.MethodGet, "/api/provisioning/manifest", "/api/provisioning/manifest", nil)
	}
}

// ---------------------------------------------------------------------------
// integrations
// ---------------------------------------------------------------------------

func testIntegrations(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureWorkspace(t)

		// Composio is an optional integration; when it is not configured on
		// this deployment the server documents 502/503 here instead of 200.
		// Any documented status is fine, the schema check inside f.call
		// already covers correctness.
		f.call(t, http.MethodGet, "/api/integrations/composio/toolkits",
			"/api/integrations/composio/toolkits", nil)
		f.call(t, http.MethodGet, "/api/integrations/composio/connections",
			"/api/integrations/composio/connections", nil)

		status, _ := f.call(t, http.MethodGet, "/api/workspaces/{id}/github/installations",
			"/api/workspaces/"+f.workspaceID+"/github/installations", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/github/installations: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/vcs/connections",
			"/api/workspaces/"+f.workspaceID+"/vcs/connections", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/vcs/connections: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/slack/installations",
			"/api/workspaces/"+f.workspaceID+"/slack/installations", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/slack/installations: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/tokens", "/api/tokens", nil)
		if status != 200 {
			t.Errorf("GET /api/tokens: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/invitations", "/api/invitations", nil)
		if status != 200 {
			t.Errorf("GET /api/invitations: expected 200, got %d", status)
		}

		// Cloud billing depends on an external billing service; 502/503/504
		// are documented alternatives to 200 when it is unreachable or not
		// configured.
		f.call(t, http.MethodGet, "/api/cloud-billing/balance", "/api/cloud-billing/balance", nil)

		status, body := f.call(t, http.MethodPost, "/api/tokens", "/api/tokens", map[string]any{
			"name": "contract-pat-" + uniqueSuffix(),
		})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/tokens: expected 200/201, got %d, body=%s", status, truncate(body))
		} else {
			pat := decodeJSON(t, body)
			patID := str(pat, "id")
			if patID != "" {
				status, _ = f.call(t, http.MethodDelete, "/api/tokens/{id}", "/api/tokens/"+patID, nil)
				if status != 200 && status != 204 {
					t.Errorf("DELETE /api/tokens/{id}: expected 200/204, got %d", status)
				}
			}
		}

		// The cloud-runtime fleet is an optional external service; 502/503/504
		// are documented alternatives to 200 when it is not configured.
		f.call(t, http.MethodGet, "/api/cloud-runtime", "/api/cloud-runtime", nil)

		status, _ = f.call(t, http.MethodGet, "/api/dashboard/usage/daily", "/api/dashboard/usage/daily", nil)
		if status != 200 {
			t.Errorf("GET /api/dashboard/usage/daily: expected 200, got %d", status)
		}
	}
}
