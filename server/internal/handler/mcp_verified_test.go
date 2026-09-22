package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mcpVerifiedFixture(t *testing.T) (runtimeID string) {
	t.Helper()
	ctx := context.Background()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, 'Mcp Verified Test Runtime', 'cloud', 'mcp_verified_test_provider', 'online', 'mcp verified test', '{}'::jsonb, $2, 'private', now())
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func postMcpVerified(t *testing.T, runtimeID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/runtimes/"+runtimeID+"/mcp-verified", bytes.NewReader(raw))
	req = withURLParam(req, "runtimeId", runtimeID)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()
	testHandler.ReportMcpVerified(w, req)
	return w
}

func TestReportMcpVerified_RecordsResult(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	resetMcpVerifiedStoreForTests()

	runtimeID := mcpVerifiedFixture(t)

	w := postMcpVerified(t, runtimeID, map[string]any{
		"server": "ews-mcp",
		"status": "ok",
	})
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	got, ok := lookupMcpVerified(runtimeID)
	if !ok {
		t.Fatal("expected a recorded result, found none")
	}
	if got.Server != "ews-mcp" || got.Status != "ok" {
		t.Fatalf("recorded result = %+v, want server=ews-mcp status=ok", got)
	}
}

func TestReportMcpVerified_AcceptsMcpFailedWithReason(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	resetMcpVerifiedStoreForTests()

	runtimeID := mcpVerifiedFixture(t)

	w := postMcpVerified(t, runtimeID, map[string]any{
		"server": "ews-mcp",
		"status": "mcp_failed(ModuleNotFoundError: mcp)",
	})
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	got, ok := lookupMcpVerified(runtimeID)
	if !ok || got.Status != "mcp_failed(ModuleNotFoundError: mcp)" {
		t.Fatalf("lookupMcpVerified = %+v, ok=%v", got, ok)
	}
}

func TestReportMcpVerified_RejectsUnknownStatus(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	resetMcpVerifiedStoreForTests()

	runtimeID := mcpVerifiedFixture(t)

	w := postMcpVerified(t, runtimeID, map[string]any{
		"server": "ews-mcp",
		"status": "definitely_not_a_real_status",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReportMcpVerified_RejectsNonMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	resetMcpVerifiedStoreForTests()

	runtimeID := mcpVerifiedFixture(t)

	raw, _ := json.Marshal(map[string]any{"server": "ews-mcp", "status": "ok"})
	req := httptest.NewRequest(http.MethodPost, "/api/runtimes/"+runtimeID+"/mcp-verified", bytes.NewReader(raw))
	req = withURLParam(req, "runtimeId", runtimeID)
	req.Header.Set("X-User-ID", "99999999-9999-9999-9999-999999999999")
	w := httptest.NewRecorder()
	testHandler.ReportMcpVerified(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a non-member caller, got %d: %s", w.Code, w.Body.String())
	}
	if _, ok := lookupMcpVerified(runtimeID); ok {
		t.Fatal("non-member request must not have recorded a result")
	}
}
