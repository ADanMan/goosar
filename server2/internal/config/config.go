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
	Port          string // PORT
	DatabaseURL   string // DATABASE_URL
	JWTSecret     string // JWT_SECRET
	AppEnv        string // APP_ENV (production | иначе dev)
	AllowSignup   bool   // ALLOW_SIGNUP
	DevVerifyCode string // GOOSAR_DEV_VERIFICATION_CODE — фиксированный код в dev-режиме
	FrontendOrigin string // FRONTEND_ORIGIN — для CORS и AppConfig
	McpSecretKey  string // GOOSAR_MCP_SECRET_KEY — наличие включает MFA/секреты
	RealtimeMetricsToken string // REALTIME_METRICS_TOKEN

	MigrateOnStart bool // MIGRATE=true — применить миграции при старте
	MigrationsDir  string

	ServerVersion string // GOOSAR_SERVER_VERSION, иначе "dev"
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
		RealtimeMetricsToken: os.Getenv("REALTIME_METRICS_TOKEN"),
		MigrateOnStart:       getBool("MIGRATE", false),
		MigrationsDir:        getenv("MIGRATIONS_DIR", "server2/migrations"),
		ServerVersion:        getenv("GOOSAR_SERVER_VERSION", "dev"),
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

// Validate проверяет обязательные для запуска сервера значения.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: DATABASE_URL не задан")
	}
	return nil
}
