package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/corpauth"
	"github.com/adanman/goosar/server/internal/logger"
)

type AuthMethodsResponse struct {
	Methods []string `json:"methods"`

	OIDCDisplayName string `json:"oidc_display_name,omitempty"`
	LDAPDisplayName string `json:"ldap_display_name,omitempty"`
}

func (h *Handler) enabledMethods() (corpauth.Methods, corpauth.Methods) {
	requested := corpauth.MethodsFromEnv()
	usable := corpauth.Methods{Email: requested.Email}
	if requested.OIDC && h.OIDC != nil {
		usable.OIDC = true
	}
	if requested.LDAP && h.Directory != nil {
		usable.LDAP = true
	}

	if !usable.Any() {
		usable.Email = true
	}
	return requested, usable
}

func (h *Handler) GetAuthMethods(w http.ResponseWriter, r *http.Request) {
	_, usable := h.enabledMethods()

	resp := AuthMethodsResponse{Methods: []string{}}
	if usable.Email {
		resp.Methods = append(resp.Methods, corpauth.MethodEmail)
	}
	if usable.OIDC {
		resp.Methods = append(resp.Methods, corpauth.MethodOIDC)
		resp.OIDCDisplayName = h.OIDC.Config().DisplayName
	}
	if usable.LDAP {
		resp.Methods = append(resp.Methods, corpauth.MethodLDAP)
		resp.LDAPDisplayName = corpauth.LDAPConfigFromEnv().DisplayName
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) methodEnabled(w http.ResponseWriter, r *http.Request, method string) bool {
	_, usable := h.enabledMethods()
	if (method == corpauth.MethodOIDC && usable.OIDC) || (method == corpauth.MethodLDAP && usable.LDAP) {
		return true
	}
	h.auditCorporate(r, auditActionCorporateFailed, corpauth.Identity{Provider: method},
		audit.OutcomeDenied, auditReasonMethodDisabled)
	writeErrorCode(w, http.StatusNotFound, ErrCodeAuthMethodDisabled,
		"this sign-in method is not enabled on this server")
	return false
}

func (h *Handler) StartOIDC(w http.ResponseWriter, r *http.Request) {
	if !h.methodEnabled(w, r, corpauth.MethodOIDC) {
		return
	}

	client := oidcClientWeb
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("client")), oidcClientDesktop) {
		client = oidcClientDesktop
	}

	st, err := newOIDCState(client)
	if err != nil {
		slog.Error("oidc start: state", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start corporate sign-in")
		return
	}

	authURL, err := h.OIDC.AuthCodeURL(r.Context(), st.State, st.Nonce, st.Verifier)
	if err != nil {
		h.corporateFailureResponse(w, r, corpauth.MethodOIDC, err)
		return
	}

	if err := h.setOIDCStateCookie(w, st); err != nil {
		slog.Error("oidc start: state cookie", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start corporate sign-in")
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *Handler) CallbackOIDC(w http.ResponseWriter, r *http.Request) {
	if !h.methodEnabled(w, r, corpauth.MethodOIDC) {
		return
	}

	h.clearOIDCStateCookie(w)

	q := r.URL.Query()

	if idpErr := q.Get("error"); idpErr != "" {
		h.auditCorporate(r, auditActionCorporateFailed, corpauth.Identity{Provider: corpauth.MethodOIDC},
			audit.OutcomeDenied, auditReasonProviderRefused)
		slog.Info("oidc callback: provider refused", append(logger.RequestAttrs(r), "idp_error", idpErr)...)
		h.redirectCorporateFailure(w, r, oidcClientWeb, ErrCodeOIDCProviderRefused)
		return
	}

	st, err := h.readOIDCState(r, q.Get("state"))
	if err != nil {
		h.auditCorporate(r, auditActionCorporateFailed, corpauth.Identity{Provider: corpauth.MethodOIDC},
			audit.OutcomeFailure, auditReasonInvalidState)
		h.redirectCorporateFailure(w, r, oidcClientWeb, ErrCodeOIDCStateInvalid)
		return
	}

	code := q.Get("code")
	if code == "" {
		h.auditCorporate(r, auditActionCorporateFailed, corpauth.Identity{Provider: corpauth.MethodOIDC},
			audit.OutcomeFailure, auditReasonInvalidState)
		h.redirectCorporateFailure(w, r, st.Client, ErrCodeOIDCStateInvalid)
		return
	}

	identity, err := h.OIDC.Exchange(r.Context(), code, st.Verifier, st.Nonce)
	if err != nil {
		h.auditCorporate(r, auditActionCorporateFailed, corpauth.Identity{Provider: corpauth.MethodOIDC},
			audit.OutcomeFailure, corporateAuditReason(err))
		slog.Warn("oidc callback: exchange failed", append(logger.RequestAttrs(r), "error", err)...)
		h.redirectCorporateFailure(w, r, st.Client, corporateErrorCode(corpauth.MethodOIDC, err))
		return
	}

	result, refusal := h.completeCorporateLogin(w, r, identity)
	if refusal != nil {
		h.redirectCorporateFailure(w, r, st.Client, refusal.Code)
		return
	}
	if result.MFAToken != "" {

		h.redirectCorporateMFA(w, r, st.Client, result.MFAToken)
		return
	}
	h.finishOIDCRedirect(w, r, st.Client, result.Token)
}

func (h *Handler) finishOIDCRedirect(w http.ResponseWriter, r *http.Request, client, token string) {
	if client == oidcClientDesktop {
		http.Redirect(w, r, "goosar://auth/callback?token="+url.QueryEscape(token), http.StatusFound)
		return
	}
	http.Redirect(w, r, corporateReturnURL("", ""), http.StatusFound)
}

func (h *Handler) redirectCorporateMFA(w http.ResponseWriter, r *http.Request, client, ticket string) {
	if client == oidcClientDesktop {
		http.Redirect(w, r, "goosar://auth/callback?mfa_token="+url.QueryEscape(ticket), http.StatusFound)
		return
	}
	base := strings.TrimRight(resolveFrontendAppURL(), "/")
	http.Redirect(w, r, base+"/login#mfa_token="+url.QueryEscape(ticket), http.StatusFound)
}

func (h *Handler) redirectCorporateFailure(w http.ResponseWriter, r *http.Request, client, code string) {
	if client == oidcClientDesktop {
		http.Redirect(w, r, "goosar://auth/callback?error="+url.QueryEscape(code), http.StatusFound)
		return
	}
	http.Redirect(w, r, corporateReturnURL("/login", code), http.StatusFound)
}

func corporateReturnURL(path, errCode string) string {
	base := strings.TrimRight(resolveFrontendAppURL(), "/")
	if path == "" {
		path = "/"
	}
	out := base + path
	if errCode != "" {
		out += "#auth_error=" + url.QueryEscape(errCode)
	}
	return out
}

type LDAPLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handler) LoginLDAP(w http.ResponseWriter, r *http.Request) {
	if !h.methodEnabled(w, r, corpauth.MethodLDAP) {
		return
	}

	var req LDAPLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		writeErrorCode(w, http.StatusUnauthorized, ErrCodeLDAPInvalidCredentials,
			"invalid username or password")
		return
	}

	identity, err := h.Directory.Authenticate(r.Context(), req.Username, req.Password)

	if err != nil {
		h.corporateFailureResponse(w, r, corpauth.MethodLDAP, err)
		return
	}

	result, refusal := h.completeCorporateLogin(w, r, identity)
	if refusal != nil {
		refusal.write(w)
		return
	}

	if result.MFAToken != "" {
		writeJSON(w, http.StatusOK, LoginResult{MFARequired: true, MFAToken: result.MFAToken})
		return
	}
	writeJSON(w, http.StatusOK, LoginResult{
		Token:                 result.Token,
		User:                  userToResponse(result.User),
		MFAEnrollmentRequired: h.mfaStateFor(r.Context(), result.User).EnrollmentRequired,
	})
}

