package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func seedNULTask(t *testing.T, label string) (agentID, taskID string) {
	t.Helper()
	ctx := context.Background()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, testWorkspaceID, label, handlerTestRuntimeID(t), testUserID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, $2, 'in_progress', 'none', $3, 'member',
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1), 0)
		RETURNING id`, testWorkspaceID, label+" fixture", testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now())
		RETURNING id`, agentID, handlerTestRuntimeID(t), issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return agentID, taskID
}

func daemonTaskRequest(t *testing.T, path, taskID string, body any) *http.Request {
	t.Helper()
	req := newDaemonTokenRequest("POST", path, body, testWorkspaceID, "nul-regression-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestCompleteTaskCallbackWithNULSucceeds(t *testing.T) {
	ctx := context.Background()
	_, taskID := seedNULTask(t, "nul-complete-agent")

	w := httptest.NewRecorder()
	req := daemonTaskRequest(t, "/api/daemon/tasks/"+taskID+"/complete", taskID, map[string]any{
		"output":     "done\x00 summary text",
		"work_dir":   "/tmp/work\x00dir",
		"session_id": "sess-nul-complete",
	})

	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask returned %d, want 200: %s", w.Code, w.Body.String())
	}

	var (
		status      string
		completedAt *string
		storedJSON  []byte
	)
	if err := testPool.QueryRow(ctx, `
		SELECT status, completed_at::text, result
		FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &completedAt, &storedJSON); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if status != "completed" {
		t.Fatalf("status = %q, want completed", status)
	}
	if completedAt == nil {
		t.Fatal("completed_at is NULL, want a terminal timestamp")
	}

	var stored TaskCompleteRequest
	if err := json.Unmarshal(storedJSON, &stored); err != nil {
		t.Fatalf("decode stored result: %v", err)
	}

	if stored.Output != "done summary text" {
		t.Fatalf("stored output = %q, want %q", stored.Output, "done summary text")
	}
	if stored.WorkDir != "/tmp/workdir" {
		t.Fatalf("stored work_dir = %q, want %q", stored.WorkDir, "/tmp/workdir")
	}
}

func TestReportTaskMessagesCallbackWithNULSucceeds(t *testing.T) {
	ctx := context.Background()
	_, taskID := seedNULTask(t, "nul-messages-agent")

	w := httptest.NewRecorder()
	req := daemonTaskRequest(t, "/api/daemon/tasks/"+taskID+"/messages", taskID, map[string]any{
		"messages": []any{
			map[string]any{
				"seq":     1,
				"type":    "tool_use\x00",
				"tool":    "Bash\x00",
				"content": "reading\x00 the binary",
				"output":  "ELF\x00\x00binary",
				"input": map[string]any{
					"command": "cat",
					"args":    []any{"-n", "build/app.bin"},
					"result": map[string]any{
						"stdout": "ELF\x00\x00binary",
					},
				},
			},
		},
	})

	testHandler.ReportTaskMessages(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ReportTaskMessages returned %d, want 200: %s", w.Code, w.Body.String())
	}

	var (
		msgType    string
		msgTool    string
		msgContent string
		msgOutput  string
		msgInput   []byte
	)
	if err := testPool.QueryRow(ctx, `
		SELECT type, COALESCE(tool, ''), COALESCE(content, ''), COALESCE(output, ''), input
		FROM task_message WHERE task_id = $1 AND seq = 1`, taskID).
		Scan(&msgType, &msgTool, &msgContent, &msgOutput, &msgInput); err != nil {
		t.Fatalf("read persisted task message: %v", err)
	}

	if msgType != "tool_use" {
		t.Fatalf("stored type = %q, want %q", msgType, "tool_use")
	}
	if msgTool != "Bash" {
		t.Fatalf("stored tool = %q, want %q", msgTool, "Bash")
	}
	if msgContent != "reading the binary" {
		t.Fatalf("stored content = %q, want %q", msgContent, "reading the binary")
	}
	if msgOutput != "ELFbinary" {
		t.Fatalf("stored output = %q, want %q", msgOutput, "ELFbinary")
	}

	var storedInput struct {
		Result struct {
			Stdout string `json:"stdout"`
		} `json:"result"`
	}
	if err := json.Unmarshal(msgInput, &storedInput); err != nil {
		t.Fatalf("decode stored input: %v", err)
	}
	if storedInput.Result.Stdout != "ELFbinary" {
		t.Fatalf("stored nested stdout = %q, want %q", storedInput.Result.Stdout, "ELFbinary")
	}
}

func TestPostgresRejectsUnsanitizedTaskPayloads(t *testing.T) {
	ctx := context.Background()
	_, taskID := seedNULTask(t, "nul-control-agent")

	t.Run("complete result JSONB", func(t *testing.T) {
		raw, err := json.Marshal(TaskCompleteRequest{Output: "done\x00 summary"})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(raw), "\\u0000") {
			t.Fatalf("premise broken: payload carries no NUL escape: %s", raw)
		}
		if _, err := testHandler.Queries.CompleteAgentTask(ctx, db.CompleteAgentTaskParams{
			ID:     util.MustParseUUID(taskID),
			Result: raw,
		}); err == nil {
			t.Fatal("PostgreSQL accepted a JSONB payload containing a NUL escape")
		}

		var status string
		if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
			t.Fatalf("read task: %v", err)
		}
		if status != "running" {
			t.Fatalf("status after rejected write = %q, want running", status)
		}
	})

	t.Run("task message input JSONB", func(t *testing.T) {
		raw, err := json.Marshal(map[string]any{
			"result": map[string]any{"stdout": "ELF\x00binary"},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := testHandler.Queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
			TaskID: util.MustParseUUID(taskID),
			Seq:    99,
			Type:   "tool_use",
			Input:  raw,
		}); err == nil {
			t.Fatal("PostgreSQL accepted nested JSONB containing a NUL escape")
		}
	})

	t.Run("task message type TEXT", func(t *testing.T) {
		if _, err := testHandler.Queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
			TaskID: util.MustParseUUID(taskID),
			Seq:    98,
			Type:   "tool_use\x00",
		}); err == nil {
			t.Fatal("PostgreSQL accepted a TEXT value containing a NUL")
		}
	})
}
