package task

import (
	"encoding/json"
	"net/http"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

type batchUpdateRequest struct {
	IssueIDs []string        `json:"issue_ids"`
	Updates  json.RawMessage `json:"updates"`
}

// handleBatchUpdate — batchUpdateIssues: применяет updates к каждой задаче
// независимо; ошибка на одной задаче молча пропускает её (contract §7911).
func (d *Deps) handleBatchUpdate(w http.ResponseWriter, r *http.Request) {
	ws, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req batchUpdateRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || len(req.IssueIDs) == 0 || len(req.Updates) == 0 {
		httpapi.BadRequest(w, "issue_ids and updates are required")
		return
	}
	fields, upreq, verr := d.parseUpdateFields(r.Context(), ws, actor, role, req.Updates)
	if verr != nil {
		writeStoreErr(w, verr)
		return
	}
	updated := 0
	for _, id := range req.IssueIDs {
		before, after, err := d.Store.UpdateIssue(r.Context(), ws.ID, id, fields)
		if err != nil {
			continue
		}
		updated++
		d.afterIssueMutated(r.Context(), ws, before, after)
		d.publishWorkspace(ws.ID, "issue:updated", map[string]any{"issue": after})
		d.enqueueAutostart(r.Context(), ws, before, after, false, "", upreq.SuppressRun, upreq.HandoffNote)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"updated": updated})
}

// handleBatchDelete — batchDeleteIssues: отменяет активные запуски, удаляет
// каждую задачу независимо, публикует issue:deleted за каждую успешную.
func (d *Deps) handleBatchDelete(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		IssueIDs []string `json:"issue_ids"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || len(req.IssueIDs) == 0 {
		httpapi.BadRequest(w, "issue_ids is required")
		return
	}
	deleted := 0
	for _, id := range req.IssueIDs {
		if _, err := d.Dispatch.CancelActiveForTicket(r.Context(), d.Store.pool(), ws.ID, id); err != nil && d.Logger != nil {
			d.Logger.Warn("task: отмена активных запусков перед пакетным удалением", "err", err, "issue_id", id)
		}
		found, err := d.Store.DeleteIssue(r.Context(), ws.ID, id)
		if err != nil || !found {
			continue
		}
		deleted++
		d.publishWorkspace(ws.ID, "issue:deleted", map[string]any{"issue_id": id})
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// ---------------------------------------------------------------------------
// quick-create
// ---------------------------------------------------------------------------

type quickCreateRequest struct {
	AgentID       string   `json:"agent_id"`
	SquadID       string   `json:"squad_id"`
	Prompt        string   `json:"prompt"`
	Priority      string   `json:"priority"`
	DueDate       *string  `json:"due_date"`
	ProjectID     *string  `json:"project_id"`
	ParentIssueID *string  `json:"parent_issue_id"`
	AttachmentIDs []string `json:"attachment_ids"`
}

// handleQuickCreate — quickCreateIssue: создаёt задачу из текстового промпта
// и сразу ставит агента/лидера отряда в очередь (dj_kind=quick_create).
// Заголовок задачи — начало prompt (контракт не уточняет алгоритм извлечения
// заголовка из промпта дальше "из текстового промпта"; решение
// зафиксировано в server2/docs/decisions.md).
func (d *Deps) handleQuickCreate(w http.ResponseWriter, r *http.Request) {
	ws, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req quickCreateRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.Prompt == "" {
		httpapi.BadRequest(w, "prompt is required")
		return
	}
	if (req.AgentID == "") == (req.SquadID == "") {
		httpapi.BadRequest(w, "exactly one of agent_id/squad_id is required")
		return
	}
	assigneeType, assigneeID := "agent", req.AgentID
	if req.SquadID != "" {
		assigneeType, assigneeID = "squad", req.SquadID
	}
	op, isSquad, err := d.Store.validateAssignee(r.Context(), ws.ID, actor, role, &assigneeType, &assigneeID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	due, err := parseDate(req.DueDate)
	if err != nil {
		httpapi.BadRequest(w, "due_date must be YYYY-MM-DD")
		return
	}
	title := req.Prompt
	if len(title) > 120 {
		title = title[:120]
	}
	creatorType := "member"
	if !actor.IsHuman {
		creatorType = "agent"
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}
	issue, err := d.Store.CreateIssue(r.Context(), d.Workspaces, CreateParams{
		WorkspaceID: ws.ID, Title: title, Status: "todo", Priority: priority,
		AssigneeType: &assigneeType, AssigneeID: &assigneeID,
		CreatorType: creatorType, CreatorID: actor.UserID,
		ParentIssueID: req.ParentIssueID, ProjectID: req.ProjectID, DueDate: due,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	d.publishWorkspace(ws.ID, "issue:created", map[string]any{"issue": issue})

	spec := dispatch.JobSpec{
		WorkspaceID: ws.ID, OperativeID: op.ID, ExecutorID: op.ExecutorID,
		TicketID: issue.ID, Kind: dispatch.KindQuickCreate, IsLeader: isSquad,
		QuickCreatePrompt: req.Prompt, QuickCreatePriority: priority,
	}
	if _, err := d.Dispatch.Enqueue(r.Context(), d.Store.pool(), spec); err != nil && d.Logger != nil {
		d.Logger.Error("task: постановка агента в очередь при quick-create", "err", err, "issue_id", issue.ID)
	}
	httpapi.WriteJSON(w, http.StatusCreated, issue)
}

// ---------------------------------------------------------------------------
// preview-trigger — симуляция правила автозапуска без побочных эффектов
// ---------------------------------------------------------------------------

type previewTriggerRequest struct {
	IssueIDs     []string `json:"issue_ids"`
	IsCreate     bool     `json:"is_create"`
	AssigneeType *string  `json:"assignee_type"`
	AssigneeID   *string  `json:"assignee_id"`
	Status       string   `json:"status"`
}

func (d *Deps) handlePreviewTrigger(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req previewTriggerRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	type triggerPreview struct {
		IssueID string `json:"issue_id"`
		AgentID string `json:"agent_id"`
		Source  string `json:"source"`
	}
	var triggers []triggerPreview
	for _, id := range req.IssueIDs {
		before, err := d.Store.GetIssue(r.Context(), ws.ID, id)
		if err != nil {
			continue
		}
		after := before
		if req.AssigneeType != nil {
			after.AssigneeType = req.AssigneeType
			after.AssigneeID = req.AssigneeID
		}
		if req.Status != "" {
			after.Status = req.Status
		}
		decision, err := d.Store.EvaluateAutostart(r.Context(), ws.ID, before, after, req.IsCreate, "", false)
		if err != nil || !decision.Triggered {
			continue
		}
		source := "assign"
		if decision.Reason == "status_reopened" {
			source = "status"
		}
		triggers = append(triggers, triggerPreview{IssueID: id, AgentID: decision.Operative.ID, Source: source})
	}
	if triggers == nil {
		triggers = []triggerPreview{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"triggers": triggers, "total_count": len(triggers)})
}
