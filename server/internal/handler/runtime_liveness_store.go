package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type LivenessStore interface {
	Available() bool

	Touch(ctx context.Context, runtimeID string, ttl time.Duration) error

	IsAliveBatch(ctx context.Context, runtimeIDs []string) (alive map[string]bool, ok bool)

	Forget(ctx context.Context, runtimeID string)
}

type noopLivenessStore struct{}

func NewNoopLivenessStore() LivenessStore { return noopLivenessStore{} }

func (noopLivenessStore) Available() bool { return false }

func (noopLivenessStore) Touch(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (noopLivenessStore) IsAliveBatch(_ context.Context, _ []string) (map[string]bool, bool) {
	return nil, false
}

func (noopLivenessStore) Forget(_ context.Context, _ string) {}

const runtimeLivenessKeyPrefix = "mul:runtime:hb:"

func runtimeLivenessKey(runtimeID string) string {
	return runtimeLivenessKeyPrefix + runtimeID
}

type RedisLivenessStore struct {
	rdb *redis.Client
}

func NewRedisLivenessStore(rdb *redis.Client) *RedisLivenessStore {
	return &RedisLivenessStore{rdb: rdb}
}

func (s *RedisLivenessStore) Available() bool { return s != nil && s.rdb != nil }

func (s *RedisLivenessStore) Touch(ctx context.Context, runtimeID string, ttl time.Duration) error {
	if !s.Available() {
		return errors.New("redis liveness store: unavailable")
	}
	if runtimeID == "" {
		return errors.New("redis liveness store: empty runtime id")
	}
	if err := s.rdb.Set(ctx, runtimeLivenessKey(runtimeID), "1", ttl).Err(); err != nil {
		return fmt.Errorf("liveness touch: %w", err)
	}
	return nil
}

func (s *RedisLivenessStore) IsAliveBatch(ctx context.Context, runtimeIDs []string) (map[string]bool, bool) {
	if !s.Available() || len(runtimeIDs) == 0 {
		return map[string]bool{}, s.Available()
	}
	keys := make([]string, len(runtimeIDs))
	for i, id := range runtimeIDs {
		keys[i] = runtimeLivenessKey(id)
	}
	values, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		slog.Warn("liveness mget failed; falling back to DB",
			"error", err, "count", len(keys))
		return nil, false
	}
	out := make(map[string]bool, len(runtimeIDs))
	for i, id := range runtimeIDs {
		out[id] = values[i] != nil
	}
	return out, true
}

func (s *RedisLivenessStore) Forget(ctx context.Context, runtimeID string) {
	if !s.Available() || runtimeID == "" {
		return
	}
	if err := s.rdb.Del(ctx, runtimeLivenessKey(runtimeID)).Err(); err != nil {
		slog.Warn("liveness forget failed", "error", err, "runtime_id", runtimeID)
	}
}
