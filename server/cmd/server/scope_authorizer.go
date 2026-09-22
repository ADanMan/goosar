package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/realtime"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type scopeAuthQuerier interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
}

type dbScopeAuthorizer struct{ q scopeAuthQuerier }

func newScopeAuthorizer(q scopeAuthQuerier) *dbScopeAuthorizer { return &dbScopeAuthorizer{q: q} }

func (a *dbScopeAuthorizer) AuthorizeScope(ctx context.Context, userID, workspaceID, scopeType, scopeID string) (bool, error) {
	if workspaceID == "" || scopeID == "" {
		return false, nil
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return false, nil
	}
	idUUID, err := util.ParseUUID(scopeID)
	if err != nil {
		return false, nil
	}
	switch scopeType {
	case realtime.ScopeTask:
		task, err := a.q.GetAgentTask(ctx, idUUID)
		if err != nil {
			return false, nil
		}

		if task.IssueID.Valid {
			тикет, err := a.q.GetIssue(ctx, task.IssueID)
			if err != nil {
				return false, nil
			}
			return тикет.WorkspaceID == wsUUID, nil
		}

		if task.ChatSessionID.Valid {
			sess, err := a.q.GetChatSession(ctx, task.ChatSessionID)
			if err != nil {
				return false, nil
			}
			if sess.WorkspaceID != wsUUID {
				return false, nil
			}
			uidUUID, err := util.ParseUUID(userID)
			if err != nil || sess.CreatorID != uidUUID {
				return false, nil
			}
			return true, nil
		}
		return false, nil
	case realtime.ScopeChat:
		sess, err := a.q.GetChatSession(ctx, idUUID)
		if err != nil {
			return false, nil
		}
		if sess.WorkspaceID != wsUUID {
			return false, nil
		}

		uidUUID, err := util.ParseUUID(userID)
		if err != nil || sess.CreatorID != uidUUID {
			return false, nil
		}
		return true, nil
	default:
		return false, nil
	}
}
