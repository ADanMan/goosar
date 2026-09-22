package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/logger"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

type CommentResponse struct {
	ID             string               `json:"id"`
	IssueID        string               `json:"issue_id"`
	AuthorType     string               `json:"author_type"`
	AuthorID       string               `json:"author_id"`
	Content        string               `json:"content"`
	Type           string               `json:"type"`
	ParentID       *string              `json:"parent_id"`
	CreatedAt      string               `json:"created_at"`
	UpdatedAt      string               `json:"updated_at"`
	ResolvedAt     *string              `json:"resolved_at"`
	ResolvedByType *string              `json:"resolved_by_type"`
	ResolvedByID   *string              `json:"resolved_by_id"`
	SourceTaskID   *string              `json:"source_task_id,omitempty"`
	Reactions      []ReactionResponse   `json:"reactions"`
	Attachments    []AttachmentResponse `json:"attachments"`

	ReplyCount     *int    `json:"reply_count,omitempty"`
	LastActivityAt *string `json:"last_activity_at,omitempty"`

	ContentTruncated *bool `json:"content_truncated,omitempty"`

	ThreadResolved *bool `json:"thread_resolved,omitempty"`
	FoldedCount    *int  `json:"folded_count,omitempty"`

	TriggerOutcomes []CommentTriggerOutcome `json:"trigger_outcomes,omitempty"`
}

type CommentTriggerOutcome struct {
	TargetType string             `json:"target_type"`
	TargetID   string             `json:"target_id"`
	Status     DispatchStatus     `json:"status"`
	ReasonCode DispatchReasonCode `json:"reason_code"`
}

func commentToResponse(c db.Comment, reactions []ReactionResponse, attachments []AttachmentResponse) CommentResponse {
	if reactions == nil {
		reactions = []ReactionResponse{}
	}
	if attachments == nil {
		attachments = []AttachmentResponse{}
	}
	return CommentResponse{
		ID:             uuidToString(c.ID),
		IssueID:        uuidToString(c.IssueID),
		AuthorType:     c.AuthorType,
		AuthorID:       uuidToString(c.AuthorID),
		Content:        c.Content,
		Type:           c.Type,
		ParentID:       uuidToPtr(c.ParentID),
		CreatedAt:      timestampToString(c.CreatedAt),
		UpdatedAt:      timestampToString(c.UpdatedAt),
		ResolvedAt:     timestampToPtr(c.ResolvedAt),
		ResolvedByType: textToPtr(c.ResolvedByType),
		ResolvedByID:   uuidToPtr(c.ResolvedByID),
		SourceTaskID:   responseSourceTaskID(c),
		Reactions:      reactions,
		Attachments:    attachments,
	}
}

func responseSourceTaskID(c db.Comment) *string {
	if util.ExposeCommentSourceTaskID(c.AuthorType, c.Type) {
		return uuidToPtr(c.SourceTaskID)
	}
	return nil
}

const summaryContentRunes = 200

func summarizeContent(content string) (string, bool) {
	count := 0
	for byteOffset := range content {
		if count == summaryContentRunes {
			return content[:byteOffset] + "…", true
		}
		count++
	}
	return content, false
}

type foldStat struct {
	FoldedCount int
}

func foldResolvedThreads(comments []db.Comment) ([]db.Comment, map[string]foldStat) {
	if len(comments) == 0 {
		return comments, nil
	}

	byID := make(map[string]db.Comment, len(comments))
	for _, c := range comments {
		byID[uuidToString(c.ID)] = c
	}

	rootOf := func(c db.Comment) db.Comment {
		cur := c
		for i := 0; i < len(comments); i++ {
			if !cur.ParentID.Valid {
				return cur
			}
			parent, ok := byID[uuidToString(cur.ParentID)]
			if !ok {
				return cur
			}
			cur = parent
		}
		return cur
	}

	type thread struct {
		root    db.Comment
		replies []db.Comment
	}
	threads := map[string]*thread{}
	for _, c := range comments {
		root := rootOf(c)
		rid := uuidToString(root.ID)
		th := threads[rid]
		if th == nil {
			th = &thread{root: root}
			threads[rid] = th
		}
		if uuidToString(c.ID) != rid {
			th.replies = append(th.replies, c)
		}
	}

	keep := make(map[string]bool, len(comments))
	stats := map[string]foldStat{}
	for rid, th := range threads {

		if th.root.ResolvedAt.Valid {
			keep[rid] = true
			stats[rid] = foldStat{FoldedCount: len(th.replies)}
			continue
		}

		var resolution *db.Comment
		for i := range th.replies {
			r := &th.replies[i]
			if !r.ResolvedAt.Valid {
				continue
			}
			if resolution == nil || r.ResolvedAt.Time.After(resolution.ResolvedAt.Time) {
				resolution = r
			}
		}
		if resolution == nil {

			keep[rid] = true
			for _, r := range th.replies {
				keep[uuidToString(r.ID)] = true
			}
			continue
		}
		keep[rid] = true
		keep[uuidToString(resolution.ID)] = true

		stats[rid] = foldStat{FoldedCount: len(th.replies) - 1}
	}

	out := make([]db.Comment, 0, len(comments))
	for _, c := range comments {
		if keep[uuidToString(c.ID)] {
			out = append(out, c)
		}
	}
	return out, stats
}

const commentHardCap = 2000

func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	q := r.URL.Query()

	var sinceTime pgtype.Timestamptz
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {

			t, err = time.Parse(time.RFC3339, v)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid since parameter; expected RFC3339 format")
				return
			}
		}
		sinceTime = pgtype.Timestamptz{Time: t, Valid: true}
	}

	threadStr := q.Get("thread")
	recentStr := q.Get("recent")
	tailStr := q.Get("tail")
	beforeTimeStr := q.Get("before")
	beforeIDStr := q.Get("before_id")
	if beforeIDStr == "" {

		beforeIDStr = q.Get("before-id")
	}

	rootsOnlyStr := q.Get("roots_only")
	if rootsOnlyStr == "" {

		rootsOnlyStr = q.Get("roots-only")
	}

	rootsOnly := false
	if rootsOnlyStr != "" {
		switch rootsOnlyStr {
		case "true":
			rootsOnly = true
		case "false":
		default:
			writeError(w, http.StatusBadRequest, "invalid roots_only parameter; expected boolean")
			return
		}
	}

	summary := false
	if summaryStr := q.Get("summary"); summaryStr != "" {
		switch summaryStr {
		case "true":
			summary = true
		case "false":
		default:
			writeError(w, http.StatusBadRequest, "invalid summary parameter; expected boolean")
			return
		}
	}

	fold := false
	if foldStr := q.Get("fold"); foldStr != "" {
		switch foldStr {
		case "true":
			fold = true
		case "false":
		default:
			writeError(w, http.StatusBadRequest, "invalid fold parameter; expected boolean")
			return
		}
	}

	if fold && sinceTime.Valid {
		writeError(w, http.StatusBadRequest, "fold and since are mutually exclusive: since returns a partial thread, and a fold over a partial thread could hide a resolution that was not fetched")
		return
	}
	if fold && tailStr != "" {
		writeError(w, http.StatusBadRequest, "fold and tail are mutually exclusive: tail returns a partial thread, which cannot be folded safely")
		return
	}
	if fold && rootsOnly {
		writeError(w, http.StatusBadRequest, "fold and roots_only are mutually exclusive: roots_only returns no replies to fold")
		return
	}
	if rootsOnly && threadStr != "" {
		writeError(w, http.StatusBadRequest, "roots_only and thread are mutually exclusive")
		return
	}
	if rootsOnly && recentStr != "" {
		writeError(w, http.StatusBadRequest, "roots_only and recent are mutually exclusive")
		return
	}
	if rootsOnly && tailStr != "" {
		writeError(w, http.StatusBadRequest, "roots_only and tail are mutually exclusive")
		return
	}
	if rootsOnly && (beforeTimeStr != "" || beforeIDStr != "") {
		writeError(w, http.StatusBadRequest, "roots_only does not support before / before_id")
		return
	}
	if threadStr != "" && recentStr != "" {
		writeError(w, http.StatusBadRequest, "thread and recent are mutually exclusive")
		return
	}
	if tailStr != "" && threadStr == "" {
		writeError(w, http.StatusBadRequest, "tail requires thread (it is a thread-scoped limit)")
		return
	}
	if (beforeTimeStr == "") != (beforeIDStr == "") {
		writeError(w, http.StatusBadRequest, "before and before_id must be set together (composite cursor)")
		return
	}

	if beforeTimeStr != "" && recentStr == "" && (threadStr == "" || tailStr == "") {
		writeError(w, http.StatusBadRequest, "before / before_id require recent (thread cursor) or thread + tail (reply cursor)")
		return
	}

	var beforeCursor pgtype.Timestamptz
	var beforeUUID pgtype.UUID
	hasCursor := false
	if beforeTimeStr != "" {
		t, err := time.Parse(time.RFC3339Nano, beforeTimeStr)
		if err != nil {
			t, err = time.Parse(time.RFC3339, beforeTimeStr)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid before parameter; expected RFC3339 format")
				return
			}
		}
		beforeCursor = pgtype.Timestamptz{Time: t, Valid: true}
		uuid, perr := util.ParseUUID(beforeIDStr)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid before_id parameter; expected UUID")
			return
		}
		beforeUUID = uuid
		hasCursor = true
	}

	recentN := 0
	if recentStr != "" {
		n, err := strconv.Atoi(recentStr)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid recent parameter; expected positive integer")
			return
		}
		if n > commentHardCap {
			n = commentHardCap
		}
		recentN = n
	}

	threadTail := -1
	threadTailSet := false
	if tailStr != "" {
		n, err := strconv.Atoi(tailStr)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid tail parameter; expected non-negative integer")
			return
		}
		if n > commentHardCap {
			n = commentHardCap
		}
		threadTail = n
		threadTailSet = true
	}

	result, err := h.fetchCommentsForList(r.Context(), fetchCommentsArgs{
		Issue:         issue,
		Since:         sinceTime,
		ThreadAnchor:  threadStr,
		ThreadTail:    threadTail,
		ThreadTailSet: threadTailSet,
		RecentN:       recentN,
		HasCursor:     hasCursor,
		BeforeAt:      beforeCursor,
		BeforeID:      beforeUUID,
		RootsOnly:     rootsOnly,
	})
	if err != nil {
		switch err {
		case errCommentThreadNotFound:
			writeError(w, http.StatusNotFound, "thread anchor not found in this issue")
			return
		case errCommentThreadBadID:
			writeError(w, http.StatusBadRequest, "invalid thread parameter; expected UUID")
			return
		default:
			writeError(w, http.StatusInternalServerError, "failed to list comments")
			return
		}
	}

	var foldInfo map[string]foldStat
	if fold {
		result.Comments, foldInfo = foldResolvedThreads(result.Comments)
	}

	commentIDs := make([]pgtype.UUID, len(result.Comments))
	for i, c := range result.Comments {
		commentIDs[i] = c.ID
	}
	grouped := h.groupReactions(r, commentIDs)
	groupedAtt := h.groupAttachments(r, commentIDs)

	resp := make([]CommentResponse, len(result.Comments))
	for i, c := range result.Comments {
		cid := uuidToString(c.ID)
		resp[i] = commentToResponse(c, grouped[cid], groupedAtt[cid])

		if st, ok := result.RootStats[cid]; ok {
			rc := st.ReplyCount
			resp[i].ReplyCount = &rc
			if st.LastActivityAt.Valid {
				la := timestampToString(st.LastActivityAt)
				resp[i].LastActivityAt = &la
			}
		}

		if st, ok := foldInfo[cid]; ok {
			resolved := true
			resp[i].ThreadResolved = &resolved
			fc := st.FoldedCount
			resp[i].FoldedCount = &fc
		}

		if summary {
			clipped, truncated := summarizeContent(resp[i].Content)
			resp[i].Content = clipped
			resp[i].ContentTruncated = &truncated
		}
	}

	if result.NextBefore != "" && result.NextBeforeID != "" {
		w.Header().Set("X-Goosar-Next-Before", result.NextBefore)
		w.Header().Set("X-Goosar-Next-Before-Id", result.NextBeforeID)
	}

	writeJSON(w, http.StatusOK, resp)
}

