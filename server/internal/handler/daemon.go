package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/daemonws"
	"github.com/adanman/goosar/server/internal/integrations/channel"
	"github.com/adanman/goosar/server/internal/integrations/slack"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/middleware"
	"github.com/adanman/goosar/server/internal/runtimeapps"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
	"github.com/adanman/goosar/server/pkg/redact"
)

func (h *Handler) requireDaemonWorkspaceAccess(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	if workspaceID == "" {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}

	if daemonWsID := middleware.DaemonWorkspaceIDFromContext(r.Context()); daemonWsID != "" {
		if daemonWsID != workspaceID {
			writeError(w, http.StatusNotFound, "not found")
			return false
		}
		return true
	}

	userID := requestUserID(r)
	if userID != "" {
		if h.MembershipCache.Get(r.Context(), userID, workspaceID) {
			return true
		}
	}

	_, ok := h.requireWorkspaceMember(w, r, workspaceID, "not found")
	if ok && userID != "" {
		h.MembershipCache.Set(r.Context(), userID, workspaceID)
	}
	return ok
}

func (h *Handler) requireDaemonRuntimeAccess(w http.ResponseWriter, r *http.Request, runtimeID string) (db.AgentRuntime, bool) {
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return db.AgentRuntime{}, false
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "runtime not found")
			return db.AgentRuntime{}, false
		}
		slog.Warn("get agent runtime failed", "runtime_id", runtimeID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load runtime")
		return db.AgentRuntime{}, false
	}
	if !h.requireDaemonWorkspaceAccess(w, r, uuidToString(rt.WorkspaceID)) {
		return db.AgentRuntime{}, false
	}
	return rt, true
}

func (h *Handler) requireDaemonTaskAccess(w http.ResponseWriter, r *http.Request, taskID string) (db.AgentTaskQueue, bool) {
	task, _, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	return task, ok
}

func (h *Handler) requireDaemonTaskAccessWithWorkspace(w http.ResponseWriter, r *http.Request, taskID string) (db.AgentTaskQueue, string, bool) {
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task_id")
	if !ok {
		return db.AgentTaskQueue{}, "", false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {

		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "task not found")
			return db.AgentTaskQueue{}, "", false
		}
		slog.Warn("get agent task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return db.AgentTaskQueue{}, "", false
	}

	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" {
		writeError(w, http.StatusNotFound, "task not found")
		return db.AgentTaskQueue{}, "", false
	}

	if !h.requireDaemonWorkspaceAccess(w, r, wsID) {
		return db.AgentTaskQueue{}, "", false
	}
	return task, wsID, true
}

func (h *Handler) verifyDaemonWorkspaceAccess(r *http.Request, workspaceID string) bool {
	if workspaceID == "" {
		return false
	}
	if daemonWsID := middleware.DaemonWorkspaceIDFromContext(r.Context()); daemonWsID != "" {
		return daemonWsID == workspaceID
	}
	userID := requestUserID(r)
	if userID == "" {
		return false
	}
	if h.MembershipCache.Get(r.Context(), userID, workspaceID) {
		return true
	}
	_, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		return false
	}
	h.MembershipCache.Set(r.Context(), userID, workspaceID)
	return true
}

type DaemonRegisterRequest struct {
	WorkspaceID string `json:"workspace_id"`
	DaemonID    string `json:"daemon_id"`

	LegacyDaemonIDs []string `json:"legacy_daemon_ids"`
	DeviceName      string   `json:"device_name"`
	CLIVersion      string   `json:"cli_version"`
	LaunchedBy      string   `json:"launched_by"`
	Runtimes        []struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Version string `json:"version"`
		Status  string `json:"status"`

		ProfileID string `json:"profile_id"`
	} `json:"runtimes"`
	FailedProfiles []struct {
		ProfileID   string `json:"profile_id"`
		CommandName string `json:"command_name"`
		Reason      string `json:"reason"`
	} `json:"failed_profiles"`
}

type daemonWorkspaceReposResponse struct {
	WorkspaceID  string          `json:"workspace_id"`
	Repos        []RepoData      `json:"repos"`
	ReposVersion string          `json:"repos_version"`
	Settings     json.RawMessage `json:"settings,omitempty"`
}

func normalizeWorkspaceRepos(repos []RepoData) []RepoData {
	if len(repos) == 0 {
		return []RepoData{}
	}

	normalized := make([]RepoData, 0, len(repos))
	seen := make(map[string]struct{}, len(repos))
	for _, repo := range repos {
		url := strings.TrimSpace(repo.URL)
		if url == "" {
			continue
		}
		if _, exists := seen[url]; exists {
			continue
		}
		seen[url] = struct{}{}
		normalized = append(normalized, RepoData{URL: url, Description: repo.Description})
	}
	return normalized
}

func workspaceReposVersion(repos []RepoData) string {
	urls := make([]string, 0, len(repos))
	for _, repo := range repos {
		if repo.URL == "" {
			continue
		}
		urls = append(urls, repo.URL)
	}
	sort.Strings(urls)
	sum := sha256.Sum256([]byte(strings.Join(urls, "\n")))
	return hex.EncodeToString(sum[:])
}

func parseWorkspaceRepos(raw []byte) []RepoData {
	if len(raw) == 0 {
		return []RepoData{}
	}

	var repos []RepoData
	if err := json.Unmarshal(raw, &repos); err != nil {
		return []RepoData{}
	}
	return normalizeWorkspaceRepos(repos)
}

func workspaceReposResponse(workspaceID string, raw []byte, settingsRaw []byte) daemonWorkspaceReposResponse {
	repos := parseWorkspaceRepos(raw)
	resp := daemonWorkspaceReposResponse{
		WorkspaceID:  workspaceID,
		Repos:        repos,
		ReposVersion: workspaceReposVersion(repos),
	}
	if len(settingsRaw) > 0 {
		resp.Settings = json.RawMessage(settingsRaw)
	}
	return resp
}

func normalizeProvider(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (h *Handler) inheritMachineCustomName(ctx context.Context, rt db.AgentRuntime, inserted bool) db.AgentRuntime {
	if !inserted || rt.CustomName.Valid || !rt.DaemonID.Valid {
		return rt
	}
	names, err := h.Queries.ListDaemonCustomNames(ctx, db.ListDaemonCustomNamesParams{
		WorkspaceID: rt.WorkspaceID,
		DaemonID:    rt.DaemonID,
		ExcludeID:   rt.ID,
	})
	if err != nil {
		return rt
	}
	shared, ok := sharedDaemonCustomName(names)
	if !ok {
		return rt
	}
	updated, err := h.Queries.UpdateAgentRuntimeCustomName(ctx, db.UpdateAgentRuntimeCustomNameParams{
		CustomName: pgtype.Text{String: shared, Valid: true},
		ID:         rt.ID,
	})
	if err != nil {
		return rt
	}
	return updated
}

func sharedDaemonCustomName(names []pgtype.Text) (string, bool) {
	if len(names) == 0 {
		return "", false
	}
	var first string
	for i, n := range names {
		if !n.Valid {
			return "", false
		}
		v := strings.TrimSpace(n.String)
		if v == "" {
			return "", false
		}
		if i == 0 {
			first = v
		} else if v != first {
			return "", false
		}
	}
	return first, true
}

func (h *Handler) DaemonRegister(w http.ResponseWriter, r *http.Request) {
	var req DaemonRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.DaemonID = strings.TrimSpace(req.DaemonID)
	req.DeviceName = strings.TrimSpace(req.DeviceName)

	if req.DaemonID == "" {
		writeError(w, http.StatusBadRequest, "daemon_id is required")
		return
	}
	if req.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if len(req.Runtimes) == 0 && len(req.FailedProfiles) == 0 {
		writeError(w, http.StatusBadRequest, "at least one runtime or failed profile is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	req.WorkspaceID = uuidToString(wsUUID)

	var ownerID pgtype.UUID
	if daemonWsID := middleware.DaemonWorkspaceIDFromContext(r.Context()); daemonWsID != "" {
		if daemonWsID != req.WorkspaceID {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}

	} else {
		member, ok := h.requireWorkspaceMember(w, r, req.WorkspaceID, "workspace not found")
		if !ok {
			return
		}
		ownerID = member.UserID
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	resp := make([]AgentRuntimeResponse, 0, len(req.Runtimes))
	for _, runtime := range req.Runtimes {
		provider := normalizeProvider(runtime.Type)
		if provider == "" {
			provider = "unknown"
		}
		name := strings.TrimSpace(runtime.Name)
		if name == "" {
			name = provider
			if req.DeviceName != "" {
				name = fmt.Sprintf("%s (%s)", provider, req.DeviceName)
			}
		}
		deviceInfo := strings.TrimSpace(req.DeviceName)
		if runtime.Version != "" && deviceInfo != "" {
			deviceInfo = fmt.Sprintf("%s · %s", deviceInfo, runtime.Version)
		} else if runtime.Version != "" {
			deviceInfo = runtime.Version
		}
		status := "online"
		if runtime.Status == "offline" {
			status = "offline"
		}
		metadata, _ := json.Marshal(map[string]any{
			"version":     runtime.Version,
			"cli_version": req.CLIVersion,
			"launched_by": req.LaunchedBy,
		})

		var registered db.AgentRuntime
		var inserted bool
		isCustom := strings.TrimSpace(runtime.ProfileID) != ""

		if isCustom {
			profileUUID, pok := parseUUIDOrBadRequest(w, strings.TrimSpace(runtime.ProfileID), "profile_id")
			if !pok {
				return
			}

			profile, perr := h.Queries.GetRuntimeProfileForWorkspace(r.Context(), db.GetRuntimeProfileForWorkspaceParams{
				ID:          profileUUID,
				WorkspaceID: wsUUID,
			})
			if perr != nil {
				writeError(w, http.StatusBadRequest, "unknown runtime profile: "+runtime.ProfileID)
				return
			}
			if !profile.Enabled {
				writeError(w, http.StatusConflict, "runtime profile is disabled: "+runtime.ProfileID)
				return
			}
			provider = profile.ProtocolFamily

			prow, err := h.Queries.UpsertAgentRuntimeWithProfile(r.Context(), db.UpsertAgentRuntimeWithProfileParams{
				WorkspaceID: wsUUID,
				DaemonID:    strToText(req.DaemonID),
				Name:        name,
				RuntimeMode: "local",
				Provider:    provider,
				Status:      status,
				DeviceInfo:  deviceInfo,
				Metadata:    metadata,
				OwnerID:     ownerID,
				ProfileID:   profileUUID,
			})
			if err != nil {
				obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.RuntimeFailed(
					uuidToString(ownerID),
					req.WorkspaceID,
					req.DaemonID,
					provider,
					"registration_failed",
					"db_error",
					true,
				))
				writeError(w, http.StatusInternalServerError, "failed to register runtime: "+err.Error())
				return
			}
			inserted = prow.Inserted
			registered = db.AgentRuntime{
				ID:             prow.ID,
				WorkspaceID:    prow.WorkspaceID,
				DaemonID:       prow.DaemonID,
				Name:           prow.Name,
				CustomName:     prow.CustomName,
				RuntimeMode:    prow.RuntimeMode,
				Provider:       prow.Provider,
				Status:         prow.Status,
				DeviceInfo:     prow.DeviceInfo,
				Metadata:       prow.Metadata,
				LastSeenAt:     prow.LastSeenAt,
				CreatedAt:      prow.CreatedAt,
				UpdatedAt:      prow.UpdatedAt,
				OwnerID:        prow.OwnerID,
				LegacyDaemonID: prow.LegacyDaemonID,
				Visibility:     prow.Visibility,
				ProfileID:      prow.ProfileID,
			}
		} else {
			row, err := h.Queries.UpsertAgentRuntime(r.Context(), db.UpsertAgentRuntimeParams{
				WorkspaceID: wsUUID,
				DaemonID:    strToText(req.DaemonID),
				Name:        name,
				RuntimeMode: "local",
				Provider:    provider,
				Status:      status,
				DeviceInfo:  deviceInfo,
				Metadata:    metadata,
				OwnerID:     ownerID,
			})
			if err != nil {
				obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.RuntimeFailed(
					uuidToString(ownerID),
					req.WorkspaceID,
					req.DaemonID,
					provider,
					"registration_failed",
					"db_error",
					true,
				))
				writeError(w, http.StatusInternalServerError, "failed to register runtime: "+err.Error())
				return
			}
			inserted = row.Inserted
			registered = db.AgentRuntime{
				ID:             row.ID,
				WorkspaceID:    row.WorkspaceID,
				DaemonID:       row.DaemonID,
				Name:           row.Name,
				CustomName:     row.CustomName,
				RuntimeMode:    row.RuntimeMode,
				Provider:       row.Provider,
				Status:         row.Status,
				DeviceInfo:     row.DeviceInfo,
				Metadata:       row.Metadata,
				LastSeenAt:     row.LastSeenAt,
				CreatedAt:      row.CreatedAt,
				UpdatedAt:      row.UpdatedAt,
				OwnerID:        row.OwnerID,
				LegacyDaemonID: row.LegacyDaemonID,
				Visibility:     row.Visibility,
				ProfileID:      row.ProfileID,
			}
		}

		registered = h.inheritMachineCustomName(r.Context(), registered, inserted)

		if inserted {
			obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.RuntimeRegistered(
				uuidToString(ownerID),
				req.WorkspaceID,
				uuidToString(registered.ID),
				req.DaemonID,
				provider,
				runtime.Version,
				req.CLIVersion,
			))
			if registered.Status == "online" {
				obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.RuntimeReady(
					uuidToString(ownerID),
					req.WorkspaceID,
					uuidToString(registered.ID),
					req.DaemonID,
					provider,
					0,
				))
			}
		}

		if !isCustom {
			h.mergeLegacyRuntimes(r, registered, provider, req.LegacyDaemonIDs)

			if ownerID.Valid && req.DeviceName != "" {
				h.reclaimStaleOwnerRuntimes(r, registered, provider, ownerID, req.DeviceName)
			}
		}

		resp = append(resp, runtimeToResponse(registered))
	}
	for _, failed := range req.FailedProfiles {
		profileID := strings.TrimSpace(failed.ProfileID)
		if profileID == "" {
			continue
		}
		profileUUID, pok := parseUUIDOrBadRequest(w, profileID, "profile_id")
		if !pok {
			return
		}
		profile, perr := h.Queries.GetRuntimeProfileForWorkspace(r.Context(), db.GetRuntimeProfileForWorkspaceParams{
			ID:          profileUUID,
			WorkspaceID: wsUUID,
		})
		if perr != nil || !profile.Enabled {
			continue
		}
		name := profile.DisplayName
		if req.DeviceName != "" {
			name = fmt.Sprintf("%s (%s)", name, req.DeviceName)
		}
		deviceInfo := strings.TrimSpace(req.DeviceName)
		reason := strings.TrimSpace(failed.Reason)
		if reason == "" {
			reason = "custom runtime command could not be resolved"
		}
		commandName := strings.TrimSpace(failed.CommandName)
		if commandName == "" {
			commandName = profile.CommandName
		}
		metadata, _ := json.Marshal(map[string]any{
			"version":                            "",
			"cli_version":                        req.CLIVersion,
			"launched_by":                        req.LaunchedBy,
			"runtime_profile_registration_error": true,
			"runtime_profile_failure_reason":     reason,
			"command_name":                       commandName,
		})

		prow, err := h.Queries.UpsertFailedAgentRuntimeProfile(r.Context(), db.UpsertFailedAgentRuntimeProfileParams{
			WorkspaceID: wsUUID,
			DaemonID:    strToText(req.DaemonID),
			Name:        name,
			RuntimeMode: "local",
			Provider:    profile.ProtocolFamily,
			Status:      "offline",
			DeviceInfo:  deviceInfo,
			Metadata:    metadata,
			OwnerID:     ownerID,
			ProfileID:   profileUUID,
		})
		if err != nil {
			slog.Warn("failed to record runtime profile registration failure",
				"workspace_id", req.WorkspaceID, "daemon_id", req.DaemonID,
				"profile_id", profileID, "error", err)
			continue
		}

		h.inheritMachineCustomName(r.Context(), db.AgentRuntime{
			ID:          prow.ID,
			WorkspaceID: prow.WorkspaceID,
			DaemonID:    prow.DaemonID,
			CustomName:  prow.CustomName,
		}, prow.Inserted)
	}

	slog.Info("daemon registered", "workspace_id", req.WorkspaceID, "daemon_id", req.DaemonID, "runtimes_count", len(resp))

	if len(resp) > 0 && ownerID.Valid {
		h.provisionMemberHelper(r.Context(), wsUUID, ownerID, r.Header.Get("Accept-Language"), helperAnnounceFull)
	}

	if len(resp) > 0 {
		h.provisionRoleAgent(r.Context(), wsUUID)
	}

	h.publish(protocol.EventDaemonRegister, req.WorkspaceID, "system", "", map[string]any{
		"runtimes": resp,
	})

	repoResp := workspaceReposResponse(req.WorkspaceID, ws.Repos, ws.Settings)

	writeJSON(w, http.StatusOK, map[string]any{
		"runtimes":      resp,
		"repos":         repoResp.Repos,
		"repos_version": repoResp.ReposVersion,
		"settings":      repoResp.Settings,
	})
}

