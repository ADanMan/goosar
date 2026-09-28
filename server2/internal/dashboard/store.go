// Package dashboard реализует тег Dashboard контракта (`/api/dashboard/**`):
// шесть маршрутов только на чтение, агрегирующих dispatch_usage/dispatch_jobs
// (008_dispatch.up.sql) по дням/агенту/провайдеру/модели/причине провала.
//
// Таймзона профиля пользователя (contract §4: "если tz не передан — берётся
// таймзона профиля пользователя, иначе UTC") не резолвится этим доменом —
// решение этой сессии (см. server2/docs/decisions.md, T-028): всегда UTC,
// если query-параметр tz не передан явно, — профиль пользователя/его
// таймзона не хранятся ни в одной уже существующей таблице на момент этой
// сессии.
package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/adanman/goosar/server2/internal/store"
)

// Store — только чтение dispatch_usage/dispatch_jobs.
type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// Window — параметры окна запроса, общие всем шести маршрутам.
type Window struct {
	WorkspaceID string
	Since       time.Time
	TZ          string
	ProjectID   string
}

func (w Window) projectFilter(argN int) (string, []any) {
	if w.ProjectID == "" {
		return "", nil
	}
	return fmt.Sprintf(" AND dj.initiative_id = $%d", argN), []any{w.ProjectID}
}

// UsageRow — одна строка usage/daily или usage/by-agent (общие поля).
type UsageRow struct {
	Provider         string
	Model            string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSDTicks     int64
	TaskCount        int
	Day              *time.Time
	AgentID          *string
}

func (r UsageRow) tokenFields() map[string]any {
	return map[string]any{
		"provider": r.Provider, "model": r.Model,
		"input_tokens": r.InputTokens, "output_tokens": r.OutputTokens,
		"cache_read_tokens": r.CacheReadTokens, "cache_write_tokens": r.CacheWriteTokens,
		"cost_usd_ticks": r.CostUSDTicks,
		// uncosted_* — эта версия не хранит отдельно "некостируемые" токены
		// (нет колонки-классификатора в dispatch_usage); всегда 0, решение
		// этой сессии (см. package doc выше).
		"uncosted_input_tokens": 0, "uncosted_output_tokens": 0,
		"uncosted_cache_read_tokens": 0, "uncosted_cache_write_tokens": 0,
	}
}

// UsageDaily — JSON DashboardUsageDaily.
func (r UsageRow) UsageDaily() map[string]any {
	out := r.tokenFields()
	out["date"] = r.Day.Format("2006-01-02")
	out["task_count"] = r.TaskCount
	return out
}

// UsageByAgent — JSON DashboardUsageByAgent.
func (r UsageRow) UsageByAgent() map[string]any {
	out := r.tokenFields()
	out["agent_id"] = *r.AgentID
	out["task_count"] = r.TaskCount
	return out
}