type fetchCommentsArgs struct {
	Issue         db.Issue
	Since         pgtype.Timestamptz
	RootsOnly     bool
	ThreadAnchor  string
	ThreadTail    int
	ThreadTailSet bool
	RecentN       int
	HasCursor     bool
	BeforeAt      pgtype.Timestamptz
	BeforeID      pgtype.UUID
}

type fetchCommentsResult struct {
	Comments     []db.Comment
	NextBefore   string
	NextBeforeID string

	RootStats map[string]rootStat
}

type rootStat struct {
	ReplyCount     int
	LastActivityAt pgtype.Timestamptz
}

var (
	errCommentThreadNotFound = &commentFetchError{"thread anchor not found"}
	errCommentThreadBadID    = &commentFetchError{"invalid thread anchor id"}
)

type commentFetchError struct{ msg string }

func (e *commentFetchError) Error() string { return e.msg }

func (h *Handler) fetchCommentsForList(ctx context.Context, args fetchCommentsArgs) (fetchCommentsResult, error) {
	issue := args.Issue

	if args.ThreadAnchor != "" {
		anchor, err := util.ParseUUID(args.ThreadAnchor)
		if err != nil {
			return fetchCommentsResult{}, errCommentThreadBadID
		}

		if args.ThreadTailSet {

			rows, err := h.Queries.ListThreadCommentsForIssuePaged(ctx, db.ListThreadCommentsForIssuePagedParams{
				AnchorID:    anchor,
				IssueID:     issue.ID,
				WorkspaceID: issue.WorkspaceID,
				HasCursor:   args.HasCursor,
				BeforeAt:    args.BeforeAt,
				BeforeID:    args.BeforeID,
				ReplyLimit:  int32(args.ThreadTail) + 1,
			})
			if err != nil {
				return fetchCommentsResult{}, err
			}
			if len(rows) == 0 {
				return fetchCommentsResult{}, errCommentThreadNotFound
			}

			var rootComment *db.Comment
			replies := make([]db.Comment, 0, len(rows))
			for _, r := range rows {
				c := db.Comment{
					ID:             r.ID,
					IssueID:        r.IssueID,
					AuthorType:     r.AuthorType,
					AuthorID:       r.AuthorID,
					Content:        r.Content,
					Type:           r.Type,
					CreatedAt:      r.CreatedAt,
					UpdatedAt:      r.UpdatedAt,
					ParentID:       r.ParentID,
					WorkspaceID:    r.WorkspaceID,
					ResolvedAt:     r.ResolvedAt,
					ResolvedByType: r.ResolvedByType,
					ResolvedByID:   r.ResolvedByID,
				}
				if !r.ParentID.Valid {
					root := c
					rootComment = &root
					continue
				}
				replies = append(replies, c)
			}

			hasMore := len(replies) > args.ThreadTail
			if hasMore {
				replies = replies[1:]
			}
			out := make([]db.Comment, 0, len(replies)+1)
			if rootComment != nil {
				out = append(out, *rootComment)
			}
			for _, r := range replies {

				if args.Since.Valid && !r.CreatedAt.Time.After(args.Since.Time) {
					continue
				}
				out = append(out, r)
			}

			res := fetchCommentsResult{Comments: out}
			emitCursor := hasMore && len(replies) > 0
			if emitCursor && args.Since.Valid && !replies[0].CreatedAt.Time.After(args.Since.Time) {
				emitCursor = false
			}
			if emitCursor {
				oldest := replies[0]
				res.NextBefore = oldest.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
				res.NextBeforeID = uuidToString(oldest.ID)
			}
			return res, nil
		}
		rows, err := h.Queries.ListThreadCommentsForIssue(ctx, db.ListThreadCommentsForIssueParams{
			AnchorID:    anchor,
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			RowLimit:    commentHardCap,
		})
		if err != nil {
			return fetchCommentsResult{}, err
		}
		if len(rows) == 0 {
			return fetchCommentsResult{}, errCommentThreadNotFound
		}
		out := make([]db.Comment, 0, len(rows))
		for _, r := range rows {
			if args.Since.Valid && !r.CreatedAt.Time.After(args.Since.Time) {
				continue
			}
			out = append(out, db.Comment{
				ID:             r.ID,
				IssueID:        r.IssueID,
				AuthorType:     r.AuthorType,
				AuthorID:       r.AuthorID,
				Content:        r.Content,
				Type:           r.Type,
				CreatedAt:      r.CreatedAt,
				UpdatedAt:      r.UpdatedAt,
				ParentID:       r.ParentID,
				WorkspaceID:    r.WorkspaceID,
				ResolvedAt:     r.ResolvedAt,
				ResolvedByType: r.ResolvedByType,
				ResolvedByID:   r.ResolvedByID,
			})
		}
		return fetchCommentsResult{Comments: out}, nil
	}

	if args.RecentN > 0 {
		rows, err := h.Queries.ListRecentThreadCommentsForIssue(ctx, db.ListRecentThreadCommentsForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			HasCursor:   args.HasCursor,
			BeforeAt:    args.BeforeAt,
			BeforeID:    args.BeforeID,
			ThreadLimit: int32(args.RecentN),
		})
		if err != nil {
			return fetchCommentsResult{}, err
		}

		comments := make([]db.Comment, 0, len(rows))
		var headRoot pgtype.UUID
		var headLast pgtype.Timestamptz
		seenRoot := map[string]struct{}{}
		for _, r := range rows {
			if !headRoot.Valid {
				headRoot = r.ThreadRootID
				headLast = r.ThreadLastActivityAt
			}
			seenRoot[uuidToString(r.ThreadRootID)] = struct{}{}

			if args.Since.Valid && !r.CreatedAt.Time.After(args.Since.Time) {
				continue
			}
			comments = append(comments, db.Comment{
				ID:             r.ID,
				IssueID:        r.IssueID,
				AuthorType:     r.AuthorType,
				AuthorID:       r.AuthorID,
				Content:        r.Content,
				Type:           r.Type,
				CreatedAt:      r.CreatedAt,
				UpdatedAt:      r.UpdatedAt,
				ParentID:       r.ParentID,
				WorkspaceID:    r.WorkspaceID,
				ResolvedAt:     r.ResolvedAt,
				ResolvedByType: r.ResolvedByType,
				ResolvedByID:   r.ResolvedByID,
			})
		}

		out := fetchCommentsResult{Comments: comments}
		emitCursor := len(seenRoot) >= args.RecentN && headRoot.Valid && headLast.Valid
		if emitCursor && args.Since.Valid && !headLast.Time.After(args.Since.Time) {
			emitCursor = false
		}
		if emitCursor {
			out.NextBefore = headLast.Time.UTC().Format(time.RFC3339Nano)
			out.NextBeforeID = uuidToString(headRoot)
		}
		return out, nil
	}

	if args.RootsOnly {

		stats := map[string]rootStat{}
		if args.Since.Valid {
			rows, err := h.Queries.ListRootCommentsSinceForIssue(ctx, db.ListRootCommentsSinceForIssueParams{
				IssueID:     issue.ID,
				WorkspaceID: issue.WorkspaceID,
				Since:       args.Since,
				RowLimit:    commentHardCap,
			})
			if err != nil {
				return fetchCommentsResult{}, err
			}
			comments := make([]db.Comment, len(rows))
			for i, r := range rows {
				comments[i] = db.Comment{
					ID: r.ID, IssueID: r.IssueID, AuthorType: r.AuthorType, AuthorID: r.AuthorID,
					Content: r.Content, Type: r.Type, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
					ParentID: r.ParentID, WorkspaceID: r.WorkspaceID, ResolvedAt: r.ResolvedAt,
					ResolvedByType: r.ResolvedByType, ResolvedByID: r.ResolvedByID,
				}
				stats[uuidToString(r.ID)] = rootStat{ReplyCount: int(r.ReplyCount), LastActivityAt: r.LastActivityAt}
			}
			return fetchCommentsResult{Comments: comments, RootStats: stats}, nil
		}

		rows, err := h.Queries.ListRootCommentsForIssue(ctx, db.ListRootCommentsForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			RowLimit:    commentHardCap,
		})
		if err != nil {
			return fetchCommentsResult{}, err
		}
		comments := make([]db.Comment, len(rows))
		for i, r := range rows {
			comments[i] = db.Comment{
				ID: r.ID, IssueID: r.IssueID, AuthorType: r.AuthorType, AuthorID: r.AuthorID,
				Content: r.Content, Type: r.Type, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
				ParentID: r.ParentID, WorkspaceID: r.WorkspaceID, ResolvedAt: r.ResolvedAt,
				ResolvedByType: r.ResolvedByType, ResolvedByID: r.ResolvedByID,
			}
			stats[uuidToString(r.ID)] = rootStat{ReplyCount: int(r.ReplyCount), LastActivityAt: r.LastActivityAt}
		}
		return fetchCommentsResult{Comments: comments, RootStats: stats}, nil
	}

	if args.Since.Valid {
		comments, err := h.Queries.ListCommentsSinceForIssue(ctx, db.ListCommentsSinceForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			CreatedAt:   args.Since,
			Limit:       commentHardCap,
		})
		return fetchCommentsResult{Comments: comments}, err
	}
	comments, err := h.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Limit:       commentHardCap,
	})
	return fetchCommentsResult{Comments: comments}, err
}

