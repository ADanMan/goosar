package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/logger"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/service"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

type InvitationResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	InviterID     string  `json:"inviter_id"`
	InviteeEmail  string  `json:"invitee_email"`
	InviteeUserID *string `json:"invitee_user_id"`
	Role          string  `json:"role"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	ExpiresAt     string  `json:"expires_at"`

	InviterName   string `json:"inviter_name,omitempty"`
	InviterEmail  string `json:"inviter_email,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
}

func invitationToResponse(inv db.WorkspaceInvitation) InvitationResponse {
	return InvitationResponse{
		ID:            uuidToString(inv.ID),
		WorkspaceID:   uuidToString(inv.WorkspaceID),
		InviterID:     uuidToString(inv.InviterID),
		InviteeEmail:  inv.InviteeEmail,
		InviteeUserID: uuidToPtr(inv.InviteeUserID),
		Role:          inv.Role,
		Status:        inv.Status,
		CreatedAt:     timestampToString(inv.CreatedAt),
		UpdatedAt:     timestampToString(inv.UpdatedAt),
		ExpiresAt:     timestampToString(inv.ExpiresAt),
	}
}

func (h *Handler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	role, valid := normalizeMemberRole(req.Role)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid member role")
		return
	}
	if role == "owner" {
		writeError(w, http.StatusBadRequest, "cannot invite as owner")
		return
	}

	existingUser, err := h.Queries.GetUserByEmail(r.Context(), email)
	if err == nil {
		_, memberErr := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      existingUser.ID,
			WorkspaceID: requester.WorkspaceID,
		})
		if memberErr == nil {
			writeErrorCode(w, http.StatusConflict, ErrCodeAlreadyMember, "user is already a member")
			return
		}
	}

	if err := h.Queries.ExpireStalePendingInvitations(r.Context(), db.ExpireStalePendingInvitationsParams{
		WorkspaceID:  requester.WorkspaceID,
		InviteeEmail: email,
	}); err != nil {
		slog.Warn("expire stale invitations failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "email_hash", hashEmailForLog(email))...)
		writeError(w, http.StatusInternalServerError, "failed to create invitation")
		return
	}

	_, err = h.Queries.GetPendingInvitationByEmail(r.Context(), db.GetPendingInvitationByEmailParams{
		WorkspaceID:  requester.WorkspaceID,
		InviteeEmail: email,
	})
	if err == nil {
		writeErrorCode(w, http.StatusConflict, ErrCodeInvitationAlreadyPending, "invitation already pending for this email")
		return
	}

	var inviteeUserID pgtype.UUID
	if existingUser.ID.Valid {
		inviteeUserID = existingUser.ID
	}

	inv, err := h.Queries.CreateInvitation(r.Context(), db.CreateInvitationParams{
		WorkspaceID:   requester.WorkspaceID,
		InviterID:     requester.UserID,
		InviteeEmail:  email,
		InviteeUserID: inviteeUserID,
		Role:          role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeErrorCode(w, http.StatusConflict, ErrCodeInvitationAlreadyPending, "invitation already pending for this email")
			return
		}
		slog.Warn("create invitation failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "email_hash", hashEmailForLog(email))...)
		writeError(w, http.StatusInternalServerError, "failed to create invitation")
		return
	}

	slog.Info("invitation created", append(logger.RequestAttrs(r), "invitation_id", uuidToString(inv.ID), "workspace_id", workspaceID, "email_hash", hashEmailForLog(email), "role", role)...)

	resp := invitationToResponse(inv)

	userID := requestUserID(r)
	eventPayload := map[string]any{"invitation": resp}
	var workspaceName string
	if ws, err := h.Queries.GetWorkspace(r.Context(), requester.WorkspaceID); err == nil {
		workspaceName = ws.Name
		eventPayload["workspace_name"] = ws.Name
	}
	h.publish(protocol.EventInvitationCreated, uuidToString(requester.WorkspaceID), "member", userID, eventPayload)

	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.TeamInviteSent(
		uuidToString(requester.UserID),
		uuidToString(requester.WorkspaceID),
		email,
		"email",
	))

	if h.EmailService != nil && workspaceName != "" {
		inviterName := email
		storedLang := ""
		if inviter, err := h.Queries.GetUser(r.Context(), requester.UserID); err == nil {
			inviterName = inviter.Name
			storedLang = inviter.Language.String
		}

		if existingUser.ID.Valid && existingUser.Language.String != "" {
			storedLang = existingUser.Language.String
		}
		lang := service.EmailLangForUserLanguage(storedLang)
		invID := uuidToString(inv.ID)
		go func() {
			if err := h.EmailService.SendInvitationEmail(email, inviterName, workspaceName, invID, lang); err != nil {
				slog.Warn("failed to send invitation email", "email_hash", hashEmailForLog(email), "error", err)
			}
		}()
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) ListWorkspaceInvitations(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	rows, err := h.Queries.ListPendingInvitationsByWorkspace(r.Context(), workspaceUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}

	resp := make([]InvitationResponse, len(rows))
	for i, row := range rows {
		resp[i] = InvitationResponse{
			ID:            uuidToString(row.ID),
			WorkspaceID:   uuidToString(row.WorkspaceID),
			InviterID:     uuidToString(row.InviterID),
			InviteeEmail:  row.InviteeEmail,
			InviteeUserID: uuidToPtr(row.InviteeUserID),
			Role:          row.Role,
			Status:        row.Status,
			CreatedAt:     timestampToString(row.CreatedAt),
			UpdatedAt:     timestampToString(row.UpdatedAt),
			ExpiresAt:     timestampToString(row.ExpiresAt),
			InviterName:   row.InviterName,
			InviterEmail:  row.InviterEmail,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	invitationID := chi.URLParam(r, "invitationId")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	invitationUUID, ok := parseUUIDOrBadRequest(w, invitationID, "invitation id")
	if !ok {
		return
	}

	inv, err := h.Queries.GetInvitation(r.Context(), invitationUUID)
	if err != nil || uuidToString(inv.WorkspaceID) != uuidToString(workspaceUUID) || inv.Status != "pending" {
		writeErrorCode(w, http.StatusNotFound, ErrCodeInvitationNotFound, "invitation not found")
		return
	}

	if err := h.Queries.RevokeInvitation(r.Context(), inv.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke invitation")
		return
	}

	slog.Info("invitation revoked", "invitation_id", invitationID, "workspace_id", workspaceID)

	userID := requestUserID(r)
	h.publish(protocol.EventInvitationRevoked, uuidToString(workspaceUUID), "member", userID, map[string]any{
		"invitation_id":   uuidToString(inv.ID),
		"invitee_email":   inv.InviteeEmail,
		"invitee_user_id": uuidToPtr(inv.InviteeUserID),
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetMyInvitation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	invitationID := chi.URLParam(r, "id")
	invitationUUID, ok := parseUUIDOrBadRequest(w, invitationID, "invitation id")
	if !ok {
		return
	}
	inv, err := h.Queries.GetInvitation(r.Context(), invitationUUID)
	if err != nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeInvitationNotFound, "invitation not found")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if strings.ToLower(user.Email) != inv.InviteeEmail && uuidToString(inv.InviteeUserID) != userID {
		writeErrorCode(w, http.StatusForbidden, ErrCodeInvitationNotYours, "invitation does not belong to you")
		return
	}

	resp := invitationToResponse(inv)

	if ws, err := h.Queries.GetWorkspace(r.Context(), inv.WorkspaceID); err == nil {
		resp.WorkspaceName = ws.Name
	}
	if inviter, err := h.Queries.GetUser(r.Context(), inv.InviterID); err == nil {
		resp.InviterName = inviter.Name
		resp.InviterEmail = inviter.Email
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListMyInvitations(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	rows, err := h.Queries.ListPendingInvitationsForUser(r.Context(), db.ListPendingInvitationsForUserParams{
		InviteeUserID: user.ID,
		InviteeEmail:  user.Email,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}

	resp := make([]InvitationResponse, len(rows))
	for i, row := range rows {
		resp[i] = InvitationResponse{
			ID:            uuidToString(row.ID),
			WorkspaceID:   uuidToString(row.WorkspaceID),
			InviterID:     uuidToString(row.InviterID),
			InviteeEmail:  row.InviteeEmail,
			InviteeUserID: uuidToPtr(row.InviteeUserID),
			Role:          row.Role,
			Status:        row.Status,
			CreatedAt:     timestampToString(row.CreatedAt),
			UpdatedAt:     timestampToString(row.UpdatedAt),
			ExpiresAt:     timestampToString(row.ExpiresAt),
			WorkspaceName: row.WorkspaceName,
			InviterName:   row.InviterName,
			InviterEmail:  row.InviterEmail,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	invitationID := chi.URLParam(r, "id")
	invitationUUID, ok := parseUUIDOrBadRequest(w, invitationID, "invitation id")
	if !ok {
		return
	}
	inv, err := h.Queries.GetInvitation(r.Context(), invitationUUID)
	if err != nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeInvitationNotFound, "invitation not found")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if strings.ToLower(user.Email) != inv.InviteeEmail && uuidToString(inv.InviteeUserID) != userID {
		writeErrorCode(w, http.StatusForbidden, ErrCodeInvitationNotYours, "invitation does not belong to you")
		return
	}

	if inv.Status != "pending" {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeInvitationNotPending, "invitation is not pending")
		return
	}

	if inv.ExpiresAt.Valid && inv.ExpiresAt.Time.Before(time.Now()) {
		writeErrorCode(w, http.StatusGone, ErrCodeInvitationExpired, "invitation has expired")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	accepted, err := qtx.AcceptInvitation(r.Context(), inv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}

	member, err := qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: accepted.WorkspaceID,
		UserID:      user.ID,
		Role:        accepted.Role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeErrorCode(w, http.StatusConflict, ErrCodeAlreadyMemberSelf, "you are already a member of this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create membership")
		return
	}

	h.auditMembership(r, audit.ActionWorkspaceMemberAdded, uuidToString(user.ID), uuidToString(user.ID), uuidToString(accepted.WorkspaceID))

	firstOnboardingCompletion := !user.OnboardedAt.Valid
	onboardedUser, err := qtx.MarkUserOnboarded(r.Context(), user.ID)
	if err != nil {
		slog.Warn("accept invitation: mark user onboarded failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", uuidToString(accepted.WorkspaceID))...)
		writeError(w, http.StatusInternalServerError, "failed to mark user onboarded")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}

	slog.Info("invitation accepted", "invitation_id", invitationID, "user_id", userID, "workspace_id", uuidToString(accepted.WorkspaceID))

	wsID := uuidToString(accepted.WorkspaceID)
	memberResp := memberWithUserResponse(member, user)

	eventPayload := map[string]any{"member": memberResp}
	if ws, err := h.Queries.GetWorkspace(r.Context(), accepted.WorkspaceID); err == nil {
		eventPayload["workspace_name"] = ws.Name
	}
	h.publish(protocol.EventMemberAdded, wsID, "member", userID, eventPayload)

	h.publish(protocol.EventInvitationAccepted, wsID, "member", userID, map[string]any{
		"invitation_id": uuidToString(accepted.ID),
		"member":        memberResp,
	})
	h.notifyDaemonWorkspacesChanged(userID)

	var daysSinceInvite int64
	if inv.CreatedAt.Valid {
		daysSinceInvite = int64(time.Since(inv.CreatedAt.Time).Hours() / 24)
	}
	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.TeamInviteAccepted(
		userID,
		wsID,
		daysSinceInvite,
	))
	if firstOnboardingCompletion {
		onboardedAt := ""
		if onboardedUser.OnboardedAt.Valid {
			onboardedAt = onboardedUser.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingCompleted(
			userID,
			wsID,
			analytics.OnboardingPathInviteAccept,
			onboardedAt,
			onboardedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, memberResp)
}

func (h *Handler) DeclineInvitation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	invitationID := chi.URLParam(r, "id")
	invitationUUID, ok := parseUUIDOrBadRequest(w, invitationID, "invitation id")
	if !ok {
		return
	}
	inv, err := h.Queries.GetInvitation(r.Context(), invitationUUID)
	if err != nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeInvitationNotFound, "invitation not found")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if strings.ToLower(user.Email) != inv.InviteeEmail && uuidToString(inv.InviteeUserID) != userID {
		writeErrorCode(w, http.StatusForbidden, ErrCodeInvitationNotYours, "invitation does not belong to you")
		return
	}

	if inv.Status != "pending" {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeInvitationNotPending, "invitation is not pending")
		return
	}

	declined, err := h.Queries.DeclineInvitation(r.Context(), inv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decline invitation")
		return
	}

	slog.Info("invitation declined", "invitation_id", invitationID, "user_id", userID)

	wsID := uuidToString(declined.WorkspaceID)
	h.publish(protocol.EventInvitationDeclined, wsID, "member", userID, map[string]any{
		"invitation_id": uuidToString(declined.ID),
		"invitee_email": declined.InviteeEmail,
	})

	w.WriteHeader(http.StatusNoContent)
}
