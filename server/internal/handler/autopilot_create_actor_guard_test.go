package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestAutopilotMutations_RejectTaskTokenActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "autopilot-guard-agent", nil)

	r := chi.NewRouter()
	r.With(RequireHumanActor).Post("/api/autopilots", testHandler.CreateAutopilot)
	r.With(RequireHumanActor).Patch("/api/autopilots/{id}", testHandler.UpdateAutopilot)
	r.With(RequireHumanActor).Delete("/api/autopilots/{id}", testHandler.DeleteAutopilot)

	t.Run("CreateAutopilot rejects task_token actor", func(t *testing.T) {
		req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
			"title":          "Guard-test autopilot (should not be created)",
			"assignee_id":    agentID,
			"execution_mode": "run_only",
		})
		req.Header.Set("X-Actor-Source", "task_token")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("CreateAutopilot as task_token actor: status = %d, want 403: %s", w.Code, w.Body.String())
		}

		var count int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM autopilot WHERE workspace_id = $1 AND title = $2`,
			testWorkspaceID, "Guard-test autopilot (should not be created)",
		).Scan(&count); err != nil {
			t.Fatalf("count query: %v", err)
		}
		if count != 0 {
			t.Fatalf("rejected create still wrote an autopilot row (count=%d)", count)
		}
	})

	var autopilotID string
	{
		req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
			"title":          "Guard-test fixture autopilot",
			"assignee_id":    agentID,
			"execution_mode": "run_only",
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("fixture CreateAutopilot as human actor: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var created AutopilotResponse
		if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
			t.Fatalf("decode fixture autopilot: %v", err)
		}
		autopilotID = created.ID
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		})
	}

	t.Run("UpdateAutopilot rejects task_token actor", func(t *testing.T) {
		req := newRequest("PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, map[string]any{
			"execution_mode": "create_issue",
		})
		req.Header.Set("X-Actor-Source", "task_token")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("UpdateAutopilot as task_token actor: status = %d, want 403: %s", w.Code, w.Body.String())
		}

		var mode string
		if err := testPool.QueryRow(ctx, `SELECT execution_mode FROM autopilot WHERE id = $1`, autopilotID).Scan(&mode); err != nil {
			t.Fatalf("load autopilot execution_mode: %v", err)
		}
		if mode != "run_only" {
			t.Fatalf("rejected update still changed execution_mode to %q", mode)
		}
	})

	t.Run("DeleteAutopilot rejects task_token actor", func(t *testing.T) {
		req := newRequest("DELETE", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, nil)
		req.Header.Set("X-Actor-Source", "task_token")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("DeleteAutopilot as task_token actor: status = %d, want 403: %s", w.Code, w.Body.String())
		}

		var status string
		if err := testPool.QueryRow(ctx, `SELECT status FROM autopilot WHERE id = $1`, autopilotID).Scan(&status); err != nil {
			t.Fatalf("load autopilot status: %v", err)
		}
		if status != "active" {
			t.Fatalf("rejected delete still archived the autopilot (status=%q)", status)
		}
	})
}

func TestAutopilotMutations_AllowHumanActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "autopilot-guard-human-agent", nil)

	r := chi.NewRouter()
	r.With(RequireHumanActor).Post("/api/autopilots", testHandler.CreateAutopilot)
	r.With(RequireHumanActor).Patch("/api/autopilots/{id}", testHandler.UpdateAutopilot)
	r.With(RequireHumanActor).Delete("/api/autopilots/{id}", testHandler.DeleteAutopilot)

	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Guard-test human-actor autopilot",
		"assignee_id":    agentID,
		"execution_mode": "run_only",
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot as human actor: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, created.ID)
	})

	updateReq := newRequest("PATCH", "/api/autopilots/"+created.ID+"?workspace_id="+testWorkspaceID, map[string]any{
		"execution_mode": "create_issue",
	})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, updateReq)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot as human actor: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	deleteReq := newRequest("DELETE", "/api/autopilots/"+created.ID+"?workspace_id="+testWorkspaceID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, deleteReq)
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("DeleteAutopilot as human actor: expected 200/204, got %d: %s", w.Code, w.Body.String())
	}
}
