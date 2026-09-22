package middleware

import (
	"context"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/adanman/goosar/server/internal/audit"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func auditRecorder(queries *db.Queries) *audit.Recorder {
	if queries == nil {

		return nil
	}
	return audit.NewRecorder(queries)
}

func auditEventFor(r *http.Request, ev audit.Event) audit.Event {
	base := audit.FromRequest(r, TrustedProxiesFromEnv())
	ev.RequestID = base.RequestID
	ev.ClientIP = base.ClientIP
	ev.UserAgent = base.UserAgent
	if ev.RequestID == "" {
		ev.RequestID = chimw.GetReqID(r.Context())
	}
	return ev
}

func auditRefusal(r *http.Request, queries *db.Queries, userID, reason string) {
	actorType := audit.ActorAnonymous
	if userID != "" {
		actorType = audit.ActorUser
	}
	ev := auditEventFor(r, audit.Event{
		Action:     audit.ActionAuthRefused,
		ActorType:  actorType,
		ActorID:    userID,
		TargetType: "session",
		TargetID:   userID,
		Outcome:    audit.OutcomeDenied,
		Reason:     reason,
	})
	if !refusalRowAllowed(r.Context(), ev.ClientIP) {

		audit.Emit(ev)
		return
	}
	auditRecorder(queries).Record(r.Context(), ev)
}

const (
	refusalRowBudget = 20
	refusalRowWindow = time.Minute
)

var refusalRowLimiter = NewMemoryRateLimitStore()

func refusalRowAllowed(ctx context.Context, clientIP string) bool {
	count, _, err := refusalRowLimiter.Incr(ctx, "audit_refusal:"+clientIP, refusalRowWindow)
	if err != nil {

		return true
	}
	return count <= refusalRowBudget
}
