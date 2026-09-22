package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func seedDeploymentPolicy(t *testing.T, policyJSON string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testHandler.Queries.SetDeploymentPolicy(ctx, db.SetDeploymentPolicyParams{
		Policy:    []byte(policyJSON),
		UpdatedBy: pgtype.UUID{},
	}); err != nil {
		t.Fatalf("seed deployment policy: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM deployment_policy`)
	})
}

type workspaceConfigSeed struct {
	baseURL string
	model   string
	apiKey  string
	mcpDoc  string
}

func seedWorkspaceConfig(t *testing.T, seed workspaceConfigSeed) {
	t.Helper()
	ctx := context.Background()

	sealedKey, err := testHandler.sealConfigSecret(seed.apiKey)
	if err != nil {
		t.Fatalf("seal workspace api key: %v", err)
	}
	var sealedMcp []byte
	if seed.mcpDoc != "" {
		sealedMcp, err = testHandler.sealConfigDocument([]byte(seed.mcpDoc))
		if err != nil {
			t.Fatalf("seal workspace mcp defaults: %v", err)
		}
	}
	if _, err := testHandler.Queries.UpsertWorkspaceConfig(ctx, db.UpsertWorkspaceConfigParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		LlmBaseUrl:  pgtype.Text{String: seed.baseURL, Valid: seed.baseURL != ""},
		LlmModel:    pgtype.Text{String: seed.model, Valid: seed.model != ""},
		LlmApiKey:   sealedKey,
		McpDefaults: sealedMcp,
		UpdatedBy:   parseUUID(testUserID),
	}); err != nil {
		t.Fatalf("seed workspace config: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_config WHERE workspace_id = $1`, testWorkspaceID)
	})
}

type userOverrideSeed struct {
	baseURL string
	model   string
	apiKey  string
	mcpDoc  string
}

func seedUserConfigOverride(t *testing.T, seed userOverrideSeed) {
	t.Helper()
	ctx := context.Background()

	sealedKey, err := testHandler.sealConfigSecret(seed.apiKey)
	if err != nil {
		t.Fatalf("seal override api key: %v", err)
	}
	var sealedMcp []byte
	if seed.mcpDoc != "" {
		sealedMcp, err = testHandler.sealConfigDocument([]byte(seed.mcpDoc))
		if err != nil {
			t.Fatalf("seal override mcp doc: %v", err)
		}
	}
	if _, err := testHandler.Queries.UpsertUserConfigOverride(ctx, db.UpsertUserConfigOverrideParams{
		WorkspaceID:  parseUUID(testWorkspaceID),
		UserID:       parseUUID(testUserID),
		LlmBaseUrl:   pgtype.Text{String: seed.baseURL, Valid: seed.baseURL != ""},
		LlmModel:     pgtype.Text{String: seed.model, Valid: seed.model != ""},
		LlmApiKey:    sealedKey,
		McpOverrides: sealedMcp,
		UpdatedBy:    parseUUID(testUserID),
	}); err != nil {
		t.Fatalf("seed user config override: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM user_config_override WHERE workspace_id = $1 AND user_id = $2`,
			testWorkspaceID, testUserID)
	})
}

func resolveForTestUser(t *testing.T) *EffectiveConfig {
	t.Helper()
	eff, err := testHandler.ResolveEffectiveConfig(context.Background(), parseUUID(testWorkspaceID), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("ResolveEffectiveConfig: %v", err)
	}
	return eff
}

func TestSealConfigSecret_FailClosedWithoutKey(t *testing.T) {
	h := &Handler{}

	if _, err := h.sealConfigSecret("sk-should-never-store"); err == nil {
		t.Fatal("sealConfigSecret without a key must fail closed, got nil error")
	}
	if _, err := h.sealConfigDocument([]byte(`{"jira":{"env":{"TOKEN":"t"}}}`)); err == nil {
		t.Fatal("sealConfigDocument without a key must fail closed, got nil error")
	}

	if sealed, err := h.sealConfigSecret(""); err != nil || sealed != nil {
		t.Fatalf("sealConfigSecret(\"\") = (%v, %v), want (nil, nil)", sealed, err)
	}
}