func (h *Handler) mergeLegacyRuntimes(r *http.Request, registered db.AgentRuntime, provider string, legacyIDs []string) {
	newID := uuidToString(registered.ID)
	merged := make(map[string]struct{})

	for _, legacyID := range legacyIDs {
		legacyID = strings.TrimSpace(legacyID)
		if legacyID == "" {
			continue
		}

		matches, err := h.Queries.FindLegacyRuntimesByDaemonID(r.Context(), db.FindLegacyRuntimesByDaemonIDParams{
			WorkspaceID: registered.WorkspaceID,
			Provider:    provider,
			DaemonID:    legacyID,
		})
		if err != nil {
			slog.Warn("legacy runtime merge: lookup failed", "legacy_daemon_id", legacyID, "error", err)
			continue
		}
		for _, old := range matches {
			oldID := uuidToString(old.ID)
			if oldID == newID {
				continue
			}
			if _, seen := merged[oldID]; seen {
				continue
			}
			merged[oldID] = struct{}{}

			agents, err := h.Queries.ReassignAgentsToRuntime(r.Context(), db.ReassignAgentsToRuntimeParams{
				NewRuntimeID: registered.ID,
				OldRuntimeID: old.ID,
			})
			if err != nil {
				slog.Warn("legacy runtime merge: reassign agents failed", "legacy_daemon_id", legacyID, "old_runtime_id", oldID, "new_runtime_id", newID, "error", err)
				continue
			}
			tasks, err := h.Queries.ReassignTasksToRuntime(r.Context(), db.ReassignTasksToRuntimeParams{
				NewRuntimeID: registered.ID,
				OldRuntimeID: old.ID,
			})
			if err != nil {
				slog.Warn("legacy runtime merge: reassign tasks failed", "legacy_daemon_id", legacyID, "old_runtime_id", oldID, "new_runtime_id", newID, "error", err)
				continue
			}
			if err := h.Queries.RecordRuntimeLegacyDaemonID(r.Context(), db.RecordRuntimeLegacyDaemonIDParams{
				ID:             registered.ID,
				LegacyDaemonID: strToText(legacyID),
			}); err != nil {
				slog.Warn("legacy runtime merge: record legacy daemon_id failed", "legacy_daemon_id", legacyID, "error", err)
			}
			if err := h.Queries.DeleteAgentRuntime(r.Context(), old.ID); err != nil {
				slog.Warn("legacy runtime merge: delete old runtime failed", "old_runtime_id", oldID, "error", err)
				continue
			}

			slog.Info("legacy runtime merged",
				"legacy_daemon_id", legacyID,
				"old_runtime_id", oldID,
				"new_runtime_id", newID,
				"provider", provider,
				"agents_reassigned", agents,
				"tasks_reassigned", tasks,
			)
		}
	}
}

func (h *Handler) reclaimStaleOwnerRuntimes(r *http.Request, registered db.AgentRuntime, provider string, ownerID pgtype.UUID, hostname string) {
	newID := uuidToString(registered.ID)

	stale, err := h.Queries.FindStaleOwnerRuntimesByHostname(r.Context(), db.FindStaleOwnerRuntimesByHostnameParams{
		WorkspaceID: registered.WorkspaceID,
		Provider:    provider,
		OwnerID:     ownerID,
		DaemonID:    registered.DaemonID.String,
		Hostname:    hostname,
	})
	if err != nil {
		slog.Warn("runtime reclaim: lookup failed", "hostname", hostname, "error", err)
		return
	}
	for _, old := range stale {
		oldID := uuidToString(old.ID)
		if oldID == newID {
			continue
		}

		agents, err := h.Queries.ReassignAgentsToRuntime(r.Context(), db.ReassignAgentsToRuntimeParams{
			NewRuntimeID: registered.ID,
			OldRuntimeID: old.ID,
		})
		if err != nil {
			slog.Warn("runtime reclaim: reassign agents failed", "old_runtime_id", oldID, "new_runtime_id", newID, "error", err)
			continue
		}
		tasks, err := h.Queries.ReassignTasksToRuntime(r.Context(), db.ReassignTasksToRuntimeParams{
			NewRuntimeID: registered.ID,
			OldRuntimeID: old.ID,
		})
		if err != nil {
			slog.Warn("runtime reclaim: reassign tasks failed", "old_runtime_id", oldID, "new_runtime_id", newID, "error", err)
			continue
		}
		if err := h.Queries.DeleteAgentRuntime(r.Context(), old.ID); err != nil {
			slog.Warn("runtime reclaim: delete old runtime failed", "old_runtime_id", oldID, "error", err)
			continue
		}

		slog.Info("stale owner runtime reclaimed",
			"hostname", hostname,
			"old_runtime_id", oldID,
			"new_runtime_id", newID,
			"provider", provider,
			"agents_reassigned", agents,
			"tasks_reassigned", tasks,
		)
	}
}

func (h *Handler) GetDaemonWorkspaceRepos(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
	if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	writeJSON(w, http.StatusOK, workspaceReposResponse(workspaceID, ws.Repos, ws.Settings))
}

