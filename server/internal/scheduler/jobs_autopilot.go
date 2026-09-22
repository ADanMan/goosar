package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const JobNameAutopilotScheduleDispatch = "autopilot_schedule_dispatch"

const ScopeKindAutopilotTrigger = "autopilot_trigger"

const DefaultAutopilotScheduleTimezone = "UTC"

const maxAutopilotScheduleLateness = 5 * time.Minute

type AutopilotScheduleDispatcher interface {
	DispatchAutopilotForPlan(
		ctx context.Context,
		autopilot db.Autopilot,
		triggerID pgtype.UUID,
		source string,
		payload []byte,
		plannedAt time.Time,
	) (*db.AutopilotRun, error)
}

func AutopilotScheduleDispatchJob(
	pool *pgxpool.Pool,
	queries *db.Queries,
	dispatcher AutopilotScheduleDispatcher,
) JobSpec {
	cache := newAutopilotScheduleCache()

	return JobSpec{
		Name:              JobNameAutopilotScheduleDispatch,
		Cadence:           0,
		ScheduleDelay:     0,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        2 * time.Minute,
		StaleTimeout:      5 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff: []time.Duration{
			1 * time.Minute,
			5 * time.Minute,
			15 * time.Minute,
		},

		MaxPlansPerTick: 5,

		Scopes:        autopilotScopes(pool, queries, cache),
		PlansForScope: autopilotPlansForScope(cache),
		Handler:       autopilotHandler(queries, dispatcher),
	}
}

type autopilotTriggerConfig struct {
	TriggerID      string
	CronExpression string
	Timezone       string
	CreatedAt      time.Time

	LastFiredAt time.Time
}

type autopilotScheduleCache struct {
	mu       sync.RWMutex
	triggers map[string]autopilotTriggerConfig
}

func newAutopilotScheduleCache() *autopilotScheduleCache {
	return &autopilotScheduleCache{triggers: make(map[string]autopilotTriggerConfig)}
}

func (c *autopilotScheduleCache) replace(next map[string]autopilotTriggerConfig) {
	c.mu.Lock()
	c.triggers = next
	c.mu.Unlock()
}

func (c *autopilotScheduleCache) get(id string) (autopilotTriggerConfig, bool) {
	c.mu.RLock()
	v, ok := c.triggers[id]
	c.mu.RUnlock()
	return v, ok
}

func autopilotScopes(
	pool *pgxpool.Pool,
	queries *db.Queries,
	cache *autopilotScheduleCache,
) ScopeProvider {
	_ = pool
	return func(ctx context.Context, now time.Time) ([]Scope, error) {
		rows, err := queries.ListSchedulableAutopilotTriggers(ctx)
		if err != nil {
			return nil, fmt.Errorf("autopilot scope: list schedulable triggers: %w", err)
		}
		next := make(map[string]autopilotTriggerConfig, len(rows))
		scopes := make([]Scope, 0, len(rows))
		for _, r := range rows {
			id := util.UUIDToString(r.ID)
			if id == "" {
				continue
			}
			tz := DefaultAutopilotScheduleTimezone
			if r.Timezone.Valid && r.Timezone.String != "" {
				tz = r.Timezone.String
			}
			cron := ""
			if r.CronExpression.Valid {
				cron = r.CronExpression.String
			}
			if cron == "" {
				continue
			}
			createdAt := time.Time{}
			if r.CreatedAt.Valid {
				createdAt = r.CreatedAt.Time.UTC()
			}
			lastFiredAt := time.Time{}
			if r.LastFiredAt.Valid {
				lastFiredAt = r.LastFiredAt.Time.UTC()
			}
			next[id] = autopilotTriggerConfig{
				TriggerID:      id,
				CronExpression: cron,
				Timezone:       tz,
				CreatedAt:      createdAt,
				LastFiredAt:    lastFiredAt,
			}
			scopes = append(scopes, Scope{Kind: ScopeKindAutopilotTrigger, ID: id})
		}
		cache.replace(next)
		return scopes, nil
	}
}

