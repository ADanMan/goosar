package contract

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// fixture holds everything set up so far in this test run: the API client,
// the loaded contract, and the ids of resources created along the way. Every
// "ensureX" method is idempotent, so any section can be run alone (e.g. `go
// test -run 'Contract/chat'`) and it will transparently create whatever it
// depends on (a workspace, a runtime, an agent, ...), while a full run only
// creates each of those once.
type fixture struct {
	mu  sync.Mutex
	doc *openapi3.T

	baseURL string
	client  *apiClient

	devCode string // GOOSAR_DEV_VERIFICATION_CODE, needed to complete login

	email  string
	userID string

	workspaceID   string
	workspaceSlug string

	runtimeID string
	agentID   string

	issueID   string
	projectID string
	labelID   string
	squadID   string

	chatSessionID string
	skillID       string

	// second/third — независимые залогиненные пользователи (T-029 доводка),
	// нужны сценариям, которым требуется второй актор: приглашения в
	// воркспейс (accept/decline), обновление/удаление участника, unauthorized
	// as-someone-else проверки.
	second       *apiClient
	secondEmail  string
	secondUserID string
	third        *apiClient
	thirdEmail   string
	thirdUserID  string

	// uploadedAttachmentID — set by testMe's POST /api/upload-file call, if it
	// succeeds; reused by the Attachments-tag calls in testIssues to exercise
	// a real attachment rather than only the 404-for-unknown-id path.
	uploadedAttachmentID string
}

var (
	fx     *fixture
	fxInit sync.Once
)

// getFixture returns the shared fixture for this test binary invocation, or
// skips the calling test if BASE_URL is not set (the documented way to opt
// this whole suite out when no server is available to test against).
func getFixture(t *testing.T) *fixture {
	t.Helper()
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		t.Skip("BASE_URL not set; skipping contract tests (see e2e/contract/README.md)")
	}
	fxInit.Do(func() {
		fx = &fixture{
			baseURL: baseURL,
			client:  newAPIClient(baseURL),
			devCode: os.Getenv("GOOSAR_DEV_VERIFICATION_CODE"),
		}
	})
	fx.doc = loadSpec(t)
	return fx
}

// call performs an HTTP request against `concretePath` (with real ids
// substituted in) and validates the response against the OpenAPI operation
// registered for method+pathTemplate (the `{param}` form, exactly as it
// appears in docs/50-api-contract.yaml). It returns the status code and raw
// body for the caller to inspect further if needed.
func (f *fixture) call(t *testing.T, method, pathTemplate, concretePath string, body any) (int, []byte) {
	t.Helper()
	status, raw, _ := f.client.raw(t, method, concretePath, body)
	validateResponse(t, f.doc, method, pathTemplate, status, raw)
	return status, raw
}

// callMultipart is like call but for the one multipart/form-data endpoint
// this suite exercises (POST /api/upload-file).
func (f *fixture) callMultipart(t *testing.T, pathTemplate, concretePath string, fields map[string]string, fileFieldName, fileName string, fileContent []byte) (int, []byte) {
	t.Helper()
	status, raw := f.client.rawMultipart(t, concretePath, fields, fileFieldName, fileName, fileContent)
	validateResponse(t, f.doc, http.MethodPost, pathTemplate, status, raw)
	return status, raw
}

// callAs is like call but issues the request with a different client (e.g.
// a second user, or a client with no auth at all) while still validating
// against the same fixture's loaded contract.
func (f *fixture) callAs(t *testing.T, c *apiClient, method, pathTemplate, concretePath string, body any) (int, []byte) {
	t.Helper()
	status, raw, _ := c.raw(t, method, concretePath, body)
	validateResponse(t, f.doc, method, pathTemplate, status, raw)
	return status, raw
}

// ---------------------------------------------------------------------------
// ensureX helpers: idempotent setup, each depending only on the previous
// step. Every one of these calls into `call`, so every setup request is
// itself schema-checked and counted toward coverage.
// ---------------------------------------------------------------------------