type CreateCommentRequest struct {
	Content          string   `json:"content"`
	Type             string   `json:"type"`
	ParentID         *string  `json:"parent_id"`
	AttachmentIDs    []string `json:"attachment_ids"`
	SuppressAgentIDs []string `json:"suppress_agent_ids"`
}

type CommentTriggerPreviewRequest struct {
	Content          string  `json:"content"`
	ParentID         *string `json:"parent_id"`
	EditingCommentID *string `json:"editing_comment_id"`
}

type CommentTriggerPreviewResponse struct {
	Agents []CommentTriggerAgentResponse `json:"agents"`

	Blocked []CommentTriggerOutcome `json:"blocked,omitempty"`
}

type CommentTriggerAgentResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url,omitempty"`
	Source    string  `json:"source"`
	Reason    string  `json:"reason"`
}

type commentAgentTriggerSource string

const (
	commentTriggerSourceIssueAssignee      commentAgentTriggerSource = "issue_assignee"
	commentTriggerSourceMentionAgent       commentAgentTriggerSource = "mention_agent"
	commentTriggerSourceMentionSquadLeader commentAgentTriggerSource = "mention_squad_leader"
	commentTriggerSourceThreadParent       commentAgentTriggerSource = "thread_parent"
	commentTriggerSourceConversation       commentAgentTriggerSource = "conversation_continuation"
)

const defaultCommentRoutingEscalationDelay = 5 * time.Minute

func (h *Handler) commentRoutingEscalationDelay(ctx context.Context, workspaceID pgtype.UUID) time.Duration {
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil || len(ws.Settings) == 0 {
		return defaultCommentRoutingEscalationDelay
	}

	var settings struct {
		CommentRouting struct {
			EscalationSeconds *int `json:"escalation_seconds"`
		} `json:"comment_routing"`
	}
	if err := json.Unmarshal(ws.Settings, &settings); err != nil || settings.CommentRouting.EscalationSeconds == nil {
		return defaultCommentRoutingEscalationDelay
	}
	if *settings.CommentRouting.EscalationSeconds <= 0 {
		return 0
	}
	return time.Duration(*settings.CommentRouting.EscalationSeconds) * time.Second
}

type commentEscalationFallback struct {
	Agent db.Agent
	Squad *db.Squad
}

type commentAgentTrigger struct {
	Agent              db.Agent
	Source             commentAgentTriggerSource
	Squad              *db.Squad
	EscalationFallback *commentEscalationFallback
	AlreadyPending     bool
}

type commentTriggerComputeOptions struct {
	ExcludeTriggerCommentID pgtype.UUID

	Originator invokeAuthority

	AutopilotDelegationAuthorityUserID string
}

func (o commentTriggerComputeOptions) effectiveInvoker() invokeAuthority {
	if o.Originator.UserID != "" {
		return o.Originator
	}
	return scopedInvokeAuthority(o.AutopilotDelegationAuthorityUserID)
}

func commentAgentTriggerReason(trigger commentAgentTrigger) string {
	switch trigger.Source {
	case commentTriggerSourceIssueAssignee:
		return "Current issue assignment will trigger this agent."
	case commentTriggerSourceMentionAgent:
		return "This agent was mentioned in the comment."
	case commentTriggerSourceMentionSquadLeader:
		return "A mentioned squad will trigger its leader."
	case commentTriggerSourceThreadParent:
		return "This reply will trigger the parent comment's author."
	case commentTriggerSourceConversation:
		return "This follow-up will continue the recent agent conversation."
	default:
		return "This comment will trigger this agent."
	}
}

func commentAgentTriggerToResponse(trigger commentAgentTrigger) CommentTriggerAgentResponse {
	return CommentTriggerAgentResponse{
		ID:        uuidToString(trigger.Agent.ID),
		Name:      trigger.Agent.Name,
		AvatarURL: textToPtr(trigger.Agent.AvatarUrl),
		Source:    string(trigger.Source),
		Reason:    commentAgentTriggerReason(trigger),
	}
}

func (h *Handler) PreviewCommentTriggers(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CommentTriggerPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var editingComment *db.Comment
	var opts commentTriggerComputeOptions
	if req.EditingCommentID != nil {
		editingID, ok := parseUUIDOrBadRequest(w, *req.EditingCommentID, "editing_comment_id")
		if !ok {
			return
		}
		comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
			ID:          editingID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil || uuidToString(comment.IssueID) != uuidToString(issue.ID) {
			writeError(w, http.StatusBadRequest, "invalid editing comment")
			return
		}
		editingComment = &comment
		opts.ExcludeTriggerCommentID = editingID
	}

	var parentID pgtype.UUID
	if req.ParentID != nil {
		parentID, ok = parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
		if !ok {
			return
		}

		if editingComment != nil && uuidToString(parentID) != uuidToString(editingComment.ParentID) {
			writeError(w, http.StatusBadRequest, "parent_id does not match editing comment")
			return
		}
	} else if editingComment != nil && editingComment.ParentID.Valid {
		parentID = editingComment.ParentID
	}

	var parentComment *db.Comment
	if parentID.Valid {
		parent, err := h.Queries.GetComment(r.Context(), parentID)
		if err != nil || uuidToString(parent.IssueID) != uuidToString(issue.ID) {
			writeError(w, http.StatusBadRequest, "invalid parent comment")
			return
		}
		parentComment = &parent
	}

	content := sanitizeNullBytes(req.Content)
	if content == "" {
		writeJSON(w, http.StatusOK, CommentTriggerPreviewResponse{Agents: []CommentTriggerAgentResponse{}})
		return
	}

	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))

	opts.Originator = h.invokeAuthorityFromRequest(r, actorType, actorID, issueInvokeScope(issue.ID))
	opts.AutopilotDelegationAuthorityUserID = h.autopilotDelegationAuthorityFromRequest(r, issue, actorType, actorID)
	triggers, targets := h.computeCommentAgentTriggers(r.Context(), issue, content, parentComment, actorType, actorID, opts)
	resp := CommentTriggerPreviewResponse{
		Agents:  make([]CommentTriggerAgentResponse, 0, len(triggers)),
		Blocked: commentBlockedTargetOutcomes(targets),
	}
	for _, trigger := range triggers {
		resp.Agents = append(resp.Agents, commentAgentTriggerToResponse(trigger))
	}
	writeJSON(w, http.StatusOK, resp)
}

func taskCoversReplyParent(task db.AgentTaskQueue, parentID pgtype.UUID) bool {
	if !parentID.Valid {
		return false
	}
	target := uuidToString(parentID)
	if task.TriggerCommentID.Valid && uuidToString(task.TriggerCommentID) == target {
		return true
	}
	for _, id := range task.CoalescedCommentIds {
		if id.Valid && uuidToString(id) == target {
			return true
		}
	}
	return false
}

