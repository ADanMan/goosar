// Package importer переносит один воркспейс со старого сервера
// (api.example/v1.2.0, docs/50-api-contract.yaml) в server2, читая источник
// только через GET-запросы контракта и записывая результат напрямую в
// server2 по SQL, одной транзакцией (см. docs/51-data-model.md, «Перенос
// данных», и server2/docs/decisions.md для решений по пробелам спецификации).
package importer

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server2/internal/migrate"
)

// Options — параметры одного запуска импорта (соответствуют флагам
// server2/cmd/import).
type Options struct {
	SourceURL   string // базовый URL исходного сервера, например http://localhost:8199
	Token       string // Bearer PAT/JWT с ролью owner в исходном воркспейсе
	Workspace   string // slug или id исходного воркспейса
	DatabaseURL string // DSN целевой базы server2
	Migrate     bool   // применить server2/migrations перед импортом
	DryRun      bool   // только прочитать источник и посчитать, не писать в БД
	MigrationsDir string // каталог с NNN_*.up.sql; по умолчанию "server2/migrations" относительно cwd
}

func (o Options) migrationsDir() string {
	if o.MigrationsDir != "" {
		return o.MigrationsDir
	}
	return "server2/migrations"
}

// Run выполняет один запуск импорта и возвращает отчёт.
func Run(ctx context.Context, opts Options) (*Report, error) {
	if opts.SourceURL == "" {
		return nil, fmt.Errorf("importer: --source-url обязателен")
	}
	if opts.Token == "" {
		return nil, fmt.Errorf("importer: --token обязателен")
	}
	if opts.Workspace == "" {
		return nil, fmt.Errorf("importer: --workspace обязателен")
	}

	api := newClient(opts.SourceURL, opts.Token)

	ws, err := api.resolveWorkspace(ctx, opts.Workspace)
	if err != nil {
		return nil, err
	}
	report := newReport(WorkspaceRef{SourceID: ws.ID, Slug: ws.Slug, Name: ws.Name}, opts.DryRun)

	fetched, err := fetchAll(ctx, api, ws)
	if err != nil {
		return nil, err
	}
	recordSkips(report, fetched)

	if opts.DryRun {
		countFetched(report, fetched)
		return report, nil
	}

	pool, err := pgxpool.New(ctx, opts.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("importer: подключение к %s: %w", "--database-url", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("importer: проверка соединения с целевой базой: %w", err)
	}

	if opts.Migrate {
		applied, err := migrate.Apply(ctx, pool, opts.migrationsDir())
		if err != nil {
			return nil, fmt.Errorf("importer: применение миграций: %w", err)
		}
		report.MigrationsRun = applied
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("importer: начало транзакции записи: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op после успешного Commit

	w := newWriter(tx)
	if err := writeAll(ctx, w, report, fetched); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("importer: коммит транзакции импорта: %w", err)
	}
	return report, nil
}
