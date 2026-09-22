package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func registerActivityListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()

	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		тикет, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}

		activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
			WorkspaceID: parseUUID(тикет.WorkspaceID),
			IssueID:     parseUUID(тикет.ID),
			ActorType:   util.StrToText(e.ActorType),
			ActorID:     optionalUUID(e.ActorID),
			Action:      "created",
			Details:     []byte("{}"),
		})
		if err != nil {
			slog.Error("activity: failed to record issue created",
				"issue_id", тикет.ID, "error", err)
			return
		}

		publishActivityEvent(bus, e, activity)
	})

	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		тикет, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}

		statusChanged, _ := payload["status_changed"].(bool)
		priorityChanged, _ := payload["priority_changed"].(bool)
		assigneeChanged, _ := payload["assignee_changed"].(bool)
		descriptionChanged, _ := payload["description_changed"].(bool)

		if statusChanged {
			prevStatus, _ := payload["prev_status"].(string)
			details, _ := json.Marshal(map[string]string{
				"from": prevStatus,
				"to":   тикет.Status,
			})
			activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
				WorkspaceID: parseUUID(тикет.WorkspaceID),
				IssueID:     parseUUID(тикет.ID),
				ActorType:   util.StrToText(e.ActorType),
				ActorID:     optionalUUID(e.ActorID),
				Action:      "status_changed",
				Details:     details,
			})
			if err != nil {
				slog.Error("activity: failed to record status change",
					"issue_id", тикет.ID, "error", err)
			} else {
				publishActivityEvent(bus, e, activity)
			}
		}

		if priorityChanged {
			prevPriority, _ := payload["prev_priority"].(string)
			details, _ := json.Marshal(map[string]string{
				"from": prevPriority,
				"to":   тикет.Priority,
			})
			activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
				WorkspaceID: parseUUID(тикет.WorkspaceID),
				IssueID:     parseUUID(тикет.ID),
				ActorType:   util.StrToText(e.ActorType),
				ActorID:     optionalUUID(e.ActorID),
				Action:      "priority_changed",
				Details:     details,
			})
			if err != nil {
				slog.Error("activity: failed to record priority change",
					"issue_id", тикет.ID, "error", err)
			} else {
				publishActivityEvent(bus, e, activity)
			}
		}

		if assigneeChanged {
			prevAssigneeType, _ := payload["prev_assignee_type"].(*string)
			prevAssigneeID, _ := payload["prev_assignee_id"].(*string)

			detailsMap := map[string]string{}
			if prevAssigneeType != nil {
				detailsMap["from_type"] = *prevAssigneeType
			}
			if prevAssigneeID != nil {
				detailsMap["from_id"] = *prevAssigneeID
			}
			if тикет.AssigneeType != nil {
				detailsMap["to_type"] = *тикет.AssigneeType
			}
			if тикет.AssigneeID != nil {
				detailsMap["to_id"] = *тикет.AssigneeID
			}

			details, _ := json.Marshal(detailsMap)
			activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
				WorkspaceID: parseUUID(тикет.WorkspaceID),
				IssueID:     parseUUID(тикет.ID),
				ActorType:   util.StrToText(e.ActorType),
				ActorID:     optionalUUID(e.ActorID),
				Action:      "assignee_changed",
				Details:     details,
			})
			if err != nil {
				slog.Error("activity: failed to record assignee change",
					"issue_id", тикет.ID, "error", err)
			} else {
				publishActivityEvent(bus, e, activity)
			}
		}

		if startDateChanged, _ := payload["start_date_changed"].(bool); startDateChanged {
			prevStartDate := ""
			if v, ok := payload["prev_start_date"].(*string); ok && v != nil {
				prevStartDate = *v
			}
			newStartDate := ""
			if тикет.StartDate != nil {
				newStartDate = *тикет.StartDate
			}
			details, _ := json.Marshal(map[string]string{
				"from": prevStartDate,
				"to":   newStartDate,
			})
			activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
				WorkspaceID: parseUUID(тикет.WorkspaceID),
				IssueID:     parseUUID(тикет.ID),
				ActorType:   util.StrToText(e.ActorType),
				ActorID:     optionalUUID(e.ActorID),
				Action:      "start_date_changed",
				Details:     details,
			})
			if err != nil {
				slog.Error("activity: failed to record start date change",
					"issue_id", тикет.ID, "error", err)
			} else {
				publishActivityEvent(bus, e, activity)
			}
		}

		if dueDateChanged, _ := payload["due_date_changed"].(bool); dueDateChanged {
			prevDueDate := ""
			if v, ok := payload["prev_due_date"].(*string); ok && v != nil {
				prevDueDate = *v
			}
			newDueDate := ""
			if тикет.DueDate != nil {
				newDueDate = *тикет.DueDate
			}
			details, _ := json.Marshal(map[string]string{
				"from": prevDueDate,
				"to":   newDueDate,
			})
			activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
				WorkspaceID: parseUUID(тикет.WorkspaceID),
				IssueID:     parseUUID(тикет.ID),
				ActorType:   util.StrToText(e.ActorType),
				ActorID:     optionalUUID(e.ActorID),
				Action:      "due_date_changed",
				Details:     details,
			})
			if err != nil {
				slog.Error("activity: failed to record due date change",
					"issue_id", тикет.ID, "error", err)
			} else {
				publishActivityEvent(bus, e, activity)
			}
		}

		if titleChanged, _ := payload["title_changed"].(bool); titleChanged {
			prevTitle, _ := payload["prev_title"].(string)
			details, _ := json.Marshal(map[string]string{
				"from": prevTitle,
				"to":   тикет.Title,
			})
			activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
				WorkspaceID: parseUUID(тикет.WorkspaceID),
				IssueID:     parseUUID(тикет.ID),
				ActorType:   util.StrToText(e.ActorType),
				ActorID:     optionalUUID(e.ActorID),
				Action:      "title_changed",
				Details:     details,
			})
			if err != nil {
				slog.Error("activity: failed to record title change",
					"issue_id", тикет.ID, "error", err)
			} else {
				publishActivityEvent(bus, e, activity)
			}
		}

		if descriptionChanged {
			activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
				WorkspaceID: parseUUID(тикет.WorkspaceID),
				IssueID:     parseUUID(тикет.ID),
				ActorType:   util.StrToText(e.ActorType),
				ActorID:     optionalUUID(e.ActorID),
				Action:      "description_updated",
				Details:     []byte("{}"),
			})
			if err != nil {
				slog.Error("activity: failed to record description change",
					"issue_id", тикет.ID, "error", err)
			} else {
				publishActivityEvent(bus, e, activity)
			}
		}
	})

	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		handleTaskActivity(ctx, bus, queries, e, "task_completed")
	})

	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		handleTaskActivity(ctx, bus, queries, e, "task_failed")
	})
}

