// Второй фактор: подключение TOTP, вызов при входе и коды восстановления.
// Вход двухшаговый: после первого фактора сервер либо выдаёт сессию, либо
// возвращает mfa_token, который обменивается на сессию через /api/auth/mfa/verify.
package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/auth/totp"
	"github.com/adanman/goosar/server/internal/corpauth"
	"github.com/adanman/goosar/server/internal/logger"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	MFAPendingTTL = 5 * time.Minute

	MFATokenType = "mfa"

	recoveryCodeCount = 10
	recoveryCodeBytes = 10

	totpIssuerEnv     = "GOOSAR_TOTP_ISSUER"
	defaultTOTPIssuer = "Goosar"
)

var recoveryCodeAlphabet = base32.StdEncoding.WithPadding(base32.NoPadding)

func (h *Handler) sealTOTPSecret(secret []byte) ([]byte, error) {
	if h.MCPSecretBox == nil {
		return nil, errMFAKeyUnavailable
	}
	return h.MCPSecretBox.Seal(secret)
}

func (h *Handler) openTOTPSecret(sealed []byte) ([]byte, error) {
	if h.MCPSecretBox == nil {
		return nil, errMFAKeyUnavailable
	}
	return h.MCPSecretBox.Open(sealed)
}

var errMFAKeyUnavailable = errors.New("GOOSAR_MCP_SECRET_KEY is not configured; the second factor cannot be stored or read")

func totpIssuer() string {
	if v := strings.TrimSpace(os.Getenv(totpIssuerEnv)); v != "" {
		return v
	}
	return defaultTOTPIssuer
}

func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}

func normalizeRecoveryCode(code string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code)))
}

func generateRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		buf := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("mfa: read random: %w", err)
		}
		codes = append(codes, recoveryCodeAlphabet.EncodeToString(buf))
	}
	return codes, nil
}

func (h *Handler) replaceRecoveryCodes(ctx context.Context, userID pgtype.UUID) ([]string, error) {
	codes, err := generateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if err := h.Queries.DeleteUserMFARecoveryCodes(ctx, userID); err != nil {
		return nil, err
	}
	for _, code := range codes {
		if err := h.Queries.InsertUserMFARecoveryCode(ctx, db.InsertUserMFARecoveryCodeParams{
			UserID:   userID,
			CodeHash: hashRecoveryCode(code),
		}); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

func (h *Handler) issueMFAPendingToken(user db.User) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": uuidToString(user.ID),
		"typ": MFATokenType,
		"tv":  user.TokenVersion,
		"exp": time.Now().Add(MFAPendingTTL).Unix(),
		"iat": time.Now().Unix(),
	})
	return token.SignedString(auth.JWTSecret())
}

func (h *Handler) parseMFAPendingToken(ctx context.Context, tokenString string) (db.User, error) {
	parsed, err := auth.ParseHS256(strings.TrimSpace(tokenString))
	if err != nil || !parsed.Valid {
		return db.User{}, errMFAPendingInvalid
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return db.User{}, errMFAPendingInvalid
	}
	if typ, _ := claims["typ"].(string); typ != MFATokenType {

		return db.User{}, errMFAPendingInvalid
	}
	sub, _ := claims["sub"].(string)
	id, err := util.ParseUUID(sub)
	if err != nil {
		return db.User{}, errMFAPendingInvalid
	}
	user, err := h.Queries.GetUser(ctx, id)
	if err != nil {
		return db.User{}, errMFAPendingInvalid
	}
	if user.DeactivatedAt.Valid {
		return db.User{}, errMFAPendingInvalid
	}

	if raw, ok := claims["tv"].(float64); !ok || int32(raw) != user.TokenVersion {
		return db.User{}, errMFAPendingInvalid
	}
	return user, nil
}

var errMFAPendingInvalid = errors.New("mfa: the pending sign-in has expired or is not valid")

type mfaState struct {
	Enrolled bool

	EnrollmentRequired bool
}

