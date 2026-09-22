package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/corpauth"
	"github.com/adanman/goosar/server/internal/logger"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/service"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type SignupError struct {
	Message string
	Code    string
}

func (e SignupError) Error() string {
	return e.Message
}

var ErrSignupProhibited = SignupError{
	Message: "user registration is disabled on this self-hosted instance",
	Code:    ErrCodeSignupDisabled,
}

var ErrEmailNotAllowed = SignupError{
	Message: "email address or domain not allowed on this instance",
	Code:    ErrCodeEmailDomainNotAllows,
}

const devVerificationCodeEnv = "GOOSAR_DEV_VERIFICATION_CODE"

func hashEmailForLog(email string) string {
	mac := hmac.New(sha256.New, emailPseudonymKey())
	mac.Write([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(mac.Sum(nil)[:8])
}

func emailPseudonymKey() []byte {
	key := sha256.Sum256(append([]byte("goosar-email-pseudonym:"), auth.JWTSecret()...))
	return key[:]
}

var supportedLanguages = map[string]struct{}{
	"en":      {},
	"zh-Hans": {},
	"ko":      {},
	"ja":      {},

	"ru": {},
}

type UserResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
	Language  *string `json:"language"`

	Timezone                *string         `json:"timezone"`
	OnboardedAt             *string         `json:"onboarded_at"`
	OnboardingQuestionnaire json.RawMessage `json:"onboarding_questionnaire"`
	StarterContentState     *string         `json:"starter_content_state"`
	ProfileDescription      string          `json:"profile_description"`
	CreatedAt               string          `json:"created_at"`
	UpdatedAt               string          `json:"updated_at"`
}

const MaxProfileDescriptionLen = 2000

func userToResponse(u db.User) UserResponse {

	q := u.OnboardingQuestionnaire
	if len(q) == 0 {
		q = []byte("{}")
	}
	return UserResponse{
		ID:                      uuidToString(u.ID),
		Name:                    u.Name,
		Email:                   u.Email,
		AvatarURL:               textToPtr(u.AvatarUrl),
		Language:                textToPtr(u.Language),
		Timezone:                textToPtr(u.Timezone),
		OnboardedAt:             timestampToPtr(u.OnboardedAt),
		OnboardingQuestionnaire: json.RawMessage(q),
		StarterContentState:     textToPtr(u.StarterContentState),
		ProfileDescription:      u.ProfileDescription,
		CreatedAt:               timestampToString(u.CreatedAt),
		UpdatedAt:               timestampToString(u.UpdatedAt),
	}
}

type LoginResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
}

type SendCodeRequest struct {
	Email string `json:"email"`
}

type VerifyCodeRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func generateCode() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	n := binary.BigEndian.Uint32(buf[:]) % 1000000
	return fmt.Sprintf("%06d", n), nil
}

func isDevVerificationCode(code string) bool {
	if isProductionEnv() {
		return false
	}

	devCode := strings.TrimSpace(os.Getenv(devVerificationCodeEnv))
	if !isSixDigitCode(devCode) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(code), []byte(devCode)) == 1
}

func isProductionEnv() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
}

func isSixDigitCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func (h *Handler) issueJWT(user db.User) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   uuidToString(user.ID),
		"email": user.Email,
		"name":  user.Name,

		"tv":  user.TokenVersion,
		"exp": time.Now().Add(auth.AuthTokenTTL()).Unix(),
		"iat": time.Now().Unix(),
	})
	return token.SignedString(auth.JWTSecret())
}

func (h *Handler) findOrCreateUser(ctx context.Context, email string) (user db.User, isNew bool, err error) {
	user, err = h.Queries.GetUserByEmail(ctx, email)
	isNew = isNotFound(err)
	if err != nil && !isNew {
		return db.User{}, false, err
	}

	if err := h.checkSignupAllowed(email, isNew); err != nil {
		return db.User{}, false, err
	}

	if !isNew {
		return user, false, nil
	}

	name := email
	if at := strings.Index(email, "@"); at > 0 {
		name = email[:at]
	}
	created, err := h.Queries.CreateUser(ctx, db.CreateUserParams{
		Name:  name,
		Email: email,
	})
	if err != nil {
		return db.User{}, false, err
	}
	return created, true, nil
}

const signupSourceMaxLen = 512

func signupSourceFromRequest(r *http.Request) string {
	c, err := r.Cookie("goosar_signup_source")
	if err != nil || c == nil {
		return ""
	}
	decoded, err := url.QueryUnescape(c.Value)
	if err != nil {
		return ""
	}
	if len(decoded) > signupSourceMaxLen {
		return ""
	}
	return decoded
}

func (h *Handler) checkSignupAllowed(email string, isNewUser bool) error {
	if !isNewUser {
		return nil
	}

	email = strings.ToLower(email)
	domain := ""
	if at := strings.Index(email, "@"); at > 0 {
		domain = email[at+1:]
	}

	if len(h.cfg.AllowedEmails) > 0 && contains(h.cfg.AllowedEmails, email) {
		return nil
	}

	if len(h.cfg.AllowedEmailDomains) > 0 && contains(h.cfg.AllowedEmailDomains, domain) {
		return nil
	}

	if !h.cfg.AllowSignup {
		return ErrSignupProhibited
	}

	if len(h.cfg.AllowedEmailDomains) > 0 || len(h.cfg.AllowedEmails) > 0 {
		return ErrSignupProhibited
	}

	return nil
}

