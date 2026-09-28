package config

import (
	"os"
	"testing"
	"time"
)

// clearEnv снимает переменные окружения этого пакета перед каждым тестом,
// чтобы значения, оставленные другим тестом (или окружением сессии), не
// просачивались — Load() читает os.Environ() напрямую, отдельного объекта
// окружения не заводит.
func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		old, had := os.LookupEnv(k)
		os.Unsetenv(k)
		t.Cleanup(func() {
			if had {
				os.Setenv(k, old)
			} else {
				os.Unsetenv(k)
			}
		})
	}
}

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		old, had := os.LookupEnv(k)
		os.Setenv(k, v)
		t.Cleanup(func() {
			if had {
				os.Setenv(k, old)
			} else {
				os.Unsetenv(k)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	// Полный список переменных, тронутых этим тестовым файлом — чтобы Load()
	// в TestLoadDefaults видела чистое окружение независимо от порядка
	// прогона тестов в пакете.
	clearEnv(t,
		"DATABASE_URL", "PORT", "APP_ENV", "JWT_SECRET", "JWT_SECRET_PREVIOUS",
		"AUTH_TOKEN_TTL", "FRONTEND_ORIGIN", "ALLOWED_ORIGINS", "CORS_ALLOWED_ORIGINS",
		"GOOSAR_APP_URL", "RESEND_FROM_EMAIL", "SMTP_PORT", "SMTP_FROM_EMAIL", "SMTP_TLS",
		"GOOSAR_AUTH_METHODS", "GOOSAR_TOTP_ISSUER", "GOOSAR_OIDC_ISSUER", "GOOSAR_OIDC_SCOPES",
		"GOOSAR_LDAP_URL", "GOOSAR_LDAP_USER_FILTER", "ATTACHMENT_DOWNLOAD_URL_TTL",
		"GOOSAR_DELIVERY_PROFILE", "GOOSAR_DEPLOYMENT_PROFILE", "GOOSAR_RETENTION_CHAT",
		"GOOSAR_ATTACHMENT_PURGE_GRACE", "GOOSAR_EXPORT_TIMEOUT", "GOOSAR_EXPORT_RETENTION",
		"POSTHOG_HOST", "LOG_FORMAT", "GOOSAR_LOG_FORMAT",
	)

	cfg := Load()

	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.AuthTokenTTL != 2592000*time.Second {
		t.Errorf("AuthTokenTTL = %v, want 30d", cfg.AuthTokenTTL)
	}
	if cfg.ResendFromEmail != "noreply@goosar.ru" {
		t.Errorf("ResendFromEmail = %q, want noreply@goosar.ru", cfg.ResendFromEmail)
	}
	if cfg.SMTPPort != 25 {
		t.Errorf("SMTPPort = %d, want 25", cfg.SMTPPort)
	}
	if cfg.SMTPFromEmail != cfg.ResendFromEmail {
		t.Errorf("SMTPFromEmail = %q, want inherit ResendFromEmail %q", cfg.SMTPFromEmail, cfg.ResendFromEmail)
	}
	if cfg.SMTPTLS != "starttls" {
		t.Errorf("SMTPTLS = %q, want starttls", cfg.SMTPTLS)
	}
	if len(cfg.AuthMethods) != 1 || cfg.AuthMethods[0] != "email" {
		t.Errorf("AuthMethods = %v, want [email]", cfg.AuthMethods)
	}
	if !cfg.AuthMethodEnabled("email") || cfg.AuthMethodEnabled("oidc") {
		t.Error("default AuthMethods should enable only email")
	}
	if cfg.TotpIssuer != "Goosar" {
		t.Errorf("TotpIssuer = %q, want Goosar", cfg.TotpIssuer)
	}
	wantScopes := []string{"openid", "profile", "email"}
	if len(cfg.OIDC.Scopes) != len(wantScopes) {
		t.Errorf("OIDC.Scopes = %v, want %v", cfg.OIDC.Scopes, wantScopes)
	}
	if cfg.LDAP.UserFilter != "(uid=%s)" {
		t.Errorf("LDAP.UserFilter = %q, want (uid=%%s)", cfg.LDAP.UserFilter)
	}
	if cfg.AttachmentDownloadTTL != 30*time.Minute {
		t.Errorf("AttachmentDownloadTTL = %v, want 30m", cfg.AttachmentDownloadTTL)
	}
	if cfg.DeliveryProfile != "cloud" {
		t.Errorf("DeliveryProfile = %q, want cloud", cfg.DeliveryProfile)
	}
	if cfg.DeploymentProfile != "perimeter" {
		t.Errorf("DeploymentProfile = %q, want perimeter", cfg.DeploymentProfile)
	}
	if cfg.RetentionChat != 0 {
		t.Errorf("RetentionChat = %v, want 0 (forever)", cfg.RetentionChat)
	}
	if cfg.AttachmentPurgeGrace != 168*time.Hour {
		t.Errorf("AttachmentPurgeGrace = %v, want 168h", cfg.AttachmentPurgeGrace)
	}
	if cfg.ExportTimeout != 2*time.Hour {
		t.Errorf("ExportTimeout = %v, want 2h", cfg.ExportTimeout)
	}
	if cfg.ExportRetention != 168*time.Hour {
		t.Errorf("ExportRetention = %v, want 168h", cfg.ExportRetention)
	}
	if cfg.PosthogHost != "https://us.i.posthog.com" {
		t.Errorf("PosthogHost = %q", cfg.PosthogHost)
	}
	if cfg.AppURL != cfg.FrontendOrigin {
		t.Errorf("AppURL = %q, want default to FrontendOrigin %q", cfg.AppURL, cfg.FrontendOrigin)
	}
}

func TestSMTPPortImpliesImplicitTLS(t *testing.T) {
	clearEnv(t, "SMTP_TLS")
	setEnv(t, map[string]string{"SMTP_PORT": "465"})
	cfg := Load()
	if cfg.SMTPTLS != "implicit" {
		t.Errorf("SMTP_TLS = %q, want implicit at port 465", cfg.SMTPTLS)
	}
}

func TestSMTPTLSAliases(t *testing.T) {
	for _, alias := range []string{"implicit", "smtps", "SSL"} {
		setEnv(t, map[string]string{"SMTP_TLS": alias})
		cfg := Load()
		if cfg.SMTPTLS != "implicit" {
			t.Errorf("SMTP_TLS=%q => %q, want implicit", alias, cfg.SMTPTLS)
		}
	}
}

func TestMailProviderPriority(t *testing.T) {
	clearEnv(t, "SMTP_HOST", "RESEND_API_KEY")
	if got := Load().MailProvider(); got != "" {
		t.Errorf("MailProvider() = %q, want empty with neither set", got)
	}
	setEnv(t, map[string]string{"RESEND_API_KEY": "re_123"})
	if got := Load().MailProvider(); got != "resend" {
		t.Errorf("MailProvider() = %q, want resend", got)
	}
	setEnv(t, map[string]string{"SMTP_HOST": "smtp.example.test"})
	if got := Load().MailProvider(); got != "smtp" {
		t.Errorf("MailProvider() = %q, want smtp (priority over Resend)", got)
	}
}

func TestEffectiveAllowedOrigins(t *testing.T) {
	clearEnv(t, "ALLOWED_ORIGINS", "CORS_ALLOWED_ORIGINS", "FRONTEND_ORIGIN")
	cfg := Load()
	origins := cfg.EffectiveAllowedOrigins()
	if len(origins) < 3 {
		t.Fatalf("EffectiveAllowedOrigins() = %v, want at least 3 local dev origins", origins)
	}

	setEnv(t, map[string]string{"CORS_ALLOWED_ORIGINS": "https://a.test,https://b.test"})
	cfg = Load()
	origins = cfg.EffectiveAllowedOrigins()
	if !contains(origins, "https://a.test") || !contains(origins, "https://b.test") {
		t.Errorf("EffectiveAllowedOrigins() = %v, want a.test/b.test from CORS_ALLOWED_ORIGINS", origins)
	}

	setEnv(t, map[string]string{"ALLOWED_ORIGINS": "https://c.test"})
	cfg = Load()
	origins = cfg.EffectiveAllowedOrigins()
	if !contains(origins, "https://c.test") {
		t.Errorf("EffectiveAllowedOrigins() = %v, want c.test from ALLOWED_ORIGINS (higher priority)", origins)
	}
	if contains(origins, "https://a.test") {
		t.Errorf("EffectiveAllowedOrigins() = %v, ALLOWED_ORIGINS set should not also carry CORS_ALLOWED_ORIGINS", origins)
	}
}

func contains(items []string, want string) bool {
	for _, it := range items {
		if it == want {
			return true
		}
	}
	return false
}

func TestEmailAllowed(t *testing.T) {
	c := Config{}
	if !c.EmailAllowed("anyone@example.test") {
		t.Error("empty allow-lists should allow everyone")
	}
	c = Config{AllowedEmails: []string{"alice@example.test"}}
	if !c.EmailAllowed("alice@example.test") {
		t.Error("exact match should be allowed")
	}
	if c.EmailAllowed("bob@example.test") {
		t.Error("non-listed email should be rejected when ALLOWED_EMAILS is set")
	}
	c = Config{AllowedEmailDomains: []string{"example.test"}}
	if !c.EmailAllowed("bob@example.test") {
		t.Error("domain match should be allowed")
	}
	if c.EmailAllowed("bob@other.test") {
		t.Error("non-matching domain should be rejected")
	}
}

func TestLDAPMethodAvailable(t *testing.T) {
	c := Config{LDAP: LDAPConfig{URL: "ldap://dc.example.test:389"}}
	if c.LDAPMethodAvailable() {
		t.Error("plain ldap:// without START_TLS must not be offered")
	}
	c.LDAP.StartTLS = true
	if !c.LDAPMethodAvailable() {
		t.Error("ldap:// with START_TLS=true should be offered")
	}
	c = Config{LDAP: LDAPConfig{URL: "ldaps://dc.example.test:636"}}
	if !c.LDAPMethodAvailable() {
		t.Error("ldaps:// should always be offered")
	}
}

func TestValidateProductionRejectsPlaceholderSecret(t *testing.T) {
	c := Config{DatabaseURL: "postgres://x", AppEnv: "production", JWTSecret: "dev-insecure-secret-change-me", RoleWorkspaces: "auto", DeliveryProfile: "cloud", DeploymentProfile: "perimeter"}
	if err := c.Validate(); err == nil {
		t.Error("Validate() should reject a known placeholder JWT_SECRET on production")
	}
	c.JWTSecret = "a-real-random-secret-value"
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil with a real secret", err)
	}
}

func TestValidateReplicasRequireRedis(t *testing.T) {
	c := Config{DatabaseURL: "postgres://x", JWTSecret: "s", Replicas: 2, DeliveryProfile: "cloud", DeploymentProfile: "perimeter"}
	if err := c.Validate(); err == nil {
		t.Error("Validate() should reject GOOSAR_REPLICAS>1 without REDIS_URL")
	}
	c.RedisURL = "redis://localhost:6379/0"
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil once REDIS_URL is set", err)
	}
}

