package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

func (d *Deps) handleListAutopilots(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	var status *string
	if q := r.URL.Query().Get("status"); q != "" {
		status = &q
	}
	list, total, err := d.Store.List(r.Context(), member.WorkspaceID, status)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]Autopilot, len(list))
	for i, a := range list {
		out[i] = d.withAccess(r.Context(), a, member.UserID, member.Role)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"autopilots": nonNilAutopilots(out), "total": total})
}

func nonNilAutopilots(a []Autopilot) []Autopilot {
	if a == nil {
		return []Autopilot{}
	}
	return a
}

// withAccess вычисляет can_write/can_manage_access для персонализированного
// ответа (contract: "Только в getAutopilot"/"Только в персонализированном
// ответе (списки/getAutopilot)") — используется и списком, и созданием/
// изменением автопилота (тоже персонализированные ответы вызывающему).
func (d *Deps) withAccess(ctx context.Context, a Autopilot, userID string, role httpapi.Role) Autopilot {
	canWrite, err := d.Store.CanWrite(ctx, a.ID, userID, role)
	if err != nil {
		canWrite = httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin)
	}
	canManage, err := d.Store.CanManageAccess(ctx, a.ID, userID, role)
	if err != nil {
		canManage = httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin)
	}
	return a.WithAccess(canWrite, canManage)
}

type createAutopilotRequest struct {
	Title              string            `json:"title"`
	Description        *string           `json:"description"`
	ProjectID          *string           `json:"project_id"`
	AssigneeType       string            `json:"assignee_type"`
	AssigneeID         string            `json:"assignee_id"`
	ExecutionMode      string            `json:"execution_mode"`
	IssueTitleTemplate *string           `json:"issue_title_template"`
	Subscribers        []subscriberInput `json:"subscribers"`
}

type subscriberInput struct {
	UserType string `json:"user_type"`
	UserID   string `json:"user_id"`
}

func toSubscriberInputs(in []subscriberInput) []SubscriberInput {
	out := make([]SubscriberInput, len(in))
	for i, s := range in {
		out[i] = SubscriberInput{UserType: s.UserType, UserID: s.UserID}
	}
	return out
}

var validExecutionModes = map[string]bool{"create_issue": true, "run_only": true}
var validAssigneeTypesAP = map[string]bool{"agent": true, "squad": true}

func (d *Deps) handleCreateAutopilot(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	var req createAutopilotRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.Title == "" {
		httpapi.BadRequest(w, "title is required")
		return
	}
	if req.AssigneeType == "" {
		req.AssigneeType = "agent"
	}
	if !validAssigneeTypesAP[req.AssigneeType] {
		httpapi.BadRequest(w, "assignee_type must be agent or squad")
		return
	}
	if req.AssigneeID == "" {
		httpapi.BadRequest(w, "assignee_id is required")
		return
	}
	if !validExecutionModes[req.ExecutionMode] {
		httpapi.BadRequest(w, "execution_mode must be create_issue or run_only")
		return
	}
	assignee, err := d.Store.ResolveAssignee(r.Context(), member.WorkspaceID, req.AssigneeType, req.AssigneeID)
	if err != nil {
		httpapi.BadRequest(w, "assignee not found or unavailable: "+err.Error())
		return
	}
	if assignee.Archived {
		httpapi.BadRequest(w, "assignee is archived")
		return
	}
	canInvoke, err := d.Store.CanInvoke(r.Context(), assignee, actor.UserID, member.Role)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !canInvoke {
		httpapi.Forbidden(w, "assignee is not available to you")
		return
	}

	a, err := d.Store.Create(r.Context(), CreateParams{
		WorkspaceID: member.WorkspaceID, Title: req.Title, Description: req.Description,
		ProjectID: req.ProjectID, AssigneeType: req.AssigneeType, AssigneeID: req.AssigneeID,
		ExecutionMode: req.ExecutionMode, IssueTitleTemplate: req.IssueTitleTemplate,
		CreatedByType: "member", CreatedByID: actor.UserID,
		Subscribers: toSubscriberInputs(req.Subscribers),
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(member.WorkspaceID, "autopilot:created", map[string]any{"autopilot": a})
	httpapi.WriteJSON(w, http.StatusCreated, d.withAccess(r.Context(), a, actor.UserID, member.Role))
}

func (d *Deps) handleCronPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.Resolver.RequireMember(w, r); !ok {
		return
	}
	expr := r.URL.Query().Get("expr")
	tz := r.URL.Query().Get("tz")
	if tz == "" {
		tz = "UTC"
	}
	loc, err := LoadTimezone(tz)
	if err != nil {
		httpapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "code": "invalid_timezone"})
		return
	}
	sched, err := ParseCron(expr)
	if err != nil {
		httpapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "code": "invalid_cron"})
		return
	}
	var next []string
	from := time.Now().UTC()
	for i := 0; i < 3; i++ {
		occ, ok := sched.NextInLocation(from, loc)
		if !ok {
			break
		}
		next = append(next, occ.Format(time.RFC3339))
		from = occ.In(loc)
	}
	if next == nil {
		next = []string{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"next_runs": next})
}

