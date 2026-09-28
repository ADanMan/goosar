package importer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetJSON_returnsApiErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"forbidden","message":"nope"}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "tok")
	err := c.getJSON(t.Context(), "/api/whatever", "", &struct{}{})
	if err == nil {
		t.Fatal("expected error")
	}
	ae, ok := err.(*apiError)
	if !ok {
		t.Fatalf("expected *apiError, got %T: %v", err, err)
	}
	if ae.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want 403", ae.StatusCode)
	}
}

func TestGetJSON_sendsBearerAndWorkspaceSlugHeader(t *testing.T) {
	var gotAuth, gotSlug string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSlug = r.Header.Get("X-Workspace-Slug")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "gsl_abc123")
	if err := c.getJSON(t.Context(), "/api/agents", "acme", &struct{}{}); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if gotAuth != "Bearer gsl_abc123" {
		t.Errorf("Authorization = %q, want Bearer gsl_abc123", gotAuth)
	}
	if gotSlug != "acme" {
		t.Errorf("X-Workspace-Slug = %q, want acme", gotSlug)
	}
}

func TestResolveWorkspace_matchesBySlugOrID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]sourceWorkspace{
			{ID: "id-1", Slug: "acme", Name: "Acme"},
			{ID: "id-2", Slug: "beta", Name: "Beta"},
		})
	}))
	defer srv.Close()
	c := newClient(srv.URL, "tok")

	ws, err := c.resolveWorkspace(t.Context(), "beta")
	if err != nil {
		t.Fatalf("resolveWorkspace(slug): %v", err)
	}
	if ws.ID != "id-2" {
		t.Errorf("resolved by slug: ID = %q, want id-2", ws.ID)
	}

	ws, err = c.resolveWorkspace(t.Context(), "id-1")
	if err != nil {
		t.Fatalf("resolveWorkspace(id): %v", err)
	}
	if ws.Slug != "acme" {
		t.Errorf("resolved by id: Slug = %q, want acme", ws.Slug)
	}

	if _, err := c.resolveWorkspace(t.Context(), "missing"); err == nil {
		t.Error("expected error for unknown workspace")
	}
}

func TestListAllIssues_paginatesUntilShortPage(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		offset := r.URL.Query().Get("offset")
		w.Header().Set("Content-Type", "application/json")
		switch offset {
		case "0":
			issues := make([]sourceIssue, 100)
			for i := range issues {
				issues[i] = sourceIssue{ID: fmt.Sprintf("p1-%d", i)}
			}
			_ = json.NewEncoder(w).Encode(sourceIssueListResponse{Issues: issues, Total: 130})
		case "100":
			issues := make([]sourceIssue, 30)
			for i := range issues {
				issues[i] = sourceIssue{ID: fmt.Sprintf("p2-%d", i)}
			}
			_ = json.NewEncoder(w).Encode(sourceIssueListResponse{Issues: issues, Total: 130})
		default:
			t.Fatalf("unexpected offset %q", offset)
		}
	}))
	defer srv.Close()

	c := newClient(srv.URL, "tok")
	issues, err := c.listAllIssues(t.Context(), "acme")
	if err != nil {
		t.Fatalf("listAllIssues: %v", err)
	}
	if len(issues) != 130 {
		t.Fatalf("len(issues) = %d, want 130", len(issues))
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (must stop once a short page is seen)", calls)
	}
}

func TestListAllChatMessages_ordersOldestFirstAcrossPages(t *testing.T) {
	// Сервер отдаёт странички НАЗАД по времени: первая страница — самые
	// новые сообщения (newest-1, newest), вторая (по курсору) — самые
	// старые (oldest). listAllChatMessages должен вернуть [oldest, newest-1, newest].
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("before_id") == "" {
			_ = json.NewEncoder(w).Encode(sourceChatMessagesPage{
				Messages: []sourceChatMessage{{ID: "newest-1"}, {ID: "newest"}},
				HasMore:  true,
				NextCursor: map[string]any{"created_at": "2024-01-01T00:00:00Z", "id": "newest-1"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(sourceChatMessagesPage{
			Messages: []sourceChatMessage{{ID: "oldest"}},
			HasMore:  false,
		})
	}))
	defer srv.Close()

	c := newClient(srv.URL, "tok")
	msgs, err := c.listAllChatMessages(t.Context(), "acme", "session-1")
	if err != nil {
		t.Fatalf("listAllChatMessages: %v", err)
	}
	want := []string{"oldest", "newest-1", "newest"}
	if len(msgs) != len(want) {
		t.Fatalf("len(msgs) = %d, want %d: %+v", len(msgs), len(want), msgs)
	}
	for i, id := range want {
		if msgs[i].ID != id {
			t.Errorf("msgs[%d].ID = %q, want %q", i, msgs[i].ID, id)
		}
	}
}

func TestGetWorkspaceConfig_returnsNilOn403WithoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"forbidden"}`))
	}))
	defer srv.Close()
	c := newClient(srv.URL, "tok")
	cfg, err := c.getWorkspaceConfig(t.Context(), "acme")
	if err != nil {
		t.Fatalf("getWorkspaceConfig should swallow 403, got error: %v", err)
	}
	if cfg != nil {
		t.Errorf("cfg = %+v, want nil", cfg)
	}
}
