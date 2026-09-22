package main

import (
	"context"
	"testing"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func inboxItemsForRecipient(t *testing.T, queries *db.Queries, recipientID string) []db.ListInboxItemsRow {
	t.Helper()
	items, err := queries.ListInboxItems(context.Background(), db.ListInboxItemsParams{
		WorkspaceID:   util.MustParseUUID(testWorkspaceID),
		RecipientType: "member",
		RecipientID:   util.MustParseUUID(recipientID),
	})
	if err != nil {
		t.Fatalf("ListInboxItems: %v", err)
	}
	return items
}

func cleanupInboxForIssue(t *testing.T, issueID string) {
	t.Helper()
	testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
}

func addTestSubscriber(t *testing.T, issueID, userType, userID, reason string) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (issue_id, user_type, user_id) DO NOTHING
	`, issueID, userType, userID, reason)
	if err != nil {
		t.Fatalf("addTestSubscriber: %v", err)
	}
}

func createTestSubIssue(t *testing.T, workspaceID, creatorID, parentIssueID string) string {
	t.Helper()
	ctx := context.Background()
	var issueID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, parent_issue_id, number)
		VALUES ($1, 'sub-issue test', 'todo', 'medium', 'member', $2, 0, $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, workspaceID, creatorID, parentIssueID).Scan(&issueID)
	if err != nil {
		t.Fatalf("createTestSubIssue: %v", err)
	}
	return issueID
}

func newNotificationBus(t *testing.T, queries *db.Queries) *events.Bus {
	t.Helper()
	bus := events.New()
	registerSubscriberListeners(bus, queries)
	registerNotificationListeners(bus, queries)
	return bus
}

func TestNotification_IssueCreated_AssigneeNotified(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	assigneeEmail := "notif-assignee-created@goosar.ru"
	assigneeID := createTestUser(t, assigneeEmail)
	t.Cleanup(func() { cleanupTestUser(t, assigneeEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})

	assigneeType := "member"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueID,
				WorkspaceID:  testWorkspaceID,
				Title:        "notif test issue",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				AssigneeType: &assigneeType,
				AssigneeID:   &assigneeID,
			},
		},
	})

	items := inboxItemsForRecipient(t, queries, assigneeID)
	if len(items) != 1 {
		t.Fatalf("expected 1 inbox item for assignee, got %d", len(items))
	}
	if items[0].Type != "issue_assigned" {
		t.Fatalf("expected type 'issue_assigned', got %q", items[0].Type)
	}
	if items[0].Severity != "action_required" {
		t.Fatalf("expected severity 'action_required', got %q", items[0].Severity)
	}

	creatorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(creatorItems) != 0 {
		t.Fatalf("expected 0 inbox items for creator, got %d", len(creatorItems))
	}

	if len(inboxEvents) < 1 {
		t.Fatal("expected at least 1 inbox:new event")
	}
}

func TestNotification_IssueCreated_SelfAssign(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})

	assigneeType := "member"
	assigneeID := testUserID
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueID,
				WorkspaceID:  testWorkspaceID,
				Title:        "self-assign issue",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				AssigneeType: &assigneeType,
				AssigneeID:   &assigneeID,
			},
		},
	})

	items := inboxItemsForRecipient(t, queries, testUserID)
	if len(items) != 0 {
		t.Fatalf("expected 0 inbox items for self-assign, got %d", len(items))
	}
	if len(inboxEvents) != 0 {
		t.Fatalf("expected 0 inbox:new events for self-assign, got %d", len(inboxEvents))
	}
}

func TestNotification_IssueCreated_NoAssignee(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})

	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "no assignee issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
		},
	})

	items := inboxItemsForRecipient(t, queries, testUserID)
	if len(items) != 0 {
		t.Fatalf("expected 0 inbox items for no-assignee issue, got %d", len(items))
	}
	if len(inboxEvents) != 0 {
		t.Fatalf("expected 0 inbox:new events, got %d", len(inboxEvents))
	}
}

func TestNotification_StatusChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	sub1Email := "notif-sub1-status@goosar.ru"
	sub1ID := createTestUser(t, sub1Email)
	t.Cleanup(func() { cleanupTestUser(t, sub1Email) })

	sub2Email := "notif-sub2-status@goosar.ru"
	sub2ID := createTestUser(t, sub2Email)
	t.Cleanup(func() { cleanupTestUser(t, sub2Email) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", sub1ID, "assignee")
	addTestSubscriber(t, issueID, "member", sub2ID, "commenter")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "status test issue",
				Status:      "in_progress",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   true,
			"prev_status":      "todo",
		},
	})

	actorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(actorItems) != 0 {
		t.Fatalf("expected 0 inbox items for actor, got %d", len(actorItems))
	}

	sub1Items := inboxItemsForRecipient(t, queries, sub1ID)
	if len(sub1Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub1, got %d", len(sub1Items))
	}
	if sub1Items[0].Type != "status_changed" {
		t.Fatalf("expected type 'status_changed', got %q", sub1Items[0].Type)
	}
	if sub1Items[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", sub1Items[0].Severity)
	}

	expectedTitle := "status test issue"
	if sub1Items[0].Title != expectedTitle {
		t.Fatalf("expected title %q, got %q", expectedTitle, sub1Items[0].Title)
	}

	sub2Items := inboxItemsForRecipient(t, queries, sub2ID)
	if len(sub2Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub2, got %d", len(sub2Items))
	}
	if sub2Items[0].Type != "status_changed" {
		t.Fatalf("expected type 'status_changed', got %q", sub2Items[0].Type)
	}
}

func TestNotification_StatusChanged_Muted(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	mutedEmail := "notif-muted-status@goosar.ru"
	mutedID := createTestUser(t, mutedEmail)
	t.Cleanup(func() { cleanupTestUser(t, mutedEmail) })

	normalEmail := "notif-normal-status@goosar.ru"
	normalID := createTestUser(t, normalEmail)
	t.Cleanup(func() { cleanupTestUser(t, normalEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO notification_preference (workspace_id, user_id, preferences)
		VALUES ($1, $2, '{"status_changes":"muted"}'::jsonb)
	`, testWorkspaceID, mutedID); err != nil {
		t.Fatalf("seed muted notification preference: %v", err)
	}

	addTestSubscriber(t, issueID, "member", mutedID, "commenter")
	addTestSubscriber(t, issueID, "member", normalID, "commenter")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "muted status test issue",
				Status:      "in_progress",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   true,
			"prev_status":      "todo",
		},
	})

	if items := inboxItemsForRecipient(t, queries, mutedID); len(items) != 0 {
		t.Fatalf("expected muted subscriber to receive 0 items, got %d", len(items))
	}
	if items := inboxItemsForRecipient(t, queries, normalID); len(items) != 1 {
		t.Fatalf("expected normal subscriber to receive 1 item, got %d", len(items))
	}
}

