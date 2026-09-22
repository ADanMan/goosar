package handler

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func (h *Handler) notifyParentOfChildDone(ctx context.Context, prev, issue db.Issue) {
	if !issue.ParentIssueID.Valid {
		return
	}

	if isTerminalChildStatus(prev.Status) || !isTerminalChildStatus(issue.Status) {
		return
	}
	parent, err := h.Queries.GetIssue(ctx, issue.ParentIssueID)
	if err != nil {
		slog.Warn("child done: failed to load parent",
			"error", err,
			"child_id", uuidToString(issue.ID),
			"parent_id", uuidToString(issue.ParentIssueID))
		return
	}
	if parent.Status == "done" || parent.Status == "cancelled" {
		return
	}

	if parent.Status == "backlog" {
		return
	}

	if parent.AssigneeType.Valid && parent.AssigneeType.String == "member" {
		return
	}

	children, err := h.Queries.ListChildIssues(ctx, parent.ID)
	if err != nil {
		slog.Warn("child done: failed to list siblings for stage barrier",
			"error", err,
			"child_id", uuidToString(issue.ID),
			"parent_id", uuidToString(parent.ID))
		return
	}
	if !stageBarrierClosed(children, issue) {
		return
	}
	staged := siblingsAreStaged(children)

	var closedStage int32
	if staged {
		closedStage = issue.Stage.Int32
	}
	h.postChildDoneComment(ctx, parent, issue, children, staged, closedStage, false)
}

func (h *Handler) notifyParentsOfBatchChildDone(ctx context.Context, completed []db.Issue) {
	if len(completed) == 0 {
		return
	}

	type parentGroup struct {
		parentID pgtype.UUID
		children []db.Issue
	}
	var groups []*parentGroup
	index := map[string]*parentGroup{}
	for _, c := range completed {
		if !c.ParentIssueID.Valid {
			continue
		}
		key := uuidToString(c.ParentIssueID)
		g, ok := index[key]
		if !ok {
			g = &parentGroup{parentID: c.ParentIssueID}
			index[key] = g
			groups = append(groups, g)
		}
		g.children = append(g.children, c)
	}

	for _, g := range groups {
		parent, err := h.Queries.GetIssue(ctx, g.parentID)
		if err != nil {
			slog.Warn("batch child done: failed to load parent",
				"error", err, "parent_id", uuidToString(g.parentID))
			continue
		}

		if parent.Status == "done" || parent.Status == "cancelled" {
			continue
		}
		if parent.Status == "backlog" {
			continue
		}
		if parent.AssigneeType.Valid && parent.AssigneeType.String == "member" {
			continue
		}

		children, err := h.Queries.ListChildIssues(ctx, parent.ID)
		if err != nil {
			slog.Warn("batch child done: failed to list siblings for stage barrier",
				"error", err, "parent_id", uuidToString(parent.ID))
			continue
		}

		batch := len(g.children) > 1
		if !siblingsAreStaged(children) {

			if !stageBarrierClosed(children, g.children[0]) {
				continue
			}
			h.postChildDoneComment(ctx, parent, g.children[0], children, false, 0, batch)
			continue
		}

		var rep db.Issue
		var bestStage int32
		found := false
		for _, c := range g.children {
			if !c.Stage.Valid {
				continue
			}
			if !stageBarrierClosed(children, c) {
				continue
			}
			if !found || c.Stage.Int32 > bestStage {
				found = true
				bestStage = c.Stage.Int32
				rep = c
			}
		}
		if !found {
			continue
		}
		h.postChildDoneComment(ctx, parent, rep, children, true, bestStage, batch)
	}
}

