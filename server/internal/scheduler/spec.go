// Пакет scheduler — планировщик периодических задач на базе БД: таблица
// sys_cron_executions служит распределённой блокировкой и журналом; уникальный ключ
// гарантирует, что каждый запуск выполняет один экземпляр.
package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type CatchUpMode int

const (
	CatchUpLatestOnly CatchUpMode = iota

	CatchUpEveryPlan
)

func (m CatchUpMode) String() string {
	switch m {
	case CatchUpLatestOnly:
		return "latest_only"
	case CatchUpEveryPlan:
		return "every_plan"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}

type Scope struct {
	Kind string
	ID   string
}

var ScopeGlobal = Scope{Kind: "global", ID: "global"}

func (s Scope) String() string { return s.Kind + "/" + s.ID }

type ScopeProvider func(ctx context.Context, now time.Time) ([]Scope, error)

type HandlerInput struct {
	Job       *JobSpec
	Scope     Scope
	PlanTime  time.Time
	Attempt   int
	RunnerID  string
	Heartbeat func(ctx context.Context) error
}

type HandlerResult struct {
	RowsAffected int64
	Result       map[string]any
}

type Handler func(ctx context.Context, in HandlerInput) (HandlerResult, error)

type JobSpec struct {
	Name string

	Cadence time.Duration

	ScheduleDelay time.Duration

	CatchUpMode CatchUpMode

	CatchUpWindow time.Duration

	MaxPlansPerTick int

	RunTimeout time.Duration

	StaleTimeout time.Duration

	HeartbeatInterval time.Duration

	AllowStaleReentry bool

	MaxAttempts int

	RetryBackoff []time.Duration

	Scopes ScopeProvider

	PlansForScope func(ctx context.Context, scope Scope, now time.Time,
		latest LatestPlanInfo) ([]time.Time, error)

	Handler Handler
}

func StaticScopes(scopes ...Scope) ScopeProvider {
	frozen := append([]Scope(nil), scopes...)
	return func(_ context.Context, _ time.Time) ([]Scope, error) {
		return frozen, nil
	}
}

func (j *JobSpec) validate() error {
	if strings.TrimSpace(j.Name) == "" {
		return fmt.Errorf("scheduler: job name is required")
	}
	if j.PlansForScope == nil && j.Cadence <= 0 {
		return fmt.Errorf("scheduler: job %q: cadence must be > 0 (or set PlansForScope)", j.Name)
	}
	if j.RunTimeout <= 0 {
		return fmt.Errorf("scheduler: job %q: run_timeout must be > 0", j.Name)
	}
	if j.StaleTimeout <= j.RunTimeout {
		return fmt.Errorf("scheduler: job %q: stale_timeout (%s) must be greater than run_timeout (%s)",
			j.Name, j.StaleTimeout, j.RunTimeout)
	}
	if j.HeartbeatInterval <= 0 || j.HeartbeatInterval >= j.StaleTimeout {
		return fmt.Errorf("scheduler: job %q: heartbeat_interval must be > 0 and < stale_timeout", j.Name)
	}
	if j.MaxAttempts < 1 {
		return fmt.Errorf("scheduler: job %q: max_attempts must be >= 1", j.Name)
	}
	if j.Scopes == nil {
		return fmt.Errorf("scheduler: job %q: scopes provider is required", j.Name)
	}
	if j.Handler == nil {
		return fmt.Errorf("scheduler: job %q: handler is required", j.Name)
	}
	if j.PlansForScope == nil && j.CatchUpMode == CatchUpEveryPlan && j.MaxPlansPerTick <= 0 {
		return fmt.Errorf("scheduler: job %q: max_plans_per_tick must be > 0 for every_plan catch-up", j.Name)
	}
	return nil
}

func (j *JobSpec) retryDelay(attempt int) time.Duration {
	if len(j.RetryBackoff) == 0 {
		return 0
	}
	idx := attempt - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(j.RetryBackoff) {
		idx = len(j.RetryBackoff) - 1
	}
	return j.RetryBackoff[idx]
}

func FloorPlan(eligible time.Time, c time.Duration) time.Time {
	if c <= 0 {
		return eligible.UTC()
	}
	t := eligible.UTC()
	return t.Truncate(c)
}
