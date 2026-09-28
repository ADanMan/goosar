package autopilot

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeClock — часы, полностью управляемые тестом (никакого time.Sleep).
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeSchedulerStore — SchedulerStore в памяти: без Postgres, без advisory
// lock (locked — просто булев флаг), нужен только для проверки логики
// Scheduler.Tick.
type fakeSchedulerStore struct {
	mu       sync.Mutex
	locked   bool
	triggers map[string]*fakeTriggerRow
}

type fakeTriggerRow struct {
	DueTrigger
	nextRunAt   time.Time
	lastFiredAt *time.Time
	enabled     bool
	autopilotOK bool // false — автопилот не active, строка не должна попадать в DueScheduleTriggers
}

func newFakeSchedulerStore() *fakeSchedulerStore {
	return &fakeSchedulerStore{triggers: make(map[string]*fakeTriggerRow)}
}

func (f *fakeSchedulerStore) TryAdvisoryLock(ctx context.Context) (func(context.Context), bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.locked {
		return nil, false, nil
	}
	f.locked = true
	return func(context.Context) {
		f.mu.Lock()
		f.locked = false
		f.mu.Unlock()
	}, true, nil
}

func (f *fakeSchedulerStore) DueScheduleTriggers(ctx context.Context, asOf time.Time, limit int) ([]DueTrigger, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []DueTrigger
	for _, row := range f.triggers {
		if !row.enabled || !row.autopilotOK {
			continue
		}
		if row.nextRunAt.After(asOf) {
			continue
		}
		out = append(out, row.DueTrigger)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeSchedulerStore) MarkFired(ctx context.Context, triggerID string, firedAt time.Time, nextRunAt *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.triggers[triggerID]
	if !ok {
		return ErrTriggerNotFound
	}
	row.lastFiredAt = &firedAt
	if nextRunAt == nil {
		row.enabled = false // расписание больше никогда не сработает
		return nil
	}
	row.nextRunAt = *nextRunAt
	return nil
}

// fakeDispatcher считает вызовы DispatchSchedule по каждому sentinelID.
type fakeDispatcher struct {
	mu    sync.Mutex
	calls []string // sentinelID:triggerID
	err   error
}

func (f *fakeDispatcher) DispatchSchedule(ctx context.Context, sentinelID, triggerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sentinelID+":"+triggerID)
	return f.err
}

func TestScheduler_Tick_FiresDueTriggerAndReschedules(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 8, 59, 0, 0, time.UTC)}
	store := newFakeSchedulerStore()
	store.triggers["trig-1"] = &fakeTriggerRow{
		DueTrigger:  DueTrigger{TriggerID: "trig-1", SentinelID: "sen-1", CronExpression: "0 9 * * *", Timezone: "UTC"},
		nextRunAt:   time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		enabled:     true,
		autopilotOK: true,
	}
	disp := &fakeDispatcher{}
	sched := &Scheduler{Store: store, Dispatcher: disp, Clock: clock, BatchLimit: 10}

	// до 9:00 — ещё не должно сработать
	if err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(disp.calls) != 0 {
		t.Fatalf("Tick before due time fired: %v", disp.calls)
	}

	clock.Advance(2 * time.Minute) // 09:01
	if err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(disp.calls) != 1 || disp.calls[0] != "sen-1:trig-1" {
		t.Fatalf("Tick: expected one dispatch call for sen-1:trig-1, got %v", disp.calls)
	}

	row := store.triggers["trig-1"]
	wantNext := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	if !row.nextRunAt.Equal(wantNext) {
		t.Errorf("next_run_at after fire = %v, want %v", row.nextRunAt, wantNext)
	}
	if row.lastFiredAt == nil || !row.lastFiredAt.Equal(clock.Now()) {
		t.Errorf("last_fired_at = %v, want %v", row.lastFiredAt, clock.Now())
	}

	// повторный тик в ту же минуту — уже не due (next_run_at сдвинут на завтра)
	disp.calls = nil
	if err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick (второй раз): %v", err)
	}
	if len(disp.calls) != 0 {
		t.Fatalf("Tick повторно сработал в ту же минуту: %v", disp.calls)
	}
}

func TestScheduler_Tick_SkipsWhenAdvisoryLockHeld(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 9, 1, 0, 0, time.UTC)}
	store := newFakeSchedulerStore()
	store.triggers["trig-1"] = &fakeTriggerRow{
		DueTrigger:  DueTrigger{TriggerID: "trig-1", SentinelID: "sen-1", CronExpression: "0 9 * * *", Timezone: "UTC"},
		nextRunAt:   time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		enabled:     true,
		autopilotOK: true,
	}
	disp := &fakeDispatcher{}
	sched := &Scheduler{Store: store, Dispatcher: disp, Clock: clock}

	store.locked = true // симулирует другой инстанс, уже держащий lock
	if err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(disp.calls) != 0 {
		t.Fatalf("Tick fired while lock was held by another instance: %v", disp.calls)
	}
}

func TestScheduler_Tick_IgnoresDisabledAndInactiveAutopilots(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 9, 1, 0, 0, time.UTC)}
	store := newFakeSchedulerStore()
	store.triggers["disabled"] = &fakeTriggerRow{
		DueTrigger: DueTrigger{TriggerID: "disabled", SentinelID: "sen-1", CronExpression: "0 9 * * *", Timezone: "UTC"},
		nextRunAt:  time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		enabled:    false, autopilotOK: true,
	}
	store.triggers["paused-autopilot"] = &fakeTriggerRow{
		DueTrigger: DueTrigger{TriggerID: "paused-autopilot", SentinelID: "sen-2", CronExpression: "0 9 * * *", Timezone: "UTC"},
		nextRunAt:  time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		enabled:    true, autopilotOK: false,
	}
	disp := &fakeDispatcher{}
	sched := &Scheduler{Store: store, Dispatcher: disp, Clock: clock}

	if err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(disp.calls) != 0 {
		t.Fatalf("Tick fired a disabled/inactive trigger: %v", disp.calls)
	}
}

func TestScheduler_Tick_ContinuesAfterDispatchError(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 9, 1, 0, 0, time.UTC)}
	store := newFakeSchedulerStore()
	store.triggers["trig-1"] = &fakeTriggerRow{
		DueTrigger: DueTrigger{TriggerID: "trig-1", SentinelID: "sen-1", CronExpression: "0 9 * * *", Timezone: "UTC"},
		nextRunAt:  time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		enabled:    true, autopilotOK: true,
	}
	disp := &fakeDispatcher{err: context.DeadlineExceeded}
	sched := &Scheduler{Store: store, Dispatcher: disp, Clock: clock}

	if err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v (a per-trigger dispatch error must not fail the whole tick)", err)
	}
	// next_run_at должен всё равно пересчитаться, даже если сам диспетч
	// вернул ошибку (иначе сломанный агент навсегда держал бы триггер
	// в due-состоянии).
	row := store.triggers["trig-1"]
	wantNext := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	if !row.nextRunAt.Equal(wantNext) {
		t.Errorf("next_run_at after failed dispatch = %v, want %v", row.nextRunAt, wantNext)
	}
}