func TestSealOpenConfigSecret_RoundTrip(t *testing.T) {
	h := &Handler{MCPSecretBox: newTestMcpBox(t)}

	sealed, err := h.sealConfigSecret("sk-roundtrip-secret")
	if err != nil {
		t.Fatalf("sealConfigSecret: %v", err)
	}
	if bytes.Contains(sealed, []byte("sk-roundtrip-secret")) {
		t.Fatalf("sealed secret leaks plaintext: %q", sealed)
	}
	opened, err := h.openConfigSecret(sealed)
	if err != nil {
		t.Fatalf("openConfigSecret: %v", err)
	}
	if opened != "sk-roundtrip-secret" {
		t.Fatalf("openConfigSecret = %q, want original", opened)
	}

	doc := []byte(`{"outlook":{"enabled":true,"env":{"EWS_PASS":"pw-secret"}}}`)
	sealedDoc, err := h.sealConfigDocument(doc)
	if err != nil {
		t.Fatalf("sealConfigDocument: %v", err)
	}
	if bytes.Contains(sealedDoc, []byte("pw-secret")) {
		t.Fatalf("sealed document leaks plaintext: %q", sealedDoc)
	}
	if !json.Valid(sealedDoc) {
		t.Fatalf("sealed document must remain valid JSON for the jsonb column: %q", sealedDoc)
	}
	openedDoc, err := h.openConfigDocument(sealedDoc)
	if err != nil {
		t.Fatalf("openConfigDocument: %v", err)
	}
	if !bytes.Equal(openedDoc, doc) {
		t.Fatalf("openConfigDocument = %s, want %s", openedDoc, doc)
	}
}

func TestOpenConfigSecret_SealedWithoutKeyErrors(t *testing.T) {
	sealer := &Handler{MCPSecretBox: newTestMcpBox(t)}
	sealed, err := sealer.sealConfigSecret("sk-locked-out")
	if err != nil {
		t.Fatalf("sealConfigSecret: %v", err)
	}

	reader := &Handler{}
	if _, err := reader.openConfigSecret(sealed); err == nil {
		t.Fatal("openConfigSecret without a key must error, not return ciphertext")
	}
}

func TestResolveEffectiveConfig_EmptyLayersIsEmptyConfigNotError(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	eff := resolveForTestUser(t)

	if eff.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", eff.SchemaVersion)
	}
	raw, err := json.Marshal(eff)
	if err != nil {
		t.Fatalf("marshal effective config: %v", err)
	}
	want := `{"schema_version":1,"revoked_packages":[]}`
	if string(raw) != want {
		t.Fatalf("empty effective config = %s, want %s", raw, want)
	}
}