func TestNotification_CommentCreated(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	commenterEmail := "notif-commenter@goosar.ru"
	commenterID := createTestUser(t, commenterEmail)
	t.Cleanup(func() { cleanupTestUser(t, commenterEmail) })

	sub1Email := "notif-sub1-comment@goosar.ru"
	sub1ID := createTestUser(t, sub1Email)
	t.Cleanup(func() { cleanupTestUser(t, sub1Email) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", sub1ID, "assignee")

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     commenterID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   commenterID,
				Content:    "test comment content",
				Type:       "comment",
			},
			"issue_title":  "comment test issue",
			"issue_status": "todo",
		},
	})

	creatorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(creatorItems) != 1 {
		t.Fatalf("expected 1 inbox item for creator, got %d", len(creatorItems))
	}
	if creatorItems[0].Type != "new_comment" {
		t.Fatalf("expected type 'new_comment', got %q", creatorItems[0].Type)
	}
	if creatorItems[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", creatorItems[0].Severity)
	}

	sub1Items := inboxItemsForRecipient(t, queries, sub1ID)
	if len(sub1Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub1, got %d", len(sub1Items))
	}
	if sub1Items[0].Type != "new_comment" {
		t.Fatalf("expected type 'new_comment', got %q", sub1Items[0].Type)
	}

	commenterItems := inboxItemsForRecipient(t, queries, commenterID)
	if len(commenterItems) != 0 {
		t.Fatalf("expected 0 inbox items for commenter, got %d", len(commenterItems))
	}
}