func autopilotPlansForScope(cache *autopilotScheduleCache) func(
	ctx context.Context, scope Scope, now time.Time, latest LatestPlanInfo,
) ([]time.Time, error) {
	const replayWindow = 24 * time.Hour
	return func(ctx context.Context, scope Scope, now time.Time, latest LatestPlanInfo) ([]time.Time, error) {
		cfg, ok := cache.get(scope.ID)
		if !ok {

			return nil, nil
		}

		if latest.RetryEligible(now) {
			return []time.Time{latest.PlanTime}, nil
		}

		var after time.Time
		switch {
		case latest.Found:
			after = latest.PlanTime
		case !cfg.LastFiredAt.IsZero():
			after = cfg.LastFiredAt
		default:
			after = cfg.CreatedAt
		}

		if oldest := now.Add(-replayWindow); after.Before(oldest) {
			after = oldest
		}

		occs, err := service.NextOccurrencesUTC(cfg.CronExpression, cfg.Timezone, after, now)
		if err != nil {
			return nil, fmt.Errorf("autopilot plans: cron eval for trigger %s: %w", scope.ID, err)
		}
		if len(occs) == 0 {
			return nil, nil
		}

		latestDue := occs[len(occs)-1]
		if isAutopilotSchedulePlanStale(now, latestDue) {
			return nil, nil
		}
		return []time.Time{latestDue}, nil
	}
}

func isAutopilotSchedulePlanStale(now, planTime time.Time) bool {
	return now.Sub(planTime) > maxAutopilotScheduleLateness
}

func autopilotHandler(
	queries *db.Queries,
	dispatcher AutopilotScheduleDispatcher,
) Handler {
	return func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		triggerID, err := parseScopeUUID(in.Scope.ID)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("autopilot handler: scope id is not a valid uuid: %w", err)
		}

		trigger, err := queries.GetAutopilotTrigger(ctx, triggerID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {

				return HandlerResult{RowsAffected: 0, Result: map[string]any{
					"skipped_reason": "trigger_not_found",
				}}, nil
			}
			return HandlerResult{}, fmt.Errorf("load trigger: %w", err)
		}
		if !trigger.Enabled || trigger.Kind != "schedule" {
			return HandlerResult{RowsAffected: 0, Result: map[string]any{
				"skipped_reason": "trigger_disabled",
			}}, nil
		}

		autopilot, err := queries.GetAutopilot(ctx, trigger.AutopilotID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return HandlerResult{RowsAffected: 0, Result: map[string]any{
					"skipped_reason": "autopilot_not_found",
				}}, nil
			}
			return HandlerResult{}, fmt.Errorf("load autopilot: %w", err)
		}
		if autopilot.Status != "active" {
			return HandlerResult{RowsAffected: 0, Result: map[string]any{
				"skipped_reason": "autopilot_inactive",
				"status":         autopilot.Status,
			}}, nil
		}

		run, err := dispatcher.DispatchAutopilotForPlan(
			ctx, autopilot, trigger.ID, "schedule", nil, in.PlanTime,
		)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("dispatch for plan: %w", err)
		}

		tz := DefaultAutopilotScheduleTimezone
		if trigger.Timezone.Valid && trigger.Timezone.String != "" {
			tz = trigger.Timezone.String
		}
		if next, ok := advancedNextRun(trigger.CronExpression.String, tz, in.PlanTime, time.Now()); ok {
			_ = queries.AdvanceTriggerNextRun(ctx, db.AdvanceTriggerNextRunParams{
				ID:        trigger.ID,
				NextRunAt: pgtype.Timestamptz{Time: next, Valid: true},
			})
		} else {
			_ = queries.TouchAutopilotTriggerFiredAt(ctx, trigger.ID)
		}

		return HandlerResult{
			RowsAffected: 1,
			Result: map[string]any{
				"run_id":     util.UUIDToString(run.ID),
				"run_status": run.Status,
			},
		}, nil
	}
}

func advancedNextRun(cronExpr, timezone string, planTime, now time.Time) (time.Time, bool) {
	anchor := now
	if planTime.After(anchor) {
		anchor = planTime
	}
	next, err := service.NextOccurrenceAfterUTC(cronExpr, timezone, anchor)
	if err != nil {
		return time.Time{}, false
	}
	return next, true
}

func parseScopeUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	if !u.Valid {
		return pgtype.UUID{}, errors.New("invalid uuid")
	}
	return u, nil
}
