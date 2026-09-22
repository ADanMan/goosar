package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	modelListKeyPrefix          = "mul:" + runtimePendingRedisHashTag + ":model_list:req:"
	modelListPendingPrefix      = "mul:" + runtimePendingRedisHashTag + ":model_list:pending:"
	modelListRedisPopMaxRetries = 5
)

func modelListKey(id string) string               { return modelListKeyPrefix + id }
func modelListPendingKey(runtimeID string) string { return modelListPendingPrefix + runtimeID }

type RedisModelListStore struct {
	rdb *redis.Client
}

func NewRedisModelListStore(rdb *redis.Client) *RedisModelListStore {
	return &RedisModelListStore{rdb: rdb}
}

func (s *RedisModelListStore) Create(ctx context.Context, runtimeID string) (*ModelListRequest, error) {
	now := time.Now()
	req := &ModelListRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    ModelListPending,
		Supported: true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	data, err := s.marshalRequest(req)
	if err != nil {
		return nil, err
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, modelListKey(req.ID), data, modelListStoreRetention)
	pipe.ZAdd(ctx, modelListPendingKey(runtimeID), redis.Z{
		Score:  float64(now.UnixNano()),
		Member: req.ID,
	})

	pipe.Expire(ctx, modelListPendingKey(runtimeID), modelListStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("persist model list request: %w", err)
	}
	return req, nil
}

func (s *RedisModelListStore) Get(ctx context.Context, id string) (*ModelListRequest, error) {
	return s.loadRequest(ctx, id)
}

func (s *RedisModelListStore) loadRequest(ctx context.Context, id string) (*ModelListRequest, error) {
	raw, err := s.rdb.Get(ctx, modelListKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get model list request: %w", err)
	}
	req, err := s.unmarshalRequest(raw)
	if err != nil {
		return nil, err
	}
	if applyModelListTimeout(req, time.Now()) {
		if err := s.persistRequest(ctx, req); err != nil {
			return nil, err
		}

		s.rdb.ZRem(ctx, modelListPendingKey(req.RuntimeID), req.ID)
	}
	return req, nil
}

func (s *RedisModelListStore) persistRequest(ctx context.Context, req *ModelListRequest) error {
	data, err := s.marshalRequest(req)
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, modelListKey(req.ID), data, modelListStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist model list request: %w", err)
	}
	return nil
}

type redisModelListEnvelope struct {
	Public       *ModelListRequest `json:"r"`
	RunStartedAt *time.Time        `json:"s,omitempty"`
}

func (s *RedisModelListStore) marshalRequest(req *ModelListRequest) ([]byte, error) {
	env := redisModelListEnvelope{Public: req, RunStartedAt: req.RunStartedAt}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal model list request: %w", err)
	}
	return data, nil
}

func (s *RedisModelListStore) unmarshalRequest(raw []byte) (*ModelListRequest, error) {
	var env redisModelListEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode model list request: %w", err)
	}
	if env.Public == nil {
		return nil, fmt.Errorf("decode model list request: missing payload")
	}
	env.Public.RunStartedAt = env.RunStartedAt
	return env.Public, nil
}

func (s *RedisModelListStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCard(ctx, modelListPendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisModelListStore) PopPending(ctx context.Context, runtimeID string) (*ModelListRequest, error) {
	pendingKey := modelListPendingKey(runtimeID)

	for attempt := 0; attempt < modelListRedisPopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]

		req, err := s.loadRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil {

			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		if req.Status != ModelListPending {

			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}

		now := time.Now()
		req.Status = ModelListRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		data, err := s.marshalRequest(req)
		if err != nil {
			return nil, err
		}

		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, modelListKey(id)},
			id, data, int(modelListStoreRetention.Seconds()),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending: %w", err)
		}
		if result == 0 {

			continue
		}
		return req, nil
	}
	return nil, nil
}

func (s *RedisModelListStore) Complete(ctx context.Context, id string, models []ModelEntry, supported bool) error {
	req, err := s.loadRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = ModelListCompleted
	req.Models = models
	req.Supported = supported
	req.UpdatedAt = time.Now()
	return s.persistRequest(ctx, req)
}

func (s *RedisModelListStore) Fail(ctx context.Context, id string, errMsg string) error {
	req, err := s.loadRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = ModelListFailed
	req.Error = errMsg
	req.UpdatedAt = time.Now()
	return s.persistRequest(ctx, req)
}
