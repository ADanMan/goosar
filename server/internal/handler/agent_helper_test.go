package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func helperTestWorkspace(t *testing.T, slug string) string {
	t.Helper()
	ctx := context.Background()

	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, 'helper provisioning test', 'HLP')
		RETURNING id
	`, "Helper "+slug, slug).Scan(&wsID); err != nil {
		t.Fatalf("create workspace %s: %v", slug, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("add owner to %s: %v", slug, err)
	}
	return wsID
}

type helperRuntimeSpec struct {
	name       string
	provider   string
	status     string
	visibility string
	ownerID    string
}

func helperTestRuntime(t *testing.T, wsID string, spec helperRuntimeSpec) string {
	t.Helper()

	if spec.visibility == "" {
		spec.visibility = "private"
	}
	if spec.ownerID == "" {
		spec.ownerID = testUserID
	}

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, $4, 'helper test runtime', '{}'::jsonb, $5, $6, now())
		RETURNING id
	`, wsID, spec.name, spec.provider, spec.status, spec.ownerID, spec.visibility).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime %s: %v", spec.name, err)
	}
	return runtimeID
}

func helperTestMember(t *testing.T, wsID, email, displayName string) string {
	t.Helper()
	ctx := context.Background()

	_, _ = testPool.Exec(ctx, `DELETE FROM member WHERE user_id IN (SELECT id FROM "user" WHERE email = $1)`, email)
	_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, displayName, email,
	).Scan(&userID); err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	t.Cleanup(func() {

		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, wsID, userID,
	); err != nil {
		t.Fatalf("add member %s: %v", email, err)
	}
	return userID
}

type helperAgentRow struct {
	ID                 string
	Name               string
	Kind               string
	SystemKey          string
	Status             string
	Visibility         string
	PermissionMode     string
	MaxConcurrentTasks int32
	AvatarURL          string
	Description        string
	Instructions       string
	RuntimeID          string
	OwnerID            string
}

func helperAgentRows(t *testing.T, wsID string) []helperAgentRow {
	t.Helper()

	rows, err := testPool.Query(context.Background(), `
		SELECT id, name, kind, COALESCE(system_key, ''), status, visibility,
		       permission_mode, max_concurrent_tasks, COALESCE(avatar_url, ''),
		       description, instructions, runtime_id, owner_id
		FROM agent
		WHERE workspace_id = $1 AND system_key = 'goosar_helper' AND archived_at IS NULL
		ORDER BY created_at ASC
	`, wsID)
	if err != nil {
		t.Fatalf("query helper agents: %v", err)
	}
	defer rows.Close()

	var out []helperAgentRow
	for rows.Next() {
		var r helperAgentRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.SystemKey, &r.Status, &r.Visibility,
			&r.PermissionMode, &r.MaxConcurrentTasks, &r.AvatarURL, &r.Description,
			&r.Instructions, &r.RuntimeID, &r.OwnerID); err != nil {
			t.Fatalf("scan helper agent: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate helper agents: %v", err)
	}
	return out
}

func helperRowFor(t *testing.T, wsID, userID string) helperAgentRow {
	t.Helper()
	var found []helperAgentRow
	for _, row := range helperAgentRows(t, wsID) {
		if row.OwnerID == userID {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one Helper owned by %s, got %d", userID, len(found))
	}
	return found[0]
}

func countWorkspaceAgents(t *testing.T, wsID string) int {
	t.Helper()
	var total int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent WHERE workspace_id = $1`, wsID,
	).Scan(&total); err != nil {
		t.Fatalf("count agents: %v", err)
	}
	return total
}

func countIntroChatSessions(t *testing.T, agentID string) int {
	t.Helper()
	var total int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_session WHERE agent_id = $1 AND is_agent_intro = true`, agentID,
	).Scan(&total); err != nil {
		t.Fatalf("count intro sessions: %v", err)
	}
	return total
}

func listAgentsRecorder(userID, wsID string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/agents", nil)
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Workspace-ID", wsID)
	testHandler.ListAgents(w, req)
	return w
}

func listAgentsRequest(t *testing.T, userID, wsID string) []AgentResponse {
	t.Helper()

	w := listAgentsRecorder(userID, wsID)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var agents []AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &agents); err != nil {
		t.Fatalf("decode agents: %v (body %s)", err, w.Body.String())
	}
	return agents
}

func daemonRegisterBody(t *testing.T, wsID, daemonID, provider string) *bytes.Buffer {
	t.Helper()
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(map[string]any{
		"workspace_id": wsID,
		"daemon_id":    daemonID,
		"device_name":  "helper-test-device",
		"runtimes": []map[string]any{
			{"name": provider + " (" + daemonID + ")", "type": provider, "version": "1.0.0", "status": "online"},
		},
	}); err != nil {
		t.Fatalf("encode register body: %v", err)
	}
	return &body
}

