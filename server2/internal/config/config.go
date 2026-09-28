// Package config читает конфигурацию сервера из переменных окружения.
// Имена переменных совпадают с теми, что документирует docs/50-api-contract.yaml
// и e2e/contract/README.md (DATABASE_URL, JWT_SECRET, ALLOW_SIGNUP,
// GOOSAR_DEV_VERIFICATION_CODE, FRONTEND_ORIGIN, PORT, APP_ENV, ...) — контракт
// на них ссылается по имени, поэтому переименовывать их (в отличие от колонок
// БД) нельзя.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config — снимок окружения процесса, прочитанный один раз при старте.
type Config struct {
	Port                 string // PORT
	DatabaseURL          string // DATABASE_URL
	JWTSecret            string // JWT_SECRET
	AppEnv               string // APP_ENV (production | иначе dev)
	AllowSignup          bool   // ALLOW_SIGNUP
	DevVerifyCode        string // GOOSAR_DEV_VERIFICATION_CODE — фиксированный код в dev-режиме
	FrontendOrigin       string // FRONTEND_ORIGIN — для CORS и AppConfig
	McpSecretKey         string // GOOSAR_MCP_SECRET_KEY — наличие включает MFA/секреты
	McpSecretKeyPrevious string // GOOSAR_MCP_SECRET_KEY_PREVIOUS — ротация ключа (T-028, internal/agent)
	RealtimeMetricsToken string // REALTIME_METRICS_TOKEN

	MigrateOnStart bool // MIGRATE=true — применить миграции при старте
	MigrationsDir  string

	// E2ECompatSQLDir — T-027 доводка: каталог необязательных, не входящих в
	// server2/migrations SQL-файлов только для e2e-тестовой инфраструктуры
	// фронтенда (см. server2/testdata/e2e-compat/, server2/README.md, раздел
	// "e2e фронтенда"). Пусто по умолчанию (ничего не применяется); даже
	// если задано, применяется только вне production — см. ApplyE2ECompat().
	E2ECompatSQLDir string // E2E_COMPAT_SQL_DIR

	ServerVersion string // GOOSAR_SERVER_VERSION, иначе "dev"

	// Вложения (T-027, internal/asset) — имена совпадают с
	// docs/50-api-contract.md §1.9 "Вложения".
	LocalUploadDir         string // LOCAL_UPLOAD_DIR — включает локальный backend и маршрут /uploads/*
	S3Bucket               string // S3_BUCKET — наличие означает выбранный (но не реализованный) S3-backend
	AttachmentDownloadMode string // ATTACHMENT_DOWNLOAD_MODE: auto/cloudfront/presign/proxy
	AttachmentDownloadTTL  int    // ATTACHMENT_DOWNLOAD_URL_TTL, минуты, по умолчанию 30

	// PublicURL — GOOSAR_PUBLIC_URL (contract §1.9): базовый публичный адрес
	// сервера, нужен для построения абсолютных URL, которые сервер сам себе
	// не может вывести из запроса (webhook_url автопилотов, github/composio
	// колбэки). Пусто по умолчанию — потребитель тогда строит URL из Host
	// самого запроса (см. server2/internal/autopilot).
	PublicURL string // GOOSAR_PUBLIC_URL

	// CloudRuntimeBaseURL/CloudRuntimeAPIKey (T-028, internal/cloudruntime) —
	// имена переменных не зафиксированы контрактом (docs/50-api-contract.md
	// §6 "Облачный runtime" описывает поведение прокси, но не имя
	// переменных конфигурации деплоя) — решение зафиксировано в
	// server2/docs/decisions.md, раздел T-028.
	CloudRuntimeBaseURL string // GOOSAR_CLOUDRUNTIME_BASE_URL — пусто = не настроено (503 по контракту)
	CloudRuntimeAPIKey  string // GOOSAR_CLOUDRUNTIME_API_KEY

	// MinDaemonVersion — правка T-028 (internal/daemon): минимальная версия
	// CLI-демона, допускаемая на claim-ручках (contract §3.7,
	// "RequireMinDaemonVersion", иначе 426 Upgrade Required). Пусто —
	// проверка версии клиента отключена (принимается любой X-Client-Version).
	MinDaemonVersion string // GOOSAR_MIN_DAEMON_VERSION

	// Почта (T-029, internal/mail) — выбор транспорта и его настройки.
	// Имена переменных не зафиксированы контрактом (docs/50-api-contract.md
	// §1.5/§1.9 называет только сами лимиты и GOOSAR_MCP_SECRET_KEY/
	// GOOSAR_TOTP_ISSUER для MFA) — решение зафиксировано в
	// server2/docs/decisions.md, раздел T-029.
	MailProvider  string // MAIL_PROVIDER: "" (по умолчанию, dev-логгер) | "resend" | "smtp"
	MailFromEmail string // MAIL_FROM_EMAIL
	MailFromName  string // MAIL_FROM_NAME

	ResendAPIKey string // RESEND_API_KEY

	SMTPHost     string // SMTP_HOST
	SMTPPort     int    // SMTP_PORT, по умолчанию 587
	SMTPUsername string // SMTP_USERNAME
	SMTPPassword string // SMTP_PASSWORD
	SMTPSecurity string // SMTP_SECURITY: "starttls" (по умолчанию) | "tls" | "none"

	// TotpIssuer — GOOSAR_TOTP_ISSUER (contract §1.9, "MFA"): имя издателя в
	// otpauth:// URI и приложениях-аутентификаторах.
	TotpIssuer string

	// RateLimits — контрактные лимиты (docs/50-api-contract.md §1.5),
	// читаемые из тех переменных окружения, что называет сам контракт.
	RateLimits RateLimits

	// OIDC/LDAP (T-029, internal/authn) — корпоративный вход. Контракт
	// прямо оговаривает, что их переменные читает пакет corpauth вне этого
	// диапазона файлов — имена ниже выбраны этой сессией, см. decisions.md.
	OIDC OIDCConfig
	LDAP LDAPConfig

	// GitHub App / VCS / Slack / Composio (T-029, internal/integration) —
	// имена совпадают с docs/50-api-contract.md §1.9 "Интеграции".
	GitHubWebhookSecret    string // GITHUB_WEBHOOK_SECRET — подпись POST /api/webhooks/github
	GitHubAppSlug          string // GITHUB_APP_SLUG — используется при построении install-URL
	GitHubAppID            string // GITHUB_APP_ID
	GitHubAppPrivateKey    string // GITHUB_APP_PRIVATE_KEY
	VCSIntegrationEnabled  bool   // GOOSAR_VCS_INTEGRATION_ENABLED
	VCSSecretKey           string // GOOSAR_VCS_SECRET_KEY — шифрует access_token/webhook_secret подключений
	VCSSecretKeyPrevious   string // GOOSAR_VCS_SECRET_KEY_PREVIOUS — ротация ключа
	ComposioAPIKey         string // COMPOSIO_API_KEY
	ComposioStateSecret    string // COMPOSIO_STATE_SECRET — иначе JWTSecret (contract §1.9)
	ComposioCallbackBase   string // COMPOSIO_CALLBACK_BASE_URL — иначе PublicURL
	SlackSecretKey         string // GOOSAR_SLACK_SECRET_KEY — шифрует bot_token/app_token BYO-установки
	SlackSecretKeyPrevious string // GOOSAR_SLACK_SECRET_KEY_PREVIOUS

	// GitHubAPIBaseURL/SlackAPIBaseURL/ComposioAPIBaseURL — решение этой
	// сессии (server2/docs/decisions.md, раздел T-029): контракт не называет
	// переменную для адреса внешнего API (в проде — github.com/slack.com/
	// backend.composio.dev как есть), но песочница этой сессии не имеет
	// доступа к интернету — без configurable base URL интеграции нельзя
	// было бы покрыть httptest-сервером. Пусто = дефолт клиента (реальный
	// внешний хост).
	GitHubAPIBaseURL    string // GOOSAR_GITHUB_API_BASE_URL
	SlackAPIBaseURL     string // GOOSAR_SLACK_API_BASE_URL
	ComposioAPIBaseURL  string // GOOSAR_COMPOSIO_API_BASE_URL
	VCSAllowedProviders string // GOOSAR_VCS_ALLOWED_PROVIDERS — csv, пусто = не ограничено

	// DeploymentLLM* — T-029 (internal/deployment): LLM-эндпоинт деплоя,
	// который проверяет GET /api/llm/health и выдаёт (десктоп-раннеру вне
	// активной задачи) GET /api/deployment/client-secrets. Ни контракт, ни
	// data-model не называют конкретный источник хранения (platform_policy
	// хранит только описательные llm.base_url/model для документа политики,
	// не сами креды) — решение зафиксировано в server2/docs/decisions.md,
	// раздел T-029: переменные окружения деплоя, тем же духом, что и
	// GOOSAR_MCP_SECRET_KEY. Пусто — LLM деплоя не настроен ("unconfigured").
	DeploymentLLMBaseURL string // GOOSAR_DEPLOYMENT_LLM_BASE_URL
	DeploymentLLMModel   string // GOOSAR_DEPLOYMENT_LLM_MODEL
	DeploymentLLMAPIKey  string // GOOSAR_DEPLOYMENT_LLM_API_KEY

	// RoleWorkspaces — T-029 CLI `provision-roles` / автопровижининг при
	// старте сервера (contract, приложение CLI: "Та же процедура выполняется
	// сервером при старте, если GOOSAR_ROLE_WORKSPACES=auto").
	RoleWorkspaces string // GOOSAR_ROLE_WORKSPACES

	// ProvisioningCatalogDir — T-029 (`/api/provisioning/{catalog,manifest,blob}`):
	// каталог пакетов деплоя не заводит отдельной таблицы в data-model (только
	// provisioning_pins — закрепления пространства), поэтому источник каталога —
	// файловая директория (тем же приёмом, что LocalUploadDir у вложений):
	// `<dir>/catalog.json` (ProvisioningCatalog) + `<dir>/blobs/<name>-<version>.zst`.
	// Пусто — вся группа отвечает 503 "provisioning не настроен" (валидный
	// документированный контрактом ответ). Решение — server2/docs/decisions.md.
	ProvisioningCatalogDir string // GOOSAR_PROVISIONING_CATALOG_DIR

	// Retention* — окна хранения для CLI `purge`/`gc-uploads` (contract,
	// приложение CLI: "Окна по умолчанию берутся из переменных
	// GOOSAR_RETENTION_*"). Часы; см. server2/docs/decisions.md за точные имена.
	RetentionChatHours            int // GOOSAR_RETENTION_CHAT_HOURS (720 = 30д)
	RetentionTasksHours           int // GOOSAR_RETENTION_TASKS_HOURS (720)
	RetentionClosedIssuesHours    int // GOOSAR_RETENTION_CLOSED_ISSUES_HOURS (8760 = 365д)
	RetentionActivityHours        int // GOOSAR_RETENTION_ACTIVITY_HOURS (720)
	RetentionAttachmentGraceHours int // GOOSAR_RETENTION_ATTACHMENT_GRACE_HOURS (168 = 7д)

	// DeploymentMcp*URL — CLI `mcp-library seed` (contract, приложение CLI):
	// адреса сервисов, которыми идемпотентно (по имени) заполняется
	// platform_mcp_servers. Пустой адрес — сервис пропускается.
	DeploymentJiraURL       string // GOOSAR_DEPLOYMENT_JIRA_URL
	DeploymentConfluenceURL string // GOOSAR_DEPLOYMENT_CONFLUENCE_URL
	DeploymentEWSURL        string // GOOSAR_DEPLOYMENT_EWS_URL
	DeploymentBitrix24URL   string // GOOSAR_DEPLOYMENT_BITRIX24_URL
	DeploymentMcpGatewayURL string // GOOSAR_DEPLOYMENT_MCP_GATEWAY_URL
}

