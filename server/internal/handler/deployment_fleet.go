// Обзор парка машин деплоя: GET /api/deployment/fleet собирает то, что уже
// копится в потоке heartbeat'ов, очереди задач и реестре агентов — какие
// демоны живы, когда последний раз выходили на связь и где зависли задачи.
// Ничего нового не пишется и не хранится; это не замена метрикам, а
// человеческая сводка на один экран.
package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"time"

	agentpkg "github.com/adanman/goosar/server/pkg/agent"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	StuckTaskGraceSeconds = 9000.0

	RuntimeStaleGraceSeconds = 150.0

	fleetMachineCap = 200
)

type DeploymentFleetAgent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SystemKey string `json:"system_key"`
	Status    string `json:"status"`
}

type DeploymentFleetRuntime struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Provider     string                 `json:"provider"`
	Visibility   string                 `json:"visibility"`
	Status       string                 `json:"status"`
	Online       bool                   `json:"online"`
	LastSeenAt   *string                `json:"last_seen_at"`
	RunningTasks int64                  `json:"running_tasks"`
	StuckTasks   int64                  `json:"stuck_tasks"`
	Agents       []DeploymentFleetAgent `json:"agents"`
}

type DeploymentFleetMachine struct {
	DaemonID      string `json:"daemon_id"`
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	WorkspaceSlug string `json:"workspace_slug"`
	OwnerEmail    string `json:"owner_email"`
	OwnerName     string `json:"owner_name"`
	DeviceInfo    string `json:"device_info"`

	Online bool `json:"online"`

	LastHeartbeatAt *string `json:"last_heartbeat_at"`

	ClientVersion string `json:"client_version"`

	VersionOutdated bool                     `json:"version_outdated"`
	RunningTasks    int64                    `json:"running_tasks"`
	StuckTasks      int64                    `json:"stuck_tasks"`
	Runtimes        []DeploymentFleetRuntime `json:"runtimes"`
}

type DeploymentFleetSummary struct {
	MachinesTotal    int   `json:"machines_total"`
	MachinesOnline   int   `json:"machines_online"`
	MachinesOffline  int   `json:"machines_offline"`
	OutdatedVersions int   `json:"outdated_versions"`
	RunningTasks     int64 `json:"running_tasks"`
	StuckTasks       int64 `json:"stuck_tasks"`
}

type DeploymentFleetResponse struct {
	Machines []DeploymentFleetMachine `json:"machines"`
	Summary  DeploymentFleetSummary   `json:"summary"`

	Total int `json:"total"`

	Truncated bool `json:"truncated"`

	MinClientVersion string `json:"min_client_version"`
}

func (h *Handler) ListDeploymentFleet(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDeploymentAdmin(w, r); !ok {
		return
	}
	runtimes, err := h.Queries.ListDeploymentFleetRuntimes(r.Context(), db.ListDeploymentFleetRuntimesParams{
		StuckSeconds:        StuckTaskGraceSeconds,
		RuntimeStaleSeconds: RuntimeStaleGraceSeconds,
	})
	if err != nil {
		slog.Error("deployment fleet: list runtimes failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list fleet runtimes")
		return
	}
	agents, err := h.Queries.ListDeploymentFleetAgents(r.Context())
	if err != nil {
		slog.Error("deployment fleet: list agents failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list fleet agents")
		return
	}
	writeJSON(w, http.StatusOK, buildDeploymentFleet(runtimes, agents, MinDaemonVersion()))
}

