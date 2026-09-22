package preflight

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeBin(t *testing.T, dir, name, output string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-executable fixtures are POSIX shell scripts")
	}
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nprintf '%s\\n' " + shellQuote(output) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	return path
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func isolatedPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir)
	t.Setenv("HOME", t.TempDir())
}

func findResult(t *testing.T, report Report, id string) Result {
	t.Helper()
	for _, r := range report.Results {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("report has no result for %q; ids: %v", id, ids(report))
	return Result{}
}

func ids(report Report) []string {
	out := make([]string, 0, len(report.Results))
	for _, r := range report.Results {
		out = append(out, r.ID)
	}
	return out
}

func TestRun_EmptyMachine(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)

	report := Run(context.Background(), Options{})
	if report.OK {
		t.Fatal("Run() reported OK on a machine with nothing installed")
	}
	for _, id := range []string{"agent-cli", "node", "npm", "git"} {
		r := findResult(t, report, id)
		if r.Status != StatusMissing {
			t.Fatalf("%s status = %q, want %q", id, r.Status, StatusMissing)
		}
		if strings.TrimSpace(r.Message) == "" || strings.TrimSpace(r.Fix) == "" {
			t.Fatalf("%s has an empty message or fix: %+v", id, r)
		}
		if !hasCyrillic(r.Message) {
			t.Fatalf("%s message is not in Russian: %q", id, r.Message)
		}
		if !hasCyrillic(r.Fix) {
			t.Fatalf("%s fix is not in Russian: %q", id, r.Fix)
		}
	}
}

func TestRun_HealthyMachine(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "claude", "1.2.3 (Claude Code)")
	fakeBin(t, dir, "node", "v22.11.0")
	fakeBin(t, dir, "npm", "10.9.0")
	fakeBin(t, dir, "git", "git version 2.47.0")
	isolatedPath(t, dir)

	report := Run(context.Background(), Options{})
	if !report.OK {
		for _, r := range report.Results {
			t.Logf("%s = %s: %s", r.ID, r.Status, r.Message)
		}
		t.Fatal("Run() reported not-OK on a machine with claude + node + npm + git")
	}
	agents := findResult(t, report, "agent-cli")
	if agents.Status != StatusOK {
		t.Fatalf("agent-cli status = %q, want ok", agents.Status)
	}
	if !strings.Contains(agents.Detail, "runtime-c") {
		t.Fatalf("agent-cli detail = %q, want it to name runtime-c", agents.Detail)
	}
	if !strings.Contains(agents.Detail, "1.2.3") {
		t.Fatalf("agent-cli detail = %q, want the reported version", agents.Detail)
	}
	if node := findResult(t, report, "node"); node.Version != "22.11.0" {
		t.Fatalf("node version = %q, want 22.11.0", node.Version)
	}
}

func TestRun_NodeTooOld(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "claude", "1.2.3")
	fakeBin(t, dir, "node", "v16.20.2")
	fakeBin(t, dir, "npm", "8.19.4")
	fakeBin(t, dir, "git", "git version 2.47.0")
	isolatedPath(t, dir)

	report := Run(context.Background(), Options{})
	node := findResult(t, report, "node")
	if node.Status != StatusOutdated {
		t.Fatalf("node status = %q, want %q", node.Status, StatusOutdated)
	}
	if report.OK {
		t.Fatal("Run() reported OK with a Node older than the supported minimum")
	}
}

func TestRun_KerberosOptionalUnlessRequested(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)

	relaxed := findResult(t, Run(context.Background(), Options{}), "kinit")
	if relaxed.Status != StatusSkipped {
		t.Fatalf("kinit status without Kerberos = %q, want %q", relaxed.Status, StatusSkipped)
	}
	strict := findResult(t, Run(context.Background(), Options{RequireKerberos: true}), "kinit")
	if strict.Status != StatusMissing {
		t.Fatalf("kinit status with RequireKerberos = %q, want %q", strict.Status, StatusMissing)
	}
}

func TestRun_DockerOnlyWhenAnMCPServerDeclaresIt(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)

	plain := findResult(t, Run(context.Background(), Options{}), "docker")
	if plain.Status != StatusSkipped {
		t.Fatalf("docker status with no container MCP server = %q, want %q", plain.Status, StatusSkipped)
	}

	cfgDir := t.TempDir()
	конфиг := filepath.Join(cfgDir, "mcp.json")
	body, _ := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"jira": map[string]any{"command": "docker", "args": []string{"run", "--rm", "jira-mcp"}},
		},
	})
	if err := os.WriteFile(конфиг, body, 0o644); err != nil {
		t.Fatalf("write mcp config: %v", err)
	}

	declared := findResult(t, Run(context.Background(), Options{MCPConfigPaths: []string{конфиг}}), "docker")
	if declared.Status != StatusMissing {
		t.Fatalf("docker status with a container MCP server = %q, want %q", declared.Status, StatusMissing)
	}
}

func TestMCPWantsDocker_IgnoresUnrelatedAndMalformedConfigs(t *testing.T) {
	dir := t.TempDir()
	npxCfg := filepath.Join(dir, "npx.json")
	os.WriteFile(npxCfg, []byte(`{"mcpServers":{"fs":{"command":"npx","args":["-y","server"]}}}`), 0o644)
	brokenCfg := filepath.Join(dir, "broken.json")
	os.WriteFile(brokenCfg, []byte(`{not json`), 0o644)

	if mcpWantsDocker([]string{npxCfg, brokenCfg, filepath.Join(dir, "absent.json")}) {
		t.Fatal("mcpWantsDocker() found Docker where no server declares it")
	}
}

func TestReport_JSONShape(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)
	data, err := json.Marshal(Run(context.Background(), Options{}))
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var decoded struct {
		OK      bool `json:"ok"`
		Results []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
			Fix     string `json:"fix"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if len(decoded.Results) == 0 {
		t.Fatal("report JSON carries no results")
	}
	for _, r := range decoded.Results {
		if r.ID == "" || r.Name == "" || r.Status == "" {
			t.Fatalf("report JSON row is missing required fields: %+v", r)
		}
	}
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if r >= 'А' && r <= 'я' {
			return true
		}
	}
	return false
}
