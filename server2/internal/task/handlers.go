package task

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// resolveWorkspace резолвит воркспейс из X-Workspace-Slug/X-Workspace-ID (или
// task-token, см. httpapi.ResolveWorkspaceRef) и проверяет членство
// вызывающего — общий вход всех обработчиков /api/issues/** (contract
// x-roles: member или agent). role=="" — вызывающий не человек-участник
// (агентская аутентификация — вне объёма этой сессии, см.
// server2/docs/decisions.md); сегодня это ветвление не достигается, потому
// что authn пока не проверяет mat_-токены.
func (d *Deps) resolveWorkspace(w http.ResponseWriter, r *http.Request) (ws workspace.Workspace, role string, actor *httpapi.Actor, ok bool) {
	actor, aok := httpapi.RequireActor(w, r)
	if !aok {
		return workspace.Workspace{}, "", nil, false
	}
	ref, isSlug, found := httpapi.ResolveWorkspaceRef(r, actor)
	if !found {
		httpapi.BadRequest(w, "workspace_id or workspace_slug is required")
		return workspace.Workspace{}, "", nil, false
	}
	var wsRow workspace.Workspace
	var err error
	if isSlug {
		wsRow, err = d.Workspaces.GetWorkspaceBySlug(r.Context(), ref)
	} else {
		wsRow, err = d.Workspaces.GetWorkspace(r.Context(), ref)
	}
	if err == workspace.ErrNotFound {
		httpapi.NotFound(w, "workspace not found")
		return workspace.Workspace{}, "", nil, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return workspace.Workspace{}, "", nil, false
	}
	if actor.IsHuman {
		member, err := d.Workspaces.GetMemberByUser(r.Context(), wsRow.ID, actor.UserID)
		if err == workspace.ErrNotFound {
			httpapi.Forbidden(w, "not a member of this workspace")
			return workspace.Workspace{}, "", nil, false
		}
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return workspace.Workspace{}, "", nil, false
		}
		return wsRow, member.Role, actor, true
	}
	// Агент-актор: контракт допускает x-roles agent на этих маршрутах, но
	// проверка его области действия (X-Task-ID) требует дожидающегося
	// daemon-протокола T-028; отклоняем как ещё не поддержанный путь, не
	// притворяясь, что дальше идёт настоящая проверка.
	httpapi.Forbidden(w, "agent-actor authentication is not wired in this session (see server2/docs/decisions.md)")
	return workspace.Workspace{}, "", nil, false
}

func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpapi.NotFound(w, "issue not found")
	case errors.Is(err, ErrLabelNotFound):
		httpapi.WriteError(w, http.StatusNotFound, err.Error(), "not_found")
	case errors.Is(err, ErrConflict):
		httpapi.WriteError(w, http.StatusConflict, err.Error(), "conflict")
	default:
		var verr *validationError
		if errors.As(err, &verr) {
			if verr.forbidden {
				httpapi.Forbidden(w, verr.message)
			} else {
				httpapi.BadRequest(w, verr.message)
			}
			return
		}
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
	}
}

// publishWorkspace — сокращение для d.Publisher.Publish, не паникующее, если
// Publisher не задан (юнит-тесты Store без HTTP-слоя).
func (d *Deps) publishWorkspace(workspaceID, eventType string, payload any) {
	if d.Publisher == nil {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{Type: eventType, Payload: payload})
}

