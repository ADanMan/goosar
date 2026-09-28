// access.go — daemonAuth (contract §1.3): каждая ручка проверяет, что
// аутентифицированный (человек или agent/daemon-токен) имеет доступ к
// воркспейсу, к которому принадлежит рантайм/задача. Резолв самого
// воркспейса различается по маршруту ({workspaceId} в пути, {runtimeId} в
// пути, {taskId} в пути, либо только заголовки для /workspaces) — здесь
// собраны маленькие переиспользуемые хелперы вместо копирования одной и той
// же проверки членства в каждом обработчике.
package daemon

import (
	"context"
	"net/http"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/runtime"
)

// hasWorkspaceAccess — человек: участник воркспейса (owner/admin/member);
// агент/daemon-токен: воркспейс совпадает с тем, к которому актор привязан
// (TaskWorkspaceID — общее поле для обоих случаев, см. httpapi.Actor).
func (d *Deps) hasWorkspaceAccess(ctx context.Context, actor *httpapi.Actor, workspaceID string) (bool, error) {
	if !actor.IsHuman {
		return actor.TaskWorkspaceID != "" && actor.TaskWorkspaceID == workspaceID, nil
	}
	var ok bool
	err := d.DB.Pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM space_members WHERE workspace_id = $1 AND account_id = $2)`,
		workspaceID, actor.UserID).Scan(&ok)
	return ok, err
}

// requireWorkspaceAccess — актор + проверка доступа к заданному workspaceID,
// пишет 401/403 сама.
func (d *Deps) requireWorkspaceAccess(w http.ResponseWriter, r *http.Request, workspaceID string) (*httpapi.Actor, bool) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return nil, false
	}
	allowed, err := d.hasWorkspaceAccess(r.Context(), actor, workspaceID)
	if err != nil {
		d.internalErr(w, err)
		return nil, false
	}
	if !allowed {
		httpapi.Forbidden(w, "not a member of this workspace")
		return nil, false
	}
	return actor, true
}

// requireRuntimeAccess — {runtimeId} в пути: рантайм существует + актор
// имеет доступ к его воркспейсу; для daemon/task-actor дополнительно
// проверяет daemon_id, когда актор — daemon-токен, привязанный к
// конкретному daemon_id (contract §2.2 handshake: "рантайм должен
// принадлежать этому же daemon_id").
func (d *Deps) requireRuntimeAccess(w http.ResponseWriter, r *http.Request) (*httpapi.Actor, runtime.Executor, bool) {
	ex, err := d.Runtime.Store.Get(r.Context(), r.PathValue("runtimeId"))
	if err == runtime.ErrNotFound {
		httpapi.NotFound(w, "runtime not found")
		return nil, runtime.Executor{}, false
	}
	if err != nil {
		d.internalErr(w, err)
		return nil, runtime.Executor{}, false
	}
	actor, ok := d.requireWorkspaceAccess(w, r, ex.WorkspaceID)
	if !ok {
		return nil, runtime.Executor{}, false
	}
	if actor.Source == httpapi.SourceDaemon && actor.DaemonID != "" &&
		(ex.DaemonID == nil || *ex.DaemonID != actor.DaemonID) {
		httpapi.NotFound(w, "runtime does not belong to this daemon")
		return nil, runtime.Executor{}, false
	}
	return actor, ex, true
}

// requireTaskAccess — {taskId} в пути (весь жизненный цикл задачи после
// claim): задача существует + актор имеет доступ к её воркспейсу. Обычный
// вызывающий здесь — сам mat_-токен задачи (SourceTaskToken, TaskWorkspaceID
// уже проверен authn при разборе токена), но контракт разрешает и
// workspace-member/daemon-token — им отдельно проверяется членство.
func (d *Deps) requireTaskAccess(w http.ResponseWriter, r *http.Request) (*httpapi.Actor, dispatch.Job, bool) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return nil, dispatch.Job{}, false
	}
	job, err := d.Dispatch.Store.GetJob(r.Context(), d.DB.Pool, r.PathValue("taskId"))
	if err == dispatch.ErrNotFound {
		httpapi.NotFound(w, "task not found")
		return nil, dispatch.Job{}, false
	}
	if err != nil {
		d.internalErr(w, err)
		return nil, dispatch.Job{}, false
	}
	allowed, err := d.hasWorkspaceAccess(r.Context(), actor, job.WorkspaceID)
	if err != nil {
		d.internalErr(w, err)
		return nil, dispatch.Job{}, false
	}
	if !allowed {
		httpapi.NotFound(w, "task not found") // не палим наличие чужой задачи 403-ом
		return nil, dispatch.Job{}, false
	}
	return actor, job, true
}
