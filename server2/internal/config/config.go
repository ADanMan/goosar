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