func (h *Handler) mfaStateFor(ctx context.Context, user db.User) mfaState {
	var st mfaState
	row, err := h.Queries.GetUserMFA(ctx, user.ID)
	if err == nil && row.EnabledAt.Valid {
		st.Enrolled = true
	}
	if st.Enrolled {
		return st
	}
	policy := h.SessionPolicy(ctx)
	if policy.RequireMFA == RequireMFANone {
		return st
	}
	isAdmin := false
	if _, err := h.Queries.GetDeploymentAdmin(ctx, user.ID); err == nil {
		isAdmin = true
	}
	st.EnrollmentRequired = policy.MFARequiredFor(isAdmin)
	return st
}

func (h *Handler) mfaChallengeRequired(ctx context.Context, user db.User, method string) bool {
	if !h.mfaStateFor(ctx, user).Enrolled {
		return false
	}
	if method == corpauth.MethodOIDC {

		return h.SessionPolicy(ctx).RequireMFA == RequireMFAAll
	}
	return true
}

type MFAStatusResponse struct {
	Enabled bool `json:"enabled"`

	PendingEnrollment bool    `json:"pending_enrollment"`
	EnabledAt         *string `json:"enabled_at"`

	RecoveryCodesRemaining int `json:"recovery_codes_remaining"`

	Required bool `json:"required"`

	Available bool `json:"available"`
}

func (h *Handler) GetMFAStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id := parseUUID(userID)
	resp := MFAStatusResponse{Available: h.MCPSecretBox != nil}
	row, err := h.Queries.GetUserMFA(r.Context(), id)
	if err == nil {
		resp.Enabled = row.EnabledAt.Valid
		resp.PendingEnrollment = !row.EnabledAt.Valid
		if row.EnabledAt.Valid {
			at := row.EnabledAt.Time.UTC().Format(time.RFC3339)
			resp.EnabledAt = &at
		}
	} else if !isNotFound(err) {
		writeError(w, http.StatusInternalServerError, "failed to load second-factor state")
		return
	}
	if count, err := h.Queries.CountUserMFARecoveryCodesUnused(r.Context(), id); err == nil {
		resp.RecoveryCodesRemaining = int(count)
	}
	if user, err := h.Queries.GetUser(r.Context(), id); err == nil {
		resp.Required = h.mfaStateFor(r.Context(), user).EnrollmentRequired
	}
	writeJSON(w, http.StatusOK, resp)
}

type MFAEnrollResponse struct {
	Secret string `json:"secret"`

	OtpauthURI string `json:"otpauth_uri"`
	Issuer     string `json:"issuer"`
	Account    string `json:"account"`
	Digits     int    `json:"digits"`
	PeriodSecs int    `json:"period_seconds"`
	Algorithm  string `json:"algorithm"`
}

func (h *Handler) EnrollTOTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id := parseUUID(userID)
	user, err := h.Queries.GetUser(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if existing, err := h.Queries.GetUserMFA(r.Context(), id); err == nil && existing.EnabledAt.Valid {
		writeErrorCode(w, http.StatusConflict, ErrCodeMFAAlreadyEnrolled,
			"a second factor is already active; disable it with a current code before enrolling a new one")
		return
	} else if err != nil && !isNotFound(err) {
		writeError(w, http.StatusInternalServerError, "failed to load second-factor state")
		return
	}

	secret, err := totp.GenerateSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate a secret")
		return
	}
	sealed, err := h.sealTOTPSecret(secret)
	if err != nil {
		h.writeMFAUnavailable(w, r, userID, err)
		return
	}
	if _, err := h.Queries.UpsertUserMFASecret(r.Context(), db.UpsertUserMFASecretParams{
		UserID:           id,
		TotpSecretSealed: sealed,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store the enrollment")
		return
	}

	h.auditUserAction(r, audit.ActionMFAEnrolled, userID, "user", userID, audit.OutcomeSuccess, "")
	writeJSON(w, http.StatusOK, MFAEnrollResponse{
		Secret:     totp.EncodeSecret(secret),
		OtpauthURI: totp.URI(totpIssuer(), user.Email, secret),
		Issuer:     totpIssuer(),
		Account:    user.Email,
		Digits:     totp.Digits,
		PeriodSecs: int(totp.Step / time.Second),
		Algorithm:  "SHA1",
	})
}