func (h *Handler) DaemonDeregister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RuntimeIDs []string `json:"runtime_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.RuntimeIDs) == 0 {
		writeError(w, http.StatusBadRequest, "runtime_ids is required")
		return
	}
	runtimeUUIDs, ok := parseUUIDSliceOrBadRequest(w, req.RuntimeIDs, "runtime_ids")
	if !ok {
		return
	}

	affectedWorkspaces := make(map[string]bool)

	for i, rid := range req.RuntimeIDs {

		rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUIDs[i])
		if err != nil {
			slog.Warn("deregister: runtime not found", "runtime_id", rid, "error", err)
			continue
		}

		wsID := uuidToString(rt.WorkspaceID)
		if !h.verifyDaemonWorkspaceAccess(r, wsID) {
			slog.Warn("deregister: workspace mismatch", "runtime_id", rid)
			continue
		}

		if err := h.Queries.SetAgentRuntimeOffline(r.Context(), rt.ID); err != nil {
			slog.Warn("deregister: failed to set offline", "runtime_id", rid, "error", err)
			continue
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.RuntimeOffline(
			uuidToString(rt.OwnerID),
			wsID,
			uuidToString(rt.ID),
			rt.DaemonID.String,
			rt.Provider,
		))

		affectedWorkspaces[wsID] = true
	}

	for wsID := range affectedWorkspaces {
		h.publish(protocol.EventDaemonRegister, wsID, "system", "", map[string]any{
			"action": "deregister",
		})
	}

	slog.Info("daemon deregistered", "runtime_ids", req.RuntimeIDs)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type DaemonHeartbeatRequest struct {
	RuntimeID           string `json:"runtime_id"`
	SupportsBatchImport bool   `json:"supports_batch_import,omitempty"`
}

const heartbeatHasPendingTimeout = 1 * time.Second

const maxLocalSkillImportBatch = 10

const runtimeLivenessTTL = 90 * time.Second

const runtimeHeartbeatDBFlushInterval = 60 * time.Second

func (h *Handler) DaemonHeartbeat(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	authPath := middleware.DaemonAuthPathFromContext(r.Context())
	var (
		outcome                                                                                            = "unauth"
		runtimeID                                                                                          string
		decodeMs, runtimeLookupMs, workspaceCheckMs                                                        int64
		authMs, updateMs, probeModelMs, popModelMs, probeSkillsMs, popSkillsMs, probeImportMs, popImportMs int64
		probeModelTimedOut, probeSkillsTimedOut, probeImportTimedOut                                       bool
	)
	defer func() {
		logHeartbeatEndpointSlow(runtimeID, outcome, authPath, start, decodeMs, runtimeLookupMs, workspaceCheckMs, authMs, updateMs, probeModelMs, popModelMs, probeSkillsMs, popSkillsMs, probeImportMs, popImportMs, probeModelTimedOut, probeSkillsTimedOut, probeImportTimedOut)
	}()

	decodeStart := time.Now()
	var req DaemonHeartbeatRequest
	decodeErr := json.NewDecoder(r.Body).Decode(&req)
	decodeMs = time.Since(decodeStart).Milliseconds()
	if decodeErr != nil {
		outcome = "bad_body"
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RuntimeID == "" {
		outcome = "missing_runtime_id"
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	runtimeID = req.RuntimeID

	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		outcome = "bad_runtime_id"
		return
	}
	lookupStart := time.Now()
	rt, lookupErr := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	runtimeLookupMs = time.Since(lookupStart).Milliseconds()
	if lookupErr != nil {

		if isNotFound(lookupErr) {
			outcome = "runtime_not_found"
			writeError(w, http.StatusNotFound, "runtime not found")
			return
		}
		outcome = "runtime_lookup_error"
		slog.Warn("get agent runtime failed", "runtime_id", req.RuntimeID, "error", lookupErr)
		writeError(w, http.StatusInternalServerError, "failed to load runtime")
		return
	}
	wsCheckStart := time.Now()
	wsOK := h.requireDaemonWorkspaceAccess(w, r, uuidToString(rt.WorkspaceID))
	workspaceCheckMs = time.Since(wsCheckStart).Milliseconds()
	if !wsOK {
		outcome = "workspace_denied"
		return
	}
	authMs = time.Since(start).Milliseconds()

	ack, m, err := h.processHeartbeat(r.Context(), rt, req.SupportsBatchImport)
	updateMs = m.UpdateMs
	probeModelMs = m.ProbeModelMs
	popModelMs = m.PopModelMs
	probeSkillsMs = m.ProbeSkillsMs
	popSkillsMs = m.PopSkillsMs
	probeImportMs = m.ProbeImportMs
	popImportMs = m.PopImportMs
	probeModelTimedOut = m.ProbeModelTimedOut
	probeSkillsTimedOut = m.ProbeSkillsTimedOut
	probeImportTimedOut = m.ProbeImportTimedOut
	if err != nil {
		outcome = "error_update"
		writeError(w, http.StatusInternalServerError, "heartbeat failed")
		return
	}

	outcome = "ok"

	resp := map[string]any{"status": ack.Status}
	if ack.PendingUpdate != nil {
		resp["pending_update"] = ack.PendingUpdate
	}
	if ack.PendingModelList != nil {
		resp["pending_model_list"] = ack.PendingModelList
	}
	if ack.PendingLocalSkills != nil {
		resp["pending_local_skills"] = ack.PendingLocalSkills
	}
	if ack.PendingLocalSkillImport != nil {
		resp["pending_local_skill_import"] = ack.PendingLocalSkillImport
	}
	if len(ack.PendingLocalSkillImports) > 0 {
		resp["pending_local_skill_imports"] = ack.PendingLocalSkillImports
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleDaemonWSHeartbeat(ctx context.Context, identity daemonws.ClientIdentity, runtimeID string, supportsBatchImport bool) (*protocol.DaemonHeartbeatAckPayload, error) {
	runtimeUUID, err := util.ParseUUID(runtimeID)
	if err != nil {
		return nil, fmt.Errorf("invalid runtime_id: %w", err)
	}
	rt, err := h.Queries.GetAgentRuntime(ctx, runtimeUUID)
	if err != nil {
		if isNotFound(err) {
			return &protocol.DaemonHeartbeatAckPayload{
				RuntimeID:   runtimeID,
				Status:      protocol.HeartbeatStatusRuntimeGone,
				RuntimeGone: true,
			}, nil
		}
		return nil, fmt.Errorf("get agent runtime: %w", err)
	}
	if !identity.AllowsWorkspace(uuidToString(rt.WorkspaceID)) {
		return nil, fmt.Errorf("runtime not in connection workspace")
	}

	if identity.UserID != "" {
		wsID := uuidToString(rt.WorkspaceID)
		if !h.MembershipCache.Get(ctx, identity.UserID, wsID) {
			_, memberErr := h.getWorkspaceMember(ctx, identity.UserID, wsID)
			switch {
			case memberErr == nil:
				h.MembershipCache.Set(ctx, identity.UserID, wsID)
			case errors.Is(memberErr, pgx.ErrNoRows):
				return nil, fmt.Errorf("workspace membership revoked")
			default:

				slog.Warn("ws heartbeat: membership recheck failed; acking anyway",
					"runtime_id", runtimeID, "error", memberErr)
			}
		}
	}
	ack, _, err := h.processHeartbeat(ctx, rt, supportsBatchImport)
	return ack, err
}

func (h *Handler) recordHeartbeat(ctx context.Context, rt db.AgentRuntime) error {
	now := time.Now()

	needDBWrite := !h.LivenessStore.Available() ||
		rt.Status != "online" ||
		!rt.LastSeenAt.Valid ||
		now.Sub(rt.LastSeenAt.Time) >= runtimeHeartbeatDBFlushInterval

	if h.LivenessStore.Available() {
		if err := h.LivenessStore.Touch(ctx, uuidToString(rt.ID), runtimeLivenessTTL); err != nil {

			slog.Warn("liveness touch failed; falling back to DB heartbeat",
				"runtime_id", uuidToString(rt.ID), "error", err)
			needDBWrite = true
		}
	}

	if !needDBWrite {
		return nil
	}

	return h.HeartbeatScheduler.Schedule(ctx, rt)
}

type heartbeatMetrics struct {
	UpdateMs, ProbeModelMs, PopModelMs, ProbeSkillsMs, PopSkillsMs, ProbeImportMs, PopImportMs int64
	ProbeModelTimedOut, ProbeSkillsTimedOut, ProbeImportTimedOut                               bool
}

func (h *Handler) processHeartbeat(ctx context.Context, rt db.AgentRuntime, supportsBatchImport bool) (*protocol.DaemonHeartbeatAckPayload, heartbeatMetrics, error) {
	var m heartbeatMetrics
	runtimeID := uuidToString(rt.ID)

	updateStart := time.Now()
	if err := h.recordHeartbeat(ctx, rt); err != nil {
		m.UpdateMs = time.Since(updateStart).Milliseconds()
		return nil, m, err
	}
	m.UpdateMs = time.Since(updateStart).Milliseconds()

	slog.Debug("daemon heartbeat", "runtime_id", runtimeID)

	ack := &protocol.DaemonHeartbeatAckPayload{
		RuntimeID:          runtimeID,
		Status:             "ok",
		ServerCapabilities: []string{protocol.DaemonCapabilityRPCV1},
	}

	probeUpdateCtx, cancelProbeUpdate := context.WithTimeout(ctx, heartbeatHasPendingTimeout)
	hasUpdate, probeUpdateErr := h.UpdateStore.HasPending(probeUpdateCtx, runtimeID)
	cancelProbeUpdate()
	switch {
	case probeUpdateErr == nil && hasUpdate:
		pending, popUpdateErr := h.UpdateStore.PopPending(ctx, runtimeID)
		if popUpdateErr != nil {
			slog.Warn("update PopPending failed", "error", popUpdateErr, "runtime_id", runtimeID)
		} else if pending != nil {
			ack.PendingUpdate = &protocol.DaemonHeartbeatPendingUpdate{
				ID:            pending.ID,
				TargetVersion: pending.TargetVersion,
			}
		}
	case probeUpdateErr != nil:
		if errors.Is(probeUpdateErr, context.DeadlineExceeded) || errors.Is(probeUpdateErr, context.Canceled) {
			slog.Warn("update HasPending timed out", "runtime_id", runtimeID)
		} else {
			slog.Warn("update HasPending failed", "error", probeUpdateErr, "runtime_id", runtimeID)
		}
	}

	probeModelStart := time.Now()
	probeModelCtx, cancelProbeModel := context.WithTimeout(ctx, heartbeatHasPendingTimeout)
	hasModel, probeModelErr := h.ModelListStore.HasPending(probeModelCtx, runtimeID)
	cancelProbeModel()
	m.ProbeModelMs = time.Since(probeModelStart).Milliseconds()
	switch {
	case probeModelErr == nil && hasModel:
		popStart := time.Now()
		pendingModel, popErr := h.ModelListStore.PopPending(ctx, runtimeID)
		m.PopModelMs = time.Since(popStart).Milliseconds()
		if popErr != nil {
			slog.Warn("model list PopPending failed", "error", popErr, "runtime_id", runtimeID)
		} else if pendingModel != nil {
			ack.PendingModelList = &protocol.DaemonHeartbeatPendingModelList{ID: pendingModel.ID}
		}
	case probeModelErr != nil:
		if errors.Is(probeModelErr, context.DeadlineExceeded) || errors.Is(probeModelErr, context.Canceled) {
			m.ProbeModelTimedOut = true
			slog.Warn("model list HasPending timed out", "runtime_id", runtimeID, "elapsed_ms", m.ProbeModelMs)
		} else {
			slog.Warn("model list HasPending failed", "error", probeModelErr, "runtime_id", runtimeID)
		}
	}

	probeSkillsStart := time.Now()
	probeSkillsCtx, cancelProbeSkills := context.WithTimeout(ctx, heartbeatHasPendingTimeout)
	hasSkills, probeErr := h.LocalSkillListStore.HasPending(probeSkillsCtx, runtimeID)
	cancelProbeSkills()
	m.ProbeSkillsMs = time.Since(probeSkillsStart).Milliseconds()
	switch {
	case probeErr == nil && hasSkills:
		popStart := time.Now()
		pendingSkills, popErr := h.LocalSkillListStore.PopPending(ctx, runtimeID)
		m.PopSkillsMs = time.Since(popStart).Milliseconds()
		if popErr != nil {
			slog.Warn("local skill list PopPending failed", "error", popErr, "runtime_id", runtimeID)
		} else if pendingSkills != nil {
			ack.PendingLocalSkills = &protocol.DaemonHeartbeatPendingLocalSkills{ID: pendingSkills.ID}
		}
	case probeErr != nil:
		if errors.Is(probeErr, context.DeadlineExceeded) || errors.Is(probeErr, context.Canceled) {
			m.ProbeSkillsTimedOut = true
			slog.Warn("local skill list HasPending timed out", "runtime_id", runtimeID, "elapsed_ms", m.ProbeSkillsMs)
		} else {
			slog.Warn("local skill list HasPending failed", "error", probeErr, "runtime_id", runtimeID)
		}
	}

	probeImportStart := time.Now()
	probeImportCtx, cancelProbeImport := context.WithTimeout(ctx, heartbeatHasPendingTimeout)
	hasImport, probeErr := h.LocalSkillImportStore.HasPending(probeImportCtx, runtimeID)
	cancelProbeImport()
	m.ProbeImportMs = time.Since(probeImportStart).Milliseconds()
	switch {
	case probeErr == nil && hasImport:
		popStart := time.Now()
		if supportsBatchImport {
			pendingImports, popErr := h.LocalSkillImportStore.PopPendingBatch(ctx, runtimeID, maxLocalSkillImportBatch)
			m.PopImportMs = time.Since(popStart).Milliseconds()
			if popErr != nil {
				slog.Warn("local skill import PopPendingBatch failed", "error", popErr, "runtime_id", runtimeID, "claimed", len(pendingImports))
			}

			if len(pendingImports) > 0 {

				ack.PendingLocalSkillImport = &protocol.DaemonHeartbeatPendingLocalSkillImport{
					ID:       pendingImports[0].ID,
					SkillKey: pendingImports[0].SkillKey,
				}
				batch := make([]protocol.DaemonHeartbeatPendingLocalSkillImport, 0, len(pendingImports))
				for _, p := range pendingImports {
					batch = append(batch, protocol.DaemonHeartbeatPendingLocalSkillImport{
						ID:       p.ID,
						SkillKey: p.SkillKey,
					})
				}
				ack.PendingLocalSkillImports = batch
			}
		} else {
			pendingImport, popErr := h.LocalSkillImportStore.PopPending(ctx, runtimeID)
			m.PopImportMs = time.Since(popStart).Milliseconds()
			if popErr != nil {
				slog.Warn("local skill import PopPending failed", "error", popErr, "runtime_id", runtimeID)
			} else if pendingImport != nil {
				ack.PendingLocalSkillImport = &protocol.DaemonHeartbeatPendingLocalSkillImport{
					ID:       pendingImport.ID,
					SkillKey: pendingImport.SkillKey,
				}
			}
		}
	case probeErr != nil:
		if errors.Is(probeErr, context.DeadlineExceeded) || errors.Is(probeErr, context.Canceled) {
			m.ProbeImportTimedOut = true
			slog.Warn("local skill import HasPending timed out", "runtime_id", runtimeID, "elapsed_ms", m.ProbeImportMs)
		} else {
			slog.Warn("local skill import HasPending failed", "error", probeErr, "runtime_id", runtimeID)
		}
	}

	return ack, m, nil
}

func logHeartbeatEndpointSlow(runtimeID, outcome, authPath string, start time.Time, decodeMs, runtimeLookupMs, workspaceCheckMs, authMs, updateMs, probeModelMs, popModelMs, probeSkillsMs, popSkillsMs, probeImportMs, popImportMs int64, probeModelTimedOut, probeSkillsTimedOut, probeImportTimedOut bool) {
	totalMs := time.Since(start).Milliseconds()
	if totalMs < 500 && !probeModelTimedOut && !probeSkillsTimedOut && !probeImportTimedOut {
		return
	}
	slog.Info("heartbeat_endpoint slow",
		"runtime_id", runtimeID,
		"outcome", outcome,
		"auth_path", authPath,
		"total_ms", totalMs,
		"auth_ms", authMs,
		"decode_ms", decodeMs,
		"runtime_lookup_ms", runtimeLookupMs,
		"workspace_check_ms", workspaceCheckMs,
		"update_ms", updateMs,
		"probe_model_ms", probeModelMs,
		"pop_model_ms", popModelMs,
		"probe_skills_ms", probeSkillsMs,
		"pop_skills_ms", popSkillsMs,
		"probe_import_ms", probeImportMs,
		"pop_import_ms", popImportMs,
		"probe_model_timed_out", probeModelTimedOut,
		"probe_skills_timed_out", probeSkillsTimedOut,
		"probe_import_timed_out", probeImportTimedOut,
	)
}

func logClaimEndpointSlow(runtimeID, outcome string, start time.Time, authMs, claimMs, buildMs int64, payloadBytes, agentSkillCount, builtinSkillCount, skillPayloadBytes int) {
	totalMs := time.Since(start).Milliseconds()
	if totalMs < 500 {
		return
	}
	slog.Info("claim_endpoint slow",
		"runtime_id", runtimeID,
		"outcome", outcome,
		"total_ms", totalMs,
		"auth_ms", authMs,
		"claim_ms", claimMs,
		"build_ms", buildMs,
		"payload_bytes", payloadBytes,
		"agent_skill_count", agentSkillCount,
		"builtin_skill_count", builtinSkillCount,
		"skill_payload_bytes", skillPayloadBytes,
	)
}

func requestHasClientCapability(r *http.Request, capability string) bool {
	for _, part := range strings.Split(r.Header.Get("X-Client-Capabilities"), ",") {
		if strings.TrimSpace(part) == capability {
			return true
		}
	}
	return false
}

func parseRuntimeConnectedAppsForClaim(raw []byte, taskID pgtype.UUID) []runtimeapps.ConnectedApp {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var apps []runtimeapps.ConnectedApp
	if err := json.Unmarshal(raw, &apps); err != nil {
		slog.Warn("daemon claim: unmarshal runtime_connected_apps failed",
			"task_id", uuidToString(taskID),
			"error", err,
		)
		return nil
	}
	return apps
}

func (h *Handler) repairStaleCommentPlanIfNeeded(ctx context.Context, task *db.AgentTaskQueue, runtimeWorkspaceID string) (handled bool, failure *claimBuildFailure) {
	if task.TriggerCommentID.Valid || len(task.CoalescedCommentIds) == 0 {
		return false, nil
	}
	if !task.IssueID.Valid {
		return true, &claimBuildFailure{outcome: "error_stale_comment_plan", status: http.StatusInternalServerError, message: "comment task has no issue"}
	}
	issue, loadErr := h.Queries.GetIssue(ctx, task.IssueID)
	if loadErr != nil {
		return true, &claimBuildFailure{outcome: "error_stale_comment_plan", status: http.StatusInternalServerError, message: "failed to repair stale comment task"}
	}
	if uuidToString(issue.WorkspaceID) != runtimeWorkspaceID {
		if _, cancelErr := h.TaskService.CancelTask(ctx, task.ID); cancelErr != nil {
			slog.Error("task claim: cancel stale cross-workspace task failed",
				"task_id", uuidToString(task.ID), "error", cancelErr)
		}
		return true, &claimBuildFailure{outcome: "error_workspace", status: http.StatusInternalServerError, message: "task workspace isolation check failed"}
	}
	cancelled, cancelErr := h.TaskService.CancelTask(ctx, task.ID)
	if cancelErr != nil {
		return true, &claimBuildFailure{outcome: "error_stale_comment_plan", status: http.StatusInternalServerError, message: "failed to repair stale comment task"}
	}
	h.retriggerCancelledTaskSurvivors(ctx, issue, []db.AgentTaskQueue{*cancelled}, pgtype.UUID{})
	return true, nil
}

const claimBatchMaxTasksCap = 32

func (h *Handler) ClaimTasksByRuntime(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req struct {
		DaemonID   string   `json:"daemon_id"`
		RuntimeIDs []string `json:"runtime_ids"`
		MaxTasks   int      `json:"max_tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.DaemonID == "" {
		writeError(w, http.StatusBadRequest, "daemon_id is required")
		return
	}
	if ctxDaemonID := middleware.DaemonIDFromContext(r.Context()); ctxDaemonID != "" && ctxDaemonID != req.DaemonID {
		writeError(w, http.StatusForbidden, "daemon_id does not match token")
		return
	}

	if req.MaxTasks < 0 {
		writeError(w, http.StatusBadRequest, "max_tasks must not be negative")
		return
	}
	if req.MaxTasks == 0 {
		writeMeasuredJSON(w, http.StatusOK, map[string]any{"tasks": []AgentTaskResponse{}})
		return
	}
	maxTasks := req.MaxTasks
	if maxTasks > claimBatchMaxTasksCap {
		maxTasks = claimBatchMaxTasksCap
	}

	idByKey := make(map[string]pgtype.UUID, len(req.RuntimeIDs))
	for _, rid := range req.RuntimeIDs {
		ruid, err := util.ParseUUID(rid)
		if err != nil {
			continue
		}
		idByKey[util.UUIDToString(ruid)] = ruid
	}
	if len(idByKey) == 0 {
		writeMeasuredJSON(w, http.StatusOK, map[string]any{"tasks": []AgentTaskResponse{}})
		return
	}
	ids := make([]pgtype.UUID, 0, len(idByKey))
	for _, id := range idByKey {
		ids = append(ids, id)
	}

	runtimes, err := h.Queries.GetAgentRuntimes(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load runtimes")
		return
	}
	runtimeByID := make(map[string]db.AgentRuntime, len(runtimes))
	authorized := make([]pgtype.UUID, 0, len(runtimes))
	for _, rt := range runtimes {
		if !h.verifyDaemonWorkspaceAccess(r, uuidToString(rt.WorkspaceID)) {
			continue
		}

		if rt.DaemonID.Valid && rt.DaemonID.String != req.DaemonID {
			continue
		}

		if !h.cfg.AllowedProviders.Allows(rt.Provider) {
			slog.Warn("batch claim: skipping runtime with provider disallowed by provider policy",
				"runtime_id", uuidToString(rt.ID), "provider", rt.Provider)
			continue
		}
		runtimeByID[uuidToString(rt.ID)] = rt
		authorized = append(authorized, rt.ID)
	}
	if len(authorized) == 0 {
		writeMeasuredJSON(w, http.StatusOK, map[string]any{"tasks": []AgentTaskResponse{}})
		return
	}

	claimed, err := h.TaskService.ClaimTasksForRuntimes(r.Context(), authorized, maxTasks)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to claim tasks: "+err.Error())
		return
	}

	out := make([]AgentTaskResponse, 0, len(claimed))
	for i := range claimed {
		task := claimed[i]
		rt, ok := runtimeByID[uuidToString(task.RuntimeID)]
		if !ok {

			continue
		}
		rtWorkspaceID := uuidToString(rt.WorkspaceID)

		if handled, _ := h.repairStaleCommentPlanIfNeeded(r.Context(), &task, rtWorkspaceID); handled {
			continue
		}
		resp, deliveredCommentIDs, _, _, failure := h.buildClaimedTaskResponse(r, &task, rt, uuidToString(task.RuntimeID), rtWorkspaceID)
		if failure != nil {

			continue
		}
		if !rt.OwnerID.Valid {
			slog.Error("batch claim: runtime owner missing; cancelling task to avoid unscoped agent credentials",
				"task_id", uuidToString(task.ID), "runtime_id", uuidToString(task.RuntimeID))
			if _, cerr := h.TaskService.CancelTask(r.Context(), task.ID); cerr != nil {
				slog.Error("batch claim: cancel after missing runtime owner failed",
					"task_id", uuidToString(task.ID), "error", cerr)
			}
			continue
		}
		tokenStr, terr := auth.GenerateAgentTaskToken()
		if terr != nil {
			slog.Error("batch claim: generate task token failed; requeueing claim",
				"task_id", uuidToString(task.ID), "error", terr)
			if _, rerr := h.TaskService.RequeueTaskAfterClaimFailure(r.Context(), task); rerr != nil {
				slog.Error("batch claim: requeue after token-gen failure failed",
					"task_id", uuidToString(task.ID), "error", rerr)
			}
			continue
		}

		commentBackedTask := task.TriggerCommentID.Valid || len(task.CoalescedCommentIds) > 0
		receipt, ferr := h.TaskService.FinalizeTaskClaim(r.Context(), task, db.CreateTaskTokenParams{
			TokenHash:   auth.HashToken(tokenStr),
			TaskID:      task.ID,
			AgentID:     task.AgentID,
			WorkspaceID: parseUUID(resp.WorkspaceID),
			UserID:      rt.OwnerID,
			ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
		}, deliveredCommentIDs, commentBackedTask)
		if ferr != nil {
			slog.Error("batch claim: finalize task claim failed; requeueing claim",
				"task_id", uuidToString(task.ID), "error", ferr)
			if _, rerr := h.TaskService.RequeueTaskAfterClaimFailure(r.Context(), task); rerr != nil {
				slog.Error("batch claim: requeue after finalize failure failed",
					"task_id", uuidToString(task.ID), "error", rerr)
			}
			continue
		}
		resp.AuthToken = tokenStr
		resp.DeliveredCommentIDs = uuidStringsOrEmpty(receipt)
		out = append(out, resp)
	}

	if len(out) > 0 {
		slog.Info("tasks claimed by runtime batch",
			"runtimes", len(authorized), "requested_max", maxTasks, "claimed", len(out),
			"total_ms", time.Since(start).Milliseconds())
	}
	writeMeasuredJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

