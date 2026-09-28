// Package config читает конфигурацию сервера из переменных окружения.
// Имена, форматы и умолчания — те же, что документирует "Приложение.
// Переменные окружения сервера" в docs/50-api-contract.md (161 переменная)
// и e2e/contract/README.md: контракт ссылается на них по имени, поэтому
// переименовывать их (в отличие от колонок БД) нельзя. Сверка со списком
// приложения и решения по каждому расхождению — server2/docs/env-parity.md.
//
// Три категории переменных из приложения:
//   - поддержаны и реально влияют на поведение (приоритет — те, что
//     отмечены в приложении "в установке: да");
//   - читаются, но поведение в этой версии server2 не реализовано (сервер
//     логирует предупреждение при старте через Config.UnsupportedNotices);
//   - server2-специфичные имена без аналога в приложении (кэш-бастеры
//     внешних API для тестов, версия сборки и т. п.) — они тоже описаны в
//     server2/docs/env-parity.md, разбирать их можно не мигрируя.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config — снимок окружения процесса, прочитанный один раз при старте.
type Config struct {
	// --- БД и запуск -----------------------------------------------------

	Port                      string        // PORT
	DatabaseURL               string        // DATABASE_URL
	DatabaseMaxConns          int32         // DATABASE_MAX_CONNS, 0 = не переопределять (пул/URL решают сами)
	DatabaseMinConns          int32         // DATABASE_MIN_CONNS, 0 = не переопределять
	AppEnv                    string        // APP_ENV (production | иначе dev)
	Replicas                  int           // GOOSAR_REPLICAS, по умолчанию 1
	ShutdownHoldDuration      time.Duration // GOOSAR_SHUTDOWN_HOLD_DURATION
	MigrationLockTimeout      time.Duration // GOOSAR_MIGRATION_LOCK_TIMEOUT, по умолчанию 5s
	MigrationStatementTimeout time.Duration // GOOSAR_MIGRATION_STATEMENT_TIMEOUT, 0 = без таймаута
	MigrationLockRetries      int           // GOOSAR_MIGRATION_LOCK_RETRIES, по умолчанию 5

	// --- Аутентификация и сессии ------------------------------------------

	JWTSecret          string        // JWT_SECRET
	JWTSecretPrevious  []string      // JWT_SECRET_PREVIOUS — отозванные секреты, ещё годные для проверки подписи
	CookieDomain       string        // COOKIE_DOMAIN — атрибут Domain сессионной куки; IP молча игнорируется
	AuthTokenTTL       time.Duration // AUTH_TOKEN_TTL, по умолчанию 2592000с (30 дней); секунды целым или Go-длительность
	FrontendOrigin     string        // FRONTEND_ORIGIN
	AllowedOrigins     []string      // ALLOWED_ORIGINS — приоритетнее CORS_ALLOWED_ORIGINS
	CORSAllowedOrigins []string      // CORS_ALLOWED_ORIGINS — на ступень ниже; ниже — FrontendOrigin
	AppURL             string        // GOOSAR_APP_URL, по умолчанию = FrontendOrigin
	PublicURL          string        // GOOSAR_PUBLIC_URL
	TrustedProxies     []string      // GOOSAR_TRUSTED_PROXIES — CIDR-список доверенных обратных прокси

	AllowSignup              bool     // ALLOW_SIGNUP
	AllowedEmails            []string // ALLOWED_EMAILS
	AllowedEmailDomains      []string // ALLOWED_EMAIL_DOMAINS
	DisableWorkspaceCreation bool     // DISABLE_WORKSPACE_CREATION
	DevVerifyCode            string   // GOOSAR_DEV_VERIFICATION_CODE
	RoleWorkspaces           string   // GOOSAR_ROLE_WORKSPACES: auto | off | "" (валидируется в Validate)
	DeploymentAdminEmails    []string // GOOSAR_DEPLOYMENT_ADMIN_EMAILS
	RealtimeMetricsToken     string   // REALTIME_METRICS_TOKEN

	// --- Почта -------------------------------------------------------------

	ResendAPIKey    string // RESEND_API_KEY
	ResendFromEmail string // RESEND_FROM_EMAIL, по умолчанию noreply@goosar.ru

	SMTPHost        string // SMTP_HOST — если задан, приоритетнее Resend
	SMTPPort        int    // SMTP_PORT, по умолчанию 25
	SMTPUsername    string // SMTP_USERNAME
	SMTPPassword    string // SMTP_PASSWORD
	SMTPFromEmail   string // SMTP_FROM_EMAIL, наследует ResendFromEmail
	SMTPTLSInsecure bool   // SMTP_TLS_INSECURE
	SMTPTLS         string // SMTP_TLS: starttls (по умолчанию) | implicit (алиасы smtps/ssl)
	SMTPEhloName    string // SMTP_EHLO_NAME, по умолчанию — имя хоста машины

	// --- OIDC/LDAP/MFA -------------------------------------------------------

	AuthMethods []string // GOOSAR_AUTH_METHODS, пусто = только email
	TotpIssuer  string   // GOOSAR_TOTP_ISSUER, по умолчанию "Goosar"
	OIDC        OIDCConfig
	LDAP        LDAPConfig

	// --- Хранилище -----------------------------------------------------------

	S3Bucket           string // S3_BUCKET — наличие означает выбранный S3-backend
	S3Region           string // S3_REGION
	AWSAccessKeyID     string // AWS_ACCESS_KEY_ID
	AWSSecretAccessKey string // AWS_SECRET_ACCESS_KEY
	AWSEndpointURL     string // AWS_ENDPOINT_URL
	S3UsePathStyle     bool   // S3_USE_PATH_STYLE, по умолчанию true при заданном AWSEndpointURL, иначе false
	S3UsePathStyleSet  bool   // true, если переменная реально задана (иначе действует дефолт по AWSEndpointURL)

	AttachmentDownloadMode string        // ATTACHMENT_DOWNLOAD_MODE, по умолчанию auto
	AttachmentDownloadTTL  time.Duration // ATTACHMENT_DOWNLOAD_URL_TTL, по умолчанию 30m

	CloudfrontKeyPairID        string // CLOUDFRONT_KEY_PAIR_ID
	CloudfrontPrivateKeySecret string // CLOUDFRONT_PRIVATE_KEY_SECRET, по умолчанию goosar/cloudfront-signing-key
	CloudfrontPrivateKey       string // CLOUDFRONT_PRIVATE_KEY
	CloudfrontDomain           string // CLOUDFRONT_DOMAIN

	LocalUploadDir     string // LOCAL_UPLOAD_DIR, по умолчанию ./data/uploads
	LocalUploadBaseURL string // LOCAL_UPLOAD_BASE_URL

	ExternalImages string   // GOOSAR_EXTERNAL_IMAGES: allow (по умолчанию) | block | allowlist
	ImageHosts     []string // GOOSAR_IMAGE_HOSTS

	ProvisioningStore         string        // GOOSAR_PROVISIONING_STORE: "" | local | oci
	ProvisioningLocalPrefix   string        // GOOSAR_PROVISIONING_LOCAL_PREFIX, по умолчанию "provisioning"
	ProvisioningOCIURL        string        // GOOSAR_PROVISIONING_OCI_URL
	ProvisioningOCIRepository string        // GOOSAR_PROVISIONING_OCI_REPOSITORY
	ProvisioningOCIUsername   string        // GOOSAR_PROVISIONING_OCI_USERNAME
	ProvisioningOCIPassword   string        // GOOSAR_PROVISIONING_OCI_PASSWORD
	ProvisioningOCIPackages   string        // GOOSAR_PROVISIONING_OCI_PACKAGES
	ProvisioningSyncTimeout   time.Duration // GOOSAR_PROVISIONING_SYNC_TIMEOUT, по умолчанию 3m

	// --- Realtime/Redis (T-026: только чтение — см. UnsupportedNotices) ------

	RedisURL                  string        // REDIS_URL
	RedisDisableClientName    bool          // REDIS_DISABLE_CLIENT_NAME
	RealtimeRelayMode         string        // REALTIME_RELAY_MODE: sharded (по умолчанию) | dual | legacy
	RealtimeRelayShards       int           // REALTIME_RELAY_SHARDS
	RealtimeRelayStreamMaxlen int           // REALTIME_RELAY_STREAM_MAXLEN
	RealtimeRelayXReadCount   int           // REALTIME_RELAY_XREAD_COUNT
	RealtimeRelayXReadBlock   time.Duration // REALTIME_RELAY_XREAD_BLOCK
	RealtimeRelayReplayGrace  time.Duration // REALTIME_RELAY_REPLAY_GRACE

	RuntimeReconnectGrace time.Duration // GOOSAR_RUNTIME_RECONNECT_GRACE, по умолчанию 3h

	// --- Лимиты ----------------------------------------------------------------

	RateLimits RateLimits

	// --- Интеграции --------------------------------------------------------------

	GitHubAppSlug       string // GITHUB_APP_SLUG
	GitHubWebhookSecret string // GITHUB_WEBHOOK_SECRET
	GitHubAppID         string // GITHUB_APP_ID
	GitHubAppPrivateKey string // GITHUB_APP_PRIVATE_KEY
	GitHubToken         string // GITHUB_TOKEN — импорт навыков из GitHub

	VCSIntegrationEnabled bool   // GOOSAR_VCS_INTEGRATION_ENABLED
	VCSSecretKey          string // GOOSAR_VCS_SECRET_KEY
	VCSSecretKeyPrevious  string // GOOSAR_VCS_SECRET_KEY_PREVIOUS

	SlackSecretKey         string // GOOSAR_SLACK_SECRET_KEY
	SlackSecretKeyPrevious string // GOOSAR_SLACK_SECRET_KEY_PREVIOUS

	ComposioAPIKey       string // COMPOSIO_API_KEY
	ComposioStateSecret  string // COMPOSIO_STATE_SECRET, по умолчанию = JWTSecret
	ComposioCallbackBase string // COMPOSIO_CALLBACK_BASE_URL, по умолчанию = PublicURL/AppURL

	CloudFleetURL     string        // GOOSAR_CLOUD_FLEET_URL, приоритетнее GOOSAR_FLEET_URL
	CloudFleetTimeout time.Duration // GOOSAR_CLOUD_FLEET_TIMEOUT, по умолчанию 35s
	RuntimeConfigPath string        // GOOSAR_RUNTIME_CONFIG_PATH

	// --- LLM -----------------------------------------------------------------

	LLMAPIKey       string // GOOSAR_LLM_API_KEY — внутренний слой LLM-хелперов
	LLMBaseURL      string // GOOSAR_LLM_BASE_URL — обязателен вместе с LLMAPIKey
	LLMDefaultModel string // GOOSAR_LLM_DEFAULT_MODEL

	DeploymentLLMAPIBase string // GOOSAR_DEPLOYMENT_LLM_API_BASE — только подсказка клиентам
	DeploymentLLMModel   string // GOOSAR_DEPLOYMENT_LLM_MODEL — тоже подсказка

	// --- Деплой/политика ---------------------------------------------------------

	DeliveryProfile   string // GOOSAR_DELIVERY_PROFILE, по умолчанию cloud
	DeploymentProfile string // GOOSAR_DEPLOYMENT_PROFILE, по умолчанию perimeter

	SkillSourcesRaw    string   // GOOSAR_SKILL_SOURCES — сырое значение (может быть "none")
	SkillSources       []string // разобранный список источников (пусто, если SkillSourcesRaw == "none")
	SkillSourcesIsNone bool     // true, если явно задано "none"

	MCPAllowedHosts    []string // GOOSAR_MCP_ALLOWED_HOSTS
	MCPAllowedCommands []string // GOOSAR_MCP_ALLOWED_COMMANDS
	AllowedProviders   []string // GOOSAR_ALLOWED_PROVIDERS

	OfficialCloudHost string // GOOSAR_OFFICIAL_CLOUD_HOST
	MinDaemonVersion  string // GOOSAR_MIN_DAEMON_VERSION

	McpSecretKey         string // GOOSAR_MCP_SECRET_KEY
	McpSecretKeyPrevious string // GOOSAR_MCP_SECRET_KEY_PREVIOUS

	AuditRetentionDays int // GOOSAR_AUDIT_RETENTION_DAYS, по умолчанию 365; 0 = хранить вечно

	DeploymentJiraURL       string // GOOSAR_DEPLOYMENT_JIRA_URL
	DeploymentConfluenceURL string // GOOSAR_DEPLOYMENT_CONFLUENCE_URL
	DeploymentEWSURL        string // GOOSAR_DEPLOYMENT_EWS_URL
	DeploymentMailDomain    string // GOOSAR_DEPLOYMENT_MAIL_DOMAIN
	DeploymentBitrix24URL   string // GOOSAR_DEPLOYMENT_BITRIX24_URL
	DeploymentMcpGatewayURL string // GOOSAR_DEPLOYMENT_MCP_GATEWAY_URL

	// --- Хранение/retention --------------------------------------------------------

	SchedulerAuditRetention time.Duration // GOOSAR_SCHEDULER_AUDIT_RETENTION, по умолчанию 720h; 0 = хранить всё
	UploadGCGrace           time.Duration // GOOSAR_UPLOAD_GC_GRACE, по умолчанию 168h; 0 = отключить очистку
	HygieneSweepInterval    time.Duration // GOOSAR_HYGIENE_SWEEP_INTERVAL, по умолчанию 1h

	RetentionChat         time.Duration // GOOSAR_RETENTION_CHAT, пусто = вечно
	RetentionTasks        time.Duration // GOOSAR_RETENTION_TASKS, пусто = вечно
	RetentionClosedIssues time.Duration // GOOSAR_RETENTION_CLOSED_ISSUES, пусто = вечно
	RetentionActivity     time.Duration // GOOSAR_RETENTION_ACTIVITY, пусто = вечно
	AttachmentPurgeGrace  time.Duration // GOOSAR_ATTACHMENT_PURGE_GRACE, по умолчанию 168h

	ExportDir       string        // GOOSAR_EXPORT_DIR
	ExportTimeout   time.Duration // GOOSAR_EXPORT_TIMEOUT, по умолчанию 2h
	ExportMaxBytes  int64         // GOOSAR_EXPORT_MAX_BYTES
	ExportRetention time.Duration // GOOSAR_EXPORT_RETENTION, по умолчанию 168h

	// --- Наблюдаемость ------------------------------------------------------------

	MetricsAddr string // METRICS_ADDR, пусто = метрики выключены

	PosthogAPIKey string // POSTHOG_API_KEY
	PosthogHost   string // POSTHOG_HOST, по умолчанию https://us.i.posthog.com

	AnalyticsFrontendEnabled bool   // ANALYTICS_FRONTEND_ENABLED
	AnalyticsDisabled        bool   // ANALYTICS_DISABLED
	AnalyticsEnvironment     string // ANALYTICS_ENVIRONMENT, по умолчанию выводится из APP_ENV

	LogFormat string // LOG_FORMAT / GOOSAR_LOG_FORMAT (GOOSAR_-версия побеждает)
	LogLevel  string // LOG_LEVEL / GOOSAR_LOG_LEVEL (GOOSAR_-версия побеждает)

	// --- server2-специфичные переменные без аналога в приложении -------------
	// (server2/docs/env-parity.md, раздел "server2 читает, но нет в приложении")

	MigrateOnStart  bool // MIGRATE=true — применить миграции при старте
	MigrationsDir   string
	E2ECompatSQLDir string // E2E_COMPAT_SQL_DIR
	ServerVersion   string // GOOSAR_SERVER_VERSION, иначе "dev"

	CloudRuntimeAPIKey string // GOOSAR_CLOUDRUNTIME_API_KEY — Bearer для облачного fleet-прокси

	GitHubAPIBaseURL   string // GOOSAR_GITHUB_API_BASE_URL — override для тестов/self-host прокси
	SlackAPIBaseURL    string // GOOSAR_SLACK_API_BASE_URL
	ComposioAPIBaseURL string // GOOSAR_COMPOSIO_API_BASE_URL

	// ParseWarnings — нераспознанные значения переменных, где спецификация
	// требует не падать стартом, а откатиться на дефолт с предупреждением в
	// лог (например Go-длительность в группе "Хранение/retention"). main.go
	// логирует их после Load().
	ParseWarnings []string
}

