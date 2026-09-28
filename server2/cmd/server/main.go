// Команда server поднимает HTTP-сервер server2 (T-026): читает конфигурацию
// из окружения (те же имена переменных, что документирует контракт),
// опционально применяет миграции server2/migrations, слушает PORT и
// обслуживает весь HTTP API из docs/50-api-contract.yaml (реализованные
// домены + 501-заглушки для остального).
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/adanman/goosar/server2/internal/app"
	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/deployment"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/migrate"
	"github.com/adanman/goosar/server2/internal/store"
)

func main() {
	migrateFlag := flag.Bool("migrate", false, "применить server2/migrations перед стартом (то же самое, что MIGRATE=true)")
	flag.Parse()

	cfg := config.Load()
	if *migrateFlag {
		cfg.MigrateOnStart = true
	}

	logger := newLogger(cfg)
	for _, w := range cfg.ParseWarnings {
		logger.Warn("config: " + w)
	}
	for _, n := range cfg.UnsupportedNotices() {
		logger.Warn("config: " + n)
	}

	if err := cfg.Validate(); err != nil {
		logger.Error("конфигурация невалидна", "err", err)
		os.Exit(1)
	}

	// GOOSAR_TRUSTED_PROXIES / RATE_LIMIT_TRUSTED_PROXIES — какому обратному
	// прокси разрешено выставлять X-Forwarded-For/X-Real-IP для
	// httpapi.ClientIP (лимитеры, audit-лог, приём вебхуков). Отдельного
	// списка для RATE_LIMIT_TRUSTED_PROXIES не заводится — используется он,
	// если задан, иначе общий GOOSAR_TRUSTED_PROXIES (contract: "Задаётся
	// отдельно от общего списка только если лимитеру нужен другой список").
	trustedProxies := cfg.RateLimits.TrustedProxies
	if len(trustedProxies) == 0 {
		trustedProxies = cfg.TrustedProxies
	}
	httpapi.SetTrustedProxies(trustedProxies)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.OpenPool(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns, cfg.DatabaseMinConns)
	if err != nil {
		logger.Error("не удалось открыть пул БД", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if cfg.MigrateOnStart {
		applied, err := migrate.ApplyLocked(ctx, db.Pool, cfg.MigrationsDir, cfg.MigrationLockTimeout, cfg.MigrationLockRetries, cfg.MigrationStatementTimeout)
		if err != nil {
			logger.Error("применение миграций провалилось", "err", err)
			os.Exit(1)
		}
		logger.Info("миграции применены", "versions", applied)
	}

	// E2E_COMPAT_SQL_DIR — T-027 доводка: необязательный, отдельный от
	// server2/migrations набор SQL (представления для e2e-тестовой
	// инфраструктуры фронтенда, см. server2/testdata/e2e-compat,
	// server2/README.md). Пусто по умолчанию — не применяется никогда;
	// даже если задано, применяется только вне production (та же защита,
	// что и config.DevCodeEnabled(), тем же принципом: тестовые удобства не
	// должны быть достижимы простой опечаткой на проде).
	if cfg.E2ECompatSQLDir != "" {
		if cfg.IsProduction() {
			logger.Warn("E2E_COMPAT_SQL_DIR задан, но APP_ENV=production — пропущено")
		} else {
			applied, err := migrate.ApplyIdempotent(ctx, db.Pool, cfg.E2ECompatSQLDir)
			if err != nil {
				logger.Error("применение e2e-совместимых SQL провалилось", "err", err)
				os.Exit(1)
			}
			logger.Info("e2e-совместимые представления применены", "files", applied)
		}
	}

	// GOOSAR_DEPLOYMENT_ADMIN_EMAILS — сидирует роль deployment-admin
	// существующим пользователям, но только пока таблица администраторов
	// пуста (contract, группа "Аутентификация и сессии"); адреса, ещё не
	// зарегистрированные, резолвятся при их будущей регистрации — эта версия
	// server2 такой хук на регистрацию не заводит (см. server2/docs/env-parity.md),
	// поэтому неудачные email просто логируются и не блокируют старт.
	if len(cfg.DeploymentAdminEmails) > 0 {
		seedDeploymentAdmins(ctx, db, cfg, logger)
	}

	// GOOSAR_ROLE_WORKSPACES=auto — на каждом старте по одному ролевому
	// воркспейсу на каждый включённый шаблон (contract, группа
	// "Аутентификация и сессии"); идемпотентно, повторяется на следующем
	// старте, если ещё нет ни одного администратора деплоя. cfg.Validate()
	// уже отверг любое значение, кроме ""/auto/off.
	deps := app.New(cfg, db, logger)
	if templates := deployment.EnabledRoleTemplates(cfg.RoleWorkspaces); len(templates) > 0 {
		results, err := deployment.ProvisionRoleWorkspaces(ctx, db, deps.Workspace.Store, templates)
		if err != nil {
			logger.Error("GOOSAR_ROLE_WORKSPACES=auto: провижининг ролевых воркспейсов провалился", "err", err)
		} else {
			for _, res := range results {
				logger.Info("роль-воркспейс", "key", res.Key, "action", res.Action, "slug", res.Slug, "reason", res.Reason)
			}
		}
	}

	router := app.NewRouter(deps)
	handler := deps.BuildHandler(router)

	// Планировщик автопилотов (T-028, internal/autopilot) — фоновый цикл
	// опроса schedule-триггеров; свою защиту от двойного запуска при
	// нескольких инстансах сервера несёт сам (advisory lock Postgres, см.
	// autopilot.Store.TryAdvisoryLock), поэтому здесь просто запускается
	// безусловно.
	go deps.Autopilot.Scheduler.Run(ctx)

	// GOOSAR_AUDIT_RETENTION_DAYS — ежедневная задача удаляет строки обоих
	// журналов аудита (объединены в platform_audit_log) старше этого срока;
	// 0 — хранить вечно (contract, группа "Деплой/политика").
	go runAuditRetentionLoop(ctx, db, cfg.AuditRetentionDays, logger)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		// GOOSAR_SHUTDOWN_HOLD_DURATION — пауза перед началом штатного
		// плавного выключения (contract, группа "БД и запуск"): платформенный
		// grace period должен покрывать эту паузу плюс само выключение, так
		// что hold идёт до, а не вместо срабатывающего ниже 10-секундного
		// таймаута graceful shutdown.
		if cfg.ShutdownHoldDuration > 0 {
			logger.Info("shutdown: пауза перед выключением", "hold", cfg.ShutdownHoldDuration)
			time.Sleep(cfg.ShutdownHoldDuration)
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("ошибка при остановке сервера", "err", err)
		}
	}()

	logger.Info("server2 слушает", "port", cfg.Port, "app_env", cfg.AppEnv)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("сервер остановлен с ошибкой", "err", err)
		os.Exit(1)
	}
}