type MFACodeRequest struct {
	Code string `json:"code"`
}

type MFAConfirmResponse struct {
	Enabled       bool     `json:"enabled"`
	RecoveryCodes []string `json:"recovery_codes"`
}

func (h *Handler) ConfirmTOTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req MFACodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := parseUUID(userID)
	row, err := h.Queries.GetUserMFA(r.Context(), id)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeMFANotEnrolled,
			"start an enrollment before confirming it")
		return
	}
	if row.EnabledAt.Valid {
		writeErrorCode(w, http.StatusConflict, ErrCodeMFAAlreadyEnrolled, "the second factor is already active")
		return
	}
	secret, err := h.openTOTPSecret(row.TotpSecretSealed)
	if err != nil {
		h.writeMFAUnavailable(w, r, userID, err)
		return
	}
	step, valid := totp.Validate(secret, req.Code, time.Now())
	if !valid {
		h.auditUserAction(r, audit.ActionMFAFailed, userID, "user", userID, audit.OutcomeFailure, audit.ReasonMFACodeInvalid)
		writeErrorCode(w, http.StatusBadRequest, ErrCodeMFAInvalidCode, "that code is not valid")
		return
	}

	affected, err := h.Queries.ConfirmUserMFA(r.Context(), db.ConfirmUserMFAParams{
		UserID: id, LastUsedStep: step,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to activate the second factor")
		return
	}
	if affected == 0 {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeMFACodeReplayed, "that code has already been used; wait for the next one")
		return
	}
	codes, err := h.replaceRecoveryCodes(r.Context(), id)
	if err != nil {
		slog.Error("mfa: failed to mint recovery codes", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to generate recovery codes")
		return
	}
	h.auditUserAction(r, audit.ActionMFAConfirmed, userID, "user", userID, audit.OutcomeSuccess, "")
	h.auditUserAction(r, audit.ActionMFARecoveryIssued, userID, "user", userID, audit.OutcomeSuccess, "")
	writeJSON(w, http.StatusOK, MFAConfirmResponse{Enabled: true, RecoveryCodes: codes})
}

func (h *Handler) DisableTOTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req MFACodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := parseUUID(userID)
	row, err := h.Queries.GetUserMFA(r.Context(), id)
	if err != nil || !row.EnabledAt.Valid {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeMFANotEnrolled, "no second factor is active")
		return
	}
	if !h.consumeSecondFactor(r, id, row, req.Code, userID) {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeMFAInvalidCode, "that code is not valid")
		return
	}
	if err := h.Queries.DeleteUserMFA(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable the second factor")
		return
	}
	if err := h.Queries.DeleteUserMFARecoveryCodes(r.Context(), id); err != nil {
		slog.Warn("mfa: recovery codes outlived a disabled factor", append(logger.RequestAttrs(r), "error", err)...)
	}
	h.auditUserAction(r, audit.ActionMFADisabled, userID, "user", userID, audit.OutcomeSuccess, "")
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
}

func (h *Handler) RegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req MFACodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := parseUUID(userID)
	row, err := h.Queries.GetUserMFA(r.Context(), id)
	if err != nil || !row.EnabledAt.Valid {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeMFANotEnrolled, "no second factor is active")
		return
	}
	if !h.consumeSecondFactor(r, id, row, req.Code, userID) {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeMFAInvalidCode, "that code is not valid")
		return
	}
	codes, err := h.replaceRecoveryCodes(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate recovery codes")
		return
	}
	h.auditUserAction(r, audit.ActionMFARecoveryIssued, userID, "user", userID, audit.OutcomeSuccess, "")
	writeJSON(w, http.StatusOK, MFAConfirmResponse{Enabled: true, RecoveryCodes: codes})
}