// RateLimits — лимиты contract §1.5, per-instance in-process fallback.
type RateLimits struct {
	Auth           int      // RATE_LIMIT_AUTH — в минуту, по IP
	AuthVerify     int      // RATE_LIMIT_AUTH_VERIFY — в минуту, по IP
	AuthEmail      int      // RATE_LIMIT_AUTH_EMAIL — в минуту, по email
	MfaVerify      int      // RATE_LIMIT_MFA_VERIFY
	Token          int      // RATE_LIMIT_TOKEN — в час, по пользователю
	API            int      // RATE_LIMIT_API — в минуту, общий
	ContactSales   int      // RATE_LIMIT_CONTACT_SALES — в час, по IP
	Export         int      // RATE_LIMIT_EXPORT — в час
	Join           int      // RATE_LIMIT_JOIN — в час
	TrustedProxies []string // RATE_LIMIT_TRUSTED_PROXIES, пусто = падает на GOOSAR_TRUSTED_PROXIES
}

// OIDCConfig — пусто (IssuerURL == "") означает "OIDC не настроен".
type OIDCConfig struct {
	IssuerURL            string   // GOOSAR_OIDC_ISSUER
	ClientID             string   // GOOSAR_OIDC_CLIENT_ID
	ClientSecret         string   // GOOSAR_OIDC_CLIENT_SECRET
	RedirectURL          string   // GOOSAR_OIDC_REDIRECT_URL
	Scopes               []string // GOOSAR_OIDC_SCOPES, по умолчанию openid,profile,email; openid добавляется всегда
	AdminClaim           string   // GOOSAR_OIDC_ADMIN_CLAIM
	AdminValue           string   // GOOSAR_OIDC_ADMIN_VALUE
	DisplayName          string   // GOOSAR_OIDC_DISPLAY_NAME
	TrustUnverifiedEmail bool     // GOOSAR_OIDC_TRUST_UNVERIFIED_EMAIL
}

