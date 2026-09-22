package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type RunEnqueueSource string

const (
	RunSourceAssign RunEnqueueSource = "assign"

	RunSourceStatus RunEnqueueSource = "status"
)

type IssueTriggerProbe struct {
	CanAccessAgent func(agent db.Agent) bool
	IsSelfLoop     func() bool
}

type IssueTriggerInput struct {
	Issue           db.Issue
	PrevStatus      string
	IsCreate        bool
	AssigneeChanged bool
	StatusChanged   bool
}

type IssueRunTrigger struct {
	IssueID      pgtype.UUID
	AgentID      pgtype.UUID
	AssigneeType string
	Source       RunEnqueueSource
}

func allowAllAgents(db.Agent) bool { return true }

func (s *IssueService) WillEnqueueRun(ctx context.Context, in IssueTriggerInput, probe IssueTriggerProbe) (IssueRunTrigger, bool) {
	issue := in.Issue
	if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return IssueRunTrigger{}, false
	}
	canAccess := probe.CanAccessAgent
	if canAccess == nil {
		canAccess = allowAllAgents
	}

	var source RunEnqueueSource
	switch {
	case in.IsCreate || in.AssigneeChanged:

		if issue.Status == "backlog" {
			return IssueRunTrigger{}, false
		}
		source = RunSourceAssign
	case in.StatusChanged && in.PrevStatus == "backlog" &&
		issue.Status != "done" && issue.Status != "cancelled":
		if probe.IsSelfLoop != nil && probe.IsSelfLoop() {
			return IssueRunTrigger{}, false
		}
		source = RunSourceStatus
	default:
		return IssueRunTrigger{}, false
	}

	switch issue.AssigneeType.String {
	case "agent":
		agent, err := s.Queries.GetAgent(ctx, issue.AssigneeID)
		if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
			return IssueRunTrigger{}, false
		}
		if !canAccess(agent) {
			return IssueRunTrigger{}, false
		}
		if source == RunSourceStatus && s.hasPendingRun(ctx, issue.ID, issue.AssigneeID) {
			return IssueRunTrigger{}, false
		}
		return IssueRunTrigger{
			IssueID:      issue.ID,
			AgentID:      issue.AssigneeID,
			AssigneeType: "agent",
			Source:       source,
		}, true

	case "squad":
		squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          issue.AssigneeID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return IssueRunTrigger{}, false
		}
		leader, err := s.Queries.GetAgent(ctx, squad.LeaderID)
		if err != nil {
			return IssueRunTrigger{}, false
		}
		ready, _, err := AgentReadiness(ctx, s.Queries, leader)
		if err != nil || !ready {
			return IssueRunTrigger{}, false
		}
		if !canAccess(leader) {
			return IssueRunTrigger{}, false
		}
		if source == RunSourceStatus && s.hasPendingRun(ctx, issue.ID, squad.LeaderID) {
			return IssueRunTrigger{}, false
		}
		return IssueRunTrigger{
			IssueID:      issue.ID,
			AgentID:      squad.LeaderID,
			AssigneeType: "squad",
			Source:       source,
		}, true
	}
	return IssueRunTrigger{}, false
}

func (s *IssueService) hasPendingRun(ctx context.Context, issueID, agentID pgtype.UUID) bool {
	pending, err := s.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: agentID,

		HeadSha: headShaText(s.TaskService.ResolveIssueReviewSHA(ctx, issueID)),
	})
	if err != nil {
		return true
	}
	return pending
}