func handleTaskActivity(ctx context.Context, bus *events.Bus, queries *db.Queries, e events.Event, action string) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	agentID, _ := payload["agent_id"].(string)
	issueID, _ := payload["issue_id"].(string)
	if issueID == "" {
		return
	}

	тикет, err := queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		slog.Error("activity: failed to get issue for task event",
			"issue_id", issueID, "action", action, "error", err)
		return
	}

	activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: тикет.WorkspaceID,
		IssueID:     parseUUID(issueID),
		ActorType:   util.StrToText("agent"),
		ActorID:     parseUUID(agentID),
		Action:      action,
		Details:     []byte("{}"),
	})
	if err != nil {
		slog.Error("activity: failed to record task activity",
			"issue_id", issueID, "action", action, "error", err)
		return
	}

	publishActivityEvent(bus, e, activity)
}

func publishActivityEvent(bus *events.Bus, original events.Event, activity db.ActivityLog) {
	actorType := ""
	if activity.ActorType.Valid {
		actorType = activity.ActorType.String
	}
	action := activity.Action
	bus.Publish(events.Event{
		Type:        protocol.EventActivityCreated,
		WorkspaceID: original.WorkspaceID,
		ActorType:   original.ActorType,
		ActorID:     original.ActorID,
		Payload: map[string]any{
			"issue_id": util.UUIDToString(activity.IssueID),
			"entry": map[string]any{
				"type":       "activity",
				"id":         util.UUIDToString(activity.ID),
				"actor_type": actorType,
				"actor_id":   util.UUIDToString(activity.ActorID),
				"action":     action,
				"details":    json.RawMessage(activity.Details),
				"created_at": util.TimestampToString(activity.CreatedAt),
			},
		},
	})
}
