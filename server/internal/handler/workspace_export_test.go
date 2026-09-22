package handler

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/adanman/goosar/server/internal/storage"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func exportTestStorage(t *testing.T) storage.Storage {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", dir)
	t.Setenv("S3_BUCKET", "")
	store := storage.FromEnv()
	if store == nil {
		t.Fatal("storage.FromEnv() returned nil with LOCAL_UPLOAD_DIR set")
	}
	return store
}

func withExportEnv(t *testing.T) {
	t.Helper()
	t.Setenv(ExportDirEnvVar, t.TempDir())
	previous := testHandler.Storage
	testHandler.Storage = exportTestStorage(t)
	t.Cleanup(func() { testHandler.Storage = previous })
}

func cleanupExportJobs(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM export_job WHERE workspace_id = $1`, testWorkspaceID)
	})
}

func startExport(t *testing.T) (*httptest.ResponseRecorder, ExportJobResponse) {
	t.Helper()
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/export", nil), "id", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.StartWorkspaceExport(rec, req)
	var resp ExportJobResponse
	if rec.Code == http.StatusAccepted {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode start response: %v (%s)", err, rec.Body.String())
		}
	}
	return rec, resp
}

func TestStartWorkspaceExportFilesAJobAndAuditsIt(t *testing.T) {
	withExportEnv(t)
	cleanupExportJobs(t)

	rec, resp := startExport(t)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST export = %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	if resp.Status != "pending" && resp.Status != "running" && resp.Status != "completed" {
		t.Errorf("status = %q, want a live job status", resp.Status)
	}
	if resp.WorkspaceID != testWorkspaceID {
		t.Errorf("workspace_id = %q, want %q", resp.WorkspaceID, testWorkspaceID)
	}

	var audited int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM admin_audit WHERE action = $1 AND target_id = $2`,
		adminAuditActionWorkspaceExport, testWorkspaceID).Scan(&audited); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if audited == 0 {
		t.Error("starting an export wrote no admin_audit row")
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM admin_audit WHERE action = $1 AND target_id = $2`,
			adminAuditActionWorkspaceExport, testWorkspaceID)
	})
}

func TestOnlyOneExportRunsPerWorkspace(t *testing.T) {
	withExportEnv(t)
	cleanupExportJobs(t)

	ctx := context.Background()
	if _, err := testHandler.Queries.CreateExportJob(ctx, db.CreateExportJobParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		RequestedBy: parseUUID(testUserID),
	}); err != nil {
		t.Fatalf("file first job: %v", err)
	}

	rec, _ := startExport(t)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second concurrent export = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

func TestExportRunProducesADownloadableArchive(t *testing.T) {
	withExportEnv(t)
	cleanupExportJobs(t)
	ctx := context.Background()

	job, err := testHandler.Queries.CreateExportJob(ctx, db.CreateExportJobParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		RequestedBy: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("file job: %v", err)
	}
	if err := testHandler.BuildExport(ctx, job); err != nil {
		t.Fatalf("BuildExport: %v", err)
	}

	statusReq := exportJobRequest(t, uuidToString(job.ID))
	statusRec := httptest.NewRecorder()
	testHandler.GetWorkspaceExport(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("GET export = %d, want 200 (%s)", statusRec.Code, statusRec.Body.String())
	}
	var status ExportJobResponse
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Status != "completed" {
		t.Fatalf("status = %q (error %v), want completed", status.Status, status.Error)
	}
	if status.DownloadURL == nil {
		t.Fatal("a completed export offers no download_url")
	}
	if status.SizeBytes <= 0 {
		t.Errorf("size_bytes = %d, want the archive size", status.SizeBytes)
	}
	if len(status.Manifest) == 0 {
		t.Error("a completed export carries no manifest")
	}

	downloadRec := httptest.NewRecorder()
	testHandler.DownloadWorkspaceExport(downloadRec, statusReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download = %d, want 200 (%s)", downloadRec.Code, downloadRec.Body.String())
	}
	if ct := downloadRec.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Errorf("Content-Type = %q, want application/gzip", ct)
	}
	entries := readArchive(t, downloadRec.Body.Bytes())
	manifest, ok := entries["manifest.json"]
	if !ok {
		t.Fatalf("archive has no manifest.json")
	}
	if !strings.Contains(string(manifest), testWorkspaceID) {
		t.Error("the manifest does not name the exported workspace")
	}
	if _, ok := entries["data/issues.json"]; !ok {
		t.Error("archive has no data/issues.json")
	}
}

func TestExportRunIsRecoveredWhenTheProcessDies(t *testing.T) {
	withExportEnv(t)
	cleanupExportJobs(t)
	ctx := context.Background()

	job, err := testHandler.Queries.CreateExportJob(ctx, db.CreateExportJobParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		RequestedBy: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("file job: %v", err)
	}

	if _, err := testHandler.Queries.StartExportJob(ctx, job.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}

	reaped, err := testHandler.Queries.ReapStaleExportJobs(ctx, 0)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	var found bool
	for _, row := range reaped {
		if uuidToString(row.ID) == uuidToString(job.ID) {
			found = true
		}
	}
	if !found {
		t.Fatal("the stale job was not reaped, so its workspace slot would stay blocked forever")
	}

	if _, err := testHandler.Queries.CreateExportJob(ctx, db.CreateExportJobParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		RequestedBy: parseUUID(testUserID),
	}); err != nil {
		t.Fatalf("filing after the reap failed, the slot is still blocked: %v", err)
	}
}

func TestDownloadRefusesAnArchiveThatIsGone(t *testing.T) {
	withExportEnv(t)
	cleanupExportJobs(t)
	ctx := context.Background()

	job, err := testHandler.Queries.CreateExportJob(ctx, db.CreateExportJobParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		RequestedBy: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("file job: %v", err)
	}
	if err := testHandler.BuildExport(ctx, job); err != nil {
		t.Fatalf("BuildExport: %v", err)
	}
	stored, err := testHandler.Queries.GetExportJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := os.Remove(stored.FilePath.String); err != nil {
		t.Fatalf("remove archive: %v", err)
	}

	req := exportJobRequest(t, uuidToString(job.ID))
	rec := httptest.NewRecorder()
	testHandler.DownloadWorkspaceExport(rec, req)
	if rec.Code != http.StatusGone {
		t.Errorf("download of a reclaimed archive = %d, want 410 (%s)", rec.Code, rec.Body.String())
	}
}

func TestSubjectExportStreamsTheCallersOwnData(t *testing.T) {
	req := newRequest(http.MethodGet, "/api/me/export", nil)
	rec := httptest.NewRecorder()
	testHandler.ExportMyData(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/me/export = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Errorf("Content-Type = %q, want application/gzip", ct)
	}
	entries := readArchive(t, rec.Body.Bytes())
	profile, ok := entries["data/profile.json"]
	if !ok {
		t.Fatalf("subject archive has no data/profile.json")
	}
	if !strings.Contains(string(profile), handlerTestEmail) {
		t.Error("the subject archive does not contain the subject's own profile")
	}
	if _, ok := entries["manifest.json"]; !ok {
		t.Error("subject archive has no manifest.json")
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM admin_audit WHERE action = $1`, adminAuditActionUserExport)
	})
}

func exportJobRequest(t *testing.T, jobID string) *http.Request {
	t.Helper()
	req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/export/"+jobID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testWorkspaceID)
	rctx.URLParams.Add("jobId", jobID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func readArchive(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	out := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s: %v", hdr.Name, err)
		}
		out[hdr.Name] = body
	}
	return out
}

func TestExportDirDefaultsInsideTheUploadsVolume(t *testing.T) {
	t.Setenv(ExportDirEnvVar, "")
	t.Setenv("LOCAL_UPLOAD_DIR", "")
	if got := ExportDir(); got != "data/uploads/exports" {
		t.Errorf("ExportDir() = %q, want it under the default uploads volume", got)
	}

	t.Setenv("LOCAL_UPLOAD_DIR", "/srv/goosar/uploads")
	if got := ExportDir(); got != "/srv/goosar/uploads/exports" {
		t.Errorf("ExportDir() = %q, want it under the configured uploads dir", got)
	}

	t.Setenv(ExportDirEnvVar, "/mnt/exports")
	if got := ExportDir(); got != "/mnt/exports" {
		t.Errorf("ExportDir() = %q, want the explicit override to win", got)
	}
}
