package squad

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

func (d *Deps) resolveWorkspace(w http.ResponseWriter, r *http.Request) (workspaceID string, role httpapi.Role, actor *httpapi.Actor, ok bool) {
	return httpapi.RequireWorkspaceMember(w, r, d.Workspace)
}

func canManage(sq Squad, actor *httpapi.Actor, role httpapi.Role) bool {
	if httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		return true
	}
	return actor.IsHuman && sq.CreatorID == actor.UserID
}

func (d *Deps) view(r *http.Request, sq Squad) (map[string]any, error) {
	count, preview, err := d.Store.MemberCountAndPreview(r.Context(), sq.ID)
	if err != nil {
		return nil, err
	}
	return sq.View(count, preview), nil
}

func (d *Deps) handleList(w http.ResponseWriter, r *http.Request) {
	wsID, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	squads, err := d.Store.List(r.Context(), wsID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(squads))
	for _, sq := range squads {
		v, err := d.view(r, sq)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		out = append(out, v)
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleCreate(w http.ResponseWriter, r *http.Request) {
	wsID, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		LeaderID    string  `json:"leader_id"`
		AvatarURL   *string `json:"avatar_url"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.Name == "" || req.LeaderID == "" {
		httpapi.BadRequest(w, "name and leader_id are required")
		return
	}
	if _, err := d.Store.ValidLeader(r.Context(), wsID, req.LeaderID, actor.UserID, actor.IsHuman, role); err != nil {
		writeLeaderErr(w, err)
		return
	}
	sq, err := d.Store.Create(r.Context(), CreateParams{
		WorkspaceID: wsID, Name: req.Name, Summary: req.Description, LeaderID: req.LeaderID,
		CreatorID: actor.UserID, AvatarURI: req.AvatarURL,
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	v, err := d.view(r, sq)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "squad:created", map[string]any{"squad": v})
	httpapi.WriteJSON(w, http.StatusCreated, v)
}

func writeLeaderErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrLeaderInvalid):
		httpapi.BadRequest(w, "leader_id is not an agent of this workspace")
	case errors.Is(err, ErrForbiddenLeader):
		httpapi.Forbidden(w, "caller is not allowed to use this agent as leader")
	default:
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
	}
}

func (d *Deps) loadSquad(w http.ResponseWriter, r *http.Request, wsID string) (Squad, bool) {
	sq, err := d.Store.Get(r.Context(), wsID, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "squad not found")
		return Squad{}, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Squad{}, false
	}
	return sq, true
}

func (d *Deps) handleGet(w http.ResponseWriter, r *http.Request) {
	wsID, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	sq, ok := d.loadSquad(w, r, wsID)
	if !ok {
		return
	}
	v, err := d.view(r, sq)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, v)
}

func (d *Deps) handleUpdate(w http.ResponseWriter, r *http.Request) {
	wsID, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	sq, ok := d.loadSquad(w, r, wsID)
	if !ok {
		return
	}
	if !canManage(sq, actor, role) {
		httpapi.Forbidden(w, "only the workspace owner/admin or the squad's creator can update it")
		return
	}
	var req struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		Instructions *string `json:"instructions"`
		LeaderID     *string `json:"leader_id"`
		AvatarURL    *string `json:"avatar_url"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.LeaderID != nil {
		if _, err := d.Store.ValidLeader(r.Context(), wsID, *req.LeaderID, actor.UserID, actor.IsHuman, role); err != nil {
			writeLeaderErr(w, err)
			return
		}
	}
	if err := d.Store.Update(r.Context(), wsID, sq.ID, UpdateParams{
		Name: req.Name, Summary: req.Description, Instructions: req.Instructions,
		LeaderID: req.LeaderID, AvatarURI: req.AvatarURL,
	}); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	updated, ok := d.loadSquad(w, r, wsID)
	if !ok {
		return
	}
	v, err := d.view(r, updated)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "squad:updated", map[string]any{"squad": v})
	httpapi.WriteJSON(w, http.StatusOK, v)
}

func (d *Deps) handleArchive(w http.ResponseWriter, r *http.Request) {
	wsID, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	sq, ok := d.loadSquad(w, r, wsID)
	if !ok {
		return
	}
	if !canManage(sq, actor, role) {
		httpapi.Forbidden(w, "only the workspace owner/admin or the squad's creator can archive it")
		return
	}
	archived, err := d.Store.Archive(r.Context(), wsID, sq.ID, actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !archived {
		httpapi.WriteError(w, http.StatusBadRequest, "squad is already archived", "squad_already_archived")
		return
	}
	d.notify(wsID, "squad:deleted", map[string]any{"squad_id": sq.ID, "leader_id": sq.LeaderID})
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) handleListMembers(w http.ResponseWriter, r *http.Request) {
	wsID, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	sq, ok := d.loadSquad(w, r, wsID)
	if !ok {
		return
	}
	members, err := d.Store.ListMembers(r.Context(), sq.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, members)
}

func (d *Deps) handleMemberStatus(w http.ResponseWriter, r *http.Request) {
	wsID, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	sq, ok := d.loadSquad(w, r, wsID)
	if !ok {
		return
	}
	statuses, err := d.Store.MemberStatuses(r.Context(), sq.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"members": statuses})
}

func (d *Deps) handleAddMember(w http.ResponseWriter, r *http.Request) {
	wsID, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	sq, ok := d.loadSquad(w, r, wsID)
	if !ok {
		return
	}
	if !canManage(sq, actor, role) {
		httpapi.Forbidden(w, "only the workspace owner/admin or the squad's creator can add members")
		return
	}
	var req struct {
		MemberType string `json:"member_type"`
		MemberID   string `json:"member_id"`
		Role       string `json:"role"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.MemberType == "" || req.MemberID == "" {
		httpapi.BadRequest(w, "member_type and member_id are required")
		return
	}
	m, err := d.Store.AddMember(r.Context(), sq.ID, req.MemberType, req.MemberID, req.Role)
	if errors.Is(err, ErrMemberExists) {
		httpapi.WriteError(w, http.StatusConflict, "this member is already in the squad", "squad_member_exists")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "squad:updated", map[string]any{"squad_id": sq.ID})
	httpapi.WriteJSON(w, http.StatusCreated, m)
}

func (d *Deps) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	wsID, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	sq, ok := d.loadSquad(w, r, wsID)
	if !ok {
		return
	}
	if !canManage(sq, actor, role) {
		httpapi.Forbidden(w, "only the workspace owner/admin or the squad's creator can remove members")
		return
	}
	var req struct {
		MemberType string `json:"member_type"`
		MemberID   string `json:"member_id"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.MemberType == "" || req.MemberID == "" {
		httpapi.BadRequest(w, "member_type and member_id are required")
		return
	}
	err := d.Store.RemoveMember(r.Context(), sq.ID, req.MemberType, req.MemberID)
	switch {
	case errors.Is(err, ErrCannotRemoveLeader):
		httpapi.WriteError(w, http.StatusBadRequest, "cannot remove the squad's current leader", "squad_cannot_remove_leader")
		return
	case errors.Is(err, ErrMemberNotFound):
		httpapi.NotFound(w, "this member was not found in the squad")
		return
	case err != nil:
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "squad:updated", map[string]any{"squad_id": sq.ID})
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) handleUpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	wsID, role, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	sq, ok := d.loadSquad(w, r, wsID)
	if !ok {
		return
	}
	if !canManage(sq, actor, role) {
		httpapi.Forbidden(w, "only the workspace owner/admin or the squad's creator can change member roles")
		return
	}
	var req struct {
		MemberType string `json:"member_type"`
		MemberID   string `json:"member_id"`
		Role       string `json:"role"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.MemberType == "" || req.MemberID == "" || req.Role == "" {
		httpapi.BadRequest(w, "member_type, member_id and role are required")
		return
	}
	m, err := d.Store.UpdateMemberRole(r.Context(), sq.ID, req.MemberType, req.MemberID, req.Role)
	if errors.Is(err, ErrMemberNotFound) {
		httpapi.NotFound(w, "this member was not found in the squad")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, m)
}

// --- recordSquadLeaderEvaluation ---------------------------------------------

var validEvaluationOutcomes = map[string]bool{"action": true, "no_action": true, "failed": true}

// handleSquadEvaluated — recordSquadLeaderEvaluation (contract §6, tag
// Squads): только сам агент-лидер отряда, назначенного на задачу, в рамках
// своего же активного запуска (X-Task-ID обязателен и должен принадлежать
// именно этой задаче и именно этому лидеру). Достижимо, как только authn
// (T-028, сосед A) аутентифицирует mat_-токен как агентского актора — см.
// server2/docs/decisions.md, раздел T-028. httpapi.Actor.UserID для
// агентского актора трактуется как id самого агента (operative id) — то же
// допущение, что требуется остальной части контракта, которая ссылается на
// "агента, действующего от своего токена" (см. internal/authn/agent_actor.go).
func (d *Deps) handleSquadEvaluated(w http.ResponseWriter, r *http.Request) {
	wsID, _, actor, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	if actor.IsHuman {
		httpapi.Forbidden(w, "only the squad leader agent, within its own run, may record this")
		return
	}
	taskID := r.Header.Get("X-Task-ID")
	if taskID == "" {
		httpapi.BadRequest(w, "X-Task-ID header is required")
		return
	}
	issueID := r.PathValue("id")

	var req struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || !validEvaluationOutcomes[req.Outcome] {
		httpapi.BadRequest(w, "outcome must be one of action, no_action, failed")
		return
	}

	leaderID, err := d.squadLeaderForIssue(r.Context(), wsID, issueID)
	if errors.Is(err, ErrNotFound) {
		httpapi.WriteError(w, http.StatusBadRequest, "issue is not assigned to a squad", "issue_not_squad_assigned")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if leaderID != actor.UserID {
		httpapi.Forbidden(w, "caller is not the leader of the squad assigned to this issue")
		return
	}

	job, err := dispatch.NewStore().GetJob(r.Context(), d.DB.Pool, taskID)
	if errors.Is(err, dispatch.ErrNotFound) {
		httpapi.NotFound(w, "task not found for this agent's run")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if job.OperativeID != actor.UserID || job.TicketID == nil || *job.TicketID != issueID {
		httpapi.NotFound(w, "the given task is not this leader's run on this issue")
		return
	}

	details, _ := json.Marshal(map[string]any{"outcome": req.Outcome, "reason": req.Reason})
	var id string
	var createdAt time.Time
	err = d.DB.Pool.QueryRow(r.Context(), `
		INSERT INTO ticket_activity (workspace_id, ticket_id, ta_actor_type, ta_actor_id, ta_action, ta_details)
		VALUES ($1,$2,'agent',$3,'squad_leader_evaluated',$4) RETURNING id, created_at`,
		wsID, issueID, leaderID, details).Scan(&id, &createdAt)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "activity:created", map[string]any{
		"issue_id": issueID,
		"entry": map[string]any{
			"type": "activity", "id": id, "actor_type": "agent", "actor_id": leaderID,
			"action": "squad_leader_evaluated", "details": json.RawMessage(details), "created_at": createdAt,
		},
	})
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{
		"id": id, "action": "squad_leader_evaluated", "created_at": createdAt,
	})
}

// squadLeaderForIssue — лидер отряда, назначенного на задачу issueID (или
// ErrNotFound, если задача не назначена на squad).
func (d *Deps) squadLeaderForIssue(ctx context.Context, workspaceID, issueID string) (string, error) {
	var assigneeType *string
	var squadID *string
	err := d.DB.Pool.QueryRow(ctx, `SELECT tk_assignee_type, tk_assignee_id FROM tickets
		WHERE workspace_id = $1 AND id = $2`, workspaceID, issueID).Scan(&assigneeType, &squadID)
	if err != nil {
		return "", fmt.Errorf("squad: получение назначения задачи: %w", err)
	}
	if assigneeType == nil || *assigneeType != "squad" || squadID == nil {
		return "", ErrNotFound
	}
	sq, err := d.Store.Get(ctx, workspaceID, *squadID)
	if err != nil {
		return "", err
	}
	return sq.LeaderID, nil
}
