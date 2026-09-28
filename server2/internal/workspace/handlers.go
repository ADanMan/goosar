package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/mail"
	"github.com/adanman/goosar/server2/internal/realtime"
)

func nowUTC() time.Time { return time.Now().UTC() }

// sendInviteEmail отправляет письмо-приглашение асинхронно; ошибка отправки
// только логируется и не меняет уже отданный HTTP-ответ (см. контракт
// inviteWorkspaceMember).
func (d *Deps) sendInviteEmail(inv Invitation) {
	if d.Mailer == nil {
		return
	}
	err := d.Mailer.Send(context.Background(), mail.Message{
		To:      inv.InviteeEmail,
		Subject: "Приглашение в пространство " + inv.WorkspaceName,
		Body:    inv.InviterName + " приглашает вас в пространство " + inv.WorkspaceName,
	})
	if err != nil {
		d.Logger.Error("workspace: отправка письма-приглашения", "err", err, "invitation_id", inv.ID)
	}
}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// reservedSlugs — пробел спецификации (docs/50-api-contract.yaml не
// перечисляет конкретный список зарезервированных слов для workspace_slug_reserved,
// см. server2/docs/decisions.md): минимальный список путей верхнего уровня,
// с которыми пересечение slug реально сломало бы адресацию.
var reservedSlugs = map[string]bool{
	"api": true, "admin": true, "www": true, "app": true, "auth": true, "health": true, "ws": true,
}

type createWorkspaceRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	IssuePrefix *string `json:"issue_prefix"`
	TemplateKey *string `json:"template_key"`
}

func (d *Deps) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	if !actor.IsHuman {
		httpapi.WriteError(w, http.StatusForbidden, "workspace creation requires a human actor", "human_only")
		return
	}
	if d.DisableWorkspaceCreation {
		httpapi.WriteError(w, http.StatusForbidden, "workspace creation is disabled on this server", "workspace_creation_disabled")
		return
	}
	var req createWorkspaceRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		httpapi.BadRequest(w, "name must not be empty")
		return
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if !slugRe.MatchString(slug) {
		httpapi.BadRequest(w, "slug is invalid")
		return
	}
	if reservedSlugs[slug] {
		httpapi.WriteError(w, http.StatusBadRequest, "slug is reserved", "workspace_slug_reserved")
		return
	}
	taken, err := d.DB0().SlugTaken(r.Context(), slug)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if taken {
		httpapi.WriteError(w, http.StatusConflict, "slug is already taken", "workspace_slug_taken")
		return
	}
	prefix := ""
	if req.IssuePrefix != nil {
		prefix = strings.ToUpper(strings.TrimSpace(*req.IssuePrefix))
	}
	if prefix == "" {
		prefix = issuePrefixFromName(name)
	}
	wsRow, err := d.DB0().CreateWorkspace(r.Context(), CreateWorkspaceParams{
		Name: name, Slug: slug, Description: req.Description, Context: req.Context,
		IssuePrefix: prefix, OwnerID: actor.UserID,
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, wsRow)
}

func issuePrefixFromName(name string) string {
	fields := strings.Fields(name)
	var b strings.Builder
	for _, f := range fields {
		if b.Len() >= 3 {
			break
		}
		b.WriteByte(strings.ToUpper(f)[0])
	}
	if b.Len() == 0 {
		return "TSK"
	}
	return b.String()
}

func (d *Deps) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	list, err := d.DB0().ListForUser(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Workspace{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

// requireMember резолвит пространство из {id} пути и проверяет членство
// вызывающего; при неудаче сам пишет ошибку и возвращает ok=false.
func (d *Deps) requireMember(w http.ResponseWriter, r *http.Request) (ws Workspace, member MemberWithUser, ok bool) {
	actor, aok := httpapi.RequireActor(w, r)
	if !aok {
		return Workspace{}, MemberWithUser{}, false
	}
	id := r.PathValue("id")
	wsRow, err := d.DB0().GetWorkspace(r.Context(), id)
	if err == ErrNotFound {
		httpapi.NotFound(w, "workspace not found")
		return Workspace{}, MemberWithUser{}, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Workspace{}, MemberWithUser{}, false
	}
	m, err := d.DB0().GetMemberByUser(r.Context(), id, actor.UserID)
	if err == ErrNotFound {
		httpapi.Forbidden(w, "not a member of this workspace")
		return Workspace{}, MemberWithUser{}, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Workspace{}, MemberWithUser{}, false
	}
	return wsRow, m, true
}

func (d *Deps) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	wsRow, _, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, wsRow)
}