// newLogger — LOG_FORMAT/GOOSAR_LOG_FORMAT (json|text) и LOG_LEVEL/
// GOOSAR_LOG_LEVEL (debug|info|warn|error), contract группа "Наблюдаемость".
// Дефолт формата — text при прямом запуске бинарника (json — только когда
// явно задан, self-host стек задаёт его сам); дефолт уровня — info на
// APP_ENV=production, иначе debug.
func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	if !cfg.IsProduction() {
		level = slog.LevelDebug
	}
	switch strings.ToLower(strings.TrimSpace(cfg.LogLevel)) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(strings.TrimSpace(cfg.LogFormat), "json") {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

// runAuditRetentionLoop — GOOSAR_AUDIT_RETENTION_DAYS: раз в сутки (и один
// раз сразу при старте) удаляет из platform_audit_log строки старше этого
// срока; retentionDays<=0 — цикл не запускается (хранить вечно).
func runAuditRetentionLoop(ctx context.Context, db *store.Store, retentionDays int, logger *slog.Logger) {
	if retentionDays <= 0 {
		return
	}
	purge := func() {
		n, err := deployment.PurgeAuditLog(ctx, db, retentionDays)
		if err != nil {
			logger.Error("GOOSAR_AUDIT_RETENTION_DAYS: очистка журнала аудита провалилась", "err", err)
			return
		}
		if n > 0 {
			logger.Info("GOOSAR_AUDIT_RETENTION_DAYS: журнал аудита очищен", "deleted_rows", n)
		}
	}
	purge()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}

// seedDeploymentAdmins — GOOSAR_DEPLOYMENT_ADMIN_EMAILS, только пока таблица
// администраторов деплоя пуста (первое развёртывание); дальше база
// авторитетна, а расхождение со списком только логируется (contract).
func seedDeploymentAdmins(ctx context.Context, db *store.Store, cfg config.Config, logger *slog.Logger) {
	admins, err := deployment.ListAdmins(ctx, db)
	if err != nil {
		logger.Error("GOOSAR_DEPLOYMENT_ADMIN_EMAILS: чтение списка администраторов", "err", err)
		return
	}
	if len(admins) > 0 {
		logger.Info("GOOSAR_DEPLOYMENT_ADMIN_EMAILS: таблица администраторов уже не пуста — список только сверяется, не применяется", "configured", len(cfg.DeploymentAdminEmails))
		return
	}
	for _, email := range cfg.DeploymentAdminEmails {
		if _, _, err := deployment.GrantAdminDirect(ctx, db, email, ""); err != nil {
			logger.Warn("GOOSAR_DEPLOYMENT_ADMIN_EMAILS: адрес не резолвится (ещё не зарегистрирован или ошибка) — будет повторено на следующем старте", "email", email, "err", err)
			continue
		}
		logger.Info("GOOSAR_DEPLOYMENT_ADMIN_EMAILS: роль администратора выдана", "email", email)
	}
}