func (d *Deps) handleGetAutopilot(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	a, err := d.Store.Get(r.Context(), member.WorkspaceID, id)
	if err == ErrNotFound {
		httpapi.NotFound(w, "autopilot not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	canWrite, err := d.Store.CanWrite(r.Context(), id, actor.UserID, member.Role)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	triggers, err := d.Store.ListTriggers(r.Context(), id)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	collaborators, err := d.Store.ListCollaborators(r.Context(), id)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	canManageAccess, err := d.Store.CanManageAccess(r.Context(), id, actor.UserID, member.Role)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	// webhook_token/webhook_path/webhook_url — только владельцу/коллаборатору
	// с правом записи (contract §7); остальным участникам — Sanitized().
	base := d.baseURL(r)
	triggerViews := make([]any, len(triggers))
	for i, trig := range triggers {
		if canWrite {
			triggerViews[i] = trig.ToJSON(base)
		} else {
			triggerViews[i] = trig.Sanitized()
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"autopilot":     a.WithAccess(canWrite, canManageAccess),
		"triggers":      triggerViews,
		"collaborators": nonNilCollaborators(collaborators),
	})
}

type updateAutopilotRequest struct {
	Title                 *string           `json:"title"`
	DescriptionSet        bool              `json:"-"`
	Description           *string           `json:"description"`
	ProjectIDSet          bool              `json:"-"`
	ProjectID             *string           `json:"project_id"`
	AssigneeType          *string           `json:"assignee_type"`
	AssigneeID            *string           `json:"assignee_id"`
	Status                *string           `json:"status"`
	ExecutionMode         *string           `json:"execution_mode"`
	IssueTitleTemplateSet bool              `json:"-"`
	IssueTitleTemplate    *string           `json:"issue_title_template"`
	SubscribersSet        bool              `json:"-"`
	Subscribers           []subscriberInput `json:"subscribers"`
}

var validStatusesAP = map[string]bool{"active": true, "paused": true, "archived": true}

func (d *Deps) handleUpdateAutopilot(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !d.requireWriteAccess(w, r, id, actor.UserID, member.Role) {
		return
	}
	body, err := httpapi.ReadBody(r)
	if err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	// fields — тело как map[string]json.RawMessage, только чтобы отличить
	// "ключ не передан" от "ключ передан как null" для nullable-полей
	// (description/project_id/issue_title_template/subscribers) — тот же
	// приём, что task.rawFields (internal/task/patch.go), решает ту же
	// contract-задачу PATCH ("не передавать поле — не трогать значение").
	fields := map[string]json.RawMessage{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &fields); err != nil {
			httpapi.BadRequest(w, "invalid JSON body")
			return
		}
	}
	// T-029 доводка: httpapi.ReadBody(r) выше уже полностью вычитал r.Body —
	// httpapi.DecodeJSON(r, &req) здесь читал бы из уже опустошённого r.Body
	// (io.EOF на первом же ключе), из-за чего PATCH /api/autopilots/{id} с
	// любым непустым телом всегда отвечал 400 "invalid JSON body", даже для
	// буквально контрактного запроса. Разбор — из уже прочитанных байт body,
	// тем же приёмом, что fields чуть выше.
	var req updateAutopilotRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			httpapi.BadRequest(w, "invalid JSON body")
			return
		}
	}
	if req.AssigneeType != nil || req.AssigneeID != nil {
		if req.AssigneeType == nil || req.AssigneeID == nil {
			httpapi.BadRequest(w, "assignee_type and assignee_id must be provided together")
			return
		}
		if !validAssigneeTypesAP[*req.AssigneeType] {
			httpapi.BadRequest(w, "assignee_type must be agent or squad")
			return
		}
		assignee, err := d.Store.ResolveAssignee(r.Context(), member.WorkspaceID, *req.AssigneeType, *req.AssigneeID)
		if err != nil {
			httpapi.BadRequest(w, "assignee not found or unavailable: "+err.Error())
			return
		}
		if assignee.Archived {
			httpapi.BadRequest(w, "assignee is archived")
			return
		}
	}
	if req.Status != nil && !validStatusesAP[*req.Status] {
		httpapi.BadRequest(w, "status must be active, paused or archived")
		return
	}
	if req.ExecutionMode != nil && !validExecutionModes[*req.ExecutionMode] {
		httpapi.BadRequest(w, "execution_mode must be create_issue or run_only")
		return
	}

	patch := UpdatePatch{}
	if req.Title != nil {
		patch.Title, patch.TitleSet = *req.Title, true
	}
	if _, present := fields["description"]; present {
		patch.Description, patch.DescriptionSet = req.Description, true
	}
	if _, present := fields["project_id"]; present {
		patch.ProjectID, patch.ProjectIDSet = req.ProjectID, true
	}
	if req.AssigneeType != nil {
		patch.AssigneeType, patch.AssigneeID, patch.AssigneeSet = *req.AssigneeType, *req.AssigneeID, true
	}
	if req.Status != nil {
		patch.Status, patch.StatusSet = *req.Status, true
	}
	if req.ExecutionMode != nil {
		patch.ExecutionMode, patch.ExecutionModeSet = *req.ExecutionMode, true
	}
	if _, present := fields["issue_title_template"]; present {
		patch.IssueTitleTemplate, patch.IssueTitleTemplateSet = req.IssueTitleTemplate, true
	}
	if _, present := fields["subscribers"]; present {
		patch.Subscribers, patch.SubscribersSet = toSubscriberInputs(req.Subscribers), true
	}

	a, err := d.Store.Update(r.Context(), member.WorkspaceID, id, patch)
	if err == ErrNotFound {
		httpapi.NotFound(w, "autopilot not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(member.WorkspaceID, "autopilot:updated", map[string]any{"autopilot": a})
	httpapi.WriteJSON(w, http.StatusOK, d.withAccess(r.Context(), a, actor.UserID, member.Role))
}

func (d *Deps) handleDeleteAutopilot(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !d.requireWriteAccess(w, r, id, actor.UserID, member.Role) {
		return
	}
	if err := d.Store.Archive(r.Context(), member.WorkspaceID, id); err == ErrNotFound {
		httpapi.NotFound(w, "autopilot not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(member.WorkspaceID, "autopilot:deleted", map[string]any{"autopilot_id": id})
	w.WriteHeader(http.StatusNoContent)
}

// requireWriteAccess проверяет, что actorID может писать в autopilotID
// (contract §7.1); сама пишет 403/404, ok=false — обработчик уже ответил.
func (d *Deps) requireWriteAccess(w http.ResponseWriter, r *http.Request, autopilotID, actorID string, role httpapi.Role) bool {
	if _, err := d.Store.GetRaw(r.Context(), autopilotID); err == ErrNotFound {
		httpapi.NotFound(w, "autopilot not found")
		return false
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return false
	}
	canWrite, err := d.Store.CanWrite(r.Context(), autopilotID, actorID, role)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return false
	}
	if !canWrite {
		httpapi.Forbidden(w, "not creator/owner/admin/collaborator of this autopilot")
		return false
	}
	return true
}

// --- collaborators -----------------------------------------------------------

type addCollaboratorRequest struct {
	UserID string `json:"user_id"`
}

func (d *Deps) handleAddCollaborator(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !d.requireManageAccess(w, r, id, actor.UserID, member.Role) {
		return
	}
	var req addCollaboratorRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.UserID == "" {
		httpapi.BadRequest(w, "user_id is required")
		return
	}
	list, err := d.Store.AddCollaborator(r.Context(), id, req.UserID, actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(member.WorkspaceID, "autopilot:updated", map[string]any{"autopilot_id": id})
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{"collaborators": nonNilCollaborators(list)})
}

func (d *Deps) handleRemoveCollaborator(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !d.requireManageAccess(w, r, id, actor.UserID, member.Role) {
		return
	}
	userID := r.PathValue("userId")
	list, err := d.Store.RemoveCollaborator(r.Context(), id, userID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(member.WorkspaceID, "autopilot:updated", map[string]any{"autopilot_id": id})
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"collaborators": nonNilCollaborators(list)})
}

func (d *Deps) requireManageAccess(w http.ResponseWriter, r *http.Request, autopilotID, actorID string, role httpapi.Role) bool {
	if _, err := d.Store.GetRaw(r.Context(), autopilotID); err == ErrNotFound {
		httpapi.NotFound(w, "autopilot not found")
		return false
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return false
	}
	can, err := d.Store.CanManageAccess(r.Context(), autopilotID, actorID, role)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return false
	}
	if !can {
		httpapi.Forbidden(w, "only the creator or workspace owner/admin can manage collaborators")
		return false
	}
	return true
}

func nonNilCollaborators(c []Collaborator) []Collaborator {
	if c == nil {
		return []Collaborator{}
	}
	return c
}