func (h *Handler) CreateComment(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Content = sanitizeNullBytes(req.Content)
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.Type == "" {
		req.Type = "comment"
	}

	var parentID pgtype.UUID
	var parentComment *db.Comment
	if req.ParentID != nil {
		var parsed pgtype.UUID
		parsed, ok = parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
		if !ok {
			return
		}
		parentID = parsed
		parent, err := h.Queries.GetComment(r.Context(), parentID)
		if err != nil || uuidToString(parent.IssueID) != uuidToString(issue.ID) {
			writeError(w, http.StatusBadRequest, "invalid parent comment")
			return
		}
		parentComment = &parent
	}

	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, req.AttachmentIDs, "attachment_ids")
	if !ok {
		return
	}
	suppressAgentIDs, ok := parseUUIDSliceOrBadRequest(w, req.SuppressAgentIDs, "suppress_agent_ids")
	if !ok {
		return
	}

	authorType, authorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))

	var sourceTaskID pgtype.UUID
	if authorType == "agent" {
		if taskIDHeader := r.Header.Get("X-Task-ID"); taskIDHeader != "" {
			taskUUID, parseErr := util.ParseUUID(taskIDHeader)
			if parseErr == nil {

				task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{
					ID:          taskUUID,
					WorkspaceID: issue.WorkspaceID,
				})
				switch {
				case err != nil || uuidToString(task.AgentID) != authorID:

				case task.IssueID.Valid && uuidToString(task.IssueID) == uuidToString(issue.ID):
					if task.TriggerCommentID.Valid {
						if !taskCoversReplyParent(task, parentID) {

							writeError(w, http.StatusConflict,
								"comment-triggered tasks cannot create top-level comments; set parent_id (--parent) to "+uuidToString(task.TriggerCommentID)+" or a coalesced comment id")
							return
						}
					}
					noAction, checkErr := service.HasSquadLeaderNoActionEvaluationForTask(r.Context(), h.Queries, task)
					if checkErr != nil {
						slog.Warn("checking squad leader no_action evaluation failed", append(logger.RequestAttrs(r),
							"error", checkErr,
							"task_id", taskIDHeader,
							"issue_id", issueID,
						)...)
					} else if noAction {
						writeError(w, http.StatusConflict, "squad leader recorded no_action; comments are not allowed for this task")
						return
					}

					sourceTaskID = taskUUID
				case task.ChatSessionID.Valid:

					sourceTaskID = taskUUID
				}
			}
		}
	}

	var rootComment *db.Comment
	if parentID.Valid {
		if root, err := h.Queries.GetThreadRoot(r.Context(), db.GetThreadRootParams{
			CommentID:   parentID,
			WorkspaceID: issue.WorkspaceID,
		}); err == nil {
			rootComment = &root
		}
	}

	comment, err := h.Queries.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:      issue.ID,
		WorkspaceID:  issue.WorkspaceID,
		AuthorType:   authorType,
		AuthorID:     parseUUID(authorID),
		Content:      req.Content,
		Type:         req.Type,
		ParentID:     parentID,
		SourceTaskID: sourceTaskID,
	})
	if err != nil {
		slog.Warn("create comment failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to create comment: "+err.Error())
		return
	}

	if len(attachmentIDs) > 0 {
		h.linkAttachmentsByIDs(r.Context(), comment.ID, issue.ID, attachmentIDs)
	}

	groupedAtt := h.groupAttachments(r, []pgtype.UUID{comment.ID})
	resp := commentToResponse(comment, nil, groupedAtt[uuidToString(comment.ID)])
	slog.Info("comment created", append(logger.RequestAttrs(r), "comment_id", uuidToString(comment.ID), "issue_id", issueID)...)
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), authorType, authorID, map[string]any{
		"comment":             resp,
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})

	h.TaskService.AutoUnresolveThreadOnReply(r.Context(), rootComment, uuidToString(issue.WorkspaceID), authorType, authorID)
	if authorType == "agent" {
		h.TaskService.CancelDeferredEscalationsForIssueAgent(r.Context(), issue.ID, comment.AuthorID)
	}

	originatorAuthority := h.invokeAuthorityFromRequest(r, authorType, authorID, issueInvokeScope(issue.ID))

	delegationAuthority := h.autopilotDelegationAuthorityFromRequest(r, issue, authorType, authorID)

	resp.TriggerOutcomes = h.triggerTasksForComment(r.Context(), issue, comment, parentComment, authorType, authorID, originatorAuthority, delegationAuthority, suppressAgentIDs)

	writeJSON(w, http.StatusCreated, resp)
}

const noteCommentPrefix = "/note"

func isNoteComment(content string) bool {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	firstToken := trimmed
	if i := strings.IndexFunc(trimmed, unicode.IsSpace); i >= 0 {
		firstToken = trimmed[:i]
	}
	return strings.EqualFold(firstToken, noteCommentPrefix)
}

func (h *Handler) triggerTasksForComment(ctx context.Context, issue db.Issue, comment db.Comment, parentComment *db.Comment, actorType, actorID string, originator invokeAuthority, delegationAuthorityUserID string, suppressAgentIDs []pgtype.UUID) []CommentTriggerOutcome {
	if isNoteComment(comment.Content) {
		return nil
	}
	triggers, targets := h.computeCommentAgentTriggers(ctx, issue, comment.Content, parentComment, actorType, actorID, commentTriggerComputeOptions{
		ExcludeTriggerCommentID:            comment.ID,
		Originator:                         originator,
		AutopilotDelegationAuthorityUserID: delegationAuthorityUserID,
	})
	triggers = filterSuppressedCommentAgentTriggers(triggers, suppressAgentIDs)
	enqueued := h.enqueueCommentAgentTriggers(ctx, issue, comment.ID, triggers)
	return commentTriggerOutcomes(targets, enqueued)
}

func filterSuppressedCommentAgentTriggers(triggers []commentAgentTrigger, suppressAgentIDs []pgtype.UUID) []commentAgentTrigger {
	if len(triggers) == 0 || len(suppressAgentIDs) == 0 {
		return triggers
	}
	suppressed := make(map[string]struct{}, len(suppressAgentIDs))
	for _, id := range suppressAgentIDs {
		if id.Valid {
			suppressed[uuidToString(id)] = struct{}{}
		}
	}
	if len(suppressed) == 0 {
		return triggers
	}
	filtered := make([]commentAgentTrigger, 0, len(triggers))
	for _, trigger := range triggers {
		if _, ok := suppressed[uuidToString(trigger.Agent.ID)]; ok {
			continue
		}
		filtered = append(filtered, trigger)
	}
	return filtered
}

type commentEnqueueResult struct {
	status      DispatchStatus
	reason      DispatchReasonCode
	execSquadID string
}

func (h *Handler) enqueueCommentAgentTriggers(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID, triggers []commentAgentTrigger) map[string]commentEnqueueResult {
	var escalationDelay time.Duration
	escalationDelayLoaded := false
	getEscalationDelay := func() time.Duration {
		if !escalationDelayLoaded {
			escalationDelay = h.commentRoutingEscalationDelay(ctx, issue.WorkspaceID)
			escalationDelayLoaded = true
		}
		return escalationDelay
	}
	results := make(map[string]commentEnqueueResult, len(triggers))
	record := func(trigger commentAgentTrigger, status DispatchStatus, reason DispatchReasonCode) {
		execSquadID := ""
		if trigger.Squad != nil {
			execSquadID = uuidToString(trigger.Squad.ID)
		}
		results[uuidToString(trigger.Agent.ID)] = commentEnqueueResult{status: status, reason: reason, execSquadID: execSquadID}
	}
	for _, trigger := range triggers {
		status, reason := h.resolveCommentTriggerEnqueue(ctx, issue, trigger, triggerCommentID, getEscalationDelay)
		record(trigger, status, reason)
	}
	return results
}

func (h *Handler) resolveCommentTriggerEnqueue(ctx context.Context, issue db.Issue, trigger commentAgentTrigger, triggerCommentID pgtype.UUID, getEscalationDelay func() time.Duration) (DispatchStatus, DispatchReasonCode) {
	pending := trigger.AlreadyPending
	lostRace := false

	var headSha pgtype.Text
	headShaLoaded := false
	getHeadSha := func() pgtype.Text {
		if !headShaLoaded {
			headSha = h.TaskService.ResolveIssueReviewSHAParam(ctx, issue.ID)
			headShaLoaded = true
		}
		return headSha
	}

	const maxAttempts = 4
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if pending {

			if status, reason, terminal := commentMergeTerminalOutcome(
				h.mergeCommentIntoPendingTask(ctx, issue, trigger, triggerCommentID, getHeadSha()),
			); terminal {
				return status, reason
			}

			if !lostRace {

				active, activeErr := h.hasActiveTaskForIssueAndAgent(ctx, issue.ID, trigger.Agent.ID)
				if status, reason, enqueueFresh := decidePostMergeMiss(active, activeErr); !enqueueFresh {
					return status, reason
				}

			} else {

				registered, err := h.registerPlannedCommentForActiveTask(ctx, issue, trigger.Agent.ID, triggerCommentID, getHeadSha())
				if err != nil {
					slog.Warn("register planned lost-race comment failed",
						"issue_id", uuidToString(issue.ID), "agent_id", uuidToString(trigger.Agent.ID), "error", err)
					return DispatchBlocked, ReasonInternalError
				}
				if registered {
					return DispatchDeferred, ReasonDeferred
				}

			}
		}
		if err := h.enqueueSingleCommentTrigger(ctx, issue, triggerCommentID, trigger, getEscalationDelay); err != nil {

			if errors.Is(err, service.ErrDuplicatePendingTask) {
				pending = true
				lostRace = true
				continue
			}
			return DispatchBlocked, commentEnqueueFailureReason(err)
		}
		return DispatchQueued, ReasonQueued
	}

	slog.Warn("comment trigger enqueue did not converge; reporting non-success",
		"issue_id", uuidToString(issue.ID), "agent_id", uuidToString(trigger.Agent.ID))
	return DispatchBlocked, ReasonInternalError
}

func commentTriggerOutcomes(targets []commentMentionTarget, enqueued map[string]commentEnqueueResult) []CommentTriggerOutcome {
	if len(targets) == 0 {
		return nil
	}
	outcomes := make([]CommentTriggerOutcome, 0, len(targets))
	for _, t := range targets {
		if t.ExecAgentID != "" {
			res, ok := enqueued[t.ExecAgentID]
			if !ok {
				continue
			}
			status, reason := res.status, res.reason

			if t.TargetType == "squad" && res.execSquadID != "" && res.execSquadID != t.TargetID && status == DispatchQueued {
				status, reason = DispatchCoalesced, ReasonCoalesced
			}
			outcomes = append(outcomes, CommentTriggerOutcome{TargetType: t.TargetType, TargetID: t.TargetID, Status: status, ReasonCode: reason})
			continue
		}
		outcomes = append(outcomes, CommentTriggerOutcome{TargetType: t.TargetType, TargetID: t.TargetID, Status: t.Status, ReasonCode: t.ReasonCode})
	}
	return outcomes
}

func commentBlockedTargetOutcomes(targets []commentMentionTarget) []CommentTriggerOutcome {
	var blocked []CommentTriggerOutcome
	for _, t := range targets {
		if t.Status == DispatchBlocked {
			blocked = append(blocked, CommentTriggerOutcome{TargetType: t.TargetType, TargetID: t.TargetID, Status: t.Status, ReasonCode: t.ReasonCode})
		}
	}
	return blocked
}

