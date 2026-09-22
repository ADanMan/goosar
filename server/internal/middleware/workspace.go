package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type contextKey int

const (
	ctxKeyWorkspaceID contextKey = iota
	ctxKeyMember
)

func MemberFromContext(ctx context.Context) (db.Member, bool) {
	m, ok := ctx.Value(ctxKeyMember).(db.Member)
	return m, ok
}

func WorkspaceIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyWorkspaceID).(string)
	return id
}

func SetMemberContext(ctx context.Context, workspaceID string, member db.Member) context.Context {
	ctx = context.WithValue(ctx, ctxKeyWorkspaceID, workspaceID)
	ctx = context.WithValue(ctx, ctxKeyMember, member)
	return ctx
}

var errWorkspaceNotFound = errors.New("workspace not found")

var errWorkspaceLookupFailed = errors.New("workspace lookup failed")

func writeLookupUnavailable(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "30")
	writeError(w, http.StatusServiceUnavailable, "workspace lookup temporarily unavailable")
}

func ResolveWorkspaceIDFromRequest(r *http.Request, queries *db.Queries) string {

	if r.Header.Get("X-Actor-Source") == "task_token" {
		return r.Header.Get("X-Workspace-ID")
	}
	if id := WorkspaceIDFromContext(r.Context()); id != "" {
		return id
	}
	if slug := r.Header.Get("X-Workspace-Slug"); slug != "" {
		if ws, err := queries.GetWorkspaceBySlug(r.Context(), slug); err == nil {
			return util.UUIDToString(ws.ID)
		}
	}
	if slug := r.URL.Query().Get("workspace_slug"); slug != "" {
		if ws, err := queries.GetWorkspaceBySlug(r.Context(), slug); err == nil {
			return util.UUIDToString(ws.ID)
		}
	}
	if id := r.Header.Get("X-Workspace-ID"); id != "" {
		return id
	}
	return r.URL.Query().Get("workspace_id")
}

type workspaceResolver func(r *http.Request) (string, error)

func resolveWorkspaceUUID(queries *db.Queries) workspaceResolver {
	return func(r *http.Request) (string, error) {

		if r.Header.Get("X-Actor-Source") == "task_token" {
			id := r.Header.Get("X-Workspace-ID")
			if id == "" {
				return "", errWorkspaceNotFound
			}
			return id, nil
		}

		if slug := r.URL.Query().Get("workspace_slug"); slug != "" {
			ws, err := queries.GetWorkspaceBySlug(r.Context(), slug)
			if errors.Is(err, pgx.ErrNoRows) {
				return "", errWorkspaceNotFound
			}
			if err != nil {
				return "", errWorkspaceLookupFailed
			}
			return util.UUIDToString(ws.ID), nil
		}
		if slug := r.Header.Get("X-Workspace-Slug"); slug != "" {
			ws, err := queries.GetWorkspaceBySlug(r.Context(), slug)
			if errors.Is(err, pgx.ErrNoRows) {
				return "", errWorkspaceNotFound
			}
			if err != nil {
				return "", errWorkspaceLookupFailed
			}
			return util.UUIDToString(ws.ID), nil
		}

		if id := r.URL.Query().Get("workspace_id"); id != "" {
			return id, nil
		}
		if id := r.Header.Get("X-Workspace-ID"); id != "" {
			return id, nil
		}
		return "", nil
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	body := map[string]string{"error": msg}
	if rid := w.Header().Get("X-Request-ID"); rid != "" {
		body["request_id"] = rid
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func RequireWorkspaceMember(queries *db.Queries) func(http.Handler) http.Handler {
	return buildMiddleware(queries, resolveWorkspaceUUID(queries), nil)
}

func RequireWorkspaceRole(queries *db.Queries, roles ...string) func(http.Handler) http.Handler {
	return buildMiddleware(queries, resolveWorkspaceUUID(queries), roles)
}

func RequireWorkspaceMemberFromURL(queries *db.Queries, param string) func(http.Handler) http.Handler {
	return buildMiddleware(queries, func(r *http.Request) (string, error) {
		id := chi.URLParam(r, param)
		if id == "" {
			return "", nil
		}
		return id, nil
	}, nil)
}

func RequireWorkspaceRoleFromURL(queries *db.Queries, param string, roles ...string) func(http.Handler) http.Handler {
	return buildMiddleware(queries, func(r *http.Request) (string, error) {
		id := chi.URLParam(r, param)
		if id == "" {
			return "", nil
		}
		return id, nil
	}, roles)
}

func buildMiddleware(queries *db.Queries, resolve workspaceResolver, roles []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			workspaceID, resolveErr := resolve(r)
			if errors.Is(resolveErr, errWorkspaceLookupFailed) {
				writeLookupUnavailable(w)
				return
			}
			if resolveErr != nil {
				writeError(w, http.StatusNotFound, "workspace not found")
				return
			}
			if workspaceID == "" {
				writeError(w, http.StatusBadRequest, "workspace_id or workspace_slug is required")
				return
			}

			if r.Header.Get("X-Actor-Source") == "task_token" {
				bound := r.Header.Get("X-Workspace-ID")
				if bound == "" || workspaceID != bound {
					writeError(w, http.StatusForbidden, "task token is bound to a different workspace")
					return
				}
			}

			userID := r.Header.Get("X-User-ID")
			if userID == "" {
				writeError(w, http.StatusUnauthorized, "user not authenticated")
				return
			}

			userUUID, err := util.ParseUUID(userID)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "user not authenticated")
				return
			}
			wsUUID, err := util.ParseUUID(workspaceID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid workspace_id")
				return
			}
			member, err := queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
				UserID:      userUUID,
				WorkspaceID: wsUUID,
			})
			if err != nil {

				if errors.Is(err, pgx.ErrNoRows) {
					writeError(w, http.StatusNotFound, "workspace not found")
					return
				}
				writeLookupUnavailable(w)
				return
			}

			if len(roles) > 0 {
				allowed := false
				for _, role := range roles {
					if member.Role == role {
						allowed = true
						break
					}
				}
				if !allowed {
					writeError(w, http.StatusForbidden, "insufficient permissions")
					return
				}
			}

			ctx := SetMemberContext(r.Context(), workspaceID, member)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
