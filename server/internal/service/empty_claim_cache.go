package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	emptyClaimCachePrefix   = "mul:claim:runtime:empty:"
	emptyClaimVersionPrefix = "mul:claim:runtime:version:"
)

const EmptyClaimCacheTTL = 3 * time.Minute

const emptyClaimVersionTTL = 24 * time.Hour

const emptyClaimRedisTimeout = 250 * time.Millisecond

type EmptyClaimCache struct {
	rdb *redis.Client
}

func NewEmptyClaimCache(rdb *redis.Client) *EmptyClaimCache {
	if rdb == nil {
		return nil
	}
	return &EmptyClaimCache{rdb: rdb}
}

func emptyClaimKey(runtimeID string) string     { return emptyClaimCachePrefix + runtimeID }
func emptyClaimVersion(runtimeID string) string { return emptyClaimVersionPrefix + runtimeID }

func (c *EmptyClaimCache) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, emptyClaimRedisTimeout)
}

func (c *EmptyClaimCache) CurrentVersion(ctx context.Context, runtimeID string) int64 {
	if c == nil || runtimeID == "" {
		return 0
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	v, err := c.rdb.Get(bctx, emptyClaimVersion(runtimeID)).Int64()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("empty_claim_cache: version get failed; falling back to DB", "error", err)
		}
		return 0
	}

	c.rdb.Expire(bctx, emptyClaimVersion(runtimeID), emptyClaimVersionTTL)
	return v
}

func (c *EmptyClaimCache) IsEmpty(ctx context.Context, runtimeID string) bool {
	if c == nil || runtimeID == "" {
		return false
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()

	vals, err := c.rdb.MGet(bctx, emptyClaimKey(runtimeID), emptyClaimVersion(runtimeID)).Result()
	if err != nil {
		slog.Warn("empty_claim_cache: mget failed; falling back to DB", "error", err)
		return false
	}
	if len(vals) != 2 || vals[0] == nil {
		return false
	}
	emptyVer, ok := vals[0].(string)
	if !ok {
		return false
	}

	curVer := "0"
	if vals[1] != nil {
		if s, ok := vals[1].(string); ok {
			curVer = s
		}
	}
	return emptyVer == curVer
}

func (c *EmptyClaimCache) MarkEmpty(ctx context.Context, runtimeID string, observedVersion int64) {
	if c == nil || runtimeID == "" {
		return
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	if err := c.rdb.Set(bctx, emptyClaimKey(runtimeID), strconv.FormatInt(observedVersion, 10), EmptyClaimCacheTTL).Err(); err != nil {
		slog.Warn("empty_claim_cache: set failed", "error", err)
	}
}

func (c *EmptyClaimCache) Bump(ctx context.Context, runtimeID string) {
	if c == nil || runtimeID == "" {
		return
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	pipe := c.rdb.Pipeline()
	pipe.Incr(bctx, emptyClaimVersion(runtimeID))
	pipe.Expire(bctx, emptyClaimVersion(runtimeID), emptyClaimVersionTTL)
	if _, err := pipe.Exec(bctx); err != nil {
		slog.Warn("empty_claim_cache: bump failed; entry will expire on TTL", "error", err)
	}
}
