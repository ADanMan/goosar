package composio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/runtimeapps"
	sdk "github.com/adanman/goosar/server/pkg/composio"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func seedActiveConnection(t *testing.T, store *fakeStore, userID pgtype.UUID, toolkit, connectedAccountID string) {
	t.Helper()
	if _, err := store.UpsertUserComposioConnection(context.Background(), db.UpsertUserComposioConnectionParams{
		UserID:             userID,
		ToolkitSlug:        toolkit,
		AuthConfigID:       "ac_test",
		ConnectedAccountID: connectedAccountID,
		ComposioUserID:     uuidToString(userID),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	idx := 0
	for i, x := range b {
		out[idx] = hex[x>>4]
		out[idx+1] = hex[x&0xf]
		idx += 2
		if i == 3 || i == 5 || i == 7 || i == 9 {
			out[idx] = '-'
			idx++
		}
	}
	return string(out)
}

func makeAgent(owner pgtype.UUID, allowlist ...string) db.Agent {
	a := db.Agent{OwnerID: owner}
	if allowlist != nil {
		a.ComposioToolkitAllowlist = allowlist
	}
	return a
}

func TestBuildTaskOverlay_FollowsOwnerRegardlessOfOriginator(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		createSessResp: &sdk.CreateSessionResponse{
			MCP: sdk.MCPDescriptor{URL: "https://mcp.composio.dev/session/noorig"},
		},
	}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)

	owner := mintUUID(7)
	agent := makeAgent(owner, "notion")
	seedActiveConnection(t, store, owner, "notion", "ca_owner_notion")

	result, err := svc.BuildTaskOverlay(context.Background(), pgtype.UUID{}, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.MCPOverlay) == 0 {
		t.Fatalf("expected overlay from owner connection even with no originator")
	}
	if sdkFake.lastSessReq.UserID != uuidToString(owner) {
		t.Errorf("CreateSession user id = %q, want owner %q", sdkFake.lastSessReq.UserID, uuidToString(owner))
	}
}

func TestBuildTaskOverlay_UsesOwnerConnectionNotOriginator(t *testing.T) {
	t.Parallel()

	owner := mintUUID(11)
	other := mintUUID(12)

	t.Run("owner-connection-used", func(t *testing.T) {
		t.Parallel()
		sdkFake := &fakeSDK{
			createSessResp: &sdk.CreateSessionResponse{
				MCP: sdk.MCPDescriptor{URL: "https://mcp.composio.dev/session/owner"},
			},
		}
		store := newFakeStore()
		svc := newTestService(t, sdkFake, store)
		agent := makeAgent(owner, "notion")
		seedActiveConnection(t, store, owner, "notion", "ca_owner_notion")

		seedActiveConnection(t, store, other, "notion", "ca_other_notion")

		result, err := svc.BuildTaskOverlay(context.Background(), other, agent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.MCPOverlay) == 0 {
			t.Fatalf("expected overlay built from owner connection")
		}
		if sdkFake.lastSessReq.UserID != uuidToString(owner) {
			t.Errorf("CreateSession user id = %q, want owner %q (not originator)", sdkFake.lastSessReq.UserID, uuidToString(owner))
		}
		assertPinnedAccount(t, sdkFake.lastSessReq, "notion", "ca_owner_notion")
	})

	t.Run("originator-connection-ignored", func(t *testing.T) {
		t.Parallel()
		sdkFake := &fakeSDK{}
		store := newFakeStore()
		svc := newTestService(t, sdkFake, store)
		agent := makeAgent(owner, "notion")
		seedActiveConnection(t, store, other, "notion", "ca_other_notion")

		result, err := svc.BuildTaskOverlay(context.Background(), other, agent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.MCPOverlay != nil {
			t.Errorf("expected nil overlay: owner has no connection, originator's must be ignored, got %s", string(result.MCPOverlay))
		}
		if sdkFake.createSessCalls != 0 {
			t.Errorf("CreateSession must not run when owner has no matching connection, got %d", sdkFake.createSessCalls)
		}
	})
}

func TestBuildTaskOverlay_EmptyAllowlistIsNoOp(t *testing.T) {
	t.Parallel()
	for name, allowlist := range map[string][]string{
		"nil-slice":   nil,
		"empty-slice": {},
		"whitespace":  {"   ", "\t"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sdkFake := &fakeSDK{}
			store := newFakeStore()
			svc := newTestService(t, sdkFake, store)
			owner := mintUUID(20)
			agent := makeAgent(owner, allowlist...)
			seedActiveConnection(t, store, owner, "notion", "ca_owner_notion")

			result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.MCPOverlay != nil {
				t.Errorf("expected nil overlay for empty allowlist, got %s", string(result.MCPOverlay))
			}
			if sdkFake.createSessCalls != 0 {
				t.Errorf("CreateSession must not run when allowlist is empty, got %d calls", sdkFake.createSessCalls)
			}
		})
	}
}

