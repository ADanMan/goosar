package skill

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/wsctx"
)

// canManage — "владелец навыка" (contract §3): создатель, либо owner/admin
// воркспейса.
func canManage(sk Skill, m wsctx.Member) bool {
	if httpapi.RoleAtLeast(m.Role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		return true
	}
	return sk.CreatedBy != nil && *sk.CreatedBy == m.UserID
}

func (d *Deps) handleList(w http.ResponseWriter, r *http.Request) {
	m, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	list, err := d.Store.List(r.Context(), m.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, sk := range list {
		out = append(out, sk.Summary())
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

type createRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Content     string          `json:"content"`
	Config      json.RawMessage `json:"config"`
	Files       []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	} `json:"files"`
}

func toFileInputs(in []struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}) []FileInput {
	out := make([]FileInput, len(in))
	for i, f := range in {
		out[i] = FileInput{Path: f.Path, Content: f.Content}
	}
	return out
}

func (d *Deps) handleCreate(w http.ResponseWriter, r *http.Request) {
	m, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	var req createRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.Name == "" {
		httpapi.BadRequest(w, "name is required")
		return
	}
	for _, f := range req.Files {
		if err := ValidatePath(f.Path); err != nil {
			httpapi.BadRequest(w, "invalid file path: "+f.Path)
			return
		}
	}
	sk, files, err := d.Store.Create(r.Context(), CreateParams{
		WorkspaceID: m.WorkspaceID, Name: req.Name, Summary: req.Description, Content: req.Content,
		Config: req.Config, CreatedBy: m.UserID, Files: toFileInputs(req.Files),
	})
	if errors.Is(err, ErrNameTaken) {
		httpapi.WriteError(w, http.StatusConflict, "a skill with this name already exists", "skill_name_taken")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	view := sk.WithFiles(files)
	d.notify(m.WorkspaceID, "skill:created", map[string]any{"skill": view})
	httpapi.WriteJSON(w, http.StatusCreated, view)
}

func (d *Deps) loadSkill(w http.ResponseWriter, r *http.Request, workspaceID string) (Skill, bool) {
	sk, err := d.Store.Get(r.Context(), workspaceID, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "skill not found")
		return Skill{}, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Skill{}, false
	}
	return sk, true
}

func (d *Deps) handleGet(w http.ResponseWriter, r *http.Request) {
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
	httpapi.WriteJSON(w, http.StatusOK, sk.WithFiles(files))
}

type updateRequest struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Content     *string         `json:"content"`
	Config      json.RawMessage `json:"config"`
	Files       *[]struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	} `json:"files"`
}

func (d *Deps) handleUpdate(w http.ResponseWriter, r *http.Request) {
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
	var req updateRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	p := UpdateParams{Name: req.Name, Summary: req.Description, Content: req.Content, Config: req.Config}
	if req.Files != nil {
		p.ReplaceFiles = true
		for _, f := range *req.Files {
			if f.Path != ReservedFilePath {
				if err := ValidatePath(f.Path); err != nil {
					httpapi.BadRequest(w, "invalid file path: "+f.Path)
					return
				}
			}
			p.Files = append(p.Files, FileInput{Path: f.Path, Content: f.Content})
		}
	}
	if err := d.Store.Update(r.Context(), m.WorkspaceID, sk.ID, p); err != nil {
		if errors.Is(err, ErrNameTaken) {
			httpapi.WriteError(w, http.StatusConflict, "a skill with this name already exists", "skill_name_taken")
			return
		}
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	updated, ok := d.loadSkill(w, r, m.WorkspaceID)
	if !ok {
		return
	}
	files, err := d.Store.ListFiles(r.Context(), updated.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	view := updated.WithFiles(files)
	d.notify(m.WorkspaceID, "skill:updated", map[string]any{"skill": view})
	httpapi.WriteJSON(w, http.StatusOK, view)
}

func (d *Deps) handleDelete(w http.ResponseWriter, r *http.Request) {
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
	if err := d.Store.Delete(r.Context(), m.WorkspaceID, sk.ID); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(m.WorkspaceID, "skill:deleted", map[string]any{"skill_id": sk.ID})
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) handleSearch(w http.ResponseWriter, r *http.Request) {
	_, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		httpapi.BadRequest(w, "q is required")
		return
	}
	disabled, err := d.Store.SearchDisabled(r.Context())
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if disabled {
		httpapi.WriteJSON(w, http.StatusForbidden, map[string]any{"code": "source_disabled", "error": "skill search is disabled on this deployment"})
		return
	}
	results, err := Search(r.Context(), q)
	if errors.Is(err, ErrUpstreamUnavailable) {
		httpapi.WriteJSON(w, http.StatusBadGateway, map[string]any{"code": "upstream_unavailable", "error": "external skill catalog is unavailable"})
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if results == nil {
		results = []SearchCandidate{}
	}
	httpapi.WriteJSON(w, http.StatusOK, results)
}
