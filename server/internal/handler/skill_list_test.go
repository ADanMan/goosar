package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestListSkills_OmitsContent(t *testing.T) {
	skillID := insertHandlerTestSkill(t, "list-omits-content", strings.Repeat("a", 4096))

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/skills?workspace_id="+testWorkspaceID, nil)
	testHandler.ListSkills(w, req)
	if w.Code != 200 {
		t.Fatalf("ListSkills: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rows []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("ListSkills: failed to decode body: %v", err)
	}

	var found bool
	for _, row := range rows {
		if row["id"] != skillID {
			continue
		}
		found = true
		if _, ok := row["content"]; ok {
			t.Fatalf("ListSkills: response must not include `content` field, got: %v", row)
		}

		for _, key := range []string{"id", "name", "description", "config", "created_at", "updated_at", "workspace_id"} {
			if _, ok := row[key]; !ok {
				t.Fatalf("ListSkills: missing expected field %q in response: %v", key, row)
			}
		}
	}
	if !found {
		t.Fatalf("ListSkills: inserted skill %s not in response", skillID)
	}
}

func TestGetSkill_IncludesContent(t *testing.T) {
	body := "# detail body\nstill served on /api/skills/{id}"
	skillID := insertHandlerTestSkill(t, "detail-includes-content", body)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/skills/"+skillID, nil)
	req = withURLParam(req, "id", skillID)
	testHandler.GetSkill(w, req)
	if w.Code != 200 {
		t.Fatalf("GetSkill: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("GetSkill: failed to decode body: %v", err)
	}
	if got, _ := resp["content"].(string); got != body {
		t.Fatalf("GetSkill: expected content %q, got %q", body, got)
	}
}

func TestListAgentSkills_OmitsContent(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Skill Summary Test", nil)
	skillID := insertHandlerTestSkill(t, "agent-skill-omits-content", strings.Repeat("b", 1024))
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`,
		agentID, skillID,
	); err != nil {
		t.Fatalf("attach skill to agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/agents/"+agentID+"/skills", nil)
	req = withURLParam(req, "id", agentID)
	testHandler.ListAgentSkills(w, req)
	if w.Code != 200 {
		t.Fatalf("ListAgentSkills: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rows []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("ListAgentSkills: failed to decode body: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("ListAgentSkills: expected at least 1 skill")
	}
	for _, row := range rows {
		if _, ok := row["content"]; ok {
			t.Fatalf("ListAgentSkills: response must not include `content` field, got: %v", row)
		}
	}
}

func TestSetAgentSkillEnabledControlsExecutionWithoutRemovingAssignment(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Skill Toggle Test", nil)
	skillID := insertHandlerTestSkill(t, "agent-skill-toggle", "# Toggle me")
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`,
		agentID, skillID,
	); err != nil {
		t.Fatalf("attach skill to agent: %v", err)
	}

	setEnabled := func(enabled bool) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/agents/"+agentID+"/skills/"+skillID+"/enabled", map[string]any{
			"enabled": enabled,
		})
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", agentID)
		rctx.URLParams.Add("skillId", skillID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		testHandler.SetAgentSkillEnabled(w, req)
		return w
	}

	if w := setEnabled(false); w.Code != 200 {
		t.Fatalf("disable skill: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	rows, err := testHandler.Queries.ListAgentSkillSummaries(context.Background(), parseUUID(agentID))
	if err != nil || len(rows) != 1 || rows[0].Enabled {
		t.Fatalf("disabled assignment not preserved: rows=%+v err=%v", rows, err)
	}

	active, err := testHandler.Queries.ListAgentSkills(context.Background(), parseUUID(agentID))
	if err != nil || len(active) != 0 {
		t.Fatalf("disabled skill leaked into execution: skills=%+v err=%v", active, err)
	}

	if w := setEnabled(true); w.Code != 200 {
		t.Fatalf("enable skill: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	active, err = testHandler.Queries.ListAgentSkills(context.Background(), parseUUID(agentID))
	if err != nil || len(active) != 1 || active[0].Name == "" {
		t.Fatalf("re-enabled skill missing from execution: skills=%+v err=%v", active, err)
	}
}

func TestSetAgentRuntimeSkillEnabledPersistsScopedOverride(t *testing.T) {
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, "Runtime Skill Toggle "+t.Name(), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	setEnabled := func(enabled bool) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/agents/"+agentID+"/runtime-skills/enabled", map[string]any{
			"runtime_id": runtimeID,
			"root":       "provider",
			"key":        "review",
			"name":       "Review Helper",
			"enabled":    enabled,
		})
		req = withURLParam(req, "id", agentID)
		testHandler.SetAgentRuntimeSkillEnabled(w, req)
		return w
	}

	if w := setEnabled(false); w.Code != 204 {
		t.Fatalf("disable inherited skill: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	row, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	disabled := decodeDisabledRuntimeSkills(row.DisabledRuntimeSkills)
	if len(disabled) != 1 || disabled[0].RuntimeID != runtimeID ||
		disabled[0].Provider != "runtime-c" || disabled[0].Root != "provider" || disabled[0].Key != "review" ||
		disabled[0].Name != "Review Helper" {
		t.Fatalf("unexpected disabled runtime skills: %+v", disabled)
	}
	if w := setEnabled(false); w.Code != 204 {
		t.Fatalf("repeat disable inherited skill: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	row, err = testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	if err != nil || len(decodeDisabledRuntimeSkills(row.DisabledRuntimeSkills)) != 1 {
		t.Fatalf("repeat disable was not idempotent: row=%+v err=%v", row, err)
	}

	if w := setEnabled(true); w.Code != 204 {
		t.Fatalf("enable inherited skill: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	row, err = testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	if err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	if disabled = decodeDisabledRuntimeSkills(row.DisabledRuntimeSkills); len(disabled) != 0 {
		t.Fatalf("re-enabled skill still disabled: %+v", disabled)
	}
}

func TestGetSkill_MalformedUUIDReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/skills/not-a-uuid", nil)
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.GetSkill(w, req)
	if w.Code != 400 {
		t.Fatalf("GetSkill malformed uuid: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func insertHandlerTestSkill(t *testing.T, namePrefix, content string) string {
	t.Helper()
	name := namePrefix + "-" + t.Name()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, $3, $4, '{}'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, "fixture", content, testUserID).Scan(&id); err != nil {
		t.Fatalf("insert skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, id)
	})
	return id
}
