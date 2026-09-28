package feed

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/workspace"
)

func withActor(req *http.Request, userID string) *http.Request {
	actor := &httpapi.Actor{UserID: userID, Email: "test@example.com", Name: "Test User", IsHuman: true, Source: httpapi.SourceSession}
	return req.WithContext(httpapi.WithActor(req.Context(), actor))
}

func doJSON(t *testing.T, router *httpapi.Router, method, path, workspaceID, userID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("X-Workspace-ID", workspaceID)
	req = withActor(req, userID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestInboxHandlersEndToEnd(t *testing.T) {
	db := newTestStore(t)
	wsID, acctID := seedWorkspace(t, db)
	wsStore := workspace.NewStore(db)
	deps := New(db, wsStore, nil, slog.Default())
	router := httpapi.New()
	Register(router, deps)

	alert, created, err := Notify(t.Context(), deps.Store.Q(), NotifyParams{
		WorkspaceID: wsID, RecipientType: "member", RecipientID: acctID, Kind: "new_comment", Title: "New comment",
	})
	if err != nil || !created {
		t.Fatalf("Notify: created=%v err=%v", created, err)
	}

	rec := doJSON(t, router, http.MethodGet, "/api/inbox", wsID, acctID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("listInbox: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list []Alert
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != alert.ID {
		t.Fatalf("unexpected inbox list: %+v", list)
	}

	rec2 := doJSON(t, router, http.MethodPost, "/api/inbox/"+alert.ID+"/read", wsID, acctID, nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("markInboxItemRead: status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	rec3 := doJSON(t, router, http.MethodGet, "/api/inbox/unread-count", wsID, acctID, nil)
	var countResp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rec3.Body.Bytes(), &countResp); err != nil {
		t.Fatal(err)
	}
	if countResp.Count != 0 {
		t.Fatalf("expected 0 unread after read, got %d", countResp.Count)
	}

	rec4 := doJSON(t, router, http.MethodPatch, "/api/notification-preferences", wsID, acctID,
		map[string]any{"preferences": map[string]string{"comments": "muted"}})
	if rec4.Code != http.StatusOK {
		t.Fatalf("patchNotificationPreferences: status=%d body=%s", rec4.Code, rec4.Body.String())
	}
	var prefsResp struct {
		Preferences map[string]string `json:"preferences"`
	}
	if err := json.Unmarshal(rec4.Body.Bytes(), &prefsResp); err != nil {
		t.Fatal(err)
	}
	if prefsResp.Preferences["comments"] != "muted" {
		t.Fatalf("unexpected preferences: %+v", prefsResp.Preferences)
	}
}
