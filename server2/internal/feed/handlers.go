package feed

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
)

// requireHumanMember — пролог для всех операций Inbox/NotificationPreferences:
// x-roles контракта на этих путях — только owner/admin/member (в отличие от
// Projects/Chat, здесь "agent" не входит в список), поэтому неархивный
// вызов task-token отклоняется как forbidden.
func (d *Deps) requireHumanMember(w http.ResponseWriter, r *http.Request) (workspaceID, accountID string, ok bool) {
	wsID, role, actor, rok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !rok {
		return "", "", false
	}
	if role == httpapi.RoleAgent {
		httpapi.Forbidden(w, "this endpoint is only available to human members")
		return "", "", false
	}
	return wsID, actor.UserID, true
}

func (d *Deps) handleListInbox(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	list, err := d.Store.ListActive(r.Context(), workspaceID, accountID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Alert{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

func (d *Deps) handleListArchivedInbox(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	list, err := d.Store.ListArchived(r.Context(), workspaceID, accountID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Alert{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

func (d *Deps) handleCountUnread(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	n, err := d.Store.CountUnread(r.Context(), workspaceID, accountID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"count": n})
}

func (d *Deps) handleUnreadSummary(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	if !actor.IsHuman {
		httpapi.Forbidden(w, "this endpoint is only available to human members")
		return
	}
	list, err := d.Store.UnreadSummary(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []WorkspaceUnread{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

func (d *Deps) publishBatch(workspaceID, event, accountID string, count int) {
	if d.Publisher == nil {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{Type: event, Payload: map[string]any{"recipient_id": accountID, "count": count}})
}

func (d *Deps) handleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	n, err := d.Store.MarkAllRead(r.Context(), workspaceID, accountID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publishBatch(workspaceID, "inbox:batch-read", accountID, n)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"count": n})
}

func (d *Deps) handleArchiveAll(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	n, err := d.Store.ArchiveAll(r.Context(), workspaceID, accountID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publishBatch(workspaceID, "inbox:batch-archived", accountID, n)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"count": n})
}

func (d *Deps) handleArchiveAllRead(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	n, err := d.Store.ArchiveAllRead(r.Context(), workspaceID, accountID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publishBatch(workspaceID, "inbox:batch-archived", accountID, n)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"count": n})
}

func (d *Deps) handleArchiveCompleted(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	n, err := d.Store.ArchiveCompleted(r.Context(), workspaceID, accountID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publishBatch(workspaceID, "inbox:batch-archived", accountID, n)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"count": n})
}

func (d *Deps) handleMarkItemRead(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	a, err := d.Store.MarkRead(r.Context(), workspaceID, accountID, r.PathValue("id"))
	if err == ErrNotFound {
		httpapi.WriteError(w, http.StatusNotFound, "inbox item not found", "not_found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "inbox:read", Payload: map[string]any{"item_id": a.ID, "recipient_id": accountID}})
	}
	httpapi.WriteJSON(w, http.StatusOK, a)
}

func (d *Deps) handleArchiveItem(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	a, err := d.Store.Archive(r.Context(), workspaceID, accountID, r.PathValue("id"))
	if err == ErrNotFound {
		httpapi.WriteError(w, http.StatusNotFound, "inbox item not found", "not_found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		payload := map[string]any{"item_id": a.ID, "recipient_id": accountID}
		if a.IssueID != nil {
			payload["issue_id"] = *a.IssueID
		}
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "inbox:archived", Payload: payload})
	}
	httpapi.WriteJSON(w, http.StatusOK, a)
}

func (d *Deps) handleUnarchiveItem(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	a, err := d.Store.Unarchive(r.Context(), workspaceID, accountID, r.PathValue("id"))
	if err == ErrNotFound {
		httpapi.WriteError(w, http.StatusNotFound, "inbox item not found", "not_found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		payload := map[string]any{"item_id": a.ID, "recipient_id": accountID}
		if a.IssueID != nil {
			payload["issue_id"] = *a.IssueID
		}
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "inbox:unarchived", Payload: payload})
	}
	httpapi.WriteJSON(w, http.StatusOK, a)
}

// --- notification preferences ---------------------------------------------------

func (d *Deps) handleGetPrefs(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	prefs, err := d.Store.GetPrefs(r.Context(), workspaceID, accountID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"workspace_id": workspaceID, "preferences": prefs})
}

type prefsRequest struct {
	Preferences map[string]string `json:"preferences"`
}

func validatePrefs(p map[string]string) bool {
	for k, v := range p {
		if !ValidGroups[k] || !ValidGroupValues[v] {
			return false
		}
	}
	return true
}

func (d *Deps) handlePatchPrefs(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	var req prefsRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.Preferences == nil {
		httpapi.WriteError(w, http.StatusBadRequest, "preferences is required", "invalid_request")
		return
	}
	if !validatePrefs(req.Preferences) {
		httpapi.WriteError(w, http.StatusBadRequest, "preferences contains an unknown group or value", "invalid_request")
		return
	}
	prefs, err := d.Store.MergePrefs(r.Context(), workspaceID, accountID, req.Preferences)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"workspace_id": workspaceID, "preferences": prefs})
}

func (d *Deps) handlePutPrefs(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := d.requireHumanMember(w, r)
	if !ok {
		return
	}
	var req prefsRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.Preferences == nil {
		httpapi.WriteError(w, http.StatusBadRequest, "preferences is required", "invalid_request")
		return
	}
	if !validatePrefs(req.Preferences) {
		httpapi.WriteError(w, http.StatusBadRequest, "preferences contains an unknown group or value", "invalid_request")
		return
	}
	prefs, err := d.Store.ReplacePrefs(r.Context(), workspaceID, accountID, req.Preferences)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"workspace_id": workspaceID, "preferences": prefs})
}
