package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type invokeAuthority struct {
	UserID string

	Scoped bool
}

func scopedInvokeAuthority(userID string) invokeAuthority {
	return invokeAuthority{UserID: userID, Scoped: true}
}

func unscopedInvokeAuthority(userID string) invokeAuthority {
	return invokeAuthority{UserID: userID, Scoped: false}
}

func (h *Handler) canInvokeAgent(ctx context.Context, agent db.Agent, actorType, actorID string, authority invokeAuthority, workspaceID string) bool {
	effectiveUser := actorID

	authorityScoped := true
	if actorType != "member" {

		effectiveUser = authority.UserID
		authorityScoped = authority.Scoped
	}

	if isAgentOwner(agent, effectiveUser) {
		return true
	}

	if agent.PermissionMode != "public_to" {

		return false
	}

	allowListUser := effectiveUser
	if !authorityScoped {
		allowListUser = ""
	}

	targets, err := h.Queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}

	workspaceBroad := actorType == "agent" || actorType == "system"
	isWorkspaceMember := false
	if allowListUser != "" {
		if _, err := h.getWorkspaceMember(ctx, allowListUser, workspaceID); err == nil {
			isWorkspaceMember = true
		}
	}

	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			if isWorkspaceMember || workspaceBroad {
				return true
			}
		case "member":

			if allowListUser != "" && uuidToString(t.TargetID) == allowListUser {
				return true
			}
		case "team":

		}
	}
	return false
}

func (h *Handler) canAccessPrivateAgent(ctx context.Context, agent db.Agent, actorType, actorID, workspaceID string) bool {
	if actorType == "agent" {
		return true
	}
	if isAgentOwner(agent, actorID) {
		return true
	}
	member, err := h.getWorkspaceMember(ctx, actorID, workspaceID)
	if err != nil {
		return false
	}
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	if agent.PermissionMode != "public_to" {
		return false
	}
	targets, err := h.Queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}
	return memberHitsInvocationTargets(targets, actorID)
}

func memberHitsInvocationTargets(targets []db.AgentInvocationTarget, userID string) bool {
	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			return true
		case "member":
			if uuidToString(t.TargetID) == userID {
				return true
			}
		}
	}
	return false
}

func memberAllowedToViewAgent(agent db.Agent, targets []db.AgentInvocationTarget, userID, role string) bool {
	if roleAllowed(role, "owner", "admin") {
		return true
	}
	if isAgentOwner(agent, userID) {
		return true
	}
	if agent.PermissionMode != "public_to" {
		return false
	}
	return memberHitsInvocationTargets(targets, userID)
}

type invokeScope func(task db.AgentTaskQueue) bool

func issueInvokeScope(issueID pgtype.UUID) invokeScope {
	return func(task db.AgentTaskQueue) bool {
		return issueID.Valid && task.IssueID.Valid &&
			uuidToString(task.IssueID) == uuidToString(issueID)
	}
}

func chatSessionInvokeScope(sessionID pgtype.UUID) invokeScope {
	return func(task db.AgentTaskQueue) bool {
		return sessionID.Valid && task.ChatSessionID.Valid &&
			uuidToString(task.ChatSessionID) == uuidToString(sessionID)
	}
}

func noInvokeScope(db.AgentTaskQueue) bool { return false }

func (h *Handler) invokeAuthorityFromRequest(r *http.Request, actorType, actorID string, scope invokeScope) invokeAuthority {
	if actorType == "member" {
		return scopedInvokeAuthority(actorID)
	}
	if actorType != "agent" {
		return invokeAuthority{}
	}
	task, ok := h.taskFromRequestHeader(r, h.resolveWorkspaceID(r), actorID)
	if !ok {
		return invokeAuthority{}
	}
	return invokeAuthority{UserID: uuidToString(task.OriginatorUserID), Scoped: scope(task)}
}

