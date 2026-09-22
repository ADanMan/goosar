package handler

import (
	"context"
	"encoding/json"
	"testing"

	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func setupDirectChatSession(t *testing.T, ctx context.Context, title string) (agentID, sessionID, runtimeID, daemonID string) {
	t.Helper()
	agentID, runtimeID, daemonID = createRuntimeGuardAgent(t, ctx)
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, title).Scan(&sessionID); err != nil {
		t.Fatalf("setup: create chat session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, sessionID) })
	return agentID, sessionID, runtimeID, daemonID
}

func sendDirectChat(t *testing.T, ctx context.Context, agentID, sessionID, content string) string {
	t.Helper()
	sess, err := testHandler.Queries.GetChatSession(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	ag, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	res, err := testHandler.TaskService.SendDirectChatMessage(ctx, sess, ag, parseUUID(testUserID), content, nil, "member", parseUUID(testUserID))
	if err != nil {
		t.Fatalf("SendDirectChatMessage: %v", err)
	}
	return uuidToString(res.Task.ID)
}

func markTaskRunning(t *testing.T, ctx context.Context, taskID string) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET status = 'running', started_at = now(), dispatched_at = now()
		WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("mark task running: %v", err)
	}
}

func completeResult(t *testing.T, output string) []byte {
	t.Helper()
	b, err := json.Marshal(TaskCompleteRequest{Output: output})
	if err != nil {
		t.Fatalf("marshal complete result: %v", err)
	}
	return b
}

func TestDirectChat_TaskOwnsItsOwnInputBatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "ownership chat")

	t1 := sendDirectChat(t, ctx, agentID, sessionID, "看上海天气")
	t2 := sendDirectChat(t, ctx, agentID, sessionID, "还有青岛")

	assertTaskInputOwner(t, ctx, t1, t1)
	assertTaskInputOwner(t, ctx, t2, t2)

	claimed := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if claimed.ChatMessage != "看上海天气" {
		t.Fatalf("first claim must deliver ONLY its owned message; got %q", claimed.ChatMessage)
	}

	markTaskRunning(t, ctx, t1)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(t1), completeResult(t, "上海晴"), "", "", false); err != nil {
		t.Fatalf("complete first task: %v", err)
	}
	claimed2 := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if claimed2.ChatMessage != "还有青岛" {
		t.Fatalf("second claim must deliver ONLY the second owned message; got %q", claimed2.ChatMessage)
	}
}

func TestDirectChat_RunningTaskDoesNotAbsorbNewMessage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "no-absorb chat")

	t1 := sendDirectChat(t, ctx, agentID, sessionID, "first")
	markTaskRunning(t, ctx, t1)

	t2 := sendDirectChat(t, ctx, agentID, sessionID, "second")
	if t1 == t2 {
		t.Fatal("second send must create a distinct task")
	}

	owned1, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(t1))
	if err != nil {
		t.Fatalf("list T1 owned input: %v", err)
	}
	if len(owned1) != 1 || owned1[0].Content != "first" {
		t.Fatalf("T1 must own only its own message; got %+v", msgContents(owned1))
	}
	owned2, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(t2))
	if err != nil {
		t.Fatalf("list T2 owned input: %v", err)
	}
	if len(owned2) != 1 || owned2[0].Content != "second" {
		t.Fatalf("T2 must own the mid-run message; got %+v", msgContents(owned2))
	}
}

func TestCompleteTask_ChatEmptyOutputWritesNoResponse(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "no-response chat")
	taskID := sendDirectChat(t, ctx, agentID, sessionID, "do a tool-only thing")
	markTaskRunning(t, ctx, taskID)

	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), completeResult(t, "   "), "", "", false); err != nil {
		t.Fatalf("complete task: %v", err)
	}

	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one assistant outcome, got %d", len(rows))
	}
	if rows[0].MessageKind != protocol.ChatMessageKindNoResponse {
		t.Fatalf("expected message_kind=no_response, got %q", rows[0].MessageKind)
	}
	if rows[0].Content == "" {
		t.Fatal("no_response row must carry a non-empty fallback body for old clients")
	}
	assertTaskStatus(t, ctx, taskID, "completed")
	assertNoRetryChild(t, ctx, taskID)
}