type updateWorkspaceRequest struct {
	Name        *string          `json:"name"`
	Description *string          `json:"description"`
	Context     *string          `json:"context"`
	Settings    *json.RawMessage `json:"settings"`
	Repos       *json.RawMessage `json:"repos"`
	IssuePrefix *string          `json:"issue_prefix"`
	AvatarURL   *string          `json:"avatar_url"`
}

func (d *Deps) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	if !httpapi.RoleAtLeast(httpapi.Role(member.Role), httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "requires owner or admin role")
		return
	}
	var req updateWorkspaceRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	patch := UpdatePatch{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			httpapi.BadRequest(w, "name must not be empty")
			return
		}
		patch.Name = &name
	}
	if req.Description != nil {
		patch.HasDesc = true
		patch.Description = req.Description
	}
	if req.Context != nil {
		patch.HasContext = true
		patch.Context = req.Context
	}
	if req.Settings != nil {
		patch.Settings = *req.Settings
	}
	if req.Repos != nil {
		patch.Repos = *req.Repos
	}
	if req.IssuePrefix != nil && *req.IssuePrefix != "" {
		up := strings.ToUpper(*req.IssuePrefix)
		patch.IssuePrefix = &up
	}
	if req.AvatarURL != nil {
		patch.AvatarURL = req.AvatarURL
	}
	updated, err := d.DB0().UpdateWorkspace(r.Context(), member.WorkspaceID, patch)
	if err == ErrNotFound {
		httpapi.NotFound(w, "workspace not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(updated.ID, realtime.Event{Type: "workspace.updated", Payload: updated})
	}
	httpapi.WriteJSON(w, http.StatusOK, updated)
}

func (d *Deps) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	if member.Role != string(httpapi.RoleOwner) {
		httpapi.Forbidden(w, "requires owner role")
		return
	}
	if err := d.DB0().DeleteWorkspace(r.Context(), member.WorkspaceID); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "workspace.deleted", Payload: map[string]string{"id": member.WorkspaceID}})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) handleGetCapabilities(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := d.requireMember(w, r); !ok {
		return
	}
	// Пространство без template_key (см. store.go: колонка ещё не введена,
	// см. decisions.md) — по контракту это валидный пустой ответ 200.
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"capabilities": []any{},
		"sample_tasks": []any{},
	})
}

// --- members -----------------------------------------------------------------

func (d *Deps) handleListMembers(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	list, err := d.DB0().ListMembers(r.Context(), member.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []MemberWithUser{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

type inviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (d *Deps) handleInviteMember(w http.ResponseWriter, r *http.Request) {
	actor, member, ok := d.requireMember(w, r)
	_ = actor
	if !ok {
		return
	}
	if !httpapi.RoleAtLeast(httpapi.Role(member.Role), httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "requires owner or admin role")
		return
	}
	var req inviteRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		httpapi.BadRequest(w, "email is required")
		return
	}
	role := req.Role
	if role == "" {
		role = "member"
	}
	if role != "member" && role != "admin" {
		httpapi.BadRequest(w, "role must be member or admin")
		return
	}

	existingMember, err := d.DB0().GetMemberByUserEmail(r.Context(), member.WorkspaceID, email)
	if err == nil && existingMember.ID != "" {
		httpapi.WriteError(w, http.StatusConflict, "already a member", "already_member")
		return
	}
	pending, err := d.DB0().PendingInvitationExists(r.Context(), member.WorkspaceID, email)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if pending {
		httpapi.WriteError(w, http.StatusConflict, "invitation already pending", "invitation_already_pending")
		return
	}
	inv, err := d.DB0().CreateInvitation(r.Context(), member.WorkspaceID, member.UserID, email, role)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "invitation.created", Payload: inv})
	}
	go d.sendInviteEmail(inv)
	httpapi.WriteJSON(w, http.StatusCreated, inv)
}

