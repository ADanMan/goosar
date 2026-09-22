// Утилита backfill_task_usage_hourly заполняет почасовую сводку task_usage_hourly
// из исторических строк task_usage. Запускается один раз после миграций почасового
// конвейера и до регистрации задания pg_cron.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/logger"
)

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("backfill failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dryRun       = flag.Bool("dry-run", false, "log slices that would be processed without touching task_usage_hourly")
		monthsBack   = flag.Int("months-back", 0, "limit backfill to the last N months (0 = all available history)")
		forcePartial = flag.Bool("force-partial", false, "acknowledge that --months-back permanently abandons buckets older than the cutoff (the watermark still advances past them)")
		sleep        = flag.Duration("sleep-between-slices", 0, "pause this long between monthly slices to throttle source-table read pressure on a busy DB (e.g. 2s)")
	)
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire advisory-lock connection: %w", err)
	}
	defer lockConn.Release()
	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock(4246)`); err != nil {
		return fmt.Errorf("acquire advisory lock 4246: %w", err)
	}
	defer func() {

		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(4246)`)
	}()

	var minTS, maxTS pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `SELECT MIN(created_at), MAX(created_at) FROM task_usage`).Scan(&minTS, &maxTS); err != nil {
		return fmt.Errorf("scan task_usage time range: %w", err)
	}
	if !minTS.Valid {
		slog.Info("task_usage is empty; nothing to backfill")
		if *dryRun {
			return nil
		}
		return stampWatermark(ctx, pool)
	}

	from, end, truncated := backfillWindow(minTS.Time, maxTS.Time, time.Now(), *monthsBack)
	if truncated {

		if !*forcePartial {
			return fmt.Errorf("--months-back=%d would skip buckets before %s (oldest available %s) and the watermark would still advance past them; re-run with --force-partial to accept this, or omit --months-back for a full backfill",
				*monthsBack, from.Format(time.RFC3339), minTS.Time.UTC().Format(time.RFC3339))
		}
		slog.Warn("partial backfill: --months-back limits coverage; older buckets will be left empty and the watermark will still advance past them",
			"months_back", *monthsBack, "effective_from", from.Format(time.RFC3339),
			"oldest_available", minTS.Time.UTC().Format(time.RFC3339))
	}

	slog.Info("backfill range", "from", from.Format(time.RFC3339), "to", end.Format(time.RFC3339), "dry_run", *dryRun, "sleep_between_slices", sleep.String())

	slices := monthSlices(from, end)
	var totalRows int64
	for i, s := range slices {
		cursor, next := s[0], s[1]
		if *dryRun {
			slog.Info("would roll up slice", "from", cursor.Format(time.RFC3339), "to", next.Format(time.RFC3339))
			continue
		}
		var rows int64
		err := pool.QueryRow(
			ctx,
			`SELECT rollup_task_usage_hourly_window($1::timestamptz, $2::timestamptz)`,
			cursor, next,
		).Scan(&rows)
		if err != nil {
			return fmt.Errorf("rollup slice %s..%s: %w", cursor.Format(time.RFC3339), next.Format(time.RFC3339), err)
		}
		totalRows += rows
		slog.Info("rolled up slice", "from", cursor.Format(time.RFC3339), "to", next.Format(time.RFC3339), "rows_touched", rows)
		if *sleep > 0 && i < len(slices)-1 {
			select {
			case <-time.After(*sleep):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	if *dryRun {
		slog.Info("dry-run complete; watermark left untouched")
		return nil
	}

	if err := stampWatermark(context.Background(), pool); err != nil {
		return err
	}
	slog.Info("backfill complete", "total_rows_touched", totalRows)
	return nil
}

func stampWatermark(ctx context.Context, pool *pgxpool.Pool) error {
	tag, err := pool.Exec(ctx, `
		UPDATE task_usage_hourly_rollup_state
		   SET watermark_at = now() - INTERVAL '5 minutes'
		 WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("stamp watermark: %w", err)
	}
	if tag.RowsAffected() == 0 {
		slog.Warn("no rollup state row to stamp; was the task_usage_hourly schema migration applied?")
		return nil
	}
	fmt.Println("watermark stamped to now() - 5 minutes")
	return nil
}

func monthFloor(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func backfillWindow(min, max, now time.Time, monthsBack int) (from, end time.Time, truncated bool) {
	from = monthFloor(min.UTC())
	end = monthFloor(max.UTC()).AddDate(0, 1, 0)
	if monthsBack > 0 {
		if cutoff := monthFloor(now.UTC()).AddDate(0, -monthsBack, 0); cutoff.After(from) {
			return cutoff, end, true
		}
	}
	return from, end, false
}

func monthSlices(from, end time.Time) [][2]time.Time {
	var out [][2]time.Time
	for cursor := from; cursor.Before(end); cursor = cursor.AddDate(0, 1, 0) {
		out = append(out, [2]time.Time{cursor, cursor.AddDate(0, 1, 0)})
	}
	return out
}
