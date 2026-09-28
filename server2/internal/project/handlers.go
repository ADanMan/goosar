package project

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
)

var validStatuses = map[string]bool{"planned": true, "in_progress": true, "paused": true, "completed": true, "cancelled": true}
var validPriorities = map[string]bool{"urgent": true, "high": true, "medium": true, "low": true, "none": true}
var validLeadTypes = map[string]bool{"member": true, "agent": true}
var validResourceTypes = map[string]bool{"github_repo": true, "local_directory": true}

func parseDate(s *string) (*time.Time, bool) {
	if s == nil {
		return nil, true
	}
	t, err := time.Parse("2006-01-02", *s)
	if err != nil {
		return nil, false
	}
	return &t, true
}

func formatDate(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

// projectView — Project, сериализуемый с датами в формате date (не date-time),
// как того требует контракт (schemas.Project.start_date/due_date: format date).
type projectView struct {
	Project
	StartDate *string `json:"start_date"`
	DueDate   *string `json:"due_date"`
}

func toView(p Project) projectView {
	return projectView{Project: p, StartDate: formatDate(p.StartDate), DueDate: formatDate(p.DueDate)}
}

type searchView struct {
	projectView
	MatchSource    string  `json:"match_source"`
	MatchedSnippet *string `json:"matched_snippet"`
}

func (d *Deps) handleSearchProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpapi.BadRequest(w, "q is required")
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			httpapi.BadRequest(w, "limit must be a non-negative integer")
			return
		}
		if n > 50 {
			n = 50
		}
		limit = n
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			httpapi.BadRequest(w, "offset must be a non-negative integer")
			return
		}
		offset = n
	}
	includeClosed := r.URL.Query().Get("include_closed") == "true"
	results, total, err := d.Store.SearchProjects(r.Context(), workspaceID, q, limit, offset, includeClosed)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	views := make([]searchView, 0, len(results))
	for _, sr := range results {
		views = append(views, searchView{toView(sr.Project), sr.MatchSource, sr.MatchedSnippet})
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"projects": views, "total": total})
}

func (d *Deps) handleListProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && !validStatuses[status] {
		httpapi.BadRequest(w, "invalid status")
		return
	}
	priority := r.URL.Query().Get("priority")
	if priority != "" && !validPriorities[priority] {
		httpapi.BadRequest(w, "invalid priority")
		return
	}
	list, err := d.Store.ListProjects(r.Context(), workspaceID, ListFilter{Status: status, Priority: priority})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	views := make([]projectView, 0, len(list))
	for _, p := range list {
		views = append(views, toView(p))
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"projects": views, "total": len(views)})
}

type resourceRequest struct {
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        *string         `json:"label"`
	Position     *int            `json:"position"`
}

type createProjectRequest struct {
	Title       string            `json:"title"`
	Description *string           `json:"description"`
	Icon        *string           `json:"icon"`
	Status      *string           `json:"status"`
	Priority    *string           `json:"priority"`
	LeadType    *string           `json:"lead_type"`
	LeadID      *string           `json:"lead_id"`
	StartDate   *string           `json:"start_date"`
	DueDate     *string           `json:"due_date"`
	Resources   []resourceRequest `json:"resources"`
}

// validateResourceRequests проверяет форму resources[] и правило "не более
// одного local_directory на daemon_id в рамках проекта" на уровне самого
// запроса (ещё до вставки в БД — см. описание createProject в контракте).
func validateResourceRequests(reqs []resourceRequest) (string, bool) {
	seenDaemons := map[string]bool{}
	seenRepos := map[string]bool{}
	for _, rr := range reqs {
		if !validResourceTypes[rr.ResourceType] {
			return "resources[].resource_type must be github_repo or local_directory", false
		}
		if len(rr.ResourceRef) == 0 {
			return "resources[].resource_ref is required", false
		}
		switch rr.ResourceType {
		case "local_directory":
			daemon := refDaemonID(rr.ResourceRef)
			if daemon == "" {
				return "resources[].resource_ref.daemon_id is required for local_directory", false
			}
			if seenDaemons[daemon] {
				return "at most one local_directory per daemon_id is allowed in a project", false
			}
			seenDaemons[daemon] = true
		case "github_repo":
			_, key, err := normalizeGithubRef(rr.ResourceRef)
			if err != nil || key == "" {
				return "resources[].resource_ref.url is required for github_repo", false
			}
			if seenRepos[key] {
				return "duplicate github_repo url in resources", false
			}
			seenRepos[key] = true
		}
	}
	return "", true
}

