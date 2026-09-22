// Сессии: выпуск, список и завершение. Сессия — строка user_session плюс JWT
// с claim sid. Все пути входа проходят через issueSessionJWT, поэтому таймаут
// бездействия, срок жизни и лимит одновременных сессий действуют везде.
package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/logger"
	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const maxUserAgentLen = 256

func hashClientIP(addr string) string {
	if addr == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(addr))
	return hex.EncodeToString(sum[:8])
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (h *Handler) issueSessionJWT(r *http.Request, user db.User) (string, pgtype.UUID, error) {
	ua := r.UserAgent()
	if len(ua) > maxUserAgentLen {
		ua = ua[:maxUserAgentLen]
	}
	session, err := h.Queries.CreateUserSession(r.Context(), db.CreateUserSessionParams{
		UserID:    user.ID,
		UserAgent: ua,
		IpHash:    hashClientIP(remoteHost(r)),
	})
	if err != nil {
		return "", pgtype.UUID{}, err
	}

	if cap := h.SessionPolicy(r.Context()).MaxConcurrent; cap > 0 {
		evicted, err := h.Queries.RevokeOldestUserSessions(r.Context(), db.RevokeOldestUserSessionsParams{
			UserID: user.ID,
			Offset: int32(cap),
		})
		if err != nil {

			slog.Warn("session cap: eviction failed", append(logger.RequestAttrs(r), "error", err)...)
		} else if evicted > 0 {
			h.auditUserAction(r, audit.ActionSessionRevoked, uuidToString(user.ID), "user_session", "",
				audit.OutcomeSuccess, audit.ReasonSessionEvicted)
		}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   uuidToString(user.ID),
		"email": user.Email,
		"name":  user.Name,
		"tv":    user.TokenVersion,

		"sid": uuidToString(session.ID),
		"exp": time.Now().Add(auth.AuthTokenTTL()).Unix(),
		"iat": time.Now().Unix(),
	})
	signed, err := token.SignedString(auth.JWTSecret())
	if err != nil {
		return "", pgtype.UUID{}, err
	}
	return signed, session.ID, nil
}

