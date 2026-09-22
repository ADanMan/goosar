package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestProfileSetSignature_StableUnderReorder(t *testing.T) {
	a := []RuntimeProfile{
		{ID: "p-a", ProtocolFamily: "runtime-e", CommandName: "a", Enabled: true},
		{ID: "p-b", ProtocolFamily: "runtime-c", CommandName: "b", Enabled: true},
	}
	b := []RuntimeProfile{
		{ID: "p-b", ProtocolFamily: "runtime-c", CommandName: "b", Enabled: true},
		{ID: "p-a", ProtocolFamily: "runtime-e", CommandName: "a", Enabled: true},
	}
	if profileSetSignature(a) != profileSetSignature(b) {
		t.Errorf("digest must be order-independent")
	}
}

func TestProfileSetSignature_DetectsRegistrationAffectingChanges(t *testing.T) {
	base := []RuntimeProfile{{
		ID:             "p1",
		ProtocolFamily: "runtime-e",
		CommandName:    "company-codex",
		FixedArgs:      []string{"--foo"},
		Visibility:     "workspace",
		Enabled:        true,
	}}
	baseSig := profileSetSignature(base)

	if profileSetSignature(nil) == baseSig {
		t.Errorf("empty list must hash differently from a populated list")
	}

	cases := []struct {
		name   string
		mutate func([]RuntimeProfile) []RuntimeProfile
	}{
		{"add new profile", func(in []RuntimeProfile) []RuntimeProfile {
			return append(in, RuntimeProfile{ID: "p2", ProtocolFamily: "runtime-c", CommandName: "c", Enabled: true})
		}},
		{"flip enabled", func(in []RuntimeProfile) []RuntimeProfile {
			out := append([]RuntimeProfile(nil), in...)
			out[0].Enabled = !out[0].Enabled
			return out
		}},
		{"change command_name", func(in []RuntimeProfile) []RuntimeProfile {
			out := append([]RuntimeProfile(nil), in...)
			out[0].CommandName = "different-bin"
			return out
		}},
		{"change protocol_family", func(in []RuntimeProfile) []RuntimeProfile {
			out := append([]RuntimeProfile(nil), in...)
			out[0].ProtocolFamily = "runtime-c"
			return out
		}},
		{"change fixed_args", func(in []RuntimeProfile) []RuntimeProfile {
			out := append([]RuntimeProfile(nil), in...)
			out[0].FixedArgs = []string{"--foo", "--bar"}
			return out
		}},
		{"change visibility", func(in []RuntimeProfile) []RuntimeProfile {
			out := append([]RuntimeProfile(nil), in...)
			out[0].Visibility = "private"
			return out
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := profileSetSignature(tc.mutate(base))
			if got == baseSig {
				t.Errorf("digest must change when %s; baseSig=%s mutatedSig=%s",
					tc.name, baseSig, got)
			}
		})
	}
}

type driftFixture struct {
	daemon              *Daemon
	server              *httptest.Server
	registerCalls       atomic.Int32
	recoverOrphansCalls []string
	recoverOrphansMu    sync.Mutex
	deregisterCalls     [][]string
	deregisterMu        sync.Mutex
	currentProfiles     []RuntimeProfile
}

func (fx *driftFixture) setProfiles(profiles []RuntimeProfile) {
	fx.currentProfiles = profiles
}

func (fx *driftFixture) recordedRecoverOrphans() []string {
	fx.recoverOrphansMu.Lock()
	defer fx.recoverOrphansMu.Unlock()
	out := make([]string, len(fx.recoverOrphansCalls))
	copy(out, fx.recoverOrphansCalls)
	return out
}

func (fx *driftFixture) recordedDeregisters() [][]string {
	fx.deregisterMu.Lock()
	defer fx.deregisterMu.Unlock()
	out := make([][]string, len(fx.deregisterCalls))
	for i, ids := range fx.deregisterCalls {
		cp := make([]string, len(ids))
		copy(cp, ids)
		out[i] = cp
	}
	return out
}

