// MFA enroll/confirm/disable/recovery-codes и завершение входа вторым
// фактором (POST /api/auth/mfa/verify) — T-029. Статус (GET /api/auth/mfa),
// список/отзыв сессий — handlers.go (T-026, не менялись по существу).
package authn

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// mfaPendingTTL — срок жизни mfa_token (LoginResult.mfa_token), тот же, что
// и окно RATE_LIMIT_MFA_VERIFY (contract §1.5: "10 / 5 минут (TTL
// «pending»-токена)" — довольно явно завязывает одно на другое).
const mfaPendingTTL = 5 * time.Minute

const recoveryCodeCount = 10

func (d *Deps) handleEnrollTotp(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	if !d.TokenOpsLimiter.Enforce(w, actor.UserID, "too many requests") {
		return
	}
	if d.Config.McpSecretKey == "" {
		httpapi.WriteError(w, http.StatusServiceUnavailable,
			"GOOSAR_MCP_SECRET_KEY not configured — MFA storage unavailable", "mfa_unavailable")
		return
	}

	factor, found, err := d.Store.GetFactor(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if found && factor.EnabledAt != nil {
		httpapi.WriteError(w, http.StatusConflict, "a second factor is already active", "mfa_already_enabled")
		return
	}

	secret, err := newTOTPSecret()
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	sealed, err := sealSecret(d.Config.McpSecretKey, secret)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if err := d.Store.UpsertPendingFactor(r.Context(), actor.UserID, sealed); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"secret":         base32Std.EncodeToString(secret),
		"otpauth_uri":    otpauthURI(d.Config.TotpIssuer, actor.Email, secret),
		"issuer":         d.Config.TotpIssuer,
		"account":        actor.Email,
		"digits":         totpDigits,
		"period_seconds": int(totpPeriod.Seconds()),
		"algorithm":      "SHA1",
	})
}

type mfaCodeRequest struct {
	Code string `json:"code"`
}

func (d *Deps) handleConfirmTotp(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	if !d.TokenOpsLimiter.Enforce(w, actor.UserID, "too many requests") {
		return
	}
	var req mfaCodeRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.Code == "" {
		httpapi.BadRequest(w, "code is required")
		return
	}

	factor, found, err := d.Store.GetFactor(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.WriteError(w, http.StatusBadRequest, "not enrolled yet", "mfa_not_enrolled")
		return
	}
	if factor.EnabledAt != nil {
		httpapi.WriteError(w, http.StatusConflict, "already enrolled", "mfa_already_enabled")
		return
	}
	secret, err := unsealSecret(d.Config.McpSecretKey, d.Config.McpSecretKeyPrevious, factor.SealedSecret)
	if err != nil || !validateTOTP(secret, req.Code) {
		httpapi.WriteError(w, http.StatusBadRequest, "code invalid", "mfa_code_invalid")
		return
	}

	codes, digests, err := generateRecoveryCodes()
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if err := d.Store.ConfirmFactor(r.Context(), actor.UserID, digests); err != nil {
		if errors.Is(err, ErrNotFound) {
			httpapi.WriteError(w, http.StatusConflict, "already enrolled", "mfa_already_enabled")
			return
		}
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true, "recovery_codes": codes})
}

func (d *Deps) handleDisableTotp(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	if !d.TokenOpsLimiter.Enforce(w, actor.UserID, "too many requests") {
		return
	}
	var req mfaCodeRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.Code == "" {
		httpapi.BadRequest(w, "code is required")
		return
	}

	ok2, err := d.verifyCurrentFactor(r.Context(), actor.UserID, req.Code)
	if errors.Is(err, errMFANotEnrolled) {
		httpapi.WriteError(w, http.StatusBadRequest, "not enrolled", "mfa_not_enrolled")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !ok2 {
		httpapi.WriteError(w, http.StatusBadRequest, "code invalid", "mfa_code_invalid")
		return
	}

	removed, err := d.Store.DisableFactor(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !removed {
		httpapi.WriteError(w, http.StatusBadRequest, "not enrolled", "mfa_not_enrolled")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"enabled": false})
}

func (d *Deps) handleRegenerateMfaRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	if !d.TokenOpsLimiter.Enforce(w, actor.UserID, "too many requests") {
		return
	}
	var req mfaCodeRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.Code == "" {
		httpapi.BadRequest(w, "code is required")
		return
	}

	ok2, err := d.verifyCurrentFactor(r.Context(), actor.UserID, req.Code)
	if errors.Is(err, errMFANotEnrolled) {
		httpapi.WriteError(w, http.StatusBadRequest, "not enrolled", "mfa_not_enrolled")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !ok2 {
		httpapi.WriteError(w, http.StatusBadRequest, "code invalid", "mfa_code_invalid")
		return
	}

	codes, digests, err := generateRecoveryCodes()
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if err := d.Store.RegenerateRecoveryCodes(r.Context(), actor.UserID, digests); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true, "recovery_codes": codes})
}

