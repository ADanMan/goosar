package task

import (
	"encoding/json"
	"net/http"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

// ---------------------------------------------------------------------------
// Подписчики
// ---------------------------------------------------------------------------

func (d *Deps) handleListSubscribers(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	subs, err := d.Store.ListSubscribers(r.Context(), ws.ID, r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, subs)
}

type subscribeRequest struct {
	UserType string `json:"user_type"`
	UserID   string `json:"user_id"`
}

// resolveSubscribeTarget — подписать/отписать себя по умолчанию; другого
// пользователя — только owner/admin (contract §1.13).
func (d *Deps) resolveSubscribeTarget(w http.ResponseWriter, r *http.Request, role string, actor *httpapi.Actor, req subscribeRequest) (userType, userID string, ok bool) {
	if req.UserType == "" && req.UserID == "" {
		userType := "member"
		if !actor.IsHuman {
			userType = "agent"
		}
		return userType, actor.UserID, true
	}
	if req.UserID == actor.UserID {
		return req.UserType, req.UserID, true
	}
	if !httpapi.RoleAtLeast(httpapi.Role(role), httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "only owner/admin can subscribe another user")
		return "", "", false
	}
	return req.UserType, req.UserID, true
}

func (d *Deps) handleSubscribeIssue(w http.ResponseWriter, r *http.Request) {
	ws, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req subscribeRequest
	_ = httpapi.DecodeJSON(r, &req)
	userType, userID, ok := d.resolveSubscribeTarget(w, r, role, actor, req)
	if !ok {
		return
	}
	issueID := r.PathValue("id")
	if err := d.Store.Subscribe(r.Context(), ws.ID, issueID, userType, userID); err != nil {
		writeStoreErr(w, err)
		return
	}
	d.publishWorkspace(ws.ID, "subscriber:added", map[string]any{"issue_id": issueID, "user_type": userType, "user_id": userID, "reason": "manual"})
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"subscribed": true})
}

func (d *Deps) handleUnsubscribeIssue(w http.ResponseWriter, r *http.Request) {
	ws, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req subscribeRequest
	_ = httpapi.DecodeJSON(r, &req)
	userType, userID, ok := d.resolveSubscribeTarget(w, r, role, actor, req)
	if !ok {
		return
	}
	issueID := r.PathValue("id")
	if err := d.Store.Unsubscribe(r.Context(), ws.ID, issueID, userType, userID); err != nil {
		writeStoreErr(w, err)
		return
	}
	d.publishWorkspace(ws.ID, "subscriber:removed", map[string]any{"issue_id": issueID, "user_type": userType, "user_id": userID})
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"subscribed": false})
}

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

func (d *Deps) handleListMetadata(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	m, err := d.Store.GetMetadata(r.Context(), ws.ID, r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"metadata": m})
}

