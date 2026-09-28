package dashboard

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// Deps — зависимости домена dashboard.
type Deps struct {
	Store     *Store
	Workspace httpapi.WorkspaceMembership
	Logger    *slog.Logger
}

func New(db *store.Store, wsStore *workspace.Store, logger *slog.Logger) *Deps {
	return &Deps{Store: NewStore(db), Workspace: wsStore.HTTPAPIMembership(), Logger: logger}
}

// Register регистрирует тег Dashboard (`/api/dashboard/**`, contract §4) —
// шесть маршрутов только на чтение, каждый со своим обработчиком ниже.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/dashboard/usage/daily", deps.handleUsageDaily)
	router.Handle(http.MethodGet, "/api/dashboard/usage/by-agent", deps.handleUsageByAgent)
	router.Handle(http.MethodGet, "/api/dashboard/agent-runtime", deps.handleAgentRuntime)
	router.Handle(http.MethodGet, "/api/dashboard/runtime/daily", deps.handleRuntimeDaily)
	router.Handle(http.MethodGet, "/api/dashboard/failures/daily", deps.handleFailuresDaily)
	router.Handle(http.MethodGet, "/api/dashboard/failures/by-agent", deps.handleFailuresByAgent)
}

// parseWindow — общие query-параметры §4: days (1..365, default 30 или 90
// для getRuntimeUsage — вне этого пакета), tz (по умолчанию UTC — см. package
// doc), project_id. exactBoundary=true — failures/by-agent (без запаса в
// один день).
func parseWindow(r *http.Request, workspaceID string, defaultDays int, exactBoundary bool) Window {
	days := defaultDays
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 365 {
			days = n
		}
	}
	tz := r.URL.Query().Get("tz")
	if tz == "" {
		tz = "UTC"
	}
	margin := 1
	if exactBoundary {
		margin = 0
	}
	since := time.Now().UTC().AddDate(0, 0, -(days + margin))
	return Window{WorkspaceID: workspaceID, Since: since, TZ: tz, ProjectID: r.URL.Query().Get("project_id")}
}

func (d *Deps) handleUsageDaily(w http.ResponseWriter, r *http.Request) {
	wsID, _, _, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	rows, err := d.Store.UsageDaily(r.Context(), parseWindow(r, wsID, 30, false))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.UsageDaily())
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleUsageByAgent(w http.ResponseWriter, r *http.Request) {
	wsID, _, _, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	rows, err := d.Store.UsageByAgent(r.Context(), parseWindow(r, wsID, 30, false))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.UsageByAgent())
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleAgentRuntime(w http.ResponseWriter, r *http.Request) {
	wsID, _, _, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	rows, err := d.Store.RunTimeByAgent(r.Context(), parseWindow(r, wsID, 30, false))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"agent_id": row.AgentID, "total_seconds": row.TotalSeconds,
			"task_count": row.TaskCount, "failed_count": row.FailedCount,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleRuntimeDaily(w http.ResponseWriter, r *http.Request) {
	wsID, _, _, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	rows, err := d.Store.RunTimeDaily(r.Context(), parseWindow(r, wsID, 30, false))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"date": row.Day.Format("2006-01-02"), "total_seconds": row.TotalSeconds,
			"task_count": row.TaskCount, "failed_count": row.FailedCount,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleFailuresDaily(w http.ResponseWriter, r *http.Request) {
	wsID, _, _, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	rows, err := d.Store.FailureDaily(r.Context(), parseWindow(r, wsID, 30, false))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"date": row.Day.Format("2006-01-02"), "failure_reason": row.FailureReason, "task_count": row.TaskCount,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleFailuresByAgent(w http.ResponseWriter, r *http.Request) {
	wsID, _, _, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	// exactBoundary=true — contract §4 "особый случай": failures/by-agent
	// использует точную границу, без запаса в один день.
	rows, err := d.Store.FailureByAgent(r.Context(), parseWindow(r, wsID, 30, true))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"agent_id": row.AgentID, "failure_reason": row.FailureReason, "task_count": row.TaskCount,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}
