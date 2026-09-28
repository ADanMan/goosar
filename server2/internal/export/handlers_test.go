package export

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adanman/goosar/server2/internal/asset"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

func withHumanActor(r *http.Request, userID string) *http.Request {
	actor := &httpapi.Actor{UserID: userID, IsHuman: true, Source: httpapi.SourceSession}
	return r.WithContext(httpapi.WithActor(r.Context(), actor))
}

func waitForJobStatus(t *testing.T, d *Deps, workspaceID, jobID, want string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, found, err := d.Store.GetJob(context.Background(), workspaceID, jobID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found && job.Status == want {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach status %q in time", jobID, want)
	return Job{}
}

func TestWorkspaceExportEndToEnd(t *testing.T) {
	db := newTestDB(t)
	storeInstance := NewStore(db)
	accountID, workspaceID := seedAccountAndWorkspace(t, storeInstance)

	storage := asset.NewLocalStorage(t.TempDir())
	d := New(db, storage, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// A pending job already occupies the partial unique index (seeded
	// directly at the Store layer, not through the handler, so this
	// assertion does not race the background goroutine the handler starts —
	// on a near-empty workspace that goroutine can finish before a second
	// HTTP call even lands, see TestCreateJobRejectsSecondActiveJob for the
	// Store-level version of this same guarantee).
	blocking, err := storeInstance.CreateJob(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("seed blocking job: %v", err)
	}
	blockedReq := withHumanActor(httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/export", nil), accountID)
	blockedReq.SetPathValue("id", workspaceID)
	blockedRec := httptest.NewRecorder()
	d.handleStartWorkspaceExport(blockedRec, blockedReq)
	if blockedRec.Code != http.StatusConflict {
		t.Fatalf("expected 409 while a job is pending, got %d: %s", blockedRec.Code, blockedRec.Body.String())
	}
	if err := storeInstance.MarkFailed(context.Background(), blocking.ID, "test cleanup"); err != nil {
		t.Fatalf("clear blocking job: %v", err)
	}

	// start (real flow)
	startReq := withHumanActor(httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/export", nil), accountID)
	startReq.SetPathValue("id", workspaceID)
	startRec := httptest.NewRecorder()
	d.handleStartWorkspaceExport(startRec, startReq)
	if startRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", startRec.Code, startRec.Body.String())
	}

	jobs, err := queryJobIDs(storeInstance, workspaceID)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("expected exactly 2 job rows (blocked + real), got %v (err=%v)", jobs, err)
	}
	jobID := ""
	for _, id := range jobs {
		if id != blocking.ID {
			jobID = id
		}
	}
	if jobID == "" {
		t.Fatalf("could not find the real job among %v", jobs)
	}

	completed := waitForJobStatus(t, d, workspaceID, jobID, "completed")
	if completed.SizeBytes <= 0 {
		t.Fatalf("expected positive size_bytes, got %d", completed.SizeBytes)
	}

	// get job status
	getReq := withHumanActor(httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/export/"+jobID, nil), accountID)
	getReq.SetPathValue("id", workspaceID)
	getReq.SetPathValue("jobId", jobID)
	getRec := httptest.NewRecorder()
	d.handleGetWorkspaceExport(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	// download
	dlReq := withHumanActor(httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/export/"+jobID+"/download", nil), accountID)
	dlReq.SetPathValue("id", workspaceID)
	dlReq.SetPathValue("jobId", jobID)
	dlRec := httptest.NewRecorder()
	d.handleDownloadWorkspaceExport(dlRec, dlReq)
	if dlRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on download, got %d: %s", dlRec.Code, dlRec.Body.String())
	}
	if dlRec.Header().Get("Content-Type") != "application/gzip" {
		t.Fatalf("expected application/gzip, got %q", dlRec.Header().Get("Content-Type"))
	}
	if dlRec.Body.Len() == 0 {
		t.Fatal("expected non-empty archive body")
	}
}

func TestStartWorkspaceExportWithoutStorageIs503(t *testing.T) {
	db := newTestDB(t)
	storeInstance := NewStore(db)
	accountID, workspaceID := seedAccountAndWorkspace(t, storeInstance)

	d := New(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := withHumanActor(httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/export", nil), accountID)
	req.SetPathValue("id", workspaceID)
	rec := httptest.NewRecorder()
	d.handleStartWorkspaceExport(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without storage, got %d", rec.Code)
	}
}

func TestMeExportStreamsArchive(t *testing.T) {
	db := newTestDB(t)
	storeInstance := NewStore(db)
	accountID, _ := seedAccountAndWorkspace(t, storeInstance)

	d := New(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := withHumanActor(httptest.NewRequest(http.MethodGet, "/api/me/export", nil), accountID)
	rec := httptest.NewRecorder()
	d.handleMeExport(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "my-data-export.tar.gz") {
		t.Fatalf("unexpected Content-Disposition: %s", rec.Header().Get("Content-Disposition"))
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty archive body")
	}
}

// queryJobIDs — тестовый доступ к id job'ов пространства напрямую по SQL
// (тот же пакет, unexported поле Store.db доступно). CreateJob/GetJob этого
// домена всегда работают с уже известным id, поэтому у Store нет метода
// "список job по воркспейсу" вне теста.
func queryJobIDs(s *Store, workspaceID string) ([]string, error) {
	rows, err := s.db.Pool.Query(context.Background(), `SELECT id FROM space_export_jobs WHERE workspace_id = $1`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