// RateLimits — лимиты contract §1.5, per-instance in-process fallback
// (Redis-бэкенд, которым можно подменить in-process лимитер за тем же
// интерфейсом, не входит в объём T-029 — см. httpapi.RateLimiter).
type RateLimits struct {
	Auth         int // RATE_LIMIT_AUTH — в минуту, по IP (send-code, ldap-login)
	AuthVerify   int // RATE_LIMIT_AUTH_VERIFY — в минуту, по IP (verify-code/-link, oidc, mfa/verify)
	AuthEmail    int // RATE_LIMIT_AUTH_EMAIL — в минуту, по email/username тела запроса
	Token        int // RATE_LIMIT_TOKEN — в час, по пользователю (cli-token, mfa enroll/confirm/disable/recovery-codes)
	MfaVerify    int // RATE_LIMIT_MFA_VERIFY — за 5 минут, по хэшу mfa_token
	API          int // RATE_LIMIT_API — в минуту, общий лимит /api/**, по пользователю иначе по IP
	ContactSales int // RATE_LIMIT_CONTACT_SALES — в час, по IP
	Export       int // RATE_LIMIT_EXPORT — в час, по пользователю
	Join         int // RATE_LIMIT_JOIN — в час, по пользователю иначе по IP
}

// OIDCConfig — пусто (IssuerURL == "") означает "OIDC не настроен на этом
// сервере" (contract: 404 на /api/auth/oidc/start и /api/auth/oidc/callback,
// "oidc" не входит в AuthMethodsResponse.methods).
type OIDCConfig struct {
	IssuerURL    string // OIDC_ISSUER_URL
	ClientID     string // OIDC_CLIENT_ID
	ClientSecret string // OIDC_CLIENT_SECRET
	RedirectURL  string // OIDC_REDIRECT_URL — если пусто, строится из GOOSAR_PUBLIC_URL
	DisplayName  string // OIDC_DISPLAY_NAME — AuthMethodsResponse.oidc_display_name
}

