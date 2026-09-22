package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const AuthCacheTTL = 10 * time.Minute

const patCachePrefix = "mul:auth:pat:"

func patCacheKey(hash string) string {
	return patCachePrefix + hash
}

type PATCache struct {
	client *redis.Client
}

func NewPATCache(rdb *redis.Client) *PATCache {
	if rdb == nil {
		return nil
	}
	return &PATCache{client: rdb}
}

func (c *PATCache) Get(ctx context.Context, hash string) (string, bool) {
	if c == nil {
		return "", false
	}

	userID, err := c.client.Get(ctx, patCacheKey(hash)).Result()
	switch {
	case err == nil:
		return userID, true
	case errors.Is(err, redis.Nil):
		return "", false
	default:
		slog.Warn("pat_cache: get failed; falling back to DB", "error", err)
		return "", false
	}
}

func (c *PATCache) Set(ctx context.Context, hash, userID string, ttl time.Duration) {
	if c == nil || ttl <= 0 {
		return
	}
	if err := c.client.Set(ctx, patCacheKey(hash), userID, ttl).Err(); err != nil {
		slog.Warn("pat_cache: set failed", "error", err)
	}
}

func (c *PATCache) Invalidate(ctx context.Context, hash string) {
	if c == nil {
		return
	}
	if err := c.client.Del(ctx, patCacheKey(hash)).Err(); err != nil {
		slog.Warn("pat_cache: invalidate failed; entry will expire on TTL", "error", err)
	}
}

func TTLForExpiry(now, expiresAt time.Time) time.Duration {
	if expiresAt.IsZero() {
		return AuthCacheTTL
	}

	if remaining := expiresAt.Sub(now); remaining < AuthCacheTTL {
		if remaining < 0 {
			return 0
		}
		return remaining
	}
	return AuthCacheTTL
}