// LDAPConfig — пусто (URL == "") означает "LDAP не настроен".
type LDAPConfig struct {
	URL          string // GOOSAR_LDAP_URL
	StartTLS     bool   // GOOSAR_LDAP_START_TLS — обязателен вместе с ldap:// (без ldaps://)
	BindDN       string // GOOSAR_LDAP_BIND_DN
	BindPassword string // GOOSAR_LDAP_BIND_PASSWORD
	BaseDN       string // GOOSAR_LDAP_BASE_DN
	UserFilter   string // GOOSAR_LDAP_USER_FILTER
	EmailAttr    string // GOOSAR_LDAP_EMAIL_ATTR, по умолчанию mail
	NameAttr     string // GOOSAR_LDAP_NAME_ATTR, по умолчанию displayName
	AdminGroup   string // GOOSAR_LDAP_ADMIN_GROUP
	DisplayName  string // GOOSAR_LDAP_DISPLAY_NAME
}

// Load собирает Config из os.Environ(). Отсутствующие необязательные значения
// получают разумные дефолты для локальной разработки, а не приводят к
// ошибке — dev-режим сервера должен подниматься без внешней инфраструктуры.
func Load() Config {
	c := Config{}

	// --- БД и запуск ---
	c.Port = getenv("PORT", "8080")
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	c.DatabaseMaxConns = c.getInt32("DATABASE_MAX_CONNS", 0)
	c.DatabaseMinConns = c.getInt32("DATABASE_MIN_CONNS", 0)
	c.AppEnv = getenv("APP_ENV", "development")
	c.Replicas = c.getInt("GOOSAR_REPLICAS", 1)
	c.ShutdownHoldDuration = c.getDuration("GOOSAR_SHUTDOWN_HOLD_DURATION", 0)
	c.MigrationLockTimeout = c.getDuration("GOOSAR_MIGRATION_LOCK_TIMEOUT", 5*time.Second)
	c.MigrationStatementTimeout = c.getDuration("GOOSAR_MIGRATION_STATEMENT_TIMEOUT", 0)
	c.MigrationLockRetries = c.getInt("GOOSAR_MIGRATION_LOCK_RETRIES", 5)

	// --- Аутентификация и сессии ---
	c.JWTSecret = getenv("JWT_SECRET", "dev-insecure-secret-change-me")
	c.JWTSecretPrevious = getCSV("JWT_SECRET_PREVIOUS")
	c.CookieDomain = os.Getenv("COOKIE_DOMAIN")
	c.AuthTokenTTL = c.getDurationOrSeconds("AUTH_TOKEN_TTL", 2592000*time.Second)
	c.FrontendOrigin = getenv("FRONTEND_ORIGIN", "http://localhost:3199")
	c.AllowedOrigins = getCSV("ALLOWED_ORIGINS")
	c.CORSAllowedOrigins = getCSV("CORS_ALLOWED_ORIGINS")
	c.PublicURL = strings.TrimRight(os.Getenv("GOOSAR_PUBLIC_URL"), "/")
	c.AppURL = getenv("GOOSAR_APP_URL", c.FrontendOrigin)
	c.TrustedProxies = getCSV("GOOSAR_TRUSTED_PROXIES")

	c.AllowSignup = getBool("ALLOW_SIGNUP", true)
	c.AllowedEmails = getCSV("ALLOWED_EMAILS")
	c.AllowedEmailDomains = getCSV("ALLOWED_EMAIL_DOMAINS")
	c.DisableWorkspaceCreation = getBool("DISABLE_WORKSPACE_CREATION", false)
	c.DevVerifyCode = os.Getenv("GOOSAR_DEV_VERIFICATION_CODE")
	c.RoleWorkspaces = strings.TrimSpace(os.Getenv("GOOSAR_ROLE_WORKSPACES"))
	c.DeploymentAdminEmails = getCSVLower("GOOSAR_DEPLOYMENT_ADMIN_EMAILS")
	c.RealtimeMetricsToken = os.Getenv("REALTIME_METRICS_TOKEN")

	// --- Почта ---
	c.ResendAPIKey = os.Getenv("RESEND_API_KEY")
	c.ResendFromEmail = getenv("RESEND_FROM_EMAIL", "noreply@goosar.ru")
	c.SMTPHost = os.Getenv("SMTP_HOST")
	c.SMTPPort = c.getInt("SMTP_PORT", 25)
	c.SMTPUsername = os.Getenv("SMTP_USERNAME")
	c.SMTPPassword = os.Getenv("SMTP_PASSWORD")
	c.SMTPFromEmail = getenv("SMTP_FROM_EMAIL", c.ResendFromEmail)
	c.SMTPTLSInsecure = getBool("SMTP_TLS_INSECURE", false)
	c.SMTPTLS = normalizeSMTPTLS(os.Getenv("SMTP_TLS"), c.SMTPPort)
	c.SMTPEhloName = getenv("SMTP_EHLO_NAME", hostname())

	// --- OIDC/LDAP/MFA ---
	c.AuthMethods = parseAuthMethods(os.Getenv("GOOSAR_AUTH_METHODS"))
	c.TotpIssuer = getenv("GOOSAR_TOTP_ISSUER", "Goosar")
	c.OIDC = OIDCConfig{
		IssuerURL:            strings.TrimRight(os.Getenv("GOOSAR_OIDC_ISSUER"), "/"),
		ClientID:             os.Getenv("GOOSAR_OIDC_CLIENT_ID"),
		ClientSecret:         os.Getenv("GOOSAR_OIDC_CLIENT_SECRET"),
		RedirectURL:          os.Getenv("GOOSAR_OIDC_REDIRECT_URL"),
		Scopes:               oidcScopes(os.Getenv("GOOSAR_OIDC_SCOPES")),
		AdminClaim:           os.Getenv("GOOSAR_OIDC_ADMIN_CLAIM"),
		AdminValue:           os.Getenv("GOOSAR_OIDC_ADMIN_VALUE"),
		DisplayName:          getenv("GOOSAR_OIDC_DISPLAY_NAME", "Single sign-on"),
		TrustUnverifiedEmail: getBool("GOOSAR_OIDC_TRUST_UNVERIFIED_EMAIL", false),
	}
	c.LDAP = LDAPConfig{
		URL:          os.Getenv("GOOSAR_LDAP_URL"),
		StartTLS:     getBool("GOOSAR_LDAP_START_TLS", false),
		BindDN:       os.Getenv("GOOSAR_LDAP_BIND_DN"),
		BindPassword: os.Getenv("GOOSAR_LDAP_BIND_PASSWORD"),
		BaseDN:       os.Getenv("GOOSAR_LDAP_BASE_DN"),
		UserFilter:   getenv("GOOSAR_LDAP_USER_FILTER", "(|(uid=%s)(sAMAccountName=%s))"),
		EmailAttr:    getenv("GOOSAR_LDAP_EMAIL_ATTR", "mail"),
		NameAttr:     getenv("GOOSAR_LDAP_NAME_ATTR", "displayName"),
		AdminGroup:   os.Getenv("GOOSAR_LDAP_ADMIN_GROUP"),
		DisplayName:  getenv("GOOSAR_LDAP_DISPLAY_NAME", "Corporate directory"),
	}

	// --- Хранилище ---
	c.S3Bucket = os.Getenv("S3_BUCKET")
	c.S3Region = os.Getenv("S3_REGION")
	c.AWSAccessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	c.AWSSecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	c.AWSEndpointURL = os.Getenv("AWS_ENDPOINT_URL")
	if v := os.Getenv("S3_USE_PATH_STYLE"); v != "" {
		c.S3UsePathStyleSet = true
		c.S3UsePathStyle = getBool("S3_USE_PATH_STYLE", false)
	} else {
		c.S3UsePathStyle = c.AWSEndpointURL != ""
	}
	c.AttachmentDownloadMode = getenv("ATTACHMENT_DOWNLOAD_MODE", "auto")
	c.AttachmentDownloadTTL = c.getDurationOrMinutes("ATTACHMENT_DOWNLOAD_URL_TTL", 30*time.Minute)
	c.CloudfrontKeyPairID = os.Getenv("CLOUDFRONT_KEY_PAIR_ID")
	c.CloudfrontPrivateKeySecret = getenv("CLOUDFRONT_PRIVATE_KEY_SECRET", "goosar/cloudfront-signing-key")
	c.CloudfrontPrivateKey = os.Getenv("CLOUDFRONT_PRIVATE_KEY")
	c.CloudfrontDomain = os.Getenv("CLOUDFRONT_DOMAIN")
	c.LocalUploadDir = getenv("LOCAL_UPLOAD_DIR", "./data/uploads")
	c.LocalUploadBaseURL = os.Getenv("LOCAL_UPLOAD_BASE_URL")
	c.ExternalImages = normalizeExternalImages(getenv("GOOSAR_EXTERNAL_IMAGES", "allow"))
	c.ImageHosts = getCSV("GOOSAR_IMAGE_HOSTS")
	c.ProvisioningStore = strings.ToLower(strings.TrimSpace(os.Getenv("GOOSAR_PROVISIONING_STORE")))
	c.ProvisioningLocalPrefix = getenv("GOOSAR_PROVISIONING_LOCAL_PREFIX", "provisioning")
	c.ProvisioningOCIURL = os.Getenv("GOOSAR_PROVISIONING_OCI_URL")
	c.ProvisioningOCIRepository = os.Getenv("GOOSAR_PROVISIONING_OCI_REPOSITORY")
	c.ProvisioningOCIUsername = os.Getenv("GOOSAR_PROVISIONING_OCI_USERNAME")
	c.ProvisioningOCIPassword = os.Getenv("GOOSAR_PROVISIONING_OCI_PASSWORD")
	c.ProvisioningOCIPackages = os.Getenv("GOOSAR_PROVISIONING_OCI_PACKAGES")
	c.ProvisioningSyncTimeout = c.getDuration("GOOSAR_PROVISIONING_SYNC_TIMEOUT", 3*time.Minute)

	// --- Realtime/Redis ---
	c.RedisURL = os.Getenv("REDIS_URL")
	c.RedisDisableClientName = getBool("REDIS_DISABLE_CLIENT_NAME", false)
	c.RealtimeRelayMode = normalizeRelayMode(os.Getenv("REALTIME_RELAY_MODE"))
	c.RealtimeRelayShards = c.getInt("REALTIME_RELAY_SHARDS", 8)
	c.RealtimeRelayStreamMaxlen = c.getInt("REALTIME_RELAY_STREAM_MAXLEN", 10000)
	c.RealtimeRelayXReadCount = c.getInt("REALTIME_RELAY_XREAD_COUNT", 100)
	c.RealtimeRelayXReadBlock = c.getDuration("REALTIME_RELAY_XREAD_BLOCK", 5*time.Second)
	c.RealtimeRelayReplayGrace = c.getDuration("REALTIME_RELAY_REPLAY_GRACE", 30*time.Second)
	c.RuntimeReconnectGrace = c.getDuration("GOOSAR_RUNTIME_RECONNECT_GRACE", 3*time.Hour)

	// --- Лимиты ---
	c.RateLimits = RateLimits{
		Auth:           c.getInt("RATE_LIMIT_AUTH", 5),
		AuthVerify:     c.getInt("RATE_LIMIT_AUTH_VERIFY", 20),
		AuthEmail:      c.getInt("RATE_LIMIT_AUTH_EMAIL", 10),
		MfaVerify:      c.getInt("RATE_LIMIT_MFA_VERIFY", 10),
		Token:          c.getInt("RATE_LIMIT_TOKEN", 20),
		API:            c.getInt("RATE_LIMIT_API", 600),
		ContactSales:   c.getInt("RATE_LIMIT_CONTACT_SALES", 5),
		Export:         c.getInt("RATE_LIMIT_EXPORT", 3),
		Join:           c.getInt("RATE_LIMIT_JOIN", 20),
		TrustedProxies: getCSV("RATE_LIMIT_TRUSTED_PROXIES"),
	}

	// --- Интеграции ---
	c.GitHubAppSlug = os.Getenv("GITHUB_APP_SLUG")
	c.GitHubWebhookSecret = os.Getenv("GITHUB_WEBHOOK_SECRET")
	c.GitHubAppID = os.Getenv("GITHUB_APP_ID")
	c.GitHubAppPrivateKey = os.Getenv("GITHUB_APP_PRIVATE_KEY")
	c.GitHubToken = os.Getenv("GITHUB_TOKEN")
	c.VCSIntegrationEnabled = getBool("GOOSAR_VCS_INTEGRATION_ENABLED", false)
	c.VCSSecretKey = os.Getenv("GOOSAR_VCS_SECRET_KEY")
	c.VCSSecretKeyPrevious = os.Getenv("GOOSAR_VCS_SECRET_KEY_PREVIOUS")
	c.SlackSecretKey = os.Getenv("GOOSAR_SLACK_SECRET_KEY")
	c.SlackSecretKeyPrevious = os.Getenv("GOOSAR_SLACK_SECRET_KEY_PREVIOUS")
	c.ComposioAPIKey = os.Getenv("COMPOSIO_API_KEY")
	c.ComposioStateSecret = os.Getenv("COMPOSIO_STATE_SECRET")
	c.ComposioCallbackBase = os.Getenv("COMPOSIO_CALLBACK_BASE_URL")
	c.CloudFleetURL = firstNonEmpty(os.Getenv("GOOSAR_CLOUD_FLEET_URL"), os.Getenv("GOOSAR_FLEET_URL"))
	c.CloudFleetTimeout = c.getDuration("GOOSAR_CLOUD_FLEET_TIMEOUT", 35*time.Second)
	c.RuntimeConfigPath = os.Getenv("GOOSAR_RUNTIME_CONFIG_PATH")

	// --- LLM ---
	c.LLMAPIKey = os.Getenv("GOOSAR_LLM_API_KEY")
	c.LLMBaseURL = os.Getenv("GOOSAR_LLM_BASE_URL")
	c.LLMDefaultModel = getenv("GOOSAR_LLM_DEFAULT_MODEL", "gpt-4o-mini")
	c.DeploymentLLMAPIBase = os.Getenv("GOOSAR_DEPLOYMENT_LLM_API_BASE")
	c.DeploymentLLMModel = os.Getenv("GOOSAR_DEPLOYMENT_LLM_MODEL")

	// --- Деплой/политика ---
	c.DeliveryProfile = strings.ToLower(strings.TrimSpace(os.Getenv("GOOSAR_DELIVERY_PROFILE")))
	if c.DeliveryProfile == "" {
		c.DeliveryProfile = "cloud"
	}
	c.DeploymentProfile = strings.ToLower(strings.TrimSpace(os.Getenv("GOOSAR_DEPLOYMENT_PROFILE")))
	if c.DeploymentProfile == "" {
		c.DeploymentProfile = "perimeter"
	}
	c.SkillSourcesRaw = strings.TrimSpace(os.Getenv("GOOSAR_SKILL_SOURCES"))
	if strings.EqualFold(c.SkillSourcesRaw, "none") {
		c.SkillSourcesIsNone = true
	} else {
		c.SkillSources = getCSV("GOOSAR_SKILL_SOURCES")
	}
	c.MCPAllowedHosts = getCSV("GOOSAR_MCP_ALLOWED_HOSTS")
	c.MCPAllowedCommands = getCSV("GOOSAR_MCP_ALLOWED_COMMANDS")
	c.AllowedProviders = getCSV("GOOSAR_ALLOWED_PROVIDERS")
	c.OfficialCloudHost = os.Getenv("GOOSAR_OFFICIAL_CLOUD_HOST")
	c.MinDaemonVersion = os.Getenv("GOOSAR_MIN_DAEMON_VERSION")
	c.McpSecretKey = os.Getenv("GOOSAR_MCP_SECRET_KEY")
	c.McpSecretKeyPrevious = os.Getenv("GOOSAR_MCP_SECRET_KEY_PREVIOUS")
	c.AuditRetentionDays = c.getInt("GOOSAR_AUDIT_RETENTION_DAYS", 365)
	c.DeploymentJiraURL = os.Getenv("GOOSAR_DEPLOYMENT_JIRA_URL")
	c.DeploymentConfluenceURL = os.Getenv("GOOSAR_DEPLOYMENT_CONFLUENCE_URL")
	c.DeploymentEWSURL = os.Getenv("GOOSAR_DEPLOYMENT_EWS_URL")
	c.DeploymentMailDomain = os.Getenv("GOOSAR_DEPLOYMENT_MAIL_DOMAIN")
	c.DeploymentBitrix24URL = os.Getenv("GOOSAR_DEPLOYMENT_BITRIX24_URL")
	c.DeploymentMcpGatewayURL = os.Getenv("GOOSAR_DEPLOYMENT_MCP_GATEWAY_URL")

	// --- Хранение/retention ---
	c.SchedulerAuditRetention = c.getDuration("GOOSAR_SCHEDULER_AUDIT_RETENTION", 720*time.Hour)
	c.UploadGCGrace = c.getDuration("GOOSAR_UPLOAD_GC_GRACE", 168*time.Hour)
	c.HygieneSweepInterval = c.getDuration("GOOSAR_HYGIENE_SWEEP_INTERVAL", time.Hour)
	c.RetentionChat = c.getDuration("GOOSAR_RETENTION_CHAT", 0)
	c.RetentionTasks = c.getDuration("GOOSAR_RETENTION_TASKS", 0)
	c.RetentionClosedIssues = c.getDuration("GOOSAR_RETENTION_CLOSED_ISSUES", 0)
	c.RetentionActivity = c.getDuration("GOOSAR_RETENTION_ACTIVITY", 0)
	c.AttachmentPurgeGrace = c.getDuration("GOOSAR_ATTACHMENT_PURGE_GRACE", 168*time.Hour)
	c.ExportDir = os.Getenv("GOOSAR_EXPORT_DIR")
	c.ExportTimeout = c.getDuration("GOOSAR_EXPORT_TIMEOUT", 2*time.Hour)
	c.ExportMaxBytes = c.getInt64("GOOSAR_EXPORT_MAX_BYTES", 5*1024*1024*1024) // 5 GiB встроенный дефолт
	c.ExportRetention = c.getDuration("GOOSAR_EXPORT_RETENTION", 168*time.Hour)

	// --- Наблюдаемость ---
	c.MetricsAddr = os.Getenv("METRICS_ADDR")
	c.PosthogAPIKey = os.Getenv("POSTHOG_API_KEY")
	c.PosthogHost = getenv("POSTHOG_HOST", "https://us.i.posthog.com")
	c.AnalyticsFrontendEnabled = getBool("ANALYTICS_FRONTEND_ENABLED", false)
	c.AnalyticsDisabled = getBool("ANALYTICS_DISABLED", false)
	c.AnalyticsEnvironment = getenv("ANALYTICS_ENVIRONMENT", normalizeAnalyticsEnv(c.AppEnv))
	c.LogFormat = firstNonEmpty(os.Getenv("GOOSAR_LOG_FORMAT"), os.Getenv("LOG_FORMAT"))
	c.LogLevel = firstNonEmpty(os.Getenv("GOOSAR_LOG_LEVEL"), os.Getenv("LOG_LEVEL"))

	// --- server2-специфичные, без аналога в приложении ---
	c.MigrateOnStart = getBool("MIGRATE", false)
	c.MigrationsDir = getenv("MIGRATIONS_DIR", "server2/migrations")
	c.E2ECompatSQLDir = os.Getenv("E2E_COMPAT_SQL_DIR")
	c.ServerVersion = getenv("GOOSAR_SERVER_VERSION", "dev")
	c.CloudRuntimeAPIKey = os.Getenv("GOOSAR_CLOUDRUNTIME_API_KEY")
	c.GitHubAPIBaseURL = os.Getenv("GOOSAR_GITHUB_API_BASE_URL")
	c.SlackAPIBaseURL = os.Getenv("GOOSAR_SLACK_API_BASE_URL")
	c.ComposioAPIBaseURL = os.Getenv("GOOSAR_COMPOSIO_API_BASE_URL")

	if c.ComposioStateSecret == "" {
		c.ComposioStateSecret = c.JWTSecret
	}
	if c.ComposioCallbackBase == "" {
		if c.PublicURL != "" {
			c.ComposioCallbackBase = c.PublicURL
		} else {
			c.ComposioCallbackBase = c.AppURL
		}
	}

	return c
}

