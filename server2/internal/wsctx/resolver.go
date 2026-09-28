// Package wsctx помогает доменам, у которых воркспейс не идёт в пути URL
// (задачи, метки, свойства, вложения, закрепления — везде контекст только в
// заголовках/query, см. docs/50-api-contract.md §1.4 "Резолв workspace"),
// резолвить его и проверять членство вызывающего одним вызовом. Домен
// workspace резолвит пространство иначе (прямо из {id} в пути) и своего
// аналога этому пакету не имеет — здесь минимальный, независимый от него
// путь до тех же двух таблиц (spaces/space_members), нужный нескольким
// доменам T-027 параллельно.
package wsctx

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
)

// ErrNotFound — воркспейс из заголовка/query не существует.
var ErrNotFound = errors.New("wsctx: пространство не найдено")

// ErrForbidden — вызывающий не состоит участником резолвленного пространства.
var ErrForbidden = errors.New("wsctx: не участник пространства")

// Member — воркспейс + роль вызывающего в нём, минимум, нужный обработчикам.
type Member struct {
	WorkspaceID string
	UserID      string
	Role        httpapi.Role
}

// Resolver резолвит пространство по заголовкам запроса и членство в нём.
type Resolver struct{ db *store.Store }

func New(db *store.Store) *Resolver { return &Resolver{db: db} }

func (r *Resolver) workspaceIDByRef(ctx context.Context, value string, isSlug bool) (string, error) {
	var (
		id  string
		err error
	)
	if isSlug {
		err = r.db.Pool.QueryRow(ctx, `SELECT id FROM spaces WHERE ws_slug = $1`, value).Scan(&id)
	} else {
		err = r.db.Pool.QueryRow(ctx, `SELECT id FROM spaces WHERE id = $1`, value).Scan(&id)
	}
	if store.IsNoRows(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("wsctx: резолв пространства: %w", err)
	}
	return id, nil
}

// MemberOf возвращает роль accountID в workspaceID, либо ErrForbidden, если
// он не состоит участником.
func (r *Resolver) MemberOf(ctx context.Context, workspaceID, accountID string) (Member, error) {
	var role string
	err := r.db.Pool.QueryRow(ctx, `
		SELECT sm_role FROM space_members WHERE workspace_id = $1 AND account_id = $2`,
		workspaceID, accountID).Scan(&role)
	if store.IsNoRows(err) {
		return Member{}, ErrForbidden
	}
	if err != nil {
		return Member{}, fmt.Errorf("wsctx: проверка членства: %w", err)
	}
	return Member{WorkspaceID: workspaceID, UserID: accountID, Role: httpapi.Role(role)}, nil
}

// RequireMember — актор из контекста + пространство из заголовков + его
// членство в нём, одним вызовом; при любой неудаче сам пишет HTTP-ошибку
// контракта и возвращает ok=false, ровно как workspace.requireMember делает
// для маршрутов, где пространство приходит из {id} в пути.
func (r *Resolver) RequireMember(w http.ResponseWriter, req *http.Request) (Member, bool) {
	actor, ok := httpapi.RequireActor(w, req)
	if !ok {
		return Member{}, false
	}
	value, isSlug, ok := httpapi.ResolveWorkspaceRef(req, actor)
	if !ok {
		httpapi.BadRequest(w, "workspace context is required (X-Workspace-Slug/X-Workspace-ID)")
		return Member{}, false
	}
	wsID, err := r.workspaceIDByRef(req.Context(), value, isSlug)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "workspace not found")
		return Member{}, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Member{}, false
	}
	m, err := r.MemberOf(req.Context(), wsID, actor.UserID)
	if errors.Is(err, ErrForbidden) {
		httpapi.Forbidden(w, "not a member of this workspace")
		return Member{}, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Member{}, false
	}
	return m, true
}
