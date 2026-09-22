package handler

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type WebhookRateLimit struct {
	Limit  int
	Window time.Duration
}

func DefaultWebhookRateLimit() WebhookRateLimit {
	return WebhookRateLimit{Limit: 60, Window: time.Minute}
}

func DefaultWebhookIPRateLimit() WebhookRateLimit {
	return WebhookRateLimit{Limit: 30, Window: time.Minute}
}

func DefaultWebhookAbsoluteIPRateLimit() WebhookRateLimit {
	return WebhookRateLimit{Limit: 600, Window: time.Minute}
}

type WebhookRateLimiter interface {
	Allow(ctx context.Context, key string) bool
}

type webhookRateLimiterInspector interface {
	Check(ctx context.Context, key string) bool
	RetryAfter(ctx context.Context, key string) time.Duration
}

func webhookLimiterCheck(ctx context.Context, limiter WebhookRateLimiter, key string) bool {
	if inspector, ok := limiter.(webhookRateLimiterInspector); ok {
		return inspector.Check(ctx, key)
	}
	return true
}

func webhookLimiterRetryAfter(ctx context.Context, limiter WebhookRateLimiter, key string) time.Duration {
	if inspector, ok := limiter.(webhookRateLimiterInspector); ok {
		return inspector.RetryAfter(ctx, key)
	}
	return time.Minute
}

type memoryWebhookRateLimiter struct {
	cfg WebhookRateLimit
	mu  sync.Mutex
	hit map[string][]time.Time
}

func NewMemoryWebhookRateLimiter(cfg WebhookRateLimit) WebhookRateLimiter {
	return &memoryWebhookRateLimiter{cfg: cfg, hit: make(map[string][]time.Time)}
}

func (l *memoryWebhookRateLimiter) Allow(_ context.Context, key string) bool {
	return l.evaluate(key, true)
}

func (l *memoryWebhookRateLimiter) Check(_ context.Context, key string) bool {
	return l.evaluate(key, false)
}

func (l *memoryWebhookRateLimiter) evaluate(key string, consume bool) bool {
	if l.cfg.Limit <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-l.cfg.Window)

	l.mu.Lock()
	defer l.mu.Unlock()

	hits := l.hit[key]

	keep := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.cfg.Limit {
		l.hit[key] = keep
		return false
	}
	if consume {
		keep = append(keep, now)
	}
	l.hit[key] = keep
	return true
}

func (l *memoryWebhookRateLimiter) RetryAfter(_ context.Context, key string) time.Duration {
	if l.cfg.Limit <= 0 {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	hits := l.hit[key]
	if len(hits) == 0 {
		return l.cfg.Window
	}
	retry := time.Until(hits[0].Add(l.cfg.Window))
	if retry <= 0 {
		return time.Second
	}
	return retry
}

const (
	webhookLimiterKeyPrefix           = "mul:webhook:rate:"
	webhookIPLimiterKeyPrefix         = "mul:webhook:ip:"
	webhookAbsoluteIPLimiterKeyPrefix = "mul:webhook:absolute-ip:"
)

const webhookLimiterAllowSrc = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local cutoff = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local member = ARGV[5]
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
local count = redis.call('ZCARD', key)
if count >= limit then
    return 0
end
redis.call('ZADD', key, now, member)
redis.call('EXPIRE', key, ttl)
return 1
`

var webhookLimiterAllowScript = redis.NewScript(webhookLimiterAllowSrc)

const webhookLimiterCheckSrc = `
local key = KEYS[1]
local cutoff = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
local count = redis.call('ZCARD', key)
if count >= limit then
    return 0
end
return 1
`

var webhookLimiterCheckScript = redis.NewScript(webhookLimiterCheckSrc)

func webhookLimiterAllowSource() string { return webhookLimiterAllowSrc }

type redisWebhookRateLimiter struct {
	cfg       WebhookRateLimit
	rdb       *redis.Client
	keyPrefix string
}

func NewRedisWebhookRateLimiter(rdb *redis.Client, cfg WebhookRateLimit) WebhookRateLimiter {
	return &redisWebhookRateLimiter{cfg: cfg, rdb: rdb, keyPrefix: webhookLimiterKeyPrefix}
}

func NewRedisWebhookIPRateLimiter(rdb *redis.Client, cfg WebhookRateLimit) WebhookRateLimiter {
	return &redisWebhookRateLimiter{cfg: cfg, rdb: rdb, keyPrefix: webhookIPLimiterKeyPrefix}
}

func NewRedisWebhookAbsoluteIPRateLimiter(rdb *redis.Client, cfg WebhookRateLimit) WebhookRateLimiter {
	return &redisWebhookRateLimiter{cfg: cfg, rdb: rdb, keyPrefix: webhookAbsoluteIPLimiterKeyPrefix}
}

func NewMemoryWebhookIPRateLimiter(cfg WebhookRateLimit) WebhookRateLimiter {
	return NewMemoryWebhookRateLimiter(cfg)
}

func NewMemoryWebhookAbsoluteIPRateLimiter(cfg WebhookRateLimit) WebhookRateLimiter {
	return NewMemoryWebhookRateLimiter(cfg)
}

func (l *redisWebhookRateLimiter) Allow(ctx context.Context, key string) bool {
	if l.cfg.Limit <= 0 || l.rdb == nil {
		return true
	}
	now := time.Now().UnixNano()
	cutoff := time.Now().Add(-l.cfg.Window).UnixNano()
	ttlSeconds := int64(l.cfg.Window/time.Second) * 2
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}
	prefix := l.keyPrefix
	if prefix == "" {
		prefix = webhookLimiterKeyPrefix
	}

	member := uuid.NewString()
	res, err := webhookLimiterAllowScript.Run(
		ctx,
		l.rdb,
		[]string{prefix + key},
		now, cutoff, l.cfg.Limit, ttlSeconds, member,
	).Int()
	if err != nil {

		return true
	}
	return res == 1
}

func (l *redisWebhookRateLimiter) Check(ctx context.Context, key string) bool {
	if l.cfg.Limit <= 0 || l.rdb == nil {
		return true
	}
	cutoff := time.Now().Add(-l.cfg.Window).UnixNano()
	prefix := l.keyPrefix
	if prefix == "" {
		prefix = webhookLimiterKeyPrefix
	}
	res, err := webhookLimiterCheckScript.Run(
		ctx,
		l.rdb,
		[]string{prefix + key},
		cutoff, l.cfg.Limit,
	).Int()
	if err != nil {
		return true
	}
	return res == 1
}

func (l *redisWebhookRateLimiter) RetryAfter(_ context.Context, _ string) time.Duration {
	if l.cfg.Window <= 0 {
		return time.Second
	}

	return l.cfg.Window
}
