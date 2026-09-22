package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
)

func newWorkspaceExportTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "export"}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().StringP("output-dir", "o", ".", "")
	cmd.Flags().Bool("wait", true, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func exportServer(t *testing.T, archive []byte) (*httptest.Server, *int32) {
	t.Helper()
	const wsID = "44444444-4444-4444-4444-444444444444"
	const jobID = "55555555-5555-5555-5555-555555555555"
	var polls int32

	job := func(status string, done bool) map[string]any {
		body := map[string]any{
			"id":           jobID,
			"workspace_id": wsID,
			"status":       status,
			"error":        nil,
			"size_bytes":   len(archive),
			"created_at":   "2026-09-08T10:00:00Z",
			"completed_at": nil,
			"manifest":     map[string]any{"schema_version": 1, "counts": map[string]any{"issue": 2}},
		}
		if done {
			body["download_url"] = "/api/workspaces/" + wsID + "/export/" + jobID + "/download"
			body["completed_at"] = "2026-09-08T10:00:05Z"
		}
		return body
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/api/workspaces/" + wsID + "/export"
		switch {
		case r.Method == http.MethodPost && strings.TrimSuffix(r.URL.Path, "/") == base:
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(job("pending", false))
		case r.Method == http.MethodGet && r.URL.Path == base+"/"+jobID:
			if atomic.AddInt32(&polls, 1) == 1 {
				_ = json.NewEncoder(w).Encode(job("running", false))
				return
			}
			_ = json.NewEncoder(w).Encode(job("completed", true))
		case r.Method == http.MethodGet && r.URL.Path == base+"/"+jobID+"/download":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &polls
}

func setupExportClientEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GOOSAR_SERVER_URL", serverURL)
	t.Setenv("GOOSAR_TOKEN", "test-token")
	t.Setenv("GOOSAR_EXPORT_POLL_INTERVAL", "1ms")
}

func TestWorkspaceExportWaitsForTheJobAndWritesTheArchive(t *testing.T) {
	archive := []byte("\x1f\x8b\x08 pretend this is a tar.gz")
	srv, polls := exportServer(t, archive)
	setupExportClientEnv(t, srv.URL)

	outDir := t.TempDir()
	cmd := newWorkspaceExportTestCmd()
	if err := cmd.Flags().Set("output-dir", outDir); err != nil {
		t.Fatalf("set --output-dir: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runWorkspaceExport(cmd, []string{"44444444-4444-4444-4444-444444444444"})
	})
	if err != nil {
		t.Fatalf("runWorkspaceExport: %v", err)
	}
	if *polls < 2 {
		t.Errorf("polls = %d, want the command to keep polling past the running status", *polls)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	path, _ := result["path"].(string)
	if path == "" {
		t.Fatalf("output has no path: %v", result)
	}
	if result["status"] != "completed" {
		t.Errorf("status = %v, want completed", result["status"])
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if string(written) != string(archive) {
		t.Errorf("archive bytes = %q, want %q", written, archive)
	}
	if filepath.Dir(path) != outDir {
		t.Errorf("archive written to %s, want it under %s", filepath.Dir(path), outDir)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat archive: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("archive mode = %o, want 600", perm)
	}
}

func TestWorkspaceExportWithoutWaitReturnsTheJobOnly(t *testing.T) {
	srv, polls := exportServer(t, []byte("archive"))
	setupExportClientEnv(t, srv.URL)

	outDir := t.TempDir()
	cmd := newWorkspaceExportTestCmd()
	if err := cmd.Flags().Set("output-dir", outDir); err != nil {
		t.Fatalf("set --output-dir: %v", err)
	}
	if err := cmd.Flags().Set("wait", "false"); err != nil {
		t.Fatalf("set --wait: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runWorkspaceExport(cmd, []string{"44444444-4444-4444-4444-444444444444"})
	})
	if err != nil {
		t.Fatalf("runWorkspaceExport: %v", err)
	}
	if *polls != 0 {
		t.Errorf("polls = %d, want 0 without --wait", *polls)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if result["status"] != "pending" {
		t.Errorf("status = %v, want pending", result["status"])
	}
	if _, ok := result["path"]; ok {
		t.Errorf("output carries a path without --wait: %v", result)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("output dir has %d entries, want none without --wait", len(entries))
	}
}

func TestWorkspaceExportReportsAFailedJob(t *testing.T) {
	const wsID = "44444444-4444-4444-4444-444444444444"
	const jobID = "55555555-5555-5555-5555-555555555555"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/api/workspaces/" + wsID + "/export"
		body := map[string]any{
			"id": jobID, "workspace_id": wsID, "status": "failed",
			"error": "export exceeded GOOSAR_EXPORT_MAX_BYTES", "size_bytes": 0,
			"created_at": "2026-09-08T10:00:00Z",
		}
		switch {
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusAccepted)
			body["status"] = "pending"
			body["error"] = nil
			_ = json.NewEncoder(w).Encode(body)
		case r.Method == http.MethodGet && r.URL.Path == base+"/"+jobID:
			_ = json.NewEncoder(w).Encode(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupExportClientEnv(t, srv.URL)

	cmd := newWorkspaceExportTestCmd()
	if err := cmd.Flags().Set("output-dir", t.TempDir()); err != nil {
		t.Fatalf("set --output-dir: %v", err)
	}
	_, err := captureStdout(t, func() error {
		return runWorkspaceExport(cmd, []string{wsID})
	})
	if err == nil {
		t.Fatal("runWorkspaceExport returned nil for a failed job")
	}
	if !strings.Contains(err.Error(), "GOOSAR_EXPORT_MAX_BYTES") {
		t.Errorf("error = %v, want the server's reason", err)
	}
}
