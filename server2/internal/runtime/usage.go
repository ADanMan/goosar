// usage.go — статистика токенов/стоимости и почасовая активность runtime
// (dispatch_usage/dispatch_jobs — таблицы домена dispatch, читаются здесь
// напрямую по join, без изменения internal/dispatch: это агрегаты на
// чтение, docs/51-data-model.md прямо относит их к "считаются на чтении, не
// материализуются отдельной таблицей").
package runtime

import (
	"context"
	"fmt"
	"time"
)

// UsageRow — общие поля UsageTokenFields контракта.
type UsageRow struct {
	Provider         *string
	Model            string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSDTicks     int64
}

// UsageDaily — RuntimeUsage[] (GET .../usage), сгруппировано по дню (UTC —
// contract позволяет tz из профиля пользователя, здесь всегда UTC,
// упрощение зафиксировано в decisions.md) и (provider, model).
type UsageDaily struct {
	UsageRow
	Date time.Time
}

func (s *Store) UsageDaily(ctx context.Context, executorID string, days int) ([]UsageDaily, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT du.du_provider, du.du_model, date_trunc('day', du.created_at)::date,
			SUM(du.du_input_tokens), SUM(du.du_output_tokens), SUM(du.du_cache_read_tokens),
			SUM(du.du_cache_write_tokens), SUM(du.du_cost_usd_ticks)
		FROM dispatch_usage du JOIN dispatch_jobs dj ON dj.id = du.dispatch_job_id
		WHERE dj.executor_id = $1 AND du.created_at >= now() - make_interval(days => $2)
		GROUP BY du.du_provider, du.du_model, date_trunc('day', du.created_at)
		ORDER BY 3 DESC`, executorID, days)
	if err != nil {
		return nil, fmt.Errorf("runtime: дневная статистика: %w", err)
	}
	defer rows.Close()
	var out []UsageDaily
	for rows.Next() {
		var u UsageDaily
		if err := rows.Scan(&u.Provider, &u.Model, &u.Date, &u.InputTokens, &u.OutputTokens,
			&u.CacheReadTokens, &u.CacheWriteTokens, &u.CostUSDTicks); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UsageByAgent — RuntimeUsageByAgent[].
type UsageByAgent struct {
	UsageRow
	AgentID   string
	TaskCount int64
}

func (s *Store) UsageByAgent(ctx context.Context, executorID string, days int) ([]UsageByAgent, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT dj.operative_id, du.du_provider, du.du_model, COUNT(DISTINCT dj.id),
			SUM(du.du_input_tokens), SUM(du.du_output_tokens), SUM(du.du_cache_read_tokens),
			SUM(du.du_cache_write_tokens), SUM(du.du_cost_usd_ticks)
		FROM dispatch_usage du JOIN dispatch_jobs dj ON dj.id = du.dispatch_job_id
		WHERE dj.executor_id = $1 AND du.created_at >= now() - make_interval(days => $2)
		GROUP BY dj.operative_id, du.du_provider, du.du_model
		ORDER BY 4 DESC`, executorID, days)
	if err != nil {
		return nil, fmt.Errorf("runtime: статистика по агенту: %w", err)
	}
	defer rows.Close()
	var out []UsageByAgent
	for rows.Next() {
		var u UsageByAgent
		if err := rows.Scan(&u.AgentID, &u.Provider, &u.Model, &u.TaskCount, &u.InputTokens, &u.OutputTokens,
			&u.CacheReadTokens, &u.CacheWriteTokens, &u.CostUSDTicks); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UsageByHour — RuntimeUsageByHour[].
type UsageByHour struct {
	UsageRow
	Hour      int
	TaskCount int64
}

func (s *Store) UsageByHour(ctx context.Context, executorID string, days int) ([]UsageByHour, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT EXTRACT(HOUR FROM du.created_at)::int, du.du_provider, du.du_model, COUNT(DISTINCT dj.id),
			SUM(du.du_input_tokens), SUM(du.du_output_tokens), SUM(du.du_cache_read_tokens),
			SUM(du.du_cache_write_tokens), SUM(du.du_cost_usd_ticks)
		FROM dispatch_usage du JOIN dispatch_jobs dj ON dj.id = du.dispatch_job_id
		WHERE dj.executor_id = $1 AND du.created_at >= now() - make_interval(days => $2)
		GROUP BY 1, du.du_provider, du.du_model
		ORDER BY 1`, executorID, days)
	if err != nil {
		return nil, fmt.Errorf("runtime: статистика по часу: %w", err)
	}
	defer rows.Close()
	var out []UsageByHour
	for rows.Next() {
		var u UsageByHour
		if err := rows.Scan(&u.Hour, &u.Provider, &u.Model, &u.TaskCount, &u.InputTokens, &u.OutputTokens,
			&u.CacheReadTokens, &u.CacheWriteTokens, &u.CostUSDTicks); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ActivityHeatmap — GET .../activity: число задач по часу суток (0..23).
type ActivityHour struct {
	Hour  int `json:"hour"`
	Count int `json:"count"`
}

func (s *Store) ActivityHeatmap(ctx context.Context, executorID string) ([]ActivityHour, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT EXTRACT(HOUR FROM created_at)::int AS hour, COUNT(*)
		FROM dispatch_jobs WHERE executor_id = $1
		GROUP BY 1 ORDER BY 1`, executorID)
	if err != nil {
		return nil, fmt.Errorf("runtime: почасовая активность: %w", err)
	}
	defer rows.Close()
	byHour := make(map[int]int, 24)
	for rows.Next() {
		var hour, count int
		if err := rows.Scan(&hour, &count); err != nil {
			return nil, err
		}
		byHour[hour] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ActivityHour, 24)
	for h := 0; h < 24; h++ {
		out[h] = ActivityHour{Hour: h, Count: byHour[h]}
	}
	return out, nil
}
