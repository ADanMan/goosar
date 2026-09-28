package runtime

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// --- GET /api/runtimes -------------------------------------------------------

func (d *Deps) handleList(w http.ResponseWriter, r *http.Request) {
	m, ok := d.WSResolve.RequireMember(w, r)
	if !ok {
		return
	}
	isManager := m.Role == httpapi.RoleOwner || m.Role == httpapi.RoleAdmin
	list, err := d.Store.ListForWorkspace(r.Context(), m.WorkspaceID, r.URL.Query().Get("owner"), m.UserID, isManager)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, toRuntimeDTOs(list))
}

// --- PATCH /api/runtimes/{runtimeId} -----------------------------------------

type updateRuntimeRequest struct {
	Visibility     *string `json:"visibility"`
	CustomName     *string `json:"custom_name"`
	ApplyToMachine bool    `json:"apply_to_machine"`
}

func (d *Deps) handleUpdate(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	if !isOwnerOrManager(acc) {
		httpapi.Forbidden(w, "not the runtime owner or workspace owner/admin")
		return
	}
	var req updateRuntimeRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.Visibility != nil && *req.Visibility != "private" && *req.Visibility != "public" {
		httpapi.BadRequest(w, "visibility must be private or public")
		return
	}
	if req.CustomName != nil && len(*req.CustomName) > 100 {
		httpapi.BadRequest(w, "custom_name too long")
		return
	}
	ex, err := d.Store.UpdateVisibilityName(r.Context(), acc.Runtime.ID, req.Visibility, req.CustomName)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if req.ApplyToMachine && req.CustomName != nil && ex.DaemonID != nil {
		var restrict *string
		if !(acc.Role == httpapi.RoleOwner || acc.Role == httpapi.RoleAdmin) {
			restrict = &acc.Actor.UserID
		}
		_ = d.Store.ApplyNameToMachine(r.Context(), ex.WorkspaceID, *ex.DaemonID, *req.CustomName, restrict)
	}
	d.publishRegisterEvent(ex.WorkspaceID, "update")
	httpapi.WriteJSON(w, http.StatusOK, toRuntimeDTO(ex))
}

// --- DELETE /api/runtimes/{runtimeId} ----------------------------------------

func (d *Deps) handleDelete(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	if !isOwnerOrManager(acc) {
		httpapi.Forbidden(w, "not the runtime owner or workspace owner/admin")
		return
	}
	if acc.Runtime.ProfileID != nil {
		httpapi.WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "runtime is instantiated from a live runtime profile; delete the profile instead",
			"code":  "runtime_profile_instance_delete_unsupported",
		})
		return
	}
	active, err := d.Store.ActiveAgentsOn(r.Context(), acc.Runtime.ID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if len(active) > 0 {
		httpapi.WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "runtime has active (non-archived) agents", "code": "runtime_has_active_agents",
			"active_agents": active,
		})
		return
	}
	hasArchivedLeaderCrew, err := d.Store.ActiveCrewsWithArchivedLeaderOn(r.Context(), acc.Runtime.ID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if hasArchivedLeaderCrew {
		httpapi.WriteError(w, http.StatusConflict, "runtime has active squads led by an archived agent", "")
		return
	}
	if err := d.Store.DeleteCascade(r.Context(), acc.Runtime.ID); err != nil {
		d.internalErr(w, err)
		return
	}
	d.publishRegisterEvent(acc.Runtime.WorkspaceID, "delete")
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- POST /api/runtimes/{runtimeId}/archive-agents-and-delete ----------------

type archiveAndDeleteRequest struct {
	ExpectedActiveAgentIDs []string `json:"expected_active_agent_ids"`
}

func (d *Deps) handleArchiveAgentsAndDelete(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	if !isOwnerOrManager(acc) {
		httpapi.Forbidden(w, "not the runtime owner or workspace owner/admin")
		return
	}
	var req archiveAndDeleteRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	active, err := d.Store.ActiveAgentsOn(r.Context(), acc.Runtime.ID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if !sameIDSet(active, req.ExpectedActiveAgentIDs) {
		httpapi.WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "the set of active agents changed since it was last shown", "code": "runtime_delete_plan_changed",
			"active_agents": active,
		})
		return
	}
	var archived []ActiveAgent
	txErr := d.Store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var err error
		archived, err = d.Store.ArchiveAgents(r.Context(), tx, acc.Runtime.ID, req.ExpectedActiveAgentIDs, acc.Actor.UserID)
		return err
	})
	if txErr != nil {
		d.internalErr(w, err)
		return
	}
	if d.Dispatch != nil {
		_, _ = d.Dispatch.Store.CancelActiveForExecutor(r.Context(), d.DB.Pool, acc.Runtime.ID)
	}
	for _, a := range archived {
		d.publish(acc.Runtime.WorkspaceID, "agent:archived", map[string]any{"agent": a})
	}
	if err := d.Store.DeleteCascade(r.Context(), acc.Runtime.ID); err != nil {
		d.internalErr(w, err)
		return
	}
	d.publishRegisterEvent(acc.Runtime.WorkspaceID, "delete")
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func sameIDSet(active []ActiveAgent, expected []string) bool {
	if len(active) != len(expected) {
		return false
	}
	set := make(map[string]bool, len(expected))
	for _, id := range expected {
		set[id] = true
	}
	for _, a := range active {
		if !set[a.ID] {
			return false
		}
	}
	return true
}

// --- POST /api/runtimes/{runtimeId}/mcp-verified -----------------------------

func (d *Deps) handleMcpVerified(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.requireRuntimeMember(w, r); !ok {
		return
	}
	var req struct {
		Server string `json:"server"`
		Status string `json:"status"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Server) == "" || strings.TrimSpace(req.Status) == "" {
		httpapi.BadRequest(w, "server and status are required")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Usage/activity -----------------------------------------------------------

func (d *Deps) handleUsage(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	rows, err := d.Store.UsageDaily(r.Context(), acc.Runtime.ID, daysParam(r, 90))
	if err != nil {
		d.internalErr(w, err)
		return
	}
	out := make([]usageDTO, len(rows))
	for i, u := range rows {
		dto := fromUsageRow(u.UsageRow)
		dto.RuntimeID = acc.Runtime.ID
		dto.Date = u.Date.Format("2006-01-02")
		out[i] = dto
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleUsageByAgent(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	rows, err := d.Store.UsageByAgent(r.Context(), acc.Runtime.ID, daysParam(r, 30))
	if err != nil {
		d.internalErr(w, err)
		return
	}
	out := make([]usageDTO, len(rows))
	for i, u := range rows {
		dto := fromUsageRow(u.UsageRow)
		dto.AgentID = u.AgentID
		tc := u.TaskCount
		dto.TaskCount = &tc
		out[i] = dto
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleUsageByHour(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	rows, err := d.Store.UsageByHour(r.Context(), acc.Runtime.ID, daysParam(r, 30))
	if err != nil {
		d.internalErr(w, err)
		return
	}
	out := make([]usageDTO, len(rows))
	for i, u := range rows {
		dto := fromUsageRow(u.UsageRow)
		h := u.Hour
		dto.Hour = &h
		tc := u.TaskCount
		dto.TaskCount = &tc
		out[i] = dto
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleActivity(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	rows, err := d.Store.ActivityHeatmap(r.Context(), acc.Runtime.ID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, rows)
}

func daysParam(r *http.Request, def int) int {
	v := r.URL.Query().Get("days")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 365 {
		return def
	}
	return n
}
