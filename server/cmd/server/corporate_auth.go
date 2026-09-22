package main

import (
	"log/slog"

	"github.com/adanman/goosar/server/internal/corpauth"
	"github.com/adanman/goosar/server/internal/handler"
)

func wireCorporateAuth(h *handler.Handler) {
	methods := corpauth.MethodsFromEnv()

	if methods.OIDC {
		cfg := corpauth.OIDCConfigFromEnv()
		if h.OIDC = corpauth.NewOIDCProvider(cfg); h.OIDC == nil {

			slog.Warn("corporate sign-in: oidc is listed in "+corpauth.MethodsEnvVar+" but the method will not be offered",
				"issuer_set", cfg.Issuer != "",
				"client_id_set", cfg.ClientID != "",
				"redirect_url_set", cfg.RedirectURL != "",
				"encrypted_issuer", cfg.EncryptedIssuer(),
			)
		} else {
			slog.Info("corporate sign-in: oidc enabled",
				"issuer", cfg.Issuer,
				"confidential_client", cfg.ClientSecret != "",
				"admin_claim_mapping", cfg.AdminClaim != "" && cfg.AdminValue != "",
			)
		}
	}

	if methods.LDAP {
		cfg := corpauth.LDAPConfigFromEnv()
		if h.Directory = corpauth.NewLDAPDirectory(cfg); h.Directory == nil {

			slog.Warn("corporate sign-in: ldap is listed in "+corpauth.MethodsEnvVar+" but the method will not be offered",
				"url_set", cfg.URL != "",
				"base_dn_set", cfg.BaseDN != "",
				"encrypted_transport", cfg.EncryptedTransport(),
			)
		} else {
			slog.Info("corporate sign-in: ldap enabled",
				"start_tls", cfg.StartTLS,
				"service_bind", cfg.BindDN != "",
				"admin_group_mapping", cfg.AdminGroup != "",
			)
		}
	}
}