func (h *Handler) consumeSecondFactor(r *http.Request, id pgtype.UUID, row db.UserMfa, code, userID string) bool {
	secret, err := h.openTOTPSecret(row.TotpSecretSealed)
	if err != nil {
		slog.Error("mfa: cannot open the stored secret", append(logger.RequestAttrs(r), "error", err)...)
		return false
	}
	step, valid := totp.Validate(secret, code, time.Now())
	if !valid {
		h.auditUserAction(r, audit.ActionMFAFailed, userID, "user", userID, audit.OutcomeFailure, audit.ReasonMFACodeInvalid)
		return false
	}
	affected, err := h.Queries.ClaimUserMFAStep(r.Context(), db.ClaimUserMFAStepParams{
		UserID: id, LastUsedStep: step,
	})
	if err != nil || affected == 0 {
		h.auditUserAction(r, audit.ActionMFAFailed, userID, "user", userID, audit.OutcomeFailure, audit.ReasonMFACodeReplayed)
		return false
	}
	return true
}

func (h *Handler) writeMFAUnavailable(w http.ResponseWriter, r *http.Request, userID string, err error) {
	slog.Error("mfa: unavailable", append(logger.RequestAttrs(r), "error", err, "user_id", userID)...)
	writeErrorCode(w, http.StatusServiceUnavailable, ErrCodeMFAUnavailable,
		"the second factor cannot be stored on this server: GOOSAR_MCP_SECRET_KEY is not configured")
}

type MFAVerifyRequest struct {
	MFAToken     string `json:"mfa_token"`
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}

func (h *Handler) VerifyMFA(w http.ResponseWriter, r *http.Request) {
	var req MFAVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := h.parseMFAPendingToken(r.Context(), req.MFAToken)
	if err != nil {
		h.auditSignIn(r, audit.ActionMFAFailed, "", audit.OutcomeFailure, audit.ReasonMFAPendingInvalid)
		writeErrorCode(w, http.StatusUnauthorized, ErrCodeMFAPendingInvalid,
			"this sign-in has expired; start again")
		return
	}
	userID := uuidToString(user.ID)

	row, err := h.Queries.GetUserMFA(r.Context(), user.ID)
	if err != nil || !row.EnabledAt.Valid {

		h.auditUserAction(r, audit.ActionMFAFailed, userID, "user", userID, audit.OutcomeFailure, audit.ReasonMFANotEnrolled)
		writeErrorCode(w, http.StatusUnauthorized, ErrCodeMFANotEnrolled, "this sign-in has expired; start again")
		return
	}

	if strings.TrimSpace(req.RecoveryCode) != "" {
		affected, err := h.Queries.ConsumeUserMFARecoveryCode(r.Context(), db.ConsumeUserMFARecoveryCodeParams{
			UserID:   user.ID,
			CodeHash: hashRecoveryCode(req.RecoveryCode),
		})
		if err != nil || affected == 0 {
			h.auditUserAction(r, audit.ActionMFAFailed, userID, "user", userID, audit.OutcomeFailure, audit.ReasonMFACodeInvalid)
			writeErrorCode(w, http.StatusUnauthorized, ErrCodeMFAInvalidCode, "that code is not valid")
			return
		}
		h.auditUserAction(r, audit.ActionMFARecoveryUsed, userID, "user", userID, audit.OutcomeSuccess, "")
	} else if !h.consumeSecondFactor(r, user.ID, row, req.Code, userID) {
		writeErrorCode(w, http.StatusUnauthorized, ErrCodeMFAInvalidCode, "that code is not valid")
		return
	}

	h.auditUserAction(r, audit.ActionMFAVerified, userID, "user", userID, audit.OutcomeSuccess, "")
	h.writeLoginSession(w, r, user, audit.ActionMFAVerified)
}