// IsProduction — APP_ENV=production включает проверки безопасности при
// старте и отключает dev-код подтверждения по почте.
func (c Config) IsProduction() bool { return strings.EqualFold(c.AppEnv, "production") }

// DevCodeEnabled — фиксированный код логина разрешён только вне production и
// только если переменная задана (пусто = отключено даже вне prod).
func (c Config) DevCodeEnabled() bool { return !c.IsProduction() && c.DevVerifyCode != "" }

// AuthMethodEnabled — метод входа перечислен в GOOSAR_AUTH_METHODS (пусто =
// только email). Метод, не перечисленный здесь, закрыт на самом эндпойнте,
// даже если ниже для него всё настроено (contract, группа OIDC/LDAP/MFA).
func (c Config) AuthMethodEnabled(method string) bool {
	for _, m := range c.AuthMethods {
		if m == method {
			return true
		}
	}
	return false
}

// LDAPMethodAvailable — LDAP предлагается только когда URL задан И (схема
// ldaps://, ИЛИ явно включён GOOSAR_LDAP_START_TLS) — простой bind по
// обычному ldap:// без StartTLS отправляет пароль в открытом виде.
func (c Config) LDAPMethodAvailable() bool {
	if c.LDAP.URL == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(c.LDAP.URL), "ldaps://") {
		return true
	}
	return c.LDAP.StartTLS
}

