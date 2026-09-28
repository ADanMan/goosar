package chat

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/workspace"
)

func newTestDeps(t *testing.T) (*Deps, fixture) {
	t.Helper()
	db := newTestStore(t)
	f := seedAgent(t, db)
	wsStore := workspace.NewStore(db)
	dispatchDeps := dispatch.New(nil, slog.Default())
	deps := New(db, wsStore, dispatchDeps, nil, slog.Default())
	return deps, f
}

func withActor(req *http.Request, userID, email string) *http.Request {
	actor := &httpapi.Actor{UserID: userID, Email: email, Name: "Test User", IsHuman: true, Source: httpapi.SourceSession}
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
	req.Header.Set("Content-Type", "application/json")
	req = withActor(req, userID, "test@example.com")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestChatSendMessageEndToEnd(t *testing.T) {
	deps, f := newTestDeps(t)
	router := httpapi.New()
	Register(router, deps)

	// createChatSession
	rec := doJSON(t, router, http.MethodPost, "/api/chat/sessions", f.WorkspaceID, f.AccountID,
		map[string]any{"agent_id": f.AgentID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("createChatSession: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sess Session
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}
	if sess.AgentID != f.AgentID {
		t.Fatalf("unexpected session: %+v", sess)
	}

	// sendChatMessage — должно поставить задачу через dispatch и сохранить сообщение.
	rec2 := doJSON(t, router, http.MethodPost, "/api/chat/sessions/"+sess.ID+"/messages", f.WorkspaceID, f.AccountID,
		map[string]any{"content": "Hello agent"})
	if rec2.Code != http.StatusCreated {
		t.Fatalf("sendChatMessage: status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var sendResp struct {
		MessageID string  `json:"message_id"`
		TaskID    *string `json:"task_id"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &sendResp); err != nil {
		t.Fatal(err)
	}
	if sendResp.MessageID == "" || sendResp.TaskID == nil || *sendResp.TaskID == "" {
		t.Fatalf("expected message_id and task_id to be set: %+v", sendResp)
	}

	// listChatMessages
	rec3 := doJSON(t, router, http.MethodGet, "/api/chat/sessions/"+sess.ID+"/messages", f.WorkspaceID, f.AccountID, nil)
	if rec3.Code != http.StatusOK {
		t.Fatalf("listChatMessages: status=%d body=%s", rec3.Code, rec3.Body.String())
	}
	var msgs []Message
	if err := json.Unmarshal(rec3.Body.Bytes(), &msgs); err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content != "Hello agent" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}

	// getPendingChatTask — задача только что поставлена, должна быть активной.
	rec4 := doJSON(t, router, http.MethodGet, "/api/chat/sessions/"+sess.ID+"/pending-task", f.WorkspaceID, f.AccountID, nil)
	if rec4.Code != http.StatusOK {
		t.Fatalf("getPendingChatTask: status=%d body=%s", rec4.Code, rec4.Body.String())
	}
	var pending PendingTask
	if err := json.Unmarshal(rec4.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if pending.TaskID != *sendResp.TaskID {
		t.Fatalf("pending task mismatch: %+v vs %s", pending, *sendResp.TaskID)
	}

	// markChatSessionRead
	rec5 := doJSON(t, router, http.MethodPost, "/api/chat/sessions/"+sess.ID+"/read", f.WorkspaceID, f.AccountID, nil)
	if rec5.Code != http.StatusNoContent {
		t.Fatalf("markChatSessionRead: status=%d body=%s", rec5.Code, rec5.Body.String())
	}

	// другой пользователь не должен иметь доступа к чужой сессии.
	rec6 := doJSON(t, router, http.MethodGet, "/api/chat/sessions/"+sess.ID, f.WorkspaceID, "00000000-0000-0000-0000-000000000099", nil)
	if rec6.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-owner, got %d body=%s", rec6.Code, rec6.Body.String())
	}

	// deleteChatSession отменяет активную задачу и удаляет сессию.
	rec7 := doJSON(t, router, http.MethodDelete, "/api/chat/sessions/"+sess.ID, f.WorkspaceID, f.AccountID, nil)
	if rec7.Code != http.StatusNoContent {
		t.Fatalf("deleteChatSession: status=%d body=%s", rec7.Code, rec7.Body.String())
	}
	rec8 := doJSON(t, router, http.MethodGet, "/api/chat/sessions/"+sess.ID, f.WorkspaceID, f.AccountID, nil)
	if rec8.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec8.Code)
	}
}

func TestChatSendMessageRejectsEmptyContent(t *testing.T) {
	deps, f := newTestDeps(t)
	router := httpapi.New()
	Register(router, deps)

	rec := doJSON(t, router, http.MethodPost, "/api/chat/sessions", f.WorkspaceID, f.AccountID, map[string]any{"agent_id": f.AgentID})
	var sess Session
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)

	rec2 := doJSON(t, router, http.MethodPost, "/api/chat/sessions/"+sess.ID+"/messages", f.WorkspaceID, f.AccountID, map[string]any{"content": "  "})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty content, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}