func commentEnqueueFailureReason(err error) DispatchReasonCode {
	if errors.Is(err, service.ErrAttributionFailClosed) {
		return ReasonAttributionBlocked
	}
	return ReasonInternalError
}

func (h *Handler) hasActiveTaskForIssueAndAgent(ctx context.Context, issueID, agentID pgtype.UUID) (bool, error) {
	active, err := h.Queries.HasActiveTaskForIssueAndAgent(ctx, db.HasActiveTaskForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: agentID,
	})
	if err != nil {
		slog.Warn("has active task for issue+agent check failed",
			"issue_id", uuidToString(issueID), "agent_id", uuidToString(agentID), "error", err)
		return false, err
	}
	return active, nil
}

func decidePostMergeMiss(active bool, activeErr error) (status DispatchStatus, reason DispatchReasonCode, enqueueFresh bool) {
	switch {
	case activeErr != nil:
		return DispatchBlocked, ReasonInternalError, false
	case active:
		return DispatchDeferred, ReasonDeferred, false
	default:
		return "", "", true
	}
}

func decideSuppressedLeaderOutcome(active bool, activeErr error) (DispatchStatus, DispatchReasonCode) {
	switch {
	case activeErr != nil:
		return DispatchBlocked, ReasonInternalError
	case active:
		return DispatchDeferred, ReasonAlreadyActive
	default:
		return DispatchBlocked, ReasonSelfTriggerSuppressed
	}
}

type commentMergeResult int

const (
	commentMergeSucceeded commentMergeResult = iota

	commentMergeNoPendingTask

	commentMergeAttributionBlocked

	commentMergeError
)

func commentMergeTerminalOutcome(result commentMergeResult) (status DispatchStatus, reason DispatchReasonCode, terminal bool) {
	switch result {
	case commentMergeSucceeded:
		return DispatchCoalesced, ReasonCoalesced, true
	case commentMergeAttributionBlocked:
		return DispatchBlocked, ReasonAttributionBlocked, true
	case commentMergeError:
		return DispatchBlocked, ReasonInternalError, true
	default:
		return "", "", false
	}
}

func (h *Handler) mergeCommentIntoPendingTask(ctx context.Context, issue db.Issue, trigger commentAgentTrigger, newTriggerCommentID pgtype.UUID, headSha pgtype.Text) commentMergeResult {

	isMention := trigger.Source != commentTriggerSourceIssueAssignee
	attr, err := h.TaskService.AttributionForMergedComment(ctx, issue.WorkspaceID, newTriggerCommentID, isMention, trigger.Agent)
	if err != nil {

		slog.Warn("refused comment merge: attribution failed, keeping original task snapshot",
			"issue_id", uuidToString(issue.ID),
			"agent_id", uuidToString(trigger.Agent.ID),
			"new_trigger_comment_id", uuidToString(newTriggerCommentID),
			"error", err)
		if errors.Is(err, service.ErrAttributionFailClosed) {
			return commentMergeAttributionBlocked
		}
		return commentMergeError
	}
	overlay, connectedApps := h.TaskService.BuildRuntimeMCPOverlayForMerge(ctx, attr.UserID, trigger.Agent)
	row, err := h.Queries.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:                 issue.ID,
		AgentID:                 trigger.Agent.ID,
		NewTriggerCommentID:     newTriggerCommentID,
		NewOriginatorUserID:     attr.UserID,
		NewAccountableUserID:    attr.AccountableUserID,
		NewOriginatorSource:     pgtype.Text{String: attr.Source.String(), Valid: true},
		NewDelegatedFromTaskID:  attr.DelegatedFromTaskID,
		NewRuleVersionID:        attr.RuleVersionID,
		NewTriggerEvidenceKind:  pgtype.Text{String: string(attr.EvidenceKind), Valid: attr.EvidenceKind != ""},
		NewTriggerEvidenceRefID: attr.EvidenceRefID,
		NewTriggerSummary:       h.TaskService.BuildCommentTriggerSummary(ctx, issue.WorkspaceID, newTriggerCommentID),
		NewRuntimeMcpOverlay:    overlay,
		NewRuntimeConnectedApps: connectedApps,
		HeadSha:                 headSha,
	})
	if err != nil {
		if isNotFound(err) {

			return commentMergeNoPendingTask
		}

		slog.Warn("merge comment into pending task failed",
			"issue_id", uuidToString(issue.ID),
			"agent_id", uuidToString(trigger.Agent.ID),
			"error", err)
		return commentMergeError
	}
	slog.Info("merged comment into pending task",
		"task_id", uuidToString(row.ID),
		"issue_id", uuidToString(issue.ID),
		"agent_id", uuidToString(trigger.Agent.ID),
		"new_trigger_comment_id", uuidToString(newTriggerCommentID),
		"coalesced_count", len(row.CoalescedCommentIds))
	return commentMergeSucceeded
}

func (h *Handler) registerPlannedCommentForActiveTask(ctx context.Context, issue db.Issue, agentID, commentID pgtype.UUID, headSha pgtype.Text) (bool, error) {
	row, err := h.Queries.RegisterPlannedCommentForActiveTask(ctx, db.RegisterPlannedCommentForActiveTaskParams{
		CommentID: commentID,
		IssueID:   issue.ID,
		AgentID:   agentID,
		HeadSha:   headSha,
	})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	slog.Info("registered lost-race comment as planned follow-up input",
		"task_id", uuidToString(row.ID),
		"issue_id", uuidToString(issue.ID),
		"agent_id", uuidToString(agentID),
		"comment_id", uuidToString(commentID),
		"coalesced_count", len(row.CoalescedCommentIds))
	return true, nil
}

func (h *Handler) propagateUncoveredCommentObligation(ctx context.Context, issue db.Issue, trigger commentAgentTrigger, commentID pgtype.UUID, headSha pgtype.Text) bool {
	noEscalation := func() time.Duration { return 0 }
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		registered, err := h.registerPlannedCommentForActiveTask(ctx, issue, trigger.Agent.ID, commentID, pgtype.Text{})
		if err != nil {
			slog.Warn("propagate comment obligation: register on active task failed",
				"issue_id", uuidToString(issue.ID), "agent_id", uuidToString(trigger.Agent.ID),
				"comment_id", uuidToString(commentID), "error", err)
		} else if registered {
			return true
		}
		if h.mergeCommentIntoPendingTask(ctx, issue, trigger, commentID, headSha) == commentMergeSucceeded {
			return true
		}
		err = h.enqueueSingleCommentTrigger(ctx, issue, commentID, trigger, noEscalation)
		if err == nil {
			return true
		}
		if !errors.Is(err, service.ErrDuplicatePendingTask) {
			return false
		}

	}
	return false
}

func logCommentEnqueueFailure(msg string, err error, attrs ...any) {
	if errors.Is(err, service.ErrDuplicatePendingTask) {
		slog.Debug(msg+": duplicate pending task, coalescing into the sibling run", attrs...)
		return
	}
	slog.Warn(msg, append(attrs, "error", err)...)
}

func (h *Handler) enqueueSingleCommentTrigger(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID, trigger commentAgentTrigger, getEscalationDelay func() time.Duration) error {
	switch trigger.Source {
	case commentTriggerSourceIssueAssignee:
		if trigger.Squad != nil {
			if _, err := h.TaskService.EnqueueTaskForSquadLeader(ctx, issue, trigger.Agent.ID, trigger.Squad.ID, triggerCommentID); err != nil {
				logCommentEnqueueFailure("enqueue squad leader task failed", err,
					"issue_id", uuidToString(issue.ID),
					"squad_id", uuidToString(trigger.Squad.ID),
					"leader_id", uuidToString(trigger.Agent.ID))
				return err
			}
			return nil
		}
		if _, err := h.TaskService.EnqueueTaskForIssue(ctx, issue, triggerCommentID); err != nil {
			slog.Warn("enqueue agent task on comment failed", "issue_id", uuidToString(issue.ID), "error", err)
			return err
		}
	case commentTriggerSourceMentionSquadLeader:
		if _, err := h.TaskService.EnqueueTaskForSquadLeader(ctx, issue, trigger.Agent.ID, trigger.Squad.ID, triggerCommentID); err != nil {
			logCommentEnqueueFailure("enqueue squad leader mention task failed", err,
				"issue_id", uuidToString(issue.ID),
				"agent_id", uuidToString(trigger.Agent.ID))
			return err
		}
	case commentTriggerSourceMentionAgent:
		if _, err := h.TaskService.EnqueueTaskForMention(ctx, issue, trigger.Agent.ID, triggerCommentID); err != nil {
			logCommentEnqueueFailure("enqueue mention agent task failed", err,
				"issue_id", uuidToString(issue.ID),
				"agent_id", uuidToString(trigger.Agent.ID))
			return err
		}
	case commentTriggerSourceThreadParent, commentTriggerSourceConversation:
		var task db.AgentTaskQueue
		var err error
		if trigger.Source == commentTriggerSourceConversation && trigger.Squad != nil {
			task, err = h.TaskService.EnqueueTaskForSquadLeader(ctx, issue, trigger.Agent.ID, trigger.Squad.ID, triggerCommentID)
		} else {
			task, err = h.TaskService.EnqueueTaskForThreadParent(ctx, issue, trigger.Agent.ID, triggerCommentID)
		}
		if err != nil {
			logCommentEnqueueFailure("enqueue routed comment agent task failed", err,
				"issue_id", uuidToString(issue.ID),
				"agent_id", uuidToString(trigger.Agent.ID),
				"source", trigger.Source)
			return err
		}
		if trigger.EscalationFallback == nil || getEscalationDelay() <= 0 {
			return nil
		}
		var squadID pgtype.UUID
		if trigger.EscalationFallback.Squad != nil {
			squadID = trigger.EscalationFallback.Squad.ID
		}
		if _, err := h.TaskService.EnqueueDeferredAssigneeFallback(ctx, issue, trigger.EscalationFallback.Agent.ID, squadID, task.ID, triggerCommentID, time.Now().Add(getEscalationDelay())); err != nil {
			slog.Warn("enqueue deferred assignee fallback failed",
				"issue_id", uuidToString(issue.ID),
				"primary_agent_id", uuidToString(trigger.Agent.ID),
				"fallback_agent_id", uuidToString(trigger.EscalationFallback.Agent.ID),
				"error", err)
		}
	}
	return nil
}

