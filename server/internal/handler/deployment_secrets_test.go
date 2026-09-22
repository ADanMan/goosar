package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func managedClientSecretsRequest() *http.Request {
	req := newRequest(http.MethodGet, "/api/deployment/client-secrets", nil)
	req.Header.Set("X-Goosar-Launched-By", "desktop")
	return req
}

func withTestLLMConfig(t *testing.T, apiBase, model, apiKey string) {
	t.Helper()
	prevBase, prevModel, prevKey := testHandler.cfg.LLMBaseURL, testHandler.cfg.LLMDefaultModel, testHandler.cfg.LLMAPIKey
	testHandler.cfg.LLMBaseURL = apiBase
	testHandler.cfg.LLMDefaultModel = model
	testHandler.cfg.LLMAPIKey = apiKey
	t.Cleanup(func() {
		testHandler.cfg.LLMBaseURL = prevBase
		testHandler.cfg.LLMDefaultModel = prevModel
		testHandler.cfg.LLMAPIKey = prevKey
	})
}

func TestGetDeploymentClientSecrets_MissingHeader403(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/deployment/client-secrets", nil)
	testHandler.GetDeploymentClientSecrets(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body["code"] != ErrCodeClientSecretsNotManaged {
		t.Fatalf("expected code %q, got %q", ErrCodeClientSecretsNotManaged, body["code"])
	}
}

func TestGetDeploymentClientSecrets_ManagedRequest200(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withTestLLMConfig(t, "https://gateway.example/v1", "gpt-5", "sk-deployment-secret")
	before := countAdminAuditRows(t, adminAuditActionDeploymentClientSecretsIssued)

	w := httptest.NewRecorder()
	testHandler.GetDeploymentClientSecrets(w, managedClientSecretsRequest())

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp DeploymentClientSecretsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.LLM == nil {
		t.Fatal("expected llm block, got nil")
	}
	if resp.LLM.APIBase != "https://gateway.example/v1" || resp.LLM.Model != "gpt-5" || resp.LLM.APIKey != "sk-deployment-secret" {
		t.Fatalf("unexpected llm block: %+v", resp.LLM)
	}

	after := countAdminAuditRows(t, adminAuditActionDeploymentClientSecretsIssued)
	if after != before+1 {
		t.Fatalf("expected exactly one new admin_audit row, before=%d after=%d", before, after)
	}

	var targetID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT target_id FROM admin_audit WHERE action = $1 AND actor_user_id = $2 ORDER BY created_at DESC LIMIT 1`,
		adminAuditActionDeploymentClientSecretsIssued, testUserID).Scan(&targetID); err != nil {
		t.Fatalf("read admin_audit row: %v", err)
	}
	if !strings.Contains(targetID, "llm.api_key") {
		t.Fatalf("expected audit target_id to name the issued fields, got %q", targetID)
	}
	if strings.Contains(targetID, "sk-deployment-secret") {
		t.Fatalf("admin_audit leaked the secret value: %q", targetID)
	}
}

func TestGetDeploymentClientSecrets_NoAPIKeyLLMNull(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withTestLLMConfig(t, "", "", "")

	w := httptest.NewRecorder()
	testHandler.GetDeploymentClientSecrets(w, managedClientSecretsRequest())

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp DeploymentClientSecretsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.LLM != nil {
		t.Fatalf("expected nil llm block, got %+v", resp.LLM)
	}
}

func TestGetDeploymentClientSecrets_RefusalDoesNotAudit(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	before := countAdminAuditRows(t, adminAuditActionDeploymentClientSecretsIssued)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/deployment/client-secrets", nil)
	testHandler.GetDeploymentClientSecrets(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	after := countAdminAuditRows(t, adminAuditActionDeploymentClientSecretsIssued)
	if after != before {
		t.Fatalf("expected no new admin_audit row on refusal, before=%d after=%d", before, after)
	}
}

func TestGetDeploymentClientSecrets_NeverLeaksPersonalCredentials(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))
	withTestLLMConfig(t, "https://gateway.example/v1", "gpt-5", "sk-deployment-secret")
	grantDeploymentAdminFixture(t, testUserID)

	serverID, _ := createDeploymentMcpServerForTest(t, "jira", `{"command":"mcp-atlassian","env":{"JIRA_URL":"https://jira.example","JIRA_PERSONAL_TOKEN":""}}`)
	if code := setDeploymentMcpEnabledForTest(t, serverID, true); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("enable deployment mcp server: unexpected status %d", code)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_mcp_user_credential WHERE server_id = $1`, serverID)
	})
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO workspace_mcp_user_credential (server_id, user_id, workspace_id, value_keys, sealed_values)
		VALUES ($1, $2, $3, ARRAY['JIRA_PERSONAL_TOKEN'], $4)
	`, serverID, testUserID, testWorkspaceID, []byte(`{"sealed":"sealed-placeholder"}`)); err != nil {
		t.Skipf("could not seed a personal credential row for this schema: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.GetDeploymentClientSecrets(w, managedClientSecretsRequest())
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "JIRA_PERSONAL_TOKEN") || strings.Contains(w.Body.String(), "sealed-placeholder") {
		t.Fatalf("response leaked a personal credential field: %s", w.Body.String())
	}

	var resp DeploymentClientSecretsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	entry, ok := resp.Integrations["jira"]
	if !ok {
		t.Fatal("expected the enabled jira integration address in the response")
	}
	if entry.URL != "https://jira.example" {
		t.Fatalf("unexpected jira address: %q", entry.URL)
	}
}
