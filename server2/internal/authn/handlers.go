package authn

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/mail"
)

const (
	codeTTL          = 10 * time.Minute
	codeResendWindow = 60 * time.Second
	linkPurpose      = "login_link"
)

type sendCodeRequest struct {
	Email string `json:"email"`
}

func (d *Deps) handleSendCode(w http.ResponseWriter, r *http.Request) {
	if !d.Config.AuthMethodEnabled("email") {
		httpapi.WriteError(w, http.StatusNotFound, "email sign-in is not enabled on this server", "email_not_enabled")
		return
	}
	ip := httpapi.ClientIP(r)
	if !d.AuthIPLimiter.Enforce(w, ip, "too many requests") {
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
	if !d.AuthEmailLimiter.Enforce(w, email, "too many requests for this email") {
		return
	}
	if !d.Config.EmailAllowed(email) {
		httpapi.WriteError(w, http.StatusForbidden, "this email is not allowed to sign in on this server", "email_not_allowed")
		return
	}

	// dev-режим: фиксированный код из GOOSAR_DEV_VERIFICATION_CODE, почта не
	// отправляется — verify-code сравнивает с ним напрямую (см.
	// handleVerifyCode), не читая эту строку. Код всё же сохраняется в
	// login_codes (тем же StoreCode, что и обычный путь) — T-027 доводка:
	// e2e-тесты фронтенда (e2e/fixtures.ts, вне server2) читают
	// подтверждающий код прямым SQL-запросом как запасной вариант, когда
	// GOOSAR_DEV_VERIFICATION_CODE не задан у клиента теста, и без строки в
	// БД эта проверка всегда возвращает "код не найден", даже когда сам логин
	// в остальном работает. Троттлинг "1 код в 60с на email"
	// (LastCodeSentAt) сюда не применяется: он защищает почтовый провайдер от
	// спама, а в dev-режиме письмо не отправляется вовсе, поэтому эта ветка —
	// раньше проверки повторной отправки, не после.
	if d.Config.DevCodeEnabled() {
		if err := d.Store.StoreCode(r.Context(), email, d.Config.DevVerifyCode, "login", codeTTL); err != nil {
			d.Logger.Warn("auth: сохранение dev-кода", "err", err)
		}
		d.Logger.Info("auth: dev-код входа", "email", email)
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"message": "code sent"})
		return
	}

	// Вне dev-режима реальная отправка обязательна — иначе код никогда не
	// доедет до пользователя. Контракт документирует ровно это как 503
	// "email delivery not configured on this instance" (см.
	// docs/50-api-contract.yaml, authSendCode).
	if !mail.Configured(d.Config) {
		httpapi.WriteError(w, http.StatusServiceUnavailable,
			"email delivery not configured on this instance", "email_delivery_unavailable")
		return
	}

	if last, ok, err := d.Store.LastCodeSentAt(r.Context(), email, "login"); err == nil && ok {
		if time.Since(last) < codeResendWindow {
			httpapi.TooManyRequests(w, "code requested too recently")
			return
		}
	}

	code, err := randomCode(6)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	linkToken, err := randomToken("", 20)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if err := d.Store.StoreCode(r.Context(), email, code, "login", codeTTL); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if err := d.Store.StoreCode(r.Context(), email, linkToken, linkPurpose, codeTTL); err != nil {
		d.Logger.Warn("auth: сохранение magic-link токена", "err", err)
	}

	locale := mail.LocaleFrom(d.Store.AccountLocale(r.Context(), email))
	msg := mail.LoginCodeMessage(email, locale, code, d.magicLinkURL(linkToken))
	if err := d.Mailer.Send(r.Context(), msg); err != nil {
		d.Logger.Error("auth: отправка кода", "err", err)
		httpapi.WriteError(w, http.StatusServiceUnavailable,
			"email delivery not configured on this instance", "email_delivery_unavailable")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"message": "code sent"})
}

// magicLinkURL строит ссылку на страницу фронтенда, которая заберёт token из
// query и вызовет POST /auth/verify-link (contract не фиксирует конкретный
// путь фронтенда — решение этой сессии, см. server2/docs/decisions.md,
// раздел T-029). GOOSAR_APP_URL (contract: "публичный адрес веб-приложения —
// используется в письмах") — пусто, если ни он, ни FRONTEND_ORIGIN не
// заданы — тогда письмо ограничивается кодом (см. mail.LoginCodeMessage).
func (d *Deps) magicLinkURL(token string) string {
	base := strings.TrimRight(d.Config.AppURL, "/")
	if base == "" {
		return ""
	}
	return base + "/login/verify?token=" + url.QueryEscape(token)
}

type verifyCodeRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func (d *Deps) handleVerifyCode(w http.ResponseWriter, r *http.Request) {
	ip := httpapi.ClientIP(r)
	if !d.AuthVerifyIPLimiter.Enforce(w, ip, "too many requests") {
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
	if !d.AuthEmailLimiter.Enforce(w, email, "too many requests for this email") {
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

	acct, err := d.signupOrFind(r, email, "")
	if err != nil {
		if errors.Is(err, errSignupDisabled) {
			httpapi.WriteError(w, http.StatusForbidden, "signup is disabled on this server", "signup_disabled")
			return
		}
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	d.completeLogin(w, r, acct)
}

var errSignupDisabled = errors.New("authn: регистрация отключена на этом сервере")

// signupOrFind — общая часть verify-code/verify-link/oidc/ldap после того,
// как identity подтверждена внешним фактором (код, magic-link, IdP,
// каталог): найти существующий аккаунт по email или завести новый, если
// ALLOW_SIGNUP=true (contract: "при первом входе создаёт пользователя,
// учитывая allow-list/домены" — allow-list/домены пока не реализованы, см.
// decisions.md). name — отображаемое имя из IdP/каталога для нового
// аккаунта; пусто — вывести его из локальной части email (displayNameFromEmail).
func (d *Deps) signupOrFind(r *http.Request, email, name string) (Account, error) {
	acct, err := d.Store.FindAccountByEmail(r.Context(), email)
	if errors.Is(err, ErrNotFound) {
		if !d.Config.AllowSignup {
			return Account{}, errSignupDisabled
		}
		if name == "" {
			name = displayNameFromEmail(email)
		}
		return d.Store.CreateAccount(r.Context(), email, name)
	}
	return acct, err
}

// issueSessionToken создаёт строку сессии, подписывает JWT и выставляет
// cookies — общая часть startSession (JSON-ответ) и finishExternalLogin
// (HTTP-редирект после OIDC), которым в остальном нужен один и тот же токен.
func (d *Deps) issueSessionToken(w http.ResponseWriter, r *http.Request, acct Account) (string, error) {
	sess, err := d.Store.CreateSession(r.Context(), acct.ID, r.UserAgent())
	if err != nil {
		return "", err
	}
	token, err := d.Signer.Sign(Claims{
		Sub: acct.ID, Email: acct.Email, Name: acct.Name, TV: acct.TokenEpoch, SID: sess.ID,
	}, d.Config.AuthTokenTTL)
	if err != nil {
		return "", err
	}
	csrf, err := randomToken("", 16)
	if err != nil {
		return "", err
	}
	d.setSessionCookies(w, token, csrf)
	return token, nil
}

// loginOrLinkExternal — общий шаг после того, как внешний провайдер (OIDC/LDAP)
// подтвердил identity: найти аккаунт, уже привязанный к (method, subject),
// иначе найти/завести по email и привязать (method, subject) к нему на
// будущее — второй вход того же пользователя пойдёт уже по subject, даже
// если email в IdP успеет измениться.
//
// В отличие от signupOrFind (email-коды/magic-link), здесь не проверяется
// ALLOW_SIGNUP: ни authLoginLdap, ни authOidcCallback не документируют 403
// "signup disabled" в docs/50-api-contract.yaml (только 401/404/503 и
// 302/404 соответственно) — решение этой сессии читает это как "ALLOW_SIGNUP
// охраняет только публичный вход по email от произвольной регистрации из
// интернета; успешный вход через корпоративный каталог/IdP сам по себе уже
// является предъявленным правом на аккаунт" (см. server2/docs/decisions.md,
// раздел T-029).
var errEmailNotAllowed = errors.New("authn: email не входит в ALLOWED_EMAILS/ALLOWED_EMAIL_DOMAINS")

func (d *Deps) loginOrLinkExternal(r *http.Request, method, subject, email, name string) (Account, error) {
	if !d.Config.EmailAllowed(email) {
		return Account{}, errEmailNotAllowed
	}
	ctx := r.Context()
	acct, err := d.Store.FindAccountByExternalSubject(ctx, method, subject)
	switch {
	case err == nil:
		return acct, nil
	case !errors.Is(err, ErrNotFound):
		return Account{}, err
	}
	acct, err = d.Store.FindAccountByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		if name == "" {
			name = displayNameFromEmail(email)
		}
		acct, err = d.Store.CreateAccount(ctx, email, name)
	}
	if err != nil {
		return Account{}, err
	}
	if err := d.Store.UpsertExternalBinding(ctx, acct.ID, method, subject, email); err != nil {
		return Account{}, err
	}
	return acct, nil
}

// startSession выпускает сессию + JWT для acct и пишет LoginResult.
func (d *Deps) startSession(w http.ResponseWriter, r *http.Request, acct Account) {
	token, err := d.issueSessionToken(w, r, acct)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
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

// completeLoginToken — ядро MFA-гейта, общее для JSON-ответа (completeLogin)
// и HTTP-редиректа (finishExternalLogin, OIDC): если у аккаунта включена
// MFA, возвращает mfa_token вместо сессионного (contract: "при необходимости
// требует MFA, иначе выдаёт сессию").
func (d *Deps) completeLoginToken(w http.ResponseWriter, r *http.Request, acct Account) (token string, mfaRequired bool, err error) {
	status, err := d.Store.MFAStatus(r.Context(), acct.ID)
	if err != nil {
		return "", false, err
	}
	if status.Enabled {
		token, err = d.Store.CreatePendingLogin(r.Context(), acct.ID, mfaPendingTTL)
		return token, true, err
	}
	token, err = d.issueSessionToken(w, r, acct)
	return token, false, err
}

// completeLogin — общий последний шаг верифицированных email-путей входа
// (код, magic-link): пишет LoginResult как JSON.
func (d *Deps) completeLogin(w http.ResponseWriter, r *http.Request, acct Account) {
	token, mfaRequired, err := d.completeLoginToken(w, r, acct)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if mfaRequired {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"mfa_required": true, "mfa_token": token})
		return
	}
	user, err := d.Store.GetUserView(r.Context(), acct.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}

type verifyLinkRequest struct {
	LinkToken string `json:"link_token"`
}

// handleVerifyLoginLink — magic-link вариант verify-code: тот же эффект
// (найти/завести аккаунт, пройти MFA-гейт, выдать сессию), но по токену из
// письма send-code вместо 6-значного кода (contract §3.3: "То же самое, но
// по magic-link токену вместо кода").
func (d *Deps) handleVerifyLoginLink(w http.ResponseWriter, r *http.Request) {
	ip := httpapi.ClientIP(r)
	if !d.AuthVerifyIPLimiter.Enforce(w, ip, "too many requests") {
		return
	}
	var req verifyLinkRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.LinkToken == "" {
		httpapi.BadRequest(w, "link_token is required")
		return
	}

	email, err := d.Store.ConsumeLinkToken(r.Context(), req.LinkToken, linkPurpose)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid or expired link", "invalid_or_expired_link")
		return
	}

	acct, err := d.signupOrFind(r, email, "")
	if err != nil {
		if errors.Is(err, errSignupDisabled) {
			httpapi.WriteError(w, http.StatusForbidden, "signup is disabled on this server", "signup_disabled")
			return
		}
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.completeLogin(w, r, acct)
}

func (d *Deps) handleLogout(w http.ResponseWriter, r *http.Request) {
	d.clearSessionCookies(w)
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (d *Deps) handleListMethods(w http.ResponseWriter, r *http.Request) {
	ip := httpapi.ClientIP(r)
	if !d.AuthVerifyIPLimiter.Enforce(w, ip, "too many requests") {
		return
	}
	methods := []string{}
	resp := map[string]any{}
	if d.Config.AuthMethodEnabled("email") {
		methods = append(methods, "email")
	}
	if d.Config.AuthMethodEnabled("oidc") && d.Config.OIDC.IssuerURL != "" {
		methods = append(methods, "oidc")
		resp["oidc_display_name"] = d.Config.OIDC.DisplayName
	}
	if d.Config.AuthMethodEnabled("ldap") && d.Config.LDAPMethodAvailable() {
		methods = append(methods, "ldap")
		resp["ldap_display_name"] = d.Config.LDAP.DisplayName
	}
	resp["methods"] = methods
	httpapi.WriteJSON(w, http.StatusOK, resp)
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