func (h *Handler) computeCommentAgentTriggers(ctx context.Context, issue db.Issue, content string, parentComment *db.Comment, actorType, actorID string, opts commentTriggerComputeOptions) ([]commentAgentTrigger, []commentMentionTarget) {
	if isNoteComment(content) {
		return nil, nil
	}

	mentions := util.ParseMentions(content)
	if util.HasMentionAll(mentions) {
		return nil, nil
	}

	if hasAgentOrSquadMention(mentions) {
		return h.resolveMentionedAgentCommentTriggers(ctx, issue, mentions, actorType, actorID, opts)
	}
	if hasMemberMention(mentions) {
		return nil, nil
	}

	if actorType != "member" {

		if issue.AssigneeType.Valid && issue.AssigneeType.String == "squad" {
			if trigger, ok := h.routeAssignedSquadLeaderFallback(ctx, issue, actorType, actorID, opts); ok {
				return []commentAgentTrigger{trigger}, nil
			}
		}
		return nil, nil
	}

	if parentComment != nil && parentComment.AuthorType == "agent" {
		trigger, ok := h.routeReplyToParentAuthor(ctx, issue, parentComment, actorType, actorID, opts)
		if !ok {
			return nil, nil
		}
		if fallback, ok := h.routeAssigneeFallback(ctx, issue, actorType, actorID, opts); ok &&
			uuidToString(fallback.Agent.ID) != uuidToString(trigger.Agent.ID) {
			trigger.EscalationFallback = &commentEscalationFallback{
				Agent: fallback.Agent,
				Squad: fallback.Squad,
			}
		}
		return []commentAgentTrigger{trigger}, nil
	}

	if parentComment != nil {
		triggers, handled := h.routeThreadRootOwners(ctx, issue, parentComment, actorID, opts)
		if handled {
			if len(triggers) == 0 {
				return nil, nil
			}
			if len(triggers) == 1 {
				if fallback, ok := h.routeAssigneeFallback(ctx, issue, actorType, actorID, opts); ok &&
					uuidToString(fallback.Agent.ID) != uuidToString(triggers[0].Agent.ID) {
					triggers[0].EscalationFallback = &commentEscalationFallback{
						Agent: fallback.Agent,
						Squad: fallback.Squad,
					}
				}
			}
			return triggers, nil
		}
	}

	if trigger, ok := h.routeAssigneeFallback(ctx, issue, actorType, actorID, opts); ok {
		return []commentAgentTrigger{trigger}, nil
	}
	return nil, nil
}

func hasAgentOrSquadMention(mentions []util.Mention) bool {
	for _, m := range mentions {
		if m.Type == "agent" || m.Type == "squad" {
			return true
		}
	}
	return false
}

func hasMemberMention(mentions []util.Mention) bool {
	for _, m := range mentions {
		if m.Type == "member" {
			return true
		}
	}
	return false
}

func (h *Handler) routeReplyToParentAuthor(ctx context.Context, issue db.Issue, parent *db.Comment, authorType, authorID string, opts commentTriggerComputeOptions) (commentAgentTrigger, bool) {
	if parent == nil || parent.AuthorType != "agent" || !parent.AuthorID.Valid {
		return commentAgentTrigger{}, false
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          parent.AuthorID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return commentAgentTrigger{}, false
	}
	if !h.canInvokeAgent(ctx, agent, authorType, authorID, opts.effectiveInvoker(), uuidToString(issue.WorkspaceID)) {
		return commentAgentTrigger{}, false
	}
	hasPending, err := h.hasPendingTaskForIssueAndAgent(ctx, issue.ID, parent.AuthorID, opts)
	if err != nil {
		return commentAgentTrigger{}, false
	}
	return commentAgentTrigger{Agent: agent, Source: commentTriggerSourceThreadParent, AlreadyPending: hasPending}, true
}

type conversationRoutedAgentInfo struct {
	SquadID pgtype.UUID
}

func (h *Handler) routeThreadRootOwners(ctx context.Context, issue db.Issue, parent *db.Comment, memberID string, opts commentTriggerComputeOptions) ([]commentAgentTrigger, bool) {
	if parent == nil || !parent.ID.Valid {
		return nil, false
	}
	root, err := h.Queries.GetThreadRoot(ctx, db.GetThreadRootParams{
		CommentID:   parent.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || !root.ID.Valid || root.AuthorType != "member" {
		return nil, false
	}
	return h.routeConversationOwnersForRoot(ctx, issue, root, memberID, opts)
}

func (h *Handler) routeConversationOwnersForRoot(ctx context.Context, issue db.Issue, root db.Comment, memberID string, opts commentTriggerComputeOptions) ([]commentAgentTrigger, bool) {
	if !root.ID.Valid || root.AuthorType != "member" {
		return nil, false
	}
	if trigger, hasExplicitOwner, ok := h.routeFirstExplicitRootMentionOwner(ctx, issue, root, memberID, opts); hasExplicitOwner {
		if !ok {
			return nil, true
		}
		return []commentAgentTrigger{trigger}, true
	}

	rootID := uuidToString(root.ID)
	excludedID := uuidToString(opts.ExcludeTriggerCommentID)

	tasks, err := h.Queries.ListTasksByIssue(ctx, issue.ID)
	if err != nil {
		return nil, false
	}
	routedAgents := make(map[string]conversationRoutedAgentInfo)
	for _, task := range tasks {
		if !task.TriggerCommentID.Valid || !task.AgentID.Valid {
			continue
		}
		if excludedID != "" && uuidToString(task.TriggerCommentID) == excludedID {
			continue
		}
		if uuidToString(task.TriggerCommentID) != rootID {
			continue
		}
		agentID := uuidToString(task.AgentID)
		info := routedAgents[agentID]
		if !info.SquadID.Valid {
			info.SquadID = task.SquadID
		}
		routedAgents[agentID] = info
	}
	if len(routedAgents) == 0 {
		return nil, false
	}

	triggers := make([]commentAgentTrigger, 0, len(routedAgents))
	for agentID, info := range routedAgents {
		trigger, ok := h.routeConversationContinuationToAgent(ctx, issue, parseUUID(agentID), info.SquadID, memberID, opts)
		if ok {
			triggers = append(triggers, trigger)
		}
	}
	return triggers, true
}

func (h *Handler) routeFirstExplicitRootMentionOwner(ctx context.Context, issue db.Issue, root db.Comment, memberID string, opts commentTriggerComputeOptions) (commentAgentTrigger, bool, bool) {
	for _, mention := range util.ParseMentions(root.Content) {
		switch mention.Type {
		case "agent":
			agentID, err := util.ParseUUID(mention.ID)
			if err != nil {
				return commentAgentTrigger{}, true, false
			}
			trigger, ok := h.routeConversationContinuationToAgent(ctx, issue, agentID, pgtype.UUID{}, memberID, opts)
			return trigger, true, ok
		case "squad":
			squadID, err := util.ParseUUID(mention.ID)
			if err != nil {
				return commentAgentTrigger{}, true, false
			}
			squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
				ID:          squadID,
				WorkspaceID: issue.WorkspaceID,
			})
			if err != nil {
				return commentAgentTrigger{}, true, false
			}
			trigger, ok := h.routeConversationContinuationToAgent(ctx, issue, squad.LeaderID, squadID, memberID, opts)
			return trigger, true, ok
		}
	}
	return commentAgentTrigger{}, false, false
}

func (h *Handler) routeConversationContinuationToAgent(ctx context.Context, issue db.Issue, agentID, squadID pgtype.UUID, memberID string, opts commentTriggerComputeOptions) (commentAgentTrigger, bool) {
	if !agentID.Valid {
		return commentAgentTrigger{}, false
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          agentID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return commentAgentTrigger{}, false
	}

	if !h.canInvokeAgent(ctx, agent, "member", memberID, scopedInvokeAuthority(memberID), uuidToString(issue.WorkspaceID)) {
		return commentAgentTrigger{}, false
	}
	hasPending, err := h.hasPendingTaskForIssueAndAgent(ctx, issue.ID, agentID, opts)
	if err != nil {
		return commentAgentTrigger{}, false
	}
	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceConversation, AlreadyPending: hasPending}
	if squadID.Valid {
		if squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          squadID,
			WorkspaceID: issue.WorkspaceID,
		}); err == nil {
			trigger.Squad = &squad
		}
	}
	return trigger, true
}

func (h *Handler) routeAssigneeFallback(ctx context.Context, issue db.Issue, authorType, authorID string, opts commentTriggerComputeOptions) (commentAgentTrigger, bool) {
	if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return commentAgentTrigger{}, false
	}
	switch issue.AssigneeType.String {
	case "agent":
		agent, hasPending, ok := h.assigneeFallbackAgent(ctx, issue, authorType, authorID, opts)
		if !ok {
			return commentAgentTrigger{}, false
		}
		return commentAgentTrigger{Agent: agent, Source: commentTriggerSourceIssueAssignee, AlreadyPending: hasPending}, true
	case "squad":
		return h.routeAssignedSquadLeaderFallback(ctx, issue, authorType, authorID, opts)
	default:
		return commentAgentTrigger{}, false
	}
}

