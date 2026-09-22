package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

type SubscriberResponse struct {
	IssueID   string `json:"issue_id"`
	UserType  string `json:"user_type"`
	UserID    string `json:"user_id"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

func subscriberToResponse(s db.IssueSubscriber) SubscriberResponse {
	return SubscriberResponse{
		IssueID:   uuidToString(s.IssueID),
		UserType:  s.UserType,
		UserID:    uuidToString(s.UserID),
		Reason:    s.Reason,
		CreatedAt: timestampToString(s.CreatedAt),
	}
}

func (h *Handler) ListIssueSubscribers(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	subscribers, err := h.Queries.ListIssueSubscribers(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list subscribers")
		return
	}

	resp := make([]SubscriberResponse, len(subscribers))
	for i, s := range subscribers {
		resp[i] = subscriberToResponse(s)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SubscribeToIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)

	callerActorType, callerActorID := h.resolveActor(r, requestUserID(r), workspaceID)
	targetUserType := callerActorType
	targetUserID := callerActorID
	var req struct {
		UserID   *string `json:"user_id"`
		UserType *string `json:"user_type"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	if req.UserID != nil && *req.UserID != "" {
		targetUserID = *req.UserID
	}
	if req.UserType != nil && *req.UserType != "" {
		targetUserType = *req.UserType
	}

	if targetUserType != callerActorType || targetUserID != callerActorID {
		if !h.callerCanManageSubscriptionTarget(w, r, workspaceID, callerActorType) {
			return
		}
	}

	if !h.isWorkspaceEntity(r.Context(), targetUserType, targetUserID, workspaceID) {
		writeError(w, http.StatusForbidden, "target user is not a member of this workspace")
		return
	}

	err := h.Queries.AddIssueSubscriber(r.Context(), db.AddIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: targetUserType,
		UserID:   parseUUID(targetUserID),
		Reason:   "manual",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe")
		return
	}

	h.publish(protocol.EventSubscriberAdded, workspaceID, callerActorType, callerActorID, map[string]any{
		"issue_id":  issueID,
		"user_type": targetUserType,
		"user_id":   targetUserID,
		"reason":    "manual",
	})

	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": true})
}

func (h *Handler) UnsubscribeFromIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)

	callerActorType, callerActorID := h.resolveActor(r, requestUserID(r), workspaceID)
	targetUserType := callerActorType
	targetUserID := callerActorID
	var req struct {
		UserID   *string `json:"user_id"`
		UserType *string `json:"user_type"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	if req.UserID != nil && *req.UserID != "" {
		targetUserID = *req.UserID
	}
	if req.UserType != nil && *req.UserType != "" {
		targetUserType = *req.UserType
	}

	if targetUserType != callerActorType || targetUserID != callerActorID {
		if !h.callerCanManageSubscriptionTarget(w, r, workspaceID, callerActorType) {
			return
		}
	}

	if !h.isWorkspaceEntity(r.Context(), targetUserType, targetUserID, workspaceID) {
		writeError(w, http.StatusForbidden, "target user is not a member of this workspace")
		return
	}

	err := h.Queries.RemoveIssueSubscriber(r.Context(), db.RemoveIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: targetUserType,
		UserID:   parseUUID(targetUserID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unsubscribe")
		return
	}

	h.publish(protocol.EventSubscriberRemoved, workspaceID, callerActorType, callerActorID, map[string]any{
		"issue_id":  issueID,
		"user_type": targetUserType,
		"user_id":   targetUserID,
	})

	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": false})
}

func (h *Handler) callerCanManageSubscriptionTarget(w http.ResponseWriter, r *http.Request, workspaceID, callerActorType string) bool {
	if callerActorType != "member" {
		writeError(w, http.StatusForbidden, "only a workspace owner or admin can manage another user's subscription")
		return false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only a workspace owner or admin can manage another user's subscription")
		return false
	}
	return true
}
