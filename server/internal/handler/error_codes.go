package handler

import "net/http"

const (
	ErrCodeEmailNotConfigured   = "email_not_configured"
	ErrCodeSignupDisabled       = "signup_disabled"
	ErrCodeEmailDomainNotAllows = "email_domain_not_allowed"
	ErrCodeCodeRateLimited      = "code_rate_limited"
	ErrCodeInvalidCode          = "invalid_or_expired_code"
	ErrCodeAccountDeactivated   = "account_deactivated"

	ErrCodeAuthMethodDisabled      = "auth_method_disabled"
	ErrCodeOIDCNotConfigured       = "oidc_not_configured"
	ErrCodeOIDCProviderUnavailable = "oidc_provider_unavailable"
	ErrCodeOIDCProviderRefused     = "oidc_provider_refused"
	ErrCodeOIDCStateInvalid        = "oidc_state_invalid"
	ErrCodeOIDCTokenInvalid        = "oidc_token_invalid"
	ErrCodeLDAPNotConfigured       = "ldap_not_configured"
	ErrCodeLDAPUnavailable         = "ldap_unavailable"

	ErrCodeLDAPInvalidCredentials = "ldap_invalid_credentials"
	ErrCodeCorporateEmailMissing  = "corporate_email_missing"

	ErrCodeCorporateEmailUnverified = "corporate_email_unverified"
	ErrCodeCorporateLoginFailed     = "corporate_login_failed"

	ErrCodeMFAPendingInvalid  = "mfa_pending_invalid"
	ErrCodeMFAInvalidCode     = "mfa_invalid_code"
	ErrCodeMFACodeReplayed    = "mfa_code_replayed"
	ErrCodeMFANotEnrolled     = "mfa_not_enrolled"
	ErrCodeMFAAlreadyEnrolled = "mfa_already_enrolled"

	ErrCodeMFAUnavailable = "mfa_unavailable"

	ErrCodeInvitationNotFound       = "invitation_not_found"
	ErrCodeInvitationNotYours       = "invitation_not_yours"
	ErrCodeInvitationExpired        = "invitation_expired"
	ErrCodeInvitationNotPending     = "invitation_not_pending"
	ErrCodeInvitationAlreadyPending = "invitation_already_pending"
	ErrCodeAlreadyMember            = "already_member"

	ErrCodeAlreadyMemberSelf = "already_member_self"

	ErrCodeWorkspaceCreationDisabled = "workspace_creation_disabled"
	ErrCodeWorkspaceSlugTaken        = "workspace_slug_taken"
	ErrCodeWorkspaceSlugReserved     = "workspace_slug_reserved"
	ErrCodeWorkspaceSlugInvalid      = "workspace_slug_invalid"
	ErrCodeInsufficientPermissions   = "insufficient_permissions"
)

func writeErrorCode(w http.ResponseWriter, status int, code, msg string) {
	body := map[string]string{"error": msg, "code": code}
	if rid := w.Header().Get("X-Request-ID"); rid != "" {
		body["request_id"] = rid
	}
	writeJSON(w, status, body)
}
