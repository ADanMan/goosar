package handler

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func TestSetAgentRuntimeSkillEnabledBroadcastsAgentStatus(t *testing.T) {
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, "Runtime Skill Broadcast "+t.Name(), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	var mu sync.Mutex
	var broadcasts []string
	testHandler.Bus.Subscribe(protocol.EventAgentStatus, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		agent, ok := payload["agent"].(AgentResponse)
		if !ok {
			return
		}
		mu.Lock()
		broadcasts = append(broadcasts, agent.ID)
		mu.Unlock()
	})

	countFor := func(id string) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, a := range broadcasts {
			if a == id {
				n++
			}
		}
		return n
	}

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
	if n := countFor(agentID); n != 1 {
		t.Fatalf("disabling a runtime skill must broadcast exactly one agent:status for %s, got %d", agentID, n)
	}

	if w := setEnabled(true); w.Code != 204 {
		t.Fatalf("enable inherited skill: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if n := countFor(agentID); n != 2 {
		t.Fatalf("re-enabling a runtime skill must broadcast a second agent:status for %s, got %d", agentID, n)
	}
}