func (d *Deps) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	workspaceID, role, actor, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	_ = role
	var req createProjectRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		httpapi.BadRequest(w, "title must not be empty")
		return
	}
	status := "planned"
	if req.Status != nil {
		if !validStatuses[*req.Status] {
			httpapi.BadRequest(w, "invalid status")
			return
		}
		status = *req.Status
	}
	priority := "none"
	if req.Priority != nil {
		if !validPriorities[*req.Priority] {
			httpapi.BadRequest(w, "invalid priority")
			return
		}
		priority = *req.Priority
	}
	if req.LeadType != nil && !validLeadTypes[*req.LeadType] {
		httpapi.BadRequest(w, "invalid lead_type")
		return
	}
	start, okStart := parseDate(req.StartDate)
	if !okStart {
		httpapi.BadRequest(w, "invalid start_date")
		return
	}
	due, okDue := parseDate(req.DueDate)
	if !okDue {
		httpapi.BadRequest(w, "invalid due_date")
		return
	}
	if msg, ok := validateResourceRequests(req.Resources); !ok {
		httpapi.BadRequest(w, msg)
		return
	}
	resources := make([]CreateResourceParams, 0, len(req.Resources))
	for i, rr := range req.Resources {
		pos := i
		if rr.Position != nil {
			pos = *rr.Position
		}
		resources = append(resources, CreateResourceParams{
			Type: rr.ResourceType, Ref: rr.ResourceRef, Label: rr.Label, Position: pos, CreatedBy: &actor.UserID,
		})
	}
	proj, res, err := d.Store.CreateProject(r.Context(), CreateParams{
		WorkspaceID: workspaceID, Title: title, Description: req.Description, Icon: req.Icon,
		Status: status, Priority: priority, LeadType: req.LeadType, LeadID: req.LeadID,
		StartDate: start, DueDate: due, Resources: resources,
	})
	if err == ErrResourceConflict {
		httpapi.WriteError(w, http.StatusConflict, "one of the resources is already attached", "resource_conflict")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "project:created", Payload: map[string]any{"project": toView(proj)}})
		for _, r := range res {
			d.Publisher.Publish(workspaceID, realtime.Event{Type: "project_resource:created", Payload: map[string]any{"resource": r, "project_id": proj.ID}})
		}
	}
	httpapi.WriteJSON(w, http.StatusCreated, toView(proj))
}

// requireProject резолвит воркспейс, роль и проект из {id} пути.
func (d *Deps) requireProject(w http.ResponseWriter, r *http.Request) (workspaceID string, role httpapi.Role, actor *httpapi.Actor, proj Project, res []Resource, ok bool) {
	workspaceID, role, actor, ok = httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	id := r.PathValue("id")
	p, resources, err := d.Store.GetProject(r.Context(), workspaceID, id)
	if err == ErrNotFound {
		httpapi.NotFound(w, "project not found")
		return workspaceID, role, actor, Project{}, nil, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return workspaceID, role, actor, Project{}, nil, false
	}
	return workspaceID, role, actor, p, resources, true
}

func (d *Deps) handleGetProject(w http.ResponseWriter, r *http.Request) {
	_, _, _, proj, _, ok := d.requireProject(w, r)
	if !ok {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, toView(proj))
}

type updateProjectRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      *string `json:"status"`
	Priority    *string `json:"priority"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
	StartDate   *string `json:"start_date"`
	DueDate     *string `json:"due_date"`
}

func (d *Deps) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, proj, _, ok := d.requireProject(w, r)
	if !ok {
		return
	}
	var req updateProjectRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	patch := UpdatePatch{}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			httpapi.BadRequest(w, "title must not be empty")
			return
		}
		patch.Title = &title
	}
	if req.Description != nil {
		patch.HasDesc, patch.Description = true, req.Description
	}
	if req.Icon != nil {
		patch.HasIcon, patch.Icon = true, req.Icon
	}
	if req.Status != nil {
		if !validStatuses[*req.Status] {
			httpapi.BadRequest(w, "invalid status")
			return
		}
		patch.Status = req.Status
	}
	if req.Priority != nil {
		if !validPriorities[*req.Priority] {
			httpapi.BadRequest(w, "invalid priority")
			return
		}
		patch.Priority = req.Priority
	}
	if req.LeadType != nil || req.LeadID != nil {
		if req.LeadType != nil && !validLeadTypes[*req.LeadType] {
			httpapi.BadRequest(w, "invalid lead_type")
			return
		}
		patch.HasLead, patch.LeadType, patch.LeadID = true, req.LeadType, req.LeadID
	}
	if req.StartDate != nil {
		t, okDate := parseDate(req.StartDate)
		if !okDate {
			httpapi.BadRequest(w, "invalid start_date")
			return
		}
		patch.HasStart, patch.StartDate = true, t
	}
	if req.DueDate != nil {
		t, okDate := parseDate(req.DueDate)
		if !okDate {
			httpapi.BadRequest(w, "invalid due_date")
			return
		}
		patch.HasDue, patch.DueDate = true, t
	}
	updated, _, err := d.Store.UpdateProject(r.Context(), workspaceID, proj.ID, patch)
	if err == ErrNotFound {
		httpapi.NotFound(w, "project not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "project:updated", Payload: map[string]any{"project": toView(updated)}})
	}
	httpapi.WriteJSON(w, http.StatusOK, toView(updated))
}

func (d *Deps) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	workspaceID, role, _, proj, _, ok := d.requireProject(w, r)
	if !ok {
		return
	}
	if !httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "requires owner or admin role")
		return
	}
	if err := d.Store.DeleteProject(r.Context(), workspaceID, proj.ID); err == ErrNotFound {
		httpapi.NotFound(w, "project not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "project:deleted", Payload: map[string]any{"project_id": proj.ID}})
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- resources -----------------------------------------------------------------

func (d *Deps) handleListProjectResources(w http.ResponseWriter, r *http.Request) {
	_, _, _, _, res, ok := d.requireProject(w, r)
	if !ok {
		return
	}
	if res == nil {
		res = []Resource{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"resources": res, "total": len(res)})
}

func (d *Deps) handleCreateProjectResource(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, actor, proj, res, ok := d.requireProject(w, r)
	if !ok {
		return
	}
	var req resourceRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if msg, valid := validateResourceRequests([]resourceRequest{req}); !valid {
		httpapi.BadRequest(w, msg)
		return
	}
	position := len(res)
	if req.Position != nil {
		position = *req.Position
	}
	created, err := d.Store.CreateResource(r.Context(), workspaceID, proj.ID, CreateResourceParams{
		Type: req.ResourceType, Ref: req.ResourceRef, Label: req.Label, Position: position, CreatedBy: &actor.UserID,
	})
	if err == ErrResourceConflict {
		httpapi.WriteError(w, http.StatusConflict, "resource already attached, or a local_directory already exists for this daemon", "resource_conflict")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "project_resource:created", Payload: map[string]any{"resource": created, "project_id": proj.ID}})
	}
	httpapi.WriteJSON(w, http.StatusCreated, created)
}

type updateResourceRequest struct {
	ResourceRef json.RawMessage `json:"resource_ref"`
	Label       *string         `json:"label"`
	Position    *int            `json:"position"`
}

func (d *Deps) handleUpdateProjectResource(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, proj, _, ok := d.requireProject(w, r)
	if !ok {
		return
	}
	var req updateResourceRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	patch := UpdateResourcePatch{Ref: req.ResourceRef, Position: req.Position}
	if req.Label != nil {
		patch.HasLabel, patch.Label = true, req.Label
	}
	updated, err := d.Store.UpdateResource(r.Context(), workspaceID, proj.ID, r.PathValue("resourceId"), patch)
	if err == ErrNotFound {
		httpapi.NotFound(w, "resource not found")
		return
	}
	if err == ErrResourceConflict {
		httpapi.WriteError(w, http.StatusConflict, "resource conflict", "resource_conflict")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "project_resource:updated", Payload: map[string]any{"resource": updated, "project_id": proj.ID}})
	}
	httpapi.WriteJSON(w, http.StatusOK, updated)
}

func (d *Deps) handleDeleteProjectResource(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, proj, _, ok := d.requireProject(w, r)
	if !ok {
		return
	}
	resourceID := r.PathValue("resourceId")
	if err := d.Store.DeleteResource(r.Context(), workspaceID, proj.ID, resourceID); err == ErrNotFound {
		httpapi.NotFound(w, "resource not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "project_resource:deleted", Payload: map[string]any{"project_id": proj.ID, "resource_id": resourceID}})
	}
	w.WriteHeader(http.StatusNoContent)
}
