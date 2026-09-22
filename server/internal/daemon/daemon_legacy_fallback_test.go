package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClaimTasksWSFirst_LegacyFallbackWhenBatchRouteMissing(t *testing.T) {
	var batchCalls, legacyCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/api/daemon/tasks/claim"):
			batchCalls.Add(1)
			http.Error(w, "404 page not found", http.StatusNotFound)
		case strings.HasSuffix(path, "/runtimes/rt1/tasks/claim"):
			legacyCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"task":{"id":"t1","runtime_id":"rt1","agent":{"name":"a"}}}`))
		case strings.HasSuffix(path, "/runtimes/rt2/tasks/claim"):
			legacyCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"task":{"id":"t2","runtime_id":"rt2","agent":{"name":"b"}}}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))

	tasks, err := d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1", "rt2"}, 5)
	if err != nil {
		t.Fatalf("ClaimTasksWSFirst: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("legacy fallback claimed %d tasks, want 2", len(tasks))
	}
	seen := map[string]bool{}
	for _, task := range tasks {
		seen[task.RuntimeID] = true
	}
	if !seen["rt1"] || !seen["rt2"] {
		t.Fatalf("expected tasks for rt1 and rt2, got %v", seen)
	}
	if batchCalls.Load() != 1 {
		t.Fatalf("batch route called %d times, want exactly 1 (the initial probe)", batchCalls.Load())
	}
	if !d.batchClaimUnsupported.Load() {
		t.Fatal("expected batchClaimUnsupported to be set after a 404")
	}

	if _, err := d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1", "rt2"}, 5); err != nil {
		t.Fatalf("second ClaimTasksWSFirst: %v", err)
	}
	if batchCalls.Load() != 1 {
		t.Fatalf("batch route retried after being marked unsupported; calls=%d", batchCalls.Load())
	}
}

func TestClaimTasksWSFirst_NoDoubleClaimOnDetach(t *testing.T) {
	var httpClaims atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/claim") {
			httpClaims.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[{"id":"http-t","runtime_id":"rt1","agent":{"name":"a"}}]}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))

	var mu sync.Mutex
	var item *wsOutbound
	generation := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		return item, nil
	})
	d.wsRPC.markRPCV1Supported(generation)

	done := make(chan struct{})
	var tasks []*Task
	var err error
	go func() {
		tasks, err = d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 2)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	item.beginWrite()
	mu.Unlock()
	d.wsRPC.attach(nil)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ClaimTasksWSFirst did not return after detach")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks on uncertain WS outcome, got %d", len(tasks))
	}
	if httpClaims.Load() != 0 {
		t.Fatalf("HTTP fallback claimed %d times after an uncertain WS claim; must be 0 to avoid double-claim", httpClaims.Load())
	}
}

func TestClaimTasksWSFirst_HTTPFallbackAfterUncertainCooldown(t *testing.T) {
	originalDelay := wsClaimUncertainFallbackDelay
	wsClaimUncertainFallbackDelay = 25 * time.Millisecond
	t.Cleanup(func() { wsClaimUncertainFallbackDelay = originalDelay })

	var httpClaims, wsClaims atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/claim") {
			httpClaims.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[{"id":"http-t","runtime_id":"rt1","agent":{"name":"a"}}]}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))

	var mu sync.Mutex
	var item *wsOutbound
	frameQueued := make(chan struct{})
	generation := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		close(frameQueued)
		return item, nil
	})
	d.wsRPC.markRPCV1Supported(generation)

	done := make(chan struct{})
	var tasks []*Task
	var err error
	go func() {
		tasks, err = d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 2)
		close(done)
	}()

	select {
	case <-frameQueued:
	case <-time.After(time.Second):
		t.Fatal("WS claim frame was not queued")
	}
	mu.Lock()
	item.beginWrite()
	mu.Unlock()
	d.wsRPC.attach(nil)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ClaimTasksWSFirst did not return after detach")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks on uncertain WS outcome, got %d", len(tasks))
	}
	if httpClaims.Load() != 0 {
		t.Fatalf("HTTP fallback claimed immediately after uncertain WS outcome; calls=%d", httpClaims.Load())
	}

	reconnectGeneration := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		wsClaims.Add(1)
		return &wsOutbound{data: frame}, nil
	})
	d.wsRPC.markRPCV1Supported(reconnectGeneration)

	tasks, err = d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 2)
	if err != nil {
		t.Fatalf("cooldown ClaimTasksWSFirst: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("cooldown claim returned %d tasks, want 0", len(tasks))
	}
	if httpClaims.Load() != 0 || wsClaims.Load() != 0 {
		t.Fatalf("cooldown attempted claims: http=%d ws=%d, want 0/0", httpClaims.Load(), wsClaims.Load())
	}

	time.Sleep(2 * wsClaimUncertainFallbackDelay)
	tasks, err = d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 2)
	if err != nil {
		t.Fatalf("post-cooldown ClaimTasksWSFirst: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "http-t" {
		t.Fatalf("tasks = %#v, want one HTTP fallback task", tasks)
	}
	if httpClaims.Load() != 1 {
		t.Fatalf("HTTP fallback claims = %d, want 1", httpClaims.Load())
	}
	if wsClaims.Load() != 0 {
		t.Fatalf("WS claims after uncertain cooldown = %d, want 0", wsClaims.Load())
	}
}

func TestClaimTasksWSFirst_ReconnectToOldServerSkipsWSRPC(t *testing.T) {
	var httpClaims, wsClaims atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/claim") {
			httpClaims.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[{"id":"http-t","runtime_id":"rt1","agent":{"name":"a"}}]}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	previousGeneration := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		return &wsOutbound{data: frame}, nil
	})
	d.wsRPC.markRPCV1Supported(previousGeneration)
	d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		wsClaims.Add(1)
		return &wsOutbound{data: frame}, nil
	})

	tasks, err := d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 1)
	if err != nil {
		t.Fatalf("ClaimTasksWSFirst: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "http-t" {
		t.Fatalf("tasks = %#v, want HTTP task", tasks)
	}
	if wsClaims.Load() != 0 {
		t.Fatalf("WS claims = %d without rpc-v1 advertisement, want 0", wsClaims.Load())
	}
	if httpClaims.Load() != 1 {
		t.Fatalf("HTTP claims = %d, want 1", httpClaims.Load())
	}
}