func registerDaemonAs(t *testing.T, userID, wsID, daemonID, provider string) {
	t.Helper()

	req := httptest.NewRequest("POST", "/api/daemon/register", daemonRegisterBody(t, wsID, daemonID, provider))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)

	w := httptest.NewRecorder()
	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DaemonRegister: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDaemonRegister_ProvisionsHelperForRegisteringMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-daemon-register")
	if rows := helperAgentRows(t, wsID); len(rows) != 0 {
		t.Fatalf("precondition: workspace must start without a Helper, got %d", len(rows))
	}

	registerDaemonAs(t, testUserID, wsID, "helper-test-daemon-register", "runtime-j")

	rows := helperAgentRows(t, wsID)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one Helper after the first runtime registered, got %d", len(rows))
	}
	got := rows[0]

	var registeredRuntimeID, registeredProvider string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id, provider FROM agent_runtime WHERE workspace_id = $1`, wsID,
	).Scan(&registeredRuntimeID, &registeredProvider); err != nil {
		t.Fatalf("read registered runtime: %v", err)
	}

	if got.RuntimeID != registeredRuntimeID {
		t.Fatalf("runtime_id = %s, want the just-registered runtime %s", got.RuntimeID, registeredRuntimeID)
	}
	if got.Name != helperAgentName {
		t.Fatalf("name = %q, want %q — the workspace's first Helper keeps the product's own name", got.Name, helperAgentName)
	}
	if got.SystemKey != helperAgentSystemKey {
		t.Fatalf("system_key = %q, want %q", got.SystemKey, helperAgentSystemKey)
	}
	if got.Kind != "user" {
		t.Fatalf("kind = %q, want \"user\" (ListAgents filters kind='user')", got.Kind)
	}

	if got.Visibility != "private" {
		t.Fatalf("visibility = %q, want \"private\" — registering a private machine must not publish it", got.Visibility)
	}
	if got.PermissionMode != "private" {
		t.Fatalf("permission_mode = %q, want \"private\" — a Helper on an unpublished machine is its owner's (plus workspace owner/admin, as with any private agent)", got.PermissionMode)
	}
	if got.MaxConcurrentTasks != helperAgentMaxConcurrentTasks {
		t.Fatalf("max_concurrent_tasks = %d, want %d", got.MaxConcurrentTasks, helperAgentMaxConcurrentTasks)
	}
	if got.AvatarURL != helperAgentAvatarURL {
		t.Fatalf("avatar_url does not match the canonical Helper avatar")
	}
	if got.OwnerID != testUserID {
		t.Fatalf("owner_id = %s, want the registering member %s", got.OwnerID, testUserID)
	}
	if got.Instructions == "" || got.Description == "" {
		t.Fatalf("instructions/description must be persisted, got %q / %q", got.Instructions, got.Description)
	}

	var targets int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_invocation_target WHERE agent_id = $1`,
		got.ID,
	).Scan(&targets); err != nil {
		t.Fatalf("count invocation targets: %v", err)
	}
	if targets != 0 {
		t.Fatalf("expected no invocation target on a private-machine Helper, got %d — a workspace target makes the whole workspace an invoker", targets)
	}

	agents := listAgentsRequest(t, testUserID, wsID)
	if len(agents) != 1 || agents[0].ID != got.ID {
		t.Fatalf("expected the Helper in GET /api/agents, got %d agents", len(agents))
	}
	if agents[0].SystemKey != helperAgentSystemKey {
		t.Fatalf("API system_key = %q, want %q — the client identifies a renamed Helper by this field",
			agents[0].SystemKey, helperAgentSystemKey)
	}
}

func TestDaemonRegister_ProvisionsHelperForInvitedMemberOnTheirOwnMachine(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-invited-registers")
	memberID := helperTestMember(t, wsID, "helper-invited-registers@goosar.ru", "Boris Invitee")

	registerDaemonAs(t, memberID, wsID, "helper-test-daemon-invited", "runtime-j")

	rows := helperAgentRows(t, wsID)
	if len(rows) != 1 {
		t.Fatalf("expected the invited member's Helper, got %d Helper rows", len(rows))
	}
	if rows[0].OwnerID != memberID {
		t.Fatalf("owner_id = %s, want the invited member %s — a Helper attributed to the workspace owner cannot live on this machine at all",
			rows[0].OwnerID, memberID)
	}

	var runtimeOwner, runtimeVisibility string
	if err := testPool.QueryRow(context.Background(),
		`SELECT owner_id, visibility FROM agent_runtime WHERE id = $1`, rows[0].RuntimeID,
	).Scan(&runtimeOwner, &runtimeVisibility); err != nil {
		t.Fatalf("read bound runtime: %v", err)
	}
	if runtimeOwner != memberID || runtimeVisibility != "private" {
		t.Fatalf("Helper bound to runtime owner=%s visibility=%s, want the member's own private machine",
			runtimeOwner, runtimeVisibility)
	}

	for _, row := range helperAgentRows(t, wsID) {
		if row.OwnerID == testUserID {
			t.Fatalf("a Helper was provisioned for the workspace owner on someone else's private machine")
		}
	}
}

