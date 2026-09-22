package daemon

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestReportModelListResult_RetriesOn500AndEventuallySucceeds(t *testing.T) {
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

	d.reportModelListResult(context.Background(), Runtime{ID: "rt-1"}, "req-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); got != 3 {
		t.Fatalf("expected 3 attempts (2 failures + 1 success), got %d", got)
	}
}

func TestReportModelListResult_DoesNotRetryOn4xx(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)

	d, calls := localSkillReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"request not found"}`, http.StatusNotFound)
	})

	d.reportModelListResult(context.Background(), Runtime{ID: "rt-1"}, "req-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected exactly 1 attempt (4xx is terminal), got %d", got)
	}
}

func TestReportModelListResult_SendsCorrectPath(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)

	var path string
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	d.reportModelListResult(context.Background(), Runtime{ID: "rt-a"}, "req-1", map[string]any{"status": "completed"})

	if !strings.HasSuffix(path, "/api/daemon/runtimes/rt-a/models/req-1/result") {
		t.Fatalf("model list report path = %q", path)
	}
}
