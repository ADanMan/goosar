package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/runtimeapps"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/featureflag"
)

type fakeRebindingOverlayBuilder struct {
	buildCalls  int
	rebindCalls int
	rebindErr   error
}

func (f *fakeRebindingOverlayBuilder) BuildTaskOverlay(_ context.Context, _ pgtype.UUID, _ db.Agent) (runtimeapps.MCPOverlayResult, error) {
	f.buildCalls++
	overlay, _ := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"plugin": map[string]any{"type": "http", "url": "https://goosar.example/api/mcp/plugin", "headers": map[string]string{"Authorization": "Bearer unbound"}},
		},
	})
	return runtimeapps.MCPOverlayResult{
		MCPOverlay:    overlay,
		ConnectedApps: []runtimeapps.ConnectedApp{{Provider: "plugin", ServerName: "plugin", ToolkitSlug: "pack-a"}},
	}, nil
}

func (f *fakeRebindingOverlayBuilder) RebindTaskOverlay(overlay []byte, taskID pgtype.UUID) ([]byte, error) {
	f.rebindCalls++
	if f.rebindErr != nil {
		return nil, f.rebindErr
	}
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(overlay, &cfg); err != nil {
		return nil, err
	}
	entry, ok := cfg.MCPServers["plugin"]
	if !ok {
		return nil, nil
	}
	_ = entry
	cfg.MCPServers["plugin"] = json.RawMessage(fmt.Sprintf(`{"type":"http","url":"https://goosar.example/api/mcp/plugin","headers":{"Authorization":"Bearer bound-%s"}}`, util.UUIDToString(taskID)))
	rebound, err := json.Marshal(map[string]any{"mcpServers": cfg.MCPServers})
	if err != nil {
		return nil, err
	}
	return rebound, nil
}

const testOverlayFlag = "test_task_overlay"

func flagOnOverlayBuilder(b TaskOverlayBuilder) TaskOverlayBuilder {
	provider := featureflag.NewStaticProvider()
	provider.Set(testOverlayFlag, featureflag.Rule{Default: true})
	return &FlagGatedOverlayBuilder{
		Flags:   featureflag.NewService(provider),
		Enabled: testOverlayFlag,
		Inner:   b,
	}
}

func TestRebindRuntimeMCPOverlayForTask_BindsTaskIDOnEnqueue(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("rebind-overlay-%d@goosar.test", suffix)
	workspaceSlug := fmt.Sprintf("rebind-overlay-%d", suffix)

	var userIDStr, workspaceIDStr, runtimeIDStr, agentIDStr, issueIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Rebind Overlay User', $1)
		RETURNING id
	`, email).Scan(&userIDStr); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userIDStr) })
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('rebind overlay ws', $1)
		RETURNING id
	`, workspaceSlug).Scan(&workspaceIDStr); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceIDStr) })
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceIDStr, userIDStr); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'rebind-overlay-r', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceIDStr, userIDStr).Scan(&runtimeIDStr); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'rebind-overlay-agent', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceIDStr, runtimeIDStr, userIDStr).Scan(&agentIDStr); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority
		)
		VALUES ($1, 'rebind overlay issue', 'member', $2, 'agent', $3, 'medium')
		RETURNING id
	`, workspaceIDStr, userIDStr, agentIDStr).Scan(&issueIDStr); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	rebinder := &fakeRebindingOverlayBuilder{}
	svc := &TaskService{
		Queries: q, TxStarter: pool, Bus: events.New(),
		OverlayBuilders: []TaskOverlayBuilder{flagOnOverlayBuilder(rebinder)},
	}
	userID := util.MustParseUUID(userIDStr)
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueIDStr),
		AssigneeID:   util.MustParseUUID(agentIDStr),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    userID,
		WorkspaceID:  util.MustParseUUID(workspaceIDStr),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}
	if rebinder.buildCalls != 1 {
		t.Fatalf("BuildTaskOverlay calls = %d, want 1", rebinder.buildCalls)
	}
	if rebinder.rebindCalls != 1 {
		t.Fatalf("RebindTaskOverlay calls = %d, want 1", rebinder.rebindCalls)
	}

	wantToken := "Bearer bound-" + util.UUIDToString(task.ID)
	var cfg struct {
		MCPServers map[string]struct {
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(task.RuntimeMcpOverlay, &cfg); err != nil {
		t.Fatalf("unmarshal returned overlay: %v", err)
	}
	if got := cfg.MCPServers["plugin"].Headers["Authorization"]; got != wantToken {
		t.Fatalf("returned overlay Authorization = %q, want %q (task-id-bound)", got, wantToken)
	}

	var storedOverlay []byte
	if err := pool.QueryRow(ctx, `SELECT runtime_mcp_overlay FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&storedOverlay); err != nil {
		t.Fatalf("read stored overlay: %v", err)
	}
	var storedCfg struct {
		MCPServers map[string]struct {
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(storedOverlay, &storedCfg); err != nil {
		t.Fatalf("unmarshal stored overlay: %v", err)
	}
	if got := storedCfg.MCPServers["plugin"].Headers["Authorization"]; got != wantToken {
		t.Fatalf("stored overlay Authorization = %q, want %q (must persist the rebound overlay, not the unbound one BuildTaskOverlay minted)", got, wantToken)
	}
}

func TestRebindRuntimeMCPOverlayForTask_RebindErrorKeepsUnboundOverlay(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("rebind-overlay-err-%d@goosar.test", suffix)
	workspaceSlug := fmt.Sprintf("rebind-overlay-err-%d", suffix)

	var userIDStr, workspaceIDStr, runtimeIDStr, agentIDStr, issueIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Rebind Overlay Err User', $1)
		RETURNING id
	`, email).Scan(&userIDStr); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userIDStr) })
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('rebind overlay err ws', $1)
		RETURNING id
	`, workspaceSlug).Scan(&workspaceIDStr); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceIDStr) })
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceIDStr, userIDStr); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'rebind-overlay-err-r', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceIDStr, userIDStr).Scan(&runtimeIDStr); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'rebind-overlay-err-agent', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceIDStr, runtimeIDStr, userIDStr).Scan(&agentIDStr); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority
		)
		VALUES ($1, 'rebind overlay err issue', 'member', $2, 'agent', $3, 'medium')
		RETURNING id
	`, workspaceIDStr, userIDStr, agentIDStr).Scan(&issueIDStr); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	rebinder := &fakeRebindingOverlayBuilder{rebindErr: fmt.Errorf("boom")}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New(), OverlayBuilders: []TaskOverlayBuilder{flagOnOverlayBuilder(rebinder)}}
	userID := util.MustParseUUID(userIDStr)
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueIDStr),
		AssigneeID:   util.MustParseUUID(agentIDStr),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    userID,
		WorkspaceID:  util.MustParseUUID(workspaceIDStr),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue must not fail on a rebind error: %v", err)
	}
	if len(task.RuntimeMcpOverlay) == 0 {
		t.Fatalf("task has empty overlay after a failed rebind; want the original unbound overlay preserved")
	}
}