func contains(slice []string, s string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

func emailLangForLoginCode(existing db.User) string {
	return service.EmailLangForUserLanguage(existing.Language.String)
}

func (h *Handler) SendCode(w http.ResponseWriter, r *http.Request) {
	var req SendCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	if h.EmailService != nil && h.EmailService.Transport() == service.TransportNone {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "email delivery is not configured on this server",
			"code":  ErrCodeEmailNotConfigured,
		})
		return
	}

	existingUser, err := h.Queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		if !isNotFound(err) {

			writeError(w, http.StatusInternalServerError, "failed to lookup user")
			return
		}

		isNewUser := true
		if err := h.checkSignupAllowed(email, isNewUser); err != nil {
			h.auditSignIn(r, audit.ActionLoginCodeSent, email, audit.OutcomeDenied, audit.ReasonSignupRestricted)
			var signupErr SignupError
			if errors.As(err, &signupErr) {
				writeErrorCode(w, http.StatusForbidden, signupErr.Code, signupErr.Error())
			} else {
				writeErrorCode(w, http.StatusForbidden, ErrCodeSignupDisabled, "user registration is disabled")
			}
			return
		}
	} else {

		isNewUser := false
		if err := h.checkSignupAllowed(email, isNewUser); err != nil {
			h.auditSignIn(r, audit.ActionLoginCodeSent, email, audit.OutcomeDenied, audit.ReasonSignupRestricted)

			var signupErr SignupError
			if errors.As(err, &signupErr) {
				writeErrorCode(w, http.StatusForbidden, signupErr.Code, signupErr.Error())
			} else {
				writeErrorCode(w, http.StatusForbidden, ErrCodeSignupDisabled, "user registration is disabled")
			}
			return
		}
	}

	latest, err := h.Queries.GetLatestCodeByEmail(r.Context(), email)
	if err == nil && time.Since(latest.CreatedAt.Time) < 60*time.Second {
		h.auditSignIn(r, audit.ActionLoginCodeSent, email, audit.OutcomeDenied, audit.ReasonRateLimited)
		writeErrorCode(w, http.StatusTooManyRequests, ErrCodeCodeRateLimited, "please wait before requesting another code")
		return
	}

	code, err := generateCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate code")
		return
	}

	linkToken, err := generateLoginLinkToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate code")
		return
	}

	if err := h.Queries.InvalidatePriorVerificationCodes(r.Context(), email); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate code")
		return
	}

	_, err = h.Queries.CreateVerificationCode(r.Context(), db.CreateVerificationCodeParams{
		Email:         email,
		Code:          code,
		ExpiresAt:     pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
		LinkTokenHash: pgtype.Text{String: hashLoginLinkToken(linkToken), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store verification code")
		return
	}

	lang := emailLangForLoginCode(existingUser)
	if err := h.EmailService.SendVerificationCode(email, code, linkToken, lang); err != nil {
		if errors.Is(err, service.ErrEmailNotConfigured) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "email delivery is not configured on this server",
				"code":  ErrCodeEmailNotConfigured,
			})
			return
		}
		slog.Error("failed to send verification code", "email_hash", hashEmailForLog(email), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to send verification code")
		return
	}

	h.auditSignIn(r, audit.ActionLoginCodeSent, email, audit.OutcomeSuccess, "")

	_ = h.Queries.DeleteExpiredVerificationCodes(r.Context())

	writeJSON(w, http.StatusOK, map[string]string{"message": "Verification code sent"})
}

func (h *Handler) VerifyCode(w http.ResponseWriter, r *http.Request) {
	var req VerifyCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	code := strings.TrimSpace(req.Code)

	if email == "" || code == "" {
		writeError(w, http.StatusBadRequest, "email and code are required")
		return
	}

	dbCode, err := h.Queries.GetLatestVerificationCode(r.Context(), email)
	if err != nil {
		h.auditSignIn(r, audit.ActionLoginCodeFailed, email, audit.OutcomeFailure, audit.ReasonExpiredCode)
		writeErrorCode(w, http.StatusBadRequest, ErrCodeInvalidCode, "invalid or expired code")
		return
	}

	isDevCode := isDevVerificationCode(code)
	if !isDevCode && subtle.ConstantTimeCompare([]byte(code), []byte(dbCode.Code)) != 1 {
		_ = h.Queries.IncrementVerificationCodeAttempts(r.Context(), dbCode.ID)

		h.auditSignIn(r, audit.ActionLoginCodeFailed, email, audit.OutcomeFailure, audit.ReasonInvalidCode)
		writeErrorCode(w, http.StatusBadRequest, ErrCodeInvalidCode, "invalid or expired code")
		return
	}

	rows, err := h.Queries.ConsumeVerificationCode(r.Context(), dbCode.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify code")
		return
	}
	if rows == 0 {
		h.auditSignIn(r, audit.ActionLoginCodeFailed, email, audit.OutcomeFailure, audit.ReasonCodeAlreadyUsed)

		writeErrorCode(w, http.StatusBadRequest, ErrCodeInvalidCode, "invalid or expired code")
		return
	}

	h.completeLogin(w, r, email, audit.ActionLoginCodeVerified)
}

