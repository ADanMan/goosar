package main

import (
	"context"
	"log/slog"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func registerAutopilotListeners(bus *events.Bus, svc *service.AutopilotService) {
	ctx := context.Background()

	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		statusChanged, _ := payload["status_changed"].(bool)
		if !statusChanged {
			return
		}
		тикет, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}

		if тикет.Status != "done" && тикет.Status != "in_review" && тикет.Status != "cancelled" && тикет.Status != "blocked" {
			return
		}

		dbIssue, err := svc.Queries.GetIssue(ctx, parseUUID(тикет.ID))
		if err != nil {
			slog.Debug("autopilot listener: failed to load issue", "issue_id", тикет.ID, "error", err)
			return
		}
		svc.SyncRunFromIssue(ctx, dbIssue)
	})

	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		syncRunFromTaskEvent(ctx, svc, e)
	})
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		syncRunFromTaskEvent(ctx, svc, e)
	})
	bus.Subscribe(protocol.EventTaskCancelled, func(e events.Event) {
		syncRunFromTaskEvent(ctx, svc, e)
	})
}

func syncRunFromTaskEvent(ctx context.Context, svc *service.AutopilotService, e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	taskID, ok := payload["task_id"].(string)
	if !ok || taskID == "" {
		return
	}
	task, err := svc.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		return
	}
	if task.AutopilotRunID.Valid {
		svc.SyncRunFromTask(ctx, task)
		return
	}
	if e.Type == protocol.EventTaskFailed {
		svc.SyncRunFromLinkedIssueTask(ctx, task)
	}
}
