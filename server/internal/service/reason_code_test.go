package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/dispatch"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestDispatchFailReasonCode(t *testing.T) {
	if got := dispatchFailReasonCode(ErrAttributionFailClosed); got != dispatch.ReasonAttributionBlocked {
		t.Errorf("bare fail-closed: got %q, want attribution_blocked", got)
	}

	wrapped := fmt.Errorf("dispatch create_issue: enqueue task for issue: %w", ErrAttributionFailClosed)
	if got := dispatchFailReasonCode(wrapped); got != dispatch.ReasonAttributionBlocked {
		t.Errorf("wrapped fail-closed: got %q, want attribution_blocked", got)
	}
	if got := dispatchFailReasonCode(errors.New("some other failure")); got != dispatch.ReasonInternalError {
		t.Errorf("generic error: got %q, want internal_error", got)
	}
}

func TestAgentReadinessReasonCode(t *testing.T) {
	validRuntime := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	archivedAt := pgtype.Timestamptz{Valid: true}

	if got := agentReadinessReasonCode(db.Agent{}); got != dispatch.ReasonRuntimeOffline {
		t.Errorf("no runtime bound: got %q, want runtime_offline", got)
	}

	if got := agentReadinessReasonCode(db.Agent{RuntimeID: validRuntime}); got != dispatch.ReasonRuntimeOffline {
		t.Errorf("bound offline runtime: got %q, want runtime_offline", got)
	}

	if got := agentReadinessReasonCode(db.Agent{ArchivedAt: archivedAt, RuntimeID: validRuntime}); got != dispatch.ReasonTargetUnavailable {
		t.Errorf("archived agent: got %q, want target_unavailable", got)
	}
}
