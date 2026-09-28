package authn

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/mail"
)

const (
	codeTTL          = 10 * time.Minute
	codeResendWindow = 60 * time.Second
)

type sendCodeRequest struct {
	Email string `json:"email"`
}

func (d *Deps) handleSendCode(w http.ResponseWriter, r *http.Request) {
	if !d.SendCodeIPLimiter.Allow(httpapi.ClientIP(r)) {
		httpapi.TooManyRequests(w, "too many requests")
		return
	}
	var req sendCodeRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	email := normalizeEmail(req.Email)
	if !looksLikeEmail(email) {
		httpapi.BadRequest(w, "email is required")
		return
	}
	if !d.SendCodeEmailLimiter.Allow(email) {
		httpapi.TooManyRequests(w, "too many requests for this email")
		return
	}

	if last, ok, err := d.Store.LastCodeSentAt(r.Context(), email, "login"); err == nil && ok {
		if time.Since(last) < codeResendWindow {
			httpapi.TooManyRequests(w, "code requested too recently")
			return
		}
	}

	// dev-режим: фиксированный код из GOOSAR_DEV_VERIFICATION_CODE, почта не
	// отправляется и код в БД не сохраняется — verify-code сравнивает с ним
	// напрямую (см. handleVerifyCode).
	if d.Config.DevCodeEnabled() {
		d.Logger.Info("auth: dev-код входа", "email", email)
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"message": "code sent"})
		return
	}

	code, err := randomCode(6)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if err := d.Store.StoreCode(r.Context(), email, code, "login", codeTTL); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if err := d.Mailer.Send(r.Context(), mail.Message{
		To:      email,
		Subject: "Ваш код входа в Goosar",
		Body:    "Код: " + code,
	}); err != nil {
		d.Logger.Error("auth: отправка кода", "err", err)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"message": "code sent"})
}

type verifyCodeRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func (d *Deps) handleVerifyCode(w http.ResponseWriter, r *http.Request) {
	if !d.VerifyIPLimiter.Allow(httpapi.ClientIP(r)) {
		httpapi.TooManyRequests(w, "too many requests")
		return
	}
	var req verifyCodeRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	email := normalizeEmail(req.Email)
	if !looksLikeEmail(email) || req.Code == "" {
		httpapi.BadRequest(w, "email and code are required")
		return
	}

	valid := false
	if d.Config.DevCodeEnabled() && constantTimeEqual(req.Code, d.Config.DevVerifyCode) {
		valid = true
	} else if err := d.Store.ConsumeCode(r.Context(), email, req.Code, "login"); err == nil {
		valid = true
	}
	if !valid {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid or expired code", "invalid_or_expired_code")
		return
	}

	acct, err := d.Store.FindAccountByEmail(r.Context(), email)
	if errors.Is(err, ErrNotFound) {
		if !d.Config.AllowSignup {
			httpapi.WriteError(w, http.StatusForbidden, "signup is disabled on this server", "signup_disabled")
			return
		}
		acct, err = d.Store.CreateAccount(r.Context(), email, displayNameFromEmail(email))
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	d.startSession(w, r, acct)
}

// startSession выпускает сессию + JWT для acct и пишет LoginResult.
func (d *Deps) startSession(w http.ResponseWriter, r *http.Request, acct Account) {
	sess, err := d.Store.CreateSession(r.Context(), acct.ID, r.UserAgent())
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	token, err := d.Signer.Sign(Claims{
		Sub: acct.ID, Email: acct.Email, Name: acct.Name, TV: acct.TokenEpoch, SID: sess.ID,
	}, sessionTTL)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	csrf, err := randomToken("", 16)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.setSessionCookies(w, token, csrf)

	user, err := d.Store.GetUserView(r.Context(), acct.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  user,
	})
}

func (d *Deps) handleVerifyLoginLink(w http.ResponseWriter, r *http.Request) {
	// Отдельный от кода поток magic-link. Оставлено как 501 (см.
	// server2/docs/decisions.md, «Пробелы спецификации») — не задействовано
	// контрактными тестами auth/workspaces/me T-026.
	httpapi.WriteNotImplemented(w, r)
}

func (d *Deps) handleLogout(w http.ResponseWriter, r *http.Request) {
	d.clearSessionCookies(w)
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (d *Deps) handleListMethods(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"methods": []string{"email"},
	})
}

func (d *Deps) handleGetMfaStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	st, err := d.Store.MFAStatus(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":                  st.Enabled,
		"pending_enrollment":       st.PendingEnrollment,
		"enabled_at":               st.EnabledAt,
		"recovery_codes_remaining": st.RecoveryCodesRemaining,
		"required":                 false,
		"available":                d.Config.McpSecretKey != "",
	})
}

func (d *Deps) handleListSessions(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	sessions, err := d.Store.ListSessions(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, map[string]any{
			"id":           s.ID,
			"user_agent":   s.UserAgent,
			"created_at":   s.CreatedAt,
			"last_seen_at": s.LastPingAt,
			"current":      s.ID == actor.SessionID,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	sessionID := r.PathValue("sessionId")
	n, err := d.Store.RevokeSession(r.Context(), sessionID, actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if n == 0 {
		httpapi.NotFound(w, "session not found")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"revoked": n})
}

func (d *Deps) handleRevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	n, err := d.Store.RevokeAllSessions(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	epoch, err := d.Store.BumpTokenEpoch(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.clearSessionCookies(w)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"revoked": n, "token_version": epoch})
}

func (d *Deps) notImplemented(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteNotImplemented(w, r)
}

// --- helpers -----------------------------------------------------------------

func normalizeEmail(e string) string { return strings.TrimSpace(strings.ToLower(e)) }

func looksLikeEmail(e string) bool {
	at := strings.IndexByte(e, '@')
	return at > 0 && at < len(e)-1
}

func displayNameFromEmail(email string) string {
	local, _, _ := strings.Cut(email, "@")
	if local == "" {
		return "New user"
	}
	return strings.ToUpper(local[:1]) + local[1:]
}