func TestCompleteTask_ChatNonEmptyOutputWritesMessage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "reply chat")
	taskID := sendDirectChat(t, ctx, agentID, sessionID, "hello")
	markTaskRunning(t, ctx, taskID)

	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), completeResult(t, "hi there"), "sess-1", "/tmp/wd", false); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one assistant message, got %d", len(rows))
	}
	if rows[0].MessageKind != protocol.ChatMessageKindMessage {
		t.Fatalf("expected message_kind=message, got %q", rows[0].MessageKind)
	}
	if rows[0].Content != "hi there" {
		t.Fatalf("expected content 'hi there', got %q", rows[0].Content)
	}
}

func TestCompleteTask_ChatCallbackIdempotent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "idempotent chat")
	taskID := sendDirectChat(t, ctx, agentID, sessionID, "hey")
	markTaskRunning(t, ctx, taskID)

	res := completeResult(t, "reply")
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), res, "", "", false); err != nil {
		t.Fatalf("first complete: %v", err)
	}

	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), res, "", "", false); err != nil {
		t.Fatalf("replayed complete must be idempotent success, got %v", err)
	}
	if rows := assistantRows(t, ctx, sessionID); len(rows) != 1 {
		t.Fatalf("expected exactly one assistant outcome after replay, got %d", len(rows))
	}
}

func TestFailTask_ChatRetryInheritsInputOwnerAndPriority(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "retry chat")
	rootID := sendDirectChat(t, ctx, agentID, sessionID, "root question")
	markTaskRunning(t, ctx, rootID)

	if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(rootID), "runtime went away", "", "", "runtime_offline", false); err != nil {
		t.Fatalf("fail task: %v", err)
	}

	var childID, childOwner string
	var childPriority, childAttempt int
	var childStatus string
	if err := testPool.QueryRow(ctx, `
		SELECT id, chat_input_task_id, priority, status, attempt
		FROM agent_task_queue
		WHERE parent_task_id = $1
	`, rootID).Scan(&childID, &childOwner, &childPriority, &childStatus, &childAttempt); err != nil {
		t.Fatalf("expected a retry child, got: %v", err)
	}
	if childOwner != rootID {
		t.Fatalf("retry child must inherit the root input owner %s, got %s", rootID, childOwner)
	}
	if childPriority < 3 {
		t.Fatalf("chat retry must be bumped above fresh chat priority (2); got %d", childPriority)
	}
	if childStatus != "deferred" {
		t.Fatalf("runtime_offline retry child must be deferred, got %q", childStatus)
	}

	var rootAttempt int
	if err := testPool.QueryRow(ctx, `SELECT attempt FROM agent_task_queue WHERE id = $1`, rootID).Scan(&rootAttempt); err != nil {
		t.Fatalf("read root attempt: %v", err)
	}
	if childAttempt != rootAttempt+1 {
		t.Fatalf("retry child attempt must be root+1 (%d), got %d", rootAttempt+1, childAttempt)
	}

	owned, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(childOwner))
	if err != nil {
		t.Fatalf("list child owned input: %v", err)
	}
	if len(owned) != 1 || owned[0].Content != "root question" {
		t.Fatalf("retry child must read the root input batch; got %+v", msgContents(owned))
	}
}

func msgContents(msgs []db.ChatMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}

func assistantRows(t *testing.T, ctx context.Context, sessionID string) []db.ChatMessage {
	t.Helper()
	all, err := testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	var out []db.ChatMessage
	for _, m := range all {
		if m.Role == "assistant" {
			out = append(out, m)
		}
	}
	return out
}

func assertTaskInputOwner(t *testing.T, ctx context.Context, taskID, wantOwner string) {
	t.Helper()
	var owner string
	if err := testPool.QueryRow(ctx, `SELECT chat_input_task_id FROM agent_task_queue WHERE id = $1`, taskID).Scan(&owner); err != nil {
		t.Fatalf("read chat_input_task_id: %v", err)
	}
	if owner != wantOwner {
		t.Fatalf("task %s input owner = %s, want %s", taskID, owner, wantOwner)
	}
}

