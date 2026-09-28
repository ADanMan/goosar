package agent

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

// agentTaskView — components/schemas/AgentTask (additionalProperties: true;
// эта версия заполняет все поля, которые daemon-клиент реально разбирает,
// см. схему в docs/50-api-contract.yaml).
func agentTaskView(j dispatch.Job) map[string]any {
	issueID := ""
	if j.TicketID != nil {
		issueID = *j.TicketID
	}
	threadName := ""
	if j.ThreadTitle != nil {
		threadName = *j.ThreadTitle
	}
	delivered := j.DeliveredNoteIDs
	if delivered == nil {
		delivered = []string{}
	}
	return map[string]any{
		"id":                    j.ID,
		"agent_id":              j.OperativeID,
		"runtime_id":            j.ExecutorID,
		"issue_id":              issueID,
		"workspace_id":          j.WorkspaceID,
		"workspace_context":     "",
		"thread_name":           threadName,
		"status":                string(j.Status),
		"priority":              j.Priority,
		"dispatched_at":         j.DispatchedAt,
		"started_at":            j.StartedAt,
		"completed_at":          j.CompletedAt,
		"result":                rawOrNil(j.Result),
		"error":                 j.Error,
		"attempt":               j.Attempt,
		"max_attempts":          j.MaxAttempts,
		"created_at":            j.CreatedAt,
		"delivered_comment_ids": delivered,
		"kind":                  string(j.Kind),
	}
}

func rawOrNil(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func (d *Deps) handleListAgentTasks(w http.ResponseWriter, r *http.Request) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	if a.PermissionMode == "private" {
		visible, err := d.Store.CanView(r.Context(), a, vc.ViewerID, vc.IsHuman, vc.Role)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if !visible {
			httpapi.Forbidden(w, "no access to this private agent")
			return
		}
	}
	out := []map[string]any{}
	if d.Dispatch != nil {
		jobs, err := d.Dispatch.Store.RunsForOperative(r.Context(), d.DB.Pool, a.ID)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		for _, j := range jobs {
			out = append(out, agentTaskView(j))
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}