func corporateErrorCode(provider string, err error) string {
	switch {
	case errors.Is(err, corpauth.ErrNotConfigured):
		if provider == corpauth.MethodLDAP {
			return ErrCodeLDAPNotConfigured
		}
		return ErrCodeOIDCNotConfigured
	case errors.Is(err, corpauth.ErrUnavailable):
		if provider == corpauth.MethodLDAP {
			return ErrCodeLDAPUnavailable
		}
		return ErrCodeOIDCProviderUnavailable
	case errors.Is(err, corpauth.ErrInvalidCredentials):
		return ErrCodeLDAPInvalidCredentials
	case errors.Is(err, corpauth.ErrTokenInvalid):
		return ErrCodeOIDCTokenInvalid
	case errors.Is(err, corpauth.ErrNoEmail):
		return ErrCodeCorporateEmailMissing
	default:
		return ErrCodeCorporateLoginFailed
	}
}

func corporateAuditReason(err error) string {
	switch {
	case errors.Is(err, corpauth.ErrNotConfigured):
		return auditReasonProviderMisconfigured
	case errors.Is(err, corpauth.ErrUnavailable):
		return auditReasonProviderUnavailable
	case errors.Is(err, corpauth.ErrInvalidCredentials):
		return auditReasonInvalidCredentials
	case errors.Is(err, corpauth.ErrTokenInvalid):
		return auditReasonInvalidToken
	case errors.Is(err, corpauth.ErrNoEmail):
		return auditReasonNoEmail
	default:
		return audit.ReasonInvalidToken
	}
}