type claimBuildFailure struct {
	outcome string
	status  int
	message string
}

func (h *Handler) buildClaimedTaskResponse(r *http.Request, task *db.AgentTaskQueue, runtime db.AgentRuntime, runtimeID, runtimeWorkspaceID string) (resp AgentTaskResponse, deliveredCommentIDs []pgtype.UUID, agentSkillCount, builtinSkillCount int, failure *claimBuildFailure) {

	resp = taskToResponse(*task, runtimeWorkspaceID)
	supportsCoalescedComments := requestHasClientCapability(r, protocol.DaemonCapabilityCoalescedCommentsV1)

	deliveredCommentIDs = []pgtype.UUID{}

	hasRuntimeMCPOverlay := len(task.RuntimeMcpOverlay) > 0
	if hasRuntimeMCPOverlay {
		resp.ConnectedApps = parseRuntimeConnectedAppsForClaim(task.RuntimeConnectedApps, task.ID)
	}

	if agent, err := h.Queries.GetAgent(r.Context(), task.AgentID); err == nil {
		useSkillRefs := requestHasClientCapability(r, protocol.DaemonCapabilitySkillBundlesV1)
		var customEnv map[string]string
		if agent.CustomEnv != nil {
			if err := json.Unmarshal(agent.CustomEnv, &customEnv); err != nil {
				slog.Warn("failed to unmarshal agent custom_env", "agent_id", uuidToString(agent.ID), "error", err)
			}
		}
		var customArgs []string
		if agent.CustomArgs != nil {
			if err := json.Unmarshal(agent.CustomArgs, &customArgs); err != nil {
				slog.Warn("failed to unmarshal agent custom_args", "agent_id", uuidToString(agent.ID), "error", err)
			}
		}

		mcpConfig, err := h.openMcpConfig(agent.McpConfig)
		if err != nil {
			h.auditSecretAccess(r, audit.ActionSecretRead, "", "agent_mcp_config",
				uuidToString(agent.ID), uuidToString(agent.WorkspaceID), audit.OutcomeFailure, audit.ReasonKeyUnavailable)
			slog.Error("daemon claim: open agent mcp_config failed; dispatching without it",
				"agent_id", uuidToString(agent.ID), "task_id", uuidToString(task.ID), "error", err)
			mcpConfig = nil
		} else if len(mcpConfig) > 0 {

			h.auditSecretAccess(r, audit.ActionSecretRead, "", "agent_mcp_config",
				uuidToString(agent.ID), uuidToString(agent.WorkspaceID), audit.OutcomeSuccess, "")
		}

		mcpCredentialUserID := agent.OwnerID
		if task.OriginatorUserID.Valid {
			mcpCredentialUserID = task.OriginatorUserID
		}
		if assignments, err := h.Queries.ListEnabledAgentMcpServers(r.Context(), db.ListEnabledAgentMcpServersParams{
			AgentID:     agent.ID,
			UserID:      mcpCredentialUserID,
			WorkspaceID: agent.WorkspaceID,
		}); err != nil {
			slog.Error("daemon claim: list assigned workspace mcp servers failed; dispatching without them",
				"agent_id", uuidToString(agent.ID), "task_id", uuidToString(task.ID), "error", err)
		} else if len(assignments) > 0 {
			assigned := make([]WorkspaceMcpAssignment, 0, len(assignments))
			for _, row := range assignments {

				opened, err := h.openConfigDocument(row.Config)
				if err != nil {
					slog.Error("daemon claim: open workspace mcp server failed; skipping it",
						"agent_id", uuidToString(agent.ID), "task_id", uuidToString(task.ID), "server", row.Name)
					continue
				}
				layered, missing, err := h.layerOwnerMcpCredentials(row.CredentialSchema, row.SealedValues, opened)
				if err != nil {
					slog.Error("daemon claim: layer owner mcp credentials failed; skipping the server",
						"agent_id", uuidToString(agent.ID), "task_id", uuidToString(task.ID), "server", row.Name)
					continue
				}
				if len(missing) > 0 {

					slog.Warn("daemon claim: shared mcp server skipped, the requesting user has not supplied required credentials",
						"agent_id", uuidToString(agent.ID), "task_id", uuidToString(task.ID),
						"server", row.Name, "missing_fields", missing,
						"credential_user_id", uuidToString(mcpCredentialUserID))
					continue
				}
				assigned = append(assigned, WorkspaceMcpAssignment{Name: row.Name, Config: layered})
			}
			if resolved, err := ResolveAgentMcpConfig(assigned, mcpConfig); err != nil {
				slog.Warn("daemon claim: resolve workspace mcp servers failed; falling back to agent mcp_config",
					"agent_id", uuidToString(agent.ID), "task_id", uuidToString(task.ID), "error", err)
			} else {
				mcpConfig = resolved
			}
		}

		if hasRuntimeMCPOverlay {
			if merged, err := mergeMCPOverlay(mcpConfig, json.RawMessage(task.RuntimeMcpOverlay)); err != nil {
				slog.Warn("daemon claim: merge runtime_mcp_overlay failed; falling back to agent mcp_config", "task_id", uuidToString(task.ID), "error", err)
			} else {
				mcpConfig = merged
			}
		}

		if filtered, dropped := h.cfg.MCPPolicy.FilterConfig(mcpConfig); len(dropped) > 0 {
			for _, v := range dropped {
				slog.Warn("daemon claim: dropping mcp_config entry disallowed by MCP policy",
					"task_id", uuidToString(task.ID), "agent_id", uuidToString(agent.ID),
					"server", v.Server, "reason", v.Reason)
			}
			mcpConfig = filtered
		}

		var runtimeConfig json.RawMessage
		if rc := bytes.TrimSpace(agent.RuntimeConfig); len(rc) > 0 && !bytes.Equal(rc, []byte("{}")) && !bytes.Equal(rc, []byte("null")) {
			runtimeConfig = json.RawMessage(agent.RuntimeConfig)
		}
		resp.Agent = &TaskAgentData{
			ID:                    uuidToString(agent.ID),
			Name:                  agent.Name,
			Instructions:          agent.Instructions,
			CustomEnv:             customEnv,
			CustomArgs:            customArgs,
			McpConfig:             mcpConfig,
			Model:                 agent.Model.String,
			ThinkingLevel:         agent.ThinkingLevel.String,
			ServiceTier:           agent.ServiceTier.String,
			RuntimeConfig:         runtimeConfig,
			DisabledRuntimeSkills: disabledRuntimeSkillsFor(agent.DisabledRuntimeSkills, runtimeID, runtime.Provider),
		}
		if useSkillRefs {
			_, skillRefs := h.TaskService.LoadAgentSkillBundles(r.Context(), task.AgentID)
			agentSkillCount = len(skillRefs)
			resp.Agent.SkillRefs = skillRefs
		} else {
			skills := h.TaskService.LoadAgentSkills(r.Context(), task.AgentID)
			agentSkillCount = len(skills)
			builtinSkills := h.TaskService.BuiltinSkills()
			builtinSkillCount = len(builtinSkills)
			skills = append(skills, builtinSkills...)
			resp.Agent.Skills = skills
		}
	}

	if runtime.OwnerID.Valid {
		if owner, err := h.Queries.GetUser(r.Context(), runtime.OwnerID); err == nil {
			resp.RequestingUserName = owner.Name
			resp.RequestingUserProfileDescription = owner.ProfileDescription
		} else {
			slog.Debug("failed to load runtime owner for brief injection",
				"runtime_id", runtimeID,
				"owner_id", uuidToString(runtime.OwnerID),
				"error", err,
			)
		}
	}

	resp.McpPolicy = daemonMcpPolicyFor(h.cfg.MCPPolicy)
	if task.InitiatorUserID.Valid {
		resp.InitiatorType = "member"
		resp.InitiatorID = uuidToString(task.InitiatorUserID)
		if u, err := h.Queries.GetUser(r.Context(), task.InitiatorUserID); err == nil {
			resp.InitiatorName = u.Name
			resp.InitiatorEmail = u.Email
		}
	}

	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(r.Context(), task.IssueID); err == nil {
			resp.WorkspaceID = uuidToString(issue.WorkspaceID)
			resp.ThreadName = issue.Title

			if resp.Agent != nil && task.IsLeaderTask && task.SquadID.Valid {
				if squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
					ID:          task.SquadID,
					WorkspaceID: issue.WorkspaceID,
				}); err == nil && uuidToString(squad.LeaderID) == resp.Agent.ID {

					ownsIssueStatus := issue.AssigneeType.Valid &&
						issue.AssigneeType.String == "squad" &&
						uuidToString(issue.AssigneeID) == uuidToString(squad.ID)
					briefing := buildSquadLeaderBriefing(r.Context(), h.Queries, squad, ownsIssueStatus)
					if strings.TrimSpace(resp.Agent.Instructions) == "" {
						resp.Agent.Instructions = briefing
					} else {
						resp.Agent.Instructions = resp.Agent.Instructions + "\n\n" + briefing
					}
					slog.Debug("injected squad leader briefing",
						"squad_id", uuidToString(squad.ID),
						"squad_name", squad.Name,
						"leader_agent_id", resp.Agent.ID,
						"owns_issue_status", ownsIssueStatus,
					)
				}
			}

			var projectRepos []RepoData
			if issue.ProjectID.Valid {
				resp.ProjectID = uuidToString(issue.ProjectID)
				if proj, err := h.Queries.GetProject(r.Context(), issue.ProjectID); err == nil {
					resp.ProjectTitle = proj.Title
					resp.ProjectDescription = proj.Description.String
				}
				if rows := h.listProjectResourcesForProject(r.Context(), issue.ProjectID); len(rows) > 0 {
					out := make([]ProjectResourceData, 0, len(rows))
					for _, row := range rows {
						label := ""
						if row.Label.Valid {
							label = row.Label.String
						}
						ref := json.RawMessage(row.ResourceRef)
						if len(ref) == 0 {
							ref = json.RawMessage("{}")
						}
						out = append(out, ProjectResourceData{
							ID:           uuidToString(row.ID),
							ResourceType: row.ResourceType,
							ResourceRef:  ref,
							Label:        label,
						})

						if row.ResourceType == "github_repo" {
							var payload struct {
								URL string `json:"url"`
								Ref string `json:"ref,omitempty"`
							}
							if json.Unmarshal(row.ResourceRef, &payload) == nil && payload.URL != "" {
								projectRepos = append(projectRepos, RepoData{URL: payload.URL, Ref: strings.TrimSpace(payload.Ref)})
							}
						}
					}
					resp.ProjectResources = out
				}
			}

			if len(projectRepos) > 0 {
				resp.Repos = projectRepos
			} else if ws, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID); err == nil && ws.Repos != nil {
				var repos []RepoData
				if json.Unmarshal(ws.Repos, &repos) == nil && len(repos) > 0 {
					resp.Repos = repos
				}
			}
		}

		plannedCommentIDs := append([]pgtype.UUID{}, task.CoalescedCommentIds...)
		if task.TriggerCommentID.Valid {
			plannedCommentIDs = append(plannedCommentIDs, task.TriggerCommentID)
		}
		loadedComments := h.buildCoalescedCommentData(r.Context(), runtime.WorkspaceID, plannedCommentIDs)
		triggerCommentID := uuidToString(task.TriggerCommentID)
		var deliveredComments []CoalescedCommentData
		triggerLoaded := false
		for _, comment := range loadedComments {
			if comment.ID == triggerCommentID {
				triggerLoaded = true
				break
			}
		}
		if task.TriggerCommentID.Valid && triggerLoaded {
			deliveredComments = selectCommentDelivery(
				loadedComments,
				triggerCommentID,
				!supportsCoalescedComments,
				maxClaimCommentPayloadBytes,
			)
		}

		deliveredCommentIDs = commentDataIDs(deliveredComments)

		resp.CoalescedCommentIDs = nil
		for _, comment := range deliveredComments {
			if comment.ID == triggerCommentID {

				resp.TriggerCommentContent = comment.Content
				resp.TriggerThreadID = comment.ThreadID
				resp.TriggerAuthorType = comment.AuthorType
				resp.TriggerAuthorName = comment.AuthorName
				continue
			}
			resp.CoalescedCommentIDs = append(resp.CoalescedCommentIDs, comment.ID)
			resp.CoalescedComments = append(resp.CoalescedComments, comment)
		}

		effectiveTriggerUUID := task.TriggerCommentID
		if effectiveTriggerUUID.Valid {

			if comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
				ID:          effectiveTriggerUUID,
				WorkspaceID: runtime.WorkspaceID,
			}); err == nil {
				resp.TriggerCommentContent = comment.Content
				resp.TriggerThreadID = uuidToString(comment.ID)
				if comment.ParentID.Valid {
					resp.TriggerThreadID = uuidToString(comment.ParentID)
				}
				resp.TriggerAuthorType = comment.AuthorType

				resp.InitiatorType = comment.AuthorType
				if comment.AuthorID.Valid {
					resp.InitiatorID = uuidToString(comment.AuthorID)
				}
				switch comment.AuthorType {
				case "agent":
					if comment.AuthorID.Valid {
						if a, err := h.Queries.GetAgent(r.Context(), comment.AuthorID); err == nil {
							resp.TriggerAuthorName = a.Name
							resp.InitiatorName = a.Name
						}
					}
				case "member":

					if comment.AuthorID.Valid {
						if u, err := h.Queries.GetUser(r.Context(), comment.AuthorID); err == nil {
							resp.TriggerAuthorName = u.Name
							resp.InitiatorName = u.Name
							resp.InitiatorEmail = u.Email
						}
					}
				}

				if startedAt, err := h.Queries.GetLastTaskStartedAtForIssueAndAgent(r.Context(), db.GetLastTaskStartedAtForIssueAndAgentParams{
					AgentID: task.AgentID,
					IssueID: comment.IssueID,
				}); err == nil && startedAt.Valid {
					if cnt, err := h.Queries.CountNewCommentsSince(r.Context(), db.CountNewCommentsSinceParams{
						AnchorID:    effectiveTriggerUUID,
						IssueID:     comment.IssueID,
						WorkspaceID: comment.WorkspaceID,
						Since:       startedAt,
						AuthorID:    task.AgentID,
					}); err == nil && cnt > 0 {
						resp.NewCommentCount = int(cnt)
						resp.NewCommentsSince = startedAt.Time.UTC().Format(time.RFC3339)
					}
				}
			}
		}

		if !supportsCoalescedComments {

			if len(resp.CoalescedComments) > 0 || (resp.TriggerCommentContent == "" && len(deliveredComments) > 0) {
				resp.TriggerCommentContent = formatLegacyCommentBundle(deliveredComments)
			}
			resp.CoalescedCommentIDs = nil
			resp.CoalescedComments = nil
		} else if resp.TriggerCommentContent == "" && len(deliveredComments) > 0 {

			resp.TriggerCommentContent = "The newest triggering comment is no longer available. Address every earlier comment included below."
		}

		if task.RerunOfTaskID.Valid {

			if src, err := h.Queries.GetAgentTask(r.Context(), task.RerunOfTaskID); err == nil {
				if src.WorkDir.Valid {
					resp.PriorWorkDir = src.WorkDir.String
				}
				if !service.ResumeUnsafeFailure(src.FailureReason.String, src.Error.String) &&
					src.SessionID.Valid && src.RuntimeID == task.RuntimeID {
					resp.PriorSessionID = src.SessionID.String
				}

				if src.SessionRolloutMissing {
					resp.PriorSessionResumeUnavailable = true
				}
			}
		} else if !task.ForceFreshSession {

			if prior, err := h.Queries.GetLastTaskSession(r.Context(), db.GetLastTaskSessionParams{
				AgentID: task.AgentID,
				IssueID: task.IssueID,
			}); err == nil && prior.SessionID.Valid {
				if prior.RuntimeID == task.RuntimeID {
					resp.PriorSessionID = prior.SessionID.String
				}
				if prior.WorkDir.Valid {
					resp.PriorWorkDir = prior.WorkDir.String
				}
			}

			if missing, err := h.Queries.GetLatestTaskRolloutMissing(r.Context(), db.GetLatestTaskRolloutMissingParams{
				AgentID: task.AgentID,
				IssueID: task.IssueID,
			}); err == nil && missing {
				resp.PriorSessionResumeUnavailable = true
			}
		}
	}

	if task.ChatSessionID.Valid {
		if cs, err := h.Queries.GetChatSession(r.Context(), task.ChatSessionID); err == nil {
			resp.WorkspaceID = uuidToString(cs.WorkspaceID)
			resp.ChatSessionID = uuidToString(cs.ID)
			resp.ThreadName = cs.Title

			if cs.IsAgentIntro {
				if hasUser, herr := h.Queries.ChatSessionHasUserMessage(r.Context(), cs.ID); herr != nil {
					slog.Warn("chat intro gate: has-user-message check failed",
						"chat_session_id", uuidToString(cs.ID), "error", herr)
				} else {
					resp.ChatIntro = !hasUser
				}
			}

			for _, channelType := range []channel.Type{slack.TypeSlack} {
				binding, berr := h.Queries.GetChannelChatSessionBindingBySession(r.Context(), db.GetChannelChatSessionBindingBySessionParams{
					ChatSessionID: cs.ID,
					ChannelType:   string(channelType),
				})
				if berr != nil {
					continue
				}
				resp.ChatChannelType = string(channelType)
				if channelType == slack.TypeSlack {

					resp.ChatInThread = binding.LastThreadID.Valid && binding.LastThreadID.String != "" &&
						binding.LastThreadID.String != binding.LastMessageID.String
				}
				break
			}

			var projectRepos []RepoData
			if cs.ProjectID.Valid {
				if project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
					ID:          cs.ProjectID,
					WorkspaceID: cs.WorkspaceID,
				}); err == nil {
					resp.ProjectID = uuidToString(project.ID)
					resp.ProjectTitle = project.Title
					resp.ProjectDescription = project.Description.String
					if rows := h.listProjectResourcesForProject(r.Context(), project.ID); len(rows) > 0 {
						resources := make([]ProjectResourceData, 0, len(rows))
						for _, row := range rows {
							label := ""
							if row.Label.Valid {
								label = row.Label.String
							}
							ref := json.RawMessage(row.ResourceRef)
							if len(ref) == 0 {
								ref = json.RawMessage("{}")
							}
							resources = append(resources, ProjectResourceData{
								ID:           uuidToString(row.ID),
								ResourceType: row.ResourceType,
								ResourceRef:  ref,
								Label:        label,
							})
							if row.ResourceType == "github_repo" {
								var payload struct {
									URL string `json:"url"`
									Ref string `json:"ref,omitempty"`
								}
								if json.Unmarshal(row.ResourceRef, &payload) == nil && payload.URL != "" {
									projectRepos = append(projectRepos, RepoData{URL: payload.URL, Ref: strings.TrimSpace(payload.Ref)})
								}
							}
						}
						resp.ProjectResources = resources
					}
				}
			}
			if len(projectRepos) > 0 {
				resp.Repos = projectRepos
			} else if ws, err := h.Queries.GetWorkspace(r.Context(), cs.WorkspaceID); err == nil && ws.Repos != nil {
				var repos []RepoData
				if json.Unmarshal(ws.Repos, &repos) == nil && len(repos) > 0 {
					resp.Repos = repos
				}
			}
			if !task.ForceFreshSession {

				if cs.SessionID.Valid && cs.RuntimeID.Valid && cs.RuntimeID == task.RuntimeID {
					resp.PriorSessionID = cs.SessionID.String
				}
				if cs.WorkDir.Valid {
					resp.PriorWorkDir = cs.WorkDir.String
				}
				if prior, err := h.Queries.GetLastChatTaskSession(r.Context(), cs.ID); err == nil && prior.SessionID.Valid {
					if resp.PriorSessionID == "" && prior.RuntimeID == task.RuntimeID {
						resp.PriorSessionID = prior.SessionID.String
					}
					if prior.WorkDir.Valid && resp.PriorWorkDir == "" {
						resp.PriorWorkDir = prior.WorkDir.String
					}
				}

				if missing, err := h.Queries.GetLatestChatTaskRolloutMissing(r.Context(), cs.ID); err == nil && missing {
					resp.PriorSessionResumeUnavailable = true
				}
			}

			var unanswered []db.ChatMessage
			var inputLoadErr error
			if task.ChatInputTaskID.Valid {
				unanswered, inputLoadErr = h.Queries.ListChatInputMessages(r.Context(), task.ChatInputTaskID)
			} else if msgs, err := h.Queries.ListChatMessages(r.Context(), cs.ID); err == nil {
				unanswered = trailingUserMessages(msgs)
			} else {
				inputLoadErr = err
			}

			if inputLoadErr != nil {
				slog.Error("chat claim: load chat input messages failed; preserving task for redelivery",
					"task_id", uuidToString(task.ID),
					"chat_session_id", uuidToString(cs.ID),
					"error", inputLoadErr)
				return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
					outcome: "error_chat_input_load",
					status:  http.StatusInternalServerError,
					message: "failed to load chat input",
				}
			}

			parts := make([]string, 0, len(unanswered))
			for _, m := range unanswered {
				if strings.TrimSpace(m.Content) != "" {
					parts = append(parts, m.Content)
				}
				if atts, attErr := h.Queries.ListAttachmentsByChatMessage(r.Context(), db.ListAttachmentsByChatMessageParams{
					ChatMessageID: m.ID,
					WorkspaceID:   parseUUID(resp.WorkspaceID),
				}); attErr == nil && len(atts) > 0 {
					for _, a := range atts {
						resp.ChatMessageAttachments = append(resp.ChatMessageAttachments, ChatAttachmentMeta{
							ID:          uuidToString(a.ID),
							Filename:    a.Filename,
							ContentType: a.ContentType,
						})
					}
				}
			}
			resp.ChatMessage = strings.Join(parts, "\n\n")

			if task.ChatInputTaskID.Valid && !resp.ChatIntro && strings.TrimSpace(resp.ChatMessage) == "" {
				slog.Error("chat claim: task-owned direct task has no user input; cancelling",
					"task_id", uuidToString(task.ID),
					"chat_session_id", uuidToString(cs.ID),
					"chat_input_task_id", uuidToString(task.ChatInputTaskID),
				)
				if _, cerr := h.TaskService.CancelTask(r.Context(), task.ID); cerr != nil {
					slog.Error("chat claim: cancel after empty input failed",
						"task_id", uuidToString(task.ID), "error", cerr)
				}
				return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
					outcome: "error_empty_chat_input",
					status:  http.StatusInternalServerError,
					message: "chat task has no user input",
				}
			}

			if strings.TrimSpace(resp.ThreadName) == "" && resp.ChatMessage != "" {
				resp.ThreadName = resp.ChatMessage
			}
		}
	}

	if task.AutopilotRunID.Valid {
		if run, err := h.Queries.GetAutopilotRun(r.Context(), task.AutopilotRunID); err == nil {
			resp.AutopilotID = uuidToString(run.AutopilotID)
			resp.AutopilotSource = run.Source
			if run.TriggerPayload != nil {
				resp.AutopilotTriggerPayload = json.RawMessage(run.TriggerPayload)
			}
			if ap, err := h.Queries.GetAutopilot(r.Context(), run.AutopilotID); err == nil {
				resp.AutopilotTitle = ap.Title
				resp.ThreadName = ap.Title
				if ap.Description.Valid {
					resp.AutopilotDescription = ap.Description.String
				}
				if resp.WorkspaceID == "" {
					resp.WorkspaceID = uuidToString(ap.WorkspaceID)
				}
				if len(resp.Repos) == 0 {
					if ws, err := h.Queries.GetWorkspace(r.Context(), ap.WorkspaceID); err == nil && ws.Repos != nil {
						var repos []RepoData
						if json.Unmarshal(ws.Repos, &repos) == nil && len(repos) > 0 {
							resp.Repos = repos
						}
					}
				}
			}
		}
	}

	hasQuickCreate := false
	if task.Context != nil && !task.IssueID.Valid && !task.ChatSessionID.Valid && !task.AutopilotRunID.Valid {
		var qc service.QuickCreateContext
		if json.Unmarshal(task.Context, &qc) == nil && qc.Type == service.QuickCreateContextType {
			hasQuickCreate = true
			resp.QuickCreatePrompt = qc.Prompt
			resp.QuickCreatePriority = qc.Priority
			resp.QuickCreateDueDate = qc.DueDate
			resp.QuickCreateAttachmentIDs = append([]string(nil), qc.AttachmentIDs...)
			resp.ThreadName = qc.Prompt
			resp.WorkspaceID = qc.WorkspaceID

			var projectRepos []RepoData
			if qc.ProjectID != "" {
				projectUUID, err := util.ParseUUID(qc.ProjectID)
				if err == nil {
					resp.ProjectID = qc.ProjectID
					if proj, err := h.Queries.GetProject(r.Context(), projectUUID); err == nil {
						resp.ProjectTitle = proj.Title
						resp.ProjectDescription = proj.Description.String
					}
					if rows := h.listProjectResourcesForProject(r.Context(), projectUUID); len(rows) > 0 {
						out := make([]ProjectResourceData, 0, len(rows))
						for _, row := range rows {
							label := ""
							if row.Label.Valid {
								label = row.Label.String
							}
							ref := json.RawMessage(row.ResourceRef)
							if len(ref) == 0 {
								ref = json.RawMessage("{}")
							}
							out = append(out, ProjectResourceData{
								ID:           uuidToString(row.ID),
								ResourceType: row.ResourceType,
								ResourceRef:  ref,
								Label:        label,
							})
							if row.ResourceType == "github_repo" {
								var payload struct {
									URL string `json:"url"`
									Ref string `json:"ref,omitempty"`
								}
								if json.Unmarshal(row.ResourceRef, &payload) == nil && payload.URL != "" {
									projectRepos = append(projectRepos, RepoData{URL: payload.URL, Ref: strings.TrimSpace(payload.Ref)})
								}
							}
						}
						resp.ProjectResources = out
					}
				}
			}

			if len(projectRepos) > 0 {
				resp.Repos = projectRepos
			} else if ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(qc.WorkspaceID)); err == nil && ws.Repos != nil {
				var repos []RepoData
				if json.Unmarshal(ws.Repos, &repos) == nil && len(repos) > 0 {
					resp.Repos = repos
				}
			}

			if qc.ParentIssueID != "" {
				resp.ParentIssueID = qc.ParentIssueID
				if parentUUID, err := util.ParseUUID(qc.ParentIssueID); err == nil {
					if wsUUID, wsErr := util.ParseUUID(qc.WorkspaceID); wsErr == nil {
						parent, perr := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
							ID:          parentUUID,
							WorkspaceID: wsUUID,
						})
						if perr == nil && parent.ID.Valid {
							if ws, werr := h.Queries.GetWorkspace(r.Context(), wsUUID); werr == nil {
								resp.ParentIssueIdentifier = ws.IssuePrefix + "-" + strconv.Itoa(int(parent.Number))
							}
						}
					}
				}
			}

			if resp.Agent != nil && qc.SquadID != "" {
				wsUUID, wsErr := util.ParseUUID(qc.WorkspaceID)
				squadUUID, sqErr := util.ParseUUID(qc.SquadID)
				if wsErr == nil && sqErr == nil {
					if squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
						ID:          squadUUID,
						WorkspaceID: wsUUID,
					}); err == nil && uuidToString(squad.LeaderID) == resp.Agent.ID {

						briefing := buildSquadLeaderBriefing(r.Context(), h.Queries, squad, false)
						if strings.TrimSpace(resp.Agent.Instructions) == "" {
							resp.Agent.Instructions = briefing
						} else {
							resp.Agent.Instructions = resp.Agent.Instructions + "\n\n" + briefing
						}

						resp.SquadID = uuidToString(squad.ID)
						resp.SquadName = squad.Name
						slog.Debug("injected squad leader briefing for quick-create",
							"squad_id", uuidToString(squad.ID),
							"squad_name", squad.Name,
							"leader_agent_id", resp.Agent.ID,
						)
					}
				}
			}
		}
	}

	if resp.WorkspaceID == "" || resp.WorkspaceID != runtimeWorkspaceID {
		slog.Error("task claim: workspace isolation check failed, cancelling task",
			"task_id", uuidToString(task.ID),
			"runtime_id", runtimeID,
			"runtime_workspace", runtimeWorkspaceID,
			"resolved_workspace", resp.WorkspaceID,
			"has_issue", task.IssueID.Valid,
			"has_chat", task.ChatSessionID.Valid,
			"has_autopilot_run", task.AutopilotRunID.Valid,
			"has_quick_create", hasQuickCreate,
		)
		if _, cerr := h.TaskService.CancelTask(r.Context(), task.ID); cerr != nil {
			slog.Error("task claim: cancel after workspace check failed",
				"task_id", uuidToString(task.ID), "error", cerr)
		}
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
			outcome: "error_workspace",
			status:  http.StatusInternalServerError,
			message: "task workspace isolation check failed",
		}
	}

	if ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(resp.WorkspaceID)); err == nil {
		if ws.Context.Valid {
			resp.WorkspaceContext = ws.Context.String
		}
	} else {
		slog.Warn("task claim: failed to load workspace for context injection",
			"task_id", uuidToString(task.ID),
			"workspace_id", resp.WorkspaceID,
			"error", err,
		)
	}

	return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, nil
}

