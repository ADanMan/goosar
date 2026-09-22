// Пакет taskusagebackfill заполняет task_usage_hourly из исторических строк
// task_usage. Используется командой backfill_task_usage_hourly.
package taskusagebackfill

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const AdvisoryLockKey int64 = 4246

const MaxLagThreshold = time.Hour

type Result struct {
	Skipped string

	SlicesProcessed int

	RowsTouched int64

	From time.Time
	To   time.Time

	WatermarkStamped bool
}

type HookOptions struct {
	Logger *slog.Logger

	LagThreshold time.Duration

	SleepBetweenSlices time.Duration
}

func Hook(ctx context.Context, pool *pgxpool.Pool, opts HookOptions) (Result, error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	threshold := opts.LagThreshold
	if threshold <= 0 {
		threshold = MaxLagThreshold
	}

	stateExists, err := rollupStateExists(ctx, pool)
	if err != nil {
		return Result{}, fmt.Errorf("check rollup state existence: %w", err)
	}
	if !stateExists {
		log.Info("task_usage hourly rollup hook: rollup state tables not present, skipping",
			"reason", "migrations 101/102 not yet applied")
		return Result{Skipped: "rollup_state_missing"}, nil
	}

	usageRange, err := loadUsageRange(ctx, pool)
	if err != nil {
		return Result{}, fmt.Errorf("load task_usage range: %w", err)
	}
	watermark, err := loadWatermark(ctx, pool)
	if err != nil {
		return Result{}, fmt.Errorf("load rollup watermark: %w", err)
	}

	if !usageRange.HasRows {

		if err := stampWatermark(ctx, pool); err != nil {
			return Result{}, err
		}
		log.Info("task_usage hourly rollup hook: task_usage empty, watermark stamped from db now()")
		return Result{Skipped: "task_usage_empty", WatermarkStamped: true}, nil
	}

	if !watermark.Valid {

		return Result{}, errors.New("task_usage_hourly_rollup_state row is missing or watermark is NULL; manual intervention required before migration 103")
	}

	maxEvent := usageRange.MaxEvent
	lag := maxEvent.Sub(watermark.Time)
	if lag <= threshold {
		log.Info("task_usage hourly rollup hook: watermark already current, skipping backfill",
			"watermark_at", watermark.Time.UTC().Format(time.RFC3339),
			"max_event", maxEvent.UTC().Format(time.RFC3339),
			"lag", lag.String(),
			"threshold", threshold.String())

		if err := stampWatermark(ctx, pool); err != nil {
			return Result{}, err
		}
		return Result{Skipped: "watermark_within_threshold", WatermarkStamped: true}, nil
	}

	log.Info("task_usage hourly rollup hook: backfilling under advisory lock",
		"watermark_at", watermark.Time.UTC().Format(time.RFC3339),
		"max_event", maxEvent.UTC().Format(time.RFC3339),
		"lag", lag.String(),
		"threshold", threshold.String())

	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquire advisory-lock connection: %w", err)
	}
	defer lockConn.Release()

	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock($1)`, AdvisoryLockKey); err != nil {
		return Result{}, fmt.Errorf("acquire advisory lock %d: %w", AdvisoryLockKey, err)
	}

	defer func() {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, AdvisoryLockKey)
	}()

	from := monthFloor(usageRange.MinEvent)
	end := monthFloor(maxEvent).AddDate(0, 1, 0)
	res := Result{From: from, To: end}

	cursor := from
	for cursor.Before(end) {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}

		next := cursor.AddDate(0, 1, 0)
		var rows int64
		err := lockConn.QueryRow(
			ctx,
			`SELECT rollup_task_usage_hourly_window($1::timestamptz, $2::timestamptz)`,
			cursor, next,
		).Scan(&rows)
		if err != nil {
			return res, fmt.Errorf("rollup slice %s..%s: %w",
				cursor.Format(time.RFC3339), next.Format(time.RFC3339), err)
		}
		res.SlicesProcessed++
		res.RowsTouched += rows
		log.Info("task_usage hourly rollup hook: slice complete",
			"from", cursor.Format(time.RFC3339),
			"to", next.Format(time.RFC3339),
			"rows_touched", rows)
		cursor = next
		if opts.SleepBetweenSlices > 0 && cursor.Before(end) {
			select {
			case <-time.After(opts.SleepBetweenSlices):
			case <-ctx.Done():
				return res, ctx.Err()
			}
		}
	}

	if err := stampWatermarkOnConn(ctx, lockConn.Conn()); err != nil {
		return res, err
	}
	res.WatermarkStamped = true

	log.Info("task_usage hourly rollup hook: complete",
		"slices", res.SlicesProcessed,
		"total_rows_touched", res.RowsTouched,
		"watermark_source", "db_now")
	return res, nil
}

type usageRange struct {
	HasRows  bool
	MinEvent time.Time
	MaxEvent time.Time
}

func loadUsageRange(ctx context.Context, pool *pgxpool.Pool) (usageRange, error) {
	var minTS, maxTS pgtype.Timestamptz

	err := pool.QueryRow(ctx, `
		SELECT MIN(created_at), MAX(COALESCE(updated_at, created_at))
		  FROM task_usage
	`).Scan(&minTS, &maxTS)
	if err != nil {
		return usageRange{}, err
	}
	if !minTS.Valid || !maxTS.Valid {
		return usageRange{HasRows: false}, nil
	}
	return usageRange{
		HasRows:  true,
		MinEvent: minTS.Time.UTC(),
		MaxEvent: maxTS.Time.UTC(),
	}, nil
}

func loadWatermark(ctx context.Context, pool *pgxpool.Pool) (pgtype.Timestamptz, error) {
	var watermark pgtype.Timestamptz
	err := pool.QueryRow(ctx, `
		SELECT watermark_at FROM task_usage_hourly_rollup_state WHERE id = 1
	`).Scan(&watermark)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.Timestamptz{}, nil
		}
		return pgtype.Timestamptz{}, err
	}
	return watermark, nil
}

func rollupStateExists(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'public'
			   AND table_name = 'task_usage_hourly_rollup_state'
		)
	`).Scan(&exists)
	return exists, err
}

func stampWatermark(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		UPDATE task_usage_hourly_rollup_state
		   SET watermark_at = now() - INTERVAL '5 minutes'
		 WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("stamp watermark: %w", err)
	}
	return nil
}

func stampWatermarkOnConn(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `
		UPDATE task_usage_hourly_rollup_state
		   SET watermark_at = now() - INTERVAL '5 minutes'
		 WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("stamp watermark: %w", err)
	}
	return nil
}

func monthFloor(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