// EmailAllowed — allow-list ALLOWED_EMAILS/ALLOWED_EMAIL_DOMAINS для
// входа/регистрации; оба пустых = без ограничений.
func (c Config) EmailAllowed(email string) bool {
	if len(c.AllowedEmails) == 0 && len(c.AllowedEmailDomains) == 0 {
		return true
	}
	email = strings.ToLower(strings.TrimSpace(email))
	for _, e := range c.AllowedEmails {
		if strings.ToLower(e) == email {
			return true
		}
	}
	domain := email
	if i := strings.LastIndexByte(email, '@'); i >= 0 {
		domain = email[i+1:]
	}
	for _, d := range c.AllowedEmailDomains {
		if strings.EqualFold(d, domain) {
			return true
		}
	}
	return false
}

// MailProvider — вычисляемый (не читаемый напрямую из окружения) выбор
// транспорта: SMTP приоритетнее Resend, если задан SMTP_HOST (contract:
// "Если задан [SMTP_HOST] — имеет приоритет над Resend").
func (c Config) MailProvider() string {
	switch {
	case c.SMTPHost != "":
		return "smtp"
	case c.ResendAPIKey != "":
		return "resend"
	default:
		return ""
	}
}

// --- парсинг -----------------------------------------------------------------

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func (c *Config) getInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		c.warnf("%s=%q не является целым числом, использован дефолт %d", key, v, def)
		return def
	}
	return n
}