func (h *Handler) ClaimTaskByRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	start := time.Now()

	var (
		outcome                  = "unauth"
		authMs, claimMs, buildMs int64
		payloadBytes             int
		agentSkillCount          int
		builtinSkillCount        int
		skillPayloadBytes        int
		buildStart               time.Time
	)
	defer func() {

		if !buildStart.IsZero() {
			buildMs = time.Since(buildStart).Milliseconds()
		}
		logClaimEndpointSlow(runtimeID, outcome, start, authMs, claimMs, buildMs, payloadBytes, agentSkillCount, builtinSkillCount, skillPayloadBytes)
	}()

	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	runtimeWorkspaceID := uuidToString(runtime.WorkspaceID)
	authMs = time.Since(start).Milliseconds()

	if !h.cfg.AllowedProviders.Allows(runtime.Provider) {
		slog.Warn("task claim: runtime provider disallowed by provider policy; not dispatching",
			"runtime_id", runtimeID, "provider", runtime.Provider)
		payloadBytes, _ = writeMeasuredJSON(w, http.StatusOK, map[string]any{"task": nil})
		outcome = "provider_disallowed"
		return
	}

	claimStart := time.Now()
	task, err := h.TaskService.ClaimTaskForRuntime(r.Context(), parseUUID(runtimeID))
	claimMs = time.Since(claimStart).Milliseconds()
	if err != nil {
		outcome = "error_claim"
		writeError(w, http.StatusInternalServerError, "failed to claim task: "+err.Error())
		return
	}

	if task == nil {
		slog.Debug("no task to claim", "runtime_id", runtimeID)
		payloadBytes, _ = writeMeasuredJSON(w, http.StatusOK, map[string]any{"task": nil})
		outcome = "no_task"
		return
	}
	if !task.TriggerCommentID.Valid && len(task.CoalescedCommentIds) > 0 {
		handled, failure := h.repairStaleCommentPlanIfNeeded(r.Context(), task, runtimeWorkspaceID)
		if handled {
			if failure != nil {
				outcome = failure.outcome
				writeError(w, failure.status, failure.message)
				return
			}
			outcome = "repaired_stale_comment_plan"
			payloadBytes, _ = writeMeasuredJSON(w, http.StatusOK, map[string]any{"task": nil})
			return
		}
	}

	outcome = "claimed"
	buildStart = time.Now()

	resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, failure := h.buildClaimedTaskResponse(r, task, runtime, runtimeID, runtimeWorkspaceID)
	if failure != nil {
		outcome = failure.outcome
		writeError(w, failure.status, failure.message)
		return
	}
	commentBackedTask := task.TriggerCommentID.Valid || len(task.CoalescedCommentIds) > 0
	requeueFailedClaim := func(reason string) {
		if _, err := h.TaskService.RequeueTaskAfterClaimFailure(r.Context(), *task); err != nil {
			slog.Error("task claim: failed to requeue after finalization error",
				"task_id", uuidToString(task.ID),
				"reason", reason,
				"error", err,
			)
		}
	}

	if !runtime.OwnerID.Valid {
		outcome = "error_token"
		slog.Error("task claim: runtime owner missing; cancelling task to avoid unscoped agent credentials",
			"task_id", uuidToString(task.ID),
			"runtime_id", runtimeID,
			"workspace_id", runtimeWorkspaceID,
		)
		if _, cerr := h.TaskService.CancelTask(r.Context(), task.ID); cerr != nil {
			slog.Error("task claim: cancel after missing runtime owner failed",
				"task_id", uuidToString(task.ID), "error", cerr)
		}
		writeError(w, http.StatusInternalServerError, "runtime owner required to mint task token")
		return
	}
	tokenStr, terr := auth.GenerateAgentTaskToken()
	if terr != nil {
		outcome = "error_token"
		slog.Error("task claim: failed to generate agent task token",
			"task_id", uuidToString(task.ID), "error", terr)
		requeueFailedClaim("token_generation")
		writeError(w, http.StatusInternalServerError, "failed to mint task token")
		return
	}
	receipt, ferr := h.TaskService.FinalizeTaskClaim(r.Context(), *task, db.CreateTaskTokenParams{
		TokenHash:   auth.HashToken(tokenStr),
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		WorkspaceID: parseUUID(resp.WorkspaceID),
		UserID:      runtime.OwnerID,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}, deliveredCommentIDs, commentBackedTask)
	if ferr != nil {
		outcome = "error_claim_finalize"
		slog.Error("task claim: failed to finalize token and comment delivery receipt",
			"task_id", uuidToString(task.ID), "error", ferr)

		requeueFailedClaim("token_and_delivery_receipt")
		writeError(w, http.StatusInternalServerError, "failed to finalize task claim")
		return
	}
	resp.AuthToken = tokenStr
	task.DeliveredCommentIds = receipt
	resp.DeliveredCommentIDs = uuidStringsOrEmpty(receipt)

	slog.Info("task claimed by runtime", "task_id", uuidToString(task.ID), "runtime_id", runtimeID, "agent_id", uuidToString(task.AgentID), "prior_session", resp.PriorSessionID)
	if resp.Agent != nil && len(resp.Agent.Skills) > 0 {
		if skillPayload, err := json.Marshal(resp.Agent.Skills); err == nil {
			skillPayloadBytes = len(skillPayload)
		}
	} else if resp.Agent != nil && len(resp.Agent.SkillRefs) > 0 {
		if skillPayload, err := json.Marshal(resp.Agent.SkillRefs); err == nil {
			skillPayloadBytes = len(skillPayload)
		}
	}
	payloadBytes, _ = writeMeasuredJSON(w, http.StatusOK, map[string]any{"task": resp})
}

