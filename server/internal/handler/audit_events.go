package handler

import (
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/adanman/goosar/server/internal/audit"
)

func (h *Handler) auditEvent(r *http.Request) audit.Event {
	ev := audit.Event{}
	if r == nil {
		return ev
	}

	ev.ClientIP = h.clientIPForRateLimit(r)
	ev.UserAgent = truncateUserAgent(r.UserAgent())
	ev.RequestID = chimw.GetReqID(r.Context())
	return ev
}

const maxAuditUserAgentLen = 512

func truncateUserAgent(ua string) string {
	if len(ua) > maxAuditUserAgentLen {
		return ua[:maxAuditUserAgentLen]
	}
	return ua
}

func (h *Handler) recordAudit(r *http.Request, ev audit.Event) {
	h.Audit.Record(r.Context(), ev)
}

func (h *Handler) auditSignIn(r *http.Request, action, email, outcome, reason string) {
	ev := h.auditEvent(r)
	ev.Action = action
	ev.ActorType = audit.ActorAnonymous
	ev.ActorID = hashEmailForLog(email)
	ev.TargetType = "email"
	ev.TargetID = hashEmailForLog(email)
	ev.Outcome = outcome
	ev.Reason = reason
	h.recordAudit(r, ev)
}

func (h *Handler) auditUserAction(r *http.Request, action, userID, targetType, targetID, outcome, reason string) {
	ev := h.auditEvent(r)
	ev.Action = action

	ev.ActorType = audit.ActorUser
	if userID == "" {
		ev.ActorType = audit.ActorAnonymous
	}
	ev.ActorID = userID
	ev.TargetType = targetType
	ev.TargetID = targetID
	ev.Outcome = outcome
	ev.Reason = reason
	h.recordAudit(r, ev)
}

func (h *Handler) auditSecretAccess(r *http.Request, action, userID, secretKind, targetID, workspaceID, outcome, reason string) {
	ev := h.auditEvent(r)
	ev.Action = action
	ev.ActorType = audit.ActorUser
	if userID == "" {
		ev.ActorType = audit.ActorSystem
	}
	ev.ActorID = userID
	ev.TargetType = secretKind
	ev.TargetID = targetID
	ev.WorkspaceID = workspaceID
	ev.Outcome = outcome
	ev.Reason = reason
	h.recordAudit(r, ev)
}

func (h *Handler) auditMembership(r *http.Request, action, actorUserID, targetUserID, workspaceID string) {
	ev := h.auditEvent(r)
	ev.Action = action
	ev.ActorType = audit.ActorUser
	ev.ActorID = actorUserID
	ev.TargetType = "workspace_member"
	ev.TargetID = targetUserID
	ev.WorkspaceID = workspaceID
	ev.Outcome = audit.OutcomeSuccess
	h.recordAudit(r, ev)
}
