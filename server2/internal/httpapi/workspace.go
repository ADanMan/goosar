package httpapi

import "net/http"

// ResolveWorkspaceRef возвращает "сырой" селектор воркспейса из заголовков/
// query запроса — либо slug, либо id, в порядке приоритета, документированном
// в components/parameters/WorkspaceSlugHeader:
//
//  1. actor с Source=task_token → его TaskWorkspaceID (заголовки клиента игнорируются);
//  2. заголовок X-Workspace-Slug;
//  3. query-параметр workspace_slug;
//  4. заголовок X-Workspace-ID;
//  5. query-параметр workspace_id.
//
// Домен получает пару (значение, isSlug) и сам резолвит его в workspace_id
// через свой store (внутри пакета workspace) — httpapi не знает о таблице spaces.
func ResolveWorkspaceRef(r *http.Request, actor *Actor) (value string, isSlug bool, ok bool) {
	if actor != nil && actor.Source == SourceTaskToken && actor.TaskWorkspaceID != "" {
		return actor.TaskWorkspaceID, false, true
	}
	if v := r.Header.Get("X-Workspace-Slug"); v != "" {
		return v, true, true
	}
	if v := r.URL.Query().Get("workspace_slug"); v != "" {
		return v, true, true
	}
	if v := r.Header.Get("X-Workspace-ID"); v != "" {
		return v, false, true
	}
	if v := r.URL.Query().Get("workspace_id"); v != "" {
		return v, false, true
	}
	return "", false, false
}

// Role — роль участника в пространстве.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// RoleAtLeast — true, если role входит в allowed (точное совпадение с одной
// из ролей; в контракте роли не образуют линейную иерархию сами по себе,
// каждый эндпоинт явно перечисляет x-roles).
func RoleAtLeast(role Role, allowed ...Role) bool {
	for _, a := range allowed {
		if role == a {
			return true
		}
	}
	return false
}