func (d *Deps) handleSetMetadataKey(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Value any `json:"value"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	issueID, key := r.PathValue("id"), r.PathValue("key")
	m, err := d.Store.SetMetadataKey(r.Context(), ws.ID, issueID, key, req.Value)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	d.publishWorkspace(ws.ID, "issue_metadata:changed", map[string]any{"issue_id": issueID, "metadata": m})
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"metadata": m})
}

func (d *Deps) handleDeleteMetadataKey(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	issueID, key := r.PathValue("id"), r.PathValue("key")
	m, err := d.Store.DeleteMetadataKey(r.Context(), ws.ID, issueID, key)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	d.publishWorkspace(ws.ID, "issue_metadata:changed", map[string]any{"issue_id": issueID, "metadata": m})
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"metadata": m})
}

// ---------------------------------------------------------------------------
// Timeline
// ---------------------------------------------------------------------------

func (d *Deps) handleTimeline(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	entries, err := d.Store.Timeline(r.Context(), ws.ID, r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, entries)
}

// ---------------------------------------------------------------------------
// Активные/исторические запуски агентов, usage, cancel/rerun (через dispatch)
// ---------------------------------------------------------------------------

func (d *Deps) handleActiveTask(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := d.Store.GetIssue(r.Context(), ws.ID, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	jobs, err := d.Dispatch.Store.ActiveForTicket(r.Context(), d.Store.pool(), id)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"tasks": toAgentTasks(jobs)})
}

func (d *Deps) handleTaskRuns(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := d.Store.GetIssue(r.Context(), ws.ID, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	jobs, err := d.Dispatch.Store.RunsForTicket(r.Context(), d.Store.pool(), id)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, toAgentTasks(jobs))
}

func (d *Deps) handleIssueUsage(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := d.Store.GetIssue(r.Context(), ws.ID, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	u, err := d.Dispatch.Store.UsageForTicket(r.Context(), d.Store.pool(), id)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"total_input_tokens": u.InputTokens, "total_output_tokens": u.OutputTokens,
		"total_cache_read_tokens": u.CacheReadTokens, "total_cache_write_tokens": u.CacheWriteTokens,
		"cost_usd_ticks": u.CostUSDTicks,
	})
}

func (d *Deps) handleCancelIssueTask(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	issueID, taskID := r.PathValue("id"), r.PathValue("taskId")
	if _, err := d.Store.GetIssue(r.Context(), ws.ID, issueID); err != nil {
		writeStoreErr(w, err)
		return
	}
	job, found, err := d.Dispatch.CancelJob(r.Context(), d.Store.pool(), ws.ID, taskID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.WriteError(w, http.StatusBadRequest, "task cannot be cancelled in its current state", "invalid_request")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, toAgentTask(job))
}

type rerunRequest struct {
	TaskID string `json:"task_id"`
}

func (d *Deps) handleRerunIssue(w http.ResponseWriter, r *http.Request) {
	ws, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	issue, err := d.Store.GetIssue(r.Context(), ws.ID, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if issue.AssigneeType == nil || issue.AssigneeID == nil {
		httpapi.BadRequest(w, "issue has no agent/squad assignee to rerun")
		return
	}
	op, isSquad, err := d.Store.validateAssignee(r.Context(), ws.ID, actor, role, issue.AssigneeType, issue.AssigneeID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var req rerunRequest
	_ = httpapi.DecodeJSON(r, &req)
	spec := dispatch.JobSpec{
		WorkspaceID: ws.ID, OperativeID: op.ID, ExecutorID: op.ExecutorID,
		TicketID: id, Kind: dispatch.KindIssue, IsLeader: isSquad, ParentJobID: req.TaskID,
	}
	jobID, err := d.Dispatch.Enqueue(r.Context(), d.Store.pool(), spec)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	job, err := d.Dispatch.Store.GetJob(r.Context(), d.Store.pool(), jobID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusAccepted, toAgentTask(job))
}

// toAgentTask/toAgentTasks — dispatch.Job -> schemas.AgentTask (contract).
func toAgentTask(j dispatch.Job) map[string]any {
	m := map[string]any{
		"id": j.ID, "agent_id": j.OperativeID, "runtime_id": j.ExecutorID,
		"workspace_id": j.WorkspaceID, "status": string(j.Status), "priority": j.Priority,
		"attempt": j.Attempt, "max_attempts": j.MaxAttempts, "created_at": j.CreatedAt,
		"delivered_comment_ids": []string{}, "kind": string(j.Kind),
	}
	issueID := ""
	if j.TicketID != nil {
		issueID = *j.TicketID
	}
	m["issue_id"] = issueID
	if j.ThreadTitle != nil {
		m["thread_name"] = *j.ThreadTitle
	}
	if j.DispatchedAt != nil {
		m["dispatched_at"] = *j.DispatchedAt
	}
	if j.StartedAt != nil {
		m["started_at"] = *j.StartedAt
	}
	if j.CompletedAt != nil {
		m["completed_at"] = *j.CompletedAt
	}
	if j.Result != nil {
		m["result"] = json.RawMessage(j.Result)
	}
	if j.Error != nil {
		m["error"] = *j.Error
	}
	return m
}

func toAgentTasks(jobs []dispatch.Job) []map[string]any {
	out := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, toAgentTask(j))
	}
	return out
}

// ---------------------------------------------------------------------------
// /api/tasks/{taskId}/messages — стенограмма одного запуска
// ---------------------------------------------------------------------------

func (d *Deps) handleTaskMessages(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := d.resolveWorkspace(w, r); !ok {
		return
	}
	job, err := d.Dispatch.Store.GetJob(r.Context(), d.Store.pool(), r.PathValue("taskId"))
	if err != nil {
		httpapi.NotFound(w, "task not found")
		return
	}
	msgs, err := d.Dispatch.Store.MessagesForJob(r.Context(), d.Store.pool(), job.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		entry := map[string]any{
			"task_id": job.ID, "seq": m.Seq, "type": m.Kind, "created_at": m.CreatedAt,
		}
		if m.Tool != nil {
			entry["tool"] = *m.Tool
		}
		if m.Body != nil {
			entry["content"] = *m.Body
		}
		if len(m.Input) > 0 {
			entry["input"] = json.RawMessage(m.Input)
		}
		if m.Output != nil {
			entry["output"] = *m.Output
		}
		out = append(out, entry)
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}
