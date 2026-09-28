package httpapi

import (
	"context"
	"net/http"
)

// wsSource — одно место, откуda можно взять ссылку на воркспейс (заголовок
// или query-параметр), и признак того, что значение — slug, а не id.
type wsSource struct {
	fromHeader string
	fromQuery  string
	isSlug     bool
}

// wsSourceOrder — порядок приоритета из components/parameters/WorkspaceSlugHeader:
// сначала оба варианта slug (заголовок, затем query), потом оба варианта id.
// Приоритет task-token'а (см. ResolveWorkspaceRef) обрабатывается отдельно,
// до этого списка — он вообще не смотрит на заголовки/query.
var wsSourceOrder = []wsSource{
	{fromHeader: "X-Workspace-Slug", isSlug: true},
	{fromQuery: "workspace_slug", isSlug: true},
	{fromHeader: "X-Workspace-ID", isSlug: false},
	{fromQuery: "workspace_id", isSlug: false},
}

// ResolveWorkspaceRef возвращает "сырой" селектор воркспейса запроса: либо
// slug, либо id, по приоритету контракта. Домен получает пару (значение,
// isSlug) и сам резолвит её в workspace_id через свой store — httpapi не
// знает о таблице spaces.
func ResolveWorkspaceRef(r *http.Request, actor *Actor) (value string, isSlug bool, ok bool) {
	if actor != nil && actor.Source == SourceTaskToken && actor.TaskWorkspaceID != "" {
		return actor.TaskWorkspaceID, false, true
	}
	for _, src := range wsSourceOrder {
		var v string
		if src.fromHeader != "" {
			v = r.Header.Get(src.fromHeader)
		} else {
			v = r.URL.Query().Get(src.fromQuery)
		}
		if v != "" {
			return v, src.isSlug, true
		}
	}
	return "", false, false
}

// Role — роль участника пространства (schemas.Member.role).
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// RoleAtLeast сообщает, входит ли role в allowed. Названо "AtLeast" по
// сложившейся в контракте формулировке x-roles, но по сути — точное
// членство в множестве: ролей в Goosar всего три, и контракт нигде не
// подразумевает, что owner автоматически проходит проверку "admin".
func RoleAtLeast(role Role, allowed ...Role) bool {
	for _, candidate := range allowed {
		if role == candidate {
			return true
		}
	}
	return false
}

// RoleAgent — псевдо-роль неархивного вызова из контекста задачи агента
// (Actor.IsHuman == false, x-roles контракта перечисляет её отдельно от
// owner/admin/member на многих операциях Projects/Chat/Inbox). Не входит в
// members-таблицу — членства с такой ролью не бывает.
const RoleAgent Role = "agent"

// WorkspaceMembership — то немногое, что нужно домену без {id}-пути (Projects,
// Inbox, Chat, ...), чтобы резолвить заголовок/query воркспейса в его id и
// узнать роль вызывающего в нём. Реализация обычно — тонкая обёртка над
// store домена workspace (см. internal/workspace.Store), которую держит сам
// домен-потребитель, а не httpapi (см. workspace.membershipBridge за образец
// того же приёма для realtime).
type WorkspaceMembership interface {
	ResolveWorkspaceID(ctx context.Context, ref string, isSlug bool) (id string, found bool, err error)
	MemberRole(ctx context.Context, workspaceID, userID string) (role Role, found bool, err error)
}

// RequireWorkspaceMember — общий пролог обработчиков доменов, чьи маршруты
// контракта резолвят пространство только по заголовку/query (не по {id} в
// пути): требует актора, резолвит ссылку на воркспейс (ResolveWorkspaceRef) и
// проверяет членство. Актор-агент (task-token, см. ActorSource) не ищется в
// таблице участников — его принадлежность воркспейсу уже проверена на этапе
// аутентификации токена, здесь ему возвращается RoleAgent. Сама пишет
// 400/401/403/404 и возвращает ok=false при любой неудаче — вызывающему
// остаётся только содержательная логика хендлера.
func RequireWorkspaceMember(w http.ResponseWriter, r *http.Request, m WorkspaceMembership) (workspaceID string, role Role, actor *Actor, ok bool) {
	a, aok := RequireActor(w, r)
	if !aok {
		return "", "", nil, false
	}
	ref, isSlug, has := ResolveWorkspaceRef(r, a)
	if !has {
		BadRequest(w, "workspace reference is required (X-Workspace-Slug or X-Workspace-ID)")
		return "", "", nil, false
	}
	id, found, err := m.ResolveWorkspaceID(r.Context(), ref, isSlug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return "", "", nil, false
	}
	if !found {
		NotFound(w, "workspace not found")
		return "", "", nil, false
	}
	if !a.IsHuman {
		return id, RoleAgent, a, true
	}
	role, memberOK, err := m.MemberRole(r.Context(), id, a.UserID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return "", "", nil, false
	}
	if !memberOK {
		Forbidden(w, "not a member of this workspace")
		return "", "", nil, false
	}
	return id, role, a, true
}