func TestBuildTaskOverlay_NoMatchingConnectionIsNoOp(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	owner := mintUUID(30)
	agent := makeAgent(owner, "notion", "github")

	seedActiveConnection(t, store, owner, "slack", "ca_owner_slack")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MCPOverlay != nil {
		t.Errorf("expected nil overlay for empty intersection, got %s", string(result.MCPOverlay))
	}
	if sdkFake.createSessCalls != 0 {
		t.Errorf("CreateSession must not run when intersection is empty, got %d calls", sdkFake.createSessCalls)
	}
}

func TestBuildTaskOverlay_HappyPath_FiltersBothWays(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		createSessResp: &sdk.CreateSessionResponse{
			MCP: sdk.MCPDescriptor{URL: "https://mcp.composio.dev/session/abc"},
		},
	}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	owner := mintUUID(13)
	agent := makeAgent(owner, "notion", "github")

	seedActiveConnection(t, store, owner, "notion", "ca_owner_notion")
	seedActiveConnection(t, store, owner, "github", "ca_owner_github")
	seedActiveConnection(t, store, owner, "slack", "ca_owner_slack")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.MCPOverlay) == 0 {
		t.Fatalf("expected non-empty overlay, got nil")
	}

	if sdkFake.lastSessReq.UserID != uuidToString(owner) {
		t.Errorf("CreateSession user id: got %q, want %q", sdkFake.lastSessReq.UserID, uuidToString(owner))
	}

	tk, _ := sdkFake.lastSessReq.Toolkits["enable"].([]string)
	if len(tk) != 2 || !containsString(tk, "notion") || !containsString(tk, "github") {
		t.Errorf("CreateSession toolkits.enable = %v, want exactly [notion github]", tk)
	}
	if containsString(tk, "slack") {
		t.Errorf("non-allowlisted slack leaked into toolkits.enable: %v", tk)
	}

	assertPinnedAccount(t, sdkFake.lastSessReq, "notion", "ca_owner_notion")
	assertPinnedAccount(t, sdkFake.lastSessReq, "github", "ca_owner_github")
	if _, leaked := sdkFake.lastSessReq.ConnectedAccounts["slack"]; leaked {
		t.Errorf("non-allowlisted slack leaked into connected_accounts")
	}
	assertConnectedApps(t, result.ConnectedApps, "github", "notion")

	var payload mcpOverlayPayload
	if err := json.Unmarshal(result.MCPOverlay, &payload); err != nil {
		t.Fatalf("unmarshal overlay: %v", err)
	}
	srv, ok := payload.MCPServers[mcpOverlayServerName]
	if !ok {
		t.Fatalf("overlay missing %q server, got %s", mcpOverlayServerName, string(result.MCPOverlay))
	}
	if srv.Type != "http" {
		t.Errorf("type: got %q, want \"http\"", srv.Type)
	}
	if srv.URL != "https://mcp.composio.dev/session/abc" {
		t.Errorf("url: got %q", srv.URL)
	}
	if srv.Headers["x-api-key"] != "secret" {
		t.Errorf("headers missing x-api-key: %v", srv.Headers)
	}
}

func TestBuildTaskOverlay_CreateSessionWireContract(t *testing.T) {
	t.Parallel()

	postedCh := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tool_router/session" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key header = %q", got)
		}
		var posted map[string]any
		if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		postedCh <- posted
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"session_id": "trs_wire",
			"mcp":        map[string]any{"type": "http", "url": "https://mcp.example/session/wire"},
		}); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer upstream.Close()

	client, err := sdk.NewClient(sdk.Options{APIKey: "test-key", BaseURL: upstream.URL})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	store := newFakeStore()
	svc := newTestService(t, client, store)
	owner := mintUUID(17)
	agent := makeAgent(owner, "notion", "github")
	seedActiveConnection(t, store, owner, "notion", "ca_owner_notion")
	seedActiveConnection(t, store, owner, "github", "ca_owner_github")
	seedActiveConnection(t, store, owner, "slack", "ca_owner_slack")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("BuildTaskOverlay: %v", err)
	}
	if len(result.MCPOverlay) == 0 {
		t.Fatal("expected non-empty overlay")
	}
	var posted map[string]any
	select {
	case posted = <-postedCh:
	default:
	}
	if posted == nil {
		t.Fatal("upstream did not receive a request body")
	}
	if got := posted["user_id"]; got != uuidToString(owner) {
		t.Fatalf("user_id = %v, want %q", got, uuidToString(owner))
	}

	toolkits, ok := posted["toolkits"].(map[string]any)
	if !ok {
		t.Fatalf("toolkits = %T(%v), want object", posted["toolkits"], posted["toolkits"])
	}
	assertJSONStringArraySet(t, toolkits["enable"], "notion", "github")
	if _, wrong := toolkits["enabled"]; wrong {
		t.Fatalf("toolkits used unexpected key \"enabled\": %v", toolkits)
	}

	connected, ok := posted["connected_accounts"].(map[string]any)
	if !ok {
		t.Fatalf("connected_accounts = %T(%v), want object", posted["connected_accounts"], posted["connected_accounts"])
	}
	assertJSONStringArraySet(t, connected["notion"], "ca_owner_notion")
	assertJSONStringArraySet(t, connected["github"], "ca_owner_github")
	if _, leaked := connected["slack"]; leaked {
		t.Fatalf("non-allowlisted slack leaked into connected_accounts: %v", connected)
	}
}

