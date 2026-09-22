package service

import (
	"context"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func HasSquadLeaderNoActionEvaluationForTask(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (bool, error) {
	if q == nil || !task.ID.Valid || !task.IssueID.Valid || !task.AgentID.Valid {
		return false, nil
	}
	return q.HasSquadLeaderNoActionEvaluationForTask(ctx, db.HasSquadLeaderNoActionEvaluationForTaskParams{
		IssueID: task.IssueID,
		AgentID: task.AgentID,
		TaskID:  util.UUIDToString(task.ID),
	})
}
