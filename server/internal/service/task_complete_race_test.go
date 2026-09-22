package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/events"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/taskfailure"
)

type mockRow struct {
	task *db.AgentTaskQueue
	err  error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	t := r.task
	ptrs := []any{
		&t.ID, &t.AgentID, &t.IssueID, &t.Status, &t.Priority,
		&t.DispatchedAt, &t.StartedAt, &t.CompletedAt, &t.Result,
		&t.Error, &t.CreatedAt, &t.Context, &t.RuntimeID,
		&t.SessionID, &t.WorkDir, &t.TriggerCommentID,
		&t.ChatSessionID, &t.AutopilotRunID,
	}
	for i, p := range ptrs {
		if i >= len(dest) {
			break
		}

		switch d := dest[i].(type) {
		case *pgtype.UUID:
			*d = *(p.(*pgtype.UUID))
		case *string:
			*d = *(p.(*string))
		case *int32:
			*d = *(p.(*int32))
		case *pgtype.Timestamptz:
			*d = *(p.(*pgtype.Timestamptz))
		case *[]byte:
			*d = *(p.(*[]byte))
		case *pgtype.Text:
			*d = *(p.(*pgtype.Text))
		}
	}
	return nil
}

type mockDBTX struct {
	task db.AgentTaskQueue
}

func (m *mockDBTX) Exec(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}

func (m *mockDBTX) Query(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
	return nil, pgx.ErrNoRows
}

func (m *mockDBTX) QueryRow(_ context.Context, sql string, _ ...interface{}) pgx.Row {

	if strings.Contains(sql, "SET status =") {
		return &mockRow{err: pgx.ErrNoRows}
	}

	return &mockRow{task: &m.task}
}

func testUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

func TestCompleteTask_AlreadyFinalized(t *testing.T) {
	taskID := testUUID(1)
	agentID := testUUID(2)

	tests := []struct {
		name   string
		status string
	}{
		{"already completed", "completed"},
		{"already cancelled", "cancelled"},
		{"already failed", "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDBTX{task: db.AgentTaskQueue{
				ID:      taskID,
				AgentID: agentID,
				Status:  tt.status,
			}}
			svc := &TaskService{
				Queries: db.New(mock),
				Bus:     events.New(),
			}

			got, err := svc.CompleteTask(context.Background(), taskID, nil, "", "", false)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got == nil {
				t.Fatal("expected task, got nil")
			}
			if got.Status != tt.status {
				t.Errorf("expected status %q, got %q", tt.status, got.Status)
			}
			if got.ID != taskID {
				t.Error("returned task ID doesn't match")
			}
		})
	}
}

func TestFailTask_AlreadyFinalized(t *testing.T) {
	taskID := testUUID(1)
	agentID := testUUID(2)

	tests := []struct {
		name   string
		status string
	}{
		{"already completed", "completed"},
		{"already cancelled", "cancelled"},
		{"already failed", "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDBTX{task: db.AgentTaskQueue{
				ID:      taskID,
				AgentID: agentID,
				Status:  tt.status,
			}}
			svc := &TaskService{
				Queries: db.New(mock),
				Bus:     events.New(),
			}

			got, err := svc.FailTask(context.Background(), taskID, "agent crashed", "", "", "", false)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got == nil {
				t.Fatal("expected task, got nil")
			}
			if got.Status != tt.status {
				t.Errorf("expected status %q, got %q", tt.status, got.Status)
			}
			if got.ID != taskID {
				t.Error("returned task ID doesn't match")
			}
		})
	}
}

func TestProviderNetworkRetrySchedule(t *testing.T) {
	const provNet = "agent_error.provider_network"

	ceilingCases := []struct {
		reason string
		max    int32
		want   int32
	}{
		{provNet, 2, providerNetworkMaxAttempts},
		{provNet, 1, 1},
		{provNet, 5, 5},
		{"timeout", 2, 2},
		{"timeout", 1, 1},
	}
	for _, tc := range ceilingCases {
		if got := retryAttemptCeiling(tc.reason, tc.max); got != tc.want {
			t.Errorf("ceiling(%q, %d) = %d, want %d", tc.reason, tc.max, got, tc.want)
		}
	}

	delayCases := []struct {
		reason        string
		failedAttempt int32
		want          time.Duration
	}{
		{provNet, 1, 0},
		{provNet, 2, providerNetworkFinalRetryWait},
		{"timeout", 2, 0},

		{"runtime_offline", 1, runtimeOfflineRetryDeferral},
	}
	for _, tc := range delayCases {
		if got := retryDelayForAttempt(tc.reason, tc.failedAttempt); got != tc.want {
			t.Errorf("retryDelayForAttempt(%q, %d) = %s, want %s", tc.reason, tc.failedAttempt, got, tc.want)
		}
	}

	mkTask := func(attempt, max int32) db.AgentTaskQueue {
		return db.AgentTaskQueue{
			Attempt:     attempt,
			MaxAttempts: max,
			IssueID:     pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		}
	}
	eligCases := []struct {
		name    string
		reason  string
		attempt int32
		max     int32
		want    bool
	}{
		{"provider_network first run retries", provNet, 1, 2, true},
		{"provider_network second run still retries (deferred tier)", provNet, 2, 2, true},
		{"provider_network third run is the ceiling", provNet, 3, 2, false},
		{"provider_network with retry disabled (max_attempts=1) never retries", provNet, 1, 1, false},
		{"timeout keeps single immediate retry", "timeout", 1, 2, true},
		{"timeout exhausts at attempt 2", "timeout", 2, 2, false},
		{"non-retryable reason never retries", "agent_error.unknown", 1, 2, false},
	}
	for _, tc := range eligCases {
		if got := retryEligible(tc.reason, mkTask(tc.attempt, tc.max)); got != tc.want {
			t.Errorf("%s: retryEligible(%q, attempt=%d/max=%d) = %v, want %v", tc.name, tc.reason, tc.attempt, tc.max, got, tc.want)
		}
	}
}

