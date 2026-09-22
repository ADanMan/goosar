// Пакет attributionbackfill приводит строки agent_task_queue к строгому инварианту
// атрибуции перед включением проверяющего CHECK-ограничения.
package attributionbackfill

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultBatchSize = 5000

type Result struct {
	RowsBackfilled int64

	Batches int

	MismatchNormalized int64
}

type HookOptions struct {
	Logger *slog.Logger

	BatchSize int

	Table string
}

const countMismatchSQL = `
SELECT count(*)
FROM %s
WHERE originator_user_id IS NOT NULL
  AND accountable_user_id IS NOT NULL
  AND accountable_user_id <> originator_user_id`

const backfillBatchSQL = `
WITH batch AS (
    SELECT id
    FROM %s
    WHERE originator_user_id IS NOT NULL
      AND accountable_user_id IS DISTINCT FROM originator_user_id
    LIMIT $1
    FOR UPDATE
)
UPDATE %s q
SET accountable_user_id = q.originator_user_id,
    originator_source   = COALESCE(q.originator_source, 'backfill')
FROM batch
WHERE q.id = batch.id
  AND q.originator_user_id IS NOT NULL
  AND q.accountable_user_id IS DISTINCT FROM q.originator_user_id`

func Hook(ctx context.Context, pool *pgxpool.Pool, opts HookOptions) (Result, error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	table := opts.Table
	if table == "" {
		table = "agent_task_queue"
	}

	var res Result

	if err := pool.QueryRow(ctx, fmt.Sprintf(countMismatchSQL, table)).Scan(&res.MismatchNormalized); err != nil {
		return res, fmt.Errorf("count attribution mismatches: %w", err)
	}
	if res.MismatchNormalized > 0 {
		log.Warn("attribution backfill: normalizing rows where accountable_user_id disagreed with a non-NULL originator_user_id; originator is authoritative but these are worth auditing",
			"mismatch_rows", res.MismatchNormalized)
	}

	updateSQL := fmt.Sprintf(backfillBatchSQL, table, table)
	for {
		tag, err := pool.Exec(ctx, updateSQL, batchSize)
		if err != nil {
			return res, fmt.Errorf("backfill accountable_user_id batch: %w", err)
		}
		n := tag.RowsAffected()
		if n == 0 {
			break
		}
		res.RowsBackfilled += n
		res.Batches++
		log.Info("attribution backfill: batch reconciled",
			"rows", n,
			"total", res.RowsBackfilled)
	}

	if res.RowsBackfilled == 0 {
		log.Info("attribution backfill: no rows needed reconciliation before migration 198")
	} else {
		log.Info("attribution backfill: complete",
			"rows_backfilled", res.RowsBackfilled,
			"batches", res.Batches,
			"mismatch_normalized", res.MismatchNormalized)
	}
	return res, nil
}
