package preflight

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stageServer(t *testing.T, root, pkg, command string) {
	t.Helper()
	bin := filepath.Join(root, pkg, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("stage %s: %v", pkg, err)
	}
	if err := os.WriteFile(filepath.Join(bin, command), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("stage %s entry point: %v", pkg, err)
	}
}

func TestMCPPackages_AllDelivered(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)
	store := t.TempDir()
	for _, server := range KitMCPServers {
		stageServer(t, store, server.Package, server.Command)
	}

	report := Run(context.Background(), Options{MCPServersDir: store})
	res := findResult(t, report, "mcp-packages")

	if res.Status != StatusOK {
		t.Fatalf("status = %q, want %q (detail: %s)", res.Status, StatusOK, res.Detail)
	}
	for _, server := range KitMCPServers {
		if !strings.Contains(res.Detail, server.Package) {
			t.Fatalf("detail does not name delivered package %s: %q", server.Package, res.Detail)
		}
	}
}

func TestMCPPackages_ReportsMissingByNameWithoutFailing(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)
	store := t.TempDir()
	stageServer(t, store, "ews-mcp", "ewsmcp")

	report := Run(context.Background(), Options{MCPServersDir: store})
	res := findResult(t, report, "mcp-packages")

	if res.Status != StatusSkipped {
		t.Fatalf("status = %q, want %q", res.Status, StatusSkipped)
	}
	if !strings.Contains(res.Message, "mcp-atlassian") || !strings.Contains(res.Message, "mcp-server-fetch") {
		t.Fatalf("message does not name the missing packages: %q", res.Message)
	}
	if !hasCyrillic(res.Message) || !hasCyrillic(res.Fix) {
		t.Fatalf("message/fix are not in Russian: %q / %q", res.Message, res.Fix)
	}
	for _, failed := range report.Failed() {
		if failed.ID == "mcp-packages" {
			t.Fatal("an undelivered optional package must not fail doctor")
		}
	}
}

func TestMCPPackages_SkippedWhenNoStore(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)

	report := Run(context.Background(), Options{MCPServersDir: filepath.Join(t.TempDir(), "absent")})
	res := findResult(t, report, "mcp-packages")

	if res.Status != StatusSkipped {
		t.Fatalf("status = %q, want %q", res.Status, StatusSkipped)
	}
	if !hasCyrillic(res.Message) {
		t.Fatalf("message is not in Russian: %q", res.Message)
	}
}

func TestMCPPackages_AcceptsAServerOnPath(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)
	store := t.TempDir()
	for _, server := range KitMCPServers {
		if server.Package == "mcp-server-fetch" {
			fakeBin(t, dir, server.Command, "1.0")
			continue
		}
		stageServer(t, store, server.Package, server.Command)
	}

	report := Run(context.Background(), Options{MCPServersDir: store})
	res := findResult(t, report, "mcp-packages")

	if res.Status != StatusOK {
		t.Fatalf("status = %q, want %q (detail: %s)", res.Status, StatusOK, res.Detail)
	}
}

func TestMCPPackages_DeliveredButNoEntryPointIsItsOwnProblem(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)
	store := t.TempDir()
	for _, server := range KitMCPServers {
		stageServer(t, store, server.Package, server.Command)
	}

	if err := os.Remove(filepath.Join(store, "b24-agent", ".venv", "bin", "mcp-server-b24")); err != nil {
		t.Fatalf("remove entry point: %v", err)
	}

	report := Run(context.Background(), Options{MCPServersDir: store})
	res := findResult(t, report, "mcp-packages")

	if res.Status != StatusMissing {
		t.Fatalf("status = %q, want %q", res.Status, StatusMissing)
	}
	if !strings.Contains(res.Message, "не запускаются") || !strings.Contains(res.Message, "mcp-server-b24") {
		t.Fatalf("message does not distinguish a delivered-but-unlaunchable package: %q", res.Message)
	}
	if strings.Contains(res.Message, "Не привезены") {
		t.Fatalf("a delivered package must not be reported as undelivered: %q", res.Message)
	}
}