type LoginResult struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`

	MFARequired bool `json:"mfa_required,omitempty"`

	MFAToken string `json:"mfa_token,omitempty"`

	MFAEnrollmentRequired bool `json:"mfa_enrollment_required,omitempty"`
}

func (h *Handler) writeLoginSession(w http.ResponseWriter, r *http.Request, user db.User, successAction string) {
	token, _, err := h.issueSessionJWT(r, user)
	if err != nil {
		slog.Warn("login failed", append(logger.RequestAttrs(r), "error", err, "user_id", uuidToString(user.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	if err := auth.SetAuthCookies(w, token); err != nil {
		slog.Warn("failed to set auth cookies", "error", err)
	}
	if h.CFSigner != nil {
		for _, cookie := range h.CFSigner.SignedCookies(time.Now().Add(auth.AuthTokenTTL())) {
			http.SetCookie(w, cookie)
		}
	}
	h.auditUserAction(r, successAction, uuidToString(user.ID), "user", uuidToString(user.ID), audit.OutcomeSuccess, "")
	slog.Info("user logged in", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID))...)
	writeJSON(w, http.StatusOK, LoginResult{
		Token:                 token,
		User:                  userToResponse(user),
		MFAEnrollmentRequired: h.mfaStateFor(r.Context(), user).EnrollmentRequired,
	})
}

func (h *Handler) writeMFAChallenge(w http.ResponseWriter, r *http.Request, user db.User) {
	ticket, err := h.issueMFAPendingToken(user)
	if err != nil {
		slog.Error("mfa: failed to issue the pending ticket", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to continue sign-in")
		return
	}
	writeJSON(w, http.StatusOK, LoginResult{MFARequired: true, MFAToken: ticket})
}

const sessionTouchInterval = time.Minute

func (h *Handler) CheckSession(ctx context.Context, sessionID string) middleware.SessionVerdict {
	alive := middleware.SessionVerdict{Allowed: true}
	if h.Queries == nil || strings.TrimSpace(sessionID) == "" {
		return alive
	}
	id, err := parseUUIDLoose(sessionID)
	if err != nil {
		return alive
	}
	row, err := h.Queries.GetUserSession(ctx, id)
	if err != nil {
		return alive
	}
	if row.RevokedAt.Valid {
		return middleware.SessionVerdict{Reason: audit.ReasonSessionRevoked}
	}
	policy := h.SessionPolicy(ctx)
	now := time.Now()
	if now.Sub(row.CreatedAt.Time) > policy.AbsoluteLifetime() {
		return middleware.SessionVerdict{Reason: audit.ReasonSessionExpired}
	}
	if now.Sub(row.LastSeenAt.Time) > policy.IdleTimeout() {
		return middleware.SessionVerdict{Reason: audit.ReasonSessionIdle}
	}
	if now.Sub(row.LastSeenAt.Time) > sessionTouchInterval {
		if err := h.Queries.TouchUserSession(ctx, id); err != nil {
			slog.Warn("session: last_seen refresh failed", "error", err)
		}
	}
	return alive
}

type SessionResponse struct {
	ID         string `json:"id"`
	UserAgent  string `json:"user_agent"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`

	Current bool `json:"current"`
}

func (h *Handler) ListMySessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListUserSessions(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	current := currentSessionID(r)
	out := make([]SessionResponse, 0, len(rows))
	for _, row := range rows {
		id := uuidToString(row.ID)
		out = append(out, SessionResponse{
			ID:         id,
			UserAgent:  row.UserAgent,
			CreatedAt:  row.CreatedAt.Time.UTC().Format(time.RFC3339),
			LastSeenAt: row.LastSeenAt.Time.UTC().Format(time.RFC3339),
			Current:    id == current,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func currentSessionID(r *http.Request) string {
	tokenString, _ := extractRequestToken(r)
	if tokenString == "" {
		return ""
	}
	parsed, err := auth.ParseHS256(tokenString)
	if err != nil || !parsed.Valid {
		return ""
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}
	sid, _ := claims["sid"].(string)
	return sid
}

func extractRequestToken(r *http.Request) (string, bool) {
	if header := r.Header.Get("Authorization"); header != "" {
		if token := strings.TrimPrefix(header, "Bearer "); token != header {
			return token, false
		}
	}
	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, true
	}
	return "", false
}

func (h *Handler) RevokeMySession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	sessionUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "sessionId"), "sessionId")
	if !ok {
		return
	}
	affected, err := h.Queries.RevokeUserSession(r.Context(), db.RevokeUserSessionParams{
		ID: sessionUUID, UserID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke the session")
		return
	}
	if affected == 0 {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	h.auditUserAction(r, audit.ActionSessionRevoked, userID, "user_session", uuidToString(sessionUUID),
		audit.OutcomeSuccess, "")
	writeJSON(w, http.StatusOK, map[string]int{"revoked": int(affected)})
}

func (h *Handler) RevokeAllMySessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	h.revokeEverySession(w, r, parseUUID(userID), userID)
}

func (h *Handler) revokeEverySession(w http.ResponseWriter, r *http.Request, target pgtype.UUID, actorID string) {
	revoked, err := h.Queries.RevokeAllUserSessions(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke sessions")
		return
	}
	user, err := h.Queries.BumpUserTokenVersion(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke sessions")
		return
	}
	h.auditUserAction(r, audit.ActionSessionsRevoked, actorID, "user", uuidToString(target), audit.OutcomeSuccess, "")
	slog.Info("sessions revoked", append(logger.RequestAttrs(r),
		"user_id", uuidToString(target), "sessions", revoked, "token_version", user.TokenVersion)...)
	auth.ClearAuthCookies(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"revoked":       revoked,
		"token_version": user.TokenVersion,
	})
}

func (h *Handler) RevokeDeploymentUserSessions(w http.ResponseWriter, r *http.Request) {
	actorUUID, ok := h.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "userId"), "userId")
	if !ok {
		return
	}
	if _, err := h.Queries.GetUser(r.Context(), targetUUID); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	revoked, err := h.Queries.RevokeAllUserSessions(r.Context(), targetUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke sessions")
		return
	}
	user, err := h.Queries.BumpUserTokenVersion(r.Context(), targetUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke sessions")
		return
	}
	h.auditUserAction(r, audit.ActionSessionsRevoked, uuidToString(actorUUID), "user",
		uuidToString(targetUUID), audit.OutcomeSuccess, "")
	writeJSON(w, http.StatusOK, map[string]any{
		"revoked":       revoked,
		"token_version": user.TokenVersion,
	})
}
