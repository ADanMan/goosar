package note

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/asset"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
)

// commentEventPayload — форма x-events для comment:created/updated/deleted
// (contract: "{ comment, issue_title, issue_assignee_type, issue_assignee_id, issue_status }").
func commentEventPayload(c Comment, t TicketInfo) map[string]any {
	return map[string]any{
		"comment": c, "issue_title": t.Headline, "issue_assignee_type": t.AssigneeType,
		"issue_assignee_id": t.AssigneeID, "issue_status": t.Status,
	}
}

// attachExtras заполняет реакции/вложения комментария; контракт требует
// непустые (не null) массивы даже когда список пуст, поэтому nil от Store
// нормализуется здесь в одном месте, а не в каждом обработчике по отдельности.
func (d *Deps) attachExtras(ctx context.Context, c *Comment) {
	if reactions, err := d.Store.ListCommentReactions(ctx, c.ID); err == nil {
		c.Reactions = reactions
	}
	if c.Reactions == nil {
		c.Reactions = []Reaction{}
	}
	if d.Assets != nil {
		if atts, err := d.Assets.ListForComment(ctx, c.ID); err == nil {
			c.Attachments = atts
		}
	}
	if c.Attachments == nil {
		c.Attachments = []asset.Attachment{}
	}
}

type createCommentRequest struct {
	Content          string   `json:"content"`
	Type             string   `json:"type"`
	ParentID         *string  `json:"parent_id"`
	AttachmentIDs    []string `json:"attachment_ids"`
	SuppressAgentIDs []string `json:"suppress_agent_ids"`
}

func toSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func (d *Deps) handleTriggerPreview(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	issueID := r.PathValue("id")
	ticket, err := d.Store.GetTicketInfo(r.Context(), member.WorkspaceID, issueID)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "issue not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	var req struct {
		Content          string  `json:"content"`
		ParentID         *string `json:"parent_id"`
		EditingCommentID *string `json:"editing_comment_id"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"agents": []any{}, "blocked": []any{}})
		return
	}
	targets, err := d.resolveTargets(r.Context(), ticket, req.Content, req.ParentID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	outcomes := d.evaluateTargets(r.Context(), ticket, targets, member.UserID, member.Role, nil, "")
	var agents, blocked []any
	for i, o := range outcomes {
		if o.Status == "blocked" {
			blocked = append(blocked, o)
			continue
		}
		agents = append(agents, map[string]any{
			"id": o.TargetID, "source": targets[i].source, "reason": o.ReasonCode,
		})
	}
	if agents == nil {
		agents = []any{}
	}
	if blocked == nil {
		blocked = []any{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"agents": agents, "blocked": blocked})
}

func (d *Deps) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	issueID := r.PathValue("id")
	ticket, err := d.Store.GetTicketInfo(r.Context(), member.WorkspaceID, issueID)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "issue not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	var req createCommentRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	content := req.Content
	if strings.TrimSpace(content) == "" {
		httpapi.BadRequest(w, "content is required")
		return
	}
	if req.ParentID != nil {
		if _, err := d.Store.GetCommentInTicket(r.Context(), issueID, *req.ParentID); errors.Is(err, ErrNotFound) {
			httpapi.BadRequest(w, "parent_id is not a comment of this issue")
			return
		} else if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
	}

	comment, err := d.Store.CreateComment(r.Context(), CreateCommentParams{
		TicketID: issueID, AuthorType: "member", AuthorID: member.UserID,
		Content: content, Kind: "comment", ParentID: req.ParentID,
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if len(req.AttachmentIDs) > 0 && d.Assets != nil {
		if err := d.Assets.AttachToComment(r.Context(), member.WorkspaceID, issueID, comment.ID, req.AttachmentIDs); err != nil {
			d.Logger.Warn("note: привязка вложений к комментарию", "err", err, "comment_id", comment.ID)
		}
	}
	d.attachExtras(r.Context(), &comment)

	if !isNoteComment(content) {
		targets, _ := d.resolveTargets(r.Context(), ticket, content, req.ParentID)
		outcomes := d.evaluateTargets(r.Context(), ticket, targets, member.UserID, member.Role, toSet(req.SuppressAgentIDs), comment.ID)
		if outcomes != nil {
			comment.TriggerOutcomes = outcomes
		}
	}

	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "comment:created", Payload: commentEventPayload(comment, ticket)})
	}
	httpapi.WriteJSON(w, http.StatusCreated, comment)
}

// commentAccess резолвит /api/comments/{commentId}: комментарий, его тикет и
// членство вызывающего в воркспейсе этого тикета (эти маршруты не несут
// {issueId} в пути — воркспейс определяется через сам комментарий, как и в
// internal/asset для /api/attachments/{id}).
func (d *Deps) commentAccess(w http.ResponseWriter, r *http.Request) (Comment, TicketInfo, string, bool) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return Comment{}, TicketInfo{}, "", false
	}
	id := r.PathValue("commentId")
	comment, err := d.Store.GetComment(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "comment not found")
		return Comment{}, TicketInfo{}, "", false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Comment{}, TicketInfo{}, "", false
	}
	wsID, err := d.Store.ticketWorkspaceID(r.Context(), comment.IssueID)
	if err != nil {
		httpapi.NotFound(w, "comment not found")
		return Comment{}, TicketInfo{}, "", false
	}
	if _, err := d.Resolver.MemberOf(r.Context(), wsID, actor.UserID); err != nil {
		httpapi.NotFound(w, "comment not found")
		return Comment{}, TicketInfo{}, "", false
	}
	ticket, err := d.Store.GetTicketInfo(r.Context(), wsID, comment.IssueID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Comment{}, TicketInfo{}, "", false
	}
	d.attachExtras(r.Context(), &comment)
	return comment, ticket, actor.UserID, true
}

func canEditComment(comment Comment, actorID string, role httpapi.Role) bool {
	if comment.AuthorType == "member" && comment.AuthorID == actorID {
		return true
	}
	return httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin)
}

type updateCommentRequest struct {
	Content          string   `json:"content"`
	AttachmentIDs    []string `json:"attachment_ids"`
	HasAttachmentIDs bool     `json:"-"`
	SuppressAgentIDs []string `json:"suppress_agent_ids"`
}

func (d *Deps) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	comment, ticket, actorID, ok := d.commentAccess(w, r)
	if !ok {
		return
	}
	member, err := d.Resolver.MemberOf(r.Context(), ticket.WorkspaceID, actorID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !canEditComment(comment, actorID, member.Role) {
		httpapi.Forbidden(w, "only the comment's author or an owner/admin may edit it")
		return
	}
	var raw map[string]json.RawMessage
	if err := httpapi.DecodeJSON(r, &raw); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	var req updateCommentRequest
	if v, ok := raw["content"]; ok {
		_ = json.Unmarshal(v, &req.Content)
	}
	if v, ok := raw["attachment_ids"]; ok {
		req.HasAttachmentIDs = true
		_ = json.Unmarshal(v, &req.AttachmentIDs)
	}
	if v, ok := raw["suppress_agent_ids"]; ok {
		_ = json.Unmarshal(v, &req.SuppressAgentIDs)
	}
	if strings.TrimSpace(req.Content) == "" {
		httpapi.BadRequest(w, "content is required")
		return
	}
	contentChanged := req.Content != comment.Content
	updated, err := d.Store.UpdateCommentContent(r.Context(), comment.ID, req.Content)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if req.HasAttachmentIDs && d.Assets != nil {
		if err := d.Assets.AttachToComment(r.Context(), ticket.WorkspaceID, ticket.ID, updated.ID, req.AttachmentIDs); err != nil {
			d.Logger.Warn("note: обновление вложений комментария", "err", err, "comment_id", updated.ID)
		}
	}
	d.attachExtras(r.Context(), &updated)
	if contentChanged && !isNoteComment(updated.Content) {
		// Отмена уже запущенных агентов этим комментарием-триггером и
		// повторный запуск — часть подсистемы задач/dispatch (см.
		// server2/docs/decisions.md): здесь заново прогоняется только
		// определение целей для нового текста.
		targets, _ := d.resolveTargets(r.Context(), ticket, updated.Content, updated.ParentID)
		updated.TriggerOutcomes = d.evaluateTargets(r.Context(), ticket, targets, actorID, member.Role, toSet(req.SuppressAgentIDs), updated.ID)
	}
	if d.Publisher != nil {
		d.Publisher.Publish(ticket.WorkspaceID, realtime.Event{Type: "comment:updated", Payload: map[string]any{"comment": updated}})
	}
	httpapi.WriteJSON(w, http.StatusOK, updated)
}

func (d *Deps) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	comment, ticket, actorID, ok := d.commentAccess(w, r)
	if !ok {
		return
	}
	member, err := d.Resolver.MemberOf(r.Context(), ticket.WorkspaceID, actorID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !canEditComment(comment, actorID, member.Role) {
		httpapi.Forbidden(w, "only the comment's author or an owner/admin may delete it")
		return
	}
	if err := d.Store.DeleteComment(r.Context(), comment.ID); err != nil && !errors.Is(err, ErrNotFound) {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(ticket.WorkspaceID, realtime.Event{Type: "comment:deleted", Payload: map[string]any{"comment_id": comment.ID, "issue_id": ticket.ID}})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) handleResolveComment(w http.ResponseWriter, r *http.Request) {
	comment, ticket, actorID, ok := d.commentAccess(w, r)
	if !ok {
		return
	}
	rootID, err := d.Store.RootOf(r.Context(), comment.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	updated, unresolvedPrev, err := d.Store.ResolveThread(r.Context(), rootID, comment.ID, "member", actorID)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "comment not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.attachExtras(r.Context(), &updated)
	if d.Publisher != nil {
		if unresolvedPrev != nil {
			if prev, err := d.Store.GetComment(r.Context(), *unresolvedPrev); err == nil {
				d.attachExtras(r.Context(), &prev)
				d.Publisher.Publish(ticket.WorkspaceID, realtime.Event{Type: "comment:unresolved", Payload: map[string]any{"comment": prev}})
			}
		}
		d.Publisher.Publish(ticket.WorkspaceID, realtime.Event{Type: "comment:resolved", Payload: map[string]any{"comment": updated}})
	}
	httpapi.WriteJSON(w, http.StatusOK, updated)
}

func (d *Deps) handleUnresolveComment(w http.ResponseWriter, r *http.Request) {
	comment, ticket, _, ok := d.commentAccess(w, r)
	if !ok {
		return
	}
	updated, wasResolved, err := d.Store.UnresolveComment(r.Context(), comment.ID)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "comment not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.attachExtras(r.Context(), &updated)
	if d.Publisher != nil && wasResolved {
		d.Publisher.Publish(ticket.WorkspaceID, realtime.Event{Type: "comment:unresolved", Payload: map[string]any{"comment": updated}})
	}
	httpapi.WriteJSON(w, http.StatusOK, updated)
}

// --- listing -----------------------------------------------------------------

func (d *Deps) handleListComments(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	issueID := r.PathValue("id")
	if _, err := d.Store.GetTicketInfo(r.Context(), member.WorkspaceID, issueID); errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "issue not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	q := r.URL.Query()
	thread := q.Get("thread")
	recent := parseIntOr(q.Get("recent"), 0)
	tail := parseIntOr(q.Get("tail"), 0)
	rootsOnly := q.Get("roots_only") == "true"
	fold := q.Get("fold") == "true"
	summary := q.Get("summary") == "true"
	_, hasCursor := q["before"]

	if thread != "" && recent > 0 {
		httpapi.BadRequest(w, "thread and recent are mutually exclusive")
		return
	}
	if rootsOnly && (thread != "" || recent > 0 || tail > 0) {
		httpapi.BadRequest(w, "roots_only cannot combine with thread/recent/tail")
		return
	}
	if tail > 0 && thread == "" {
		httpapi.BadRequest(w, "tail requires thread")
		return
	}
	if hasCursor && recent == 0 && !(thread != "" && tail > 0) {
		httpapi.BadRequest(w, "before/before_id cursor requires recent or thread+tail")
		return
	}
	if fold && (q.Get("since") != "" || tail > 0 || rootsOnly) {
		httpapi.BadRequest(w, "fold cannot combine with since/tail/roots_only")
		return
	}

	var list []Comment
	var err error
	switch {
	case thread != "":
		list, err = d.Store.ListThread(r.Context(), thread, tail)
	case recent > 0:
		ids, e := d.Store.ListRecentThreads(r.Context(), issueID, recent)
		if e != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		for _, id := range ids {
			thread, e := d.Store.ListThread(r.Context(), id, 0)
			if e != nil {
				httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
				return
			}
			list = append(list, thread...)
		}
	case rootsOnly:
		list, err = d.Store.ListRoots(r.Context(), issueID)
	default:
		var since *time.Time
		if s := q.Get("since"); s != "" {
			if t, e := time.Parse(time.RFC3339, s); e == nil {
				since = &t
			} else {
				httpapi.BadRequest(w, "since must be RFC3339")
				return
			}
		}
		list, err = d.Store.ListAll(r.Context(), issueID, since)
	}
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "thread anchor not found in this issue")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Comment{}
	}
	for i := range list {
		d.attachExtras(r.Context(), &list[i])
	}
	if fold {
		list = foldResolvedThreads(list)
	}
	if summary {
		for i := range list {
			applySummary(&list[i])
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

func parseIntOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

const summaryRuneLimit = 200

func applySummary(c *Comment) {
	runes := []rune(c.Content)
	if len(runes) <= summaryRuneLimit {
		return
	}
	c.Content = string(runes[:summaryRuneLimit])
	truncated := true
	c.ContentTruncated = &truncated
}

// foldResolvedThreads — упрощённая реализация fold=true: для каждый решённый
// корневой комментарий оставляет только корень (плюс его собственный текст
// как заглушку резолюции — контракт говорит "корень и его
// комментарий-резолюцию, если разрешён явным ответом", что требует знать,
// какой именно ответ разрешил тред; в этой версии used эвристика "последний
// комментарий треда, если он и есть тот, кто вызвал resolve" не
// прослеживается отдельной колонкой — упрощение задокументировано в
// server2/docs/decisions.md).
func foldResolvedThreads(all []Comment) []Comment {
	byParent := map[string][]Comment{}
	var roots []Comment
	for _, c := range all {
		if c.ParentID == nil {
			roots = append(roots, c)
		} else {
			byParent[*c.ParentID] = append(byParent[*c.ParentID], c)
		}
	}
	var out []Comment
	for _, root := range roots {
		if root.ResolvedAt == nil {
			out = append(out, root)
			out = append(out, byParent[root.ID]...)
			continue
		}
		resolved := true
		root.ThreadResolved = &resolved
		count := len(byParent[root.ID])
		root.FoldedCount = &count
		out = append(out, root)
	}
	return out
}