func (h *Handler) routeAssignedSquadLeaderFallback(ctx context.Context, issue db.Issue, authorType, authorID string, opts commentTriggerComputeOptions) (commentAgentTrigger, bool) {
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return commentAgentTrigger{}, false
	}
	if authorType == "agent" && authorID == uuidToString(squad.LeaderID) &&
		h.shouldSuppressSquadLeaderSelfTrigger(ctx, issue.ID, squad.LeaderID, squad.ID) {
		return commentAgentTrigger{}, false
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          squad.LeaderID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return commentAgentTrigger{}, false
	}
	if !h.canInvokeAgent(ctx, agent, authorType, authorID, opts.effectiveInvoker(), uuidToString(issue.WorkspaceID)) {
		return commentAgentTrigger{}, false
	}
	hasPending, err := h.hasPendingTaskForIssueAndAgent(ctx, issue.ID, squad.LeaderID, opts)
	if err != nil {
		return commentAgentTrigger{}, false
	}
	return commentAgentTrigger{Agent: agent, Source: commentTriggerSourceIssueAssignee, Squad: &squad, AlreadyPending: hasPending}, true
}

func (h *Handler) hasPendingTaskForIssueAndAgent(ctx context.Context, issueID, agentID pgtype.UUID, opts commentTriggerComputeOptions) (bool, error) {

	headSha := h.TaskService.ResolveIssueReviewSHAParam(ctx, issueID)
	if opts.ExcludeTriggerCommentID.Valid {
		return h.Queries.HasPendingTaskForIssueAndAgentExcludingTriggerComment(ctx, db.HasPendingTaskForIssueAndAgentExcludingTriggerCommentParams{
			IssueID:                 issueID,
			AgentID:                 agentID,
			ExcludeTriggerCommentID: opts.ExcludeTriggerCommentID,
			HeadSha:                 headSha,
		})
	}
	return h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: agentID,
		HeadSha: headSha,
	})
}

type commentMentionTarget struct {
	TargetType  string
	TargetID    string
	ExecAgentID string
	Status      DispatchStatus
	ReasonCode  DispatchReasonCode
}

func (h *Handler) resolveMentionedAgentCommentTriggers(ctx context.Context, issue db.Issue, mentions []util.Mention, authorType, authorID string, opts commentTriggerComputeOptions) ([]commentAgentTrigger, []commentMentionTarget) {
	wsID := uuidToString(issue.WorkspaceID)
	triggers := make([]commentAgentTrigger, 0, len(mentions))

	seen := make(map[string]int, len(mentions))
	add := func(trigger commentAgentTrigger) {
		id := uuidToString(trigger.Agent.ID)
		if idx, ok := seen[id]; ok {
			if triggers[idx].Source != commentTriggerSourceMentionSquadLeader &&
				trigger.Source == commentTriggerSourceMentionSquadLeader {
				triggers[idx] = trigger
			}
			return
		}
		seen[id] = len(triggers)
		triggers = append(triggers, trigger)
	}

	var targets []commentMentionTarget
	targetSeen := make(map[string]struct{}, len(mentions))
	addTarget := func(t commentMentionTarget) {
		key := t.TargetType + ":" + t.TargetID
		if _, ok := targetSeen[key]; ok {
			return
		}
		targetSeen[key] = struct{}{}
		targets = append(targets, t)
	}

	blockTarget := func(targetType, targetID string, reason DispatchReasonCode) {
		addTarget(commentMentionTarget{TargetType: targetType, TargetID: targetID, Status: DispatchBlocked, ReasonCode: reason})
	}
	for _, m := range mentions {
		if m.Type == "squad" {

			squadUUID := parseUUID(m.ID)
			squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
				ID:          squadUUID,
				WorkspaceID: issue.WorkspaceID,
			})
			if err != nil {
				blockTarget("squad", m.ID, ReasonTargetUnavailable)
				continue
			}
			leaderID := squad.LeaderID

			if authorType == "agent" && authorID == uuidToString(leaderID) &&
				h.shouldSuppressSquadLeaderSelfTrigger(ctx, issue.ID, leaderID, squad.ID) {
				active, activeErr := h.hasActiveTaskForIssueAndAgent(ctx, issue.ID, leaderID)
				status, reason := decideSuppressedLeaderOutcome(active, activeErr)
				addTarget(commentMentionTarget{TargetType: "squad", TargetID: m.ID, Status: status, ReasonCode: reason})
				continue
			}
			agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
				ID:          leaderID,
				WorkspaceID: issue.WorkspaceID,
			})
			if err != nil {
				blockTarget("squad", m.ID, ReasonTargetUnavailable)
				continue
			}

			if !h.canInvokeAgent(ctx, agent, authorType, authorID, opts.effectiveInvoker(), wsID) {
				blockTarget("squad", m.ID, ReasonInvocationNotAllowed)
				continue
			}
			if agent.ArchivedAt.Valid {
				blockTarget("squad", m.ID, ReasonTargetUnavailable)
				continue
			}
			if !agent.RuntimeID.Valid {
				blockTarget("squad", m.ID, ReasonRuntimeOffline)
				continue
			}
			hasPending, err := h.hasPendingTaskForIssueAndAgent(ctx, issue.ID, leaderID, opts)
			if err != nil {
				blockTarget("squad", m.ID, ReasonInternalError)
				continue
			}
			add(commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionSquadLeader, Squad: &squad, AlreadyPending: hasPending})
			addTarget(commentMentionTarget{TargetType: "squad", TargetID: m.ID, ExecAgentID: uuidToString(leaderID)})
			continue
		}
		if m.Type != "agent" {
			continue
		}
		agentUUID := parseUUID(m.ID)

		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          agentUUID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {

			blockTarget("agent", m.ID, ReasonInvocationNotAllowed)
			continue
		}

		if !h.canInvokeAgent(ctx, agent, authorType, authorID, opts.effectiveInvoker(), wsID) {
			blockTarget("agent", m.ID, ReasonInvocationNotAllowed)
			continue
		}
		if agent.ArchivedAt.Valid {
			blockTarget("agent", m.ID, ReasonTargetUnavailable)
			continue
		}
		if !agent.RuntimeID.Valid {
			blockTarget("agent", m.ID, ReasonRuntimeOffline)
			continue
		}
		hasPending, err := h.hasPendingTaskForIssueAndAgent(ctx, issue.ID, agentUUID, opts)
		if err != nil {
			blockTarget("agent", m.ID, ReasonInternalError)
			continue
		}
		add(commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent, AlreadyPending: hasPending})
		addTarget(commentMentionTarget{TargetType: "agent", TargetID: m.ID, ExecAgentID: uuidToString(agentUUID)})
	}
	return triggers, targets
}

