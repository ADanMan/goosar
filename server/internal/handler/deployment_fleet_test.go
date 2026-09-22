package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func deploymentFleetTestRoutes() chi.Router {
	r := chi.NewRouter()
	r.Route("/api/deployment", func(r chi.Router) {
		r.Use(RequireHumanActor)
		r.Get("/fleet", testHandler.ListDeploymentFleet)
	})
	return r
}

func fleetRuntimeFixture(t *testing.T, workspaceID, ownerID, daemonID, name, cliVersion, status, lastSeenSQL string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime
			(workspace_id, owner_id, daemon_id, name, runtime_mode, provider,
			 status, device_info, metadata, last_seen_at)
		VALUES ($1, $2, $3, $4, 'local', $4, $5, 'MacBook',
			jsonb_build_object('cli_version', $6::text), `+lastSeenSQL+`)
		RETURNING id
	`, workspaceID, ownerID, daemonID, name, status, cliVersion).Scan(&id); err != nil {
		t.Fatalf("create fleet runtime fixture: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE runtime_id = $1`, id)
		testPool.Exec(ctx, `DELETE FROM agent WHERE runtime_id = $1`, id)
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, id)
	})
	return id
}

func fleetAgentFixture(t *testing.T, workspaceID, ownerID, runtimeID, name, systemKey, status string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (workspace_id, owner_id, runtime_id, name, runtime_mode, status, system_key)
		VALUES ($1, $2, $3, $4, 'local', $5, NULLIF($6::text, ''))
		RETURNING id
	`, workspaceID, ownerID, runtimeID, name, status, systemKey).Scan(&id); err != nil {
		t.Fatalf("create fleet agent fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, id)
	})
	return id
}

func fleetTaskFixture(t *testing.T, agentID, runtimeID, issueID, status string, startedAgoSecs int) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at)
		VALUES ($1, $2, $3, $4, now() - make_interval(secs => $5::double precision))
	`, agentID, issueID, runtimeID, status, startedAgoSecs); err != nil {
		t.Fatalf("create fleet task fixture: %v", err)
	}
}

