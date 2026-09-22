package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/runtimeapps"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type namedOverlayBuilder struct {
	server      string
	buildCalls  int
	rebindCalls int

	lastOriginator pgtype.UUID
}

func (f *namedOverlayBuilder) BuildTaskOverlay(_ context.Context, originatorUserID pgtype.UUID, _ db.Agent) (runtimeapps.MCPOverlayResult, error) {
	f.buildCalls++
	f.lastOriginator = originatorUserID
	overlay, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			f.server: map[string]any{
				"type":    "http",
				"url":     "https://goosar.example/api/mcp/" + f.server,
				"headers": map[string]string{"Authorization": "Bearer unbound"},
			},
		},
	})
	if err != nil {
		return runtimeapps.MCPOverlayResult{}, err
	}
	return runtimeapps.MCPOverlayResult{
		MCPOverlay:    overlay,
		ConnectedApps: []runtimeapps.ConnectedApp{{Provider: f.server, ServerName: f.server, ToolkitSlug: f.server}},
	}, nil
}

func (f *namedOverlayBuilder) RebindTaskOverlay(overlay []byte, taskID pgtype.UUID) ([]byte, error) {
	f.rebindCalls++
	var top map[string]json.RawMessage
	if err := json.Unmarshal(overlay, &top); err != nil {
		return nil, err
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(top["mcpServers"], &servers); err != nil {
		return nil, err
	}
	if _, ok := servers[f.server]; !ok {
		return nil, nil
	}
	entry, err := json.Marshal(map[string]any{
		"type":    "http",
		"url":     "https://goosar.example/api/mcp/" + f.server,
		"headers": map[string]string{"Authorization": "Bearer bound-" + util.UUIDToString(taskID)},
	})
	if err != nil {
		return nil, err
	}
	servers[f.server] = entry
	serversRaw, err := json.Marshal(servers)
	if err != nil {
		return nil, err
	}
	top["mcpServers"] = serversRaw
	return json.Marshal(top)
}

func dispatchRunOnlyOverlay(t *testing.T, builder TaskOverlayBuilder) (storedOverlay, storedApps []byte, taskID pgtype.UUID) {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	autopilotID, runID := seedRunOnlyAutopilot(t, pool, workspaceID, agentID, userID)
	_ = autopilotID

	svc := &AutopilotService{
		Queries: q, TxStarter: pool, Bus: events.New(),
		TaskSvc: &TaskService{
			Queries: q, TxStarter: pool, Bus: events.New(),
			OverlayBuilders: []TaskOverlayBuilder{flagOnOverlayBuilder(builder)},
		},
	}
	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}
	run, err := q.GetAutopilotRun(ctx, util.MustParseUUID(runID))
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if err := svc.dispatchRunOnly(ctx, ap, &run, util.MustParseUUID(userID)); err != nil {
		t.Fatalf("dispatchRunOnly: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT id, runtime_mcp_overlay, runtime_connected_apps
		FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID,
	).Scan(&taskID, &storedOverlay, &storedApps); err != nil {
		t.Fatalf("read dispatched task: %v", err)
	}
	return storedOverlay, storedApps, taskID
}

func overlayServerNames(t *testing.T, overlay []byte) map[string]map[string]any {
	t.Helper()
	var cfg struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(overlay, &cfg); err != nil {
		t.Fatalf("unmarshal stored overlay %q: %v", overlay, err)
	}
	return cfg.MCPServers
}

func TestDispatchRunOnly_StoresOverlay_ComposioProvider(t *testing.T) {
	builder := &namedOverlayBuilder{server: "composio"}
	overlay, apps, taskID := dispatchRunOnlyOverlay(t, builder)

	if builder.buildCalls != 1 {
		t.Fatalf("BuildTaskOverlay calls = %d, want 1 — run_only must build the overlay like every other execution mode", builder.buildCalls)
	}
	if len(overlay) == 0 {
		t.Fatal("dispatched run_only task has no runtime_mcp_overlay; the daemon gates overlay dispatch on this column")
	}
	servers := overlayServerNames(t, overlay)
	if _, ok := servers["composio"]; !ok {
		t.Fatalf("stored overlay %q carries no \"composio\" mcpServers entry", overlay)
	}
	if len(apps) == 0 || !jsonListContains(apps, "composio") {
		t.Fatalf("runtime_connected_apps = %q, want the composio entry", apps)
	}

	if builder.rebindCalls != 1 {
		t.Fatalf("RebindTaskOverlay calls = %d, want 1", builder.rebindCalls)
	}
	auth, _ := servers["composio"]["headers"].(map[string]any)
	if got, _ := auth["Authorization"].(string); got != "Bearer bound-"+util.UUIDToString(taskID) {
		t.Fatalf("stored Authorization = %q, want the task-id-bound token", got)
	}
}

func TestDispatchRunOnly_OverlayCarriesNoOriginator(t *testing.T) {
	builder := &namedOverlayBuilder{server: "plugin"}
	_, _, taskID := dispatchRunOnlyOverlay(t, builder)

	if builder.buildCalls != 1 {
		t.Fatalf("BuildTaskOverlay calls = %d, want 1", builder.buildCalls)
	}
	if builder.lastOriginator.Valid {
		t.Fatalf("overlay built with originator %v; an autopilot run must not borrow a human's external identity",
			util.UUIDToString(builder.lastOriginator))
	}

	pool := newResolveOriginatorPool(t)
	var accountable pgtype.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT accountable_user_id FROM agent_task_queue WHERE id = $1`, taskID).Scan(&accountable); err != nil {
		t.Fatalf("read task attribution: %v", err)
	}
	if !accountable.Valid {
		t.Fatal("dispatched task has no accountable_user_id; attribution must survive the credential refusal")
	}
}

func jsonListContains(raw []byte, want string) bool {
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return false
	}
	for _, entry := range decoded {
		for _, v := range entry {
			if s, ok := v.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}