func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	commentId := chi.URLParam(r, "commentId")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	commentUUID, ok := parseUUIDOrBadRequest(w, commentId, "comment id")
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	existing, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          commentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	isAuthor := existing.AuthorType == actorType && uuidToString(existing.AuthorID) == actorID
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	if !isAuthor && !isAdmin {
		writeError(w, http.StatusForbidden, "only comment author or admin can edit")
		return
	}

	var req struct {
		Content          string    `json:"content"`
		AttachmentIDs    *[]string `json:"attachment_ids"`
		SuppressAgentIDs []string  `json:"suppress_agent_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Content = sanitizeNullBytes(req.Content)
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	var attachmentIDs []pgtype.UUID
	replaceAttachments := req.AttachmentIDs != nil
	if replaceAttachments {
		var ok bool
		attachmentIDs, ok = parseUUIDSliceOrBadRequest(w, *req.AttachmentIDs, "attachment_ids")
		if !ok {
			return
		}
	}
	suppressAgentIDs, ok := parseUUIDSliceOrBadRequest(w, req.SuppressAgentIDs, "suppress_agent_ids")
	if !ok {
		return
	}

	oldContent := existing.Content

	sourceTaskID := existing.SourceTaskID
	var triggerIssue *db.Issue
	var cancelled []db.AgentTaskQueue
	if oldContent != req.Content {
		issue, err := h.Queries.GetIssue(r.Context(), existing.IssueID)
		if err != nil {
			slog.Warn("load issue for edit post-processing failed", "issue_id", uuidToString(existing.IssueID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to load issue")
			return
		}
		triggerIssue = &issue

		if actorType == "agent" && isAuthor {
			sourceTaskID = h.commentSourceTaskIDForIssue(r, issue, actorID)
		} else {
			sourceTaskID = pgtype.UUID{}
		}
		cancelled, err = h.TaskService.CancelTasksByTriggerComment(r.Context(), existing.ID)
		if err != nil {
			slog.Warn("cancel tasks for edited comment failed", "comment_id", uuidToString(existing.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to prepare comment edit")
			return
		}
	}

	comment, err := h.Queries.UpdateComment(r.Context(), db.UpdateCommentParams{
		ID:           commentUUID,
		Content:      req.Content,
		SourceTaskID: sourceTaskID,
	})
	if err != nil {
		slog.Warn("update comment failed", append(logger.RequestAttrs(r), "error", err, "comment_id", commentId)...)
		if triggerIssue != nil {

			h.retriggerCancelledTaskSurvivors(r.Context(), *triggerIssue, cancelled, pgtype.UUID{})
		}
		writeError(w, http.StatusInternalServerError, "failed to update comment")
		return
	}
	retriggerEditedComment := func() []CommentTriggerOutcome {
		if oldContent == comment.Content {
			return nil
		}
		issue := *triggerIssue
		var parentComment *db.Comment
		if existing.ParentID.Valid {
			parent, err := h.Queries.GetComment(r.Context(), existing.ParentID)
			if err == nil {
				parentComment = &parent
			}
		}

		h.retriggerCancelledTaskSurvivors(r.Context(), issue, cancelled, existing.ID)

		delegationAuthority := h.autopilotDelegationAuthorityFromComment(r.Context(), issue, comment)

		return h.triggerTasksForComment(r.Context(), issue, comment, parentComment, actorType, actorID, h.invokeAuthorityFromRequest(r, actorType, actorID, issueInvokeScope(issue.ID)), delegationAuthority, suppressAgentIDs)
	}

	if replaceAttachments {
		if err := h.Queries.ReplaceCommentAttachments(r.Context(), db.ReplaceCommentAttachmentsParams{
			CommentID:     comment.ID,
			IssueID:       existing.IssueID,
			AttachmentIds: attachmentIDs,
		}); err != nil {
			slog.Error("failed to replace comment attachments", "error", err)

			retriggerEditedComment()
			writeError(w, http.StatusInternalServerError, "failed to update attachments")
			return
		}
	}

	grouped := h.groupReactions(r, []pgtype.UUID{comment.ID})
	groupedAtt := h.groupAttachments(r, []pgtype.UUID{comment.ID})
	cid := uuidToString(comment.ID)
	resp := commentToResponse(comment, grouped[cid], groupedAtt[cid])
	slog.Info("comment updated", append(logger.RequestAttrs(r), "comment_id", commentId)...)
	h.publish(protocol.EventCommentUpdated, workspaceID, actorType, actorID, map[string]any{"comment": resp})

	resp.TriggerOutcomes = retriggerEditedComment()

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	commentId := chi.URLParam(r, "commentId")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	commentUUID, ok := parseUUIDOrBadRequest(w, commentId, "comment id")
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          commentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	isAuthor := comment.AuthorType == actorType && uuidToString(comment.AuthorID) == actorID
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	if !isAuthor && !isAdmin {
		writeError(w, http.StatusForbidden, "only comment author or admin can delete")
		return
	}
	issue, err := h.Queries.GetIssue(r.Context(), comment.IssueID)
	if err != nil {
		slog.Warn("load issue for delete post-processing failed", "issue_id", uuidToString(comment.IssueID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load issue")
		return
	}

	attachmentURLs, _ := h.Queries.ListAttachmentURLsByCommentID(r.Context(), comment.ID)

	cancelled, cancelErr := h.TaskService.CancelTasksByTriggerComment(r.Context(), comment.ID)
	if cancelErr != nil {
		slog.Warn("cancel tasks for deleted trigger comment failed", append(logger.RequestAttrs(r), "error", cancelErr, "comment_id", commentId)...)
	}

	if err := h.Queries.DeleteComment(r.Context(), db.DeleteCommentParams{
		ID:          comment.ID,
		WorkspaceID: comment.WorkspaceID,
	}); err != nil {
		slog.Warn("delete comment failed", append(logger.RequestAttrs(r), "error", err, "comment_id", commentId)...)

		h.retriggerCancelledTaskSurvivors(r.Context(), issue, cancelled, pgtype.UUID{})
		writeError(w, http.StatusInternalServerError, "failed to delete comment")
		return
	}

	h.deleteS3Objects(r.Context(), attachmentURLs)
	slog.Info("comment deleted", append(logger.RequestAttrs(r), "comment_id", commentId, "issue_id", uuidToString(comment.IssueID))...)
	h.publish(protocol.EventCommentDeleted, workspaceID, actorType, actorID, map[string]any{
		"comment_id": uuidToString(comment.ID),
		"issue_id":   uuidToString(comment.IssueID),
	})
	h.retriggerCancelledTaskSurvivors(r.Context(), issue, cancelled, comment.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) retriggerCancelledTaskSurvivors(ctx context.Context, issue db.Issue, cancelled []db.AgentTaskQueue, excludedCommentID pgtype.UUID) {
	if len(cancelled) == 0 {
		return
	}
	targetsByComment := make(map[string]map[string]pgtype.UUID)
	for _, task := range cancelled {
		if !task.AgentID.Valid {
			continue
		}
		plannedCommentIDs := append([]pgtype.UUID{}, task.CoalescedCommentIds...)
		if task.TriggerCommentID.Valid {
			plannedCommentIDs = append(plannedCommentIDs, task.TriggerCommentID)
		}
		for _, commentID := range plannedCommentIDs {
			if !commentID.Valid || (excludedCommentID.Valid && commentID == excludedCommentID) {
				continue
			}
			commentKey := uuidToString(commentID)
			if targetsByComment[commentKey] == nil {
				targetsByComment[commentKey] = make(map[string]pgtype.UUID)
			}
			targetsByComment[commentKey][uuidToString(task.AgentID)] = task.AgentID
		}
	}

	comments := make([]db.Comment, 0, len(targetsByComment))
	for commentID := range targetsByComment {
		comment, err := h.Queries.GetComment(ctx, parseUUID(commentID))
		if err != nil {
			slog.Warn("retrigger cancelled comment batch: load survivor failed",
				"issue_id", uuidToString(issue.ID), "comment_id", commentID, "error", err)
			continue
		}
		if comment.IssueID != issue.ID {
			continue
		}
		comments = append(comments, comment)
	}
	sort.Slice(comments, func(i, j int) bool {
		if !comments[i].CreatedAt.Time.Equal(comments[j].CreatedAt.Time) {
			return comments[i].CreatedAt.Time.Before(comments[j].CreatedAt.Time)
		}
		return uuidToString(comments[i].ID) < uuidToString(comments[j].ID)
	})

	for i := range comments {
		comment := comments[i]
		if isNoteComment(comment.Content) {
			continue
		}
		var parentComment *db.Comment
		if comment.ParentID.Valid {
			if parent, err := h.Queries.GetComment(ctx, comment.ParentID); err == nil {
				parentComment = &parent
			}
		}
		actorType := comment.AuthorType
		actorID := uuidToString(comment.AuthorID)

		originator := scopedInvokeAuthority(actorID)
		var delegationAuthority string
		if actorType != "member" {
			originator = scopedInvokeAuthority(uuidToString(h.TaskService.ResolveOriginatorFromTriggerComment(ctx, issue.WorkspaceID, comment.ID)))

			delegationAuthority = h.autopilotDelegationAuthorityFromComment(ctx, issue, comment)
		}
		triggers, _ := h.computeCommentAgentTriggers(ctx, issue, comment.Content, parentComment, actorType, actorID, commentTriggerComputeOptions{
			ExcludeTriggerCommentID:            comment.ID,
			Originator:                         originator,
			AutopilotDelegationAuthorityUserID: delegationAuthority,
		})
		targets := targetsByComment[uuidToString(comment.ID)]
		scoped := make([]commentAgentTrigger, 0, len(targets))
		for _, trigger := range triggers {
			if _, ok := targets[uuidToString(trigger.Agent.ID)]; ok {
				scoped = append(scoped, trigger)
			}
		}
		if len(scoped) > 0 {
			h.enqueueCommentAgentTriggers(ctx, issue, comment.ID, scoped)
		}
	}
}

func (h *Handler) loadCommentForActor(w http.ResponseWriter, r *http.Request) (db.Comment, string, string, string, bool) {
	commentId := chi.URLParam(r, "commentId")
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Comment{}, "", "", "", false
	}
	commentUUID, ok := parseUUIDOrBadRequest(w, commentId, "comment id")
	if !ok {
		return db.Comment{}, "", "", "", false
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Comment{}, "", "", "", false
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return db.Comment{}, "", "", "", false
	}
	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          commentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return db.Comment{}, "", "", "", false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	return comment, workspaceID, actorType, actorID, true
}

func (h *Handler) ResolveComment(w http.ResponseWriter, r *http.Request) {
	comment, workspaceID, actorType, actorID, ok := h.loadCommentForActor(w, r)
	if !ok {
		return
	}
	wasResolved := comment.ResolvedAt.Valid

	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid actor id")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve comment")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	cleared, err := qtx.ClearOtherThreadResolutions(r.Context(), db.ClearOtherThreadResolutionsParams{
		TargetID:    comment.ID,
		IssueID:     comment.IssueID,
		WorkspaceID: comment.WorkspaceID,
	})
	if err != nil {
		slog.Warn("clear other thread resolutions failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(comment.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to resolve comment")
		return
	}

	updated, err := qtx.ResolveComment(r.Context(), db.ResolveCommentParams{
		ID:             comment.ID,
		ResolvedByType: pgtype.Text{String: actorType, Valid: true},
		ResolvedByID:   actorUUID,
	})
	if err != nil {
		slog.Warn("resolve comment failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(comment.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to resolve comment")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("resolve comment commit failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(comment.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to resolve comment")
		return
	}

	for _, c := range cleared {
		clearedID := uuidToString(c.ID)
		clearedReactions := h.groupReactions(r, []pgtype.UUID{c.ID})
		clearedAtt := h.groupAttachments(r, []pgtype.UUID{c.ID})
		clearedResp := commentToResponse(c, clearedReactions[clearedID], clearedAtt[clearedID])
		slog.Info("comment unresolved (replaced)", append(logger.RequestAttrs(r), "comment_id", clearedID)...)
		h.publish(protocol.EventCommentUnresolved, workspaceID, actorType, actorID, map[string]any{"comment": clearedResp})
	}

	grouped := h.groupReactions(r, []pgtype.UUID{updated.ID})
	groupedAtt := h.groupAttachments(r, []pgtype.UUID{updated.ID})
	cid := uuidToString(updated.ID)
	resp := commentToResponse(updated, grouped[cid], groupedAtt[cid])

	if !wasResolved {
		slog.Info("comment resolved", append(logger.RequestAttrs(r), "comment_id", cid)...)
		h.publish(protocol.EventCommentResolved, workspaceID, actorType, actorID, map[string]any{"comment": resp})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UnresolveComment(w http.ResponseWriter, r *http.Request) {
	comment, workspaceID, actorType, actorID, ok := h.loadCommentForActor(w, r)
	if !ok {
		return
	}
	wasResolved := comment.ResolvedAt.Valid

	updated, err := h.Queries.UnresolveComment(r.Context(), comment.ID)
	if err != nil {
		slog.Warn("unresolve comment failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(comment.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to unresolve comment")
		return
	}

	grouped := h.groupReactions(r, []pgtype.UUID{updated.ID})
	groupedAtt := h.groupAttachments(r, []pgtype.UUID{updated.ID})
	cid := uuidToString(updated.ID)
	resp := commentToResponse(updated, grouped[cid], groupedAtt[cid])

	if wasResolved {
		slog.Info("comment unresolved", append(logger.RequestAttrs(r), "comment_id", cid)...)
		h.publish(protocol.EventCommentUnresolved, workspaceID, actorType, actorID, map[string]any{"comment": resp})
	}
	writeJSON(w, http.StatusOK, resp)
}
