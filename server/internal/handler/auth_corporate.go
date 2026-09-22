// Корпоративный вход: правила, общие для OIDC и LDAP. Протоколы живут в
// internal/corpauth и заканчиваются проверенной corpauth.Identity; всё дальше —
// продуктовая политика, реализованная здесь один раз для обоих провайдеров.
package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/corpauth"
	"github.com/adanman/goosar/server/internal/logger"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	auditActionCorporateLogin      = "auth.corporate.verified"
	auditActionCorporateFailed     = "auth.corporate.failed"
	auditActionCorporateAdminFiled = "auth.corporate.admin_requested"

	auditReasonProviderUnavailable   = "provider_unavailable"
	auditReasonProviderMisconfigured = "provider_misconfigured"
	auditReasonProviderRefused       = "provider_refused"
	auditReasonInvalidCredentials    = "invalid_credentials"
	auditReasonInvalidToken          = "invalid_token"
	auditReasonInvalidState          = "invalid_state"
	auditReasonNoEmail               = "no_email"
	auditReasonMethodDisabled        = "method_disabled"
	auditReasonUnverifiedEmail       = "unverified_email"
)

var errUnverifiedAddressInherit = errors.New("corpauth: the provider does not vouch for this address")

func trustUnverifiedCorporateEmail() bool {
	return os.Getenv("GOOSAR_OIDC_TRUST_UNVERIFIED_EMAIL") == "1"
}

type corporateLoginResult struct {
	User  db.User
	Token string

	MFAToken string
}

type corporateRefusal struct {
	Status  int
	Code    string
	Message string
}

func (ref corporateRefusal) write(w http.ResponseWriter) {
	writeErrorCode(w, ref.Status, ref.Code, ref.Message)
}

func (h *Handler) resolveCorporateUser(ctx context.Context, id corpauth.Identity) (db.User, bool, error) {
	email := strings.ToLower(strings.TrimSpace(id.Email))
	if email == "" {
		return db.User{}, false, corpauth.ErrNoEmail
	}

	if link, err := h.Queries.GetUserIdentity(ctx, db.GetUserIdentityParams{
		Provider: id.Provider,
		Subject:  id.Subject,
	}); err == nil {
		user, err := h.Queries.GetUser(ctx, link.UserID)
		if err == nil {
			return user, false, nil
		}

		slog.Warn("corporate identity link points at a missing user; re-resolving by address",
			"provider", id.Provider)
	} else if !isNotFound(err) {
		return db.User{}, false, err
	}

	if !id.EmailVerified && !trustUnverifiedCorporateEmail() {
		if _, err := h.Queries.GetUserByEmail(ctx, email); err == nil {
			return db.User{}, false, errUnverifiedAddressInherit
		} else if !isNotFound(err) {
			return db.User{}, false, err
		}
	}
	return h.findOrCreateUser(ctx, email)
}

func (h *Handler) completeCorporateLogin(w http.ResponseWriter, r *http.Request, id corpauth.Identity) (corporateLoginResult, *corporateRefusal) {
	user, isNew, err := h.resolveCorporateUser(r.Context(), id)
	if err != nil {
		var signupErr SignupError
		switch {
		case errors.As(err, &signupErr):
			h.auditCorporate(r, auditActionCorporateFailed, id, audit.OutcomeDenied, audit.ReasonSignupRestricted)
			return corporateLoginResult{}, &corporateRefusal{
				Status: http.StatusForbidden, Code: signupErr.Code, Message: signupErr.Error(),
			}
		case errors.Is(err, errUnverifiedAddressInherit):
			h.auditCorporate(r, auditActionCorporateFailed, id, audit.OutcomeDenied, auditReasonUnverifiedEmail)
			return corporateLoginResult{}, &corporateRefusal{
				Status:  http.StatusForbidden,
				Code:    ErrCodeCorporateEmailUnverified,
				Message: "an account with this address already exists and the directory does not vouch for the address",
			}
		case errors.Is(err, corpauth.ErrNoEmail):
			h.auditCorporate(r, auditActionCorporateFailed, id, audit.OutcomeDenied, auditReasonNoEmail)
			return corporateLoginResult{}, &corporateRefusal{
				Status:  http.StatusForbidden,
				Code:    ErrCodeCorporateEmailMissing,
				Message: "the directory record carries no email address",
			}
		default:
			slog.Error("corporate login: resolve user", append(logger.RequestAttrs(r), "error", err)...)
			return corporateLoginResult{}, &corporateRefusal{
				Status:  http.StatusInternalServerError,
				Code:    ErrCodeCorporateLoginFailed,
				Message: "failed to resolve account",
			}
		}
	}

	if user.DeactivatedAt.Valid {
		h.auditUserAction(r, audit.ActionLoginDenied, uuidToString(user.ID), "user", uuidToString(user.ID),
			audit.OutcomeDenied, audit.ReasonDeactivated)
		slog.Warn("corporate login refused: account deactivated",
			append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID))...)
		return corporateLoginResult{}, &corporateRefusal{
			Status: http.StatusForbidden, Code: ErrCodeAccountDeactivated, Message: "account deactivated",
		}
	}

	if _, err := h.Queries.LinkUserIdentity(r.Context(), db.LinkUserIdentityParams{
		UserID:      user.ID,
		Provider:    id.Provider,
		Subject:     id.Subject,
		EmailAtLink: id.Email,
	}); err != nil {

		slog.Warn("corporate login: failed to record identity link",
			append(logger.RequestAttrs(r), "error", err, "user_id", uuidToString(user.ID))...)
	}

	if isNew {
		obsmetrics.RecordEvent(h.Analytics, h.Metrics,
			analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r)))
	}

	if id.IsAdmin {
		h.fileCorporateAdminRequest(r, user)
	}

	if h.mfaChallengeRequired(r.Context(), user, id.Provider) {
		ticket, err := h.issueMFAPendingToken(user)
		if err != nil {
			slog.Error("corporate login: mfa ticket", append(logger.RequestAttrs(r), "error", err)...)
			return corporateLoginResult{}, &corporateRefusal{
				Status:  http.StatusInternalServerError,
				Code:    ErrCodeCorporateLoginFailed,
				Message: "failed to continue sign-in",
			}
		}
		h.auditUserAction(r, audit.ActionMFARequired, uuidToString(user.ID), "user", uuidToString(user.ID),
			audit.OutcomeSuccess, id.Provider)
		return corporateLoginResult{User: user, MFAToken: ticket}, nil
	}

	token, _, err := h.issueSessionJWT(r, user)
	if err != nil {
		slog.Warn("corporate login: token", append(logger.RequestAttrs(r), "error", err)...)
		return corporateLoginResult{}, &corporateRefusal{
			Status:  http.StatusInternalServerError,
			Code:    ErrCodeCorporateLoginFailed,
			Message: "failed to generate token",
		}
	}
	if err := auth.SetAuthCookies(w, token); err != nil {
		slog.Warn("failed to set auth cookies", "error", err)
	}

	h.auditUserAction(r, auditActionCorporateLogin, uuidToString(user.ID), "user", uuidToString(user.ID),
		audit.OutcomeSuccess, id.Provider)
	slog.Info("user logged in via corporate directory",
		append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "provider", id.Provider)...)
	return corporateLoginResult{User: user, Token: token}, nil
}

