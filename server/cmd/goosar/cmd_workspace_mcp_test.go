package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newWorkspaceMcpTestCmd(use string) *cobra.Command {
	cmd := &cobra.Command{Use: use}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("output", "json", "")
	if use == "update" {
		cmd.Flags().String("name", "", "")
	}
	return cmd
}

func setMcpTestEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GOOSAR_SERVER_URL", serverURL)
	t.Setenv("GOOSAR_TOKEN", "test-token")
	t.Setenv("GOOSAR_WORKSPACE_ID", "11111111-1111-1111-1111-111111111111")
}

func feedStdin(t *testing.T, text string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
	go func() {
		defer w.Close()
		w.Write([]byte(text))
	}()
}

func TestRunWorkspaceMcpAddReadsConfigFromStdin(t *testing.T) {
	const конфиг = `{"url":"https://mcp.example/sse","headers":{"Authorization":"Bearer sekret"}}`
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/workspace-mcp-servers" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id": "22222222-2222-2222-2222-222222222222", "name": "jira", "transport": "http",
		})
	}))
	defer srv.Close()
	setMcpTestEnv(t, srv.URL)
	feedStdin(t, конфиг+"\n")

	cmd := newWorkspaceMcpTestCmd("add")
	out, err := captureStdout(t, func() error {
		return runWorkspaceMcpAdd(cmd, []string{"jira", "-"})
	})
	if err != nil {
		t.Fatalf("runWorkspaceMcpAdd: %v", err)
	}

	if gotBody["name"] != "jira" {
		t.Errorf("name = %v, want jira", gotBody["name"])
	}
	sent, _ := json.Marshal(gotBody["config"])
	if !strings.Contains(string(sent), "Bearer sekret") {
		t.Errorf("config from stdin did not reach the request body: %s", sent)
	}
	if !strings.Contains(out, "jira") {
		t.Errorf("output should carry the created entry, got %q", out)
	}
}

func TestRunWorkspaceMcpUpdateSendsConfigAndRename(t *testing.T) {
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
		json.NewEncoder(w).Encode(map[string]any{
			"id": "22222222-2222-2222-2222-222222222222", "name": "renamed", "transport": "stdio",
		})
	}))
	defer srv.Close()
	setMcpTestEnv(t, srv.URL)
	feedStdin(t, `{"command":"npx"}`)

	cmd := newWorkspaceMcpTestCmd("update")
	if err := cmd.Flags().Set("name", "renamed"); err != nil {
		t.Fatalf("set --name: %v", err)
	}
	if _, err := captureStdout(t, func() error {
		return runWorkspaceMcpUpdate(cmd, []string{"22222222-2222-2222-2222-222222222222", "-"})
	}); err != nil {
		t.Fatalf("runWorkspaceMcpUpdate: %v", err)
	}

	if gotPath != "/api/workspace-mcp-servers/22222222-2222-2222-2222-222222222222" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["name"] != "renamed" {
		t.Errorf("name = %v, want renamed", gotBody["name"])
	}
	sent, _ := json.Marshal(gotBody["config"])
	if !strings.Contains(string(sent), "npx") {
		t.Errorf("config did not reach the request body: %s", sent)
	}
}

func TestReadMcpServerConfigNeverEchoesTheInput(t *testing.T) {
	const notJSON = `url=https://mcp.example?token=sekret-value`
	_, err := readMcpServerConfig(notJSON)
	if err == nil {
		t.Fatal("expected an error for non-JSON input")
	}
	if strings.Contains(err.Error(), "sekret-value") {
		t.Fatalf("the error echoed the credential-bearing input: %v", err)
	}
}

func TestWorkspaceMcpSecretCommandsAdvertiseStdinFirst(t *testing.T) {
	for _, cmd := range []*cobra.Command{workspaceMcpAddCmd, workspaceMcpUpdateCmd} {
		if !strings.Contains(cmd.Use, "-") || strings.Contains(cmd.Use, "<config-json>") {
			t.Errorf("%s: usage %q must lead with the stdin form", cmd.Name(), cmd.Use)
		}
		if !strings.Contains(cmd.Long, "ps") {
			t.Errorf("%s: help must warn that an inline configuration is visible via ps", cmd.Name())
		}
		if !strings.Contains(cmd.Long, "stdin") {
			t.Errorf("%s: help must document the stdin form", cmd.Name())
		}
	}
}
