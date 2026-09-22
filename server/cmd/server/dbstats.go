package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	dbStatsInterval = 15 * time.Second

	defaultMaxConns int32 = 25
	defaultMinConns int32 = 5
)

func newDBPool(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	urlParams := poolParamsFromURL(dbURL)

	maxFallback := defaultMaxConns
	if urlParams["pool_max_conns"] {
		maxFallback = cfg.MaxConns
	}
	cfg.MaxConns = envInt32("DATABASE_MAX_CONNS", maxFallback)

	minFallback := defaultMinConns
	if urlParams["pool_min_conns"] {
		minFallback = cfg.MinConns
	}
	cfg.MinConns = envInt32("DATABASE_MIN_CONNS", minFallback)

	if cfg.MinConns > cfg.MaxConns {
		cfg.MinConns = cfg.MaxConns
	}

	return pgxpool.NewWithConfig(ctx, cfg)
}

func poolParamsFromURL(dbURL string) map[string]bool {
	out := map[string]bool{}
	u, err := url.Parse(dbURL)
	if err != nil {
		return out
	}
	for k := range u.Query() {
		out[k] = true
	}
	return out
}

func envInt32(name string, def int32) int32 {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, using default",
			"name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return int32(v)
}

func logPoolConfig(pool *pgxpool.Pool) {
	cfg := pool.Config()
	slog.Info("db pool config",
		"max_conns", cfg.MaxConns,
		"min_conns", cfg.MinConns,
		"max_conn_lifetime", cfg.MaxConnLifetime.String(),
		"max_conn_idle_time", cfg.MaxConnIdleTime.String(),
		"health_check_period", cfg.HealthCheckPeriod.String(),
	)
}

func runDBStatsLogger(ctx context.Context, pool *pgxpool.Pool) {
	ticker := time.NewTicker(dbStatsInterval)
	defer ticker.Stop()

	var (
		lastEmpty      int64
		lastAcquire    int64
		lastAcquireDur time.Duration
		lastCanceled   int64
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		s := pool.Stat()
		emptyDelta := s.EmptyAcquireCount() - lastEmpty
		acquireDelta := s.AcquireCount() - lastAcquire
		acquireDurDelta := s.AcquireDuration() - lastAcquireDur
		canceledDelta := s.CanceledAcquireCount() - lastCanceled

		var avgAcquireMs int64
		if acquireDelta > 0 {
			avgAcquireMs = (acquireDurDelta).Milliseconds() / acquireDelta
		}

		fields := []any{
			"max_conns", s.MaxConns(),
			"total_conns", s.TotalConns(),
			"acquired_conns", s.AcquiredConns(),
			"idle_conns", s.IdleConns(),
			"constructing_conns", s.ConstructingConns(),
			"acquire_count_delta", acquireDelta,
			"empty_acquire_delta", emptyDelta,
			"canceled_acquire_delta", canceledDelta,
			"avg_acquire_ms", avgAcquireMs,
		}

		if emptyDelta > 0 || canceledDelta > 0 {
			slog.Warn("db pool pressure", fields...)
		} else {
			slog.Info("db pool stats", fields...)
		}

		lastEmpty = s.EmptyAcquireCount()
		lastAcquire = s.AcquireCount()
		lastAcquireDur = s.AcquireDuration()
		lastCanceled = s.CanceledAcquireCount()
	}
}

const samplerMaxConns int32 = 2

func newSamplerDBPool(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url for sampler: %w", err)
	}
	cfg.MaxConns = samplerMaxConns
	cfg.MinConns = 0

	cfg.MaxConnIdleTime = 5 * time.Minute
	return pgxpool.NewWithConfig(ctx, cfg)
}