func (h *Handler) autopilotDelegationAuthority(ctx context.Context, issue db.Issue, authorType, authorID string, task db.AgentTaskQueue) string {
	if authorType != "agent" {
		return ""
	}
	if !issue.OriginType.Valid || issue.OriginType.String != "autopilot" || !issue.OriginID.Valid {
		return ""
	}

	if !task.AgentID.Valid || uuidToString(task.AgentID) != authorID {
		return ""
	}
	if !task.IssueID.Valid || uuidToString(task.IssueID) != uuidToString(issue.ID) {
		return ""
	}
	ap, err := h.Queries.GetAutopilotInWorkspace(ctx, db.GetAutopilotInWorkspaceParams{
		ID:          issue.OriginID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || ap.CreatedByType != "member" || !ap.CreatedByID.Valid {
		return ""
	}
	return uuidToString(ap.CreatedByID)
}

func (h *Handler) autopilotDelegationAuthorityFromRequest(r *http.Request, issue db.Issue, actorType, actorID string) string {
	if actorType != "agent" {
		return ""
	}
	task, ok := h.taskFromRequestHeader(r, uuidToString(issue.WorkspaceID), actorID)
	if !ok {
		return ""
	}
	return h.autopilotDelegationAuthority(r.Context(), issue, actorType, actorID, task)
}

func (h *Handler) autopilotDelegationAuthorityFromComment(ctx context.Context, issue db.Issue, comment db.Comment) string {
	if comment.AuthorType != "agent" || !comment.SourceTaskID.Valid {
		return ""
	}
	task, err := h.Queries.GetAgentTask(ctx, comment.SourceTaskID)
	if err != nil {
		return ""
	}
	return h.autopilotDelegationAuthority(ctx, issue, comment.AuthorType, uuidToString(comment.AuthorID), task)
}

func (h *Handler) commentSourceTaskIDForIssue(r *http.Request, issue db.Issue, actorAgentID string) pgtype.UUID {
	task, ok := h.taskFromRequestHeader(r, uuidToString(issue.WorkspaceID), actorAgentID)
	if !ok {
		return pgtype.UUID{}
	}
	if task.IssueID.Valid && uuidToString(task.IssueID) == uuidToString(issue.ID) {
		return task.ID
	}
	if task.ChatSessionID.Valid {
		return task.ID
	}
	return pgtype.UUID{}
}

func (h *Handler) taskFromRequestHeader(r *http.Request, workspaceID, expectedAgentID string) (db.AgentTaskQueue, bool) {
	taskIDHeader := r.Header.Get("X-Task-ID")
	if taskIDHeader == "" {
		return db.AgentTaskQueue{}, false
	}
	taskUUID, err := util.ParseUUID(taskIDHeader)
	if err != nil {
		return db.AgentTaskQueue{}, false
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return db.AgentTaskQueue{}, false
	}
	task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{
		ID:          taskUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		return db.AgentTaskQueue{}, false
	}
	if expectedAgentID != "" && uuidToString(task.AgentID) != expectedAgentID {
		return db.AgentTaskQueue{}, false
	}
	return task, true
}

func (h *Handler) accessibleAgentIDs(ctx context.Context, workspaceID, actorType, actorID, role string) (map[string]struct{}, bool) {
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, false
	}
	agents, err := h.Queries.ListAllAgents(ctx, wsUUID)
	if err != nil {
		return nil, false
	}
	targetsByAgent, ok := h.loadInvocationTargetsByAgent(ctx, agents)
	if !ok {
		return nil, false
	}
	allowed := make(map[string]struct{}, len(agents))
	for _, a := range agents {
		if actorType == "member" {
			if !memberAllowedToViewAgent(a, targetsByAgent[uuidToString(a.ID)], actorID, role) {
				continue
			}
		}
		allowed[uuidToString(a.ID)] = struct{}{}
	}
	return allowed, true
}

func (h *Handler) loadInvocationTargetsByAgent(ctx context.Context, agents []db.Agent) (map[string][]db.AgentInvocationTarget, bool) {
	ids := make([]pgtype.UUID, 0, len(agents))
	for _, a := range agents {
		ids = append(ids, a.ID)
	}
	out := make(map[string][]db.AgentInvocationTarget, len(agents))
	if len(ids) == 0 {
		return out, true
	}
	rows, err := h.Queries.ListAgentInvocationTargetsByAgentIDs(ctx, ids)
	if err != nil {
		return nil, false
	}
	for _, row := range rows {
		aid := uuidToString(row.AgentID)
		out[aid] = append(out[aid], row)
	}
	return out, true
}

func (h *Handler) canEnqueueSquadLeader(ctx context.Context, leaderID pgtype.UUID, actorType, actorID string, authority invokeAuthority, workspaceID string) bool {
	agent, err := h.Queries.GetAgent(ctx, leaderID)
	if err != nil {
		return false
	}
	return h.canInvokeAgent(ctx, agent, actorType, actorID, authority, workspaceID)
}
