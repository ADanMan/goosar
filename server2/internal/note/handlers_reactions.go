package note

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
)

// readEmoji декодирует {"emoji": "..."} и требует непустое значение — общий
// первый шаг всех четырёх реакционных обработчиков ниже, вынесенный отдельно
// вместо четырёх копий одного и того же decode+validate.
func readEmoji(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Emoji string `json:"emoji"`
	}
	if err := httpapi.DecodeJSON(r, &body); err != nil || body.Emoji == "" {
		httpapi.BadRequest(w, "emoji is required")
		return "", false
	}
	return body.Emoji, true
}

// --- comment reactions ---------------------------------------------------------

func (d *Deps) handleAddCommentReaction(w http.ResponseWriter, r *http.Request) {
	comment, ticket, actorID, ok := d.commentAccess(w, r)
	emoji, emojiOK := readEmoji(w, r)
	if !ok || !emojiOK {
		return
	}
	re, err := d.Store.AddCommentReaction(r.Context(), comment.ID, "member", actorID, emoji)
	switch {
	case err == ErrNotFound:
		httpapi.NotFound(w, "comment not found")
		return
	case err != nil:
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(ticket.WorkspaceID, "reaction:added", map[string]any{
		"reaction": re, "issue_id": ticket.ID, "issue_title": ticket.Headline, "issue_status": ticket.Status,
		"comment_id": comment.ID, "comment_author_type": comment.AuthorType, "comment_author_id": comment.AuthorID,
	})
	httpapi.WriteJSON(w, http.StatusCreated, re)
}

func (d *Deps) handleRemoveCommentReaction(w http.ResponseWriter, r *http.Request) {
	comment, ticket, actorID, ok := d.commentAccess(w, r)
	emoji, emojiOK := readEmoji(w, r)
	if !ok || !emojiOK {
		return
	}
	if err := d.Store.RemoveCommentReaction(r.Context(), comment.ID, "member", actorID, emoji); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(ticket.WorkspaceID, "reaction:removed", map[string]any{
		"comment_id": comment.ID, "issue_id": ticket.ID, "emoji": emoji, "actor_type": "member", "actor_id": actorID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// --- issue reactions -------------------------------------------------------------

func (d *Deps) requireTicketForReaction(w http.ResponseWriter, r *http.Request) (TicketInfo, string, bool) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return TicketInfo{}, "", false
	}
	ticket, err := d.Store.GetTicketInfo(r.Context(), member.WorkspaceID, r.PathValue("id"))
	switch {
	case err == ErrNotFound:
		httpapi.NotFound(w, "issue not found")
		return TicketInfo{}, "", false
	case err != nil:
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return TicketInfo{}, "", false
	}
	return ticket, member.UserID, true
}

func (d *Deps) handleAddIssueReaction(w http.ResponseWriter, r *http.Request) {
	ticket, userID, ok := d.requireTicketForReaction(w, r)
	emoji, emojiOK := readEmoji(w, r)
	if !ok || !emojiOK {
		return
	}
	re, err := d.Store.AddIssueReaction(r.Context(), ticket.ID, "member", userID, emoji)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(ticket.WorkspaceID, "issue_reaction:added", map[string]any{
		"reaction": re, "issue_id": ticket.ID, "issue_title": ticket.Headline, "issue_status": ticket.Status,
		"creator_type": "member", "creator_id": userID,
	})
	httpapi.WriteJSON(w, http.StatusCreated, re)
}

func (d *Deps) handleRemoveIssueReaction(w http.ResponseWriter, r *http.Request) {
	ticket, userID, ok := d.requireTicketForReaction(w, r)
	emoji, emojiOK := readEmoji(w, r)
	if !ok || !emojiOK {
		return
	}
	if err := d.Store.RemoveIssueReaction(r.Context(), ticket.ID, "member", userID, emoji); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(ticket.WorkspaceID, "issue_reaction:removed", map[string]any{
		"issue_id": ticket.ID, "emoji": emoji, "actor_type": "member", "actor_id": userID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// publish — обёртка над Publisher.Publish, которая сама проверяет nil (тест
// double и dev-запуск без /ws подписчиков не обязаны заводить хаб).
func (d *Deps) publish(workspaceID, eventType string, payload any) {
	if d.Publisher == nil {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{Type: eventType, Payload: payload})
}