func TestBuildTaskOverlay_EmptyURL(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		createSessResp: &sdk.CreateSessionResponse{
			MCP: sdk.MCPDescriptor{URL: ""},
		},
	}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	owner := mintUUID(14)
	agent := makeAgent(owner, "github")
	seedActiveConnection(t, store, owner, "github", "ca_owner_github")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MCPOverlay != nil {
		t.Errorf("expected nil overlay when MCP URL is empty, got %s", string(result.MCPOverlay))
	}
}

func TestBuildTaskOverlay_SDKError(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{createSessErr: errors.New("composio: 503 backend")}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	owner := mintUUID(15)
	agent := makeAgent(owner, "slack")
	seedActiveConnection(t, store, owner, "slack", "ca_owner_slack")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err == nil {
		t.Fatalf("expected error from SDK failure, got nil")
	}
	if !strings.Contains(err.Error(), "create session") {
		t.Errorf("error should mention create session, got %v", err)
	}
	if result.MCPOverlay != nil {
		t.Errorf("expected nil overlay on SDK error, got %s", string(result.MCPOverlay))
	}
}

func TestBuildTaskOverlay_NormalisesAllowlistAndConnectionSlugs(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		createSessResp: &sdk.CreateSessionResponse{
			MCP: sdk.MCPDescriptor{URL: "https://mcp.composio.dev/session/x"},
		},
	}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	owner := mintUUID(40)

	agent := makeAgent(owner, " Notion ", "GITHUB")

	seedActiveConnection(t, store, owner, "notion", "ca_a")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MCPOverlay == nil {
		t.Fatalf("expected non-empty overlay despite uppercase/padded allowlist")
	}
	assertPinnedAccount(t, sdkFake.lastSessReq, "notion", "ca_a")
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func assertJSONStringArraySet(t *testing.T, raw any, want ...string) {
	t.Helper()
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("value = %T(%v), want JSON string array %v", raw, raw, want)
	}
	got := make(map[string]struct{}, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("array item = %T(%v), want string", item, item)
		}
		got[s] = struct{}{}
	}
	if len(got) != len(want) {
		t.Fatalf("array = %v, want set %v", arr, want)
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Fatalf("array = %v, missing %q", arr, w)
		}
	}
}

func assertPinnedAccount(t *testing.T, req sdk.CreateSessionRequest, slug, want string) {
	t.Helper()
	got, ok := req.ConnectedAccounts[slug].([]string)
	if !ok {
		t.Fatalf("connected_accounts[%s] = %T(%v), want []string{%q}", slug, req.ConnectedAccounts[slug], req.ConnectedAccounts[slug], want)
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("connected_accounts[%s] = %v, want [%s]", slug, got, want)
	}
}

func assertConnectedApps(t *testing.T, apps []runtimeapps.ConnectedApp, want ...string) {
	t.Helper()
	if len(apps) != len(want) {
		t.Fatalf("connected apps = %+v, want slugs %v", apps, want)
	}
	got := make(map[string]runtimeapps.ConnectedApp, len(apps))
	for _, app := range apps {
		got[app.ToolkitSlug] = app
	}
	for _, slug := range want {
		app, ok := got[slug]
		if !ok {
			t.Fatalf("connected apps = %+v, missing %q", apps, slug)
		}
		if app.Provider != "composio" {
			t.Errorf("%s provider = %q, want composio", slug, app.Provider)
		}
		if app.ServerName != mcpOverlayServerName {
			t.Errorf("%s server = %q, want %q", slug, app.ServerName, mcpOverlayServerName)
		}
		if app.ToolkitName == "" {
			t.Errorf("%s has empty display name", slug)
		}
	}
	if _, leaked := got["slack"]; leaked {
		t.Fatalf("non-allowlisted slack leaked into connected apps: %+v", apps)
	}
}
