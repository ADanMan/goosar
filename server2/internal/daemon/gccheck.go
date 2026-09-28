// gccheck.go — существование/статус issue/chat-session/autopilot-run/task
// по id, для локальной сборки мусора на стороне демона (contract §3.7).
// Читает tickets/convos/sentinel_runs напрямую (эти домены не дают отдельного
// "внутреннего" метода под эту узкую форму ответа — решение см. decisions.md).
package daemon

import (
	"context"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
)

type gcStatusRow struct {
	Found       bool
	WorkspaceID string
	Status      string
	Timestamp   any
}

// lookupGcStatus — общий низ для "проверить одну строку table по её id и
// вернуть (workspace_id, статус-колонка, момент)", параметризованный именами
// таблицы/колонок — три из четырёх маршрутов gc-check устроены одинаково.
func (d *Deps) lookupGcStatus(ctx context.Context, sqlText string, id string) (gcStatusRow, error) {
	var row gcStatusRow
	err := d.DB.Pool.QueryRow(ctx, sqlText, id).Scan(&row.WorkspaceID, &row.Status, &row.Timestamp)
	switch {
	case store.IsNoRows(err):
		return gcStatusRow{}, nil
	case err != nil:
		return gcStatusRow{}, err
	default:
		row.Found = true
		return row, nil
	}
}

func (d *Deps) writeGcStatus(w http.ResponseWriter, r *http.Request, sqlText, timeField string) {
	row, err := d.lookupGcStatus(r.Context(), sqlText, gcTargetID(r))
	switch {
	case err != nil:
		d.internalErr(w, err)
	case !row.Found:
		httpapi.NotFound(w, "not found")
	default:
		if _, ok := d.requireWorkspaceAccess(w, r, row.WorkspaceID); !ok {
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"status": row.Status, timeField: row.Timestamp})
	}
}

// gcTargetID — маршруты этого файла используют разные имена для {id} в
// пути (issueId/sessionId/runId); список сверен с register.go.
func gcTargetID(r *http.Request) string {
	for _, name := range []string{"issueId", "sessionId", "runId"} {
		if v := r.PathValue(name); v != "" {
			return v
		}
	}
	return ""
}

// handleIssueGcCheck — GET /api/daemon/issues/{issueId}/gc-check.
func (d *Deps) handleIssueGcCheck(w http.ResponseWriter, r *http.Request) {
	d.writeGcStatus(w, r, `SELECT workspace_id, tk_status, updated_at FROM tickets WHERE id = $1`, "updated_at")
}

// handleChatSessionGcCheck — GET /api/daemon/chat-sessions/{sessionId}/gc-check.
func (d *Deps) handleChatSessionGcCheck(w http.ResponseWriter, r *http.Request) {
	d.writeGcStatus(w, r, `SELECT workspace_id, cv_status, updated_at FROM convos WHERE id = $1`, "updated_at")
}

// handleAutopilotRunGcCheck — GET /api/daemon/autopilot-runs/{runId}/gc-check.
func (d *Deps) handleAutopilotRunGcCheck(w http.ResponseWriter, r *http.Request) {
	d.writeGcStatus(w, r, `
		SELECT sen.workspace_id, srun.srun_status, srun.srun_completed_at
		FROM sentinel_runs srun JOIN sentinels sen ON sen.id = srun.sentinel_id
		WHERE srun.id = $1`, "completed_at")
}

// handleTaskGcCheck — GET /api/daemon/tasks/{taskId}/gc-check.
func (d *Deps) handleTaskGcCheck(w http.ResponseWriter, r *http.Request) {
	_, job, ok := d.requireTaskAccess(w, r)
	if !ok {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"status": string(job.Status), "completed_at": job.CompletedAt})
}

// handleBatchIssueGcCheck — POST /api/daemon/workspaces/{workspaceId}/issues/gc-check.
func (d *Deps) handleBatchIssueGcCheck(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := d.requireWorkspaceAccess(w, r, workspaceID); !ok {
		return
	}
	var req struct {
		IssueIDs []string `json:"issue_ids"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || len(req.IssueIDs) == 0 || len(req.IssueIDs) > 500 {
		httpapi.BadRequest(w, "issue_ids is required (max 500)")
		return
	}
	byID, err := d.ticketStatuses(r.Context(), workspaceID, req.IssueIDs)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"issues": mergeGcResults(req.IssueIDs, byID)})
}

func (d *Deps) ticketStatuses(ctx context.Context, workspaceID string, ids []string) (map[string]gcStatusRow, error) {
	rows, err := d.DB.Pool.Query(ctx, `
		SELECT id, tk_status, updated_at FROM tickets WHERE workspace_id = $1 AND id = ANY($2)`, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[string]gcStatusRow, len(ids))
	for rows.Next() {
		var id string
		var row gcStatusRow
		if scanErr := rows.Scan(&id, &row.Status, &row.Timestamp); scanErr != nil {
			return nil, scanErr
		}
		row.Found = true
		byID[id] = row
	}
	return byID, rows.Err()
}

func mergeGcResults(ids []string, byID map[string]gcStatusRow) []map[string]any {
	out := make([]map[string]any, len(ids))
	for i, id := range ids {
		row, known := byID[id]
		if !known {
			out[i] = map[string]any{"id": id, "found": false}
			continue
		}
		out[i] = map[string]any{"id": id, "found": true, "status": row.Status, "updated_at": row.Timestamp}
	}
	return out
}
