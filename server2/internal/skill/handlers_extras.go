// handlers_extras.go — вспомогательные файлы навыка и его метки в одном
// файле: обе группы — маленькие CRUD-обвязки вокруг Store, разделять их на
// два файла на три метода каждый не оправдано.
package skill

import (
	"errors"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// --- вспомогательные файлы навыка -------------------------------------------

func (d *Deps) handleListFiles(w http.ResponseWriter, r *http.Request) {
	m, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	sk, ok := d.loadSkill(w, r, m.WorkspaceID)
	if !ok {
		return
	}
	files, err := d.Store.ListFiles(r.Context(), sk.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, files)
}

type fileInputRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (d *Deps) handleUpsertFile(w http.ResponseWriter, r *http.Request) {
	m, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	sk, ok := d.loadSkill(w, r, m.WorkspaceID)
	if !ok {
		return
	}
	if !canManage(sk, m) {
		httpapi.Forbidden(w, "only the skill's creator or workspace owner/admin can manage it")
		return
	}
	var req fileInputRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	f, err := d.Store.UpsertFile(r.Context(), sk.ID, req.Path, req.Content)
	switch {
	case errors.Is(err, ErrReservedPath):
		httpapi.BadRequest(w, "SKILL.md cannot be replaced through this endpoint")
		return
	case errors.Is(err, ErrInvalidPath):
		httpapi.BadRequest(w, "invalid file path")
		return
	case err != nil:
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, f)
}

func (d *Deps) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	m, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	sk, ok := d.loadSkill(w, r, m.WorkspaceID)
	if !ok {
		return
	}
	if !canManage(sk, m) {
		httpapi.Forbidden(w, "only the skill's creator or workspace owner/admin can manage it")
		return
	}
	if err := d.Store.DeleteFile(r.Context(), sk.ID, r.PathValue("fileId")); err != nil {
		if errors.Is(err, ErrFileNotFound) {
			httpapi.NotFound(w, "file not found")
			return
		}
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- метки навыка -------------------------------------------------------------

func (d *Deps) resourceLabelsAllowed(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	enabled, err := d.Store.ResourceLabelsEnabled(r.Context(), workspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return false
	}
	if !enabled {
		httpapi.WriteError(w, http.StatusNotFound, "resource labels are not enabled for this workspace", "resource_labels_disabled")
		return false
	}
	return true
}

func (d *Deps) handleListLabels(w http.ResponseWriter, r *http.Request) {
	m, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	if !d.resourceLabelsAllowed(w, r, m.WorkspaceID) {
		return
	}
	sk, ok := d.loadSkill(w, r, m.WorkspaceID)
	if !ok {
		return
	}
	labels, err := d.Store.ListLabels(r.Context(), sk.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"labels": labels})
}

func (d *Deps) handleAttachLabel(w http.ResponseWriter, r *http.Request) {
	m, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	if !d.resourceLabelsAllowed(w, r, m.WorkspaceID) {
		return
	}
	sk, ok := d.loadSkill(w, r, m.WorkspaceID)
	if !ok {
		return
	}
	if !canManage(sk, m) {
		httpapi.Forbidden(w, "only the skill's creator or workspace owner/admin can manage it")
		return
	}
	var req struct {
		LabelID string `json:"label_id"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	labels, err := d.Store.AttachLabel(r.Context(), m.WorkspaceID, sk.ID, req.LabelID)
	if errors.Is(err, ErrLabelNotFound) {
		httpapi.NotFound(w, "label not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"labels": labels})
}

func (d *Deps) handleDetachLabel(w http.ResponseWriter, r *http.Request) {
	m, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	if !d.resourceLabelsAllowed(w, r, m.WorkspaceID) {
		return
	}
	sk, ok := d.loadSkill(w, r, m.WorkspaceID)
	if !ok {
		return
	}
	if !canManage(sk, m) {
		httpapi.Forbidden(w, "only the skill's creator or workspace owner/admin can manage it")
		return
	}
	labels, err := d.Store.DetachLabel(r.Context(), sk.ID, r.PathValue("labelId"))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"labels": labels})
}