func (f *fixture) ensureAuth(t *testing.T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.client.token != "" {
		return
	}
	if f.devCode == "" {
		t.Fatalf("GOOSAR_DEV_VERIFICATION_CODE is not set; the contract suite needs it to complete " +
			"login without a real mailbox (see e2e/contract/README.md)")
	}

	email := fmt.Sprintf("contract-%s@example.test", uniqueSuffix())

	status, _ := f.call(t, http.MethodPost, "/auth/send-code", "/auth/send-code", map[string]any{
		"email": email,
	})
	if status != 200 {
		t.Fatalf("POST /auth/send-code: expected 200, got %d", status)
	}

	status, body := f.call(t, http.MethodPost, "/auth/verify-code", "/auth/verify-code", map[string]any{
		"email": email,
		"code":  f.devCode,
	})
	if status != 200 {
		t.Fatalf("POST /auth/verify-code: expected 200, got %d, body=%s", status, truncate(body))
	}
	login := decodeJSON(t, body)
	token := mustStr(t, login, "token")
	user, _ := login["user"].(map[string]any)

	f.client.token = token
	f.email = email
	f.userID = mustStr(t, user, "id")
}

// loginFreshUser logs a brand-new user in (email code, dev verification
// code), independent of the shared fixture's own client — for scenarios that
// need a second/third actor (invitations, cross-user visibility checks).
func loginFreshUser(t *testing.T, f *fixture) (client *apiClient, email, userID string) {
	t.Helper()
	if f.devCode == "" {
		t.Fatalf("GOOSAR_DEV_VERIFICATION_CODE is not set; needed to log a second user in")
	}
	email = fmt.Sprintf("contract-2nd-%s@example.test", uniqueSuffix())
	c := newAPIClient(f.baseURL)
	status, _ := f.callAs(t, c, http.MethodPost, "/auth/send-code", "/auth/send-code", map[string]any{"email": email})
	if status != 200 {
		t.Fatalf("POST /auth/send-code (second user): expected 200, got %d", status)
	}
	status, body := f.callAs(t, c, http.MethodPost, "/auth/verify-code", "/auth/verify-code", map[string]any{
		"email": email, "code": f.devCode,
	})
	if status != 200 {
		t.Fatalf("POST /auth/verify-code (second user): expected 200, got %d, body=%s", status, truncate(body))
	}
	login := decodeJSON(t, body)
	c.token = mustStr(t, login, "token")
	user, _ := login["user"].(map[string]any)
	userID = mustStr(t, user, "id")
	return c, email, userID
}

// ensureSecondUser/ensureThirdUser — idempotent, memoized second/third actors.
func (f *fixture) ensureSecondUser(t *testing.T) (*apiClient, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.second != nil {
		return f.second, f.secondEmail
	}
	f.second, f.secondEmail, f.secondUserID = loginFreshUser(t, f)
	return f.second, f.secondEmail
}

func (f *fixture) ensureThirdUser(t *testing.T) (*apiClient, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.third != nil {
		return f.third, f.thirdEmail
	}
	f.third, f.thirdEmail, f.thirdUserID = loginFreshUser(t, f)
	return f.third, f.thirdEmail
}

func (f *fixture) ensureWorkspace(t *testing.T) {
	f.ensureAuth(t)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.workspaceID != "" {
		return
	}

	suffix := uniqueSuffix()
	status, body := f.call(t, http.MethodPost, "/api/workspaces", "/api/workspaces", map[string]any{
		"name": "Contract Test " + suffix,
		"slug": "contract-test-" + suffix,
	})
	if status != 201 && status != 200 {
		t.Fatalf("POST /api/workspaces: expected 200/201, got %d, body=%s", status, truncate(body))
	}
	ws := decodeJSON(t, body)
	f.workspaceID = mustStr(t, ws, "id")
	f.workspaceSlug = str(ws, "slug")
	f.client.workspaceID = f.workspaceID
}

func (f *fixture) ensureRuntime(t *testing.T) {
	f.ensureWorkspace(t)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runtimeID != "" {
		return
	}

	daemonID := "contract-daemon-" + uniqueSuffix()
	status, body := f.call(t, http.MethodPost, "/api/daemon/register", "/api/daemon/register", map[string]any{
		"workspace_id": f.workspaceID,
		"daemon_id":    daemonID,
		"device_name":  "contract-test",
		"runtimes": []map[string]any{
			{"name": "contract-runtime", "type": "claude", "status": "online"},
		},
	})
	if status != 200 {
		t.Fatalf("POST /api/daemon/register: expected 200, got %d, body=%s", status, truncate(body))
	}
	resp := decodeJSON(t, body)
	runtimes, _ := resp["runtimes"].([]any)
	if len(runtimes) == 0 {
		t.Fatalf("POST /api/daemon/register: expected at least one runtime in response, got %s", truncate(body))
	}
	first, _ := runtimes[0].(map[string]any)
	f.runtimeID = mustStr(t, first, "id")
}