func (c *Config) getInt32(key string, def int32) int32 {
	return int32(c.getInt(key, int(def)))
}

func (c *Config) getInt64(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		c.warnf("%s=%q не является целым числом, использован дефолт %d", key, v, def)
		return def
	}
	return n
}

// getDuration разбирает Go-длительность ("300s", "5m", "168h"); пустое
// значение — def, нераспознанное — предупреждение и откат к def (contract,
// группа "Хранение/retention": "Нераспознанное значение — предупреждение в
// лог и откат к дефолту" — применяется здесь ко всем длительностям
// единообразно).
func (c *Config) getDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		c.warnf("%s=%q не является Go-длительностью, использован дефолт %s", key, v, def)
		return def
	}
	return d
}

// getDurationOrSeconds — как getDuration, но значение из одних цифр без
// суффикса читается как целые секунды (AUTH_TOKEN_TTL: "секунды целым числом
// или Go-длительность").
func (c *Config) getDurationOrSeconds(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Duration(n) * time.Second
	}
	return c.getDuration(key, def)
}

// getDurationOrMinutes — как getDuration, но целые числа без суффикса
// трактуются как минуты (ATTACHMENT_DOWNLOAD_URL_TTL: старое поведение
// server2 читало эту переменную как целые минуты — оставлено как
// совместимость в придачу к Go-длительности из контракта).
func (c *Config) getDurationOrMinutes(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Duration(n) * time.Minute
	}
	return c.getDuration(key, def)
}

