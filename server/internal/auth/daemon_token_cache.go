package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const daemonTokenCachePrefix = "mul:auth:daemon:"

type DaemonTokenIdentity struct {
	WorkspaceID string `json:"w"`
	DaemonID    string `json:"d"`
}

type DaemonTokenCache struct {
	rdb *redis.Client
}

func NewDaemonTokenCache(rdb *redis.Client) *DaemonTokenCache {
	if rdb == nil {
		return nil
	}
	return &DaemonTokenCache{rdb: rdb}
}

func daemonTokenCacheKey(hash string) string { return daemonTokenCachePrefix + hash }

func (c *DaemonTokenCache) Get(ctx context.Context, hash string) (DaemonTokenIdentity, bool) {
	var id DaemonTokenIdentity
	if c == nil {
		return id, false
	}
	raw, err := c.rdb.Get(ctx, daemonTokenCacheKey(hash)).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("daemon_token_cache: get failed; falling back to DB", "error", err)
		}
		return id, false
	}
	if err := json.Unmarshal(raw, &id); err != nil {
		slog.Warn("daemon_token_cache: malformed entry; falling back to DB", "error", err)
		return DaemonTokenIdentity{}, false
	}
	return id, true
}

func (c *DaemonTokenCache) Set(ctx context.Context, hash string, id DaemonTokenIdentity, ttl time.Duration) {
	if c == nil || ttl <= 0 {
		return
	}
	raw, err := json.Marshal(id)
	if err != nil {
		slog.Warn("daemon_token_cache: marshal failed", "error", err)
		return
	}
	if err := c.rdb.Set(ctx, daemonTokenCacheKey(hash), raw, ttl).Err(); err != nil {
		slog.Warn("daemon_token_cache: set failed", "error", err)
	}
}

func (c *DaemonTokenCache) Invalidate(ctx context.Context, hash string) {
	if c == nil {
		return
	}
	if err := c.rdb.Del(ctx, daemonTokenCacheKey(hash)).Err(); err != nil {
		slog.Warn("daemon_token_cache: invalidate failed; entry will expire on TTL", "error", err)
	}
}