// UsageDaily — usage/daily: по дням, провайдеру и модели.
func (s *Store) UsageDaily(ctx context.Context, w Window) ([]UsageRow, error) {
	filter, args := w.projectFilter(4)
	rows, err := s.db.Pool.Query(ctx, `
		SELECT date_trunc('day', du.created_at AT TIME ZONE $3)::date AS day,
			COALESCE(du.du_provider,''), du.du_model,
			SUM(du.du_input_tokens), SUM(du.du_output_tokens), SUM(du.du_cache_read_tokens),
			SUM(du.du_cache_write_tokens), SUM(du.du_cost_usd_ticks), COUNT(DISTINCT du.dispatch_job_id)
		FROM dispatch_usage du JOIN dispatch_jobs dj ON dj.id = du.dispatch_job_id
		WHERE dj.workspace_id = $1 AND du.created_at >= $2`+filter+`
		GROUP BY day, du.du_provider, du.du_model ORDER BY day ASC`,
		append([]any{w.WorkspaceID, w.Since, w.TZ}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("dashboard: usage/daily: %w", err)
	}
	defer rows.Close()
	var out []UsageRow
	for rows.Next() {
		var r UsageRow
		var day time.Time
		if err := rows.Scan(&day, &r.Provider, &r.Model, &r.InputTokens, &r.OutputTokens,
			&r.CacheReadTokens, &r.CacheWriteTokens, &r.CostUSDTicks, &r.TaskCount); err != nil {
			return nil, err
		}
		r.Day = &day
		out = append(out, r)
	}
	return out, rows.Err()
}

// UsageByAgent — usage/by-agent: по агенту, провайдеру и модели.
func (s *Store) UsageByAgent(ctx context.Context, w Window) ([]UsageRow, error) {
	filter, args := w.projectFilter(3)
	rows, err := s.db.Pool.Query(ctx, `
		SELECT dj.operative_id, COALESCE(du.du_provider,''), du.du_model,
			SUM(du.du_input_tokens), SUM(du.du_output_tokens), SUM(du.du_cache_read_tokens),
			SUM(du.du_cache_write_tokens), SUM(du.du_cost_usd_ticks), COUNT(DISTINCT du.dispatch_job_id)
		FROM dispatch_usage du JOIN dispatch_jobs dj ON dj.id = du.dispatch_job_id
		WHERE dj.workspace_id = $1 AND du.created_at >= $2`+filter+`
		GROUP BY dj.operative_id, du.du_provider, du.du_model ORDER BY dj.operative_id ASC`,
		append([]any{w.WorkspaceID, w.Since}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("dashboard: usage/by-agent: %w", err)
	}
	defer rows.Close()
	var out []UsageRow
	for rows.Next() {
		var r UsageRow
		var agentID string
		if err := rows.Scan(&agentID, &r.Provider, &r.Model, &r.InputTokens, &r.OutputTokens,
			&r.CacheReadTokens, &r.CacheWriteTokens, &r.CostUSDTicks, &r.TaskCount); err != nil {
			return nil, err
		}
		r.AgentID = &agentID
		out = append(out, r)
	}
	return out, rows.Err()
}

// RunTimeByAgent — agent-runtime.
type RunTimeByAgent struct {
	AgentID      string
	TotalSeconds int64
	TaskCount    int
	FailedCount  int
}

func (s *Store) RunTimeByAgent(ctx context.Context, w Window) ([]RunTimeByAgent, error) {
	filter, args := w.projectFilter(3)
	rows, err := s.db.Pool.Query(ctx, `
		SELECT dj.operative_id,
			COALESCE(SUM(EXTRACT(EPOCH FROM (COALESCE(dj.dj_completed_at, now()) - dj.dj_started_at)))
				FILTER (WHERE dj.dj_started_at IS NOT NULL), 0),
			COUNT(*), COUNT(*) FILTER (WHERE dj.dj_status = 'failed')
		FROM dispatch_jobs dj
		WHERE dj.workspace_id = $1 AND dj.created_at >= $2`+filter+`
		GROUP BY dj.operative_id ORDER BY dj.operative_id ASC`,
		append([]any{w.WorkspaceID, w.Since}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("dashboard: agent-runtime: %w", err)
	}
	defer rows.Close()
	var out []RunTimeByAgent
	for rows.Next() {
		var r RunTimeByAgent
		var seconds float64
		if err := rows.Scan(&r.AgentID, &seconds, &r.TaskCount, &r.FailedCount); err != nil {
			return nil, err
		}
		r.TotalSeconds = int64(seconds)
		out = append(out, r)
	}
	return out, rows.Err()
}

// RunTimeDaily — runtime/daily.
type RunTimeDaily struct {
	Day          time.Time
	TotalSeconds int64
	TaskCount    int
	FailedCount  int
}

func (s *Store) RunTimeDaily(ctx context.Context, w Window) ([]RunTimeDaily, error) {
	filter, args := w.projectFilter(4)
	rows, err := s.db.Pool.Query(ctx, `
		SELECT date_trunc('day', dj.created_at AT TIME ZONE $3)::date AS day,
			COALESCE(SUM(EXTRACT(EPOCH FROM (COALESCE(dj.dj_completed_at, now()) - dj.dj_started_at)))
				FILTER (WHERE dj.dj_started_at IS NOT NULL), 0),
			COUNT(*), COUNT(*) FILTER (WHERE dj.dj_status = 'failed')
		FROM dispatch_jobs dj
		WHERE dj.workspace_id = $1 AND dj.created_at >= $2`+filter+`
		GROUP BY day ORDER BY day ASC`,
		append([]any{w.WorkspaceID, w.Since, w.TZ}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("dashboard: runtime/daily: %w", err)
	}
	defer rows.Close()
	var out []RunTimeDaily
	for rows.Next() {
		var r RunTimeDaily
		var seconds float64
		if err := rows.Scan(&r.Day, &seconds, &r.TaskCount, &r.FailedCount); err != nil {
			return nil, err
		}
		r.TotalSeconds = int64(seconds)
		out = append(out, r)
	}
	return out, rows.Err()
}

// FailureDaily — failures/daily.
type FailureDaily struct {
	Day           time.Time
	FailureReason string
	TaskCount     int
}

func (s *Store) FailureDaily(ctx context.Context, w Window) ([]FailureDaily, error) {
	filter, args := w.projectFilter(4)
	rows, err := s.db.Pool.Query(ctx, `
		SELECT date_trunc('day', dj.created_at AT TIME ZONE $3)::date AS day,
			COALESCE(dj.dj_failure_reason, 'unknown'), COUNT(*)
		FROM dispatch_jobs dj
		WHERE dj.workspace_id = $1 AND dj.dj_status = 'failed' AND dj.created_at >= $2`+filter+`
		GROUP BY day, dj.dj_failure_reason ORDER BY day ASC`,
		append([]any{w.WorkspaceID, w.Since, w.TZ}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("dashboard: failures/daily: %w", err)
	}
	defer rows.Close()
	var out []FailureDaily
	for rows.Next() {
		var r FailureDaily
		if err := rows.Scan(&r.Day, &r.FailureReason, &r.TaskCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FailureByAgent — failures/by-agent (граница окна точная, без запаса в
// один день — contract §4 "особый случай").
type FailureByAgent struct {
	AgentID       string
	FailureReason string
	TaskCount     int
}

func (s *Store) FailureByAgent(ctx context.Context, w Window) ([]FailureByAgent, error) {
	filter, args := w.projectFilter(3)
	rows, err := s.db.Pool.Query(ctx, `
		SELECT dj.operative_id, COALESCE(dj.dj_failure_reason, 'unknown'), COUNT(*)
		FROM dispatch_jobs dj
		WHERE dj.workspace_id = $1 AND dj.dj_status = 'failed' AND dj.created_at >= $2`+filter+`
		GROUP BY dj.operative_id, dj.dj_failure_reason ORDER BY dj.operative_id ASC`,
		append([]any{w.WorkspaceID, w.Since}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("dashboard: failures/by-agent: %w", err)
	}
	defer rows.Close()
	var out []FailureByAgent
	for rows.Next() {
		var r FailureByAgent
		if err := rows.Scan(&r.AgentID, &r.FailureReason, &r.TaskCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
