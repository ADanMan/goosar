package daemon

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/runtime"
)

type registerRuntimeInput struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Version   string `json:"version"`
	Status    string `json:"status"`
	ProfileID string `json:"profile_id"`
}

type failedProfileInput struct {
	ProfileID   string `json:"profile_id"`
	CommandName string `json:"command_name"`
	Reason      string `json:"reason"`
}

type registerRequest struct {
	WorkspaceID     string                 `json:"workspace_id"`
	DaemonID        string                 `json:"daemon_id"`
	LegacyDaemonIDs []string               `json:"legacy_daemon_ids"`
	DeviceName      string                 `json:"device_name"`
	CLIVersion      string                 `json:"cli_version"`
	LaunchedBy      string                 `json:"launched_by"`
	Runtimes        []registerRuntimeInput `json:"runtimes"`
	FailedProfiles  []failedProfileInput   `json:"failed_profiles"`
}

// handleRegister — POST /api/daemon/register.
func (d *Deps) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.WorkspaceID == "" || req.DaemonID == "" {
		httpapi.BadRequest(w, "workspace_id and daemon_id are required")
		return
	}
	actor, ok := d.requireWorkspaceAccess(w, r, req.WorkspaceID)
	if !ok {
		return
	}
	if len(req.Runtimes) == 0 && len(req.FailedProfiles) == 0 {
		httpapi.BadRequest(w, "at least one of runtimes/failed_profiles must be non-empty")
		return
	}
	ownerID := ""
	if actor.IsHuman {
		ownerID = actor.UserID
	}
	entries := make([]runtime.RegisterEntry, len(req.Runtimes))
	for i, rt := range req.Runtimes {
		entries[i] = runtime.RegisterEntry{Name: rt.Name, Type: rt.Type, Version: rt.Version, Status: rt.Status, ProfileID: rt.ProfileID}
	}
	registered, err := d.Runtime.Store.Register(r.Context(), req.WorkspaceID, req.DaemonID, ownerID, req.DeviceName, entries)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	repos, reposVersion, settings, err := d.workspaceRepos(r.Context(), req.WorkspaceID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	d.publishRegisterEvent(req.WorkspaceID, map[string]any{"action": "register", "daemon_id": req.DaemonID})
	for _, ex := range registered {
		d.notifyTaskAvailable(ex.ID, "") // рантайм подключился — если очередь уже непуста, дать шанс сразу опросить
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"runtimes": runtimeDTOsFor(registered), "repos": repos, "repos_version": reposVersion, "settings": settings,
	})
}

