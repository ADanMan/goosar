package handler

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type TimelineEntry struct {
	Type string `json:"type"`
	ID   string `json:"id"`

	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id"`
	CreatedAt string `json:"created_at"`

	Action  *string         `json:"action,omitempty"`
	Details json.RawMessage `json:"details,omitempty"`

	Content        *string              `json:"content,omitempty"`
	ParentID       *string              `json:"parent_id,omitempty"`
	UpdatedAt      *string              `json:"updated_at,omitempty"`
	CommentType    *string              `json:"comment_type,omitempty"`
	Reactions      []ReactionResponse   `json:"reactions,omitempty"`
	Attachments    []AttachmentResponse `json:"attachments,omitempty"`
	ResolvedAt     *string              `json:"resolved_at,omitempty"`
	ResolvedByType *string              `json:"resolved_by_type,omitempty"`
	ResolvedByID   *string              `json:"resolved_by_id,omitempty"`
	SourceTaskID   *string              `json:"source_task_id,omitempty"`
}

const timelineHardCap = 2000

type timelinePaginatedResponse struct {
	Entries       []TimelineEntry `json:"entries"`
	NextCursor    *string         `json:"next_cursor"`
	PrevCursor    *string         `json:"prev_cursor"`
	HasMoreBefore bool            `json:"has_more_before"`
	HasMoreAfter  bool            `json:"has_more_after"`
	TargetIndex   *int            `json:"target_index,omitempty"`
}

func (h *Handler) ListTimeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	ctx := r.Context()

	comments, err := h.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Limit:       timelineHardCap,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	activities, err := h.Queries.ListActivitiesForIssue(ctx, db.ListActivitiesForIssueParams{
		IssueID: issue.ID,
		Limit:   timelineHardCap,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}

	q := r.URL.Query()
	wantWrapped := q.Get("limit") != "" || q.Get("before") != "" ||
		q.Get("after") != "" || q.Get("around") != ""

	if wantWrapped {
		entries := h.mergeTimeline(r, comments, activities, false)
		if entries == nil {
			entries = []TimelineEntry{}
		}
		resp := timelinePaginatedResponse{Entries: entries}

		if anchor := q.Get("around"); anchor != "" {
			for i, e := range entries {
				if e.ID == anchor {
					idx := i
					resp.TargetIndex = &idx
					break
				}
			}
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	entries := h.mergeTimeline(r, comments, activities, true)
	if entries == nil {
		entries = []TimelineEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (h *Handler) mergeTimeline(r *http.Request, comments []db.Comment, activities []db.ActivityLog, ascending bool) []TimelineEntry {
	out := make([]TimelineEntry, 0, len(comments)+len(activities))
	out = append(out, h.commentsToEntries(r, comments)...)
	for _, a := range activities {
		out = append(out, activityToEntry(a))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			if ascending {
				return out[i].CreatedAt < out[j].CreatedAt
			}
			return out[i].CreatedAt > out[j].CreatedAt
		}
		if ascending {
			return out[i].ID < out[j].ID
		}
		return out[i].ID > out[j].ID
	})
	return out
}

func (h *Handler) commentsToEntries(r *http.Request, comments []db.Comment) []TimelineEntry {
	if len(comments) == 0 {
		return nil
	}
	ids := make([]pgtype.UUID, len(comments))
	for i, c := range comments {
		ids[i] = c.ID
	}
	reactions := h.groupReactions(r, ids)
	attachments := h.groupAttachments(r, ids)

	out := make([]TimelineEntry, len(comments))
	for i, c := range comments {
		content := c.Content
		commentType := c.Type
		updatedAt := timestampToString(c.UpdatedAt)
		cid := uuidToString(c.ID)
		out[i] = TimelineEntry{
			Type:           "comment",
			ID:             cid,
			ActorType:      c.AuthorType,
			ActorID:        uuidToString(c.AuthorID),
			Content:        &content,
			CommentType:    &commentType,
			ParentID:       uuidToPtr(c.ParentID),
			CreatedAt:      timestampToString(c.CreatedAt),
			UpdatedAt:      &updatedAt,
			Reactions:      reactions[cid],
			Attachments:    attachments[cid],
			ResolvedAt:     timestampToPtr(c.ResolvedAt),
			ResolvedByType: textToPtr(c.ResolvedByType),
			ResolvedByID:   uuidToPtr(c.ResolvedByID),

			SourceTaskID: responseSourceTaskID(c),
		}
	}
	return out
}

func activityToEntry(a db.ActivityLog) TimelineEntry {
	action := a.Action
	actorType := ""
	if a.ActorType.Valid {
		actorType = a.ActorType.String
	}
	return TimelineEntry{
		Type:      "activity",
		ID:        uuidToString(a.ID),
		ActorType: actorType,
		ActorID:   uuidToString(a.ActorID),
		Action:    &action,
		Details:   a.Details,
		CreatedAt: timestampToString(a.CreatedAt),
	}
}

type AssigneeFrequencyEntry struct {
	AssigneeType string `json:"assignee_type"`
	AssigneeID   string `json:"assignee_id"`
	Frequency    int64  `json:"frequency"`
}

func (h *Handler) GetAssigneeFrequency(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	freq := map[string]int64{}

	activityCounts, err := h.Queries.CountAssigneeChangesByActor(r.Context(), db.CountAssigneeChangesByActorParams{
		WorkspaceID: parseUUID(workspaceID),
		ActorID:     parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get assignee frequency")
		return
	}
	for _, row := range activityCounts {
		aType, _ := row.AssigneeType.(string)
		aID, _ := row.AssigneeID.(string)
		if aType != "" && aID != "" {
			freq[aType+":"+aID] += row.Frequency
		}
	}

	issueCounts, err := h.Queries.CountCreatedIssueAssignees(r.Context(), db.CountCreatedIssueAssigneesParams{
		WorkspaceID: parseUUID(workspaceID),
		CreatorID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get assignee frequency")
		return
	}
	for _, row := range issueCounts {
		if !row.AssigneeType.Valid || !row.AssigneeID.Valid {
			continue
		}
		key := row.AssigneeType.String + ":" + uuidToString(row.AssigneeID)
		freq[key] += row.Frequency
	}

	result := make([]AssigneeFrequencyEntry, 0, len(freq))
	for key, count := range freq {

		var aType, aID string
		for i := 0; i < len(key); i++ {
			if key[i] == ':' {
				aType = key[:i]
				aID = key[i+1:]
				break
			}
		}
		result = append(result, AssigneeFrequencyEntry{
			AssigneeType: aType,
			AssigneeID:   aID,
			Frequency:    count,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Frequency > result[j].Frequency
	})

	writeJSON(w, http.StatusOK, result)
}