func (f *fixture) ensureAgent(t *testing.T) {
	f.ensureRuntime(t)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.agentID != "" {
		return
	}

	status, body := f.call(t, http.MethodPost, "/api/agents", "/api/agents", map[string]any{
		"name":       "Contract Agent " + uniqueSuffix(),
		"runtime_id": f.runtimeID,
	})
	if status != 201 && status != 200 {
		t.Fatalf("POST /api/agents: expected 200/201, got %d, body=%s", status, truncate(body))
	}
	agent := decodeJSON(t, body)
	f.agentID = mustStr(t, agent, "id")
}

func (f *fixture) ensureIssue(t *testing.T) {
	f.ensureWorkspace(t)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issueID != "" {
		return
	}

	status, body := f.call(t, http.MethodPost, "/api/issues", "/api/issues", map[string]any{
		"title": "Contract issue " + uniqueSuffix(),
	})
	if status != 201 && status != 200 {
		t.Fatalf("POST /api/issues: expected 200/201, got %d, body=%s", status, truncate(body))
	}
	issue := decodeJSON(t, body)
	f.issueID = mustStr(t, issue, "id")
}

func (f *fixture) ensureProject(t *testing.T) {
	f.ensureWorkspace(t)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.projectID != "" {
		return
	}

	status, body := f.call(t, http.MethodPost, "/api/projects", "/api/projects", map[string]any{
		"title": "Contract project " + uniqueSuffix(),
	})
	if status != 201 && status != 200 {
		t.Fatalf("POST /api/projects: expected 200/201, got %d, body=%s", status, truncate(body))
	}
	project := decodeJSON(t, body)
	f.projectID = mustStr(t, project, "id")
}

func (f *fixture) ensureLabel(t *testing.T) {
	f.ensureWorkspace(t)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.labelID != "" {
		return
	}

	status, body := f.call(t, http.MethodPost, "/api/labels", "/api/labels", map[string]any{
		"name":  "contract-" + uniqueSuffix(),
		"color": "#336699",
	})
	if status != 201 && status != 200 {
		t.Fatalf("POST /api/labels: expected 200/201, got %d, body=%s", status, truncate(body))
	}
	label := decodeJSON(t, body)
	f.labelID = mustStr(t, label, "id")
}

func (f *fixture) ensureSquad(t *testing.T) {
	f.ensureAgent(t)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.squadID != "" {
		return
	}

	// A squad's leader must be an agent in the workspace, not a human
	// member (confirmed by the server: "leader must be a valid agent in
	// this workspace").
	status, body := f.call(t, http.MethodPost, "/api/squads", "/api/squads", map[string]any{
		"name":      "Contract squad " + uniqueSuffix(),
		"leader_id": f.agentID,
	})
	if status != 201 && status != 200 {
		t.Fatalf("POST /api/squads: expected 200/201, got %d, body=%s", status, truncate(body))
	}
	squad := decodeJSON(t, body)
	f.squadID = mustStr(t, squad, "id")
}

func (f *fixture) ensureChatSession(t *testing.T) {
	f.ensureAgent(t)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.chatSessionID != "" {
		return
	}

	status, body := f.call(t, http.MethodPost, "/api/chat/sessions", "/api/chat/sessions", map[string]any{
		"agent_id": f.agentID,
	})
	if status != 201 && status != 200 {
		t.Fatalf("POST /api/chat/sessions: expected 200/201, got %d, body=%s", status, truncate(body))
	}
	session := decodeJSON(t, body)
	f.chatSessionID = mustStr(t, session, "id")
}

func (f *fixture) ensureSkill(t *testing.T) {
	f.ensureWorkspace(t)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.skillID != "" {
		return
	}

	status, body := f.call(t, http.MethodPost, "/api/skills", "/api/skills", map[string]any{
		"name": "contract-skill-" + uniqueSuffix(),
	})
	if status != 201 && status != 200 {
		t.Fatalf("POST /api/skills: expected 200/201, got %d, body=%s", status, truncate(body))
	}
	skill := decodeJSON(t, body)
	f.skillID = mustStr(t, skill, "id")
}