func (h *Handler) completeLogin(w http.ResponseWriter, r *http.Request, email string, successAction string) {
	user, isNew, err := h.findOrCreateUser(r.Context(), email)
	if err != nil {
		var signupErr SignupError
		if errors.As(err, &signupErr) {
			writeErrorCode(w, http.StatusForbidden, signupErr.Code, signupErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	if user.DeactivatedAt.Valid {
		h.auditUserAction(r, audit.ActionLoginDenied, uuidToString(user.ID), "user", uuidToString(user.ID), audit.OutcomeDenied, audit.ReasonDeactivated)
		slog.Warn("login refused: account deactivated", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID))...)
		writeErrorCode(w, http.StatusForbidden, ErrCodeAccountDeactivated, "account deactivated")
		return
	}
	if isNew {
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r)))

		if raw := os.Getenv(DeploymentAdminEmailsEnvVar); raw != "" && h.deploymentHasNoAdmins(r.Context()) {
			if err := SeedDeploymentAdmins(r.Context(), h.TxStarter, h.Queries, raw); err != nil {

				slog.Warn("deployment admin seed on signup failed", "error", err)
			}
		}
	}

	if h.mfaChallengeRequired(r.Context(), user, corpauth.MethodEmail) {
		h.auditUserAction(r, audit.ActionMFARequired, uuidToString(user.ID), "user", uuidToString(user.ID),
			audit.OutcomeSuccess, "")
		h.writeMFAChallenge(w, r, user)
		return
	}

	h.writeLoginSession(w, r, user, successAction)
}

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}

type UpdateMeRequest struct {
	Name               *string `json:"name"`
	AvatarURL          *string `json:"avatar_url"`
	Language           *string `json:"language"`
	ProfileDescription *string `json:"profile_description"`

	Timezone *string `json:"timezone"`
}

func (h *Handler) IssueCliToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	tokenString, _, err := h.issueSessionJWT(r, user)
	if err != nil {
		h.auditUserAction(r, audit.ActionCliTokenIssued, userID, "user", userID, audit.OutcomeFailure, "")
		slog.Warn("cli-token: failed to issue JWT", append(logger.RequestAttrs(r), "error", err, "user_id", userID)...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	h.auditUserAction(r, audit.ActionCliTokenIssued, userID, "user", userID, audit.OutcomeSuccess, "")
	writeJSON(w, http.StatusOK, map[string]string{"token": tokenString})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {

	h.auditUserAction(r, audit.ActionLogout, verifiedUserIDFromRequest(r), "session", "", audit.OutcomeSuccess, "")
	auth.ClearAuthCookies(w)
	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func verifiedUserIDFromRequest(r *http.Request) string {
	tokenString := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tokenString == r.Header.Get("Authorization") {
		tokenString = ""
	}
	if tokenString == "" {
		cookie, err := r.Cookie(auth.AuthCookieName)
		if err != nil || cookie.Value == "" {
			return ""
		}
		tokenString = cookie.Value
	}

	token, err := auth.ParseSessionHS256(tokenString)
	if err != nil || !token.Valid {
		return ""
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}
	sub, _ := claims["sub"].(string)
	return sub
}

func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req UpdateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	currentUser, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	name := currentUser.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
	}

	params := db.UpdateUserParams{
		ID:   currentUser.ID,
		Name: name,
	}
	if req.AvatarURL != nil {
		params.AvatarUrl = pgtype.Text{String: strings.TrimSpace(*req.AvatarURL), Valid: true}
	}
	if req.Language != nil {
		lang := strings.TrimSpace(*req.Language)
		if _, ok := supportedLanguages[lang]; !ok {
			writeError(w, http.StatusBadRequest, "unsupported language")
			return
		}
		params.Language = pgtype.Text{String: lang, Valid: true}
	}
	if req.ProfileDescription != nil {

		desc := strings.TrimSpace(*req.ProfileDescription)
		if utf8.RuneCountInString(desc) > MaxProfileDescriptionLen {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("profile_description exceeds %d characters", MaxProfileDescriptionLen))
			return
		}
		params.ProfileDescription = pgtype.Text{String: desc, Valid: true}
	}

	if req.Timezone != nil {

		tz := strings.TrimSpace(*req.Timezone)
		if tz != "" {
			if loc, err := time.LoadLocation(tz); err != nil || loc == nil {
				writeError(w, http.StatusBadRequest, "invalid timezone")
				return
			}
		}
		params.Timezone = pgtype.Text{String: tz, Valid: true}
	}

	updatedUser, err := h.Queries.UpdateUser(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}

	writeJSON(w, http.StatusOK, userToResponse(updatedUser))
}