func newDriftFixture(t *testing.T, initial []RuntimeProfile) *driftFixture {
	t.Helper()
	fx := &driftFixture{currentProfiles: initial}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/daemon/register":
			fx.registerCalls.Add(1)
			var body struct {
				Runtimes []map[string]any `json:"runtimes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			var resp RegisterResponse
			for i, rt := range body.Runtimes {
				id := "rt-" + strconv.Itoa(i)
				profileID, _ := rt["profile_id"].(string)
				typ, _ := rt["type"].(string)
				resp.Runtimes = append(resp.Runtimes, Runtime{
					ID: id, Name: "n", Provider: typ, Status: "online", ProfileID: profileID,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasPrefix(r.URL.Path, "/api/daemon/runtimes/") && strings.HasSuffix(r.URL.Path, "/recover-orphans"):

			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/daemon/runtimes/"), "/")
			runtimeID := parts[0]
			fx.recoverOrphansMu.Lock()
			fx.recoverOrphansCalls = append(fx.recoverOrphansCalls, runtimeID)
			fx.recoverOrphansMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"orphaned":0,"retried":0}`))
		case r.URL.Path == "/api/daemon/deregister":
			var body struct {
				RuntimeIDs []string `json:"runtime_ids"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			fx.deregisterMu.Lock()
			ids := make([]string, len(body.RuntimeIDs))
			copy(ids, body.RuntimeIDs)
			fx.deregisterCalls = append(fx.deregisterCalls, ids)
			fx.deregisterMu.Unlock()
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/runtime-profiles"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RuntimeProfilesResponse{
				WorkspaceID:     "ws-1",
				RuntimeProfiles: fx.currentProfiles,
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

func TestSyncWorkspacesSkipsRuntimeProfileRefreshOnExistingWorkspace(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"
	var profileCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/daemon/workspaces":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]WorkspaceInfo{{ID: workspaceID, Name: "ws"}})
		case "/api/daemon/workspaces/" + workspaceID + "/runtime-profiles":
			profileCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RuntimeProfilesResponse{WorkspaceID: workspaceID})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.workspaces[workspaceID] = newWorkspaceState(workspaceID, []string{"rt-1"}, "", nil, nil)
	d.workspaces[workspaceID].profileSetSig = profileSetSignature(nil)
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1", Provider: "runtime-e"}

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if got := profileCalls.Load(); got != 0 {
		t.Fatalf("workspace sync polled runtime-profiles %d times, want 0", got)
	}
}

func TestSyncWorkspacesRefreshesRuntimeProfilesOnReconcile(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"
	var profileCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/daemon/workspaces":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]WorkspaceInfo{{ID: workspaceID, Name: "ws"}})
		case "/api/daemon/workspaces/" + workspaceID + "/runtime-profiles":
			profileCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RuntimeProfilesResponse{WorkspaceID: workspaceID})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.workspaces[workspaceID] = newWorkspaceState(workspaceID, []string{"rt-1"}, "", nil, nil)
	d.workspaces[workspaceID].profileSetSig = profileSetSignature(nil)
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1", Provider: "runtime-e"}

	if err := d.syncWorkspacesFromAPI(context.Background(), true); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if got := profileCalls.Load(); got != 1 {
		t.Fatalf("reconcile fetched runtime-profiles %d times, want 1", got)
	}
}

func TestRefreshWorkspaceRuntimeProfiles_NoDrift_DoesNotReregister(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	profiles := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "runtime-e", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	fx := newDriftFixture(t, profiles)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{}

	resp, profileSig, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("initial register: %v", err)
	}
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{resp.Runtimes[0].ID}, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = profileSig
	for _, rt := range resp.Runtimes {
		d.runtimeIndex[rt.ID] = rt
	}
	if fx.registerCalls.Load() != 1 {
		t.Fatalf("setup expected 1 register call, got %d", fx.registerCalls.Load())
	}

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}
	if fx.registerCalls.Load() != 1 {
		t.Errorf("no-drift refresh must not re-register; got %d total register calls", fx.registerCalls.Load())
	}
}

func TestRefreshWorkspaceRuntimeProfiles_NewProfileTriggersReregister(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{
		"company-codex": "/opt/bin/company-codex",
		"team-claude":   "/opt/bin/team-claude",
	})
	initial := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "runtime-e", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	fx := newDriftFixture(t, initial)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{}

	resp, profileSig, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("initial register: %v", err)
	}
	ids := make([]string, 0, len(resp.Runtimes))
	for _, rt := range resp.Runtimes {
		ids = append(ids, rt.ID)
		d.runtimeIndex[rt.ID] = rt
	}
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", ids, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = profileSig
	beforeRegisterCalls := fx.registerCalls.Load()

	fx.setProfiles([]RuntimeProfile{
		{
			ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
			ProtocolFamily: "runtime-e", CommandName: "company-codex",
			Visibility: "workspace", Enabled: true,
		},
		{
			ID: "prof-2", WorkspaceID: "ws-1", DisplayName: "Team Claude",
			ProtocolFamily: "runtime-c", CommandName: "team-claude",
			Visibility: "workspace", Enabled: true,
		},
	})

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	if got := fx.registerCalls.Load(); got != beforeRegisterCalls+1 {
		t.Errorf("new profile must trigger one re-register; before=%d after=%d", beforeRegisterCalls, got)
	}

	d.mu.Lock()
	var seenProf2 bool
	for _, rt := range d.runtimeIndex {
		if rt.ProfileID == "prof-2" {
			seenProf2 = true
			break
		}
	}
	d.mu.Unlock()
	if !seenProf2 {
		t.Errorf("expected runtimeIndex to contain a runtime for prof-2 after refresh")
	}

	stableCalls := fx.registerCalls.Load()
	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if got := fx.registerCalls.Load(); got != stableCalls {
		t.Errorf("steady-state refresh must not re-register; before=%d after=%d", stableCalls, got)
	}

	if got := fx.recordedRecoverOrphans(); len(got) != 0 {
		t.Errorf("drift path must not trigger recover-orphans for any runtime; got %v", got)
	}
}

func TestRefreshWorkspaceRuntimeProfiles_DriftWithRunningRuntimeSkipsOrphanRecovery(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{
		"company-codex": "/opt/bin/company-codex",
		"team-claude":   "/opt/bin/team-claude",
	})

	initial := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "runtime-e", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	fx := newDriftFixture(t, initial)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"runtime-c": {Path: "/usr/bin/true"}}

	resp, profileSig, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("initial register: %v", err)
	}
	ids := make([]string, 0, len(resp.Runtimes))
	for _, rt := range resp.Runtimes {
		ids = append(ids, rt.ID)
		d.runtimeIndex[rt.ID] = rt
	}
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", ids, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = profileSig
	if len(ids) < 2 {
		t.Fatalf("setup expected at least 2 runtimes (built-in + custom); got %d", len(ids))
	}

	fx.setProfiles([]RuntimeProfile{
		{
			ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
			ProtocolFamily: "runtime-e", CommandName: "company-codex",
			Visibility: "workspace", Enabled: true,
		},
		{
			ID: "prof-2", WorkspaceID: "ws-1", DisplayName: "Team Claude",
			ProtocolFamily: "runtime-c", CommandName: "team-claude",
			Visibility: "workspace", Enabled: true,
		},
	})

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	if got := fx.recordedRecoverOrphans(); len(got) != 0 {
		t.Errorf("drift refresh leaked recover-orphans calls (would fail running tasks on existing runtimes): %v", got)
	}
}

func TestRefreshWorkspaceRuntimeProfiles_DisableConvergesCustomOnlyDaemon(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	initial := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "runtime-e", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	fx := newDriftFixture(t, initial)
	d := fx.daemon

	d.cfg.Agents = map[string]AgentEntry{}

	resp, profileSig, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("initial register: %v", err)
	}
	if len(resp.Runtimes) != 1 {
		t.Fatalf("setup expected exactly one runtime; got %d", len(resp.Runtimes))
	}
	initialRuntimeID := resp.Runtimes[0].ID
	d.runtimeIndex[initialRuntimeID] = resp.Runtimes[0]
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{initialRuntimeID}, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = profileSig

	fx.setProfiles(nil)

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	d.mu.Lock()
	gotRuntimeIDs := append([]string(nil), d.workspaces["ws-1"].runtimeIDs...)
	_, stillIndexed := d.runtimeIndex[initialRuntimeID]
	gotSig := d.workspaces["ws-1"].profileSetSig
	d.mu.Unlock()
	if len(gotRuntimeIDs) != 0 {
		t.Errorf("workspaceState.runtimeIDs must be empty after convergence-to-zero; got %v", gotRuntimeIDs)
	}
	if stillIndexed {
		t.Errorf("runtimeIndex must drop the previously-tracked runtime %q after convergence-to-zero", initialRuntimeID)
	}
	if gotSig != profileSetSignature(nil) {
		t.Errorf("converged signature must match the empty-profile-list digest so duplicate notifications are no-ops; got %q want %q", gotSig, profileSetSignature(nil))
	}

	deregs := fx.recordedDeregisters()
	if len(deregs) == 0 {
		t.Fatalf("expected one Deregister call for the orphaned runtime ID; got none")
	}
	var sawInitial bool
	for _, ids := range deregs {
		for _, id := range ids {
			if id == initialRuntimeID {
				sawInitial = true
			}
		}
	}
	if !sawInitial {
		t.Errorf("Deregister payload must include the initial runtime ID %q; got %v", initialRuntimeID, deregs)
	}

	if got := fx.recordedRecoverOrphans(); len(got) != 0 {
		t.Errorf("convergence-to-zero must not trigger recover-orphans; got %v", got)
	}

	stableRegisters := fx.registerCalls.Load()
	stableDeregs := len(fx.recordedDeregisters())
	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("second refresh after convergence: %v", err)
	}
	if got := fx.registerCalls.Load(); got != stableRegisters {
		t.Errorf("converged steady state must not re-register; before=%d after=%d", stableRegisters, got)
	}
	if got := len(fx.recordedDeregisters()); got != stableDeregs {
		t.Errorf("converged steady state must not deregister again; before=%d after=%d", stableDeregs, got)
	}
}

func TestRefreshWorkspaceRuntimeProfiles_DisableOneOfManyDeregistersDroppedID(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	initial := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "runtime-e", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	fx := newDriftFixture(t, initial)
	d := fx.daemon

	d.cfg.Agents = map[string]AgentEntry{"runtime-c": {Path: "/usr/bin/true"}}

	resp, profileSig, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("initial register: %v", err)
	}
	if len(resp.Runtimes) != 2 {
		t.Fatalf("setup expected 2 runtimes; got %d (%+v)", len(resp.Runtimes), resp.Runtimes)
	}
	var customID string
	for _, rt := range resp.Runtimes {
		d.runtimeIndex[rt.ID] = rt
		if rt.ProfileID == "prof-1" {
			customID = rt.ID
		}
	}
	if customID == "" {
		t.Fatalf("setup expected a runtime for prof-1; got %+v", resp.Runtimes)
	}
	initialIDs := []string{resp.Runtimes[0].ID, resp.Runtimes[1].ID}
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", initialIDs, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = profileSig

	fx.setProfiles(nil)

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	deregs := fx.recordedDeregisters()
	var sawCustom bool
	for _, ids := range deregs {
		for _, id := range ids {
			if id == customID {
				sawCustom = true
			}
		}
	}
	if !sawCustom {
		t.Errorf("Deregister payload must include the dropped custom runtime ID %q; got %v", customID, deregs)
	}

	for _, ids := range deregs {
		for _, id := range ids {
			if id != customID {
				t.Errorf("Deregister leaked surviving runtime ID %q (only %q should be deregistered)", id, customID)
			}
		}
	}

	if got := fx.recordedRecoverOrphans(); len(got) != 0 {
		t.Errorf("partial-drift refresh leaked recover-orphans calls: %v", got)
	}
}

func TestRefreshWorkspaceRuntimeProfiles_FetchErrorIsBestEffort(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	profiles := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "runtime-e", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/runtime-profiles") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	d := freshDaemon(srv.URL)
	d.profileLaunchSpecs = make(map[string]profileLaunchSpec)
	knownSig := profileSetSignature(profiles)
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{"rt-1"}, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = knownSig

	err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1")
	if err == nil {
		t.Fatalf("404 must surface as an error so the caller can log it at debug")
	}

	d.mu.Lock()
	gotSig := d.workspaces["ws-1"].profileSetSig
	d.mu.Unlock()
	if gotSig != knownSig {
		t.Errorf("transient fetch error must not clobber cached sig; want %q got %q", knownSig, gotSig)
	}
}
