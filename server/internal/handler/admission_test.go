package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestRunToResponseDoesNotReverseEngineerReasonCode(t *testing.T) {
	run := db.AutopilotRun{
		Status: "skipped",
		FailureReason: pgtype.Text{
			String: "assignee agent lacks access to private assignee agent",
			Valid:  true,
		},
	}
	resp := runToResponse(run)
	if resp.ReasonCode != nil {
		t.Fatalf("runToResponse should not synthesize a reason_code from failure_reason, got %q", *resp.ReasonCode)
	}
	if resp.FailureReason == nil || *resp.FailureReason == "" {
		t.Errorf("failure_reason should still be surfaced for history rows")
	}
}

func TestDispatchBlockedFallbackMessageIsNonEnumerating(t *testing.T) {
	codes := []DispatchReasonCode{
		ReasonInvocationNotAllowed, ReasonTargetUnavailable, ReasonRuntimeOffline,
		ReasonAttributionBlocked, ReasonAlreadyActive, ReasonInternalError,
		DispatchReasonCode("some_future_code"),
	}
	for _, c := range codes {
		msg := dispatchBlockedFallbackMessage(c)
		if msg == "" {
			t.Errorf("reason %q: empty fallback message", c)
		}
	}

	if got := dispatchBlockedFallbackMessage(ReasonInvocationNotAllowed); got != "you don't have permission to use this target" {
		t.Errorf("invocation_not_allowed fallback = %q, changed to something more revealing?", got)
	}
}
