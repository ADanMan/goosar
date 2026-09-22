package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/runtimeapps"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type fakeOverlayBuilder struct {
	calls  int
	result runtimeapps.MCPOverlayResult
	err    error
}

func (f *fakeOverlayBuilder) BuildTaskOverlay(_ context.Context, _ pgtype.UUID, _ db.Agent) (runtimeapps.MCPOverlayResult, error) {
	f.calls++
	if f.err != nil {
		return runtimeapps.MCPOverlayResult{}, f.err
	}
	return f.result, nil
}

func overlayResult(t *testing.T, serverName, url string, apps ...runtimeapps.ConnectedApp) runtimeapps.MCPOverlayResult {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			serverName: map[string]any{"type": "http", "url": url},
		},
	})
	if err != nil {
		t.Fatalf("marshal overlay fixture: %v", err)
	}
	return runtimeapps.MCPOverlayResult{MCPOverlay: raw, ConnectedApps: apps}
}

func decodeMergedServers(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal merged overlay: %v", err)
	}
	return cfg.MCPServers
}

func TestBuildRuntimeMCPOverlay_MergesMultipleBuilders(t *testing.T) {
	composioLike := &fakeOverlayBuilder{result: overlayResult(t, "composio", "https://mcp.example/composio",
		runtimeapps.ConnectedApp{Provider: "composio", ServerName: "composio", ToolkitSlug: "notion"})}
	pluginLike := &fakeOverlayBuilder{result: overlayResult(t, "plugin", "https://goosar.example/api/mcp/plugin",
		runtimeapps.ConnectedApp{Provider: "plugin", ServerName: "plugin", ToolkitSlug: "pack-a"})}

	svc := &TaskService{OverlayBuilders: []TaskOverlayBuilder{composioLike, pluginLike}}
	data := svc.buildRuntimeMCPOverlay(context.Background(), pgtype.UUID{}, db.Agent{})

	if composioLike.calls != 1 || pluginLike.calls != 1 {
		t.Fatalf("calls = (%d, %d), want (1, 1)", composioLike.calls, pluginLike.calls)
	}
	servers := decodeMergedServers(t, data.Overlay)
	if _, ok := servers["composio"]; !ok {
		t.Errorf("merged overlay missing %q server: %s", "composio", data.Overlay)
	}
	if _, ok := servers["plugin"]; !ok {
		t.Errorf("merged overlay missing %q server: %s", "plugin", data.Overlay)
	}

	var apps []runtimeapps.ConnectedApp
	if err := json.Unmarshal(data.ConnectedApps, &apps); err != nil {
		t.Fatalf("unmarshal connected apps: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("connected apps = %+v, want 2 entries", apps)
	}
}

func TestBuildRuntimeMCPOverlay_OneBuilderErrorDoesNotSuppressOther(t *testing.T) {
	failing := &fakeOverlayBuilder{err: errors.New("boom")}
	working := &fakeOverlayBuilder{result: overlayResult(t, "plugin", "https://goosar.example/api/mcp/plugin")}

	svc := &TaskService{OverlayBuilders: []TaskOverlayBuilder{failing, working}}
	data := svc.buildRuntimeMCPOverlay(context.Background(), pgtype.UUID{}, db.Agent{})

	if failing.calls != 1 || working.calls != 1 {
		t.Fatalf("calls = (%d, %d), want (1, 1)", failing.calls, working.calls)
	}
	servers := decodeMergedServers(t, data.Overlay)
	if _, ok := servers["plugin"]; !ok {
		t.Fatalf("merged overlay missing %q server despite the other builder erroring: %s", "plugin", data.Overlay)
	}
}

func TestBuildRuntimeMCPOverlay_SameNameLaterBuilderWins(t *testing.T) {
	first := &fakeOverlayBuilder{result: overlayResult(t, "shared", "https://first.example")}
	second := &fakeOverlayBuilder{result: overlayResult(t, "shared", "https://second.example")}

	svc := &TaskService{OverlayBuilders: []TaskOverlayBuilder{first, second}}
	data := svc.buildRuntimeMCPOverlay(context.Background(), pgtype.UUID{}, db.Agent{})

	servers := decodeMergedServers(t, data.Overlay)
	entry, ok := servers["shared"]
	if !ok {
		t.Fatalf("merged overlay missing %q server: %s", "shared", data.Overlay)
	}
	var decoded struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(entry, &decoded); err != nil {
		t.Fatalf("unmarshal shared server entry: %v", err)
	}
	if decoded.URL != "https://second.example" {
		t.Errorf("shared server url = %q, want the later builder's url (https://second.example)", decoded.URL)
	}
}

func TestBuildRuntimeMCPOverlay_ComposioFieldStillWorksAlongsideOverlayBuilders(t *testing.T) {
	composioFake := &stubOverlayBuilder{
		resp: json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.example/session"}}}`),
		apps: []runtimeapps.ConnectedApp{{Provider: "composio", ServerName: "composio", ToolkitSlug: "notion"}},
	}
	pluginFake := &fakeOverlayBuilder{result: overlayResult(t, "plugin", "https://goosar.example/api/mcp/plugin")}

	svc := &TaskService{
		Composio:        composioFake,
		OverlayBuilders: []TaskOverlayBuilder{pluginFake},
		FeatureFlags:    composioMCPAppsTestFlags(true),
	}
	data := svc.buildRuntimeMCPOverlay(context.Background(), pgtype.UUID{}, db.Agent{})

	if composioFake.calls != 1 {
		t.Fatalf("composio builder calls = %d, want 1", composioFake.calls)
	}
	if pluginFake.calls != 1 {
		t.Fatalf("plugin-like builder calls = %d, want 1", pluginFake.calls)
	}
	servers := decodeMergedServers(t, data.Overlay)
	if _, ok := servers["composio"]; !ok {
		t.Errorf("merged overlay missing composio server: %s", data.Overlay)
	}
	if _, ok := servers["plugin"]; !ok {
		t.Errorf("merged overlay missing plugin server: %s", data.Overlay)
	}
}