func TestDaemonRegister_HelperGetsPostCreateSideEffects(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-side-effects")
	registerDaemonAs(t, testUserID, wsID, "helper-test-daemon-effects", "runtime-j")

	rows := helperAgentRows(t, wsID)
	if len(rows) != 1 {
		t.Fatalf("expected one Helper, got %d", len(rows))
	}
	if rows[0].Status != "idle" {
		t.Fatalf("status = %q, want \"idle\": an agent on an online runtime is reconciled at creation, not left on the column DEFAULT", rows[0].Status)
	}
	if n := countIntroChatSessions(t, rows[0].ID); n != 1 {
		t.Fatalf("expected one is_agent_intro chat session, got %d", n)
	}
}

func TestListAgents_BackfillDoesNotQueueAWelcomeChat(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-quiet-backfill")
	helperTestRuntime(t, wsID, helperRuntimeSpec{name: "Quiet Backfill Runtime", provider: "runtime-j", status: "online"})

	listAgentsRequest(t, testUserID, wsID)

	helper := helperRowFor(t, wsID, testUserID)
	if n := countIntroChatSessions(t, helper.ID); n != 0 {
		t.Fatalf("a GET listing queued %d self-introduction chat sessions; reading a workspace must not start an LLM run", n)
	}
	if helper.Status != "idle" {
		t.Fatalf("status = %q, want \"idle\": the cache-facing side effects still run on the quiet path", helper.Status)
	}
}

func TestDaemonRegister_DaemonTokenDoesNotProvisionHelper(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-daemon-token")

	req := httptest.NewRequest("POST", "/api/daemon/register", daemonRegisterBody(t, wsID, "helper-test-daemon-token", "runtime-j"))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.WithDaemonContext(req.Context(), wsID, "helper-test-daemon-token"))

	w := httptest.NewRecorder()
	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DaemonRegister: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := countWorkspaceAgents(t, wsID); n != 0 {
		t.Fatalf("a daemon-token registration provisioned %d agents; there is no member to own one", n)
	}
}

func TestCreateWorkspace_LeavesNoRuntimeForAHelperToBindTo(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const slug = "helper-tests-no-runtime"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, slug)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces", map[string]any{
		"name": "Helper No Runtime",
		"slug": slug,
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkspace: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var wsID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM workspace WHERE slug = $1`, slug).Scan(&wsID); err != nil {
		t.Fatalf("lookup workspace: %v", err)
	}

	var runtimes int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE workspace_id = $1`, wsID).Scan(&runtimes); err != nil {
		t.Fatalf("count runtimes: %v", err)
	}
	if runtimes != 0 {
		t.Fatalf("a freshly created workspace has %d runtimes; Helper provisioning is documented as impossible here precisely because it has none — a placeholder runtime is not the fix", runtimes)
	}
	if n := countWorkspaceAgents(t, wsID); n != 0 {
		t.Fatalf("expected no agent in a workspace with no runtime, got %d", n)
	}
}

func TestListAgents_HelperOnPrivateRuntimeIsHiddenFromPlainMembers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-private-hidden")
	memberID := helperTestMember(t, wsID, "helper-private-hidden@goosar.ru", "Denis Daemon")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:       "Denis Laptop",
		provider:   "runtime-j",
		status:     "online",
		visibility: "private",
		ownerID:    memberID,
	})
	teammateID := helperTestMember(t, wsID, "helper-private-teammate@goosar.ru", "Tanya Teammate")

	if agents := listAgentsRequest(t, memberID, wsID); len(agents) != 1 {
		t.Fatalf("machine owner: expected their own Helper, got %d agents", len(agents))
	}

	if agents := listAgentsRequest(t, teammateID, wsID); len(agents) != 0 {
		t.Fatalf("plain teammate sees %d agents; a Helper on an unpublished machine must stay private", len(agents))
	}
}

func TestListAgents_HelperIsVisibleToInvitedMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-invited")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:       "Helper Invited Runtime",
		provider:   "runtime-j",
		status:     "online",
		visibility: "public",
	})
	inviteeID := helperTestMember(t, wsID, "helper-invitee@goosar.ru", "Irina Invitee")

	if agents := listAgentsRequest(t, testUserID, wsID); len(agents) != 1 {
		t.Fatalf("owner: expected exactly one agent, got %d", len(agents))
	}

	agents := listAgentsRequest(t, inviteeID, wsID)
	if len(agents) != 1 {
		t.Fatalf("invited member: expected to see exactly one agent, got %d", len(agents))
	}
	if agents[0].Name != helperAgentName {
		t.Fatalf("invited member sees %q, want %q", agents[0].Name, helperAgentName)
	}
	if agents[0].OwnerID == nil || *agents[0].OwnerID != testUserID {
		t.Fatalf("invited member should be seeing the OWNER's Helper, not one of their own")
	}

	helper := helperRowFor(t, wsID, testUserID)
	if helper.Visibility != "workspace" || helper.PermissionMode != "public_to" {
		t.Fatalf("Helper on a published machine: visibility=%q permission_mode=%q, want workspace/public_to",
			helper.Visibility, helper.PermissionMode)
	}
	var targets int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_invocation_target WHERE agent_id = $1 AND target_type = 'workspace'`,
		helper.ID,
	).Scan(&targets); err != nil {
		t.Fatalf("count invocation targets: %v", err)
	}
	if targets != 1 {
		t.Fatalf("expected one workspace invocation target on a published-machine Helper, got %d", targets)
	}
}

func TestListAgents_BackfillGivesEachMemberTheirOwnHelper(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-per-member")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:     "Owner Laptop",
		provider: "runtime-j",
		status:   "online",
	})
	memberID := helperTestMember(t, wsID, "helper-per-member@goosar.ru", "Marina Member")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:     "Marina Laptop",
		provider: "runtime-j",
		status:   "online",
		ownerID:  memberID,
	})

	listAgentsRequest(t, testUserID, wsID)
	listAgentsRequest(t, memberID, wsID)

	rows := helperAgentRows(t, wsID)
	if len(rows) != 2 {
		t.Fatalf("expected one Helper per member, got %d", len(rows))
	}

	ownerHelper := helperRowFor(t, wsID, testUserID)
	memberHelper := helperRowFor(t, wsID, memberID)
	if ownerHelper.ID == memberHelper.ID {
		t.Fatalf("both members resolved to the same agent row")
	}
	if ownerHelper.Name != helperAgentName {
		t.Fatalf("the first Helper's name = %q, want the unqualified %q", ownerHelper.Name, helperAgentName)
	}
	if memberHelper.Name == ownerHelper.Name {
		t.Fatalf("two Helpers share the name %q; agent_workspace_name_unique forbids it", memberHelper.Name)
	}
	if !strings.Contains(memberHelper.Name, "Marina Member") {
		t.Fatalf("second Helper name = %q, want it to name its owner so a list of agents says whose it is", memberHelper.Name)
	}
	if memberHelper.SystemKey != helperAgentSystemKey {
		t.Fatalf("second Helper system_key = %q, want %q — identity is the key, not the name", memberHelper.SystemKey, helperAgentSystemKey)
	}
}

func TestListAgents_BackfillFindsARenamedHelperByIdentity(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-renamed")
	helperTestRuntime(t, wsID, helperRuntimeSpec{name: "Renamed Runtime", provider: "runtime-j", status: "online"})

	listAgentsRequest(t, testUserID, wsID)
	helper := helperRowFor(t, wsID, testUserID)

	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET name = 'Секретарь' WHERE id = $1`, helper.ID,
	); err != nil {
		t.Fatalf("rename helper: %v", err)
	}

	listAgentsRequest(t, testUserID, wsID)
	if n := countWorkspaceAgents(t, wsID); n != 1 {
		t.Fatalf("renaming the Helper produced %d agents, want 1", n)
	}
}

func TestListAgents_BackfillRefusesAnotherMembersPrivateRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-private-runtime")
	memberID := helperTestMember(t, wsID, "helper-private-runtime@goosar.ru", "Pavel Private")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:       "Member Laptop",
		provider:   "runtime-j",
		status:     "online",
		visibility: "private",
		ownerID:    memberID,
	})

	if agents := listAgentsRequest(t, testUserID, wsID); len(agents) != 0 {
		t.Fatalf("owner: expected no agent, got %d", len(agents))
	}
	if n := countWorkspaceAgents(t, wsID); n != 0 {
		t.Fatalf("a Helper was provisioned onto another member's private runtime: %d agent rows", n)
	}
}

func TestListAgents_BackfillUsesTheMembersOwnPrivateRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-own-private-runtime")
	memberID := helperTestMember(t, wsID, "helper-own-private@goosar.ru", "Olga Owner")
	runtimeID := helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:       "Olga Laptop",
		provider:   "runtime-j",
		status:     "online",
		visibility: "private",
		ownerID:    memberID,
	})

	listAgentsRequest(t, memberID, wsID)

	helper := helperRowFor(t, wsID, memberID)
	if helper.RuntimeID != runtimeID {
		t.Fatalf("runtime_id = %s, want the member's own machine %s", helper.RuntimeID, runtimeID)
	}
}

func TestListAgents_BackfillRefusesAnotherMembersPublicRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-public-runtime")
	memberID := helperTestMember(t, wsID, "helper-public-runtime@goosar.ru", "Pyotr Publisher")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:       "Shared Build Box",
		provider:   "runtime-j",
		status:     "online",
		visibility: "public",
		ownerID:    memberID,
	})

	if agents := listAgentsRequest(t, testUserID, wsID); len(agents) != 0 {
		t.Fatalf("expected no agent for a member without their own runtime, got %d", len(agents))
	}
	if n := countWorkspaceAgents(t, wsID); n != 0 {
		t.Fatalf("a Helper was provisioned onto another member's published runtime: %d agent rows", n)
	}
}

