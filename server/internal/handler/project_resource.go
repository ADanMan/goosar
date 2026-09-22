package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

type ProjectResourceResponse struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"project_id"`
	WorkspaceID  string          `json:"workspace_id"`
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        *string         `json:"label"`
	Position     int32           `json:"position"`
	CreatedAt    string          `json:"created_at"`
	CreatedBy    *string         `json:"created_by"`
}

func projectResourceToResponse(r db.ProjectResource) ProjectResourceResponse {
	ref := json.RawMessage(r.ResourceRef)
	if len(ref) == 0 {
		ref = json.RawMessage("{}")
	}
	return ProjectResourceResponse{
		ID:           uuidToString(r.ID),
		ProjectID:    uuidToString(r.ProjectID),
		WorkspaceID:  uuidToString(r.WorkspaceID),
		ResourceType: r.ResourceType,
		ResourceRef:  ref,
		Label:        textToPtr(r.Label),
		Position:     r.Position,
		CreatedAt:    timestampToString(r.CreatedAt),
		CreatedBy:    uuidToPtr(r.CreatedBy),
	}
}

type CreateProjectResourceRequest struct {
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        *string         `json:"label"`
	Position     *int32          `json:"position"`
}

type UpdateProjectResourceRequest struct {
	ResourceRef json.RawMessage `json:"resource_ref"`
	Label       *string         `json:"label"`
	Position    *int32          `json:"position"`
}

func validateAndNormalizeResourceRef(resourceType string, ref json.RawMessage) (json.RawMessage, error) {
	if len(ref) == 0 {
		return nil, errors.New("resource_ref is required")
	}
	switch resourceType {
	case "github_repo":
		return validateGithubRepoRef(ref)
	case "local_directory":
		return validateLocalDirectoryRef(ref)
	default:
		return nil, fmt.Errorf("unknown resource_type %q", resourceType)
	}
}

type githubRepoRef struct {
	URL               string `json:"url"`
	DefaultBranchHint string `json:"default_branch_hint,omitempty"`
	Ref               string `json:"ref,omitempty"`
}

func validateGithubRepoRef(ref json.RawMessage) (json.RawMessage, error) {
	var payload githubRepoRef
	if err := json.Unmarshal(ref, &payload); err != nil {
		return nil, fmt.Errorf("invalid github_repo payload: %w", err)
	}
	payload.URL = strings.TrimSpace(payload.URL)
	if payload.URL == "" {
		return nil, errors.New("github_repo: url is required")
	}
	if !isValidGitRepoURL(payload.URL) {
		return nil, errors.New("github_repo: url must be a valid http(s) or ssh git URL")
	}
	payload.DefaultBranchHint = strings.TrimSpace(payload.DefaultBranchHint)
	payload.Ref = strings.TrimSpace(payload.Ref)
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type localDirectoryRef struct {
	LocalPath string `json:"local_path"`
	DaemonID  string `json:"daemon_id"`
	Label     string `json:"label,omitempty"`
}

func validateLocalDirectoryRef(ref json.RawMessage) (json.RawMessage, error) {
	var payload localDirectoryRef
	if err := json.Unmarshal(ref, &payload); err != nil {
		return nil, fmt.Errorf("invalid local_directory payload: %w", err)
	}
	payload.LocalPath = strings.TrimSpace(payload.LocalPath)
	if payload.LocalPath == "" {
		return nil, errors.New("local_directory: local_path is required")
	}
	if !isAbsoluteLocalPath(payload.LocalPath) {
		return nil, errors.New("local_directory: local_path must be an absolute path")
	}
	payload.DaemonID = strings.TrimSpace(payload.DaemonID)
	if payload.DaemonID == "" {
		return nil, errors.New("local_directory: daemon_id is required")
	}
	payload.Label = strings.TrimSpace(payload.Label)
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func isAbsoluteLocalPath(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '/' {
		return true
	}
	if strings.HasPrefix(s, `\\`) {
		return true
	}
	if len(s) >= 3 && isDriveLetter(s[0]) && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	return false
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isValidGitRepoURL(s string) bool {
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		switch u.Scheme {
		case "http", "https", "ssh", "git":
			return true
		}
	}

	if strings.Contains(s, " ") || strings.Contains(s, "://") {
		return false
	}
	colon := strings.Index(s, ":")
	if colon <= 0 || colon == len(s)-1 {
		return false
	}

	at := strings.Index(s, "@")
	if at >= colon {
		return false
	}
	hostStart := 0
	if at >= 0 {
		hostStart = at + 1
	}
	host := s[hostStart:colon]
	path := s[colon+1:]
	if host == "" || path == "" {
		return false
	}
	return true
}

func (h *Handler) loadProjectForResource(w http.ResponseWriter, r *http.Request, projectIDParam string) (db.Project, bool) {
	projectUUID, ok := parseUUIDOrBadRequest(w, projectIDParam, "project id")
	if !ok {
		return db.Project{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return db.Project{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, false
	}
	return project, true
}

func (h *Handler) ListProjectResources(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	resources, err := h.Queries.ListProjectResources(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project resources")
		return
	}
	resp := make([]ProjectResourceResponse, len(resources))
	for i, res := range resources {
		resp[i] = projectResourceToResponse(res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"resources": resp, "total": len(resp)})
}

func (h *Handler) CreateProjectResource(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req CreateProjectResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ResourceType = strings.TrimSpace(req.ResourceType)
	if req.ResourceType == "" {
		writeError(w, http.StatusBadRequest, "resource_type is required")
		return
	}
	normalizedRef, err := validateAndNormalizeResourceRef(req.ResourceType, req.ResourceRef)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if conflict, err := h.findLocalDirectoryConflict(r.Context(), project.ID, req.ResourceType, normalizedRef, pgtype.UUID{}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check existing resources")
		return
	} else if conflict {
		writeError(w, http.StatusConflict, "this daemon already has a local_directory attached to the project; remove it before adding another")
		return
	}

	var label pgtype.Text
	if req.Label != nil && strings.TrimSpace(*req.Label) != "" {
		label = pgtype.Text{String: strings.TrimSpace(*req.Label), Valid: true}
	}
	var position int32
	if req.Position != nil {
		position = *req.Position
	} else {

		count, _ := h.Queries.CountProjectResources(r.Context(), project.ID)
		position = int32(count)
	}

	creator, _ := h.parseUserUUIDOrZero(userID)
	resource, err := h.Queries.CreateProjectResource(r.Context(), db.CreateProjectResourceParams{
		ProjectID:    project.ID,
		WorkspaceID:  project.WorkspaceID,
		ResourceType: req.ResourceType,
		ResourceRef:  normalizedRef,
		Label:        label,
		Position:     position,
		CreatedBy:    creator,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "this resource is already attached to the project")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create project resource")
		return
	}

	resp := projectResourceToResponse(resource)
	h.publish(
		protocol.EventProjectResourceCreated,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{"resource": resp, "project_id": uuidToString(project.ID)},
	)
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateProjectResource(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	resourceUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "resourceId"), "resource id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	existing, err := h.Queries.GetProjectResourceInWorkspace(r.Context(), db.GetProjectResourceInWorkspaceParams{
		ID: resourceUUID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project resource not found")
		return
	}
	if uuidToString(existing.ProjectID) != uuidToString(project.ID) {
		writeError(w, http.StatusNotFound, "project resource not found")
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	nextRef := json.RawMessage(existing.ResourceRef)
	if rawRef, ok := raw["resource_ref"]; ok {
		normalized, err := validateAndNormalizeResourceRef(existing.ResourceType, rawRef)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		nextRef = normalized
	}

	if conflict, err := h.findLocalDirectoryConflict(r.Context(), project.ID, existing.ResourceType, nextRef, existing.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check existing resources")
		return
	} else if conflict {
		writeError(w, http.StatusConflict, "another local_directory on this daemon is already attached to the project")
		return
	}

	nextLabel := existing.Label
	if rawLabel, ok := raw["label"]; ok {
		var labelStr *string
		if err := json.Unmarshal(rawLabel, &labelStr); err != nil {
			writeError(w, http.StatusBadRequest, "label must be a string or null")
			return
		}
		if labelStr == nil || strings.TrimSpace(*labelStr) == "" {
			nextLabel = pgtype.Text{}
		} else {
			nextLabel = pgtype.Text{String: strings.TrimSpace(*labelStr), Valid: true}
		}
	}

	nextPosition := existing.Position
	if rawPos, ok := raw["position"]; ok {
		var pos *int32
		if err := json.Unmarshal(rawPos, &pos); err != nil {
			writeError(w, http.StatusBadRequest, "position must be an integer")
			return
		}
		if pos != nil {
			nextPosition = *pos
		}
	}

	updated, err := h.Queries.UpdateProjectResource(r.Context(), db.UpdateProjectResourceParams{
		ID:          existing.ID,
		ResourceRef: nextRef,
		Label:       nextLabel,
		Position:    nextPosition,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "this resource is already attached to the project")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update project resource")
		return
	}

	resp := projectResourceToResponse(updated)
	h.publish(
		protocol.EventProjectResourceUpdated,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{"resource": resp, "project_id": uuidToString(project.ID)},
	)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) findLocalDirectoryConflict(ctx context.Context, projectID pgtype.UUID, resourceType string, normalizedRef json.RawMessage, excludeID pgtype.UUID) (bool, error) {
	if resourceType != "local_directory" {
		return false, nil
	}
	var incoming localDirectoryRef
	if err := json.Unmarshal(normalizedRef, &incoming); err != nil {
		return false, err
	}
	rows, err := h.Queries.ListProjectResources(ctx, projectID)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.ResourceType != "local_directory" {
			continue
		}
		if excludeID.Valid && uuidToString(row.ID) == uuidToString(excludeID) {
			continue
		}
		var existing localDirectoryRef
		if err := json.Unmarshal(row.ResourceRef, &existing); err != nil {
			continue
		}

		if existing.DaemonID == incoming.DaemonID {
			return true, nil
		}
	}
	return false, nil
}

func (h *Handler) DeleteProjectResource(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	resourceUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "resourceId"), "resource id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	resource, err := h.Queries.GetProjectResourceInWorkspace(r.Context(), db.GetProjectResourceInWorkspaceParams{
		ID: resourceUUID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project resource not found")
		return
	}
	if uuidToString(resource.ProjectID) != uuidToString(project.ID) {
		writeError(w, http.StatusNotFound, "project resource not found")
		return
	}
	if err := h.Queries.DeleteProjectResource(r.Context(), resource.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project resource")
		return
	}
	h.publish(
		protocol.EventProjectResourceDeleted,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{
			"project_id":  uuidToString(project.ID),
			"resource_id": uuidToString(resource.ID),
		},
	)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) parseUserUUIDOrZero(userID string) (pgtype.UUID, bool) {
	if userID == "" {
		return pgtype.UUID{}, false
	}
	u, err := parseUUIDLoose(userID)
	if err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}

func parseUUIDLoose(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

func (h *Handler) listProjectResourcesForProject(ctx context.Context, projectID pgtype.UUID) []db.ProjectResource {
	if !projectID.Valid {
		return nil
	}
	rows, err := h.Queries.ListProjectResources(ctx, projectID)
	if err != nil {
		return nil
	}
	return rows
}