type resolveSkillBundlesRequest struct {
	Skills []resolveSkillBundleRef `json:"skills"`

	ContentEncoding string `json:"content_encoding,omitempty"`
}

const skillBundleEncodingBase64 = "base64"

func encodeSkillBundleContent(bundles []service.AgentSkillData) []service.AgentSkillData {
	encoded := make([]service.AgentSkillData, len(bundles))
	for i, bundle := range bundles {
		copied := bundle
		copied.Content = base64.StdEncoding.EncodeToString([]byte(bundle.Content))
		if len(bundle.Files) > 0 {
			files := make([]service.AgentSkillFileData, len(bundle.Files))
			for j, file := range bundle.Files {
				copiedFile := file
				copiedFile.Content = base64.StdEncoding.EncodeToString([]byte(file.Content))
				files[j] = copiedFile
			}
			copied.Files = files
		}
		encoded[i] = copied
	}
	return encoded
}

type resolveSkillBundleRef struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Hash   string `json:"hash"`
}

func (h *Handler) ResolveTaskSkillBundles(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	taskID := chi.URLParam(r, "taskId")

	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	task, taskWorkspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}
	if taskWorkspaceID != uuidToString(runtime.WorkspaceID) || uuidToString(task.RuntimeID) != runtimeID {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status != "dispatched" && task.Status != "waiting_local_directory" {
		writeError(w, http.StatusConflict, "task is not preparing")
		return
	}

	var req resolveSkillBundlesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Skills) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"bundles": []service.AgentSkillData{}})
		return
	}

	bundles, _ := h.TaskService.LoadAgentSkillBundles(r.Context(), task.AgentID)
	allowed := make(map[string]service.AgentSkillData, len(bundles))
	for _, bundle := range bundles {
		allowed[bundle.Source+"\x00"+bundle.ID] = bundle
	}

	resolved := make([]service.AgentSkillData, 0, len(req.Skills))
	for _, ref := range req.Skills {
		if ref.ID == "" || ref.Source == "" || ref.Hash == "" {
			writeError(w, http.StatusBadRequest, "invalid skill ref")
			return
		}
		bundle, ok := allowed[ref.Source+"\x00"+ref.ID]
		if !ok {
			writeError(w, http.StatusNotFound, "skill bundle not found")
			return
		}
		resolved = append(resolved, bundle)
	}

	if req.ContentEncoding == skillBundleEncodingBase64 {
		writeJSON(w, http.StatusOK, map[string]any{
			"bundles": encodeSkillBundleContent(resolved),

			"content_encoding": skillBundleEncodingBase64,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bundles": resolved})
}

func trailingUserMessages(msgs []db.ChatMessage) []db.ChatMessage {
	start := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			start = i + 1
			break
		}
	}
	msgs = msgs[start:]
	now := time.Now()
	for i := range msgs {
		pending := msgs[i].ChannelMediaPendingUntil
		if pending.Valid && pending.Time.After(now) {
			return msgs[:i]
		}
	}
	return msgs
}