func TestNotification_SystemCommentSkipsInboxAndMentions(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	subEmail := "notif-system-comment-sub@goosar.ru"
	subID := createTestUser(t, subEmail)
	t.Cleanup(func() { cleanupTestUser(t, subEmail) })

	targetEmail := "notif-system-comment-target@goosar.ru"
	targetID := createTestUser(t, targetEmail)
	t.Cleanup(func() { cleanupTestUser(t, targetEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", subID, "manual")

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    issueID,
				AuthorType: "system",
				AuthorID:   "00000000-0000-0000-0000-000000000000",
				Content:    "Sub-issue done — see [@Target](mention://member/" + targetID + ").",
				Type:       "system",
			},
			"issue_title":  "system comment isolation",
			"issue_status": "in_progress",
		},
	})

	if items := inboxItemsForRecipient(t, queries, subID); len(items) != 0 {
		t.Errorf("expected 0 inbox rows for issue subscriber, got %d", len(items))
	}
	if items := inboxItemsForRecipient(t, queries, targetID); len(items) != 0 {
		t.Errorf("expected 0 inbox rows for smuggled @mention target, got %d", len(items))
	}
}

func TestSubscriberSystemCommentDoesNotSubscribe(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    issueID,
				AuthorType: "system",
				AuthorID:   "00000000-0000-0000-0000-000000000000",
				Content:    "platform notify",
				Type:       "system",
			},
		},
	})

	if count := subscriberCount(t, queries, issueID); count != 0 {
		t.Fatalf("expected 0 subscribers after system comment, got %d", count)
	}
}

func TestNotification_AssigneeChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	oldAssigneeEmail := "notif-old-assignee@goosar.ru"
	oldAssigneeID := createTestUser(t, oldAssigneeEmail)
	t.Cleanup(func() { cleanupTestUser(t, oldAssigneeEmail) })

	newAssigneeEmail := "notif-new-assignee@goosar.ru"
	newAssigneeID := createTestUser(t, newAssigneeEmail)
	t.Cleanup(func() { cleanupTestUser(t, newAssigneeEmail) })

	bystanderEmail := "notif-bystander@goosar.ru"
	bystanderID := createTestUser(t, bystanderEmail)
	t.Cleanup(func() { cleanupTestUser(t, bystanderEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", oldAssigneeID, "assignee")
	addTestSubscriber(t, issueID, "member", bystanderID, "commenter")

	newAssigneeType := "member"
	oldAssigneeType := "member"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueID,
				WorkspaceID:  testWorkspaceID,
				Title:        "assignee change issue",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				AssigneeType: &newAssigneeType,
				AssigneeID:   &newAssigneeID,
			},
			"assignee_changed":   true,
			"status_changed":     false,
			"prev_assignee_type": &oldAssigneeType,
			"prev_assignee_id":   &oldAssigneeID,
		},
	})

	newItems := inboxItemsForRecipient(t, queries, newAssigneeID)
	if len(newItems) != 1 {
		t.Fatalf("expected 1 inbox item for new assignee, got %d", len(newItems))
	}
	if newItems[0].Type != "issue_assigned" {
		t.Fatalf("expected type 'issue_assigned', got %q", newItems[0].Type)
	}
	if newItems[0].Severity != "action_required" {
		t.Fatalf("expected severity 'action_required', got %q", newItems[0].Severity)
	}

	oldItems := inboxItemsForRecipient(t, queries, oldAssigneeID)
	if len(oldItems) != 1 {
		t.Fatalf("expected 1 inbox item for old assignee, got %d", len(oldItems))
	}
	if oldItems[0].Type != "unassigned" {
		t.Fatalf("expected type 'unassigned', got %q", oldItems[0].Type)
	}
	if oldItems[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", oldItems[0].Severity)
	}

	bystanderItems := inboxItemsForRecipient(t, queries, bystanderID)
	if len(bystanderItems) != 1 {
		t.Fatalf("expected 1 inbox item for bystander, got %d", len(bystanderItems))
	}
	if bystanderItems[0].Type != "assignee_changed" {
		t.Fatalf("expected type 'assignee_changed', got %q", bystanderItems[0].Type)
	}
	if bystanderItems[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", bystanderItems[0].Severity)
	}

	actorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(actorItems) != 0 {
		t.Fatalf("expected 0 inbox items for actor, got %d", len(actorItems))
	}
}

