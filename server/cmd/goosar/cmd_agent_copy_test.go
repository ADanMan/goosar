package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newAgentCopyTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "copy"}
	registerAgentCopyFlags(c)
	c.Flags().String("profile", "", "")
	return c
}

func fullSourceAgent() map[string]any {
	return map[string]any{
		"id":                   "agent-src",
		"name":                 "Src",
		"runtime_id":           "runtime-1",
		"description":          "a description",
		"instructions":         "some instructions",
		"avatar_url":           "https://img.example/a.png",
		"custom_args":          []any{"--foo", "--bar"},
		"max_concurrent_tasks": 9,
		"model":                "claude-sonnet-4-6",
		"thinking_level":       "high",
		"service_tier":         "priority",
		"permission_mode":      "public_to",
		"invocation_targets":   []any{map[string]any{"target_type": "workspace"}},
		"skills": []any{
			map[string]any{"id": "skill-1", "name": "One"},
			map[string]any{"id": "skill-2", "name": "Two"},
		},

		"has_custom_env":       true,
		"custom_env_key_count": 2,
		"mcp_config_redacted":  true,
	}
}

func copyMockServer(t *testing.T, source map[string]any, gotBody *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents/"+source["id"].(string):
			_ = json.NewEncoder(w).Encode(source)
		case r.Method == http.MethodPost && r.URL.Path == "/api/agents":
			if err := json.NewDecoder(r.Body).Decode(gotBody); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "agent-new", "name": "Src (copy)"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func setCopyTestEnv(t *testing.T, serverURL string) {
	t.Helper()

	t.Chdir(t.TempDir())
	t.Setenv("GOOSAR_SERVER_URL", serverURL)
	t.Setenv("GOOSAR_WORKSPACE_ID", "ws-1")
	t.Setenv("GOOSAR_TOKEN", "test-token")
	t.Setenv("GOOSAR_AGENT_ID", "")
	t.Setenv("GOOSAR_TASK_ID", "")
}

func TestAgentCopySameRuntimeCopiesPortableFields(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}

	if gotBody == nil {
		t.Fatal("create was never called")
	}
	if gotBody["name"] != "Src (copy)" {
		t.Errorf("name = %v, want \"Src (copy)\"", gotBody["name"])
	}

	if gotBody["runtime_id"] != "runtime-1" {
		t.Errorf("runtime_id = %v, want runtime-1", gotBody["runtime_id"])
	}
	if gotBody["description"] != "a description" {
		t.Errorf("description = %v", gotBody["description"])
	}
	if gotBody["instructions"] != "some instructions" {
		t.Errorf("instructions = %v", gotBody["instructions"])
	}
	if gotBody["avatar_url"] != "https://img.example/a.png" {
		t.Errorf("avatar_url = %v", gotBody["avatar_url"])
	}
	if !reflect.DeepEqual(gotBody["custom_args"], []any{"--foo", "--bar"}) {
		t.Errorf("custom_args = %v", gotBody["custom_args"])
	}
	if gotBody["max_concurrent_tasks"] != float64(9) {
		t.Errorf("max_concurrent_tasks = %v, want 9", gotBody["max_concurrent_tasks"])
	}

	if gotBody["model"] != "claude-sonnet-4-6" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if gotBody["thinking_level"] != "high" {
		t.Errorf("thinking_level = %v", gotBody["thinking_level"])
	}
	if gotBody["service_tier"] != "priority" {
		t.Errorf("service_tier = %v", gotBody["service_tier"])
	}

	if gotBody["permission_mode"] != "public_to" {
		t.Errorf("permission_mode = %v", gotBody["permission_mode"])
	}
	if !reflect.DeepEqual(gotBody["invocation_targets"], []any{map[string]any{"target_type": "workspace"}}) {
		t.Errorf("invocation_targets = %v", gotBody["invocation_targets"])
	}

	if !reflect.DeepEqual(gotBody["skill_ids"], []any{"skill-1", "skill-2"}) {
		t.Errorf("skill_ids = %v, want [skill-1 skill-2]", gotBody["skill_ids"])
	}

	for _, k := range []string{"custom_env", "mcp_config", "runtime_config", "has_custom_env"} {
		if _, ok := gotBody[k]; ok {
			t.Errorf("body must not contain %q, got %v", k, gotBody[k])
		}
	}
}