func (h *Handler) ListPendingTasksByRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	workspaceID := uuidToString(runtime.WorkspaceID)

	tasks, err := h.Queries.ListPendingTasksByRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending tasks")
		return
	}

	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToResponse(t, workspaceID)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ExtendTaskPrepareLease(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	taskID := chi.URLParam(r, "taskId")

	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	task, taskWorkspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}
	if taskWorkspaceID != uuidToString(runtime.WorkspaceID) || uuidToString(task.RuntimeID) != runtimeID {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	updated, err := h.TaskService.ExtendTaskPrepareLease(r.Context(), parseUUID(taskID), parseUUID(runtimeID))
	if err != nil {
		slog.Warn("extend task prepare lease failed", "task_id", taskID, "runtime_id", runtimeID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, taskToResponse(*updated, taskWorkspaceID))
}

func (h *Handler) StartTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	_, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}

	task, err := h.TaskService.StartTask(r.Context(), parseUUID(taskID))
	if err != nil {
		slog.Warn("start task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("task started", "task_id", taskID, "agent_id", uuidToString(task.AgentID))
	writeJSON(w, http.StatusOK, taskToResponse(*task, workspaceID))
}

type TaskWaitLocalDirectoryRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) MarkTaskWaitingLocalDirectory(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	_, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}

	var req TaskWaitLocalDirectoryRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	task, err := h.TaskService.MarkTaskWaitingLocalDirectory(r.Context(), parseUUID(taskID), req.Reason)
	if err != nil {
		slog.Warn("mark task waiting_local_directory failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, taskToResponse(*task, workspaceID))
}

type TaskProgressRequest struct {
	Summary string `json:"summary"`
	Step    int    `json:"step"`
	Total   int    `json:"total"`
}

func (h *Handler) ReportTaskProgress(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	var req TaskProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}

	workspaceID := ""
	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(r.Context(), task.IssueID); err == nil {
			workspaceID = uuidToString(issue.WorkspaceID)
		}
	}

	h.TaskService.ReportProgress(r.Context(), taskID, workspaceID, req.Summary, req.Step, req.Total)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type TaskCompleteRequest struct {
	PRURL     string `json:"pr_url"`
	Output    string `json:"output"`
	SessionID string `json:"session_id"`
	WorkDir   string `json:"work_dir"`

	SessionRolloutMissing bool `json:"session_rollout_missing,omitempty"`
}

func sanitizeTaskCompleteRequest(req *TaskCompleteRequest) {
	req.PRURL = util.SanitizeTextForPostgres(req.PRURL)
	req.Output = util.SanitizeTextForPostgres(req.Output)
	req.SessionID = util.SanitizeTextForPostgres(req.SessionID)
	req.WorkDir = util.SanitizeTextForPostgres(req.WorkDir)
}

func sanitizeTaskFailRequest(req *TaskFailRequest) {
	req.Error = util.SanitizeTextForPostgres(req.Error)
	req.SessionID = util.SanitizeTextForPostgres(req.SessionID)
	req.WorkDir = util.SanitizeTextForPostgres(req.WorkDir)
	req.FailureReason = util.SanitizeTextForPostgres(req.FailureReason)
}

func (h *Handler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	_, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}

	var req TaskCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	sanitizeTaskCompleteRequest(&req)

	result, _ := json.Marshal(req)

	task, err := h.TaskService.CompleteTask(r.Context(), parseUUID(taskID), result, req.SessionID, req.WorkDir, req.SessionRolloutMissing)
	if err != nil {

		slog.Warn("complete task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.emitIssueExecutedOnFirstCompletion(r, task)

	h.reconcileCommentsOnCompletion(r.Context(), task)

	h.TaskService.NotifyTaskFinished(*task)

	if err := h.Queries.DeleteTaskTokensByTask(r.Context(), task.ID); err != nil {
		slog.Warn("complete task: failed to revoke task tokens", "task_id", uuidToString(task.ID), "error", err)
	}

	slog.Info("task completed", "task_id", taskID, "agent_id", uuidToString(task.AgentID))
	writeJSON(w, http.StatusOK, taskToResponse(*task, workspaceID))
}

func (h *Handler) emitIssueExecutedOnFirstCompletion(r *http.Request, task *db.AgentTaskQueue) {
	if task == nil {
		return
	}
	marked, err := h.Queries.MarkIssueFirstExecuted(r.Context(), task.IssueID)
	if err != nil {
		if !isNotFound(err) {
			slog.Warn("analytics: mark issue first-executed failed", "issue_id", uuidToString(task.IssueID), "error", err)
		}
		return
	}
	var durationMS int64
	if task.StartedAt.Valid && task.CompletedAt.Valid {
		durationMS = task.CompletedAt.Time.Sub(task.StartedAt.Time).Milliseconds()
	}
	taskContext := h.TaskService.AnalyticsContextForTask(r.Context(), *task)

	distinct := uuidToString(marked.CreatorID)
	if marked.CreatorType == "agent" {
		distinct = "agent:" + distinct
	}
	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.IssueExecuted(
		distinct,
		uuidToString(marked.WorkspaceID),
		uuidToString(marked.ID),
		uuidToString(task.ID),
		uuidToString(task.AgentID),
		taskContext.Source,
		taskContext.RuntimeMode,
		taskContext.Provider,
		durationMS,
	))
}

func (h *Handler) reconcileCommentsOnCompletion(ctx context.Context, task *db.AgentTaskQueue) {
	if task == nil || !task.IssueID.Valid || !task.AgentID.Valid || !task.CreatedAt.Valid {
		return
	}
	plannedCommentIDs := append([]pgtype.UUID{}, task.CoalescedCommentIds...)
	if task.TriggerCommentID.Valid {
		plannedCommentIDs = append(plannedCommentIDs, task.TriggerCommentID)
	}
	comments, err := h.Queries.ListReconcilableCommentsForIssueSince(ctx, db.ListReconcilableCommentsForIssueSinceParams{
		IssueID:           task.IssueID,
		Since:             task.CreatedAt,
		PlannedCommentIds: plannedCommentIDs,
	})
	if err != nil {
		slog.Warn("reconcile comments on completion: list comments failed",
			"issue_id", uuidToString(task.IssueID), "task_id", uuidToString(task.ID), "error", err)
		return
	}
	if len(comments) == 0 {
		return
	}

	delivered := make(map[string]struct{}, len(task.DeliveredCommentIds))
	for _, id := range task.DeliveredCommentIds {
		if id.Valid {
			delivered[uuidToString(id)] = struct{}{}
		}
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		slog.Warn("reconcile comments on completion: load issue failed",
			"issue_id", uuidToString(task.IssueID), "error", err)
		return
	}
	agentID := uuidToString(task.AgentID)
	scheduled := 0
	for i := range comments {
		c := comments[i]
		if _, ok := delivered[uuidToString(c.ID)]; ok {

			continue
		}
		if isNoteComment(c.Content) {
			continue
		}
		var parentComment *db.Comment
		if c.ParentID.Valid {

			if parent, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
				ID:          c.ParentID,
				WorkspaceID: issue.WorkspaceID,
			}); err == nil {
				parentComment = &parent
			}
		}

		actorType := c.AuthorType
		actorID := uuidToString(c.AuthorID)
		originator := scopedInvokeAuthority(actorID)
		var delegationAuthority string
		if actorType != "member" {
			originator = scopedInvokeAuthority(uuidToString(h.TaskService.ResolveOriginatorFromTriggerComment(ctx, issue.WorkspaceID, c.ID)))

			delegationAuthority = h.autopilotDelegationAuthorityFromComment(ctx, issue, c)
		}
		triggers, _ := h.computeCommentAgentTriggers(ctx, issue, c.Content, parentComment, actorType, actorID, commentTriggerComputeOptions{
			ExcludeTriggerCommentID:            c.ID,
			Originator:                         originator,
			AutopilotDelegationAuthorityUserID: delegationAuthority,
		})

		if actorType != "member" {
			triggers = keepExplicitMentionTriggers(triggers)
		}
		scoped := make([]commentAgentTrigger, 0, 1)
		for _, trigger := range triggers {
			if uuidToString(trigger.Agent.ID) == agentID {
				scoped = append(scoped, trigger)
			}
		}
		if len(scoped) == 0 {
			continue
		}

		if res := h.enqueueCommentAgentTriggers(ctx, issue, c.ID, scoped)[agentID]; res.status == DispatchBlocked {
			headSha := h.TaskService.ResolveIssueReviewSHAParam(ctx, task.IssueID)
			if h.propagateUncoveredCommentObligation(ctx, issue, scoped[0], c.ID, headSha) {
				slog.Info("reconcile comments on completion: replay blocked, obligation handed to the active task",
					"issue_id", uuidToString(task.IssueID), "agent_id", agentID, "comment_id", uuidToString(c.ID))
			} else {

				slog.Error("reconcile comments on completion: replay blocked and obligation could not be handed off; comment needs a durable obligation record",
					"issue_id", uuidToString(task.IssueID), "agent_id", agentID, "comment_id", uuidToString(c.ID),
					"reason", res.reason)
			}
		}
		scheduled++
	}
	if scheduled > 0 {
		slog.Info("reconcile comments on completion: scheduled follow-up",
			"issue_id", uuidToString(task.IssueID),
			"completed_task_id", uuidToString(task.ID),
			"agent_id", agentID,
			"undelivered_comments", scheduled)
	}
}

func keepExplicitMentionTriggers(triggers []commentAgentTrigger) []commentAgentTrigger {
	if len(triggers) == 0 {
		return triggers
	}
	filtered := make([]commentAgentTrigger, 0, len(triggers))
	for _, trigger := range triggers {
		switch trigger.Source {
		case commentTriggerSourceMentionAgent, commentTriggerSourceMentionSquadLeader:
			filtered = append(filtered, trigger)
		}
	}
	return filtered
}

func (h *Handler) buildCoalescedCommentData(ctx context.Context, workspaceID pgtype.UUID, ids []pgtype.UUID) []CoalescedCommentData {
	if len(ids) == 0 {
		return nil
	}
	out := make([]CoalescedCommentData, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !id.Valid {
			continue
		}
		idString := uuidToString(id)
		if _, ok := seen[idString]; ok {
			continue
		}
		seen[idString] = struct{}{}

		comment, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
			ID:          id,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			continue
		}
		data := CoalescedCommentData{
			ID:         uuidToString(comment.ID),
			ThreadID:   uuidToString(comment.ID),
			AuthorType: comment.AuthorType,
			Content:    comment.Content,
			CreatedAt:  timestampToString(comment.CreatedAt),
		}
		if comment.ParentID.Valid {
			data.ThreadID = uuidToString(comment.ParentID)
		}
		if comment.AuthorID.Valid {
			switch comment.AuthorType {
			case "agent":
				if a, err := h.Queries.GetAgent(ctx, comment.AuthorID); err == nil {
					data.AuthorName = a.Name
				}
			case "member":
				if u, err := h.Queries.GetUser(ctx, comment.AuthorID); err == nil {
					data.AuthorName = u.Name
				}
			}
		}
		out = append(out, data)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, out[i].CreatedAt)
		right, rightErr := time.Parse(time.RFC3339Nano, out[j].CreatedAt)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.Before(right)
		}
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func commentDataIDs(comments []CoalescedCommentData) []pgtype.UUID {
	if len(comments) == 0 {
		return []pgtype.UUID{}
	}
	ids := make([]pgtype.UUID, 0, len(comments))
	for _, comment := range comments {
		ids = append(ids, parseUUID(comment.ID))
	}
	return ids
}

const maxClaimCommentPayloadBytes = 512 << 10

func selectCommentDelivery(comments []CoalescedCommentData, triggerID string, legacy bool, limit int) []CoalescedCommentData {
	if len(comments) == 0 {
		return nil
	}
	mandatoryID := triggerID
	mandatoryFound := false
	for _, comment := range comments {
		if comment.ID == mandatoryID {
			mandatoryFound = true
			break
		}
	}
	if !mandatoryFound {

		mandatoryID = comments[len(comments)-1].ID
	}

	selected := map[string]struct{}{mandatoryID: {}}
	used := commentDeliveryBaseSize(legacy) + commentDeliveryEntrySize(commentByID(comments, mandatoryID), legacy)
	for _, comment := range comments {
		if comment.ID == mandatoryID {
			continue
		}
		cost := commentDeliveryEntrySize(comment, legacy)
		if limit > 0 && used+cost > limit {
			break
		}
		selected[comment.ID] = struct{}{}
		used += cost
	}

	out := make([]CoalescedCommentData, 0, len(selected))
	for _, comment := range comments {
		if _, ok := selected[comment.ID]; ok {
			out = append(out, comment)
		}
	}
	return out
}

func commentByID(comments []CoalescedCommentData, id string) CoalescedCommentData {
	for _, comment := range comments {
		if comment.ID == id {
			return comment
		}
	}
	return CoalescedCommentData{}
}

func commentDeliveryBaseSize(legacy bool) int {
	if legacy {
		return escapedJSONStringContentSize(legacyCommentBundleHeader)
	}
	return 2
}

func commentDeliveryEntrySize(comment CoalescedCommentData, legacy bool) int {
	if legacy {
		return escapedJSONStringContentSize(formatLegacyCommentEntry(comment))
	}
	encoded, err := json.Marshal(comment)
	if err != nil {
		return maxClaimCommentPayloadBytes + 1
	}
	return len(encoded) + 1
}

func escapedJSONStringContentSize(value string) int {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) < 2 {
		return maxClaimCommentPayloadBytes + 1
	}

	return len(encoded) - 2
}

const legacyCommentBundleHeader = "This run covers multiple distinct issue comments. Address every comment below in chronological order; do not treat this bundle as one rewritten comment.\n"

func formatLegacyCommentBundle(comments []CoalescedCommentData) string {
	if len(comments) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(legacyCommentBundleHeader)
	for _, comment := range comments {
		b.WriteString(formatLegacyCommentEntry(comment))
	}
	return strings.TrimSpace(b.String())
}