func TestNotification_TaskCompleted(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	agentID := "00000000-0000-0000-0000-aaaaaaaaaaaa"

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "agent", agentID, "assignee")

	bus.Publish(events.Event{
		Type:        protocol.EventTaskCompleted,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-bbbbbbbbbbbb",
			"agent_id": agentID,
			"issue_id": issueID,
			"status":   "completed",
		},
	})

	creatorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(creatorItems) != 0 {
		t.Fatalf("expected 0 inbox items for creator on task:completed, got %d", len(creatorItems))
	}
}

func TestNotification_TaskFailed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	agentID := "00000000-0000-0000-0000-aaaaaaaaaaaa"

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "agent", agentID, "assignee")

	bus.Publish(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-bbbbbbbbbbbb",
			"agent_id": agentID,
			"issue_id": issueID,
			"status":   "failed",
		},
	})

	creatorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(creatorItems) != 1 {
		t.Fatalf("expected 1 inbox item for creator, got %d", len(creatorItems))
	}
	if creatorItems[0].Type != "task_failed" {
		t.Fatalf("expected type 'task_failed', got %q", creatorItems[0].Type)
	}
	if creatorItems[0].Severity != "action_required" {
		t.Fatalf("expected severity 'action_required', got %q", creatorItems[0].Severity)
	}
}

func TestNotification_PriorityChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	sub1Email := "notif-sub1-priority@goosar.ru"
	sub1ID := createTestUser(t, sub1Email)
	t.Cleanup(func() { cleanupTestUser(t, sub1Email) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", sub1ID, "assignee")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "priority test issue",
				Status:      "todo",
				Priority:    "high",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   false,
			"priority_changed": true,
			"prev_priority":    "medium",
		},
	})

	actorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(actorItems) != 0 {
		t.Fatalf("expected 0 inbox items for actor, got %d", len(actorItems))
	}

	sub1Items := inboxItemsForRecipient(t, queries, sub1ID)
	if len(sub1Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub1, got %d", len(sub1Items))
	}
	if sub1Items[0].Type != "priority_changed" {
		t.Fatalf("expected type 'priority_changed', got %q", sub1Items[0].Type)
	}
	if sub1Items[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", sub1Items[0].Severity)
	}

	expectedTitle := "priority test issue"
	if sub1Items[0].Title != expectedTitle {
		t.Fatalf("expected title %q, got %q", expectedTitle, sub1Items[0].Title)
	}
}