// handleDeregister — POST /api/daemon/deregister.
func (d *Deps) handleDeregister(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	var req struct {
		RuntimeIDs []string `json:"runtime_ids"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || len(req.RuntimeIDs) == 0 {
		httpapi.BadRequest(w, "runtime_ids is required")
		return
	}
	allowed := make([]string, 0, len(req.RuntimeIDs))
	touched := map[string]bool{}
	for _, id := range req.RuntimeIDs {
		ex, err := d.Runtime.Store.Get(r.Context(), id)
		if err != nil {
			continue // неизвестный id — молча пропускается (контракт)
		}
		ok, err := d.hasWorkspaceAccess(r.Context(), actor, ex.WorkspaceID)
		if err != nil || !ok {
			continue // чужой рантайм — молча пропускается
		}
		allowed = append(allowed, id)
		touched[ex.WorkspaceID] = true
	}
	if len(allowed) > 0 {
		if _, err := d.Runtime.Store.Deregister(r.Context(), allowed); err != nil {
			d.internalErr(w, err)
			return
		}
	}
	for wsID := range touched {
		d.publishRegisterEvent(wsID, map[string]any{"action": "deregister"})
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleHeartbeat — POST /api/daemon/heartbeat (используется также
// /api/daemon/ws, см. ws.go, чтобы не дублировать логику ack).
func (d *Deps) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	var req struct {
		RuntimeID           string `json:"runtime_id"`
		SupportsBatchImport bool   `json:"supports_batch_import"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.RuntimeID == "" {
		httpapi.BadRequest(w, "runtime_id is required")
		return
	}
	ex, err := d.Runtime.Store.Get(r.Context(), req.RuntimeID)
	if err == runtime.ErrNotFound {
		httpapi.NotFound(w, "runtime not found")
		return
	}
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if allowed, err := d.hasWorkspaceAccess(r.Context(), actor, ex.WorkspaceID); err != nil || !allowed {
		httpapi.Forbidden(w, "not a member of this workspace")
		return
	}
	ack, err := d.heartbeatAck(r.Context(), req.RuntimeID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, ack)
}

// heartbeatAck — тело DaemonHeartbeatAck, общее для HTTP и daemon:heartbeat
// по WS (contract §2.2).
func (d *Deps) heartbeatAck(ctx context.Context, runtimeID string) (map[string]any, error) {
	if _, ok, err := d.Runtime.Store.Heartbeat(ctx, runtimeID); err != nil {
		return nil, err
	} else if !ok {
		return map[string]any{"status": "runtime_gone"}, nil
	}
	ack := map[string]any{"status": "ok"}
	if p, ok, err := d.Runtime.Store.PendingByKind(ctx, runtimeID, runtime.ProbeUpdate); err != nil {
		return nil, err
	} else if ok {
		ack["pending_update"] = pendingUpdatePayload(p)
	}
	if p, ok, err := d.Runtime.Store.PendingByKind(ctx, runtimeID, runtime.ProbeModelList); err != nil {
		return nil, err
	} else if ok {
		ack["pending_model_list"] = map[string]any{"id": p.ID}
	}
	if p, ok, err := d.Runtime.Store.PendingByKind(ctx, runtimeID, runtime.ProbeLocalSkills); err != nil {
		return nil, err
	} else if ok {
		ack["pending_local_skills"] = map[string]any{"id": p.ID}
	}
	imports, err := d.Runtime.Store.AllPendingImports(ctx, runtimeID)
	if err != nil {
		return nil, err
	}
	if len(imports) > 0 {
		first := imports[0]
		ack["pending_local_skill_import"] = map[string]any{"id": first.ID, "skill_key": skillKeyOf(first)}
		list := make([]map[string]any, len(imports))
		for i, p := range imports {
			list[i] = map[string]any{"id": p.ID, "skill_key": skillKeyOf(p)}
		}
		ack["pending_local_skill_imports"] = list
	}
	return ack, nil
}

func pendingUpdatePayload(p runtime.Probe) map[string]any {
	out := map[string]any{"id": p.ID}
	var req struct {
		TargetVersion string `json:"target_version"`
	}
	_ = json.Unmarshal(p.Request, &req)
	out["target_version"] = req.TargetVersion
	return out
}

func skillKeyOf(p runtime.Probe) string {
	var req struct {
		SkillKey string `json:"skill_key"`
	}
	_ = json.Unmarshal(p.Request, &req)
	return req.SkillKey
}

func runtimeDTOsFor(list []runtime.Executor) []map[string]any {
	out := make([]map[string]any, len(list))
	for i, ex := range list {
		out[i] = map[string]any{
			"id": ex.ID, "workspace_id": ex.WorkspaceID, "daemon_id": ex.DaemonID, "name": ex.Title,
			"custom_name": ex.CustomTitle, "runtime_mode": ex.Mode, "provider": ex.Provider,
			"status": ex.Status(), "device_info": ex.DeviceInfo, "metadata": ex.Metadata,
			"owner_id": ex.OwnerID, "visibility": ex.Visibility, "profile_id": ex.ProfileID,
			"last_seen_at": ex.LastSeenAt, "created_at": ex.CreatedAt, "updated_at": ex.UpdatedAt,
		}
	}
	return out
}
