package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func uuidToString(u pgtype.UUID) string { return util.UUIDToString(u) }

type SessionChecker interface {
	CheckSession(ctx context.Context, sessionID string) SessionVerdict
}

type SessionVerdict struct {
	Allowed bool

	Reason string
}

func Auth(queries *db.Queries, patCache *auth.PATCache, cloudPAT *auth.CloudPATVerifier, sessions SessionChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			r.Header.Del("X-Actor-Source")

			tokenString, fromCookie := extractToken(r)
			if tokenString == "" {
				slog.Debug("auth: no token found", "path", r.URL.Path)
				http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
				return
			}

			if fromCookie && !auth.ValidateCSRF(r) {
				slog.Debug("auth: CSRF validation failed", "path", r.URL.Path)
				http.Error(w, `{"error":"CSRF validation failed"}`, http.StatusForbidden)
				return
			}

			if strings.HasPrefix(tokenString, "mat_") {
				if queries == nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				hash := auth.HashToken(tokenString)
				tt, err := queries.GetTaskTokenByHash(r.Context(), hash)
				if err != nil {
					slog.Warn("auth: invalid task token", "path", r.URL.Path, "error", err)
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}

				if !offboardingAllows(w, r, queries, uuidToString(tt.UserID), nil) {
					return
				}
				r.Header.Set("X-User-ID", uuidToString(tt.UserID))
				r.Header.Set("X-Agent-ID", uuidToString(tt.AgentID))
				r.Header.Set("X-Task-ID", uuidToString(tt.TaskID))
				r.Header.Set("X-Workspace-ID", uuidToString(tt.WorkspaceID))

				r.Header.Set("X-Actor-Source", "task_token")
				next.ServeHTTP(w, r)
				return
			}

			if strings.HasPrefix(tokenString, auth.CloudPATPrefix) {
				if cloudPAT == nil {
					slog.Warn("auth: gsln_ token presented but cloud verifier not configured", "path", r.URL.Path)
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				identity, err := cloudPAT.Verify(r.Context(), tokenString, ownerLookupFor(queries))
				if err != nil {
					if errors.Is(err, auth.ErrCloudPATInvalid) {
						slog.Warn("auth: cloud rejected gsln_ token", "path", r.URL.Path, "error", err)
						http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
						return
					}

					slog.Warn("auth: cloud pat verify unavailable", "path", r.URL.Path, "error", err)
					http.Error(w, `{"error":"cloud pat verifier unavailable"}`, http.StatusServiceUnavailable)
					return
				}
				if !offboardingAllows(w, r, queries, identity.OwnerID, nil) {
					return
				}
				r.Header.Set("X-User-ID", identity.OwnerID)

				r.Header.Set("X-Actor-Source", "cloud_pat")
				next.ServeHTTP(w, r)
				return
			}

			if strings.HasPrefix(tokenString, auth.PATPrefix) {
				hash := auth.HashToken(tokenString)

				if userID, ok := patCache.Get(r.Context(), hash); ok {
					if !offboardingAllows(w, r, queries, userID, nil) {
						return
					}
					r.Header.Set("X-User-ID", userID)
					next.ServeHTTP(w, r)
					return
				}

				if queries == nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				pat, err := queries.GetPersonalAccessTokenByHash(r.Context(), hash)
				if err != nil {
					slog.Warn("auth: invalid PAT", "path", r.URL.Path, "error", err)
					auditRefusal(r, queries, "", audit.ReasonInvalidToken)
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}

				userID := uuidToString(pat.UserID)
				if !offboardingAllows(w, r, queries, userID, nil) {
					return
				}
				r.Header.Set("X-User-ID", userID)

				var expiresAt time.Time
				if pat.ExpiresAt.Valid {
					expiresAt = pat.ExpiresAt.Time
				}
				patCache.Set(r.Context(), hash, userID, auth.TTLForExpiry(time.Now(), expiresAt))

				if !pat.LastUsedAt.Valid {
					auditRecorder(queries).Record(r.Context(), auditEventFor(r, audit.Event{
						Action:     audit.ActionPATFirstUse,
						ActorType:  audit.ActorUser,
						ActorID:    userID,
						TargetType: "personal_access_token",
						TargetID:   uuidToString(pat.ID),
						Outcome:    audit.OutcomeSuccess,
					}))
				}

				go queries.UpdatePersonalAccessTokenLastUsed(context.Background(), pat.ID)

				next.ServeHTTP(w, r)
				return
			}

			token, err := auth.ParseHS256(tokenString)
			if err != nil || !token.Valid {
				slog.Warn("auth: invalid token", "path", r.URL.Path, "error", err)
				auditRefusal(r, queries, "", audit.ReasonInvalidToken)
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				slog.Warn("auth: invalid claims", "path", r.URL.Path)
				http.Error(w, `{"error":"invalid claims"}`, http.StatusUnauthorized)
				return
			}

			sub, ok := claims["sub"].(string)
			if !ok || strings.TrimSpace(sub) == "" {
				slog.Warn("auth: invalid claims", "path", r.URL.Path)
				http.Error(w, `{"error":"invalid claims"}`, http.StatusUnauthorized)
				return
			}

			if typ, _ := claims["typ"].(string); typ != "" {
				slog.Warn("auth: non-session token presented as a session", "path", r.URL.Path, "typ", typ)
				auditRefusal(r, queries, sub, audit.ReasonInvalidToken)
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}
			claimedVersion := int32(0)
			if raw, ok := claims["tv"].(float64); ok {
				claimedVersion = int32(raw)
			}
			if !offboardingAllows(w, r, queries, sub, &claimedVersion) {
				return
			}

			if sessions != nil {
				sid, _ := claims["sid"].(string)
				if sid != "" {
					if verdict := sessions.CheckSession(r.Context(), sid); !verdict.Allowed {
						slog.Warn("auth: session ended by policy", "path", r.URL.Path, "reason", verdict.Reason)
						auditRefusal(r, queries, sub, verdict.Reason)
						http.Error(w, `{"error":"session expired","code":"session_expired"}`, http.StatusUnauthorized)
						return
					}
				}
			}
			r.Header.Set("X-User-ID", sub)
			if email, ok := claims["email"].(string); ok {
				r.Header.Set("X-User-Email", email)
			}

			next.ServeHTTP(w, r)
		})
	}
}