func TestNotification_DueDateChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	sub1Email := "notif-sub1-duedate@goosar.ru"
	sub1ID := createTestUser(t, sub1Email)
	t.Cleanup(func() { cleanupTestUser(t, sub1Email) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", sub1ID, "assignee")

	dueDate := "2026-04-15"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "due date test issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
				DueDate:     &dueDate,
			},
			"assignee_changed": false,
			"status_changed":   false,
			"due_date_changed": true,
		},
	})

	actorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(actorItems) != 0 {
		t.Fatalf("expected 0 inbox items for actor, got %d", len(actorItems))
	}

	sub1Items := inboxItemsForRecipient(t, queries, sub1ID)
	if len(sub1Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub1, got %d", len(sub1Items))
	}
	if sub1Items[0].Type != "due_date_changed" {
		t.Fatalf("expected type 'due_date_changed', got %q", sub1Items[0].Type)
	}
	if sub1Items[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", sub1Items[0].Severity)
	}
}

func TestNotification_StartDateChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	sub1Email := "notif-sub1-startdate@goosar.ru"
	sub1ID := createTestUser(t, sub1Email)
	t.Cleanup(func() { cleanupTestUser(t, sub1Email) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", sub1ID, "assignee")

	startDate := "2026-04-01"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "start date test issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
				StartDate:   &startDate,
			},
			"assignee_changed":   false,
			"status_changed":     false,
			"start_date_changed": true,
		},
	})

	actorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(actorItems) != 0 {
		t.Fatalf("expected 0 inbox items for actor, got %d", len(actorItems))
	}

	sub1Items := inboxItemsForRecipient(t, queries, sub1ID)
	if len(sub1Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub1, got %d", len(sub1Items))
	}
	if sub1Items[0].Type != "start_date_changed" {
		t.Fatalf("expected type 'start_date_changed', got %q", sub1Items[0].Type)
	}
	if sub1Items[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", sub1Items[0].Severity)
	}
}

func TestNotification_ParentBubble_StatusChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	parentSubEmail := "notif-parent-sub-status@goosar.ru"
	parentSubID := createTestUser(t, parentSubEmail)
	t.Cleanup(func() { cleanupTestUser(t, parentSubEmail) })

	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, parentID)
		cleanupTestIssue(t, parentID)
	})
	subID := createTestSubIssue(t, testWorkspaceID, testUserID, parentID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, subID)
		cleanupTestIssue(t, subID)
	})

	addTestSubscriber(t, parentID, "member", parentSubID, "manual")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          subID,
				WorkspaceID: testWorkspaceID,
				Title:       "sub-issue status bubble",
				Status:      "done",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   true,
			"prev_status":      "in_progress",
		},
	})

	items := inboxItemsForRecipient(t, queries, parentSubID)
	if len(items) != 1 {
		t.Fatalf("expected 1 inbox item bubbled to parent subscriber, got %d", len(items))
	}
	if items[0].Type != "status_changed" {
		t.Fatalf("expected type 'status_changed', got %q", items[0].Type)
	}

	if util.UUIDToString(items[0].IssueID) != subID {
		t.Fatalf("expected inbox item issue_id=%s (sub-issue), got %s",
			subID, util.UUIDToString(items[0].IssueID))
	}
}

func TestNotification_ParentBubble_NewCommentSuppressed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	commenterEmail := "notif-parent-bubble-commenter@goosar.ru"
	commenterID := createTestUser(t, commenterEmail)
	t.Cleanup(func() { cleanupTestUser(t, commenterEmail) })

	parentSubEmail := "notif-parent-sub-comment@goosar.ru"
	parentSubID := createTestUser(t, parentSubEmail)
	t.Cleanup(func() { cleanupTestUser(t, parentSubEmail) })

	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, parentID)
		cleanupTestIssue(t, parentID)
	})
	subID := createTestSubIssue(t, testWorkspaceID, testUserID, parentID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, subID)
		cleanupTestIssue(t, subID)
	})

	addTestSubscriber(t, parentID, "member", parentSubID, "manual")

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     commenterID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    subID,
				AuthorType: "member",
				AuthorID:   commenterID,
				Content:    "comment on sub-issue",
				Type:       "comment",
			},
			"issue_title":  "sub-issue comment bubble",
			"issue_status": "todo",
		},
	})

	items := inboxItemsForRecipient(t, queries, parentSubID)
	if len(items) != 0 {
		t.Fatalf("expected 0 inbox items bubbled to parent subscriber for new_comment, got %d", len(items))
	}
}

