package authn

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует все операции тега Auth. Пути даны ровно так, как в
// docs/50-api-contract.yaml — часть без префикса /api (/auth/send-code,
// /auth/verify-code, /auth/verify-link, /auth/logout), часть с ним
// (/api/auth/**) — это не опечатка контракта, так он и оформлен.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodPost, "/auth/send-code", deps.handleSendCode)
	router.Handle(http.MethodPost, "/auth/verify-code", deps.handleVerifyCode)
	router.Handle(http.MethodPost, "/auth/verify-link", deps.handleVerifyLoginLink)
	router.Handle(http.MethodPost, "/auth/logout", deps.handleLogout)

	router.Handle(http.MethodPost, "/api/auth/mfa/verify", deps.handleVerifyMfaChallenge)
	router.Handle(http.MethodGet, "/api/auth/methods", deps.handleListMethods)
	router.Handle(http.MethodGet, "/api/auth/oidc/start", deps.handleOidcStart)
	router.Handle(http.MethodGet, "/api/auth/oidc/callback", deps.handleOidcCallback)
	router.Handle(http.MethodPost, "/api/auth/ldap/login", deps.handleLoginLdap)

	router.Handle(http.MethodGet, "/api/auth/mfa", deps.handleGetMfaStatus)
	router.Handle(http.MethodPost, "/api/auth/mfa/totp/enroll", deps.handleEnrollTotp)
	router.Handle(http.MethodPost, "/api/auth/mfa/totp/confirm", deps.handleConfirmTotp)
	router.Handle(http.MethodPost, "/api/auth/mfa/totp/disable", deps.handleDisableTotp)
	router.Handle(http.MethodPost, "/api/auth/mfa/recovery-codes", deps.handleRegenerateMfaRecoveryCodes)

	router.Handle(http.MethodGet, "/api/auth/sessions", deps.handleListSessions)
	router.Handle(http.MethodDelete, "/api/auth/sessions/{sessionId}", deps.handleRevokeSession)
	router.Handle(http.MethodPost, "/api/auth/sessions/revoke-all", deps.handleRevokeAllSessions)
}
