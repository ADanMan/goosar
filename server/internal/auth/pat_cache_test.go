package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisTestDB = 11

func newRedisTestClient(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse REDIS_TEST_URL: %v", err)
	}
	opts.DB = redisTestDB
	rdb := redis.NewClient(opts)
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("REDIS_TEST_URL unreachable: %v", err)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flushdb: %v", err)
	}
	t.Cleanup(func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	})
	return rdb
}

func TestPATCache_NilSafe(t *testing.T) {
	var c *PATCache
	ctx := context.Background()

	if v, ok := c.Get(ctx, "any-hash"); ok || v != "" {
		t.Fatalf("nil cache must miss; got (%q, %v)", v, ok)
	}
	c.Set(ctx, "any-hash", "user-1", AuthCacheTTL)
	c.Invalidate(ctx, "any-hash")
}

func TestNewPATCache_NilRedisReturnsNil(t *testing.T) {
	if c := NewPATCache(nil); c != nil {
		t.Fatalf("NewPATCache(nil) must return nil, got %#v", c)
	}
}

func TestPATCache_SetGetInvalidate(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewPATCache(rdb)
	if c == nil {
		t.Fatal("NewPATCache returned nil")
	}
	ctx := context.Background()

	if _, ok := c.Get(ctx, "missing"); ok {
		t.Fatal("expected miss before set")
	}

	c.Set(ctx, "hash-A", "user-A", AuthCacheTTL)
	if v, ok := c.Get(ctx, "hash-A"); !ok || v != "user-A" {
		t.Fatalf("expected hit user-A, got (%q, %v)", v, ok)
	}

	c.Invalidate(ctx, "hash-A")
	if v, ok := c.Get(ctx, "hash-A"); ok {
		t.Fatalf("expected miss after invalidate, got (%q, %v)", v, ok)
	}
}

func TestPATCache_TTL(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewPATCache(rdb)
	if c == nil {
		t.Fatal("NewPATCache returned nil")
	}
	ctx := context.Background()

	c.Set(ctx, "hash-T", "user-T", AuthCacheTTL)
	ttl, err := rdb.TTL(ctx, patCacheKey("hash-T")).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}

	if ttl <= 0 || ttl > AuthCacheTTL+time.Second {
		t.Fatalf("unexpected TTL %v (want ~%v)", ttl, AuthCacheTTL)
	}
}

func TestTTLForExpiry(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	if got := TTLForExpiry(now, time.Time{}); got != AuthCacheTTL {
		t.Fatalf("zero expires_at: got %v, want %v", got, AuthCacheTTL)
	}

	far := now.Add(24 * time.Hour)
	if got := TTLForExpiry(now, far); got != AuthCacheTTL {
		t.Fatalf("far-future expires_at: got %v, want %v", got, AuthCacheTTL)
	}

	soon := now.Add(10 * time.Second)
	if got := TTLForExpiry(now, soon); got != 10*time.Second {
		t.Fatalf("sooner expires_at: got %v, want 10s", got)
	}

	if got := TTLForExpiry(now, now); got != 0 {
		t.Fatalf("expires_at == now: got %v, want 0", got)
	}
	if got := TTLForExpiry(now, now.Add(-time.Second)); got != 0 {
		t.Fatalf("past expires_at: got %v, want 0", got)
	}
}

func TestPATCache_Set_RespectsClampedTTL(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewPATCache(rdb)
	if c == nil {
		t.Fatal("NewPATCache returned nil")
	}
	ctx := context.Background()

	c.Set(ctx, "hash-short", "user-short", 5*time.Second)
	ttl, err := rdb.TTL(ctx, patCacheKey("hash-short")).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > 5*time.Second+time.Second {
		t.Fatalf("expected clamped TTL ~5s, got %v", ttl)
	}

	c.Set(ctx, "hash-zero", "user-zero", 0)
	if _, ok := c.Get(ctx, "hash-zero"); ok {
		t.Fatal("zero-TTL Set must not cache")
	}
	c.Set(ctx, "hash-neg", "user-neg", -time.Second)
	if _, ok := c.Get(ctx, "hash-neg"); ok {
		t.Fatal("negative-TTL Set must not cache")
	}
}
