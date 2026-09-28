package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

// handleTaskStatus — GET /api/daemon/tasks/{taskId}/status.
func (d *Deps) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": string(job.Status)})
}

// handleTaskStart — POST /api/daemon/tasks/{taskId}/start.
func (d *Deps) handleTaskStart(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	updated, found, err := d.Dispatch.Store.Start(r.Context(), d.DB.Pool, job.ID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if !found {
		httpapi.BadRequest(w, "task cannot transition to running from its current status")
		return
	}
	d.publish(job.WorkspaceID, "task:running", taskEventPayload(updated))
	d.respondTask(w, r, updated.ID)
}

// handleTaskWaitLocalDirectory — POST /api/daemon/tasks/{taskId}/wait-local-directory.
func (d *Deps) handleTaskWaitLocalDirectory(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = httpapi.DecodeJSON(r, &req)
	updated, found, err := d.Dispatch.Store.WaitLocalDirectory(r.Context(), d.DB.Pool, job.ID, req.Reason)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if !found {
		httpapi.BadRequest(w, "invalid transition")
		return
	}
	payload := taskEventPayload(updated)
	payload["wait_reason"] = req.Reason
	d.publish(job.WorkspaceID, "task:waiting_local_directory", payload)
	d.respondTask(w, r, updated.ID)
}

// handleTaskProgress — POST /api/daemon/tasks/{taskId}/progress.
func (d *Deps) handleTaskProgress(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	var req struct {
		Summary string `json:"summary"`
		Step    *int   `json:"step"`
		Total   *int   `json:"total"`
	}
	_ = httpapi.DecodeJSON(r, &req)
	d.publish(job.WorkspaceID, "task:progress", map[string]any{
		"task_id": job.ID, "summary": req.Summary, "step": req.Step, "total": req.Total,
	})
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleTaskComplete — POST /api/daemon/tasks/{taskId}/complete.
func (d *Deps) handleTaskComplete(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	var req dispatch.CompleteInput
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	updated, found, err := d.Dispatch.Store.Complete(r.Context(), d.DB.Pool, job.ID, req)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if !found {
		httpapi.BadRequest(w, "invalid transition")
		return
	}
	d.publish(job.WorkspaceID, "task:completed", taskEventPayload(updated))
	d.onFirstIssueCompletion(r.Context(), updated)
	d.respondTask(w, r, updated.ID)
}

// handleTaskFail — POST /api/daemon/tasks/{taskId}/fail.
func (d *Deps) handleTaskFail(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	var req dispatch.FailInput
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	updated, found, err := d.Dispatch.Store.Fail(r.Context(), d.DB.Pool, job.ID, req)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if !found {
		httpapi.WriteError(w, http.StatusInternalServerError, "failure persistence error", "")
		return
	}
	d.publish(job.WorkspaceID, "task:failed", taskEventPayload(updated))
	d.respondTask(w, r, updated.ID)
}

// handleTaskUsage — POST /api/daemon/tasks/{taskId}/usage.
func (d *Deps) handleTaskUsage(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	var req struct {
		Usage []dispatch.UsageEntry `json:"usage"`
	}
	_ = httpapi.DecodeJSON(r, &req)
	_, _ = d.Dispatch.Store.AppendUsage(r.Context(), d.DB.Pool, job.ID, req.Usage)
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleTaskMessagesPost — POST /api/daemon/tasks/{taskId}/messages.
func (d *Deps) handleTaskMessagesPost(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	var req struct {
		Messages []dispatch.MessageInput `json:"messages"`
	}
	_ = httpapi.DecodeJSON(r, &req)
	saved, err := d.Dispatch.Store.AppendMessages(r.Context(), d.DB.Pool, job.ID, req.Messages)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "failed to persist a message", "")
		return
	}
	for _, m := range saved {
		d.publish(job.WorkspaceID, "task:message", map[string]any{
			"task_id": job.ID, "issue_id": job.TicketID, "chat_session_id": job.ConvoID,
			"seq": m.Seq, "type": m.Kind, "tool": m.Tool, "content": m.Body, "input": m.Input, "output": m.Output, "created_at": m.CreatedAt,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleTaskMessagesGet — GET /api/daemon/tasks/{taskId}/messages.
func (d *Deps) handleTaskMessagesGet(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	since := 0
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			httpapi.BadRequest(w, "invalid since parameter")
			return
		}
		since = n
	}
	msgs, err := d.Dispatch.Store.ListMessagesSince(r.Context(), d.DB.Pool, job.ID, since)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, msgs)
}

// handleTaskCancelAck — POST /api/daemon/tasks/{taskId}/cancel-ack.
func (d *Deps) handleTaskCancelAck(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	if job.ConvoID != nil {
		d.publish(job.WorkspaceID, "chat:cancel_finalized", map[string]any{
			"outcome": "stopped", "chat_session_id": *job.ConvoID, "task_id": job.ID,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleTaskSession — POST /api/daemon/tasks/{taskId}/session.
func (d *Deps) handleTaskSession(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
		WorkDir   string `json:"work_dir"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || (req.SessionID == "" && req.WorkDir == "") {
		httpapi.BadRequest(w, "neither session_id nor work_dir given")
		return
	}
	found, err := d.Dispatch.Store.PinSession(r.Context(), d.DB.Pool, job.ID, req.SessionID, req.WorkDir)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if !found {
		httpapi.NotFound(w, "task not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) respondTask(w http.ResponseWriter, r *http.Request, jobID string) {
	task, ok, err := d.buildAgentTask(r.Context(), jobID, "")
	if err != nil || !ok {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, task)
}

func taskEventPayload(j dispatch.Job) map[string]any {
	ev := map[string]any{"task_id": j.ID, "agent_id": j.OperativeID, "status": string(j.Status)}
	if j.TicketID != nil {
		ev["issue_id"] = *j.TicketID
	}
	if j.ConvoID != nil {
		ev["chat_session_id"] = *j.ConvoID
	}
	return ev
}

// onFirstIssueCompletion — contract: "на первом завершении задачи по issue
// может запустить сопутствующие эффекты выполнения issue". Ни контракт, ни
// data-model не формализуют, какие именно эффекты (переход тикета в done?
// комментарий агента?) — реализовано минимально и консервативно: комментарий
// агента с результатом, только если задача issue-scoped и это первый
// завершённый запуск по этой задаче (dj_parent_job_id пуст либо цепочка была
// неудачной ранее). Решение и его границы — server2/docs/decisions.md.
func (d *Deps) onFirstIssueCompletion(ctx context.Context, job dispatch.Job) {
	if job.TicketID == nil {
		return
	}
	output := ""
	if job.Result != nil {
		var res struct {
			Output string `json:"output"`
		}
		_ = json.Unmarshal(job.Result, &res)
		output = res.Output
	}
	if output == "" {
		return
	}
	_, _ = d.DB.Pool.Exec(ctx, `
		INSERT INTO ticket_notes (ticket_id, tn_author_type, tn_author_id, tn_body, tn_kind, tn_source_dispatch_job_id)
		VALUES ($1, 'agent', $2, $3, 'comment', $4)`, *job.TicketID, job.OperativeID, output, job.ID)
}