func TestHelperAgent_OwnerAlwaysMatchesRuntimeOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-owner-invariant")
	publisherID := helperTestMember(t, wsID, "helper-owner-invariant@goosar.ru", "Polina Publisher")

	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:       "Polina Build Box",
		provider:   "runtime-j",
		status:     "online",
		visibility: "public",
		ownerID:    publisherID,
	})
	listAgentsRequest(t, testUserID, wsID)

	ownRuntimeID := helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:     "My Own Laptop",
		provider: "runtime-j",
		status:   "online",
	})
	listAgentsRequest(t, testUserID, wsID)

	rows := helperAgentRows(t, wsID)
	for _, row := range rows {
		var runtimeOwner string
		if err := testPool.QueryRow(context.Background(),
			`SELECT owner_id FROM agent_runtime WHERE id = $1`, row.RuntimeID,
		).Scan(&runtimeOwner); err != nil {
			t.Fatalf("read runtime owner for helper %s: %v", row.ID, err)
		}
		if runtimeOwner != row.OwnerID {
			t.Fatalf("helper %s: owner_id = %s but its runtime belongs to %s — the CLI this agent's instructions describe is authenticated as someone else",
				row.ID, row.OwnerID, runtimeOwner)
		}
	}

	helper := helperRowFor(t, wsID, testUserID)
	if helper.RuntimeID != ownRuntimeID {
		t.Fatalf("runtime_id = %s, want the member's own machine %s", helper.RuntimeID, ownRuntimeID)
	}
}

func TestListAgents_BackfillPrefersTheMembersOwnRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-own-runtime-first")
	publisherID := helperTestMember(t, wsID, "helper-runtime-publisher@goosar.ru", "Polina Publisher")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:       "Shared Build Box",
		provider:   "runtime-j",
		status:     "online",
		visibility: "public",
		ownerID:    publisherID,
	})
	ownID := helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:       "My Laptop",
		provider:   "runtime-j",
		status:     "online",
		visibility: "private",
		ownerID:    testUserID,
	})

	listAgentsRequest(t, testUserID, wsID)

	helper := helperRowFor(t, wsID, testUserID)
	if helper.RuntimeID != ownID {
		t.Fatalf("runtime_id = %s, want the member's own machine %s rather than a shared one", helper.RuntimeID, ownID)
	}
}

func TestListAgents_BackfillRefusesOfflineRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-offline-runtime")
	helperTestRuntime(t, wsID, helperRuntimeSpec{name: "Dead Laptop", provider: "runtime-j", status: "offline"})

	if agents := listAgentsRequest(t, testUserID, wsID); len(agents) != 0 {
		t.Fatalf("expected no agent on an offline-only workspace, got %d", len(agents))
	}
	if n := countWorkspaceAgents(t, wsID); n != 0 {
		t.Fatalf("a Helper was bound to an offline runtime: %d agent rows", n)
	}
}

func TestListAgents_BackfillPrefersOnlineHermesRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-runtime-order")
	helperTestRuntime(t, wsID, helperRuntimeSpec{name: "Claude First", provider: "runtime-c", status: "online"})
	hermesID := helperTestRuntime(t, wsID, helperRuntimeSpec{name: "Hermes Second", provider: "runtime-j", status: "online"})

	listAgentsRequest(t, testUserID, wsID)

	helper := helperRowFor(t, wsID, testUserID)
	if helper.RuntimeID != hermesID {
		t.Fatalf("runtime_id = %s, want the hermes runtime %s", helper.RuntimeID, hermesID)
	}
}

func TestListAgents_BackfillsHelperForLegacyWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-backfill")
	runtimeID := helperTestRuntime(t, wsID, helperRuntimeSpec{name: "Helper Backfill Runtime", provider: "runtime-j", status: "online"})

	if rows := helperAgentRows(t, wsID); len(rows) != 0 {
		t.Fatalf("precondition: workspace must start without a Helper, got %d", len(rows))
	}

	agents := listAgentsRequest(t, testUserID, wsID)
	if len(agents) != 1 {
		t.Fatalf("expected the backfilled Helper in the response, got %d agents", len(agents))
	}
	if agents[0].Name != helperAgentName {
		t.Fatalf("agent name = %q, want %q", agents[0].Name, helperAgentName)
	}

	helper := helperRowFor(t, wsID, testUserID)
	if helper.RuntimeID != runtimeID {
		t.Fatalf("runtime_id = %s, want %s", helper.RuntimeID, runtimeID)
	}
}

