package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunAgentMcpAddPostsAssignment(t *testing.T) {
	var gotBody map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "srv-1", "name": "jira", "transport": "http", "enabled": true},
		})
	}))
	defer srv.Close()
	setMcpTestEnv(t, srv.URL)

	cmd := newWorkspaceMcpTestCmd("add")
	if _, err := captureStdout(t, func() error {
		return runAgentMcpAdd(cmd, []string{"agent-1", "srv-1"})
	}); err != nil {
		t.Fatalf("runAgentMcpAdd: %v", err)
	}

	if gotPath != "/api/agents/agent-1/mcp-servers" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["server_id"] != "srv-1" {
		t.Errorf("server_id = %v, want srv-1", gotBody["server_id"])
	}
}

func TestRunAgentMcpDisableSendsEnabledFalse(t *testing.T) {
	var gotBody map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.NotFound(w, r)
			return
		}
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "srv-1", "name": "jira", "transport": "http", "enabled": false},
		})
	}))
	defer srv.Close()
	setMcpTestEnv(t, srv.URL)

	cmd := newWorkspaceMcpTestCmd("disable")
	if _, err := captureStdout(t, func() error {
		return runAgentMcpDisable(cmd, []string{"agent-1", "srv-1"})
	}); err != nil {
		t.Fatalf("runAgentMcpDisable: %v", err)
	}

	if gotPath != "/api/agents/agent-1/mcp-servers/srv-1/enabled" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["enabled"] != false {
		t.Errorf("enabled = %v, want false", gotBody["enabled"])
	}
}