func (c *Config) warnf(format string, args ...any) {
	c.ParseWarnings = append(c.ParseWarnings, fmt.Sprintf(format, args...))
}

// getCSV — список через запятую, с обрезкой пробелов и отбрасыванием пустых
// элементов; незаданная переменная — nil.
func getCSV(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getCSVLower(key string) []string {
	items := getCSV(key)
	out := make([]string, len(items))
	for i, v := range items {
		out[i] = strings.ToLower(v)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "localhost"
	}
	return h
}

// normalizeSMTPTLS — starttls (по умолчанию) | implicit (алиасы smtps/ssl);
// автоматически implicit при порте 465, если переменная не задана явно
// (contract: "implicit обязателен для провайдеров, отдающих только порт
// 465 без STARTTLS").
func normalizeSMTPTLS(v string, port int) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "implicit", "smtps", "ssl":
		return "implicit"
	case "starttls":
		return "starttls"
	case "":
		if port == 465 {
			return "implicit"
		}
		return "starttls"
	default:
		return "starttls"
	}
}

func normalizeExternalImages(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "allow":
		return "allow"
	case "block":
		return "block"
	case "allowlist":
		return "allowlist"
	case "":
		return "allow"
	default:
		// "любое другое значение трактуется как block (отказ закрытый)"
		return "block"
	}
}

func normalizeRelayMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "sharded", "":
		return "sharded"
	case "dual":
		return "dual"
	case "legacy":
		return "legacy"
	default:
		return "sharded"
	}
}

func normalizeAnalyticsEnv(appEnv string) string {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "production":
		return "production"
	case "staging":
		return "staging"
	default:
		return "dev"
	}
}

// parseAuthMethods — пусто = только email (contract: "Пусто = только
// email"); иначе — ровно перечисленное (email нужно перечислить явно, чтобы
// остаться доступным вместе с oidc/ldap).
func parseAuthMethods(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return []string{"email"}
	}
	return getCSV("GOOSAR_AUTH_METHODS")
}

// oidcScopes — по умолчанию openid,profile,email; openid добавляется даже
// если не перечислен явно.
func oidcScopes(v string) []string {
	scopes := getCSV("GOOSAR_OIDC_SCOPES")
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	for _, s := range scopes {
		if s == "openid" {
			return scopes
		}
	}
	return append([]string{"openid"}, scopes...)
}

// knownInsecureSecrets — известные плейсхолдеры JWT_SECRET/_PREVIOUS,
// которые на APP_ENV=production — отказ старта (contract: "пустое значение
// или известный плейсхолдер — отказ старта"). Список не претендует на
// исчерпывающую эвристику "слабого" секрета — только буквальные значения,
// которые реально встречаются как дефолт/пример в этом репозитории и в
// типичных how-to.
var knownInsecureSecrets = map[string]bool{
	"":                              true,
	"dev-insecure-secret-change-me": true,
	"changeme":                      true,
	"change-me":                     true,
	"change_me":                     true,
	"secret":                        true,
	"secretkey":                     true,
	"your-secret-here":              true,
	"your-secret-key":               true,
	"replace-me":                    true,
	"replace_me":                    true,
	"insecure":                      true,
	"test-secret":                   true,
	"example":                       true,
}

func isKnownInsecureSecret(s string) bool {
	return knownInsecureSecrets[strings.ToLower(strings.TrimSpace(s))]
}

