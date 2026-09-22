package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/perimeterpolicy"
	"github.com/adanman/goosar/server/pkg/agent"
)

func writeLocalClaudeMcpConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	config := `{"mcpServers":{
	  "corp-jira":{"command":"mcp-atlassian"},
	  "rogue":{"command":"mcp-remote","args":["https://evil.example/mcp"]}
	}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

func corpPolicy() *perimeterpolicy.MCPPolicy {
	return perimeterpolicy.NewMCPPolicy(
		[]string{"jira.corp.example"}, true,
		[]string{"mcp-atlassian"}, true,
	)
}

func TestMergeRuntimeAndAgentMcpConfigDropsLocalServerOutsideAllowlist(t *testing.T) {
	writeLocalClaudeMcpConfig(t)

	merged, blocked, err := mergeRuntimeAndAgentMcpConfig("runtime-c", json.RawMessage(`{"mcpServers":{}}`), corpPolicy())
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	servers := mcpServerNames(t, merged)
	if _, ok := servers["rogue"]; ok {
		t.Fatalf("blocked local server reached the agent config: %s", merged)
	}
	if _, ok := servers["corp-jira"]; !ok {
		t.Fatalf("allowlisted local server was dropped: %s", merged)
	}
	if len(blocked) != 1 || blocked[0].Name != "rogue" || blocked[0].Reason == "" {
		t.Fatalf("blocked report = %#v", blocked)
	}
}

func TestMergeRuntimeAndAgentMcpConfigRestrictedPolicyStopsNativeInheritance(t *testing.T) {
	writeLocalClaudeMcpConfig(t)

	merged, blocked, err := mergeRuntimeAndAgentMcpConfig("runtime-c", nil, corpPolicy())
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	servers := mcpServerNames(t, merged)
	if _, ok := servers["rogue"]; ok {
		t.Fatalf("blocked local server reached the agent config: %s", merged)
	}
	if _, ok := servers["corp-jira"]; !ok {
		t.Fatalf("allowlisted local server was dropped: %s", merged)
	}
	if len(blocked) != 1 {
		t.Fatalf("blocked report = %#v", blocked)
	}
}

func TestMergeRuntimeAndAgentMcpConfigNoPolicyKeepsNativeInheritance(t *testing.T) {
	writeLocalClaudeMcpConfig(t)

	merged, blocked, err := mergeRuntimeAndAgentMcpConfig("runtime-c", nil, nil)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if merged != nil || len(blocked) != 0 {
		t.Fatalf("merged=%s blocked=%#v", merged, blocked)
	}
}

func TestListRuntimeLocalMcpServersMarksBlocked(t *testing.T) {
	writeLocalClaudeMcpConfig(t)

	servers, supported, err := listRuntimeLocalMcpServers("runtime-c", corpPolicy())
	if err != nil || !supported {
		t.Fatalf("supported=%v err=%v", supported, err)
	}
	byName := map[string]runtimeLocalMcpServerSummary{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	if s := byName["rogue"]; !s.Blocked || s.BlockedReason == "" {
		t.Fatalf("rogue summary = %#v", s)
	}
	if s := byName["corp-jira"]; s.Blocked {
		t.Fatalf("allowlisted summary = %#v", s)
	}
}

func TestBlockedLocalMcpServerNeverReachesFakeAgentConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	writeLocalClaudeMcpConfig(t)

	merged, _, err := mergeRuntimeAndAgentMcpConfig("runtime-c", json.RawMessage(`{"mcpServers":{}}`), corpPolicy())
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	dir := t.TempDir()
	seen := filepath.Join(dir, "seen-mcp-config.json")
	fakePath := filepath.Join(dir, "claude")

	script := "#!/bin/sh\n" +
		"prev=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--mcp-config\" ]; then cat \"$a\" > \"" + seen + "\"; fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
		"printf '{\"type\":\"system\",\"session_id\":\"ses_fake\"}\\n'\n" +
		"exit 0\n"
	if err := os.WriteFile(fakePath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	backend, err := agent.New("runtime-c", agent.Config{
		ExecutablePath: fakePath,
		Env:            map[string]string{"IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", agent.ExecOptions{
		Timeout:   10 * time.Second,
		McpConfig: merged,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case <-session.Result:
	case <-ctx.Done():
		t.Fatal("fake agent did not finish")
	}

	raw, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("fake agent received no --mcp-config file: %v", err)
	}
	servers := mcpServerNames(t, raw)
	if _, ok := servers["rogue"]; ok {
		t.Fatalf("blocked server reached the agent: %s", raw)
	}
	if _, ok := servers["corp-jira"]; !ok {
		t.Fatalf("allowlisted server missing from the agent config: %s", raw)
	}
}

func mcpServerNames(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse merged config %s: %v", raw, err)
	}
	return doc.McpServers
}
