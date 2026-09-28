package tagging

import (
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
)

const maxLabelNameLen = 32

func hasControlChars(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func validLabelName(name string) bool {
	return name != "" && len(name) <= maxLabelNameLen && !hasControlChars(name)
}

// resourceTypeAllowed — resource_type=issue всегда доступен; agent/skill —
// только если в воркспейсе включена фича ресурсных меток (контракт §3).
func (d *Deps) resourceTypeAllowed(w http.ResponseWriter, r *http.Request, workspaceID, resourceType string) bool {
	if resourceType == "issue" {
		return true
	}
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
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	resourceType := r.URL.Query().Get("resource_type")
	if resourceType == "" {
		resourceType = "issue"
	}
	if !d.resourceTypeAllowed(w, r, member.WorkspaceID, resourceType) {
		return
	}
	list, err := d.Store.ListLabels(r.Context(), member.WorkspaceID, resourceType)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Label{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"labels": list, "total": len(list)})
}

type createLabelRequest struct {
	ResourceType string `json:"resource_type"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Color        string `json:"color"`
}

func (d *Deps) handleCreateLabel(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	var req createLabelRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	resourceType := req.ResourceType
	if resourceType == "" {
		resourceType = "issue"
	}
	if !d.resourceTypeAllowed(w, r, member.WorkspaceID, resourceType) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validLabelName(name) {
		httpapi.BadRequest(w, "name must be 1-32 characters without control characters")
		return
	}
	color := normalizeColor(req.Color)
	if !hexColorRe.MatchString(color) {
		httpapi.BadRequest(w, "color must be a 6-digit hex value")
		return
	}
	label, err := d.Store.CreateLabel(r.Context(), CreateLabelParams{
		WorkspaceID: member.WorkspaceID, ResourceType: resourceType, Name: name,
		Description: strings.TrimSpace(req.Description), Color: color,
	})
	if errors.Is(err, ErrNameTaken) {
		httpapi.WriteError(w, http.StatusConflict, "a label with this name already exists", "label_name_taken")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "label:created", Payload: map[string]any{"label": label}})
	}
	httpapi.WriteJSON(w, http.StatusCreated, label)
}

func (d *Deps) handleGetLabel(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	label, err := d.Store.GetLabel(r.Context(), member.WorkspaceID, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "label not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, label)
}

type updateLabelRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
}

func (d *Deps) handleUpdateLabel(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	var req updateLabelRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	patch := UpdateLabelParams{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if !validLabelName(name) {
			httpapi.BadRequest(w, "name must be 1-32 characters without control characters")
			return
		}
		patch.Name = &name
	}
	if req.Description != nil {
		patch.HasDesc = true
		desc := strings.TrimSpace(*req.Description)
		patch.Description = &desc
	}
	if req.Color != nil {
		color := normalizeColor(*req.Color)
		if !hexColorRe.MatchString(color) {
			httpapi.BadRequest(w, "color must be a 6-digit hex value")
			return
		}
		patch.Color = &color
	}
	label, err := d.Store.UpdateLabel(r.Context(), member.WorkspaceID, r.PathValue("id"), patch)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "label not found")
		return
	}
	if errors.Is(err, ErrNameTaken) {
		httpapi.WriteError(w, http.StatusConflict, "a label with this name already exists", "label_name_taken")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "label:updated", Payload: map[string]any{"label": label}})
	}
	httpapi.WriteJSON(w, http.StatusOK, label)
}

func (d *Deps) handleDeleteLabel(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := d.Store.DeleteLabel(r.Context(), member.WorkspaceID, id); errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "label not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "label:deleted", Payload: map[string]any{"label_id": id}})
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- issue <-> label ---------------------------------------------------------

func (d *Deps) ticketExists(w http.ResponseWriter, r *http.Request, workspaceID, issueID string) bool {
	exists, err := d.Store.db.RowExists(r.Context(), `SELECT EXISTS(SELECT 1 FROM tickets WHERE workspace_id = $1 AND id = $2)`,
		workspaceID, issueID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return false
	}
	if !exists {
		httpapi.NotFound(w, "issue not found")
		return false
	}
	return true
}

func (d *Deps) handleListIssueLabels(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	issueID := r.PathValue("id")
	if !d.ticketExists(w, r, member.WorkspaceID, issueID) {
		return
	}
	list, err := d.Store.ListIssueLabels(r.Context(), member.WorkspaceID, issueID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Label{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"labels": list})
}

type attachLabelRequest struct {
	LabelID string `json:"label_id"`
}

func (d *Deps) handleAttachIssueLabel(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	issueID := r.PathValue("id")
	if !d.ticketExists(w, r, member.WorkspaceID, issueID) {
		return
	}
	var req attachLabelRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.LabelID == "" {
		httpapi.BadRequest(w, "label_id is required")
		return
	}
	if err := d.Store.AttachIssueLabel(r.Context(), member.WorkspaceID, issueID, req.LabelID); errors.Is(err, ErrNotFound) {
		httpapi.WriteError(w, http.StatusNotFound, "label not found", "label_not_found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.respondIssueLabels(w, r, member.WorkspaceID, issueID, "issue_labels:changed")
}

func (d *Deps) handleDetachIssueLabel(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	issueID := r.PathValue("id")
	if !d.ticketExists(w, r, member.WorkspaceID, issueID) {
		return
	}
	labelID := r.PathValue("labelId")
	if err := d.Store.DetachIssueLabel(r.Context(), member.WorkspaceID, issueID, labelID); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.respondIssueLabels(w, r, member.WorkspaceID, issueID, "issue_labels:changed")
}

func (d *Deps) respondIssueLabels(w http.ResponseWriter, r *http.Request, workspaceID, issueID, eventName string) {
	list, err := d.Store.ListIssueLabels(r.Context(), workspaceID, issueID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Label{}
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: eventName, Payload: map[string]any{"issue_id": issueID, "labels": list}})
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"labels": list})
}
