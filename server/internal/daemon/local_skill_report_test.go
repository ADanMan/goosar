package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func withFastLocalSkillReportBackoffs(t *testing.T) {
	t.Helper()
	prev := runtimeReportBackoffs
	runtimeReportBackoffs = []time.Duration{0, 0, 0, 0}
	t.Cleanup(func() { runtimeReportBackoffs = prev })
}

func localSkillReportDaemon(t *testing.T, handler http.HandlerFunc) (*Daemon, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return &Daemon{
		client: NewClient(srv.URL),
		logger: slog.Default(),
	}, &calls
}

func TestReportLocalSkillListResult_RetriesOn500AndEventuallySucceeds(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)

	var hits int32
	d, calls := localSkillReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {

		n := atomic.AddInt32(&hits, 1)
		if n <= 2 {
			http.Error(w, "{}", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	d.reportLocalSkillListResult(context.Background(), Runtime{ID: "rt-1"}, "req-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); got != 3 {
		t.Fatalf("expected 3 attempts (2 failures + 1 success), got %d", got)
	}
}

func TestReportLocalSkillListResult_DoesNotRetryOn4xx(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)

	d, calls := localSkillReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {

		http.Error(w, `{"error":"request not found"}`, http.StatusNotFound)
	})

	d.reportLocalSkillListResult(context.Background(), Runtime{ID: "rt-1"}, "req-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected exactly 1 attempt (4xx is terminal), got %d", got)
	}
}

func TestReportLocalSkillImportResult_RetriesOn500AndEventuallySucceeds(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)

	var hits int32
	d, calls := localSkillReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			http.Error(w, "{}", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	d.reportLocalSkillImportResult(context.Background(), Runtime{ID: "rt-1"}, "req-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestReportLocalSkillResult_GivesUpAfterAllAttemptsFail(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)

	d, calls := localSkillReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "{}", http.StatusInternalServerError)
	})

	d.reportLocalSkillListResult(context.Background(), Runtime{ID: "rt-1"}, "req-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); int(got) != len(runtimeReportBackoffs) {
		t.Fatalf("expected %d attempts, got %d", len(runtimeReportBackoffs), got)
	}
}

func TestReportLocalSkillResult_AbortsOnContextCancel(t *testing.T) {

	prev := runtimeReportBackoffs
	runtimeReportBackoffs = []time.Duration{0, 30 * time.Second}
	t.Cleanup(func() { runtimeReportBackoffs = prev })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	d, calls := localSkillReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		http.Error(w, "{}", http.StatusInternalServerError)
	})

	d.reportLocalSkillListResult(ctx, Runtime{ID: "rt-1"}, "req-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected exactly 1 attempt before cancel, got %d", got)
	}
}

func TestReportLocalSkillResult_SendsCorrectPath(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)

	var listPath, importPath string
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {

		if strings.Contains(r.URL.Path, "/import/") {
			importPath = r.URL.Path
		} else {
			listPath = r.URL.Path
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	ctx := context.Background()
	d.reportLocalSkillListResult(ctx, Runtime{ID: "rt-a"}, "req-list", map[string]any{"status": "completed"})
	d.reportLocalSkillImportResult(ctx, Runtime{ID: "rt-a"}, "req-import", map[string]any{"status": "completed"})

	if !strings.HasSuffix(listPath, "/api/daemon/runtimes/rt-a/local-skills/req-list/result") {
		t.Fatalf("list path = %q", listPath)
	}
	if !strings.HasSuffix(importPath, "/api/daemon/runtimes/rt-a/local-skills/import/req-import/result") {
		t.Fatalf("import path = %q", importPath)
	}
}
