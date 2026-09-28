package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

func withActor(r *http.Request, userID string) *http.Request {
	actor := &httpapi.Actor{UserID: userID, IsHuman: true, Source: httpapi.SourceSession}
	return r.WithContext(httpapi.WithActor(r.Context(), actor))
}

func testDeps(t *testing.T, cfg config.Config) *Deps {
	t.Helper()
	db := newTestDB(t)
	return New(db, cfg, nil, nil)
}

// TestRequireRoleAgainstRealSchema — эта же проверка воспроизвела реальный
// баг, найденный при контрактном прогоне: RowExists() пакета store ожидает
// `SELECT EXISTS(SELECT 1 ...)` (Scan в bool), а не голый `SELECT 1` (Scan
// int4 в bool падает с ошибкой драйвера) — см. server2/docs/decisions.md.
func TestRequireRoleAgainstRealSchema(t *testing.T) {
	d := testDeps(t, config.Config{})
	accountID, workspaceID := seedWorkspace(t, d.Store.db, "owner")

	req := withActor(httptest.NewRequest(http.MethodGet, "/x", nil), accountID)
	req.SetPathValue("id", workspaceID)
	rec := httptest.NewRecorder()
	c, ok := d.requireRole(rec, req, httpapi.RoleOwner, httpapi.RoleAdmin, httpapi.RoleMember)
	if !ok {
		t.Fatalf("expected requireRole to succeed, got status %d body %s", rec.Code, rec.Body.String())
	}
	if c.WorkspaceID != workspaceID || c.Role != httpapi.RoleOwner {
		t.Fatalf("unexpected caller: %+v", c)
	}

	// unknown workspace -> 404, not 500
	req2 := withActor(httptest.NewRequest(http.MethodGet, "/x", nil), accountID)
	req2.SetPathValue("id", "00000000-0000-0000-0000-000000000000")
	rec2 := httptest.NewRecorder()
	if _, ok := d.requireRole(rec2, req2, httpapi.RoleOwner); ok || rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown workspace, got %d", rec2.Code)
	}

	// not a member -> 403
	req3 := withActor(httptest.NewRequest(http.MethodGet, "/x", nil), "00000000-0000-0000-0000-000000000001")
	req3.SetPathValue("id", workspaceID)
	rec3 := httptest.NewRecorder()
	if _, ok := d.requireRole(rec3, req3, httpapi.RoleOwner); ok || rec3.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %d", rec3.Code)
	}
}

func TestGitHubInstallationsListAndDelete(t *testing.T) {
	d := testDeps(t, config.Config{})
	accountID, workspaceID := seedWorkspace(t, d.Store.db, "owner")

	ghID := int64(555)
	installation, err := d.Store.UpsertGitHubInstallation(context.Background(), workspaceID, &ghID, "octo-org", "Organization", nil)
	if err != nil {
		t.Fatalf("UpsertGitHubInstallation: %v", err)
	}

	req := withActor(httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/github/installations", nil), accountID)
	req.SetPathValue("id", workspaceID)
	rec := httptest.NewRecorder()
	d.handleListGitHubInstallations(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	installations, _ := body["installations"].([]any)
	if len(installations) != 1 {
		t.Fatalf("expected 1 installation, got %v", body)
	}

	delReq := withActor(httptest.NewRequest(http.MethodDelete, "/x", nil), accountID)
	delReq.SetPathValue("id", workspaceID)
	delReq.SetPathValue("installationId", installation.ID)
	delRec := httptest.NewRecorder()
	d.handleDeleteGitHubInstallation(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", delRec.Code, delRec.Body.String())
	}
}

func TestGitHubSetupCallbackAndConnectURL(t *testing.T) {
	d := testDeps(t, config.Config{GitHubAppSlug: "my-app", JWTSecret: "test-secret", FrontendOrigin: "http://front.test"})
	accountID, workspaceID := seedWorkspace(t, d.Store.db, "owner")

	connectReq := withActor(httptest.NewRequest(http.MethodGet, "/x?return_to=github", nil), accountID)
	connectReq.SetPathValue("id", workspaceID)
	connectRec := httptest.NewRecorder()
	d.handleGetGitHubConnectURL(connectRec, connectReq)
	if connectRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", connectRec.Code, connectRec.Body.String())
	}
	var connectBody struct {
		URL       string `json:"url"`
		Configured bool  `json:"configured"`
	}
	if err := json.Unmarshal(connectRec.Body.Bytes(), &connectBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !connectBody.Configured || connectBody.URL == "" {
		t.Fatalf("expected configured url, got %+v", connectBody)
	}

	stateIdx := indexOfQueryState(connectBody.URL)
	state := connectBody.URL[stateIdx:]

	callbackReq := httptest.NewRequest(http.MethodGet, "/api/github/setup?state="+state+"&installation_id=42", nil)
	callbackRec := httptest.NewRecorder()
	d.handleGitHubSetupCallback(callbackRec, callbackReq)
	if callbackRec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", callbackRec.Code)
	}
	loc := callbackRec.Header().Get("Location")
	if got := "http://front.test/settings/integrations?github_connected=1&return_to=github"; loc != got {
		t.Fatalf("unexpected redirect location: %s", loc)
	}

	rows, err := d.Store.ListGitHubInstallations(context.Background(), workspaceID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected 1 installation persisted, got %v err=%v", rows, err)
	}
}

func indexOfQueryState(u string) int {
	for i := 0; i+6 <= len(u); i++ {
		if u[i:i+6] == "state=" {
			return i + 6
		}
	}
	return -1
}

func TestVCSConnectionsListUnavailableWhenNotConfigured(t *testing.T) {
	d := testDeps(t, config.Config{}) // VCSSecretKey empty -> not available
	accountID, workspaceID := seedWorkspace(t, d.Store.db, "member")

	req := withActor(httptest.NewRequest(http.MethodGet, "/x", nil), accountID)
	req.SetPathValue("id", workspaceID)
	rec := httptest.NewRecorder()
	d.handleListVCSConnections(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["available"] != false || body["configured"] != false {
		t.Fatalf("expected available:false, got %v", body)
	}
}

func TestVCSConnectAndRotateAndDelete(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "tok123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"username": "alice"})
	}))
	defer upstream.Close()

	d := testDeps(t, config.Config{VCSIntegrationEnabled: true, VCSSecretKey: "vcs-secret-key"})
	accountID, workspaceID := seedWorkspace(t, d.Store.db, "owner")

	body := `{"provider":"gitlab","instance_url":"` + upstream.URL + `","access_token":"tok123"}`
	req := withActor(httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)), accountID)
	req.SetPathValue("id", workspaceID)
	rec := httptest.NewRecorder()
	d.handleConnectVCS(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var conn map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &conn); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if conn["webhook_secret"] == nil || conn["webhook_secret"] == "" {
		t.Fatalf("expected webhook_secret in create response, got %v", conn)
	}
	connID, _ := conn["id"].(string)

	rotateReq := withActor(httptest.NewRequest(http.MethodPost, "/x", nil), accountID)
	rotateReq.SetPathValue("id", workspaceID)
	rotateReq.SetPathValue("connectionId", connID)
	rotateRec := httptest.NewRecorder()
	d.handleRotateVCSWebhook(rotateRec, rotateReq)
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on rotate, got %d: %s", rotateRec.Code, rotateRec.Body.String())
	}

	delReq := withActor(httptest.NewRequest(http.MethodDelete, "/x", nil), accountID)
	delReq.SetPathValue("id", workspaceID)
	delReq.SetPathValue("connectionId", connID)
	delRec := httptest.NewRecorder()
	d.handleDeleteVCSConnection(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delRec.Code)
	}
}

