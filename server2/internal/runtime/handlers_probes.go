// handlers_probes.go — асинхронный паттерн "заявка → опрос" (contract §5):
// update/models/local-skills/local-skills-import, HTTP-часть "создать
// заявку"/"опросить статус"; сама заявка и её таймауты — probes.go.
package runtime

import (
	"net/http"
	"strings"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

func (d *Deps) handleInitiateUpdate(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	if !isOwnerOrManager(acc) {
		httpapi.Forbidden(w, "not the runtime owner or workspace owner/admin")
		return
	}
	var req struct {
		TargetVersion string `json:"target_version"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.TargetVersion) == "" {
		httpapi.BadRequest(w, "target_version is required")
		return
	}
	p, created, err := d.Store.CreatePendingUpdate(r.Context(), acc.Runtime.ID, req.TargetVersion)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if !created {
		httpapi.WriteError(w, http.StatusConflict, "an update request is already pending for this runtime", "")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, probeToJSON(p))
}

func (d *Deps) handleGetUpdate(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	if !isOwnerOrManager(acc) {
		// Контракт также допускает исходного инициатора запроса; probe_request
		// не хранит инициатора отдельной колонкой (см. decisions.md, T-028) —
		// упрощение: только владелец рантайма/owner/admin.
		httpapi.Forbidden(w, "not allowed to view this update request")
		return
	}
	p, err := d.Store.GetProbe(r.Context(), acc.Runtime.ID, r.PathValue("updateId"))
	if err == ErrProbeNotFound {
		httpapi.NotFound(w, "update request not found")
		return
	}
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, probeToJSON(p))
}

func (d *Deps) handleInitiateModelList(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	if acc.Runtime.Status() != "online" {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "runtime is not online", "")
		return
	}
	p, err := d.Store.CreateProbe(r.Context(), acc.Runtime.ID, ProbeModelList, map[string]any{})
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, probeToJSON(p))
}

func (d *Deps) handleGetModelList(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	p, err := d.Store.GetProbe(r.Context(), acc.Runtime.ID, r.PathValue("requestId"))
	if err == ErrProbeNotFound {
		httpapi.NotFound(w, "request not found")
		return
	}
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, probeToJSON(p))
}

func (d *Deps) handleInitiateLocalSkills(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	if acc.Runtime.Status() != "online" {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "runtime is not online", "")
		return
	}
	p, err := d.Store.CreateProbe(r.Context(), acc.Runtime.ID, ProbeLocalSkills, map[string]any{})
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, probeToJSON(p))
}

func (d *Deps) handleGetLocalSkills(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	p, err := d.Store.GetProbe(r.Context(), acc.Runtime.ID, r.PathValue("requestId"))
	if err == ErrProbeNotFound {
		httpapi.NotFound(w, "request not found")
		return
	}
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, probeToJSON(p))
}

func (d *Deps) handleInitiateLocalSkillImport(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	if acc.Runtime.OwnerID == nil || *acc.Runtime.OwnerID != acc.Actor.UserID {
		httpapi.Forbidden(w, "only the runtime owner can import local skills")
		return
	}
	if acc.Runtime.Status() != "online" {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "runtime is not online", "")
		return
	}
	var req struct {
		SkillKey         string `json:"skill_key"`
		Name             string `json:"name"`
		Description      string `json:"description"`
		Action           string `json:"action"`
		TargetSkillID    string `json:"target_skill_id"`
		SupportsConflict bool   `json:"supports_conflict"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.SkillKey) == "" {
		httpapi.BadRequest(w, "skill_key is required")
		return
	}
	if req.Action != "" && req.Action != "overwrite" {
		httpapi.BadRequest(w, "action must be empty or overwrite")
		return
	}
	if req.Action == "overwrite" && req.TargetSkillID == "" {
		httpapi.BadRequest(w, "target_skill_id is required for action=overwrite")
		return
	}
	if req.Action == "overwrite" {
		req.SupportsConflict = true
	}
	p, err := d.Store.CreateProbe(r.Context(), acc.Runtime.ID, ProbeLocalSkillImport, req)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, probeToJSON(p))
}

func (d *Deps) handleGetLocalSkillImport(w http.ResponseWriter, r *http.Request) {
	acc, ok := d.requireRuntimeMember(w, r)
	if !ok {
		return
	}
	if acc.Runtime.OwnerID == nil || *acc.Runtime.OwnerID != acc.Actor.UserID {
		if !isOwnerOrManager(acc) {
			httpapi.Forbidden(w, "only the runtime owner can view this import request")
			return
		}
	}
	p, err := d.Store.GetProbe(r.Context(), acc.Runtime.ID, r.PathValue("requestId"))
	if err == ErrProbeNotFound {
		httpapi.NotFound(w, "request not found")
		return
	}
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, probeToJSON(p))
}
