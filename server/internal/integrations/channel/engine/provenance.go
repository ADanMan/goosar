package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type ChannelProvenanceQueries interface {
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
}

func TaskInputIsChannelIngested(ctx context.Context, q ChannelProvenanceQueries, task db.AgentTaskQueue) (bool, error) {
	if !task.ChatInputTaskID.Valid {
		return true, nil
	}
	return q.TaskHasChannelIngestedMessages(ctx, task.ChatInputTaskID)
}