func TestValidateEnumFields(t *testing.T) {
	base := Config{DatabaseURL: "postgres://x", JWTSecret: "s", DeliveryProfile: "cloud", DeploymentProfile: "perimeter"}
	if err := base.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for a valid base config", err)
	}
	bad := base
	bad.DeliveryProfile = "nonsense"
	if err := bad.Validate(); err == nil {
		t.Error("Validate() should reject an unknown GOOSAR_DELIVERY_PROFILE")
	}
	bad = base
	bad.DeploymentProfile = "nonsense"
	if err := bad.Validate(); err == nil {
		t.Error("Validate() should reject an unknown GOOSAR_DEPLOYMENT_PROFILE")
	}
	bad = base
	bad.RoleWorkspaces = "nonsense"
	if err := bad.Validate(); err == nil {
		t.Error("Validate() should reject an unknown GOOSAR_ROLE_WORKSPACES")
	}
}

func TestDatabaseURLRequired(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Error("Validate() should require DATABASE_URL")
	}
}

func TestGetDurationFallsBackOnUnparsable(t *testing.T) {
	clearEnv(t, "GOOSAR_SHUTDOWN_HOLD_DURATION")
	setEnv(t, map[string]string{"GOOSAR_SHUTDOWN_HOLD_DURATION": "not-a-duration"})
	cfg := Load()
	if cfg.ShutdownHoldDuration != 0 {
		t.Errorf("ShutdownHoldDuration = %v, want fallback to 0 on unparsable value", cfg.ShutdownHoldDuration)
	}
	if len(cfg.ParseWarnings) == 0 {
		t.Error("ParseWarnings should record the unparsable GOOSAR_SHUTDOWN_HOLD_DURATION value")
	}
}

