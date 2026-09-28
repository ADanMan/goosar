// handlers_activity.go — четыре read-only агрегата активности агентов
// воркспейса (contract §7) плюс POST /api/tasks/{taskId}/cancel
// (cancelTaskByUser). Ни контракт, ни docs/31-backlog.md T-027/T-028 не
// относят их явно ни к одному домену; internal/app/stubs_gen.go на начало
// этой сессии не показывал их занятыми ни task, ни chat, ни dispatch —
// решение поместить их сюда зафиксировано в decisions.md.
package runtime

import (
	"net/http"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// snapshotRow — по одной строке на активный запуск (agent-task-snapshot).
type snapshotRow struct {
	TaskID    string  `json:"task_id"`
	AgentID   string  `json:"agent_id"`
	RuntimeID string  `json:"runtime_id"`
	IssueID   *string `json:"issue_id,omitempty"`
	ChatID    *string `json:"chat_session_id,omitempty"`
	Status    string  `json:"status"`
	Kind      string  `json:"kind"`
}

func (d *Deps) handleAgentTaskSnapshot(w http.ResponseWriter, r *http.Request) {
	m, ok := d.WSResolve.RequireMember(w, r)
	if !ok {
		return
	}
	rows, err := d.DB.Pool.Query(r.Context(), `
		SELECT id, operative_id, executor_id, ticket_id, convo_id, dj_status, dj_kind
		FROM dispatch_jobs
		WHERE workspace_id = $1 AND dj_status IN ('queued','dispatched','waiting_local_directory','running')
		ORDER BY created_at DESC LIMIT 200`, m.WorkspaceID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	defer rows.Close()
	out := []snapshotRow{}
	for rows.Next() {
		var s snapshotRow
		if err := rows.Scan(&s.TaskID, &s.AgentID, &s.RuntimeID, &s.IssueID, &s.ChatID, &s.Status, &s.Kind); err != nil {
			d.internalErr(w, err)
			return
		}
		out = append(out, s)
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// workingAgentRow — агенты, у которых прямо сейчас есть выполняемая задача.
type workingAgentRow struct {
	AgentID      string `json:"agent_id"`
	RuntimeID    string `json:"runtime_id"`
	TaskID       string `json:"task_id"`
	RunningTasks int    `json:"running_tasks"`
}

func (d *Deps) handleWorkingAgents(w http.ResponseWriter, r *http.Request) {
	m, ok := d.WSResolve.RequireMember(w, r)
	if !ok {
		return
	}
	rows, err := d.DB.Pool.Query(r.Context(), `
		SELECT operative_id, executor_id, (array_agg(id ORDER BY created_at DESC))[1], COUNT(*)
		FROM dispatch_jobs
		WHERE workspace_id = $1 AND dj_status = 'running'
		GROUP BY operative_id, executor_id`, m.WorkspaceID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	defer rows.Close()
	out := []workingAgentRow{}
	for rows.Next() {
		var wr workingAgentRow
		if err := rows.Scan(&wr.AgentID, &wr.RuntimeID, &wr.TaskID, &wr.RunningTasks); err != nil {
			d.internalErr(w, err)
			return
		}
		out = append(out, wr)
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// activityDayRow — задачи по агенту по дню за последние 30 дней.
type activityDayRow struct {
	AgentID string `json:"agent_id"`
	Date    string `json:"date"`
	Count   int    `json:"count"`
}

func (d *Deps) handleAgentActivity30d(w http.ResponseWriter, r *http.Request) {
	m, ok := d.WSResolve.RequireMember(w, r)
	if !ok {
		return
	}
	rows, err := d.DB.Pool.Query(r.Context(), `
		SELECT operative_id, date_trunc('day', created_at)::date, COUNT(*)
		FROM dispatch_jobs
		WHERE workspace_id = $1 AND created_at >= now() - interval '30 days'
		GROUP BY operative_id, date_trunc('day', created_at)
		ORDER BY 2`, m.WorkspaceID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	defer rows.Close()
	out := []activityDayRow{}
	for rows.Next() {
		var a activityDayRow
		var day time.Time
		if err := rows.Scan(&a.AgentID, &day, &a.Count); err != nil {
			d.internalErr(w, err)
			return
		}
		a.Date = day.Format("2006-01-02")
		out = append(out, a)
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// runCountRow — общее число запусков по агенту (всё время).
type runCountRow struct {
	AgentID string `json:"agent_id"`
	Count   int    `json:"count"`
}

func (d *Deps) handleAgentRunCounts(w http.ResponseWriter, r *http.Request) {
	m, ok := d.WSResolve.RequireMember(w, r)
	if !ok {
		return
	}
	rows, err := d.DB.Pool.Query(r.Context(), `
		SELECT operative_id, COUNT(*) FROM dispatch_jobs WHERE workspace_id = $1 GROUP BY operative_id`, m.WorkspaceID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	defer rows.Close()
	out := []runCountRow{}
	for rows.Next() {
		var rr runCountRow
		if err := rows.Scan(&rr.AgentID, &rr.Count); err != nil {
			d.internalErr(w, err)
			return
		}
		out = append(out, rr)
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// --- POST /api/tasks/{taskId}/cancel -----------------------------------------

func (d *Deps) handleCancelTaskByUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	taskID := r.PathValue("taskId")
	job, err := d.Dispatch.Store.GetJob(r.Context(), d.DB.Pool, taskID)
	if err != nil {
		httpapi.NotFound(w, "task not found in workspace")
		return
	}
	if _, ok := d.memberRole(r.Context(), job.WorkspaceID, actor); !ok {
		httpapi.Forbidden(w, "not a member of this workspace")
		return
	}
	cancelled, found, err := d.Dispatch.CancelJob(r.Context(), d.DB.Pool, job.WorkspaceID, taskID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if !found {
		httpapi.BadRequest(w, "task cannot be cancelled in its current status")
		return
	}
	if cancelled.ConvoID != nil {
		d.publish(job.WorkspaceID, "chat:cancel_finalized", map[string]any{
			"outcome": "stopped", "chat_session_id": *cancelled.ConvoID, "task_id": taskID,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"cancelled_chat_message": nil})
}