// Validate проверяет обязательные для запуска сервера значения и
// перечислимые переменные, для которых contract требует "иное значение —
// отказ старта" вместо мягкого отката к дефолту.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: DATABASE_URL не задан")
	}
	if c.IsProduction() {
		if isKnownInsecureSecret(c.JWTSecret) {
			return fmt.Errorf("config: APP_ENV=production и JWT_SECRET пуст или является известным плейсхолдером — задайте случайный секрет (openssl rand -hex 32)")
		}
		for _, s := range c.JWTSecretPrevious {
			if isKnownInsecureSecret(s) {
				return fmt.Errorf("config: APP_ENV=production и JWT_SECRET_PREVIOUS содержит известный плейсхолдер")
			}
		}
	}
	if c.Replicas > 1 && c.RedisURL == "" {
		return fmt.Errorf("config: GOOSAR_REPLICAS=%d требует REDIS_URL — без него у каждой реплики свой лимитер и свой realtime-хаб", c.Replicas)
	}
	switch strings.ToLower(strings.TrimSpace(c.RoleWorkspaces)) {
	case "", "auto", "off":
	default:
		return fmt.Errorf("config: GOOSAR_ROLE_WORKSPACES=%q — допустимы только auto/off", c.RoleWorkspaces)
	}
	switch c.DeliveryProfile {
	case "cloud", "perimeter":
	default:
		return fmt.Errorf("config: GOOSAR_DELIVERY_PROFILE=%q — допустимы только cloud/perimeter", c.DeliveryProfile)
	}
	switch c.DeploymentProfile {
	case "perimeter", "demo", "dev", "local":
	default:
		return fmt.Errorf("config: GOOSAR_DEPLOYMENT_PROFILE=%q — допустимы только perimeter/demo/dev/local", c.DeploymentProfile)
	}
	if c.SkillSourcesRaw != "" && !c.SkillSourcesIsNone {
		for _, s := range c.SkillSources {
			switch s {
			case "clawhub", "github", "skillssh":
			default:
				return fmt.Errorf("config: GOOSAR_SKILL_SOURCES содержит неизвестный источник %q — допустимы clawhub, github, skillssh, none", s)
			}
		}
	}
	return nil
}

// UnsupportedNotices — переменные из докс-приложения, которые Config читает,
// но чьё поведение эта версия server2 не реализует (server2/docs/env-parity.md
// перечисляет их подробно): main.go логирует эти строки один раз при старте
// уровнем warn, как и требует раздел "Спорные места" контракта — тихая
// деградация без объяснения в логе была бы хуже честного предупреждения.
func (c Config) UnsupportedNotices() []string {
	var out []string
	note := func(env, what string) {
		out = append(out, fmt.Sprintf("%s задан, но %s — не поддерживается в этой версии server2 (см. server2/docs/env-parity.md)", env, what))
	}
	if c.RedisURL != "" {
		note("REDIS_URL", "многоузловой лимитер/realtime fan-out/кеш токенов через Redis")
	}
	if c.RedisDisableClientName {
		note("REDIS_DISABLE_CLIENT_NAME", "Redis-клиент")
	}
	if os.Getenv("REALTIME_RELAY_MODE") != "" || os.Getenv("REALTIME_RELAY_SHARDS") != "" {
		note("REALTIME_RELAY_*", "realtime-relay поверх Redis Streams")
	}
	if c.S3Bucket != "" {
		note("S3_BUCKET", "загрузка вложений в S3 (backend вложений — только локальный диск)")
	}
	if c.AWSAccessKeyID != "" || c.AWSSecretAccessKey != "" || c.AWSEndpointURL != "" {
		note("AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY/AWS_ENDPOINT_URL", "S3-совместимое хранилище")
	}
	if c.CloudfrontKeyPairID != "" || c.CloudfrontPrivateKey != "" || c.CloudfrontDomain != "" {
		note("CLOUDFRONT_*", "подпись ссылок CloudFront")
	}
	if c.ProvisioningStore == "oci" {
		note("GOOSAR_PROVISIONING_STORE=oci", "зеркалирование пакетов из OCI/ORAS-реестра")
	}
	if c.RuntimeConfigPath != "" {
		note("GOOSAR_RUNTIME_CONFIG_PATH", "переопределение встроенного каталога рантаймов/моделей")
	}
	if c.LLMAPIKey != "" || c.LLMBaseURL != "" {
		note("GOOSAR_LLM_API_KEY/GOOSAR_LLM_BASE_URL", "внутренний слой LLM-хелперов (например заголовок чата через LLM)")
	}
	if c.GitHubToken != "" {
		note("GITHUB_TOKEN", "импорт навыков из GitHub с явным токеном")
	}
	if !c.SkillSourcesIsNone && len(c.SkillSources) > 0 {
		note("GOOSAR_SKILL_SOURCES", "включение/выключение конкретных внешних источников навыков (сетевой доступ ограничен только флагом available в ответе, не enforcement на fetch)")
	}
	if len(c.MCPAllowedHosts) > 0 || len(c.MCPAllowedCommands) > 0 {
		note("GOOSAR_MCP_ALLOWED_HOSTS/GOOSAR_MCP_ALLOWED_COMMANDS", "проверка хостов/команд записей MCP-сервера при сохранении mcp_config")
	}
	if len(c.AllowedProviders) > 0 {
		note("GOOSAR_ALLOWED_PROVIDERS", "запрет назначения задач рантаймам вне списка (сейчас только отражается в /api/config)")
	}
	if c.OfficialCloudHost != "" {
		note("GOOSAR_OFFICIAL_CLOUD_HOST", "отличение официального облака от прочих развёртываний")
	}
	if c.SchedulerAuditRetention != 720*time.Hour || os.Getenv("GOOSAR_HYGIENE_SWEEP_INTERVAL") != "" {
		note("GOOSAR_SCHEDULER_AUDIT_RETENTION/GOOSAR_HYGIENE_SWEEP_INTERVAL", "периодический фоновый сборщик в процессе сервера (доступно только вручную через goosar_admin purge/gc-uploads)")
	}
	if c.ExportDir != "" {
		note("GOOSAR_EXPORT_DIR", "отдельный каталог экспорта (server2 пишет экспорт через internal/asset.Storage, тот же backend, что вложения)")
	}
	if c.ExportMaxBytes != 5*1024*1024*1024 {
		note("GOOSAR_EXPORT_MAX_BYTES", "ограничение размера архива экспорта")
	}
	if c.MetricsAddr != "" {
		note("METRICS_ADDR", "HTTP-listener метрик Prometheus")
	}
	if c.PosthogAPIKey != "" {
		note("POSTHOG_API_KEY", "отправка продуктовых событий в PostHog")
	}
	if c.AnalyticsFrontendEnabled {
		note("ANALYTICS_FRONTEND_ENABLED", "передача ключа PostHog в браузер через /api/config")
	}
	return out
}
