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
	"syscall"
	"time"

	"github.com/adanman/goosar/server2/internal/app"
	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/migrate"
	"github.com/adanman/goosar/server2/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	migrateFlag := flag.Bool("migrate", false, "применить server2/migrations перед стартом (то же самое, что MIGRATE=true)")
	flag.Parse()

	cfg := config.Load()
	if *migrateFlag {
		cfg.MigrateOnStart = true
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("конфигурация невалидна", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("не удалось открыть пул БД", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if cfg.MigrateOnStart {
		applied, err := migrate.Apply(ctx, db.Pool, cfg.MigrationsDir)
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

	deps := app.New(cfg, db, logger)
	router := app.NewRouter(deps)
	handler := deps.BuildHandler(router)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
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
