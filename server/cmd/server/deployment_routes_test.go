package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/realtime"
)

var deploymentRoutesUnderTest = []string{
	"/api/deployment/workspaces",
	"/api/deployment/workspaces/{workspaceId}/members",
	"/api/deployment/workspaces/{workspaceId}/config/overrides",

	"/api/deployment/fleet",
}

func TestDeploymentDirectoryRoutes_RegisteredOnRealRouter(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)

	registered := map[string]bool{}
	err := chi.Walk(router, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk router: %v", err)
	}
	for _, route := range deploymentRoutesUnderTest {
		if !registered[http.MethodGet+" "+route] {
			t.Fatalf("GET %s is not registered on the production router", route)
		}
	}
}

func TestDeploymentDirectoryRoutes_RejectMachineActorOnRealRouter(t *testing.T) {
	if testPool == nil || testServer == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	if _, err := testPool.Exec(ctx,
		`INSERT INTO deployment_admin (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`,
		parseUUID(testUserID)); err != nil {
		t.Fatalf("grant deployment admin: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM deployment_admin WHERE user_id = $1`, parseUUID(testUserID))
		_, _ = testPool.Exec(context.Background(), `DELETE FROM admin_audit`)
	})

	taskToken := mintAgentTaskTokenFixture(t)

	for _, route := range deploymentRoutesUnderTest {
		path := replaceRouteParams(route)
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, testServer.URL+path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+taskToken)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("machine actor on %s: status = %d, want 403", path, resp.StatusCode)
			}
		})
	}
}

func replaceRouteParams(route string) string {
	out := ""
	for i := 0; i < len(route); i++ {
		if route[i] == '{' {
			j := i
			for j < len(route) && route[j] != '}' {
				j++
			}
			out += testWorkspaceID
			i = j
			continue
		}
		out += string(route[i])
	}
	return out
}

func mintAgentTaskTokenFixture(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Deployment routes runtime', 'local', 'issue243_routes', 'online', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id::text
	`, parseUUID(testWorkspaceID)).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'issue243-routes-agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id::text
	`, parseUUID(testWorkspaceID), runtimeID, parseUUID(testUserID)).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'dispatched', 0)
		RETURNING id::text
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	token, err := auth.GenerateAgentTaskToken()
	if err != nil {
		t.Fatalf("mint task token: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + interval '1 hour')
	`, auth.HashToken(token), taskID, agentID, parseUUID(testWorkspaceID), parseUUID(testUserID)); err != nil {
		t.Fatalf("create task token: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_token WHERE token_hash = $1`, auth.HashToken(token))
	})
	return token
}