func TestNotification_ParentBubble_PriorityChangeSuppressed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	parentSubEmail := "notif-parent-sub-priority@goosar.ru"
	parentSubID := createTestUser(t, parentSubEmail)
	t.Cleanup(func() { cleanupTestUser(t, parentSubEmail) })

	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, parentID)
		cleanupTestIssue(t, parentID)
	})
	subID := createTestSubIssue(t, testWorkspaceID, testUserID, parentID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, subID)
		cleanupTestIssue(t, subID)
	})

	addTestSubscriber(t, parentID, "member", parentSubID, "manual")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          subID,
				WorkspaceID: testWorkspaceID,
				Title:       "sub-issue priority bubble",
				Status:      "todo",
				Priority:    "high",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   false,
			"priority_changed": true,
			"prev_priority":    "medium",
		},
	})

	items := inboxItemsForRecipient(t, queries, parentSubID)
	if len(items) != 0 {
		t.Fatalf("expected 0 inbox items bubbled to parent subscriber for priority_changed, got %d", len(items))
	}
}

func countInboxByTypeForRecipient(t *testing.T, recipientID, notifType string) (active, archived int) {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT archived FROM inbox_item
		WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND type = $3
	`, testWorkspaceID, recipientID, notifType)
	if err != nil {
		t.Fatalf("countInboxByTypeForRecipient: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var isArchived bool
		if err := rows.Scan(&isArchived); err != nil {
			t.Fatalf("countInboxByTypeForRecipient scan: %v", err)
		}
		if isArchived {
			archived++
		} else {
			active++
		}
	}
	return active, archived
}

func publishStatusChange(bus *events.Bus, issueID, newStatus, prevStatus string) {
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "task_failed dismiss test",
				Status:      newStatus,
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   true,
			"prev_status":      prevStatus,
		},
	})
}

func TestNotification_StatusChange_ArchivesStaleTaskFailed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	subEmail := "notif-archive-task-failed-sub@goosar.ru"
	subID := createTestUser(t, subEmail)
	t.Cleanup(func() { cleanupTestUser(t, subEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", subID, "assignee")

	agentID := "00000000-0000-0000-0000-aaaaaaaaaaaa"

	for i := 0; i < 2; i++ {
		bus.Publish(events.Event{
			Type:        protocol.EventTaskFailed,
			WorkspaceID: testWorkspaceID,
			ActorType:   "system",
			Payload: map[string]any{
				"task_id":  "00000000-0000-0000-0000-bbbbbbbbbbbb",
				"agent_id": agentID,
				"issue_id": issueID,
			},
		})
	}

	_, err := testPool.Exec(context.Background(), `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, details)
		VALUES ($1, 'member', $2, 'new_comment', 'info', $3, 'sibling notification', '{}')
	`, testWorkspaceID, testUserID, issueID)
	if err != nil {
		t.Fatalf("seed sibling notification: %v", err)
	}

	if active, _ := countInboxByTypeForRecipient(t, testUserID, "task_failed"); active != 2 {
		t.Fatalf("precondition: expected 2 active task_failed rows for creator, got %d", active)
	}
	if active, _ := countInboxByTypeForRecipient(t, subID, "task_failed"); active != 2 {
		t.Fatalf("precondition: expected 2 active task_failed rows for sub, got %d", active)
	}

	var batchArchived []events.Event
	bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) {
		batchArchived = append(batchArchived, e)
	})

	publishStatusChange(bus, issueID, "in_review", "in_progress")

	for _, recipient := range []string{testUserID, subID} {
		active, archived := countInboxByTypeForRecipient(t, recipient, "task_failed")
		if active != 0 {
			t.Fatalf("recipient %s: expected 0 active task_failed rows after terminal status, got %d", recipient, active)
		}
		if archived != 2 {
			t.Fatalf("recipient %s: expected 2 archived task_failed rows after terminal status, got %d", recipient, archived)
		}
	}

	if active, _ := countInboxByTypeForRecipient(t, testUserID, "new_comment"); active != 1 {
		t.Fatalf("expected sibling new_comment row to remain active, got %d active", active)
	}

	if len(batchArchived) != 2 {
		t.Fatalf("expected 2 inbox:batch-archived events (one per recipient), got %d", len(batchArchived))
	}
	seenRecipients := map[string]bool{}
	for _, e := range batchArchived {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			t.Fatalf("inbox:batch-archived: unexpected payload type %T", e.Payload)
		}
		recipientID, _ := payload["recipient_id"].(string)
		if recipientID == "" {
			t.Fatalf("inbox:batch-archived: missing recipient_id in payload %+v", payload)
		}
		if payload["issue_id"] != issueID {
			t.Fatalf("inbox:batch-archived: expected issue_id %q, got %v", issueID, payload["issue_id"])
		}
		if payload["reason"] != "issue_status_terminal" {
			t.Fatalf("inbox:batch-archived: expected reason 'issue_status_terminal', got %v", payload["reason"])
		}
		if count, _ := payload["count"].(int64); count != 2 {
			t.Fatalf("inbox:batch-archived: expected count=2 for recipient %s, got %v", recipientID, payload["count"])
		}
		seenRecipients[recipientID] = true
	}
	if !seenRecipients[testUserID] || !seenRecipients[subID] {
		t.Fatalf("expected batch-archived events for both creator and sub, got %v", seenRecipients)
	}
}

func TestNotification_StatusChange_NonTerminalKeepsTaskFailed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")

	bus.Publish(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-bbbbbbbbbbbb",
			"agent_id": "00000000-0000-0000-0000-aaaaaaaaaaaa",
			"issue_id": issueID,
		},
	})

	if active, _ := countInboxByTypeForRecipient(t, testUserID, "task_failed"); active != 1 {
		t.Fatalf("precondition: expected 1 active task_failed row, got %d", active)
	}

	publishStatusChange(bus, issueID, "in_progress", "todo")

	active, archived := countInboxByTypeForRecipient(t, testUserID, "task_failed")
	if active != 1 || archived != 0 {
		t.Fatalf("expected task_failed row to remain active after non-terminal transition, got active=%d archived=%d", active, archived)
	}
}

func TestNotification_StatusChange_ReopenSurfacesNewTaskFailed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")

	agentID := "00000000-0000-0000-0000-aaaaaaaaaaaa"

	bus.Publish(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-bbbbbbbbbbbb",
			"agent_id": agentID,
			"issue_id": issueID,
		},
	})

	publishStatusChange(bus, issueID, "in_review", "in_progress")
	if active, archived := countInboxByTypeForRecipient(t, testUserID, "task_failed"); active != 0 || archived != 1 {
		t.Fatalf("after terminal transition: expected active=0 archived=1, got active=%d archived=%d", active, archived)
	}

	publishStatusChange(bus, issueID, "in_progress", "in_review")
	bus.Publish(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-cccccccccccc",
			"agent_id": agentID,
			"issue_id": issueID,
		},
	})

	active, archived := countInboxByTypeForRecipient(t, testUserID, "task_failed")
	if active != 1 {
		t.Fatalf("expected 1 active task_failed row after reopen+fail, got %d", active)
	}
	if archived != 1 {
		t.Fatalf("expected 1 archived task_failed row preserved from prior cycle, got %d", archived)
	}
}
