package autopilot

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"
)

// advisoryLockKey — единый на весь деплой ключ pg_try_advisory_lock; любое
// стабильное число подошло бы, здесь — FNV-64 от строки-имени, чтобы не
// хардкодить магическое число без объяснения происхождения.
var advisoryLockKey = int64(mustFNV("goosar_autopilot_scheduler"))

func mustFNV(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// TryAdvisoryLock реализует SchedulerStore для production: сессионный
// advisory lock на выделенном соединении пула — держится ровно на время
// одного Tick (защита от того, что несколько инстансов server2 одновременно
// обработают одну и ту же пачку due-триггеров), не между тиками.
func (s *Store) TryAdvisoryLock(ctx context.Context) (unlock func(context.Context), ok bool, err error) {
	conn, err := s.db.Pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("autopilot: получение соединения для advisory lock: %w", err)
	}
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, advisoryLockKey).Scan(&ok); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("autopilot: pg_try_advisory_lock: %w", err)
	}
	if !ok {
		conn.Release()
		return nil, false, nil
	}
	return func(unlockCtx context.Context) {
		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
		conn.Release()
	}, true, nil
}

// DueScheduleTriggers — schedule-триггеры активных, не архивных/paused
// автопилотов, чей next_run_at уже настал; FOR UPDATE SKIP LOCKED — ещё один
// слой защиты от гонки между инстансами (в дополнение к advisory lock выше:
// он не даёт двум *одновременным* Tick'ам разных инстансов, случайно не
// защищённым из-за истёкшего таймаута захвата, обработать одну и ту же
// строку дважды).
func (s *Store) DueScheduleTriggers(ctx context.Context, asOf time.Time, limit int) ([]DueTrigger, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT st.id, st.sentinel_id, st.strig_cron_expression, COALESCE(st.strig_timezone, 'UTC')
		FROM sentinel_triggers st
		JOIN sentinels s ON s.id = st.sentinel_id
		WHERE st.strig_kind = 'schedule' AND st.strig_enabled AND s.sen_status = 'active'
			AND st.strig_next_run_at IS NOT NULL AND st.strig_next_run_at <= $1
		ORDER BY st.strig_next_run_at
		LIMIT $2
		FOR UPDATE OF st SKIP LOCKED`, asOf, limit)
	if err != nil {
		return nil, fmt.Errorf("autopilot: выборка due-триггеров: %w", err)
	}
	defer rows.Close()
	var out []DueTrigger
	for rows.Next() {
		var t DueTrigger
		if err := rows.Scan(&t.TriggerID, &t.SentinelID, &t.CronExpression, &t.Timezone); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// MarkFired обновляет strig_last_fired_at/strig_next_run_at после срабатывания.
func (s *Store) MarkFired(ctx context.Context, triggerID string, firedAt time.Time, nextRunAt *time.Time) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE sentinel_triggers SET strig_last_fired_at = $2, strig_next_run_at = $3, updated_at = now()
		WHERE id = $1`, triggerID, firedAt, nextRunAt)
	if err != nil {
		return fmt.Errorf("autopilot: обновление next_run_at триггера: %w", err)
	}
	return nil
}