func TestDeploymentFleet_RejectNonAdmin(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	w := httptest.NewRecorder()
	deploymentFleetTestRoutes().ServeHTTP(w, newRequest(http.MethodGet, "/api/deployment/fleet", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin: status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestDeploymentFleet_RejectMachineActors(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentFleetTestRoutes()
	for _, source := range []string{"task_token", "cloud_pat"} {
		t.Run(source, func(t *testing.T) {
			req := newRequest(http.MethodGet, "/api/deployment/fleet", nil)
			req.Header.Set("X-Actor-Source", source)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("machine actor %s: status = %d, want 403: %s", source, w.Code, w.Body.String())
			}
		})
	}
}

func TestDeploymentFleet_GroupsMachinesAcrossWorkspaces(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)

	adminID, _ := deploymentUserFixture(t, "fleet")
	grantDeploymentAdminFixture(t, adminID)

	otherWorkspaceID, otherOwnerID := secondWorkspaceFixture(t)
	daemonA := "fleet-a-" + randomID()[:8]
	daemonB := "fleet-b-" + randomID()[:8]

	rtA1 := fleetRuntimeFixture(t, testWorkspaceID, testUserID, daemonA, "runtime-c", "9.9.9", "online", "now()")
	rtA2 := fleetRuntimeFixture(t, testWorkspaceID, testUserID, daemonA, "zeta", "9.9.9", "online", "now()")
	agentA := fleetAgentFixture(t, testWorkspaceID, testUserID, rtA1, "Fleet Alpha", "goosar_helper", "working")
	issue := createTestIssue(t, "Fleet stuck task", "in_progress", "none")
	fleetTaskFixture(t, agentA, rtA1, issue, "running", int(StuckTaskGraceSeconds)+60)
	fleetTaskFixture(t, agentA, rtA1, issue, "dispatched", 0)

	rtB := fleetRuntimeFixture(t, otherWorkspaceID, otherOwnerID, daemonB, "runtime-c", "0.0.1", "offline", "now() - interval '2 hours'")
	agentB := fleetAgentFixture(t, otherWorkspaceID, otherOwnerID, rtB, "Fleet Beta", "", "idle")
	fleetTaskFixture(t, agentB, rtB, issue, "running", int(StuckTaskGraceSeconds)+60)

	w := httptest.NewRecorder()
	deploymentFleetTestRoutes().ServeHTTP(w,
		newRequestAsUser(adminID, http.MethodGet, "/api/deployment/fleet", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("fleet: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp DeploymentFleetResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode fleet: %v", err)
	}
	if resp.MinClientVersion == "" {
		t.Fatalf("min_client_version is empty; the version gate should be on by default")
	}
	if resp.Total != resp.Summary.MachinesTotal {
		t.Fatalf("total = %d, summary.machines_total = %d", resp.Total, resp.Summary.MachinesTotal)
	}

	byDaemon := map[string]DeploymentFleetMachine{}
	for _, m := range resp.Machines {
		byDaemon[m.DaemonID] = m
	}

	a, ok := byDaemon[daemonA]
	if !ok {
		t.Fatalf("machine %s missing from the fleet: %s", daemonA, w.Body.String())
	}
	if len(a.Runtimes) != 2 {
		t.Fatalf("machine A runtimes = %d, want 2 (both daemon-%s runtimes on ONE machine)", len(a.Runtimes), daemonA)
	}
	if !a.Online {
		t.Fatalf("machine A should be online")
	}
	if a.WorkspaceID != testWorkspaceID {
		t.Fatalf("machine A workspace = %q, want %q", a.WorkspaceID, testWorkspaceID)
	}
	if a.ClientVersion != "9.9.9" || a.VersionOutdated {
		t.Fatalf("machine A version = %q outdated=%v, want 9.9.9 / false", a.ClientVersion, a.VersionOutdated)
	}
	if a.LastHeartbeatAt == nil {
		t.Fatalf("machine A must carry a last_heartbeat_at")
	}
	if a.RunningTasks != 2 {
		t.Fatalf("machine A running_tasks = %d, want 2", a.RunningTasks)
	}
	if a.StuckTasks != 0 {
		t.Fatalf("machine A stuck_tasks = %d, want 0: its daemon is still heartbeating, so the sweeper would not fail that task either", a.StuckTasks)
	}

	var found bool
	for _, rt := range a.Runtimes {
		if rt.ID != rtA1 {
			continue
		}
		found = true
		if len(rt.Agents) != 1 || rt.Agents[0].Name != "Fleet Alpha" ||
			rt.Agents[0].SystemKey != "goosar_helper" || rt.Agents[0].Status != "working" {
			t.Fatalf("runtime A1 agents = %+v", rt.Agents)
		}
		if rt.Visibility != "private" {
			t.Fatalf("runtime A1 visibility = %q, want the private default", rt.Visibility)
		}
	}
	if !found {
		t.Fatalf("runtime %s missing from machine A", rtA1)
	}
	for _, rt := range a.Runtimes {
		if rt.ID == rtA2 && len(rt.Agents) != 0 {
			t.Fatalf("runtime A2 should carry no agents, got %+v", rt.Agents)
		}
	}

	b, ok := byDaemon[daemonB]
	if !ok {
		t.Fatalf("machine %s missing from the fleet", daemonB)
	}
	if b.Online {
		t.Fatalf("machine B should be offline")
	}
	if b.WorkspaceID != otherWorkspaceID {
		t.Fatalf("machine B workspace = %q, want the second workspace %q", b.WorkspaceID, otherWorkspaceID)
	}
	if !b.VersionOutdated {
		t.Fatalf("machine B (CLI %q) must read as outdated below %q", b.ClientVersion, resp.MinClientVersion)
	}
	if len(b.Runtimes) != 1 || b.Runtimes[0].ID != rtB {
		t.Fatalf("machine B runtimes = %+v", b.Runtimes)
	}
	if b.RunningTasks != 1 || b.StuckTasks != 1 {
		t.Fatalf("machine B: running=%d stuck=%d, want 1/1 — a dead daemon's overdue task is stuck", b.RunningTasks, b.StuckTasks)
	}

	if resp.Summary.MachinesOnline+resp.Summary.MachinesOffline != resp.Summary.MachinesTotal {
		t.Fatalf("summary bands do not add up: %+v", resp.Summary)
	}
	if resp.Summary.OutdatedVersions < 1 || resp.Summary.StuckTasks < 1 {
		t.Fatalf("summary must count the outdated machine and the stuck task: %+v", resp.Summary)
	}

	if resp.Machines[0].StuckTasks == 0 && resp.Summary.StuckTasks > 0 {
		t.Fatalf("machine with stuck tasks must sort first, got %q", resp.Machines[0].DaemonID)
	}
}

func TestDeploymentFleet_IsL0Read(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	adminID, _ := deploymentUserFixture(t, "fleet-audit")
	grantDeploymentAdminFixture(t, adminID)

	before := adminAuditCount(t)
	w := httptest.NewRecorder()
	deploymentFleetTestRoutes().ServeHTTP(w,
		newRequestAsUser(adminID, http.MethodGet, "/api/deployment/fleet", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("fleet: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if after := adminAuditCount(t); after != before {
		t.Fatalf("fleet read wrote %d admin_audit rows; §3 L0 reads write none", after-before)
	}
}

func TestFleetVersionVerdict(t *testing.T) {
	cases := []struct {
		version, minimum string
		want             bool
	}{
		{"0.0.1", "0.2.21", true},
		{"9.9.9", "0.2.21", false},
		{"", "0.2.21", false},
		{"not-a-version", "0.2.21", false},
		{"0.0.1", "", false},
	}
	for _, tc := range cases {
		if got := isDaemonVersionOutdated(tc.version, tc.minimum); got != tc.want {
			t.Fatalf("isDaemonVersionOutdated(%q, %q) = %v, want %v", tc.version, tc.minimum, got, tc.want)
		}
	}
}

func TestBuildDeploymentFleet_AlarmingRowsSurviveTheCap(t *testing.T) {
	rows := make([]db.ListDeploymentFleetRuntimesRow, 0, fleetMachineCap+1)
	for i := 0; i < fleetMachineCap; i++ {
		rows = append(rows, fleetRow(fmt.Sprintf("healthy-%03d", i), "online", "9.9.9", 0))
	}

	rows = append(rows, fleetRow("zzz-outdated", "online", "0.0.1", 0))

	resp := buildDeploymentFleet(rows, nil, "0.2.21")
	if resp.Total != fleetMachineCap+1 || !resp.Truncated {
		t.Fatalf("total = %d truncated = %v, want %d/true", resp.Total, resp.Truncated, fleetMachineCap+1)
	}
	if resp.Summary.OutdatedVersions != 1 {
		t.Fatalf("summary.outdated_versions = %d, want 1", resp.Summary.OutdatedVersions)
	}
	if len(resp.Machines) != fleetMachineCap {
		t.Fatalf("machines = %d, want the cap %d", len(resp.Machines), fleetMachineCap)
	}
	if !resp.Machines[0].VersionOutdated || resp.Machines[0].DaemonID != "zzz-outdated" {
		t.Fatalf("the outdated machine must survive the cap, got %q", resp.Machines[0].DaemonID)
	}
}

func fleetRow(daemonID, status, cliVersion string, stuck int64) db.ListDeploymentFleetRuntimesRow {
	return db.ListDeploymentFleetRuntimesRow{
		ID:          parseUUID(uuid.NewString()),
		WorkspaceID: parseUUID(uuid.NewString()),
		DaemonID:    pgtype.Text{String: daemonID, Valid: true},
		RuntimeName: "runtime-c",
		Status:      status,
		Metadata:    []byte(`{"cli_version":"` + cliVersion + `"}`),
		StuckTasks:  stuck,
	}
}