func (h *Handler) postChildDoneComment(ctx context.Context, parent, completed db.Issue, children []db.Issue, staged bool, closedStage int32, batch bool) {
	prefix := h.getIssuePrefix(ctx, completed.WorkspaceID)
	identifier := prefix + "-" + strconv.Itoa(int(completed.Number))
	childID := uuidToString(completed.ID)
	title := sanitizeChildTitleForSystemComment(completed.Title)
	parentID := uuidToString(parent.ID)

	mentionPrefix := h.buildParentAssigneeMention(ctx, parent)

	var content string
	if staged {
		summary, nextStage := stageProgressSummary(children, closedStage)
		advance := stageAdvanceInstruction(nextStage, parentID)
		if batch {
			content = fmt.Sprintf(
				"%sStage %d of this issue is complete — its sub-issues just finished together in a batch update, most recently [%s](mention://issue/%s) — \"%s\". Stage progress — %s.%s",
				mentionPrefix, closedStage, identifier, childID, title, summary, advance,
			)
		} else {
			content = fmt.Sprintf(
				"%sStage %d of this issue is complete — its last sub-issue [%s](mention://issue/%s) — \"%s\" — just finished. Stage progress — %s.%s",
				mentionPrefix, closedStage, identifier, childID, title, summary, advance,
			)
		}
	} else {
		if batch {
			content = fmt.Sprintf(
				"%sAll sub-issues are complete — they just finished together in a batch update, most recently [%s](mention://issue/%s) — \"%s\". Continue the parent: synthesize the children's results and move it forward, or — if nothing remains — run `goosar issue status %s in_review` to mark the parent ready for review.",
				mentionPrefix, identifier, childID, title, parentID,
			)
		} else {
			content = fmt.Sprintf(
				"%sAll sub-issues are complete — the last one, [%s](mention://issue/%s) — \"%s\", just finished. Continue the parent: synthesize the children's results and move it forward, or — if nothing remains — run `goosar issue status %s in_review` to mark the parent ready for review.",
				mentionPrefix, identifier, childID, title, parentID,
			)
		}
	}

	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     parent.ID,
		WorkspaceID: parent.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("child done: create system comment failed",
			"error", err,
			"child_id", childID,
			"parent_id", uuidToString(parent.ID))
		return
	}

	h.publish(protocol.EventCommentCreated, uuidToString(parent.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         parent.Title,
		"issue_assignee_type": textToPtr(parent.AssigneeType),
		"issue_assignee_id":   uuidToPtr(parent.AssigneeID),
		"issue_status":        parent.Status,
	})

	h.dispatchParentAssigneeTrigger(ctx, parent, comment)
}

func isTerminalChildStatus(status string) bool {
	return status == "done" || status == "cancelled"
}

func siblingsAreStaged(children []db.Issue) bool {
	for _, c := range children {
		if c.Stage.Valid {
			return true
		}
	}
	return false
}

func stageBarrierClosed(children []db.Issue, completed db.Issue) bool {
	if !siblingsAreStaged(children) {
		for _, c := range children {
			if !isTerminalChildStatus(c.Status) {
				return false
			}
		}
		return true
	}

	if !completed.Stage.Valid {
		return false
	}
	s := completed.Stage.Int32
	for _, c := range children {
		if !c.Stage.Valid {
			continue
		}
		if c.Stage.Int32 <= s && !isTerminalChildStatus(c.Status) {
			return false
		}
	}
	return true
}

