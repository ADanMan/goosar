package handler

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMemoryWebhookRateLimiter_AllowsBelowLimit(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 3, Window: time.Minute})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if !l.Allow(ctx, "tok") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
}

func TestMemoryWebhookRateLimiter_RejectsAboveLimit(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 2, Window: time.Minute})
	ctx := context.Background()
	if !l.Allow(ctx, "tok") {
		t.Fatal("first should pass")
	}
	if !l.Allow(ctx, "tok") {
		t.Fatal("second should pass")
	}
	if l.Allow(ctx, "tok") {
		t.Fatal("third should be rejected")
	}

	if !l.Allow(ctx, "other") {
		t.Fatal("different key should pass")
	}
}

func TestMemoryWebhookRateLimiter_WindowExpiry(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 1, Window: 10 * time.Millisecond})
	ctx := context.Background()
	if !l.Allow(ctx, "tok") {
		t.Fatal("first should pass")
	}
	if l.Allow(ctx, "tok") {
		t.Fatal("second should fail (within window)")
	}
	time.Sleep(20 * time.Millisecond)
	if !l.Allow(ctx, "tok") {
		t.Fatal("third should pass after window")
	}
}

func TestMemoryWebhookRateLimiter_ZeroLimitDisabled(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 0, Window: time.Minute})
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		if !l.Allow(ctx, "tok") {
			t.Fatalf("Limit=0 should be unbounded, rejected at %d", i)
		}
	}
}

func TestMemoryWebhookRateLimiter_CheckDoesNotConsumeBudget(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 1, Window: time.Minute})
	ctx := context.Background()
	if !webhookLimiterCheck(ctx, l, "shared-ip") || !webhookLimiterCheck(ctx, l, "shared-ip") {
		t.Fatal("non-consuming checks should remain allowed before bad debt is charged")
	}
	if !l.Allow(ctx, "shared-ip") {
		t.Fatal("check unexpectedly consumed the only budget slot")
	}
	if webhookLimiterCheck(ctx, l, "shared-ip") {
		t.Fatal("check should reject after bad debt reaches the limit")
	}
	if retry := webhookLimiterRetryAfter(ctx, l, "shared-ip"); retry <= 0 || retry > time.Minute {
		t.Fatalf("unexpected retry interval: %v", retry)
	}
}

func TestWebhookLimiterLuaScript_StructureGuard(t *testing.T) {

	src := webhookLimiterAllowSource()
	mustBefore := func(a, b string) {
		t.Helper()
		ia, ib := strings.Index(src, a), strings.Index(src, b)
		if ia < 0 || ib < 0 {
			t.Fatalf("script must contain %q and %q, src=%s", a, b, src)
		}
		if ia >= ib {
			t.Fatalf("expected %q to appear before %q, src=%s", a, b, src)
		}
	}
	mustBefore("ZREMRANGEBYSCORE", "ZCARD")
	mustBefore("ZCARD", "ZADD")
	mustBefore("ZADD", "EXPIRE")
}

func TestRedisWebhookRateLimiter_RejectsAboveLimit(t *testing.T) {
	rdb := newRedisTestClient(t)
	defer rdb.Close()

	l := NewRedisWebhookRateLimiter(rdb, WebhookRateLimit{Limit: 3, Window: time.Minute})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if !l.Allow(ctx, "tok") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if l.Allow(ctx, "tok") {
		t.Fatal("fourth request should be rejected")
	}

	if !l.Allow(ctx, "other") {
		t.Fatal("different key should pass")
	}
}

func TestRedisWebhookIPRateLimiter_HasSeparateBudgetFromTokenLimiter(t *testing.T) {

	rdb := newRedisTestClient(t)
	defer rdb.Close()

	tok := NewRedisWebhookRateLimiter(rdb, WebhookRateLimit{Limit: 1, Window: time.Minute})
	ip := NewRedisWebhookIPRateLimiter(rdb, WebhookRateLimit{Limit: 2, Window: time.Minute})
	ctx := context.Background()
	if !tok.Allow(ctx, "alice") {
		t.Fatal("token limiter first request should pass")
	}
	if tok.Allow(ctx, "alice") {
		t.Fatal("token limiter second request should be rejected")
	}

	if !ip.Allow(ctx, "alice") {
		t.Fatal("IP limiter first request should pass")
	}
	if !ip.Allow(ctx, "alice") {
		t.Fatal("IP limiter second request should pass")
	}
	if ip.Allow(ctx, "alice") {
		t.Fatal("IP limiter third request should be rejected")
	}
}