func TestAgentCopyCrossRuntimeRequiresModel(t *testing.T) {
	postCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(fullSourceAgent())
			return
		}
		postCalled = true
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "agent-new"})
	}))
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("runtime-id", "runtime-2")

	err := runAgentCopy(cmd, []string{"agent-src"})
	if err == nil {
		t.Fatal("expected error when copying across runtimes without --model")
	}
	if !strings.Contains(err.Error(), "--model") {
		t.Errorf("error = %q, want it to mention --model", err.Error())
	}
	if postCalled {
		t.Error("create must not be called when validation fails")
	}
}

func TestAgentCopyCrossRuntimeDropsRuntimeSpecificFields(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("runtime-id", "runtime-2")
	_ = cmd.Flags().Set("model", "openai/gpt-4o")

	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}
	if gotBody["runtime_id"] != "runtime-2" {
		t.Errorf("runtime_id = %v, want runtime-2", gotBody["runtime_id"])
	}
	if gotBody["model"] != "openai/gpt-4o" {
		t.Errorf("model = %v, want openai/gpt-4o", gotBody["model"])
	}

	if _, ok := gotBody["thinking_level"]; ok {
		t.Errorf("thinking_level must be dropped on a runtime change, got %v", gotBody["thinking_level"])
	}
	if _, ok := gotBody["service_tier"]; ok {
		t.Errorf("service_tier must be dropped on a runtime change, got %v", gotBody["service_tier"])
	}

	if !reflect.DeepEqual(gotBody["skill_ids"], []any{"skill-1", "skill-2"}) {
		t.Errorf("skill_ids = %v", gotBody["skill_ids"])
	}
}

func TestAgentCopyCrossRuntimeEmptyModelIsAllowed(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("runtime-id", "runtime-2")
	_ = cmd.Flags().Set("model", "")

	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}
	if gotBody["model"] != "" {
		t.Errorf("model = %v, want empty string", gotBody["model"])
	}
}

func TestAgentCopyNoSkillsOmitsSkillIDs(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("no-skills", "true")

	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}
	if _, ok := gotBody["skill_ids"]; ok {
		t.Errorf("skill_ids must be omitted with --no-skills, got %v", gotBody["skill_ids"])
	}
}

func TestAgentCopyPermissionOverrideReplacesSource(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("permission-mode", "private")

	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}
	if gotBody["permission_mode"] != "private" {
		t.Errorf("permission_mode = %v, want private", gotBody["permission_mode"])
	}

	if got, ok := gotBody["invocation_targets"].([]any); !ok || len(got) != 0 {
		t.Errorf("invocation_targets = %v, want empty list", gotBody["invocation_targets"])
	}
}

func TestAgentCopyAcceptsExplicitCustomEnv(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("custom-env", `{"API_KEY":"fresh"}`)

	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}
	ce, ok := gotBody["custom_env"].(map[string]any)
	if !ok {
		t.Fatalf("custom_env = %v, want a JSON object", gotBody["custom_env"])
	}
	if ce["API_KEY"] != "fresh" {
		t.Errorf("custom_env[API_KEY] = %v, want fresh", ce["API_KEY"])
	}
}

func TestAgentCopyExposesSecretSafeFlags(t *testing.T) {
	for _, name := range []string{
		"custom-env-stdin", "custom-env-file",
		"mcp-config-stdin", "mcp-config-file",
	} {
		if agentCopyCmd.Flag(name) == nil {
			t.Errorf("agent copy is missing the %q flag", name)
		}
	}
}
