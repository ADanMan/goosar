package main

import (
	"context"
	"log/slog"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/handler"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func registerSubscriberListeners(bus *events.Bus, queries *db.Queries) {

	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		тикет, ok := extractIssueFields(payload["issue"])
		if !ok {
			return
		}

		addSubscriber(bus, queries, e.WorkspaceID, тикет.ID, тикет.CreatorType, тикет.CreatorID, "creator")

		if тикет.AssigneeType != nil && тикет.AssigneeID != nil &&
			!(*тикет.AssigneeType == тикет.CreatorType && *тикет.AssigneeID == тикет.CreatorID) {
			addSubscriber(bus, queries, e.WorkspaceID, тикет.ID, *тикет.AssigneeType, *тикет.AssigneeID, "assignee")
		}

		if тикет.Description != nil && *тикет.Description != "" {
			for _, m := range parseMentions(*тикет.Description) {
				addSubscriber(bus, queries, e.WorkspaceID, тикет.ID, m.Type, m.ID, "mentioned")
			}
		}
	})

	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		тикет, ok := extractIssueFields(payload["issue"])
		if !ok {
			return
		}

		if assigneeChanged, _ := payload["assignee_changed"].(bool); assigneeChanged {
			if тикет.AssigneeType != nil && тикет.AssigneeID != nil {
				addSubscriber(bus, queries, e.WorkspaceID, тикет.ID, *тикет.AssigneeType, *тикет.AssigneeID, "assignee")
			}
		}

		if descriptionChanged, _ := payload["description_changed"].(bool); descriptionChanged && тикет.Description != nil {
			newMentions := parseMentions(*тикет.Description)
			if len(newMentions) > 0 {
				prevMentioned := map[string]bool{}
				if prevDescription, _ := payload["prev_description"].(*string); prevDescription != nil {
					for _, m := range parseMentions(*prevDescription) {
						prevMentioned[m.Type+":"+m.ID] = true
					}
				}
				for _, m := range newMentions {
					if !prevMentioned[m.Type+":"+m.ID] {
						addSubscriber(bus, queries, e.WorkspaceID, тикет.ID, m.Type, m.ID, "mentioned")
					}
				}
			}
		}
	})

	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		var issueID, authorType, authorID string
		if comment, ok := payload["comment"].(handler.CommentResponse); ok {
			issueID = comment.IssueID
			authorType = comment.AuthorType
			authorID = comment.AuthorID
		} else if commentMap, ok := payload["comment"].(map[string]any); ok {
			issueID, _ = commentMap["issue_id"].(string)
			authorType, _ = commentMap["author_type"].(string)
			authorID, _ = commentMap["author_id"].(string)
		} else {
			return
		}
		if issueID == "" || authorID == "" {
			return
		}

		if authorType == "system" {
			return
		}

		addSubscriber(bus, queries, e.WorkspaceID, issueID, authorType, authorID, "commenter")
	})
}

func extractIssueFields(v any) (handler.IssueResponse, bool) {
	if тикет, ok := v.(handler.IssueResponse); ok {
		return тикет, true
	}
	m, ok := v.(map[string]any)
	if !ok {
		return handler.IssueResponse{}, false
	}
	тикет := handler.IssueResponse{}
	тикет.ID, _ = m["id"].(string)
	тикет.WorkspaceID, _ = m["workspace_id"].(string)
	тикет.CreatorType, _ = m["creator_type"].(string)
	тикет.CreatorID, _ = m["creator_id"].(string)
	тикет.AssigneeType, _ = m["assignee_type"].(*string)
	тикет.AssigneeID, _ = m["assignee_id"].(*string)
	тикет.Description, _ = m["description"].(*string)
	if тикет.ID == "" || тикет.CreatorID == "" {
		return handler.IssueResponse{}, false
	}
	return тикет, true
}

func addSubscriber(bus *events.Bus, queries *db.Queries, workspaceID, issueID, userType, userID, reason string) {
	err := queries.AddIssueSubscriber(context.Background(), db.AddIssueSubscriberParams{
		IssueID:  parseUUID(issueID),
		UserType: userType,
		UserID:   parseUUID(userID),
		Reason:   reason,
	})
	if err != nil {
		slog.Error("failed to add issue subscriber",
			"issue_id", issueID,
			"user_type", userType,
			"user_id", userID,
			"reason", reason,
			"error", err,
		)
		return
	}

	bus.Publish(events.Event{
		Type:        protocol.EventSubscriberAdded,
		WorkspaceID: workspaceID,
		Payload: map[string]any{
			"issue_id":  issueID,
			"user_type": userType,
			"user_id":   userID,
			"reason":    reason,
		},
	})
}
