// scheduler.go — фоновый планировщик расписаний автопилотов (contract §7:
// "Триггер: schedule (cron + таймзона)"). Свой цикл в процессе сервера, без
// внешней cron-библиотеки (ADR 0001-stack.md: robfig/cron исключён правилами
// тикета T-028) — разбор выражений в cron.go, здесь только цикл опроса.
//
// Защита от двойного запуска при нескольких инстансах сервера — advisory
// lock Postgres (pg_try_advisory_lock), см. SchedulerStore.TryAdvisoryLock и
// его реализацию в scheduler_store.go: только один инстанс единовременно
// обрабатывает пачку due-триггеров, остальные тихо пропускают тик.
//
// Логика цикла (Tick) отделена от SQL и системных часов через два узких
// интерфейса (SchedulerStore, Clock) специально для юнит-теста с фиктивными
// часами и in-memory реализацией стора (см. scheduler_test.go) — интеграционный
// прогон с реальным Postgres — store_integration_test.go.
package autopilot

import (
	"context"
	"log/slog"
	"time"
)

// Clock — абстракция текущего времени, чтобы тест мог управлять им явно.
type Clock interface{ Now() time.Time }

// RealClock — обычные системные часы (UTC).
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// DueTrigger — минимум, нужный планировщику про один готовый к срабатыванию
// schedule-триггер.
type DueTrigger struct {
	TriggerID      string
	SentinelID     string
	CronExpression string
	Timezone       string
}

// SchedulerStore — SQL-поверхность, нужная планировщику; production-реализация
// — *Store (см. scheduler_store.go), тестовая — fakeSchedulerStore
// (scheduler_test.go).
type SchedulerStore interface {
	// TryAdvisoryLock пытается захватить единый на весь деплой advisory
	// lock; ok=false — уже занят другим инстансом, тик надо тихо пропустить.
	// unlock всегда небудет nil, если ok=true (вызывать в defer).
	TryAdvisoryLock(ctx context.Context) (unlock func(context.Context), ok bool, err error)
	// DueScheduleTriggers — enabled-триггеры активных автопилотов, чей
	// strig_next_run_at <= asOf, не более limit штук, упорядоченные по
	// next_run_at (самые просроченные — первыми).
	DueScheduleTriggers(ctx context.Context, asOf time.Time, limit int) ([]DueTrigger, error)
	// MarkFired фиксирует срабатывание: strig_last_fired_at=firedAt,
	// strig_next_run_at=nextRunAt (nil, если расписание больше никогда не
	// сработает — contract не описывает этот край, next_run_at тогда просто
	// остаётся null, autopilot больше не появляется в cron-очереди).
	MarkFired(ctx context.Context, triggerID string, firedAt time.Time, nextRunAt *time.Time) error
}

// RunDispatcher — то немногое, что планировщику нужно от Dispatcher, чтобы
// не тянуть весь internal/dispatch+internal/task в юнит-тест.
type RunDispatcher interface {
	DispatchSchedule(ctx context.Context, sentinelID, triggerID string) error
}

// Scheduler — фоновый цикл. Interval — период опроса (по умолчанию 30с в
// production, см. deps.go); BatchLimit — сколько due-триггеров обрабатывать
// за один Tick (по умолчанию 50).
type Scheduler struct {
	Store      SchedulerStore
	Dispatcher RunDispatcher
	Clock      Clock
	Logger     *slog.Logger
	Interval   time.Duration
	BatchLimit int
}

// Run крутит цикл до отмены ctx — вызывается один раз из cmd/server в своей
// горутине.
func (s *Scheduler) Run(ctx context.Context) {
	interval := s.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && s.Logger != nil {
				s.Logger.Error("autopilot: тик планировщика", "error", err)
			}
		}
	}
}

// Tick — одна итерация: захват advisory lock, выборка due-триггеров,
// диспетч каждого, пересчёт next_run_at. Экспортирован (не только для
// цикла Run) — тесты и, при желании, ручной "прогнать сейчас" зовут его
// напрямую.
func (s *Scheduler) Tick(ctx context.Context) error {
	unlock, ok, err := s.Store.TryAdvisoryLock(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return nil // другой инстанс уже обрабатывает эту пачку
	}
	defer unlock(ctx)

	limit := s.BatchLimit
	if limit <= 0 {
		limit = 50
	}
	now := s.Clock.Now()
	due, err := s.Store.DueScheduleTriggers(ctx, now, limit)
	if err != nil {
		return err
	}
	for _, t := range due {
		s.fire(ctx, t, now)
	}
	return nil
}

func (s *Scheduler) fire(ctx context.Context, t DueTrigger, now time.Time) {
	next := s.nextRunAt(t, now)
	if err := s.Dispatcher.DispatchSchedule(ctx, t.SentinelID, t.TriggerID); err != nil && s.Logger != nil {
		s.Logger.Error("autopilot: диспетч по расписанию", "trigger_id", t.TriggerID, "error", err)
	}
	if err := s.Store.MarkFired(ctx, t.TriggerID, now, next); err != nil && s.Logger != nil {
		s.Logger.Error("autopilot: пересчёт next_run_at", "trigger_id", t.TriggerID, "error", err)
	}
}

func (s *Scheduler) nextRunAt(t DueTrigger, now time.Time) *time.Time {
	loc, err := LoadTimezone(t.Timezone)
	if err != nil {
		return nil
	}
	sched, err := ParseCron(t.CronExpression)
	if err != nil {
		return nil
	}
	next, ok := sched.NextInLocation(now, loc)
	if !ok {
		return nil
	}
	return &next
}