func TestListAgents_BackfillSkipsMemberWhoAlreadyOwnsAnAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	wsID := helperTestWorkspace(t, "helper-tests-existing-agent")
	runtimeID := helperTestRuntime(t, wsID, helperRuntimeSpec{name: "Helper Existing Runtime", provider: "runtime-j", status: "online"})

	var legacyID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', 6, $4)
		RETURNING id
	`, wsID, helperAgentName, runtimeID, testUserID).Scan(&legacyID); err != nil {
		t.Fatalf("seed legacy client-created Helper: %v", err)
	}

	agents := listAgentsRequest(t, testUserID, wsID)
	if len(agents) != 1 {
		t.Fatalf("expected the existing agent only, got %d", len(agents))
	}
	if agents[0].ID != legacyID {
		t.Fatalf("backfill replaced the existing agent: got %s, want %s", agents[0].ID, legacyID)
	}
	if n := countWorkspaceAgents(t, wsID); n != 1 {
		t.Fatalf("expected the workspace to keep exactly one agent, got %d", n)
	}
}

func TestListAgents_BackfillSkipsMemberWhoArchivedTheirOwnHelper(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	wsID := helperTestWorkspace(t, "helper-tests-archived")
	helperTestRuntime(t, wsID, helperRuntimeSpec{name: "Helper Archived Runtime", provider: "runtime-j", status: "online"})

	listAgentsRequest(t, testUserID, wsID)
	helper := helperRowFor(t, wsID, testUserID)

	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET archived_at = now(), archived_by = $2 WHERE id = $1`, helper.ID, testUserID,
	); err != nil {
		t.Fatalf("archive helper: %v", err)
	}

	if agents := listAgentsRequest(t, testUserID, wsID); len(agents) != 0 {
		t.Fatalf("expected no active agents, got %d", len(agents))
	}
	if n := countWorkspaceAgents(t, wsID); n != 1 {
		t.Fatalf("the archived Helper was resurrected or duplicated: %d agent rows", n)
	}
}

func TestListAgents_BackfillIgnoresAnUnrelatedArchivedAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	wsID := helperTestWorkspace(t, "helper-tests-archived-agents")
	runtimeID := helperTestRuntime(t, wsID, helperRuntimeSpec{name: "Helper Archived Runtime", provider: "runtime-j", status: "online"})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			archived_at, archived_by
		)
		VALUES ($1, 'Deploy Bot', '', 'cloud', '{}'::jsonb, $2, 'workspace', 'public_to', 6, $3, now(), $3)
	`, wsID, runtimeID, testUserID); err != nil {
		t.Fatalf("seed archived agent: %v", err)
	}

	if agents := listAgentsRequest(t, testUserID, wsID); len(agents) != 1 {
		t.Fatalf("expected the Helper to be provisioned anyway, got %d active agents", len(agents))
	}
	helperRowFor(t, wsID, testUserID)
}

func TestListAgents_BackfillAfterRevocationAndReinvite(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	wsID := helperTestWorkspace(t, "helper-tests-reinvite")
	memberID := helperTestMember(t, wsID, "helper-reinvite@goosar.ru", "Rita Returned")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:       "Rita Laptop",
		provider:   "runtime-j",
		status:     "online",
		visibility: "private",
		ownerID:    memberID,
	})

	listAgentsRequest(t, memberID, wsID)
	original := helperRowFor(t, wsID, memberID)

	var memberRowID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, wsID, memberID,
	).Scan(&memberRowID); err != nil {
		t.Fatalf("read member row: %v", err)
	}
	if _, err := testHandler.revokeAndRemoveMember(ctx, parseUUID(wsID), parseUUID(memberID), parseUUID(memberRowID), parseUUID(testUserID)); err != nil {
		t.Fatalf("revoke member: %v", err)
	}

	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, wsID, memberID,
	); err != nil {
		t.Fatalf("re-invite member: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_runtime SET status = 'online' WHERE workspace_id = $1 AND owner_id = $2`, wsID, memberID,
	); err != nil {
		t.Fatalf("bring runtime back online: %v", err)
	}

	listAgentsRequest(t, memberID, wsID)

	restored := helperRowFor(t, wsID, memberID)
	if restored.ID == original.ID {
		t.Fatalf("the archived row was resurrected in place instead of a new Helper being provisioned")
	}
	if restored.Name == original.Name {
		t.Fatalf("the new Helper reused the archived Helper's name %q; agent_workspace_name_unique covers archived rows", restored.Name)
	}
}

func TestListAgents_BackfillIsIdempotent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-idempotent")
	helperTestRuntime(t, wsID, helperRuntimeSpec{name: "Helper Idempotent Runtime", provider: "runtime-j", status: "online"})

	listAgentsRequest(t, testUserID, wsID)
	listAgentsRequest(t, testUserID, wsID)
	if rows := helperAgentRows(t, wsID); len(rows) != 1 {
		t.Fatalf("sequential visits produced %d Helpers, want 1", len(rows))
	}
}

func TestListAgents_ConcurrentFirstVisitsCreateOneHelperPerMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-concurrent")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:     "Helper Concurrent Runtime",
		provider: "runtime-j",
		status:   "online",
	})
	memberID := helperTestMember(t, wsID, "helper-concurrent@goosar.ru", "Konstantin Concurrent")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:     "Konstantin Laptop",
		provider: "runtime-j",
		status:   "online",
		ownerID:  memberID,
	})

	const callers = 6
	codes := make([]int, callers)
	bodies := make([]string, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			userID := testUserID
			if i%2 == 1 {
				userID = memberID
			}
			<-start
			w := listAgentsRecorder(userID, wsID)
			codes[i] = w.Code
			bodies[i] = w.Body.String()
		}(i)
	}
	close(start)
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("caller %d: ListAgents returned %d: %s", i, code, bodies[i])
		}
	}
	rows := helperAgentRows(t, wsID)
	if len(rows) != 2 {
		t.Fatalf("concurrent first visits produced %d Helpers, want one per member (2)", len(rows))
	}
	if rows[0].Name == rows[1].Name {
		t.Fatalf("concurrent provisioning allocated the same name %q twice", rows[0].Name)
	}
	if n := countWorkspaceAgents(t, wsID); n != 2 {
		t.Fatalf("concurrent first visits produced %d agents, want 2", n)
	}
}