func offboardingAllows(w http.ResponseWriter, r *http.Request, queries *db.Queries, userID string, claimedVersion *int32) bool {
	if queries == nil || strings.TrimSpace(userID) == "" {
		return true
	}
	id, err := util.ParseUUID(userID)
	if err != nil {
		return true
	}
	state, err := queries.GetUserAuthState(r.Context(), id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):

		denyOffboarded(w, r, queries, userID, audit.ReasonDeactivated)
		return false
	case err != nil:

		slog.Error("auth: offboarding check unavailable", "error", err, "path", r.URL.Path)
		http.Error(w, `{"error":"service unavailable","code":"auth_check_unavailable"}`, http.StatusServiceUnavailable)
		return false
	}
	if state.DeactivatedAt.Valid {
		denyOffboarded(w, r, queries, userID, audit.ReasonDeactivated)
		return false
	}
	if claimedVersion != nil && *claimedVersion != state.TokenVersion {
		denyOffboarded(w, r, queries, userID, audit.ReasonSessionRevoked)
		return false
	}
	return true
}

func denyOffboarded(w http.ResponseWriter, r *http.Request, queries *db.Queries, userID, reason string) {
	slog.Warn("auth: credential refused — account deactivated or session revoked", "path", r.URL.Path)
	auditRefusal(r, queries, userID, reason)
	http.Error(w, `{"error":"session revoked","code":"session_revoked"}`, http.StatusUnauthorized)
}

func extractToken(r *http.Request) (token string, fromCookie bool) {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString != authHeader {
			return tokenString, false
		}
	}

	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, true
	}

	return "", false
}
