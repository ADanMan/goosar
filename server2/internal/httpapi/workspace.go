package httpapi

import "net/http"

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
