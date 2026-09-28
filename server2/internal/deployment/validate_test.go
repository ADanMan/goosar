package deployment

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestValidMcpName(t *testing.T) {
	cases := map[string]bool{
		"jira":        true,
		"mcp-gateway": true,
		"my_server_1": true,
		"":            false,
		"has space":   false,
		"emoji😀":      false,
	}
	for name, want := range cases {
		if got := validMcpName(name); got != want {
			t.Errorf("validMcpName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestValidateMcpConfig_rejectsEnabledDisabledAndMask(t *testing.T) {
	if _, ok := validateMcpConfig(map[string]any{"enabled": true}, false); ok {
		t.Error("config with enabled key should be rejected")
	}
	if _, ok := validateMcpConfig(map[string]any{"disabled": false}, false); ok {
		t.Error("config with disabled key should be rejected")
	}
	if _, ok := validateMcpConfig(map[string]any{"token": maskMarker}, false); ok {
		t.Error("config containing the mask marker should be rejected")
	}
	if _, ok := validateMcpConfig(map[string]any{"command": "run"}, false); !ok {
		t.Error("plain config should be accepted")
	}
}

func TestValidateMcpConfig_requiresEnvCapableTransportForCredentials(t *testing.T) {
	if _, ok := validateMcpConfig(map[string]any{"transport": "http"}, true); ok {
		t.Error("http transport with credential_schema should be rejected (no env channel)")
	}
	if _, ok := validateMcpConfig(map[string]any{"transport": "stdio"}, true); !ok {
		t.Error("stdio transport with credential_schema should be accepted")
	}
}

func TestValidateCredentialSchema(t *testing.T) {
	if _, ok := validateCredentialSchema([]McpCredentialField{{Key: "1bad"}}); ok {
		t.Error("key starting with a digit should be rejected")
	}
	if _, ok := validateCredentialSchema([]McpCredentialField{{Key: "ok"}, {Key: "ok"}}); ok {
		t.Error("duplicate keys should be rejected")
	}
	if _, ok := validateCredentialSchema([]McpCredentialField{{Key: "api_key", Required: true}}); !ok {
		t.Error("valid single field should be accepted")
	}
}

func TestValidatePolicyBody(t *testing.T) {
	if _, ok := validatePolicyBody([]byte(`{"llm":{"model":"gpt"}}`)); !ok {
		t.Error("plain llm object should be accepted")
	}
	if _, ok := validatePolicyBody([]byte(`{"unknown":{}}`)); ok {
		t.Error("unknown top-level key should be rejected")
	}
	if _, ok := validatePolicyBody([]byte(`{"llm":{"api_key":"secret"}}`)); ok {
		t.Error("field name resembling a secret should be rejected at any depth")
	}
	if _, ok := validatePolicyBody([]byte(`{"mcp":{"*":{"enabled":false,"locked":true}}}`)); !ok {
		t.Error(`mcp["*"] in canonical form should be accepted`)
	}
	if _, ok := validatePolicyBody([]byte(`{"mcp":{"*":{"enabled":true,"locked":true}}}`)); ok {
		t.Error(`mcp["*"] with enabled=true should be rejected (only the canonical disable form is allowed)`)
	}
	if _, ok := validatePolicyBody([]byte(`not json`)); ok {
		t.Error("non-JSON body should be rejected")
	}
}

func TestIsAbsoluteHTTPURL(t *testing.T) {
	cases := map[string]bool{
		"https://example.test": true,
		"http://example.test":  true,
		"ftp://example.test":   false,
		"example.test":         false,
		"/relative/path":       false,
		"":                     false,
	}
	for url, want := range cases {
		if got := isAbsoluteHTTPURL(url); got != want {
			t.Errorf("isAbsoluteHTTPURL(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestMergeMcpDocument_addsUpdatesAndDeletes(t *testing.T) {
	current := map[string]any{
		"jira": map[string]any{"enabled": true, "env": map[string]any{"TOKEN": "x"}},
		"gone": map[string]any{"enabled": false},
	}
	patch := json.RawMessage(`{
		"jira": {"env": {"TOKEN": null, "NEW_KEY": "v"}},
		"gone": null,
		"fresh": {"enabled": true}
	}`)
	merged, err := mergeMcpDocument(current, patch)
	if err != nil {
		t.Fatalf("mergeMcpDocument: %v", err)
	}
	if _, has := merged["gone"]; has {
		t.Error(`"gone" should have been removed by an explicit null patch entry`)
	}
	jira, ok := merged["jira"].(map[string]any)
	if !ok {
		t.Fatalf(`"jira" entry missing or wrong type: %#v`, merged["jira"])
	}
	if jira["enabled"] != true {
		t.Errorf(`"jira".enabled should be untouched (true), got %#v`, jira["enabled"])
	}
	env, ok := jira["env"].(map[string]any)
	if !ok {
		t.Fatalf(`"jira".env missing or wrong type: %#v`, jira["env"])
	}
	if _, has := env["TOKEN"]; has {
		t.Error(`"jira".env.TOKEN should have been deleted by a null patch value`)
	}
	if env["NEW_KEY"] != "v" {
		t.Errorf(`"jira".env.NEW_KEY = %#v, want "v"`, env["NEW_KEY"])
	}
	fresh, ok := merged["fresh"].(map[string]any)
	if !ok || fresh["enabled"] != true {
		t.Errorf(`"fresh" entry = %#v, want {"enabled": true}`, merged["fresh"])
	}
}

func TestMaskMcpDocument_hidesEnvValues(t *testing.T) {
	doc := map[string]any{
		"jira": map[string]any{"enabled": true, "env": map[string]any{"TOKEN": "super-secret"}},
	}
	masked := maskMcpDocument(doc)
	jira, ok := masked["jira"].(map[string]any)
	if !ok {
		t.Fatalf("masked jira entry missing: %#v", masked)
	}
	env, ok := jira["env"].(map[string]any)
	if !ok {
		t.Fatalf("masked env missing: %#v", jira)
	}
	if env["TOKEN"] != true {
		t.Errorf(`masked env["TOKEN"] = %#v, want true (presence only, not the value)`, env["TOKEN"])
	}
	if reflect.DeepEqual(env, doc["jira"].(map[string]any)["env"]) {
		t.Error("masking must not leak the original env map")
	}
}
