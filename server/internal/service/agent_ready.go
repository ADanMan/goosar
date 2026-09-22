package service

import (
	"context"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func AgentReadiness(ctx context.Context, q *db.Queries, agent db.Agent) (ready bool, reason string, err error) {
	if agent.ArchivedAt.Valid {
		return false, "agent is archived", nil
	}
	if !agent.RuntimeID.Valid {
		return false, "agent has no runtime bound", nil
	}
	rt, err := q.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return false, "", err
	}
	if rt.Status != "online" {
		return false, "agent runtime is " + rt.Status, nil
	}
	return true, "", nil
}