func TestJWTSecretPreviousCSV(t *testing.T) {
	clearEnv(t, "JWT_SECRET_PREVIOUS")
	setEnv(t, map[string]string{"JWT_SECRET_PREVIOUS": "old-one, old-two ,,"})
	cfg := Load()
	want := []string{"old-one", "old-two"}
	if len(cfg.JWTSecretPrevious) != len(want) {
		t.Fatalf("JWTSecretPrevious = %v, want %v", cfg.JWTSecretPrevious, want)
	}
	for i := range want {
		if cfg.JWTSecretPrevious[i] != want[i] {
			t.Errorf("JWTSecretPrevious[%d] = %q, want %q", i, cfg.JWTSecretPrevious[i], want[i])
		}
	}
}

func TestExternalImagesUnknownValueFailsClosed(t *testing.T) {
	clearEnv(t, "GOOSAR_EXTERNAL_IMAGES")
	setEnv(t, map[string]string{"GOOSAR_EXTERNAL_IMAGES": "typo-value"})
	cfg := Load()
	if cfg.ExternalImages != "block" {
		t.Errorf("ExternalImages = %q, want block (fail closed on unknown value)", cfg.ExternalImages)
	}
}

func TestCloudFleetURLPriority(t *testing.T) {
	clearEnv(t, "GOOSAR_CLOUD_FLEET_URL", "GOOSAR_FLEET_URL")
	setEnv(t, map[string]string{"GOOSAR_FLEET_URL": "https://fleet-alias.test"})
	if got := Load().CloudFleetURL; got != "https://fleet-alias.test" {
		t.Errorf("CloudFleetURL = %q, want fallback to GOOSAR_FLEET_URL", got)
	}
	setEnv(t, map[string]string{"GOOSAR_CLOUD_FLEET_URL": "https://fleet.test"})
	if got := Load().CloudFleetURL; got != "https://fleet.test" {
		t.Errorf("CloudFleetURL = %q, want GOOSAR_CLOUD_FLEET_URL to win", got)
	}
}