func TestResolveEffectiveConfig_LlmPrecedenceTable(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))

	policyDoc := `{"llm":{"base_url":"https://policy.example/v1","model":"policy-model"}}`
	lockedPolicyDoc := `{"llm":{"base_url":"https://policy.example/v1","model":"policy-model","locked":true}}`

	cases := []struct {
		name        string
		policy      string
		workspace   *workspaceConfigSeed
		override    *userOverrideSeed
		wantBaseURL string
		wantModel   string
		wantAPIKey  string
		wantOrigin  string
		wantLocked  bool
	}{
		{
			name:        "policy only",
			policy:      policyDoc,
			wantBaseURL: "https://policy.example/v1",
			wantModel:   "policy-model",
			wantOrigin:  ConfigOriginPolicy,
		},
		{
			name:        "workspace only",
			workspace:   &workspaceConfigSeed{baseURL: "https://ws.example/v2", model: "ws-model", apiKey: "sk-ws"},
			wantBaseURL: "https://ws.example/v2",
			wantModel:   "ws-model",
			wantAPIKey:  "sk-ws",
			wantOrigin:  ConfigOriginWorkspace,
		},
		{
			name:        "workspace beats policy",
			policy:      policyDoc,
			workspace:   &workspaceConfigSeed{baseURL: "https://ws.example/v2", model: "ws-model"},
			wantBaseURL: "https://ws.example/v2",
			wantModel:   "ws-model",
			wantOrigin:  ConfigOriginWorkspace,
		},
		{
			name:        "user override beats workspace",
			workspace:   &workspaceConfigSeed{baseURL: "https://ws.example/v2", model: "ws-model", apiKey: "sk-ws"},
			override:    &userOverrideSeed{baseURL: "https://user.example/v3", model: "user-model", apiKey: "sk-user"},
			wantBaseURL: "https://user.example/v3",
			wantModel:   "user-model",
			wantAPIKey:  "sk-user",
			wantOrigin:  ConfigOriginUserOverride,
		},
		{
			name:        "user override beats policy and workspace",
			policy:      policyDoc,
			workspace:   &workspaceConfigSeed{baseURL: "https://ws.example/v2", model: "ws-model"},
			override:    &userOverrideSeed{model: "user-model"},
			wantBaseURL: "https://ws.example/v2",
			wantModel:   "user-model",
			wantOrigin:  ConfigOriginUserOverride,
		},
		{

			name:        "locked policy pins its fields, lower-layer api_key passes",
			policy:      lockedPolicyDoc,
			workspace:   &workspaceConfigSeed{baseURL: "https://ws.example/v2", model: "ws-model", apiKey: "sk-ws"},
			override:    &userOverrideSeed{baseURL: "https://user.example/v3"},
			wantBaseURL: "https://policy.example/v1",
			wantModel:   "policy-model",
			wantAPIKey:  "sk-ws",
			wantOrigin:  ConfigOriginWorkspace,
			wantLocked:  true,
		},
		{

			name:        "user api_key pierces locked policy pin",
			policy:      lockedPolicyDoc,
			workspace:   &workspaceConfigSeed{apiKey: "sk-ws"},
			override:    &userOverrideSeed{baseURL: "https://user.example/v3", apiKey: "sk-user"},
			wantBaseURL: "https://policy.example/v1",
			wantModel:   "policy-model",
			wantAPIKey:  "sk-user",
			wantOrigin:  ConfigOriginUserOverride,
			wantLocked:  true,
		},
		{

			name:        "partial locked policy pins only the fields it set",
			policy:      `{"llm":{"base_url":"https://policy.example/v1","locked":true}}`,
			workspace:   &workspaceConfigSeed{baseURL: "https://ws.example/v2", model: "ws-model"},
			wantBaseURL: "https://policy.example/v1",
			wantModel:   "ws-model",
			wantOrigin:  ConfigOriginWorkspace,
			wantLocked:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.policy != "" {
				seedDeploymentPolicy(t, tc.policy)
			}
			if tc.workspace != nil {
				seedWorkspaceConfig(t, *tc.workspace)
			}
			if tc.override != nil {
				seedUserConfigOverride(t, *tc.override)
			}

			eff := resolveForTestUser(t)
			if eff.LLM == nil {
				t.Fatal("llm block missing")
			}
			if eff.LLM.BaseURL != tc.wantBaseURL {
				t.Errorf("base_url = %q, want %q", eff.LLM.BaseURL, tc.wantBaseURL)
			}
			if eff.LLM.Model != tc.wantModel {
				t.Errorf("model = %q, want %q", eff.LLM.Model, tc.wantModel)
			}
			if eff.LLM.APIKey != tc.wantAPIKey {
				t.Errorf("api_key = %q, want %q", eff.LLM.APIKey, tc.wantAPIKey)
			}
			if eff.LLM.Origin != tc.wantOrigin {
				t.Errorf("origin = %q, want %q", eff.LLM.Origin, tc.wantOrigin)
			}
			if eff.LLM.Locked != tc.wantLocked {
				t.Errorf("locked = %v, want %v", eff.LLM.Locked, tc.wantLocked)
			}
		})
	}
}

func TestResolveEffectiveConfig_PartialOverrideKeepsLowerLayerFields(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))

	seedWorkspaceConfig(t, workspaceConfigSeed{baseURL: "https://ws.example/v2", model: "ws-model", apiKey: "sk-ws"})
	seedUserConfigOverride(t, userOverrideSeed{apiKey: "sk-personal"})

	eff := resolveForTestUser(t)
	if eff.LLM == nil {
		t.Fatal("llm block missing")
	}
	if eff.LLM.BaseURL != "https://ws.example/v2" || eff.LLM.Model != "ws-model" {
		t.Fatalf("workspace fields lost under partial override: %+v", eff.LLM)
	}
	if eff.LLM.APIKey != "sk-personal" {
		t.Fatalf("api_key = %q, want the user override's key", eff.LLM.APIKey)
	}
	if eff.LLM.Origin != ConfigOriginUserOverride {
		t.Fatalf("origin = %q, want %q", eff.LLM.Origin, ConfigOriginUserOverride)
	}
}