// enqueueAutostart — общая точка для createIssue/updateIssue/moveIssue/
// batchUpdateIssues: если правило автозапуска (contract §1.9) сработало,
// ставит в очередь через dispatch.Deps.Enqueue. requestingAgentID — id
// агента-актора текущего запроса, если применимо (для защиты от самозапуска;
// сегодня всегда "", пока агентская аутентификация не подключена).
func (d *Deps) enqueueAutostart(ctx context.Context, ws workspace.Workspace, before, after Issue, isCreate bool, requestingAgentID string, suppressRun bool, handoffNote string) {
	decision, err := d.Store.EvaluateAutostart(ctx, ws.ID, before, after, isCreate, requestingAgentID, suppressRun)
	if err != nil || !decision.Triggered {
		return
	}
	spec := dispatch.JobSpec{
		WorkspaceID: ws.ID,
		OperativeID: decision.Operative.ID,
		ExecutorID:  decision.Operative.ExecutorID,
		TicketID:    after.ID,
		Kind:        dispatch.KindIssue,
		IsLeader:    decision.IsSquad,
		HandoffNote: handoffNote,
	}
	if _, err := d.Dispatch.Enqueue(ctx, d.Store.pool(), spec); err != nil && d.Logger != nil {
		d.Logger.Error("task: постановка агента в очередь после автозапуска", "err", err, "issue_id", after.ID)
	}
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

type createIssueRequest struct {
	Title         string   `json:"title"`
	Description   *string  `json:"description"`
	Status        string   `json:"status"`
	Priority      string   `json:"priority"`
	AssigneeType  *string  `json:"assignee_type"`
	AssigneeID    *string  `json:"assignee_id"`
	ParentIssueID *string  `json:"parent_issue_id"`
	ProjectID     *string  `json:"project_id"`
	Stage         *int     `json:"stage"`
	StartDate     *string  `json:"start_date"`
	DueDate       *string  `json:"due_date"`
	LabelIDs      []string `json:"label_ids"`
}

func parseDate(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := time.Parse(dateLayout, *s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (d *Deps) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	ws, _, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req createIssueRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		httpapi.BadRequest(w, "title must not be empty")
		return
	}
	if req.Status != "" && !validStatuses[req.Status] {
		httpapi.BadRequest(w, "invalid status")
		return
	}
	if req.Priority != "" && !validPriorities[req.Priority] {
		httpapi.BadRequest(w, "invalid priority")
		return
	}
	if (req.AssigneeType == nil) != (req.AssigneeID == nil) {
		httpapi.BadRequest(w, "assignee_type and assignee_id must be provided together")
		return
	}
	role := ""
	if actor.IsHuman {
		if m, err := d.Workspaces.GetMemberByUser(r.Context(), ws.ID, actor.UserID); err == nil {
			role = m.Role
		}
	}
	if req.AssigneeType != nil {
		if !validAssigneeTypes[*req.AssigneeType] {
			httpapi.BadRequest(w, "invalid assignee_type")
			return
		}
		if _, _, err := d.Store.validateAssignee(r.Context(), ws.ID, actor, role, req.AssigneeType, req.AssigneeID); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	start, err := parseDate(req.StartDate)
	if err != nil {
		httpapi.BadRequest(w, "start_date must be YYYY-MM-DD")
		return
	}
	due, err := parseDate(req.DueDate)
	if err != nil {
		httpapi.BadRequest(w, "due_date must be YYYY-MM-DD")
		return
	}

	creatorType := "member"
	if !actor.IsHuman {
		creatorType = "agent"
	}
	issue, err := d.Store.CreateIssue(r.Context(), d.Workspaces, CreateParams{
		WorkspaceID: ws.ID, Title: req.Title, Description: req.Description,
		Status: req.Status, Priority: req.Priority,
		AssigneeType: req.AssigneeType, AssigneeID: req.AssigneeID,
		CreatorType: creatorType, CreatorID: actor.UserID,
		ParentIssueID: req.ParentIssueID, ProjectID: req.ProjectID, Stage: req.Stage,
		StartDate: start, DueDate: due,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if err := d.Store.Subscribe(r.Context(), ws.ID, issue.ID, "member", actor.UserID); err != nil && d.Logger != nil {
		d.Logger.Warn("task: автоподписка создателя", "err", err)
	}
	for _, labelID := range req.LabelIDs {
		if _, err := d.Store.AttachIssueLabel(r.Context(), ws.ID, issue.ID, labelID); err != nil && d.Logger != nil {
			d.Logger.Warn("task: прикрепление метки при создании", "err", err, "label_id", labelID)
		}
	}
	if labels, err := d.Store.ListIssueLabels(r.Context(), ws.ID, issue.ID); err == nil {
		issue.Labels = labels
	}

	d.publishWorkspace(ws.ID, "issue:created", map[string]any{"issue": issue})
	d.enqueueAutostart(r.Context(), ws, Issue{}, issue, true, "", false, "")
	d.notifyAssigned(r.Context(), ws, issue, actorKind(actor.IsHuman), actor.UserID)

	httpapi.WriteJSON(w, http.StatusCreated, issue)
}

func (d *Deps) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	issue, err := d.Store.GetIssue(r.Context(), ws.ID, r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if labels, err := d.Store.ListIssueLabels(r.Context(), ws.ID, issue.ID); err == nil {
		issue.Labels = labels
	}
	httpapi.WriteJSON(w, http.StatusOK, issue)
}

type updateIssueRequest struct {
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
	SuppressRun  bool    `json:"suppress_run"`
	HandoffNote  string  `json:"handoff_note"`
}

func (d *Deps) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	ws, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	body, err := httpapi.ReadBody(r)
	if err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	fields, req, verr := d.parseUpdateFields(r.Context(), ws, actor, role, body)
	if verr != nil {
		writeStoreErr(w, verr)
		return
	}
	before, after, err := d.Store.UpdateIssue(r.Context(), ws.ID, id, fields)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if labels, err := d.Store.ListIssueLabels(r.Context(), ws.ID, after.ID); err == nil {
		after.Labels = labels
	}
	d.afterIssueMutated(r.Context(), ws, before, after)
	d.notifyIssueChange(r.Context(), ws, before, after, actorKind(actor.IsHuman), actor.UserID)

	d.publishWorkspace(ws.ID, "issue:updated", map[string]any{"issue": after})
	d.enqueueAutostart(r.Context(), ws, before, after, false, "", req.SuppressRun, req.HandoffNote)
	httpapi.WriteJSON(w, http.StatusOK, after)
}

// notifyIssueChange — общая точка для updateIssue/moveIssue: назначение и
// смена статуса — единственные два источника инбокс-уведомлений из этого
// домена по правилам контракта (см. internal/task/notify.go); упоминание и
// комментарий в подписанной задаче уведомляются из internal/note.
func (d *Deps) notifyIssueChange(ctx context.Context, ws workspace.Workspace, before, after Issue, actorType, actorID string) {
	if !ptrEq(before.AssigneeType, after.AssigneeType) || !ptrEq(before.AssigneeID, after.AssigneeID) {
		d.notifyAssigned(ctx, ws, after, actorType, actorID)
	}
	d.notifyStatusChanged(ctx, ws, before, after, actorType, actorID)
}

// afterIssueMutated — если исполнитель снят или статус ушёл в done/cancelled,
// отменяет активные запуски агентов на этой задаче (contract: постановка
// задачи "снимается" при снятии назначения/смене статуса — та часть API
// пакета dispatch, которую заказывал тикет T-027).
func (d *Deps) afterIssueMutated(ctx context.Context, ws workspace.Workspace, before, after Issue) {
	assigneeCleared := before.AssigneeID != nil && after.AssigneeID == nil
	wentTerminal := terminalStatuses[after.Status] && !terminalStatuses[before.Status]
	if !assigneeCleared && !wentTerminal {
		return
	}
	jobs, err := d.Dispatch.CancelActiveForTicket(ctx, d.Store.pool(), ws.ID, after.ID)
	if err != nil && d.Logger != nil {
		d.Logger.Error("task: отмена активных запусков после изменения задачи", "err", err, "issue_id", after.ID)
	}
	_ = jobs
}

// parseUpdateFields разбирает и валидирует тело PUT /api/issues/{id}
// (updateIssueRequest) в UpdateFields, включая пересчёт assignee-полномочий.
func (d *Deps) parseUpdateFields(ctx context.Context, ws workspace.Workspace, actor *httpapi.Actor, role string, body json.RawMessage) (UpdateFields, updateIssueRequest, error) {
	var req updateIssueRequest
	_ = json.Unmarshal(body, &req)
	fields, err := d.Store.fieldsFromRaw(body)
	if err != nil {
		return UpdateFields{}, req, errBadRequest("invalid JSON body")
	}
	if fields.AssigneeSet {
		if (fields.AssigneeType == nil) != (fields.AssigneeID == nil) {
			return UpdateFields{}, req, errBadRequest("assignee_type and assignee_id must be provided together")
		}
		if fields.AssigneeType != nil {
			if !validAssigneeTypes[*fields.AssigneeType] {
				return UpdateFields{}, req, errBadRequest("invalid assignee_type")
			}
			if _, _, err := d.Store.validateAssignee(ctx, ws.ID, actor, role, fields.AssigneeType, fields.AssigneeID); err != nil {
				return UpdateFields{}, req, err
			}
		}
	}
	if fields.StatusSet && !validStatuses[fields.Status] {
		return UpdateFields{}, req, errBadRequest("invalid status")
	}
	if fields.PrioritySet && !validPriorities[fields.Priority] {
		return UpdateFields{}, req, errBadRequest("invalid priority")
	}
	return fields, req, nil
}

func (d *Deps) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := d.Dispatch.CancelActiveForTicket(r.Context(), d.Store.pool(), ws.ID, id); err != nil && d.Logger != nil {
		d.Logger.Warn("task: отмена активных запусков перед удалением", "err", err, "issue_id", id)
	}
	found, err := d.Store.DeleteIssue(r.Context(), ws.ID, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !found {
		httpapi.NotFound(w, "issue not found")
		return
	}
	d.publishWorkspace(ws.ID, "issue:deleted", map[string]any{"issue_id": id})
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---------------------------------------------------------------------------
// Move
// ---------------------------------------------------------------------------

type moveIssueRequest struct {
	BeforeID *string `json:"before_id"`
	AfterID  *string `json:"after_id"`
}

func (d *Deps) handleMoveIssue(w http.ResponseWriter, r *http.Request) {
	ws, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	body, err := httpapi.ReadBody(r)
	if err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	var mv moveIssueRequest
	if err := json.Unmarshal(body, &mv); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	fields, req, verr := d.parseUpdateFields(r.Context(), ws, actor, role, body)
	if verr != nil {
		writeStoreErr(w, verr)
		return
	}
	before, err := d.Store.GetIssue(r.Context(), ws.ID, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	after, err := d.Store.MoveIssue(r.Context(), ws.ID, id, mv.BeforeID, mv.AfterID, fields)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if labels, err := d.Store.ListIssueLabels(r.Context(), ws.ID, after.ID); err == nil {
		after.Labels = labels
	}
	d.afterIssueMutated(r.Context(), ws, before, after)
	d.notifyIssueChange(r.Context(), ws, before, after, actorKind(actor.IsHuman), actor.UserID)
	d.publishWorkspace(ws.ID, "issue:updated", map[string]any{"issue": after})
	d.enqueueAutostart(r.Context(), ws, before, after, false, "", req.SuppressRun, req.HandoffNote)
	httpapi.WriteJSON(w, http.StatusOK, after)
}

// ---------------------------------------------------------------------------
// Listing / search / children / grouped
// ---------------------------------------------------------------------------

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func (d *Deps) parseListParams(r *http.Request, issuePrefix string) ListParams {
	q := r.URL.Query()
	p := ListParams{
		Limit: atoiOr(q.Get("limit"), 100), Offset: atoiOr(q.Get("offset"), 0),
		OpenOnly: q.Get("open_only") == "true", Sort: q.Get("sort"), Direction: q.Get("direction"),
		AssigneeID: q.Get("assignee_id"), IncludeNoAssignee: q.Get("include_no_assignee") == "true",
		CreatorID: q.Get("creator_id"), ProjectID: q.Get("project_id"), IncludeNoProject: q.Get("include_no_project") == "true",
		TopLevelOnly: q.Get("top_level_only") == "true", Q: q.Get("q"), Scheduled: q.Get("scheduled") == "true",
		DateField: q.Get("date_field"), IssuePrefix: issuePrefix,
	}
	if statuses := q.Get("statuses"); statuses != "" {
		p.Statuses = splitCSV(statuses)
	} else if legacy := q.Get("status"); legacy != "" {
		p.Statuses = splitCSV(legacy)
	}
	if priorities := q.Get("priorities"); priorities != "" {
		p.Priorities = splitCSV(priorities)
	} else if legacy := q.Get("priority"); legacy != "" {
		p.Priorities = splitCSV(legacy)
	}
	p.AssigneeIDs = splitCSV(q.Get("assignee_ids"))
	p.AssigneeTypes = splitCSV(q.Get("assignee_types"))
	p.AssigneeFilters = ParseTypeIDList(q.Get("assignee_filters"))
	p.CreatorFilters = ParseTypeIDList(q.Get("creator_filters"))
	p.ProjectIDs = splitCSV(q.Get("project_ids"))
	p.LabelIDs = splitCSV(q.Get("label_ids"))
	if q.Has("ids") {
		p.IDsGiven = true
		p.IDs = splitCSV(q.Get("ids"))
	}
	if ds := q.Get("date_start"); ds != "" {
		if t, err := time.Parse(time.RFC3339, ds); err == nil {
			p.DateStart = &t
		}
	}
	if de := q.Get("date_end"); de != "" {
		if t, err := time.Parse(time.RFC3339, de); err == nil {
			p.DateEnd = &t
		}
	}
	return p
}

func (d *Deps) handleListIssues(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	p := d.parseListParams(r, ws.IssuePrefix)
	issues, total, err := d.Store.ListIssues(r.Context(), ws.ID, p)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"issues": issues, "total": total})
}

func (d *Deps) handleQueryIssues(w http.ResponseWriter, r *http.Request) {
	// queryIssues — те же фильтры, но в JSON-теле вместо query-параметров;
	// переиспользуем разбор query, наливая тело в URL-подобный набор
	// значений было бы избыточно, поэтому здесь — прямой разбор JSON в ту же
	// структуру полей ListParams через промежуточный DTO с теми же именами.
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var raw map[string]any
	if err := httpapi.DecodeJSON(r, &raw); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	p := listParamsFromMap(raw, ws.IssuePrefix)
	issues, total, err := d.Store.ListIssues(r.Context(), ws.ID, p)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"issues": issues, "total": total})
}

func listParamsFromMap(m map[string]any, issuePrefix string) ListParams {
	p := ListParams{Limit: 100, IssuePrefix: issuePrefix}
	if v, ok := m["limit"].(float64); ok {
		p.Limit = int(v)
	}
	if v, ok := m["offset"].(float64); ok {
		p.Offset = int(v)
	}
	if v, ok := m["open_only"].(bool); ok {
		p.OpenOnly = v
	}
	if v, ok := m["sort"].(string); ok {
		p.Sort = v
	}
	if v, ok := m["direction"].(string); ok {
		p.Direction = v
	}
	if v, ok := m["q"].(string); ok {
		p.Q = v
	}
	if v, ok := m["top_level_only"].(bool); ok {
		p.TopLevelOnly = v
	}
	p.Statuses = stringsFromAny(m["statuses"])
	p.Priorities = stringsFromAny(m["priorities"])
	p.AssigneeIDs = stringsFromAny(m["assignee_ids"])
	p.LabelIDs = stringsFromAny(m["label_ids"])
	if v, ok := m["ids"]; ok {
		p.IDsGiven = true
		p.IDs = stringsFromAny(v)
	}
	return p
}

func stringsFromAny(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (d *Deps) handleSearchIssues(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	p := d.parseListParams(r, ws.IssuePrefix)
	issues, total, err := d.Store.SearchIssues(r.Context(), ws.ID, p)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"issues": issues, "total": total})
}

func (d *Deps) handleChildIssues(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	issues, err := d.Store.ChildIssues(r.Context(), ws.ID, r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"issues": issues})
}

func (d *Deps) handleChildProgress(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	entries, err := d.Store.ChildProgress(r.Context(), ws.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"progress": entries})
}

func (d *Deps) handleGroupedIssues(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	groups, err := d.Store.GroupedByAssignee(r.Context(), ws.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (d *Deps) handleAssigneeFrequency(w http.ResponseWriter, r *http.Request) {
	ws, _, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	callerType := "member"
	if !actor.IsHuman {
		callerType = "agent"
	}
	entries, err := d.Store.AssigneeFrequency(r.Context(), ws.ID, callerType, actor.UserID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, entries)
}

func (d *Deps) handlePullRequests(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	links, err := d.Store.PullRequests(r.Context(), ws.ID, r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, links)
}
