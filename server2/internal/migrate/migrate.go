// Package migrate применяет SQL-миграции server2 (server2/migrations/NNN_name.up.sql)
// к целевой базе по порядку номеров и ведёт таблицу учёта уже применённых файлов.
//
// Раннер сознательно простой (нет параллельного применения, каждая миграция —
// в своей транзакции): server2/migrations/check_names.py и docs/51-data-model.md
// описывают миграции как последовательные NNN_*.up.sql, применяемые по порядку
// имени файла — этого раннеру достаточно и для сервера (T-026+), и для
// server2/cmd/import --migrate.
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LedgerTable — таблица учёта применённых миграций server2. Имя нарочно не
// совпадает с тем, что мог использовать старый server (server/** не читался
// по правилам чистой комнаты, поэтому совпадение исключается выбором
// отчётливо другого, непохожего имени).
const LedgerTable = "server2_migration_ledger"

var upFileRe = regexp.MustCompile(`^(\d+)_[a-zA-Z0-9_]+\.up\.sql$`)

// File — одна миграция, найденная в каталоге.
type File struct {
	Version int
	Name    string // NNN_domain
	Path    string
	SQL     string
}

// Load читает и сортирует по номеру все NNN_*.up.sql в dir.
func Load(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("migrate: чтение каталога %s: %w", dir, err)
	}
	var files []File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		f, ok, err := parseEntry(e.Name(), func(name string) ([]byte, error) {
			return os.ReadFile(filepath.Join(dir, name))
		})
		if err != nil {
			return nil, err
		}
		if ok {
			f.Path = filepath.Join(dir, e.Name())
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Version < files[j].Version })
	return files, nil
}

// LoadFS — тот же Load, но поверх fs.FS (используется в тестах на встроенных
// через go:embed данных, без обращения к реальной файловой системе).
func LoadFS(fsys fs.FS, dir string) ([]File, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("migrate: чтение каталога %s: %w", dir, err)
	}
	var files []File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		f, ok, err := parseEntry(e.Name(), func(name string) ([]byte, error) {
			return fs.ReadFile(fsys, dir+"/"+name)
		})
		if err != nil {
			return nil, err
		}
		if ok {
			f.Path = dir + "/" + e.Name()
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Version < files[j].Version })
	return files, nil
}

func parseEntry(name string, read func(string) ([]byte, error)) (File, bool, error) {
	m := upFileRe.FindStringSubmatch(name)
	if m == nil {
		return File{}, false, nil
	}
	var version int
	if _, err := fmt.Sscanf(m[1], "%d", &version); err != nil {
		return File{}, false, fmt.Errorf("migrate: не удалось разобрать номер миграции %q: %w", name, err)
	}
	body, err := read(name)
	if err != nil {
		return File{}, false, fmt.Errorf("migrate: чтение %s: %w", name, err)
	}
	return File{
		Version: version,
		Name:    strings.TrimSuffix(name, ".up.sql"),
		SQL:     string(body),
	}, true, nil
}

// EnsureLedger создаёт таблицу учёта применённых миграций, если её ещё нет.
func EnsureLedger(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS `+LedgerTable+` (
    version     integer PRIMARY KEY,
    name        text NOT NULL,
    applied_at  timestamptz NOT NULL DEFAULT now()
)`)
	if err != nil {
		return fmt.Errorf("migrate: создание таблицы учёта: %w", err)
	}
	return nil
}

// Applied возвращает набор версий, уже отмеченных как применённые.
func Applied(ctx context.Context, pool *pgxpool.Pool) (map[int]bool, error) {
	rows, err := pool.Query(ctx, `SELECT version FROM `+LedgerTable)
	if err != nil {
		return nil, fmt.Errorf("migrate: чтение таблицы учёта: %w", err)
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

// Apply применяет все ещё не применённые миграции из dir к пулу pool, каждую
// миграцию — в своей собственной транзакции (чтобы одна большая транзакция на
// все 12 файлов не держала блокировки дольше необходимого, и чтобы частичный
// сбой оставлял предыдущие миграции применёнными, а не откатывал всё сразу).
// Возвращает версии, применённые в этом вызове (пусто, если всё уже было
// применено раньше — вызов идемпотентен).
func Apply(ctx context.Context, pool *pgxpool.Pool, dir string) ([]int, error) {
	files, err := Load(dir)
	if err != nil {
		return nil, err
	}
	return applyFiles(ctx, pool, files)
}

// ApplyIdempotent исполняет по порядку номеров все NNN_*.up.sql в dir,
// каждый файл — в своей транзакции, но, в отличие от Apply, НЕ ведёт таблицу
// учёта (LedgerTable) и поэтому применяет каждый файл заново при каждом
// вызове. Предназначена для необязательных, не входящих в основную схему
// наборов SQL (например server2/testdata/e2e-compat — представления для
// e2e-тестовой инфраструктуры фронтенда, см. server2/README.md), которые
// сами обязаны быть идемпотентными (DROP ... IF EXISTS перед CREATE) именно
// потому, что раннер не помнит, что уже применял их раньше.
func ApplyIdempotent(ctx context.Context, pool *pgxpool.Pool, dir string) ([]string, error) {
	files, err := Load(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, f := range files {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return names, fmt.Errorf("migrate: начало транзакции для %s: %w", f.Name, err)
		}
		if _, err := tx.Exec(ctx, f.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return names, fmt.Errorf("migrate: применение %s: %w", f.Name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return names, fmt.Errorf("migrate: коммит %s: %w", f.Name, err)
		}
		names = append(names, f.Name)
	}
	return names, nil
}

func applyFiles(ctx context.Context, pool *pgxpool.Pool, files []File) ([]int, error) {
	if err := EnsureLedger(ctx, pool); err != nil {
		return nil, err
	}
	done, err := Applied(ctx, pool)
	if err != nil {
		return nil, err
	}
	var appliedNow []int
	for _, f := range files {
		if done[f.Version] {
			continue
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return appliedNow, fmt.Errorf("migrate: начало транзакции для %s: %w", f.Name, err)
		}
		if _, err := tx.Exec(ctx, f.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return appliedNow, fmt.Errorf("migrate: применение %s: %w", f.Name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO `+LedgerTable+` (version, name) VALUES ($1, $2)`,
			f.Version, f.Name); err != nil {
			_ = tx.Rollback(ctx)
			return appliedNow, fmt.Errorf("migrate: запись в таблицу учёта для %s: %w", f.Name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return appliedNow, fmt.Errorf("migrate: коммит %s: %w", f.Name, err)
		}
		appliedNow = append(appliedNow, f.Version)
	}
	return appliedNow, nil
}
