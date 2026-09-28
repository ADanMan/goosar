package integration

import (
	"context"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
)

// caller — актор запроса и его роль в пространстве из {id} пути.
type caller struct {
	Actor       *httpapi.Actor
	WorkspaceID string
	Role        httpapi.Role
}

// requireRole резолвит {id} пути как пространство, требует, что вызывающий
// в нём состоит, и что его роль входит в allowed; при первой неудаче сам
// пишет HTTP-ошибку и возвращает ok=false. Пространство и членство читаются
// напрямую из spaces/space_members — тот же приём, что internal/wsctx и
// internal/dashboard используют для доменов, которым не нужен весь Store
// пакета workspace целиком (см. server2/docs/decisions.md, раздел T-029).
func (d *Deps) requireRole(w http.ResponseWriter, r *http.Request, allowed ...httpapi.Role) (caller, bool) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return caller{}, false
	}
	workspaceID := r.PathValue("id")
	exists, err := d.Store.db.RowExists(r.Context(), `SELECT EXISTS(SELECT 1 FROM spaces WHERE id = $1)`, workspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return caller{}, false
	}
	if !exists {
		httpapi.NotFound(w, "workspace not found")
		return caller{}, false
	}
	role, ok := d.memberRole(r.Context(), workspaceID, actor.UserID)
	if !ok {
		httpapi.Forbidden(w, "not a member of this workspace")
		return caller{}, false
	}
	if len(allowed) > 0 && !httpapi.RoleAtLeast(role, allowed...) {
		httpapi.Forbidden(w, "insufficient permissions")
		return caller{}, false
	}
	return caller{Actor: actor, WorkspaceID: workspaceID, Role: role}, true
}

func (d *Deps) memberRole(ctx context.Context, workspaceID, accountID string) (httpapi.Role, bool) {
	var role string
	err := d.Store.db.Pool.QueryRow(ctx,
		`SELECT sm_role FROM space_members WHERE workspace_id = $1 AND account_id = $2`,
		workspaceID, accountID).Scan(&role)
	if store.IsNoRows(err) {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return httpapi.Role(role), true
}
