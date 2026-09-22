package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestSubscriberAPI(t *testing.T) {
	ctx := context.Background()

	createIssue := func(t *testing.T) string {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title": "Subscriber test issue",
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var issue IssueResponse
		json.NewDecoder(w.Body).Decode(&issue)
		return issue.ID
	}

	deleteIssue := func(t *testing.T, issueID string) {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/issues/"+issueID, nil)
		req = withURLParam(req, "id", issueID)
		testHandler.DeleteIssue(w, req)
	}

	t.Run("Subscribe", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]bool
		json.NewDecoder(w.Body).Decode(&resp)
		if !resp["subscribed"] {
			t.Fatal("SubscribeToIssue: expected subscribed=true")
		}

		subscribed, err := testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "member",
			UserID:   parseUUID(testUserID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber: %v", err)
		}
		if !subscribed {
			t.Fatal("expected user to be subscribed in DB")
		}
	})

	t.Run("SubscribeIdempotent", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue (1st): expected 200, got %d: %s", w.Code, w.Body.String())
		}

		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue (2nd): expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("ListSubscribers", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		w = httptest.NewRecorder()
		req = newRequest("GET", "/api/issues/"+issueID+"/subscribers", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.ListIssueSubscribers(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssueSubscribers: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var subscribers []SubscriberResponse
		json.NewDecoder(w.Body).Decode(&subscribers)
		if len(subscribers) == 0 {
			t.Fatal("ListIssueSubscribers: expected at least 1 subscriber")
		}
		found := false
		for _, s := range subscribers {
			if s.UserID == testUserID && s.UserType == "member" && s.Reason == "manual" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ListIssueSubscribers: expected to find test user subscriber, got %+v", subscribers)
		}
	})

	t.Run("Unsubscribe", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/issues/"+issueID+"/unsubscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.UnsubscribeFromIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("UnsubscribeFromIssue: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]bool
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["subscribed"] {
			t.Fatal("UnsubscribeFromIssue: expected subscribed=false")
		}

		subscribed, err := testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "member",
			UserID:   parseUUID(testUserID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber: %v", err)
		}
		if subscribed {
			t.Fatal("expected user to NOT be subscribed in DB")
		}
	})

	t.Run("SubscribeCrossWorkspaceUser", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		foreignUserID := "00000000-0000-0000-0000-000000000099"
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", map[string]any{
			"user_id":   foreignUserID,
			"user_type": "member",
		})
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("SubscribeToIssue with cross-workspace user: expected 403, got %d: %s", w.Code, w.Body.String())
		}

		subscribed, err := testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "member",
			UserID:   parseUUID(foreignUserID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber: %v", err)
		}
		if subscribed {
			t.Fatal("cross-workspace user should NOT be subscribed in DB")
		}
	})

	t.Run("UnsubscribeCrossWorkspaceUser", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		foreignUserID := "00000000-0000-0000-0000-000000000099"
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/unsubscribe", map[string]any{
			"user_id":   foreignUserID,
			"user_type": "member",
		})
		req = withURLParam(req, "id", issueID)
		testHandler.UnsubscribeFromIssue(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("UnsubscribeFromIssue with cross-workspace user: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("AgentCallerSubscribesItself", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		var agentID string
		err := testPool.QueryRow(ctx,
			`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "Handler Test Agent",
		).Scan(&agentID)
		if err != nil {
			t.Fatalf("failed to find test agent: %v", err)
		}

		agentTask := createHandlerTestTaskForAgent(t, agentID)
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", agentTask)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue (agent caller): expected 200, got %d: %s", w.Code, w.Body.String())
		}

		agentSubscribed, err := testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "agent",
			UserID:   parseUUID(agentID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber (agent): %v", err)
		}
		if !agentSubscribed {
			t.Fatal("expected agent to be subscribed in DB when X-Agent-ID is set")
		}

		memberSubscribed, err := testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "member",
			UserID:   parseUUID(testUserID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber (member): %v", err)
		}
		if memberSubscribed {
			t.Fatal("member must not be auto-subscribed when caller is an agent")
		}

		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/issues/"+issueID+"/unsubscribe", nil)
		req = withURLParam(req, "id", issueID)
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", agentTask)
		testHandler.UnsubscribeFromIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("UnsubscribeFromIssue (agent caller): expected 200, got %d: %s", w.Code, w.Body.String())
		}

		agentSubscribed, err = testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "agent",
			UserID:   parseUUID(agentID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber (agent, after unsubscribe): %v", err)
		}
		if agentSubscribed {
			t.Fatal("expected agent to be unsubscribed in DB when X-Agent-ID is set")
		}
	})

	t.Run("ListAfterUnsubscribe", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)

		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/issues/"+issueID+"/unsubscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.UnsubscribeFromIssue(w, req)

		w = httptest.NewRecorder()
		req = newRequest("GET", "/api/issues/"+issueID+"/subscribers", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.ListIssueSubscribers(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssueSubscribers: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var subscribers []SubscriberResponse
		json.NewDecoder(w.Body).Decode(&subscribers)
		if len(subscribers) != 0 {
			t.Fatalf("ListIssueSubscribers: expected 0 subscribers after unsubscribe, got %d", len(subscribers))
		}
	})
}