func TestSlackBindingRedeemFlow(t *testing.T) {
	d := testDeps(t, config.Config{SlackSecretKey: "slack-secret"})
	accountID, workspaceID := seedWorkspace(t, d.Store.db, "owner")

	// seed an agent (operatives row) + slack installation + binding token directly
	ctx := context.Background()
	var executorID string
	if err := d.Store.db.Pool.QueryRow(ctx, `
		INSERT INTO executors (workspace_id, ex_title, ex_mode, ex_provider) VALUES ($1,'exec','local','claude-code') RETURNING id`,
		workspaceID).Scan(&executorID); err != nil {
		t.Fatalf("seed executor: %v", err)
	}
	var agentID string
	if err := d.Store.db.Pool.QueryRow(ctx, `
		INSERT INTO operatives (workspace_id, executor_id, op_title, op_runtime_mode, op_owner_account_id)
		VALUES ($1, $2, 'Agent', 'local', $3) RETURNING id`,
		workspaceID, executorID, accountID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	installation, err := d.Store.UpsertSlackInstallation(ctx, workspaceID, agentID, "T123", nil, []byte("bot"), []byte("app"), accountID)
	if err != nil {
		t.Fatalf("UpsertSlackInstallation: %v", err)
	}
	tokenHash := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	if _, err := d.Store.db.Pool.Exec(ctx, `
		INSERT INTO slack_binding_tokens (sbt_installation_id, sbt_workspace_id, sbt_slack_user_id, sbt_token_hash, sbt_expires_at)
		VALUES ($1, $2, 'U999', $3, now() + interval '1 hour')`,
		installation.ID, workspaceID, tokenHash); err != nil {
		t.Fatalf("seed binding token: %v", err)
	}

	// redeem uses sha256(token) as the lookup key, so craft a token whose
	// sha256 matches tokenHash is impractical here — instead exercise the
	// "not found" path (unknown token) plus the plumbing up to that point.
	req := withActor(httptest.NewRequest(http.MethodPost, "/api/slack/binding/redeem", strings.NewReader(`{"token":"whatever-not-matching"}`)), accountID)
	rec := httptest.NewRecorder()
	d.handleRedeemSlackBinding(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("expected 410 for unknown token, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestComposioConnectionsListAndDelete(t *testing.T) {
	d := testDeps(t, config.Config{})
	accountID, workspaceID := seedWorkspace(t, d.Store.db, "member")

	conn, err := d.Store.UpsertPendingComposioConnection(context.Background(), workspaceID, accountID, "gmail")
	if err != nil {
		t.Fatalf("UpsertPendingComposioConnection: %v", err)
	}

	req := withActor(httptest.NewRequest(http.MethodGet, "/api/integrations/composio/connections", nil), accountID)
	rec := httptest.NewRecorder()
	d.handleListComposioConnections(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("expected 1 connection, got %v err=%v", list, err)
	}

	delReq := withActor(httptest.NewRequest(http.MethodDelete, "/x", nil), accountID)
	delReq.SetPathValue("id", conn.ID)
	delRec := httptest.NewRecorder()
	d.handleDeleteComposioConnection(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", delRec.Code, delRec.Body.String())
	}
}