func stageProgressSummary(children []db.Issue, closedStage int32) (summary string, nextStage int32) {
	type agg struct{ total, done int }
	byStage := map[int32]*agg{}
	order := []int32{}
	for _, c := range children {
		if !c.Stage.Valid {
			continue
		}
		s := c.Stage.Int32
		a, ok := byStage[s]
		if !ok {
			a = &agg{}
			byStage[s] = a
			order = append(order, s)
		}
		a.total++
		if isTerminalChildStatus(c.Status) {
			a.done++
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	parts := make([]string, 0, len(order))
	for _, s := range order {
		a := byStage[s]
		label := fmt.Sprintf("Stage %d: %d/%d done", s, a.done, a.total)
		if nextStage == 0 && s > closedStage && a.done < a.total {
			nextStage = s
			label += " (next)"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "; "), nextStage
}

func stageAdvanceInstruction(nextStage int32, parentID string) string {
	if nextStage > 0 {
		return fmt.Sprintf(
			" Stage %d is next. Review the full layout with `goosar issue children %s`, and if Stage %d's dependencies are satisfied promote its `backlog` sub-issues to `todo` to continue. Read each sub-issue's description first and only promote items whose stated dependencies are already met — do not rely on this parent's higher-level breakdown alone. If a description conflicts with that breakdown, leave it `backlog` and post a comment to confirm first.",
			nextStage, parentID, nextStage,
		)
	}
	return fmt.Sprintf(" Completing this stage does not mean the whole issue is done. Decide whether the issue is actually complete — if so, synthesize the results and run `goosar issue status %s in_review` to mark the parent ready for review — or whether the next stage still needs to be created, in which case create that stage and its sub-issues now.", parentID)
}

func sanitizeChildTitleForSystemComment(title string) string {

	cleaned := strings.ReplaceAll(title, "](mention://", "] (mention-stripped://")
	return cleaned
}

func (h *Handler) buildParentAssigneeMention(ctx context.Context, parent db.Issue) string {
	if !parent.AssigneeType.Valid || !parent.AssigneeID.Valid {
		return ""
	}
	label, ok := h.resolveAssigneeMentionLabel(ctx, parent.WorkspaceID, parent.AssigneeType.String, parent.AssigneeID)
	if !ok {
		return ""
	}
	return fmt.Sprintf("[@%s](mention://%s/%s) ", label, parent.AssigneeType.String, uuidToString(parent.AssigneeID))
}

func (h *Handler) resolveAssigneeMentionLabel(ctx context.Context, workspaceID pgtype.UUID, assigneeType string, assigneeID pgtype.UUID) (string, bool) {
	switch assigneeType {
	case "agent":
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          assigneeID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return "", false
		}
		return sanitizeMentionLabel(agent.Name), true
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          assigneeID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return "", false
		}
		return sanitizeMentionLabel(squad.Name), true
	}
	return "", false
}

func sanitizeMentionLabel(name string) string {
	cleaned := strings.ReplaceAll(name, "]", "")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "assignee"
	}
	return cleaned
}

func (h *Handler) dispatchParentAssigneeTrigger(ctx context.Context, parent db.Issue, systemComment db.Comment) {
	if !parent.AssigneeType.Valid || !parent.AssigneeID.Valid {
		return
	}

	switch parent.AssigneeType.String {
	case "agent":
		h.triggerChildDoneAgent(ctx, parent, systemComment.ID)
	case "squad":
		h.triggerChildDoneSquad(ctx, parent, systemComment.ID)
	}
}

func (h *Handler) triggerChildDoneAgent(ctx context.Context, parent db.Issue, triggerCommentID pgtype.UUID) {
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          parent.AssigneeID,
		WorkspaceID: parent.WorkspaceID,
	})
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: parent.ID,
		AgentID: parent.AssigneeID,

		HeadSha: h.TaskService.ResolveIssueReviewSHAParam(ctx, parent.ID),
	})
	if err != nil || hasPending {
		return
	}

	if _, err := h.TaskService.EnqueueTaskForMention(ctx, parent, parent.AssigneeID, triggerCommentID); err != nil {
		slog.Warn("child done: enqueue parent agent task failed",
			"error", err,
			"parent_id", uuidToString(parent.ID),
			"agent_id", uuidToString(parent.AssigneeID))
	}
}

func (h *Handler) triggerChildDoneSquad(ctx context.Context, parent db.Issue, triggerCommentID pgtype.UUID) {
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          parent.AssigneeID,
		WorkspaceID: parent.WorkspaceID,
	})
	if err != nil {
		return
	}

	agent, err := h.Queries.GetAgent(ctx, squad.LeaderID)
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: parent.ID,
		AgentID: squad.LeaderID,

		HeadSha: h.TaskService.ResolveIssueReviewSHAParam(ctx, parent.ID),
	})
	if err != nil || hasPending {
		return
	}

	if _, err := h.TaskService.EnqueueTaskForSquadLeader(ctx, parent, squad.LeaderID, squad.ID, triggerCommentID); err != nil {
		slog.Warn("child done: enqueue parent squad leader task failed",
			"error", err,
			"parent_id", uuidToString(parent.ID),
			"squad_id", uuidToString(squad.ID),
			"leader_id", uuidToString(squad.LeaderID))
	}
}