// LDAPConfig — пусто (URL == "") означает "LDAP не настроен" (contract: 404
// на /api/auth/ldap/login, "ldap" не входит в AuthMethodsResponse.methods).
type LDAPConfig struct {
	URL          string // LDAP_URL, например ldap://dc.example.test:389 или ldaps://...
	BindDN       string // LDAP_BIND_DN — служебная учётная запись для поиска пользователя
	BindPassword string // LDAP_BIND_PASSWORD
	BaseDN       string // LDAP_BASE_DN — корень поиска
	UserFilter   string // LDAP_USER_FILTER, по умолчанию "(uid=%s)" — %s подставляется username
	EmailAttr    string // LDAP_EMAIL_ATTRIBUTE, по умолчанию "mail"
	NameAttr     string // LDAP_NAME_ATTRIBUTE, по умолчанию "cn"
	DisplayName  string // LDAP_DISPLAY_NAME — AuthMethodsResponse.ldap_display_name
}

// Load собирает Config из os.Environ(). Отсутствующие необязательные значения
// получают разумные значения по умолчанию для локальной разработки, а не
// приводят к ошибке — это соответствует духу дефолтов контракта (dev-режим
// сервера должен подниматься без внешней инфраструктуры: почты, Redis, S3 и т.д.).
func Load() Config {
	c := Config{
		Port:                 getenv("PORT", "8080"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		JWTSecret:            getenv("JWT_SECRET", "dev-insecure-secret-change-me"),
		AppEnv:               getenv("APP_ENV", "development"),
		AllowSignup:          getBool("ALLOW_SIGNUP", true),
		DevVerifyCode:        os.Getenv("GOOSAR_DEV_VERIFICATION_CODE"),
		FrontendOrigin:       getenv("FRONTEND_ORIGIN", "http://localhost:3199"),
		McpSecretKey:         os.Getenv("GOOSAR_MCP_SECRET_KEY"),
		McpSecretKeyPrevious: os.Getenv("GOOSAR_MCP_SECRET_KEY_PREVIOUS"),
		RealtimeMetricsToken: os.Getenv("REALTIME_METRICS_TOKEN"),
		MigrateOnStart:       getBool("MIGRATE", false),
		MigrationsDir:        getenv("MIGRATIONS_DIR", "server2/migrations"),
		E2ECompatSQLDir:      os.Getenv("E2E_COMPAT_SQL_DIR"),
		ServerVersion:        getenv("GOOSAR_SERVER_VERSION", "dev"),

		LocalUploadDir:         os.Getenv("LOCAL_UPLOAD_DIR"),
		S3Bucket:               os.Getenv("S3_BUCKET"),
		AttachmentDownloadMode: getenv("ATTACHMENT_DOWNLOAD_MODE", "auto"),
		AttachmentDownloadTTL:  getInt("ATTACHMENT_DOWNLOAD_URL_TTL", 30),

		PublicURL: os.Getenv("GOOSAR_PUBLIC_URL"),

		CloudRuntimeBaseURL: os.Getenv("GOOSAR_CLOUDRUNTIME_BASE_URL"),
		CloudRuntimeAPIKey:  os.Getenv("GOOSAR_CLOUDRUNTIME_API_KEY"),

		MinDaemonVersion: os.Getenv("GOOSAR_MIN_DAEMON_VERSION"),

		MailProvider:  strings.ToLower(os.Getenv("MAIL_PROVIDER")),
		MailFromEmail: getenv("MAIL_FROM_EMAIL", "noreply@goosar.local"),
		MailFromName:  getenv("MAIL_FROM_NAME", "Goosar"),
		ResendAPIKey:  os.Getenv("RESEND_API_KEY"),
		SMTPHost:      os.Getenv("SMTP_HOST"),
		SMTPPort:      getInt("SMTP_PORT", 587),
		SMTPUsername:  os.Getenv("SMTP_USERNAME"),
		SMTPPassword:  os.Getenv("SMTP_PASSWORD"),
		SMTPSecurity:  strings.ToLower(getenv("SMTP_SECURITY", "starttls")),
		TotpIssuer:    getenv("GOOSAR_TOTP_ISSUER", "Goosar"),

		RateLimits: RateLimits{
			Auth:         getInt("RATE_LIMIT_AUTH", 5),
			AuthVerify:   getInt("RATE_LIMIT_AUTH_VERIFY", 20),
			AuthEmail:    getInt("RATE_LIMIT_AUTH_EMAIL", 10),
			Token:        getInt("RATE_LIMIT_TOKEN", 20),
			MfaVerify:    getInt("RATE_LIMIT_MFA_VERIFY", 10),
			API:          getInt("RATE_LIMIT_API", 600),
			ContactSales: getInt("RATE_LIMIT_CONTACT_SALES", 5),
			Export:       getInt("RATE_LIMIT_EXPORT", 3),
			Join:         getInt("RATE_LIMIT_JOIN", 20),
		},

		OIDC: OIDCConfig{
			IssuerURL:    os.Getenv("OIDC_ISSUER_URL"),
			ClientID:     os.Getenv("OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("OIDC_REDIRECT_URL"),
			DisplayName:  getenv("OIDC_DISPLAY_NAME", "Single sign-on"),
		},
		LDAP: LDAPConfig{
			URL:          os.Getenv("LDAP_URL"),
			BindDN:       os.Getenv("LDAP_BIND_DN"),
			BindPassword: os.Getenv("LDAP_BIND_PASSWORD"),
			BaseDN:       os.Getenv("LDAP_BASE_DN"),
			UserFilter:   getenv("LDAP_USER_FILTER", "(uid=%s)"),
			EmailAttr:    getenv("LDAP_EMAIL_ATTRIBUTE", "mail"),
			NameAttr:     getenv("LDAP_NAME_ATTRIBUTE", "cn"),
			DisplayName:  getenv("LDAP_DISPLAY_NAME", "Corporate directory"),
		},

		GitHubWebhookSecret:    os.Getenv("GITHUB_WEBHOOK_SECRET"),
		GitHubAppSlug:          os.Getenv("GITHUB_APP_SLUG"),
		GitHubAppID:            os.Getenv("GITHUB_APP_ID"),
		GitHubAppPrivateKey:    os.Getenv("GITHUB_APP_PRIVATE_KEY"),
		VCSIntegrationEnabled:  getBool("GOOSAR_VCS_INTEGRATION_ENABLED", true),
		VCSSecretKey:           os.Getenv("GOOSAR_VCS_SECRET_KEY"),
		VCSSecretKeyPrevious:   os.Getenv("GOOSAR_VCS_SECRET_KEY_PREVIOUS"),
		ComposioAPIKey:         os.Getenv("COMPOSIO_API_KEY"),
		ComposioStateSecret:    os.Getenv("COMPOSIO_STATE_SECRET"),
		ComposioCallbackBase:   os.Getenv("COMPOSIO_CALLBACK_BASE_URL"),
		SlackSecretKey:         os.Getenv("GOOSAR_SLACK_SECRET_KEY"),
		SlackSecretKeyPrevious: os.Getenv("GOOSAR_SLACK_SECRET_KEY_PREVIOUS"),
		GitHubAPIBaseURL:       os.Getenv("GOOSAR_GITHUB_API_BASE_URL"),
		SlackAPIBaseURL:        os.Getenv("GOOSAR_SLACK_API_BASE_URL"),
		ComposioAPIBaseURL:     os.Getenv("GOOSAR_COMPOSIO_API_BASE_URL"),
		VCSAllowedProviders:    os.Getenv("GOOSAR_VCS_ALLOWED_PROVIDERS"),

		DeploymentLLMBaseURL: os.Getenv("GOOSAR_DEPLOYMENT_LLM_BASE_URL"),
		DeploymentLLMModel:   os.Getenv("GOOSAR_DEPLOYMENT_LLM_MODEL"),
		DeploymentLLMAPIKey:  os.Getenv("GOOSAR_DEPLOYMENT_LLM_API_KEY"),

		RoleWorkspaces:         os.Getenv("GOOSAR_ROLE_WORKSPACES"),
		ProvisioningCatalogDir: os.Getenv("GOOSAR_PROVISIONING_CATALOG_DIR"),

		RetentionChatHours:            getInt("GOOSAR_RETENTION_CHAT_HOURS", 720),
		RetentionTasksHours:           getInt("GOOSAR_RETENTION_TASKS_HOURS", 720),
		RetentionClosedIssuesHours:    getInt("GOOSAR_RETENTION_CLOSED_ISSUES_HOURS", 8760),
		RetentionActivityHours:        getInt("GOOSAR_RETENTION_ACTIVITY_HOURS", 720),
		RetentionAttachmentGraceHours: getInt("GOOSAR_RETENTION_ATTACHMENT_GRACE_HOURS", 168),

		DeploymentJiraURL:       os.Getenv("GOOSAR_DEPLOYMENT_JIRA_URL"),
		DeploymentConfluenceURL: os.Getenv("GOOSAR_DEPLOYMENT_CONFLUENCE_URL"),
		DeploymentEWSURL:        os.Getenv("GOOSAR_DEPLOYMENT_EWS_URL"),
		DeploymentBitrix24URL:   os.Getenv("GOOSAR_DEPLOYMENT_BITRIX24_URL"),
		DeploymentMcpGatewayURL: os.Getenv("GOOSAR_DEPLOYMENT_MCP_GATEWAY_URL"),
	}
	if c.ComposioStateSecret == "" {
		c.ComposioStateSecret = c.JWTSecret
	}
	if c.ComposioCallbackBase == "" {
		c.ComposioCallbackBase = c.PublicURL
	}
	return c
}

// IsProduction — APP_ENV=production отключает dev-код подтверждения по почте.
func (c Config) IsProduction() bool { return strings.EqualFold(c.AppEnv, "production") }

// DevCodeEnabled — фиксированный код логина разрешён только вне production и
// только если переменная задана (пусто = отключено даже вне prod).
func (c Config) DevCodeEnabled() bool { return !c.IsProduction() && c.DevVerifyCode != "" }

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

func getInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Validate проверяет обязательные для запуска сервера значения.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: DATABASE_URL не задан")
	}
	return nil
}