func (h *Handler) fileCorporateAdminRequest(r *http.Request, user db.User) {
	if h.TxStarter == nil {
		return
	}
	if _, err := h.Queries.GetDeploymentAdmin(r.Context(), user.ID); err == nil {
		return
	} else if !isNotFound(err) {
		slog.Warn("corporate admin mapping: role lookup failed", "error", err)
		return
	}

	_, filed, err := h.fileDeploymentAdminPending(r.Context(), user.ID,
		deploymentAdminPendingActionGrant, user.ID, adminAuditRequestID(r))
	if err != nil {

		slog.Warn("corporate admin mapping: failed to file pending grant",
			append(logger.RequestAttrs(r), "error", err, "user_id", uuidToString(user.ID))...)
		return
	}
	if filed {
		h.auditUserAction(r, auditActionCorporateAdminFiled, uuidToString(user.ID), "user",
			uuidToString(user.ID), audit.OutcomeSuccess, "directory_admin_group")
		slog.Info("corporate directory admin group filed a pending deployment_admin grant",
			append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID))...)
	}
}

func (h *Handler) auditCorporate(r *http.Request, action string, id corpauth.Identity, outcome, reason string) {
	ev := h.auditEvent(r)
	ev.Action = action
	ev.ActorType = audit.ActorAnonymous
	if id.Email != "" {
		ev.ActorID = hashEmailForLog(id.Email)
	}
	ev.TargetType = "corporate_identity"
	ev.TargetID = id.Provider
	ev.Outcome = outcome
	ev.Reason = reason
	h.recordAudit(r, ev)
}

func (h *Handler) corporateFailureResponse(w http.ResponseWriter, r *http.Request, provider string, err error) {
	id := corpauth.Identity{Provider: provider}
	switch {
	case errors.Is(err, corpauth.ErrNotConfigured):
		h.auditCorporate(r, auditActionCorporateFailed, id, audit.OutcomeFailure, auditReasonProviderMisconfigured)
		code, msg := ErrCodeOIDCNotConfigured, "corporate sign-in is not configured on this server"
		if provider == corpauth.MethodLDAP {
			code = ErrCodeLDAPNotConfigured
			msg = "directory sign-in is not configured on this server"
		}
		writeErrorCode(w, http.StatusServiceUnavailable, code, msg)
	case errors.Is(err, corpauth.ErrUnavailable):
		h.auditCorporate(r, auditActionCorporateFailed, id, audit.OutcomeFailure, auditReasonProviderUnavailable)
		code, msg := ErrCodeOIDCProviderUnavailable, "the identity provider did not answer"
		if provider == corpauth.MethodLDAP {
			code = ErrCodeLDAPUnavailable
			msg = "the directory did not answer"
		}

		writeErrorCode(w, http.StatusBadGateway, code, msg)
	case errors.Is(err, corpauth.ErrInvalidCredentials):
		h.auditCorporate(r, auditActionCorporateFailed, id, audit.OutcomeFailure, auditReasonInvalidCredentials)
		writeErrorCode(w, http.StatusUnauthorized, ErrCodeLDAPInvalidCredentials, "invalid username or password")
	case errors.Is(err, corpauth.ErrTokenInvalid):
		h.auditCorporate(r, auditActionCorporateFailed, id, audit.OutcomeFailure, auditReasonInvalidToken)
		writeErrorCode(w, http.StatusUnauthorized, ErrCodeOIDCTokenInvalid, "the identity provider assertion failed verification")
	case errors.Is(err, corpauth.ErrNoEmail):
		h.auditCorporate(r, auditActionCorporateFailed, id, audit.OutcomeDenied, auditReasonNoEmail)
		writeErrorCode(w, http.StatusForbidden, ErrCodeCorporateEmailMissing, "the directory record carries no email address")
	default:
		slog.Error("corporate login failed", append(logger.RequestAttrs(r), "error", err, "provider", provider)...)
		writeError(w, http.StatusInternalServerError, "corporate sign-in failed")
	}
}