func buildDeploymentFleet(
	rows []db.ListDeploymentFleetRuntimesRow,
	agentRows []db.ListDeploymentFleetAgentsRow,
	minVersion string,
) DeploymentFleetResponse {
	agentsByRuntime := make(map[string][]DeploymentFleetAgent, len(agentRows))
	for _, a := range agentRows {
		id := uuidToString(a.RuntimeID)
		agentsByRuntime[id] = append(agentsByRuntime[id], DeploymentFleetAgent{
			ID:        uuidToString(a.ID),
			Name:      a.Name,
			SystemKey: a.SystemKey,
			Status:    a.Status,
		})
	}

	machines := make([]DeploymentFleetMachine, 0, len(rows))
	index := make(map[string]int, len(rows))

	freshest := make(map[int]time.Time, len(rows))
	for _, row := range rows {
		online := row.Status == "online"
		lastSeen := timestampToPtr(row.LastSeenAt)
		runtime := DeploymentFleetRuntime{
			ID:           uuidToString(row.ID),
			Name:         row.RuntimeName,
			Provider:     row.Provider,
			Visibility:   row.Visibility,
			Status:       row.Status,
			Online:       online,
			LastSeenAt:   lastSeen,
			RunningTasks: row.RunningTasks,
			StuckTasks:   row.StuckTasks,
			Agents:       agentsByRuntime[uuidToString(row.ID)],
		}
		if runtime.Agents == nil {
			runtime.Agents = []DeploymentFleetAgent{}
		}

		key := uuidToString(row.WorkspaceID) + "|" + row.DaemonID.String
		if !row.DaemonID.Valid || row.DaemonID.String == "" {
			key = "runtime|" + runtime.ID
		}
		i, seen := index[key]
		if !seen {
			machines = append(machines, DeploymentFleetMachine{
				DaemonID:      row.DaemonID.String,
				WorkspaceID:   uuidToString(row.WorkspaceID),
				WorkspaceName: row.WorkspaceName,
				WorkspaceSlug: row.WorkspaceSlug,
				OwnerEmail:    row.OwnerEmail,
				OwnerName:     row.OwnerName,
				DeviceInfo:    row.DeviceInfo,
				ClientVersion: readRuntimeCLIVersion(row.Metadata),
				Runtimes:      []DeploymentFleetRuntime{},
			})
			i = len(machines) - 1
			index[key] = i
		}
		m := &machines[i]
		m.Runtimes = append(m.Runtimes, runtime)
		m.RunningTasks += runtime.RunningTasks
		m.StuckTasks += runtime.StuckTasks
		if online {
			m.Online = true
		}
		if row.LastSeenAt.Valid && (m.LastHeartbeatAt == nil || row.LastSeenAt.Time.After(freshest[i])) {
			m.LastHeartbeatAt = lastSeen
			freshest[i] = row.LastSeenAt.Time
		}
		if m.ClientVersion == "" {
			m.ClientVersion = readRuntimeCLIVersion(row.Metadata)
		}
	}

	summary := DeploymentFleetSummary{MachinesTotal: len(machines)}
	for i := range machines {
		machines[i].VersionOutdated = isDaemonVersionOutdated(machines[i].ClientVersion, minVersion)
		if machines[i].Online {
			summary.MachinesOnline++
		} else {
			summary.MachinesOffline++
		}
		if machines[i].VersionOutdated {
			summary.OutdatedVersions++
		}
		summary.RunningTasks += machines[i].RunningTasks
		summary.StuckTasks += machines[i].StuckTasks
	}

	sort.SliceStable(machines, func(a, b int) bool {
		ma, mb := machines[a], machines[b]
		if (ma.StuckTasks > 0) != (mb.StuckTasks > 0) {
			return ma.StuckTasks > 0
		}
		if ma.Online != mb.Online {
			return !ma.Online
		}
		if ma.VersionOutdated != mb.VersionOutdated {
			return ma.VersionOutdated
		}
		return false
	})

	resp := DeploymentFleetResponse{
		Machines:         machines,
		Summary:          summary,
		Total:            len(machines),
		MinClientVersion: minVersion,
	}
	if len(resp.Machines) > fleetMachineCap {
		resp.Machines = resp.Machines[:fleetMachineCap]
		resp.Truncated = true
	}
	return resp
}

func isDaemonVersionOutdated(version, minVersion string) bool {
	if minVersion == "" || version == "" {
		return false
	}
	return errors.Is(agentpkg.CheckMinCLIVersionFor(version, minVersion), agentpkg.ErrCLIVersionTooOld)
}