func formatLegacyCommentEntry(comment CoalescedCommentData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n--- comment %s", comment.ID)
	if comment.ThreadID != "" {
		fmt.Fprintf(&b, " [thread %s]", comment.ThreadID)
	}
	if comment.AuthorType != "" || comment.AuthorName != "" {
		fmt.Fprintf(&b, " [author %s", comment.AuthorType)
		if comment.AuthorName != "" {
			fmt.Fprintf(&b, ": %s", comment.AuthorName)
		}
		b.WriteString("]")
	}
	if comment.CreatedAt != "" {
		fmt.Fprintf(&b, " [created %s]", comment.CreatedAt)
	}
	b.WriteString(" ---\n")
	b.WriteString(comment.Content)
	fmt.Fprintf(&b, "\n--- end comment %s ---\n", comment.ID)
	return b.String()
}

type TaskUsagePayload struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`

	CostUSDTicks int64 `json:"cost_usd_ticks"`
}

func authoritativeCostTicks(ticks int64) pgtype.Int8 {
	if ticks <= 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: ticks, Valid: true}
}

func (h *Handler) ReportTaskUsage(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}

	var req struct {
		Usage []TaskUsagePayload `json:"usage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var runtimeProvider string
	runtimeProviderLoaded := false
	for _, u := range req.Usage {
		provider := normalizeProvider(u.Provider)
		if provider == "" {
			if !runtimeProviderLoaded {
				if rt, err := h.Queries.GetAgentRuntime(r.Context(), task.RuntimeID); err == nil {
					runtimeProvider = normalizeProvider(rt.Provider)
				} else {
					slog.Warn("load runtime provider for usage backfill failed",
						"task_id", taskID, "runtime_id", uuidToString(task.RuntimeID), "error", err)
				}
				runtimeProviderLoaded = true
			}
			provider = runtimeProvider
		}
		if err := h.Queries.UpsertTaskUsage(r.Context(), db.UpsertTaskUsageParams{
			TaskID:           parseUUID(taskID),
			Provider:         provider,
			Model:            u.Model,
			InputTokens:      u.InputTokens,
			OutputTokens:     u.OutputTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
			CostUsdTicks:     authoritativeCostTicks(u.CostUSDTicks),
		}); err != nil {
			slog.Warn("upsert task usage failed", "task_id", taskID, "model", u.Model, "error", err)
			continue
		}
		h.TaskService.CaptureTaskUsage(r.Context(), task, provider, u.Model, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens, u.CostUSDTicks)

		if totalInput := u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens; totalInput > 0 {
			slog.Info("task prompt-cache usage",
				"task_id", taskID,
				"provider", provider,
				"model", u.Model,
				"input_tokens", u.InputTokens,
				"output_tokens", u.OutputTokens,
				"cache_read_tokens", u.CacheReadTokens,
				"cache_write_tokens", u.CacheWriteTokens,
				"cache_read_ratio", float64(u.CacheReadTokens)/float64(totalInput),
			)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) GetTaskStatus(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": task.Status})
}

type TaskFailRequest struct {
	Error         string `json:"error"`
	SessionID     string `json:"session_id,omitempty"`
	WorkDir       string `json:"work_dir,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`

	SessionRolloutMissing bool `json:"session_rollout_missing,omitempty"`
}

func (h *Handler) FailTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	_, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}

	var req TaskFailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	sanitizeTaskFailRequest(&req)

	task, err := h.TaskService.FailTask(r.Context(), parseUUID(taskID), req.Error, req.SessionID, req.WorkDir, req.FailureReason, req.SessionRolloutMissing)
	if err != nil {

		slog.Warn("fail task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.TaskService.NotifyTaskFinished(*task)

	if err := h.Queries.DeleteTaskTokensByTask(r.Context(), task.ID); err != nil {
		slog.Warn("fail task: failed to revoke task tokens", "task_id", uuidToString(task.ID), "error", err)
	}

	slog.Info("task failed", "task_id", taskID, "agent_id", uuidToString(task.AgentID), "task_error", req.Error, "failure_reason", req.FailureReason)
	writeJSON(w, http.StatusOK, taskToResponse(*task, workspaceID))
}

type TaskMessageRequest struct {
	Seq     int            `json:"seq"`
	Type    string         `json:"type"`
	Tool    string         `json:"tool,omitempty"`
	Content string         `json:"content,omitempty"`
	Input   map[string]any `json:"input,omitempty"`
	Output  string         `json:"output,omitempty"`
}

type TaskMessageBatchRequest struct {
	Messages []TaskMessageRequest `json:"messages"`
}

func (h *Handler) ReportTaskMessages(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	var req TaskMessageBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}

	workspaceID := ""
	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(r.Context(), task.IssueID); err == nil {
			workspaceID = uuidToString(issue.WorkspaceID)
		}
	}
	if workspaceID == "" && task.ChatSessionID.Valid {
		if cs, err := h.Queries.GetChatSession(r.Context(), task.ChatSessionID); err == nil {
			workspaceID = uuidToString(cs.WorkspaceID)
		}
	}

	for _, msg := range req.Messages {

		msg.Content = redact.Text(msg.Content)
		msg.Output = redact.Text(msg.Output)
		msg.Input = redact.InputMap(msg.Input)

		msg.Type = util.SanitizeTextForPostgres(msg.Type)
		msg.Tool = util.SanitizeTextForPostgres(msg.Tool)
		msg.Content = util.SanitizeTextForPostgres(msg.Content)
		msg.Output = util.SanitizeTextForPostgres(msg.Output)
		if msg.Input != nil {
			if cleaned, ok := util.SanitizeJSONForPostgres(msg.Input).(map[string]any); ok {
				msg.Input = cleaned
			}
		}

		var inputJSON []byte
		if msg.Input != nil {
			inputJSON, _ = json.Marshal(msg.Input)
		}
		created, createErr := h.Queries.CreateTaskMessage(r.Context(), db.CreateTaskMessageParams{
			TaskID:  parseUUID(taskID),
			Seq:     int32(msg.Seq),
			Type:    msg.Type,
			Tool:    pgtype.Text{String: msg.Tool, Valid: msg.Tool != ""},
			Content: pgtype.Text{String: msg.Content, Valid: msg.Content != ""},
			Input:   inputJSON,
			Output:  pgtype.Text{String: msg.Output, Valid: msg.Output != ""},
		})
		if createErr != nil {
			slog.Error("failed to create task message", "task_id", taskID, "seq", msg.Seq, "error", createErr)
			writeError(w, http.StatusInternalServerError, "failed to persist task message")
			return
		}

		if workspaceID != "" {
			h.publishTask(protocol.EventTaskMessage, workspaceID, "system", "", taskID,
				taskMessageToPayload(created, taskID, uuidToString(task.IssueID)))
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AckTaskCancelled(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}
	h.TaskService.FinalizeDeferredCancelledChat(r.Context(), task.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func taskMessageToPayload(m db.TaskMessage, taskID, issueID string) protocol.TaskMessagePayload {
	var input map[string]any
	if m.Input != nil {
		json.Unmarshal(m.Input, &input)
	}
	createdAt := ""
	if m.CreatedAt.Valid {
		createdAt = m.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return protocol.TaskMessagePayload{
		TaskID:    taskID,
		IssueID:   issueID,
		Seq:       int(m.Seq),
		Type:      m.Type,
		Tool:      m.Tool.String,
		Content:   m.Content.String,
		Input:     input,
		Output:    m.Output.String,
		CreatedAt: createdAt,
	}
}

func (h *Handler) ListTaskMessages(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}

	var (
		messages []db.TaskMessage
		err      error
	)
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		sinceSeq, parseErr := strconv.Atoi(sinceStr)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid since parameter")
			return
		}
		messages, err = h.Queries.ListTaskMessagesSince(r.Context(), db.ListTaskMessagesSinceParams{
			TaskID: parseUUID(taskID),
			Seq:    int32(sinceSeq),
		})
	} else {
		messages, err = h.Queries.ListTaskMessages(r.Context(), parseUUID(taskID))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list task messages")
		return
	}

	issueID := uuidToString(task.IssueID)

	resp := make([]protocol.TaskMessagePayload, len(messages))
	for i, m := range messages {
		resp[i] = taskMessageToPayload(m, taskID, issueID)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetActiveTaskForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	tasks, err := h.Queries.ListActiveTasksByIssue(r.Context(), issue.ID)
	if err != nil {
		tasks = nil
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToResponse(t, workspaceID)
	}

	h.hydrateTaskAttributions(r.Context(), attributionsOf(resp))

	writeJSON(w, http.StatusOK, map[string]any{"tasks": resp})
}

func (h *Handler) CancelTask(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	taskID := chi.URLParam(r, "taskId")
	existing, err := h.Queries.GetAgentTask(r.Context(), parseUUID(taskID))
	if err != nil || uuidToString(existing.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	task, err := h.TaskService.CancelTask(r.Context(), existing.ID)
	if err != nil {
		slog.Warn("cancel task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("task cancelled by user", "task_id", taskID, "issue_id", uuidToString(task.IssueID))
	resp := taskToResponse(*task, uuidToString(issue.WorkspaceID))

	h.hydrateTaskAttributions(r.Context(), []*TaskAttribution{resp.Attribution})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListTasksByIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	tasks, err := h.Queries.ListTasksByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToResponse(t, workspaceID)
	}

	h.hydrateTaskAttributions(r.Context(), attributionsOf(resp))

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListTaskMessagesByUser(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task_id")
	if !ok {
		return
	}

	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" || wsID != middleware.WorkspaceIDFromContext(r.Context()) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	var (
		messages []db.TaskMessage
		queryErr error
	)
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		sinceSeq, parseErr := strconv.Atoi(sinceStr)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid since parameter")
			return
		}
		messages, queryErr = h.Queries.ListTaskMessagesSince(r.Context(), db.ListTaskMessagesSinceParams{
			TaskID: taskUUID,
			Seq:    int32(sinceSeq),
		})
	} else {
		messages, queryErr = h.Queries.ListTaskMessages(r.Context(), taskUUID)
	}
	if queryErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to list task messages")
		return
	}

	issueID := uuidToString(task.IssueID)

	resp := make([]protocol.TaskMessagePayload, len(messages))
	for i, m := range messages {
		resp[i] = taskMessageToPayload(m, taskID, issueID)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetIssueUsage(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	row, err := h.Queries.GetIssueUsageSummary(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get issue usage")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total_input_tokens":       row.TotalInputTokens,
		"total_output_tokens":      row.TotalOutputTokens,
		"total_cache_read_tokens":  row.TotalCacheReadTokens,
		"total_cache_write_tokens": row.TotalCacheWriteTokens,

		"cost_usd_ticks":              row.TotalCostUsdTicks,
		"uncosted_input_tokens":       row.UncostedInputTokens,
		"uncosted_output_tokens":      row.UncostedOutputTokens,
		"uncosted_cache_read_tokens":  row.UncostedCacheReadTokens,
		"uncosted_cache_write_tokens": row.UncostedCacheWriteTokens,
		"task_count":                  row.TaskCount,
	})
}

const (
	maxIssueGCBatchSize      = 500
	maxIssueGCBatchBodyBytes = 64 << 10
)

type batchIssueGCCheckRequest struct {
	IssueIDs []string `json:"issue_ids"`
}

type batchIssueGCCheckItem struct {
	ID        string     `json:"id"`
	Found     bool       `json:"found"`
	Status    string     `json:"status,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

func (h *Handler) BatchIssueGCCheck(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceId")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxIssueGCBatchBodyBytes)
	var req batchIssueGCCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IssueIDs) > maxIssueGCBatchSize {
		writeError(w, http.StatusBadRequest, "too many issue_ids")
		return
	}

	parsedIDs := make([]pgtype.UUID, 0, len(req.IssueIDs))
	canonicalIDs := make([]string, 0, len(req.IssueIDs))
	for _, issueID := range req.IssueIDs {
		parsedID, err := util.ParseUUID(issueID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid issue_id")
			return
		}
		parsedIDs = append(parsedIDs, parsedID)
		canonicalIDs = append(canonicalIDs, uuidToString(parsedID))
	}

	rows := make(map[string]db.ListIssueGCStatusesRow, len(parsedIDs))
	if len(parsedIDs) > 0 {
		result, err := h.Queries.ListIssueGCStatuses(r.Context(), db.ListIssueGCStatusesParams{
			WorkspaceID: workspaceUUID,
			IssueIds:    parsedIDs,
		})
		if err != nil {
			slog.Warn("list issue GC statuses failed", "workspace_id", workspaceID, "count", len(parsedIDs), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to check issues")
			return
		}
		for _, row := range result {
			rows[uuidToString(row.ID)] = row
		}
	}

	items := make([]batchIssueGCCheckItem, 0, len(req.IssueIDs))
	for i, issueID := range req.IssueIDs {
		row, found := rows[canonicalIDs[i]]
		item := batchIssueGCCheckItem{ID: issueID, Found: found}
		if found {
			item.Status = row.Status
			updatedAt := row.UpdatedAt.Time
			item.UpdatedAt = &updatedAt
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": items})
}

func (h *Handler) GetIssueGCCheck(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueId")
	issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue_id")
	if !ok {
		return
	}
	issue, err := h.Queries.GetIssueGCStatus(r.Context(), issueUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if !h.requireDaemonWorkspaceAccess(w, r, uuidToString(issue.WorkspaceID)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     issue.Status,
		"updated_at": issue.UpdatedAt.Time,
	})
}

func (h *Handler) GetChatSessionGCCheck(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	sessionUUID, ok := parseUUIDOrBadRequest(w, sessionID, "session_id")
	if !ok {
		return
	}
	session, err := h.Queries.GetChatSession(r.Context(), sessionUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return
	}
	if !h.requireDaemonWorkspaceAccess(w, r, uuidToString(session.WorkspaceID)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     session.Status,
		"updated_at": session.UpdatedAt.Time,
	})
}

func (h *Handler) GetAutopilotRunGCCheck(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	runUUID, ok := parseUUIDOrBadRequest(w, runID, "run_id")
	if !ok {
		return
	}
	run, err := h.Queries.GetAutopilotRun(r.Context(), runUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot run not found")
		return
	}
	autopilot, err := h.Queries.GetAutopilot(r.Context(), run.AutopilotID)
	if err != nil {

		writeError(w, http.StatusNotFound, "autopilot run not found")
		return
	}
	if !h.requireDaemonWorkspaceAccess(w, r, uuidToString(autopilot.WorkspaceID)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       run.Status,
		"completed_at": run.CompletedAt.Time,
	})
}

func (h *Handler) GetTaskGCCheck(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       task.Status,
		"completed_at": task.CompletedAt.Time,
	})
}
