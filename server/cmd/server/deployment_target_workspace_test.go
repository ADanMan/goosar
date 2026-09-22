package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func targetWorkspaceFixture(t *testing.T) (workspaceAID, overriddenUserID string) {
	t.Helper()
	ctx := context.Background()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ('Issue 243 Target', 'issue243-target', 'Target workspace for the path-param pin')
		RETURNING id::text
	`).Scan(&workspaceAID); err != nil {
		t.Fatalf("create target workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, parseUUID(workspaceAID))
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Issue 243 Target Member', 'issue243-target-member@goosar.ru')
		RETURNING id::text
	`).Scan(&overriddenUserID); err != nil {
		t.Fatalf("create target member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, parseUUID(overriddenUserID))
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, parseUUID(workspaceAID), parseUUID(overriddenUserID)); err != nil {
		t.Fatalf("add target member: %v", err)
	}

	seed := []struct {
		workspaceID string
		baseURL     string
		model       string
	}{
		{workspaceAID, "https://target-a.example/v1", "target-a-model"},
		{testWorkspaceID, "https://caller-b.example/v1", "caller-b-model"},
	}
	for _, s := range seed {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO workspace_config (workspace_id, llm_base_url, llm_model)
			VALUES ($1, $2, $3)
			ON CONFLICT (workspace_id) DO UPDATE SET llm_base_url = EXCLUDED.llm_base_url, llm_model = EXCLUDED.llm_model
		`, parseUUID(s.workspaceID), s.baseURL, s.model); err != nil {
			t.Fatalf("seed workspace config for %s: %v", s.workspaceID, err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace_config WHERE workspace_id = $1`, parseUUID(testWorkspaceID))
	})

	overrideSeed := []struct {
		workspaceID string
		userID      string
		model       string
	}{
		{workspaceAID, overriddenUserID, "target-a-override"},
		{testWorkspaceID, testUserID, "caller-b-override"},
	}
	for _, s := range overrideSeed {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO user_config_override (workspace_id, user_id, llm_model)
			VALUES ($1, $2, $3)
			ON CONFLICT (workspace_id, user_id) DO UPDATE SET llm_model = EXCLUDED.llm_model
		`, parseUUID(s.workspaceID), parseUUID(s.userID), s.model); err != nil {
			t.Fatalf("seed override in %s: %v", s.workspaceID, err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM user_config_override WHERE workspace_id = $1`, parseUUID(testWorkspaceID))
	})

	if _, err := testPool.Exec(ctx,
		`INSERT INTO deployment_admin (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`,
		parseUUID(testUserID)); err != nil {
		t.Fatalf("grant deployment admin: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM deployment_admin WHERE user_id = $1`, parseUUID(testUserID))
		_, _ = testPool.Exec(context.Background(), `DELETE FROM admin_audit`)
	})
	return workspaceAID, overriddenUserID
}

func deploymentRequestFromOtherWorkspace(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, testServer.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Workspace-Slug", integrationTestWorkspaceSlug)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return resp
}

func TestDeploymentWorkspaceReadsUseTheURLNotTheCallerHeaders(t *testing.T) {
	if testPool == nil || testServer == nil {
		t.Skip("database not available")
	}
	workspaceAID, overriddenUserID := targetWorkspaceFixture(t)

	t.Run("config", func(t *testing.T) {
		resp := deploymentRequestFromOtherWorkspace(t, http.MethodGet,
			"/api/deployment/workspaces/"+workspaceAID+"/config", nil)
		if resp.StatusCode != http.StatusOK {
			defer resp.Body.Close()
			t.Fatalf("GET config: status = %d, want 200", resp.StatusCode)
		}
		var got struct {
			LlmBaseURL string `json:"llm_base_url"`
			LlmModel   string `json:"llm_model"`
		}
		readJSON(t, resp, &got)
		if got.LlmBaseURL != "https://target-a.example/v1" || got.LlmModel != "target-a-model" {
			t.Fatalf("the card of workspace A was served workspace B's configuration: %+v", got)
		}
	})

	t.Run("overrides", func(t *testing.T) {
		resp := deploymentRequestFromOtherWorkspace(t, http.MethodGet,
			"/api/deployment/workspaces/"+workspaceAID+"/config/overrides", nil)
		if resp.StatusCode != http.StatusOK {
			defer resp.Body.Close()
			t.Fatalf("GET overrides: status = %d, want 200", resp.StatusCode)
		}
		var entries []struct {
			UserID   string `json:"user_id"`
			LlmModel string `json:"llm_model"`
		}
		readJSON(t, resp, &entries)
		if len(entries) != 1 {
			t.Fatalf("override listing length = %d, want exactly A's single row: %+v", len(entries), entries)
		}
		if entries[0].UserID != overriddenUserID || entries[0].LlmModel != "target-a-override" {
			t.Fatalf("the roster of workspace A was served workspace B's overrides: %+v", entries[0])
		}
	})
}

