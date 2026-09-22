package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type batchFixture struct {
	daemon *Daemon
	server *httptest.Server

	mu sync.Mutex

	workspaces []WorkspaceInfo

	profiles map[string][]RuntimeProfile

	registered []registeredCall

	probes map[string]int

	probeErr func(path string, attempt int) error
}

type registeredCall struct {
	workspaceID string
	types       []string
}

func (fx *batchFixture) setWorkspaces(ws ...WorkspaceInfo) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.workspaces = ws
}

func (fx *batchFixture) probeCount(path string) int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.probes[path]
}

func (fx *batchFixture) setProbeErr(fn func(path string, attempt int) error) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.probeErr = fn
}

func (fx *batchFixture) registrationFor(workspaceID string) ([]string, int) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	var types []string
	var calls int
	for _, call := range fx.registered {
		if call.workspaceID == workspaceID {
			types = call.types
			calls++
		}
	}
	return types, calls
}

func (fx *batchFixture) registerCallCount() int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return len(fx.registered)
}

func newBatchFixture(t *testing.T) *batchFixture {
	t.Helper()
	fx := &batchFixture{
		profiles: make(map[string][]RuntimeProfile),
		probes:   make(map[string]int),
	}

	origDetect := detectAgentVersion
	origCheck := checkAgentMinVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetect
		checkAgentMinVersion = origCheck
	})
	detectAgentVersion = func(_ context.Context, path string) (string, error) {
		fx.mu.Lock()
		fx.probes[path]++
		attempt := fx.probes[path]
		probeErr := fx.probeErr
		fx.mu.Unlock()
		if probeErr != nil {
			if err := probeErr(path, attempt); err != nil {
				return "", err
			}
		}
		return "9.9.9", nil
	}
	checkAgentMinVersion = func(_, _ string) error { return nil }

	var runtimeSeq atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/daemon/workspaces":
			fx.mu.Lock()
			list := append([]WorkspaceInfo(nil), fx.workspaces...)
			fx.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(list)
		case r.URL.Path == "/api/daemon/register":
			var body struct {
				WorkspaceID string              `json:"workspace_id"`
				Runtimes    []map[string]string `json:"runtimes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			call := registeredCall{workspaceID: body.WorkspaceID}
			var resp RegisterResponse
			for _, rt := range body.Runtimes {
				call.types = append(call.types, rt["type"])
				resp.Runtimes = append(resp.Runtimes, Runtime{
					ID:        "rt-" + strconv.Itoa(int(runtimeSeq.Add(1))),
					Name:      rt["name"],
					Provider:  rt["type"],
					Status:    "online",
					ProfileID: rt["profile_id"],
				})
			}
			fx.mu.Lock()
			fx.registered = append(fx.registered, call)
			fx.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasSuffix(r.URL.Path, "/runtime-profiles"):
			workspaceID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/daemon/workspaces/"), "/runtime-profiles")
			fx.mu.Lock()
			profiles := fx.profiles[workspaceID]
			fx.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RuntimeProfilesResponse{
				WorkspaceID:     workspaceID,
				RuntimeProfiles: profiles,
			})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.profileLaunchSpecs = make(map[string]profileLaunchSpec)
	fx.daemon = d
	fx.server = srv
	return fx
}

func TestSyncWorkspaces_ProbesBuiltinCLIsOncePerBatch(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{
		"runtime-c": {Path: "/fake/claude"},
		"runtime-e": {Path: "/fake/codex"},
	}
	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
	)

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	for _, path := range []string{"/fake/claude", "/fake/codex"} {
		if got := fx.probeCount(path); got != 1 {
			t.Errorf("probed %s %d times, want 1 (built-ins are machine-level, not per-workspace)", path, got)
		}
	}

	if got := fx.registerCallCount(); got != 2 {
		t.Fatalf("got %d Register calls, want 2 (one per workspace)", got)
	}
	for _, workspaceID := range []string{"ws-1", "ws-2"} {
		types, calls := fx.registrationFor(workspaceID)
		if calls != 1 {
			t.Errorf("%s registered %d times, want 1", workspaceID, calls)
		}
		sort.Strings(types)
		if len(types) != 2 || types[0] != "runtime-c" || types[1] != "runtime-e" {
			t.Errorf("%s registered runtimes %v, want [claude codex]", workspaceID, types)
		}
	}
}

func TestSyncWorkspaces_CustomProfilesDoNotLeakAcrossWorkspaces(t *testing.T) {
	fx := newBatchFixture(t)
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"runtime-c": {Path: "/fake/claude"}}
	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
	)
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "runtime-e", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	withProfile, _ := fx.registrationFor("ws-1")
	sort.Strings(withProfile)
	if len(withProfile) != 2 || withProfile[0] != "runtime-c" || withProfile[1] != "runtime-e" {
		t.Errorf("ws-1 registered %v, want its built-in plus its custom profile [runtime-c runtime-e]", withProfile)
	}

	withoutProfile, _ := fx.registrationFor("ws-2")
	if len(withoutProfile) != 1 || withoutProfile[0] != "runtime-c" {
		t.Errorf("ws-2 registered %v, want only the built-in [runtime-c]; ws-1's profile leaked", withoutProfile)
	}
}

func TestSyncWorkspaces_ReprobesOnNextSync(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"runtime-c": {Path: "/fake/claude"}}

	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if got := fx.probeCount("/fake/claude"); got != 1 {
		t.Fatalf("first sync probed %d times, want 1", got)
	}

	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
	)
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if got := fx.probeCount("/fake/claude"); got != 2 {
		t.Fatalf("second sync probed %d times total, want 2 (one fresh probe for the new workspace)", got)
	}
}

func TestSyncWorkspaces_SkipsProbeWhenNothingToRegister(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"runtime-c": {Path: "/fake/claude"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{"rt-1"}, "", nil, nil)
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1", Provider: "runtime-c"}

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if got := fx.probeCount("/fake/claude"); got != 0 {
		t.Fatalf("steady-state sync probed %d times, want 0", got)
	}
}

func TestRegisterRuntimesForWorkspace_ProbesOnStandaloneCall(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"runtime-c": {Path: "/fake/claude"}}

	for i := 1; i <= 2; i++ {
		if _, _, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1"); err != nil {
			t.Fatalf("register #%d: %v", i, err)
		}
		if got := fx.probeCount("/fake/claude"); got != i {
			t.Fatalf("after %d standalone registrations, probed %d times; want %d", i, got, i)
		}
	}
}

func stubProbeRetry(t *testing.T, delay, window time.Duration) {
	t.Helper()
	origDelay, origWindow := runtimeVersionProbeRetryDelay, runtimeVersionProbeRetryWindow
	t.Cleanup(func() {
		runtimeVersionProbeRetryDelay = origDelay
		runtimeVersionProbeRetryWindow = origWindow
	})
	runtimeVersionProbeRetryDelay = delay
	runtimeVersionProbeRetryWindow = window
}

func TestSyncWorkspaces_RetriesFailedProbeForWholeBatch(t *testing.T) {
	fx := newBatchFixture(t)
	stubProbeRetry(t, time.Millisecond, time.Second)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{
		"runtime-c": {Path: "/fake/claude"},
		"runtime-e": {Path: "/fake/codex"},
	}

	fx.setProbeErr(func(path string, attempt int) error {
		if path == "/fake/codex" && attempt == 1 {
			return errors.New("fork/exec: resource temporarily unavailable")
		}
		return nil
	})
	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
	)

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if got := fx.probeCount("/fake/codex"); got != 2 {
		t.Errorf("probed /fake/codex %d times, want 2 (one failure + one retry)", got)
	}

	if got := fx.probeCount("/fake/claude"); got != 1 {
		t.Errorf("probed /fake/claude %d times, want 1 (only the failed provider retries)", got)
	}
	for _, workspaceID := range []string{"ws-1", "ws-2"} {
		types, _ := fx.registrationFor(workspaceID)
		sort.Strings(types)
		if len(types) != 2 || types[0] != "runtime-c" || types[1] != "runtime-e" {
			t.Errorf("%s registered %v, want [claude codex]; one transient probe failure cost the whole batch a runtime", workspaceID, types)
		}
	}
}

func TestDetectBuiltinRuntimes_DropsProviderAfterRetriesExhausted(t *testing.T) {
	fx := newBatchFixture(t)
	stubProbeRetry(t, time.Millisecond, time.Second)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{
		"runtime-c": {Path: "/fake/claude"},
		"runtime-e": {Path: "/fake/codex"},
	}
	fx.setProbeErr(func(path string, _ int) error {
		if path == "/fake/codex" {
			return errors.New("no such file or directory")
		}
		return nil
	})

	runtimes := d.detectBuiltinRuntimes(context.Background())

	if got := fx.probeCount("/fake/codex"); got != runtimeVersionProbeAttempts {
		t.Errorf("probed /fake/codex %d times, want %d (retry must stay bounded)", got, runtimeVersionProbeAttempts)
	}
	if len(runtimes) != 1 || runtimes[0]["type"] != "runtime-c" {
		t.Errorf("detected %v, want only claude", runtimes)
	}
}

func TestDetectBuiltinRuntimes_DoesNotRetrySlowProbe(t *testing.T) {
	fx := newBatchFixture(t)
	const probeWindow = 20 * time.Millisecond
	stubProbeRetry(t, time.Millisecond, probeWindow)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"runtime-e": {Path: "/fake/codex"}}
	fx.setProbeErr(func(path string, _ int) error {
		time.Sleep(2 * probeWindow)
		return errors.New("signal: killed")
	})

	if runtimes := d.detectBuiltinRuntimes(context.Background()); len(runtimes) != 0 {
		t.Fatalf("detected %v, want none", runtimes)
	}
	if got := fx.probeCount("/fake/codex"); got != 1 {
		t.Errorf("probed /fake/codex %d times, want 1 (a probe that ran to its timeout is not retried)", got)
	}
}

func vanishedPinnedPath(t *testing.T) (missing, healed string) {
	t.Helper()
	root := t.TempDir()
	stableBin := filepath.Join(root, "bin")
	writeExecStub(t, filepath.Join(stableBin, "codex"))

	t.Setenv("PATH", stableBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	return filepath.Join(root, "gone", "codex"), canonicalExecutablePath(filepath.Join(stableBin, "codex"))
}

func countingVersionProbe(t *testing.T, answer func(path string) (string, error)) *atomic.Int32 {
	t.Helper()
	origDetect := detectAgentVersion
	origCheck := checkAgentMinVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetect
		checkAgentMinVersion = origCheck
	})
	var probes atomic.Int32
	detectAgentVersion = func(_ context.Context, path string) (string, error) {
		probes.Add(1)
		return answer(path)
	}
	checkAgentMinVersion = func(_, _ string) error { return nil }
	return &probes
}

func TestDetectBuiltinRuntimes_DoesNotRetryWhenSelfHealBurnsTheWindow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH/exec-bit stub layout is POSIX-specific")
	}
	const probeWindow = 20 * time.Millisecond
	stubProbeRetry(t, time.Millisecond, probeWindow)
	missing, _ := vanishedPinnedPath(t)
	probes := countingVersionProbe(t, func(path string) (string, error) {
		if path == missing {

			return "", errors.New("no such file or directory")
		}

		time.Sleep(2 * probeWindow)
		return "", errors.New("signal: killed")
	})

	d := freshDaemon("")
	d.cfg.Agents = map[string]AgentEntry{"runtime-e": {Path: missing, Command: "codex"}}

	if runtimes := d.detectBuiltinRuntimes(context.Background()); len(runtimes) != 0 {
		t.Fatalf("detected %v, want none", runtimes)
	}
	if got := probes.Load(); got != 2 {
		t.Errorf("ran %d version probes, want 2 (one self-heal + one outer probe); the retry window must cover the whole attempt, not just the outer probe", got)
	}
}

func TestDetectBuiltinRuntimes_BoundsRetryWhenSelfHealRejectsVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH/exec-bit stub layout is POSIX-specific")
	}
	stubProbeRetry(t, time.Millisecond, time.Second)
	missing, healed := vanishedPinnedPath(t)
	probes := countingVersionProbe(t, func(path string) (string, error) {
		if path == missing {
			return "", errors.New("no such file or directory")
		}
		return "0.0.1", nil
	})
	checkAgentMinVersion = func(_, version string) error {
		if version == "0.0.1" {
			return errors.New("version too old")
		}
		return nil
	}

	d := freshDaemon("")
	d.cfg.Agents = map[string]AgentEntry{"runtime-e": {Path: missing, Command: "codex"}}

	if runtimes := d.detectBuiltinRuntimes(context.Background()); len(runtimes) != 0 {
		t.Fatalf("detected %v (healed path %q), want none: a below-minimum candidate must not be adopted", runtimes, healed)
	}

	if got := probes.Load(); got != int32(2*runtimeVersionProbeAttempts) {
		t.Errorf("ran %d version probes, want %d (%d bounded attempts)", got, 2*runtimeVersionProbeAttempts, runtimeVersionProbeAttempts)
	}
}

func TestDetectBuiltinRuntimes_DoesNotRetryMinVersionRejection(t *testing.T) {
	fx := newBatchFixture(t)
	stubProbeRetry(t, time.Millisecond, time.Second)
	checkAgentMinVersion = func(provider, _ string) error {
		if provider == "runtime-e" {
			return errors.New("version too old")
		}
		return nil
	}
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"runtime-e": {Path: "/fake/codex"}}

	if runtimes := d.detectBuiltinRuntimes(context.Background()); len(runtimes) != 0 {
		t.Fatalf("detected %v, want none", runtimes)
	}
	if got := fx.probeCount("/fake/codex"); got != 1 {
		t.Errorf("probed /fake/codex %d times, want 1 (a below-minimum version is not retried)", got)
	}
}

func TestRegisterRuntimesForWorkspaceBatch_DoesNotMutateSharedPayload(t *testing.T) {
	fx := newBatchFixture(t)
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{}
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "runtime-e", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}

	builtins := []map[string]string{
		{"name": "Runtime C", "type": "runtime-c", "version": "9.9.9", "status": "online"},
	}
	if _, _, err := d.registerRuntimesForWorkspaceBatch(context.Background(), "ws-1", builtins); err != nil {
		t.Fatalf("batch register: %v", err)
	}

	if len(builtins) != 1 || builtins[0]["type"] != "runtime-c" {
		t.Fatalf("shared built-in payload was mutated: %v", builtins)
	}
}
