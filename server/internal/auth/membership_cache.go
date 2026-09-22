package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const MembershipCacheTTL = 5 * time.Minute

const membershipCachePrefix = "mul:auth:member:"

type MembershipCache struct {
	rdb *redis.Client
}

func NewMembershipCache(rdb *redis.Client) *MembershipCache {
	if rdb == nil {
		return nil
	}
	return &MembershipCache{rdb: rdb}
}

func membershipKey(userID, workspaceID string) string {
	return fmt.Sprintf("%s%s:%s", membershipCachePrefix, userID, workspaceID)
}

func (c *MembershipCache) Get(ctx context.Context, userID, workspaceID string) bool {
	if c == nil {
		return false
	}
	err := c.rdb.Get(ctx, membershipKey(userID, workspaceID)).Err()
	switch {
	case err == nil:
		return true
	case errors.Is(err, redis.Nil):
		return false
	default:
		slog.Warn("membership_cache: get failed; falling back to DB", "error", err)
		return false
	}
}

func (c *MembershipCache) Set(ctx context.Context, userID, workspaceID string) {
	if c == nil {
		return
	}
	if err := c.rdb.Set(ctx, membershipKey(userID, workspaceID), "1", MembershipCacheTTL).Err(); err != nil {
		slog.Warn("membership_cache: set failed", "error", err)
	}
}

func (c *MembershipCache) Invalidate(ctx context.Context, userID, workspaceID string) {
	if c == nil {
		return
	}
	if err := c.rdb.Del(ctx, membershipKey(userID, workspaceID)).Err(); err != nil {
		slog.Warn("membership_cache: invalidate failed", "error", err)
	}
}