func TestListAgents_DoesNotTakeHelperLockWhenIneligible(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	cases := []struct {
		name  string
		slug  string
		setup func(t *testing.T, wsID string)
	}{
		{
			name:  "workspace with no runtime at all",
			slug:  "helper-tests-hot-path-empty",
			setup: func(*testing.T, string) {},
		},
		{
			name: "only online runtime is another member's private machine",
			slug: "helper-tests-hot-path-private",
			setup: func(t *testing.T, wsID string) {
				otherID := helperTestMember(t, wsID, "helper-hot-path@goosar.ru", "Hanna Hotpath")
				helperTestRuntime(t, wsID, helperRuntimeSpec{
					name:       "Someone Else Laptop",
					provider:   "runtime-j",
					status:     "online",
					visibility: "private",
					ownerID:    otherID,
				})
			},
		},
		{

			name: "only online runtime is another member's published machine",
			slug: "helper-tests-hot-path-public",
			setup: func(t *testing.T, wsID string) {
				otherID := helperTestMember(t, wsID, "helper-hot-path-pub@goosar.ru", "Hugo Hotpath")
				helperTestRuntime(t, wsID, helperRuntimeSpec{
					name:       "Someone Else Build Box",
					provider:   "runtime-j",
					status:     "online",
					visibility: "public",
					ownerID:    otherID,
				})
			},
		},
		{
			name: "member already owns an agent",
			slug: "helper-tests-hot-path-has-agent",
			setup: func(t *testing.T, wsID string) {
				runtimeID := helperTestRuntime(t, wsID, helperRuntimeSpec{
					name: "Hot Path Runtime", provider: "runtime-j", status: "online",
				})
				if _, err := testPool.Exec(context.Background(), `
					INSERT INTO agent (
						workspace_id, name, description, runtime_mode, runtime_config,
						runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
					)
					VALUES ($1, 'Hand Made', '', 'cloud', '{}'::jsonb, $2, 'workspace', 'public_to', 6, $3)
				`, wsID, runtimeID, testUserID); err != nil {
					t.Fatalf("seed agent: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			wsID := helperTestWorkspace(t, tc.slug)
			tc.setup(t, wsID)

			conn, err := testPool.Acquire(ctx)
			if err != nil {
				t.Fatalf("acquire connection: %v", err)
			}
			defer conn.Release()

			lockKey := helperLockKey(parseUUID(wsID))
			if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1::text, 0))`, lockKey); err != nil {
				t.Fatalf("take helper lock: %v", err)
			}

			defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1::text, 0))`, lockKey)

			done := make(chan int, 1)
			go func() {
				done <- listAgentsRecorder(testUserID, wsID).Code
			}()

			select {
			case code := <-done:
				if code != http.StatusOK {
					t.Fatalf("ListAgents returned %d, want 200", code)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("ListAgents blocked on the Helper advisory lock for a member who can never receive one — the cheap probe must apply the same criteria the locked path does")
			}
		})
	}
}

func TestUpdateAgent_PlainMemberCanEditTheirOwnHelper(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	wsID := helperTestWorkspace(t, "helper-tests-member-edits")
	memberID := helperTestMember(t, wsID, "helper-member-edits@goosar.ru", "Egor Editor")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:       "Egor Laptop",
		provider:   "runtime-j",
		status:     "online",
		visibility: "private",
		ownerID:    memberID,
	})

	listAgentsRequest(t, memberID, wsID)
	helper := helperRowFor(t, wsID, memberID)

	w := httptest.NewRecorder()
	req := withURLParam(newRequestAs(memberID, http.MethodPut, "/api/agents/"+helper.ID, map[string]any{
		"mcp_config": map[string]any{"mcpServers": map[string]any{}},
	}), "id", helper.ID)
	req.Header.Set("X-Workspace-ID", wsID)
	testHandler.UpdateAgent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("a plain member could not edit their own Helper: got %d: %s", w.Code, w.Body.String())
	}
}