type updateMemberRequest struct {
	Role            *string `json:"role"`
	PerimeterAccess *bool   `json:"perimeter_access"`
}

func (d *Deps) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	actor, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	if !httpapi.RoleAtLeast(httpapi.Role(member.Role), httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "requires owner or admin role")
		return
	}
	var req updateMemberRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.Role == nil && req.PerimeterAccess == nil {
		httpapi.BadRequest(w, "role or perimeter_access is required")
		return
	}
	memberID := r.PathValue("memberId")
	target, err := d.DB0().GetMemberByID(r.Context(), member.WorkspaceID, memberID)
	if err == ErrNotFound {
		httpapi.NotFound(w, "member not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if (target.Role == "owner" || (req.Role != nil && *req.Role == "owner")) && member.Role != "owner" {
		httpapi.Forbidden(w, "only an owner can change an owner")
		return
	}
	if target.Role == "owner" && req.Role != nil && *req.Role != "owner" {
		n, err := d.DB0().CountOwners(r.Context(), member.WorkspaceID)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if n <= 1 {
			httpapi.WriteError(w, http.StatusBadRequest, "at least one owner must remain", "last_owner")
			return
		}
	}
	_ = actor
	updated, err := d.DB0().UpdateMember(r.Context(), member.WorkspaceID, memberID, req.Role, req.PerimeterAccess)
	if err == ErrNotFound {
		httpapi.NotFound(w, "member not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "member.updated", Payload: updated})
	}
	httpapi.WriteJSON(w, http.StatusOK, updated)
}

func (d *Deps) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	if !httpapi.RoleAtLeast(httpapi.Role(member.Role), httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "requires owner or admin role")
		return
	}
	memberID := r.PathValue("memberId")
	target, err := d.DB0().GetMemberByID(r.Context(), member.WorkspaceID, memberID)
	if err == ErrNotFound {
		httpapi.NotFound(w, "member not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if target.Role == "owner" && member.Role != "owner" {
		httpapi.Forbidden(w, "only an owner can remove an owner")
		return
	}
	if target.Role == "owner" {
		n, err := d.DB0().CountOwners(r.Context(), member.WorkspaceID)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if n <= 1 {
			httpapi.WriteError(w, http.StatusBadRequest, "at least one owner must remain", "last_owner")
			return
		}
	}
	n, err := d.DB0().RemoveMember(r.Context(), member.WorkspaceID, memberID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if n == 0 {
		httpapi.NotFound(w, "member not found")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "member.removed", Payload: map[string]string{"member_id": memberID}})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) handleLeaveWorkspace(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	if member.Role == "owner" {
		n, err := d.DB0().CountOwners(r.Context(), member.WorkspaceID)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if n <= 1 {
			httpapi.WriteError(w, http.StatusBadRequest, "the last owner cannot leave", "last_owner")
			return
		}
	}
	if _, err := d.DB0().RemoveMember(r.Context(), member.WorkspaceID, member.ID); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "member.removed", Payload: map[string]string{"member_id": member.ID}})
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- workspace-scoped invitations ------------------------------------------------

