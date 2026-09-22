// Пакет audit — журнал аутентификации и доступа к секретам. Один вызов пишет
// в два места: строку в auth_audit (читается через GET /api/deployment/audit)
// и JSON-объект в STDOUT для SIEM.
package audit

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	"github.com/adanman/goosar/server/internal/util/clientip"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const LoggerName = "audit"

const SchemaVersion = 1

const maxUserAgentLen = 512

const (
	ActorUser      = "user"
	ActorAgent     = "agent"
	ActorSystem    = "system"
	ActorAnonymous = "anonymous"
)

const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
	OutcomeDenied  = "denied"
)

const (
	ActionLoginCodeSent     = "auth.login_code.sent"
	ActionLoginCodeVerified = "auth.login_code.verified"
	ActionLoginCodeFailed   = "auth.login_code.failed"
	ActionLoginLinkVerified = "auth.login_link.verified"
	ActionLoginLinkFailed   = "auth.login_link.failed"
	ActionLoginDenied       = "auth.login.denied"
	ActionLogout            = "auth.logout"
	ActionCliTokenIssued    = "auth.cli_token.issued"

	ActionMFARequired       = "auth.mfa.required"
	ActionMFAEnrolled       = "auth.mfa.enrolled"
	ActionMFAConfirmed      = "auth.mfa.confirmed"
	ActionMFADisabled       = "auth.mfa.disabled"
	ActionMFAVerified       = "auth.mfa.verified"
	ActionMFAFailed         = "auth.mfa.failed"
	ActionMFARecoveryUsed   = "auth.mfa.recovery_used"
	ActionMFARecoveryIssued = "auth.mfa.recovery_issued"
	ActionMFAReset          = "auth.mfa.reset"
	ActionSessionRevoked    = "auth.session.revoked"
	ActionSessionsRevoked   = "auth.sessions.revoked_all"

	ActionPATCreated  = "auth.pat.created"
	ActionPATRevoked  = "auth.pat.revoked"
	ActionPATFirstUse = "auth.pat.first_use"

	ActionAuthRefused = "auth.credential.refused"

	ActionWorkspaceMemberAdded   = "workspace.member.added"
	ActionWorkspaceMemberRemoved = "workspace.member.removed"

	ActionSecretRead    = "secret.read"
	ActionSecretWrite   = "secret.write"
	ActionSecretRotated = "secret.rotate"
)

const (
	ReasonInvalidCode      = "invalid_code"
	ReasonExpiredCode      = "expired_code"
	ReasonCodeAlreadyUsed  = "code_already_used"
	ReasonInvalidLink      = "invalid_link"
	ReasonInvalidToken     = "invalid_token"
	ReasonSessionRevoked   = "session_revoked"
	ReasonDeactivated      = "deactivated"
	ReasonSignupRestricted = "signup_restricted"
	ReasonRateLimited      = "rate_limited"
	ReasonKeyUnavailable   = "key_unavailable"

	ReasonMFAPendingInvalid = "mfa_pending_invalid"
	ReasonMFACodeInvalid    = "mfa_code_invalid"
	ReasonMFACodeReplayed   = "mfa_code_replayed"
	ReasonMFANotEnrolled    = "mfa_not_enrolled"
	ReasonSessionIdle       = "session_idle_timeout"
	ReasonSessionExpired    = "session_absolute_lifetime"
	ReasonSessionEvicted    = "session_concurrency_cap"
)

type Event struct {
	Action      string
	ActorType   string
	ActorID     string
	ActorRole   string
	TargetType  string
	TargetID    string
	Outcome     string
	Reason      string
	WorkspaceID string
	RequestID   string
	ClientIP    string
	UserAgent   string
}

type Recorder struct {
	Queries *db.Queries
}

func NewRecorder(queries *db.Queries) *Recorder { return &Recorder{Queries: queries} }

func (rec *Recorder) Record(ctx context.Context, ev Event) {
	Emit(ev)
	if rec == nil || rec.Queries == nil {
		return
	}
	if _, err := rec.Queries.InsertAuthAudit(ctx, db.InsertAuthAuditParams{
		ActorType:   ev.ActorType,
		ActorID:     text(ev.ActorID),
		ActorRole:   text(ev.ActorRole),
		Action:      ev.Action,
		TargetType:  ev.TargetType,
		TargetID:    text(ev.TargetID),
		Outcome:     ev.Outcome,
		Reason:      text(ev.Reason),
		WorkspaceID: optionalUUID(ev.WorkspaceID),
		RequestID:   text(ev.RequestID),
		ClientIp:    text(ev.ClientIP),
		UserAgent:   text(ev.UserAgent),
	}); err != nil {

		slog.Error("audit: failed to persist an event — the stdout line is still the record of it",
			"action", ev.Action, "error", err)
	}
}

func FromRequest(r *http.Request, trustedProxies []*net.IPNet) Event {
	if r == nil {
		return Event{}
	}
	ua := r.UserAgent()
	if len(ua) > maxUserAgentLen {
		ua = ua[:maxUserAgentLen]
	}
	return Event{
		RequestID: chimw.GetReqID(r.Context()),
		ClientIP:  clientip.Of(r, trustedProxies),
		UserAgent: ua,
	}
}

var (
	streamMu     sync.RWMutex
	streamWriter io.Writer = os.Stdout
)

func Emit(ev Event) {
	line := map[string]any{
		"time":        time.Now().UTC().Format(time.RFC3339Nano),
		"logger":      LoggerName,
		"schema":      SchemaVersion,
		"action":      ev.Action,
		"actor_type":  ev.ActorType,
		"target_type": ev.TargetType,
		"outcome":     ev.Outcome,
	}

	for key, value := range map[string]string{
		"actor_id":     ev.ActorID,
		"actor_role":   ev.ActorRole,
		"target_id":    ev.TargetID,
		"reason":       ev.Reason,
		"workspace_id": ev.WorkspaceID,
		"request_id":   ev.RequestID,
		"client_ip":    ev.ClientIP,
		"user_agent":   ev.UserAgent,
	} {
		if strings.TrimSpace(value) != "" {
			line[key] = value
		}
	}
	encoded, err := json.Marshal(line)
	if err != nil {
		slog.Error("audit: could not encode an event", "action", ev.Action, "error", err)
		return
	}
	streamMu.RLock()
	w := streamWriter
	streamMu.RUnlock()

	if _, err := w.Write(append(encoded, '\n')); err != nil {
		slog.Error("audit: could not write to the audit stream", "action", ev.Action, "error", err)
	}
}

func setStreamWriterForTest(w io.Writer) func() {
	streamMu.Lock()
	previous := streamWriter
	streamWriter = w
	streamMu.Unlock()
	return func() {
		streamMu.Lock()
		streamWriter = previous
		streamMu.Unlock()
	}
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func optionalUUID(s string) pgtype.UUID {
	if strings.TrimSpace(s) == "" {
		return pgtype.UUID{}
	}
	id, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}