func TestResolveEffectiveConfig_McpMergeAcrossLayers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))

	seedDeploymentPolicy(t, `{"mcp":{
		"telegram":{"enabled":false,"locked":true},
		"jira":{"enabled":true,"env":{"JIRA_URL":"https://jira.policy.example"}}
	}}`)

	seedWorkspaceConfig(t, workspaceConfigSeed{mcpDoc: `{
		"telegram":{"enabled":true},
		"jira":{"enabled":true,"env":{"JIRA_URL":"https://jira.ws.example"}},
		"outlook":{"enabled":true,"env":{"EWS_USER":"ws-user"}}
	}`})

	seedUserConfigOverride(t, userOverrideSeed{mcpDoc: `{
		"outlook":{"enabled":true,"env":{"EWS_USER":"personal-user"}},
		"personal-notes":{"enabled":true}
	}`})

	eff := resolveForTestUser(t)
	if eff.MCP == nil {
		t.Fatal("mcp block missing")
	}

	tg, ok := eff.MCP["telegram"]
	if !ok {
		t.Fatal("telegram entry missing")
	}
	if tg.Enabled || !tg.Locked || tg.Origin != ConfigOriginPolicy {
		t.Fatalf("locked policy prohibition must survive workspace override: %+v", tg)
	}

	jira, ok := eff.MCP["jira"]
	if !ok {
		t.Fatal("jira entry missing")
	}
	if jira.Origin != ConfigOriginWorkspace || jira.Env["JIRA_URL"] != "https://jira.ws.example" {
		t.Fatalf("unlocked policy entry must be overridable by workspace: %+v", jira)
	}

	outlook, ok := eff.MCP["outlook"]
	if !ok {
		t.Fatal("outlook entry missing")
	}
	if outlook.Origin != ConfigOriginUserOverride || outlook.Env["EWS_USER"] != "personal-user" {
		t.Fatalf("user override must beat workspace defaults: %+v", outlook)
	}

	personal, ok := eff.MCP["personal-notes"]
	if !ok {
		t.Fatal("personal-notes entry missing")
	}
	if personal.Origin != ConfigOriginUserOverride || !personal.Enabled {
		t.Fatalf("user-added entry wrong: %+v", personal)
	}
}

func TestResolveEffectiveConfig_SecretsInResponseNotInLogs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))

	const apiKeySecret = "sk-log-canary-a7f3"
	const envSecret = "ews-pass-canary-b81c"

	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	seedWorkspaceConfig(t, workspaceConfigSeed{
		baseURL: "https://gw.corp.example/v3",
		model:   "coding-medium",
		apiKey:  apiKeySecret,
		mcpDoc:  `{"outlook":{"enabled":true,"env":{"EWS_PASS":"` + envSecret + `"}}}`,
	})

	eff := resolveForTestUser(t)

	raw, err := json.Marshal(eff)
	if err != nil {
		t.Fatalf("marshal effective config: %v", err)
	}
	if !strings.Contains(string(raw), apiKeySecret) {
		t.Fatalf("resolved config must carry the api key for the daemon; got %s", raw)
	}
	if !strings.Contains(string(raw), envSecret) {
		t.Fatalf("resolved config must carry mcp env values for the daemon; got %s", raw)
	}

	logs := logBuf.String()
	if strings.Contains(logs, apiKeySecret) || strings.Contains(logs, envSecret) {
		t.Fatalf("secret material leaked into logs:\n%s", logs)
	}
}

func TestResolveEffectiveConfig_SealedRowWithoutKeyErrors(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))
	seedWorkspaceConfig(t, workspaceConfigSeed{apiKey: "sk-sealed-away"})

	withTestMcpBox(t, nil)
	if _, err := testHandler.ResolveEffectiveConfig(context.Background(), parseUUID(testWorkspaceID), parseUUID(testUserID)); err == nil {
		t.Fatal("resolver must error on sealed rows without GOOSAR_MCP_SECRET_KEY")
	}
}