func TestListAgents_HelperContentFollowsItsOwnersLanguage(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	wsID := helperTestWorkspace(t, "helper-tests-language")
	helperTestRuntime(t, wsID, helperRuntimeSpec{name: "Helper Language Runtime", provider: "runtime-j", status: "online"})

	if _, err := testPool.Exec(ctx, `UPDATE "user" SET language = 'en' WHERE id = $1`, testUserID); err != nil {
		t.Fatalf("set user language: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE "user" SET language = NULL WHERE id = $1`, testUserID)
	})

	listAgentsRequest(t, testUserID, wsID)

	helper := helperRowFor(t, wsID, testUserID)
	if helper.Instructions != helperInstructionsByLang["en"] {
		t.Fatalf("instructions were not persisted in the owner's language")
	}
	if helper.Description != helperDescriptionByLang["en"] {
		t.Fatalf("description = %q, want the English variant", helper.Description)
	}
}

func TestListAgents_HelperContentIsPerOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	wsID := helperTestWorkspace(t, "helper-tests-language-member")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:     "Helper Language Member Runtime",
		provider: "runtime-j",
		status:   "online",
	})
	inviteeID := helperTestMember(t, wsID, "helper-language@goosar.ru", "Lena Language")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:     "Lena Laptop",
		provider: "runtime-j",
		status:   "online",
		ownerID:  inviteeID,
	})

	if _, err := testPool.Exec(ctx, `UPDATE "user" SET language = NULL WHERE id = $1`, testUserID); err != nil {
		t.Fatalf("clear owner language: %v", err)
	}

	listAgentsRequest(t, testUserID, wsID)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/agents", nil)
	req.Header.Set("X-User-ID", inviteeID)
	req.Header.Set("X-Workspace-ID", wsID)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	testHandler.ListAgents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ownerHelper := helperRowFor(t, wsID, testUserID)
	if ownerHelper.Instructions != helperInstructionsByLang[helperDefaultContentLang] {
		t.Fatalf("another member's Accept-Language rewrote the owner's Helper instructions")
	}
	memberHelper := helperRowFor(t, wsID, inviteeID)
	if memberHelper.Instructions != helperInstructionsByLang["en"] {
		t.Fatalf("the member's own Helper ignored their browser language")
	}
}

func TestHelperAgentNameFor(t *testing.T) {
	alice := helperUserFixture("11111111-2222-3333-4444-555555555555", "Alice Anderson", "alice@goosar.ru")
	bob := helperUserFixture("66666666-7777-8888-9999-aaaaaaaaaaaa", "Alice Anderson", "bob@goosar.ru")
	nameless := helperUserFixture("bbbbbbbb-cccc-dddd-eeee-ffffffffffff", "  ", "nameless@goosar.ru")

	got, ok := helperAgentNameFor(alice, nil, helperAgentName)
	if !ok || got != helperAgentName {
		t.Fatalf("first Helper name = %q (ok=%v), want the unqualified %q", got, ok, helperAgentName)
	}

	got, ok = helperAgentNameFor(alice, []string{helperAgentName}, helperAgentName)
	if !ok || got != helperAgentName+" (Alice Anderson)" {
		t.Fatalf("second Helper name = %q (ok=%v), want it qualified with the owner", got, ok)
	}

	taken := []string{helperAgentName, helperAgentName + " (Alice Anderson)"}
	got, ok = helperAgentNameFor(bob, taken, helperAgentName)
	if !ok {
		t.Fatalf("namesake got no name at all")
	}
	if !strings.HasPrefix(got, helperAgentName+" (Alice Anderson ") {
		t.Fatalf("namesake name = %q, want the owner's id appended to the display name", got)
	}
	for _, t2 := range taken {
		if got == t2 {
			t.Fatalf("namesake reused the taken name %q", got)
		}
	}

	got, ok = helperAgentNameFor(nameless, []string{helperAgentName}, helperAgentName)
	if !ok || got != helperAgentName+" (nameless)" {
		t.Fatalf("nameless owner name = %q (ok=%v), want the email local part", got, ok)
	}

	all := []string{helperAgentName, helperAgentName + " (Alice Anderson)"}
	for i := 0; i < 3; i++ {
		next, ok := helperAgentNameFor(alice, all, helperAgentName)
		if !ok {
			t.Fatalf("ran out of names after %d rows", len(all))
		}
		for _, t2 := range all {
			if next == t2 {
				t.Fatalf("allocated the taken name %q", next)
			}
		}
		all = append(all, next)
	}
}

func helperUserFixture(id, name, email string) db.User {
	return db.User{ID: parseUUID(id), Name: name, Email: email}
}

func TestHelperConstantsMatchDeprecatedShim(t *testing.T) {
	for _, c := range []struct{ name, canonical, shim string }{
		{"name", helperAgentName, onboardingAssistantName},
		{"avatar", helperAgentAvatarURL, onboardingAssistantAvatarURL},
		{"template", helperAgentTemplate, onboardingAgentTemplate},
	} {
		if c.canonical != c.shim {
			t.Errorf("%s drifted between agent_helper.go and onboarding_shim.go:\n canonical: %s\n shim:      %s",
				c.name, truncateForDiff(c.canonical), truncateForDiff(c.shim))
		}
	}
}

func truncateForDiff(s string) string {
	if len(s) <= 80 {
		return s
	}
	return fmt.Sprintf("%s… (%d bytes)", s[:80], len(s))
}
