package main

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func applyPoolSizing(t *testing.T, dbURL string, envMax, envMin string) (max, min int32) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	urlParams := poolParamsFromURL(dbURL)

	maxFallback := defaultMaxConns
	if urlParams["pool_max_conns"] {
		maxFallback = cfg.MaxConns
	}
	if envMax != "" {
		t.Setenv("DATABASE_MAX_CONNS", envMax)
	}
	cfg.MaxConns = envInt32("DATABASE_MAX_CONNS", maxFallback)

	minFallback := defaultMinConns
	if urlParams["pool_min_conns"] {
		minFallback = cfg.MinConns
	}
	if envMin != "" {
		t.Setenv("DATABASE_MIN_CONNS", envMin)
	}
	cfg.MinConns = envInt32("DATABASE_MIN_CONNS", minFallback)

	if cfg.MinConns > cfg.MaxConns {
		cfg.MinConns = cfg.MaxConns
	}
	return cfg.MaxConns, cfg.MinConns
}

func TestPoolSizing_DefaultsWhenNothingSet(t *testing.T) {
	max, min := applyPoolSizing(t, "postgres://u:p@h/db?sslmode=disable", "", "")
	if max != defaultMaxConns || min != defaultMinConns {
		t.Fatalf("got max=%d min=%d, want %d/%d", max, min, defaultMaxConns, defaultMinConns)
	}
}

func TestPoolSizing_URLParamsHonoredWhenEnvUnset(t *testing.T) {
	url := "postgres://u:p@h/db?sslmode=disable&pool_max_conns=40&pool_min_conns=8"
	max, min := applyPoolSizing(t, url, "", "")
	if max != 40 || min != 8 {
		t.Fatalf("URL params should win when env unset; got max=%d min=%d", max, min)
	}
}

func TestPoolSizing_EnvOverridesURL(t *testing.T) {
	url := "postgres://u:p@h/db?sslmode=disable&pool_max_conns=40&pool_min_conns=8"
	max, min := applyPoolSizing(t, url, "100", "20")
	if max != 100 || min != 20 {
		t.Fatalf("env should win over URL; got max=%d min=%d", max, min)
	}
}

func TestPoolSizing_PartialURLParam(t *testing.T) {

	url := "postgres://u:p@h/db?sslmode=disable&pool_max_conns=40"
	max, min := applyPoolSizing(t, url, "", "")
	if max != 40 {
		t.Fatalf("URL pool_max_conns should be honored; got max=%d", max)
	}
	if min != defaultMinConns {
		t.Fatalf("min should default; got min=%d, want %d", min, defaultMinConns)
	}
}

func TestPoolSizing_InvalidEnvFallsBackToCodeDefault(t *testing.T) {

	max, min := applyPoolSizing(t, "postgres://u:p@h/db?sslmode=disable", "not-a-number", "")
	if max != defaultMaxConns {
		t.Fatalf("invalid env should fall back to code default; got max=%d, want %d", max, defaultMaxConns)
	}
	if min != defaultMinConns {
		t.Fatalf("got min=%d, want %d", min, defaultMinConns)
	}
}

func TestPoolSizing_InvalidEnvFallsBackToURLParam(t *testing.T) {

	url := "postgres://u:p@h/db?sslmode=disable&pool_max_conns=40"
	max, _ := applyPoolSizing(t, url, "not-a-number", "")
	if max != 40 {
		t.Fatalf("invalid env should fall back to URL param; got max=%d, want 40", max)
	}
}

func TestPoolSizing_MinClampedToMax(t *testing.T) {
	max, min := applyPoolSizing(t, "postgres://u:p@h/db?sslmode=disable", "10", "50")
	if min > max {
		t.Fatalf("min should be clamped to max; got max=%d min=%d", max, min)
	}
}