func (d *Deps) handleListWorkspaceInvitations(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	list, err := d.DB0().ListPendingForWorkspace(r.Context(), member.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Invitation{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

func (d *Deps) handleRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	if !httpapi.RoleAtLeast(httpapi.Role(member.Role), httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "requires owner or admin role")
		return
	}
	id := r.PathValue("invitationId")
	if err := d.DB0().RevokeInvitation(r.Context(), member.WorkspaceID, id); err == ErrNotFound {
		httpapi.WriteError(w, http.StatusNotFound, "invitation not found", "invitation_not_found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "invitation.revoked", Payload: map[string]string{"id": id}})
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- my invitations (tag Invitations) ---------------------------------------------

func (d *Deps) handleListMyInvitations(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	list, err := d.DB0().ListForRecipient(r.Context(), actor.UserID, actor.Email)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Invitation{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

func (d *Deps) invitationForActor(r *http.Request, actorID, actorEmail, id string) (Invitation, bool, error) {
	inv, err := d.DB0().GetInvitation(r.Context(), id)
	if err != nil {
		return Invitation{}, false, err
	}
	mine := (inv.InviteeUserID != nil && *inv.InviteeUserID == actorID) || strings.EqualFold(inv.InviteeEmail, actorEmail)
	return inv, mine, nil
}

func (d *Deps) handleGetMyInvitation(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	inv, mine, err := d.invitationForActor(r, actor.UserID, actor.Email, id)
	if err == ErrNotFound {
		httpapi.WriteError(w, http.StatusNotFound, "invitation not found", "invitation_not_found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !mine {
		httpapi.WriteError(w, http.StatusForbidden, "invitation is not yours", "invitation_not_yours")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, inv)
}

func (d *Deps) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	inv, mine, err := d.invitationForActor(r, actor.UserID, actor.Email, id)
	if err == ErrNotFound {
		httpapi.WriteError(w, http.StatusNotFound, "invitation not found", "invitation_not_found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !mine {
		httpapi.WriteError(w, http.StatusForbidden, "invitation is not yours", "invitation_not_yours")
		return
	}
	if inv.Status != "pending" {
		httpapi.WriteError(w, http.StatusBadRequest, "invitation is not pending", "invitation_not_pending")
		return
	}
	if inv.ExpiresAt.Before(nowUTC()) {
		httpapi.WriteError(w, http.StatusGone, "invitation has expired", "invitation_expired")
		return
	}
	if existing, err := d.DB0().GetMemberByUser(r.Context(), inv.WorkspaceID, actor.UserID); err == nil && existing.ID != "" {
		httpapi.WriteError(w, http.StatusConflict, "already a member", "already_member_self")
		return
	}
	if err := d.DB0().SetInvitationStatus(r.Context(), id, "accepted", true); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	member, err := d.DB0().AddMember(r.Context(), inv.WorkspaceID, actor.UserID, inv.Role)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	_, _ = d.Authn.Store.CompleteOnboarding(r.Context(), actor.UserID)
	if d.Publisher != nil {
		d.Publisher.Publish(inv.WorkspaceID, realtime.Event{Type: "member.added", Payload: member})
		d.Publisher.Publish(inv.WorkspaceID, realtime.Event{Type: "invitation.accepted", Payload: inv})
	}
	httpapi.WriteJSON(w, http.StatusOK, member)
}

func (d *Deps) handleDeclineInvitation(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	inv, mine, err := d.invitationForActor(r, actor.UserID, actor.Email, id)
	if err == ErrNotFound {
		httpapi.WriteError(w, http.StatusNotFound, "invitation not found", "invitation_not_found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !mine {
		httpapi.WriteError(w, http.StatusForbidden, "invitation is not yours", "invitation_not_yours")
		return
	}
	if inv.Status != "pending" {
		httpapi.WriteError(w, http.StatusBadRequest, "invitation is not pending", "invitation_not_pending")
		return
	}
	if err := d.DB0().SetInvitationStatus(r.Context(), id, "declined", false); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(inv.WorkspaceID, realtime.Event{Type: "invitation.declined", Payload: map[string]string{"id": id}})
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- runtime profiles ----------------------------------------------------------

type createProfileRequest struct {
	DisplayName    string          `json:"display_name"`
	ProtocolFamily string          `json:"protocol_family"`
	CommandName    string          `json:"command_name"`
	Description    *string         `json:"description"`
	FixedArgs      json.RawMessage `json:"fixed_args"`
	Enabled        *bool           `json:"enabled"`
}

func (d *Deps) handleCreateRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	if !httpapi.RoleAtLeast(httpapi.Role(member.Role), httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "requires owner or admin role")
		return
	}
	var req createProfileRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.DisplayName) == "" || strings.TrimSpace(req.ProtocolFamily) == "" ||
		strings.TrimSpace(req.CommandName) == "" || strings.ContainsAny(req.CommandName, " \x00") {
		httpapi.BadRequest(w, "display_name, protocol_family and command_name are required")
		return
	}
	taken, err := d.DB0().DisplayNameTaken(r.Context(), member.WorkspaceID, req.DisplayName, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if taken {
		httpapi.WriteError(w, http.StatusConflict, "display_name already taken", "display_name_taken")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	prof, err := d.DB0().CreateRuntimeProfile(r.Context(), CreateProfileParams{
		WorkspaceID: member.WorkspaceID, DisplayName: req.DisplayName, ProtocolFamily: req.ProtocolFamily,
		CommandName: req.CommandName, Description: req.Description, FixedArgs: req.FixedArgs,
		Enabled: enabled, CreatedBy: member.UserID,
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "daemon.register", Payload: prof})
	}
	httpapi.WriteJSON(w, http.StatusCreated, prof)
}

func (d *Deps) handleListRuntimeProfiles(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	list, err := d.DB0().ListRuntimeProfiles(r.Context(), member.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []RuntimeProfile{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"runtime_profiles": list})
}

func (d *Deps) handleGetRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	prof, err := d.DB0().GetRuntimeProfile(r.Context(), member.WorkspaceID, r.PathValue("profileId"))
	if err == ErrNotFound {
		httpapi.NotFound(w, "runtime profile not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, prof)
}

type updateProfileRequest struct {
	DisplayName *string         `json:"display_name"`
	CommandName *string         `json:"command_name"`
	Description *string         `json:"description"`
	FixedArgs   json.RawMessage `json:"fixed_args"`
	Enabled     *bool           `json:"enabled"`
}

func (d *Deps) handleUpdateRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	if !httpapi.RoleAtLeast(httpapi.Role(member.Role), httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "requires owner or admin role")
		return
	}
	profileID := r.PathValue("profileId")
	var req updateProfileRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.DisplayName != nil {
		taken, err := d.DB0().DisplayNameTaken(r.Context(), member.WorkspaceID, *req.DisplayName, profileID)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if taken {
			httpapi.WriteError(w, http.StatusConflict, "display_name already taken", "display_name_taken")
			return
		}
	}
	patch := UpdateProfileParams{DisplayName: req.DisplayName, CommandName: req.CommandName, FixedArgs: req.FixedArgs, Enabled: req.Enabled}
	if req.Description != nil {
		patch.HasDesc = true
		patch.Description = req.Description
	}
	prof, err := d.DB0().UpdateRuntimeProfile(r.Context(), member.WorkspaceID, profileID, patch)
	if err == ErrNotFound {
		httpapi.NotFound(w, "runtime profile not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "daemon.register", Payload: prof})
	}
	httpapi.WriteJSON(w, http.StatusOK, prof)
}

func (d *Deps) handleDeleteRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	_, member, ok := d.requireMember(w, r)
	if !ok {
		return
	}
	if !httpapi.RoleAtLeast(httpapi.Role(member.Role), httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "requires owner or admin role")
		return
	}
	profileID := r.PathValue("profileId")
	if _, err := d.DB0().GetRuntimeProfile(r.Context(), member.WorkspaceID, profileID); err == ErrNotFound {
		httpapi.NotFound(w, "runtime profile not found")
		return
	}
	active, err := d.DB0().HasActiveAgentsOnProfile(r.Context(), profileID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if active {
		httpapi.WriteError(w, http.StatusConflict, "profile has active agents", "profile_has_active_agents")
		return
	}
	n, err := d.DB0().DeleteRuntimeProfile(r.Context(), member.WorkspaceID, profileID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if n == 0 {
		httpapi.NotFound(w, "runtime profile not found")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "daemon.register", Payload: map[string]string{"deleted_runtime_profile_id": profileID}})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) notImplemented(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteNotImplemented(w, r)
}