func assertTaskStatus(t *testing.T, ctx context.Context, taskID, want string) {
	t.Helper()
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != want {
		t.Fatalf("task %s status = %q, want %q", taskID, status, want)
	}
}

func assertNoRetryChild(t *testing.T, ctx context.Context, taskID string) {
	t.Helper()
	var n int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, taskID).Scan(&n); err != nil {
		t.Fatalf("count retry children: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected no retry child for a completed no_response turn, got %d", n)
	}
}

func insertChannelChatTask(t *testing.T, ctx context.Context, agentID, runtimeID, sessionID string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, dispatched_at)
		VALUES ($1, $2, $3, 'running', 2, now(), now())
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create channel chat task: %v", err)
	}
	return taskID
}

func insertSealedChannelChatTask(t *testing.T, ctx context.Context, agentID, runtimeID, sessionID, content string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, dispatched_at)
		VALUES ($1, $2, $3, 'running', 2, now(), now())
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create sealed channel chat task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("setup: set input owner: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, task_id, channel_ingested)
		VALUES ($1, 'user', $2, $3, TRUE)
	`, sessionID, content, taskID); err != nil {
		t.Fatalf("setup: seal channel user message: %v", err)
	}
	return taskID
}

func TestCompleteTask_ChannelEmptyOutputWritesNoRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "channel-like chat")

	emptyTask := insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(emptyTask), completeResult(t, "   "), "", "", false); err != nil {
		t.Fatalf("complete channel task (empty): %v", err)
	}
	if rows := assistantRows(t, ctx, sessionID); len(rows) != 0 {
		t.Fatalf("channel empty completion must write NO assistant row (Slack/Lark silent-drop), got %d", len(rows))
	}

	textTask := insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(textTask), completeResult(t, "channel reply"), "", "", false); err != nil {
		t.Fatalf("complete channel task (text): %v", err)
	}
	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("channel non-empty completion must write exactly one message, got %d", len(rows))
	}
	if rows[0].MessageKind != protocol.ChatMessageKindMessage || rows[0].Content != "channel reply" {
		t.Fatalf("channel message = kind %q content %q, want message/'channel reply'", rows[0].MessageKind, rows[0].Content)
	}
}

func TestCompleteTask_SealedChannelEmptyOutputWritesNoRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "sealed channel chat")

	emptyTask := insertSealedChannelChatTask(t, ctx, agentID, runtimeID, sessionID, "[Image]")
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(emptyTask), completeResult(t, "   "), "", "", false); err != nil {
		t.Fatalf("complete sealed channel task (empty): %v", err)
	}
	if rows := assistantRows(t, ctx, sessionID); len(rows) != 0 {
		t.Fatalf("sealed channel empty completion must write NO assistant row, got %d", len(rows))
	}

	textTask := insertSealedChannelChatTask(t, ctx, agentID, runtimeID, sessionID, "hello")
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(textTask), completeResult(t, "sealed channel reply"), "", "", false); err != nil {
		t.Fatalf("complete sealed channel task (text): %v", err)
	}
	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 || rows[0].MessageKind != protocol.ChatMessageKindMessage || rows[0].Content != "sealed channel reply" {
		t.Fatalf("sealed channel non-empty completion = %+v, want one ordinary message", rows)
	}
}

func TestCompleteTask_SealedChannelRetryEmptyOutputWritesNoRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "sealed channel retry chat")

	parentTask := insertSealedChannelChatTask(t, ctx, agentID, runtimeID, sessionID, "[Video]")
	var retryTask string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, dispatched_at, parent_task_id, retry_of_task_id, chat_input_task_id)
		VALUES ($1, $2, $3, 'running', 2, now(), now(), $4, $4, $4)
		RETURNING id
	`, agentID, runtimeID, sessionID, parentTask).Scan(&retryTask); err != nil {
		t.Fatalf("setup: create retry clone: %v", err)
	}

	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(retryTask), completeResult(t, ""), "", "", false); err != nil {
		t.Fatalf("complete sealed channel retry (empty): %v", err)
	}
	if rows := assistantRows(t, ctx, sessionID); len(rows) != 0 {
		t.Fatalf("sealed channel retry empty completion must write NO assistant row, got %d", len(rows))
	}
}
