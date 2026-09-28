package project

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

func TestProjectHandlersEndToEnd(t *testing.T) {
	db := newTestStore(t)
	wsID, acctID := seedWorkspace(t, db)
	wsStore := workspace.NewStore(db)
	deps := New(db, wsStore, nil, slog.Default())
	router := httpapi.New()
	Register(router, deps)

	rec := doJSON(t, router, http.MethodPost, "/api/projects", wsID, acctID, map[string]any{"title": "Launch"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("createProject: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created projectView
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != "planned" || created.Priority != "none" {
		t.Fatalf("unexpected defaults: %+v", created)
	}

	rec2 := doJSON(t, router, http.MethodGet, "/api/projects", wsID, acctID, nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("listProjects: status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var listResp struct {
		Projects []projectView `json:"projects"`
		Total    int           `json:"total"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if listResp.Total != 1 {
		t.Fatalf("expected 1 project, got %d", listResp.Total)
	}

	rec3 := doJSON(t, router, http.MethodDelete, "/api/projects/"+created.ID, wsID, acctID, nil)
	if rec3.Code != http.StatusNoContent {
		t.Fatalf("deleteProject: status=%d body=%s", rec3.Code, rec3.Body.String())
	}
	rec4 := doJSON(t, router, http.MethodGet, "/api/projects/"+created.ID, wsID, acctID, nil)
	if rec4.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec4.Code)
	}
}