func TestTaskFailureClassifiers(t *testing.T) {
	cases := []struct {
		reason       string
		wantType     string
		wantResumeOK bool
		wantRetry    bool
	}{
		{reason: "timeout", wantType: "timeout", wantResumeOK: true, wantRetry: true},
		{reason: "codex_semantic_inactivity", wantType: "timeout", wantResumeOK: false, wantRetry: true},

		{reason: "agent_error.provider_network", wantType: "agent_error", wantResumeOK: true, wantRetry: true},
		{reason: "runtime_recovery", wantType: "runtime", wantResumeOK: true, wantRetry: true},
		{reason: "iteration_limit", wantType: "agent_output", wantResumeOK: false, wantRetry: false},
		{reason: "api_invalid_request", wantType: "agent_error", wantResumeOK: false, wantRetry: false},
		{reason: "agent_error.context_overflow", wantType: "agent_error", wantResumeOK: false, wantRetry: false},
		{reason: "agent_error", wantType: "agent_error", wantResumeOK: true, wantRetry: false},

		{reason: "agent_error.unknown", wantType: "agent_error", wantResumeOK: true, wantRetry: false},
	}

	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			if got := taskErrorType(tc.reason); got != tc.wantType {
				t.Fatalf("taskErrorType(%q) = %q, want %q", tc.reason, got, tc.wantType)
			}
			if got := !resumeUnsafeFailureReason(tc.reason); got != tc.wantResumeOK {
				t.Fatalf("resume-safe(%q) = %v, want %v", tc.reason, got, tc.wantResumeOK)
			}
			if got := retryableReasons[tc.reason]; got != tc.wantRetry {
				t.Fatalf("retryableReasons[%q] = %v, want %v", tc.reason, got, tc.wantRetry)
			}
		})
	}
}

func TestSkillBundleFailureFromLegacyDaemonRetries(t *testing.T) {
	const legacyErr = "resolve skill bundles: context deadline exceeded"
	task := db.AgentTaskQueue{
		Attempt:     1,
		MaxAttempts: 2,
		IssueID:     pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
	}

	legacyReason := taskfailure.ReasonAgentUnknown.String()
	if retryEligible(legacyReason, task) {
		t.Fatal("precondition: the raw catchall must not be retryable, or this test proves nothing")
	}

	normalized := taskfailure.NormalizeDaemonReason(legacyReason, legacyErr).String()
	if normalized != taskfailure.ReasonSkillBundleUnavailable.String() {
		t.Fatalf("normalized reason = %q, want %q", normalized, taskfailure.ReasonSkillBundleUnavailable)
	}
	if !retryEligible(normalized, task) {
		t.Errorf("a skill-bundle failure reported by an old daemon must still be retried; got reason %q", normalized)
	}

	current := taskfailure.NormalizeDaemonReason(
		taskfailure.ReasonSkillBundleUnavailable.String(),
		`skill bundle unavailable: skill "x" (id=1, 10 bytes) after 30s: context deadline exceeded`,
	).String()
	if !retryEligible(current, task) {
		t.Errorf("a skill-bundle failure reported by a current daemon must be retried; got reason %q", current)
	}
}

func TestContextOverflowFromLegacyDaemonRetiresSession(t *testing.T) {

	const overflowErr = "API Error: The model has reached its context window limit."

	legacyReason := taskfailure.ReasonAgentUnknown.String()
	if ResumeUnsafeFailure(legacyReason, overflowErr) {
		t.Fatal("precondition: the raw catchall must be resume-safe, or this test proves nothing")
	}

	normalized := taskfailure.NormalizeDaemonReason(legacyReason, overflowErr).String()
	if normalized != taskfailure.ReasonAgentContextOverflow.String() {
		t.Fatalf("normalized reason = %q, want %q", normalized, taskfailure.ReasonAgentContextOverflow)
	}
	if !ResumeUnsafeFailure(normalized, overflowErr) {
		t.Errorf("an overflow reported by an old daemon must retire the session; got reason %q", normalized)
	}

	current := taskfailure.NormalizeDaemonReason(taskfailure.ReasonAgentContextOverflow.String(), overflowErr).String()
	if !ResumeUnsafeFailure(current, overflowErr) {
		t.Errorf("an overflow reported by a current daemon must retire the session; got reason %q", current)
	}
}
