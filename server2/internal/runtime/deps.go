// Package runtime реализует тег Runtimes (docs/50-api-contract.md §5) —
// список/видимость/переименование/удаление runtime-сред (таблица
// `executors`, 003_agents.up.sql), статистику использования и активности,
// асинхронный паттерн "заявка → опрос" (self-update/model-list/
// local-skills/local-skills-import — таблица `executor_probes`), а также
// четыре read-only агрегата активности агентов воркспейса
// (agent-task-snapshot/working-agents/agent-activity-30d/agent-run-counts) и
// отмену задачи пользователем (`POST /api/tasks/{taskId}/cancel`) — эти пять
// маршрутов контракт не относит явно ни к одному домену T-027, к моменту
// начала этой сессии ни один домен их не занял (проверено по
// internal/app/stubs_gen.go).
//
// online/offline runtime — вычисляется при чтении (heartbeatTimeout от
// ex_last_seen_at), не отдельный фоновый воркер: см. Store.effectiveStatus.
// Регистрация/heartbeat/протокол /api/daemon/ws (запись в executors и
// первичное provisioning) — домен daemon (server2/internal/daemon), который
// использует этот пакет как нижний слой (daemon импортирует runtime, не
// наоборот) вместо повторной реализации SQL над executors/executor_probes.
package runtime

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/wsctx"
)

// Deps — зависимости домена runtime.
type Deps struct {
	DB        *store.Store
	Store     *Store
	Dispatch  *dispatch.Deps
	Publisher realtime.Publisher
	WSResolve *wsctx.Resolver
	Logger    *slog.Logger
}

func New(db *store.Store, dispatchDeps *dispatch.Deps, pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{
		DB:        db,
		Store:     NewStore(db),
		Dispatch:  dispatchDeps,
		Publisher: pub,
		WSResolve: wsctx.New(db),
		Logger:    logger,
	}
}

// publish — уведомление воркспейса о смене состояния рантайма/агента.
func (d *Deps) publish(workspaceID, eventType string, payload any) {
	if d.Publisher == nil || workspaceID == "" {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{Type: eventType, Payload: payload})
}

// internalErr — общий 500 контракта; вынесен в один метод, потому что
// повторялся дословно на каждой непредвиденной ошибке БД во всех
// обработчиках пакета.
func (d *Deps) internalErr(w http.ResponseWriter, err error) {
	if d.Logger != nil {
		d.Logger.Error("runtime: internal error", "err", err)
	}
	httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
}

// publishRegisterEvent — contract: почти каждая мутация Runtime публикует
// `daemon:register` с {action}, форма свободная (см. §2.1 "Daemon (эхо в
// клиентский канал)").
func (d *Deps) publishRegisterEvent(workspaceID, action string) {
	d.publish(workspaceID, "daemon:register", map[string]string{"action": action})
}

// --- резолв воркспейса/роли для маршрутов этого домена ----------------------
//
// `/api/runtimes` (без {runtimeId} в пути) резолвит воркспейс из заголовка —
// используется общий internal/wsctx (см. Deps.WSResolve выше). Маршруты с
// {runtimeId} в пути резолвят воркспейс из самого рантайма — этому пакету
// нужна своя, более узкая проверка (владелец рантайма ИЛИ owner/admin
// воркспейса, а не только "любой участник"), которой wsctx не даёт.

// runtimeAccess — актор + рантайм + его роль в воркспейсе рантайма, resolved
// одним вызовом requireRuntimeMember.
type runtimeAccess struct {
	Actor    *httpapi.Actor
	Runtime  Executor
	Role     httpapi.Role
	IsMember bool // false для агентского актора без человеческой роли (не используется этим доменом, но держит форму общей с wsctx)
}

// requireRuntimeMember: актор + рантайм существует + актор — участник
// воркспейса рантайма (owner/admin/member). Пишет 401/404/403 сама.
func (d *Deps) requireRuntimeMember(w http.ResponseWriter, r *http.Request) (runtimeAccess, bool) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return runtimeAccess{}, false
	}
	ex, err := d.Store.Get(r.Context(), r.PathValue("runtimeId"))
	if err == ErrNotFound {
		httpapi.NotFound(w, "runtime not found")
		return runtimeAccess{}, false
	}
	if err != nil {
		d.internalErr(w, err)
		return runtimeAccess{}, false
	}
	role, roleOK := d.memberRole(r.Context(), ex.WorkspaceID, actor)
	if !roleOK {
		httpapi.Forbidden(w, "not a member of this workspace")
		return runtimeAccess{}, false
	}
	return runtimeAccess{Actor: actor, Runtime: ex, Role: role, IsMember: true}, true
}

// isOwnerOrManager — вызывающий владеет рантаймом либо owner/admin
// воркспейса (contract: "владелец либо owner/admin" почти на каждом
// маршруте {runtimeId}).
func isOwnerOrManager(a runtimeAccess) bool {
	if a.Runtime.OwnerID != nil && *a.Runtime.OwnerID == a.Actor.UserID {
		return true
	}
	return a.Role == httpapi.RoleOwner || a.Role == httpapi.RoleAdmin
}

// memberRole — роль человека-участника воркспейса, либо RoleAgent для
// агента/daemon-токена, чей TaskWorkspaceID совпадает с этим воркспейсом.
func (d *Deps) memberRole(ctx context.Context, workspaceID string, actor *httpapi.Actor) (httpapi.Role, bool) {
	if !actor.IsHuman {
		if actor.TaskWorkspaceID == workspaceID {
			return httpapi.RoleAgent, true
		}
		return "", false
	}
	m, err := d.WSResolve.MemberOf(ctx, workspaceID, actor.UserID)
	if err != nil {
		return "", false
	}
	return m.Role, true
}
