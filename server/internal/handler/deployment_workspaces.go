// Справочник рабочих пространств деплоя для администратора: список всех
// пространств и состав участников одного из них. Закрывает разрыв между
// существующим API точечных override'ов по известному id пространства и
// отсутствием способа найти нужное пространство и пользователя. Эти чтения
// не пишут в admin_audit, и секретов в ответах нет по построению.
package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type DeploymentWorkspaceEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	MemberCount int64  `json:"member_count"`
}

type DeploymentWorkspaceMemberEntry struct {
	UserID string `json:"user_id"`
	Name   string `json:"name,omitempty"`
	Email  string `json:"email,omitempty"`
	Role   string `json:"role"`

	Deactivated bool `json:"deactivated"`
}

func (h *Handler) ListDeploymentWorkspaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDeploymentAdmin(w, r); !ok {
		return
	}
	rows, err := h.Queries.ListAllWorkspacesWithMemberCount(r.Context())
	if err != nil {
		slog.Error("deployment workspaces: list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list workspaces")
		return
	}
	entries := make([]DeploymentWorkspaceEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, DeploymentWorkspaceEntry{
			ID:          uuidToString(row.ID),
			Name:        row.Name,
			Slug:        row.Slug,
			MemberCount: row.MemberCount,
		})
	}
	writeJSON(w, http.StatusOK, entries)
}

func (h *Handler) ListDeploymentWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDeploymentAdmin(w, r); !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workspaceId"), "workspaceId")
	if !ok {
		return
	}
	if _, err := h.Queries.GetWorkspace(r.Context(), workspaceUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		slog.Error("deployment workspace members: workspace lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load workspace")
		return
	}
	rows, err := h.Queries.ListMembersWithUser(r.Context(), workspaceUUID)
	if err != nil {
		slog.Error("deployment workspace members: list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list workspace members")
		return
	}
	entries := make([]DeploymentWorkspaceMemberEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, DeploymentWorkspaceMemberEntry{
			UserID:      uuidToString(row.UserID),
			Name:        row.UserName,
			Email:       row.UserEmail,
			Role:        row.Role,
			Deactivated: row.UserDeactivatedAt.Valid,
		})
	}
	writeJSON(w, http.StatusOK, entries)
}
