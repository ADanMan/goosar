package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func patchRuntimeCustomName(actorID, runtimeID string, body map[string]any) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := newRequestAs(actorID, http.MethodPatch, "/api/runtimes/"+runtimeID, body)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	return w
}

func TestUpdateAgentRuntime_CustomNamePatchApplies(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)

	w := patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"custom_name": "  Prod Box  "})
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH custom_name: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.CustomName == nil || *resp.CustomName != "Prod Box" {
		t.Fatalf("custom_name: got %v, want trimmed \"Prod Box\"", resp.CustomName)
	}

	if resp.Name != "Visibility Test Runtime" {
		t.Fatalf("name should be untouched by rename: got %q", resp.Name)
	}

	w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"custom_name": "   "})
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH clear custom_name: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp = AgentRuntimeResponse{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.CustomName != nil {
		t.Fatalf("custom_name should be cleared to null, got %q", *resp.CustomName)
	}

	w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"custom_name": strings.Repeat("x", maxRuntimeCustomNameLen+1)})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PATCH over-long custom_name: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	w = patchRuntimeCustomName(plainMemberID, runtimeID, map[string]any{"custom_name": "hijack"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("PATCH custom_name as plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentRuntime_CustomNameMachineFanout(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	_, runtimeOwnerID, _ := runtimeVisibilityFixture(t)
	ctx := context.Background()

	const daemonID = "custom-name-test-daemon"
	makeRuntime := func(provider string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (
				workspace_id, daemon_id, name, runtime_mode, provider, status,
				device_info, metadata, owner_id, visibility, last_seen_at
			)
			VALUES ($1, $2, $3, 'local', $4, 'online', 'host', '{}'::jsonb, $5, 'private', now())
			RETURNING id
		`, testWorkspaceID, daemonID, provider+" (host)", provider, runtimeOwnerID).Scan(&id); err != nil {
			t.Fatalf("create runtime %s: %v", provider, err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, id)
		})
		return id
	}

	idA := makeRuntime("ccn_a")
	idB := makeRuntime("ccn_b")

	w := patchRuntimeCustomName(runtimeOwnerID, idA, map[string]any{
		"custom_name":      "Bohan's MacBook",
		"apply_to_machine": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH machine rename: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	for _, id := range []string{idA, idB} {
		var name *string
		if err := testPool.QueryRow(ctx, `SELECT custom_name FROM agent_runtime WHERE id = $1`, id).Scan(&name); err != nil {
			t.Fatalf("read custom_name for %s: %v", id, err)
		}
		if name == nil || *name != "Bohan's MacBook" {
			t.Fatalf("runtime %s custom_name = %v, want machine name applied", id, name)
		}
	}
}

func registerRuntimeOnDaemon(t *testing.T, daemonID, provider string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    daemonID,
		"device_name":  "host",
		"runtimes": []map[string]any{
			{"name": provider + " (host)", "type": provider, "version": "1.0.0", "status": "online"},
		},
	}, testWorkspaceID, daemonID)
	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("register %s: expected 200, got %d: %s", provider, w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	runtimes, ok := resp["runtimes"].([]any)
	if !ok || len(runtimes) == 0 {
		t.Fatalf("register %s: no runtimes in response: %v", provider, resp)
	}
	return runtimes[0].(map[string]any)
}

func TestDaemonRegister_PreservesCustomNameInResponse(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const daemonID = "custom-name-register-preserve"
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE daemon_id = $1`, daemonID)
	})

	first := registerRuntimeOnDaemon(t, daemonID, "runtime-c")
	runtimeID := first["id"].(string)

	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET custom_name = 'Prod Box' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("set custom_name: %v", err)
	}

	again := registerRuntimeOnDaemon(t, daemonID, "runtime-c")
	if again["custom_name"] != "Prod Box" {
		t.Fatalf("register response custom_name = %v, want \"Prod Box\" (must not be dropped)", again["custom_name"])
	}
}

func TestDaemonRegister_NewRuntimeInheritsMachineName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const daemonID = "custom-name-register-inherit"
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE daemon_id = $1`, daemonID)
	})

	first := registerRuntimeOnDaemon(t, daemonID, "runtime-c")
	idA := first["id"].(string)
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET custom_name = 'Bohan MacBook' WHERE id = $1`, idA); err != nil {
		t.Fatalf("name machine: %v", err)
	}

	second := registerRuntimeOnDaemon(t, daemonID, "runtime-e")
	if second["custom_name"] != "Bohan MacBook" {
		t.Fatalf("new runtime response custom_name = %v, want inherited \"Bohan MacBook\"", second["custom_name"])
	}
	var persisted *string
	if err := testPool.QueryRow(ctx, `SELECT custom_name FROM agent_runtime WHERE id = $1`, second["id"].(string)).Scan(&persisted); err != nil {
		t.Fatalf("read persisted custom_name: %v", err)
	}
	if persisted == nil || *persisted != "Bohan MacBook" {
		t.Fatalf("persisted custom_name = %v, want inherited \"Bohan MacBook\"", persisted)
	}
}

func TestDaemonRegister_FailedProfileInheritsMachineName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const daemonID = "custom-name-register-failed-profile"
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE daemon_id = $1`, daemonID)
	})

	first := registerRuntimeOnDaemon(t, daemonID, "runtime-c")
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET custom_name = 'Bohan MacBook' WHERE id = $1`, first["id"].(string)); err != nil {
		t.Fatalf("name machine: %v", err)
	}

	profileID := insertRuntimeProfileFixture(t, ctx, "Custom Codex", "runtime-e", "missing-codex")

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    daemonID,
		"device_name":  "host",
		"failed_profiles": []map[string]any{
			{"profile_id": profileID, "command_name": "missing-codex", "reason": "command not found on PATH"},
		},
	}, testWorkspaceID, daemonID)
	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("register failed profile: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var name *string
	if err := testPool.QueryRow(ctx, `
		SELECT custom_name FROM agent_runtime
		WHERE workspace_id = $1 AND daemon_id = $2 AND profile_id = $3
	`, testWorkspaceID, daemonID, profileID).Scan(&name); err != nil {
		t.Fatalf("read failed-profile custom_name: %v", err)
	}
	if name == nil || *name != "Bohan MacBook" {
		t.Fatalf("failed-profile custom_name = %v, want inherited \"Bohan MacBook\"", name)
	}
}