func TestDeploymentWorkspaceConfigWriteTargetsTheURLWorkspace(t *testing.T) {
	if testPool == nil || testServer == nil {
		t.Skip("database not available")
	}
	workspaceAID, _ := targetWorkspaceFixture(t)
	ctx := context.Background()

	resp := deploymentRequestFromOtherWorkspace(t, http.MethodPut,
		"/api/deployment/workspaces/"+workspaceAID+"/config",
		map[string]any{"llm_model": "written-into-a"})
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		t.Fatalf("PUT config: status = %d, want 200", resp.StatusCode)
	}
	var saved struct {
		LlmModel string `json:"llm_model"`
	}
	readJSON(t, resp, &saved)
	if saved.LlmModel != "written-into-a" {
		t.Fatalf("PUT response model = %q, want the value just written", saved.LlmModel)
	}

	modelOf := func(workspaceID string) string {
		t.Helper()
		var model string
		if err := testPool.QueryRow(ctx,
			`SELECT COALESCE(llm_model, '') FROM workspace_config WHERE workspace_id = $1`,
			parseUUID(workspaceID)).Scan(&model); err != nil {
			t.Fatalf("read stored config of %s: %v", workspaceID, err)
		}
		return model
	}
	if got := modelOf(workspaceAID); got != "written-into-a" {
		t.Fatalf("workspace A's stored model = %q, want the written value", got)
	}
	if got := modelOf(testWorkspaceID); got != "caller-b-model" {
		t.Fatalf("the caller's OWN workspace was rewritten by an edit aimed at another workspace: model = %q", got)
	}

	var targetID string
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(target_id, '')
		FROM admin_audit
		WHERE action = 'workspace_config.set' AND target_type = 'workspace'
		ORDER BY created_at DESC
		LIMIT 1
	`).Scan(&targetID); err != nil {
		t.Fatalf("the cross-workspace edit was not journaled at all: %v", err)
	}
	if targetID != workspaceAID {
		t.Fatalf("admin_audit target_id = %q, want workspace A (%q)", targetID, workspaceAID)
	}
}

func TestDeploymentWorkspaceEmptySegmentIsRefused(t *testing.T) {
	if testPool == nil || testServer == nil {
		t.Skip("database not available")
	}
	targetWorkspaceFixture(t)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"read config", http.MethodGet, "/api/deployment/workspaces//config", nil},
		{
			"write config", http.MethodPut, "/api/deployment/workspaces//config",
			map[string]any{"llm_base_url": "https://sneaked-in.example/v1"},
		},
		{"read overrides", http.MethodGet, "/api/deployment/workspaces//config/overrides", nil},
		{"read members", http.MethodGet, "/api/deployment/workspaces//members", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := deploymentRequestFromOtherWorkspace(t, tc.method, tc.path, tc.body)
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Fatalf("%s %s answered 200 for an empty target: the caller's own "+
					"workspace was served through a URL that named none", tc.method, tc.path)
			}
			if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s %s status = %d, want 400 (malformed) or 404 (no such route)",
					tc.method, tc.path, resp.StatusCode)
			}
		})
	}
}