type mfaVerifyRequest struct {
	MfaToken     string `json:"mfa_token"`
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}

// handleVerifyMfaChallenge — POST /api/auth/mfa/verify: обменивает mfa_token
// на сессию (contract: "Exchanges the short-lived mfa_token ... for a real
// session"). Для этой операции контракт документирует только 401 (нет 400),
// поэтому любой некорректный ввод, включая отсутствующий mfa_token, отвечает
// 401, а не 400.
func (d *Deps) handleVerifyMfaChallenge(w http.ResponseWriter, r *http.Request) {
	ip := httpapi.ClientIP(r)
	if !d.AuthVerifyIPLimiter.Enforce(w, ip, "too many requests") {
		return
	}
	var req mfaVerifyRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.MfaToken == "" {
		httpapi.WriteError(w, http.StatusUnauthorized, "mfa_token is required", "mfa_token_invalid")
		return
	}

	tokenKey := digest(req.MfaToken)
	if !d.MfaVerifyTokenLimiter.Enforce(w, tokenKey, "too many requests") {
		return
	}

	accountID, err := d.Store.FindPendingLogin(r.Context(), req.MfaToken)
	if err != nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "mfa_token expired or invalid", "mfa_token_invalid")
		return
	}

	ok := false
	switch {
	case req.Code != "":
		factor, found, ferr := d.Store.GetFactor(r.Context(), accountID)
		if ferr != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if found && factor.EnabledAt != nil {
			if secret, serr := unsealSecret(d.Config.McpSecretKey, d.Config.McpSecretKeyPrevious, factor.SealedSecret); serr == nil {
				ok = validateTOTP(secret, req.Code)
			}
		}
	case req.RecoveryCode != "":
		ok, err = d.Store.ConsumeRecoveryCode(r.Context(), accountID, req.RecoveryCode)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
	}
	if !ok {
		httpapi.WriteError(w, http.StatusUnauthorized, "code or recovery code invalid", "mfa_code_invalid")
		return
	}

	if err := d.Store.ConsumePendingLogin(r.Context(), req.MfaToken); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	acct, err := d.Store.FindAccountByID(r.Context(), accountID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.startSession(w, r, acct)
}

var errMFANotEnrolled = errors.New("authn: mfa: не включён")

// verifyCurrentFactor — общая проверка "текущий код или recovery-код" для
// disable/regenerate-recovery-codes (contract: "Требует текущий код/
// recovery-код"): TOTP-поле схемы MFACodeRequest одно (code), поэтому оно
// проверяется сначала как TOTP, а при неудаче — как recovery-код (решение
// этой сессии, см. server2/docs/decisions.md, раздел T-029 — схема не заводит
// отдельного recovery_code поля для этих двух ручек, в отличие от
// MFAVerifyRequest).
func (d *Deps) verifyCurrentFactor(ctx context.Context, accountID, code string) (bool, error) {
	factor, found, err := d.Store.GetFactor(ctx, accountID)
	if err != nil {
		return false, err
	}
	if !found || factor.EnabledAt == nil {
		return false, errMFANotEnrolled
	}
	if secret, serr := unsealSecret(d.Config.McpSecretKey, d.Config.McpSecretKeyPrevious, factor.SealedSecret); serr == nil {
		if validateTOTP(secret, code) {
			return true, nil
		}
	}
	return d.Store.ConsumeRecoveryCode(ctx, accountID, code)
}

// generateRecoveryCodes выпускает recoveryCodeCount кодов и параллельный
// список их отпечатков (то, что реально уходит в БД — см. store_mfa.go).
func generateRecoveryCodes() (codes []string, digests []string, err error) {
	codes, err = newRecoveryCodes(recoveryCodeCount)
	if err != nil {
		return nil, nil, err
	}
	digests = make([]string, len(codes))
	for i, c := range codes {
		digests[i] = digest(c)
	}
	return codes, digests, nil
}
