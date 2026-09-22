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
	runtimePendingRedisHashTag = "{runtime_pending}"

	localSkillListKeyPrefix       = "mul:" + runtimePendingRedisHashTag + ":local_skill:list:"
	localSkillListPendingPrefix   = "mul:" + runtimePendingRedisHashTag + ":local_skill:list:pending:"
	localSkillImportKeyPrefix     = "mul:" + runtimePendingRedisHashTag + ":local_skill:import:"
	localSkillImportPendingPrefix = "mul:" + runtimePendingRedisHashTag + ":local_skill:import:pending:"
	localSkillRedisPopMaxRetries  = 5
)

var claimPendingScript = redis.NewScript(`
local removed = redis.call('ZREM', KEYS[1], ARGV[1])
if removed == 0 then
    return 0
end
redis.call('SET', KEYS[2], ARGV[2], 'EX', tonumber(ARGV[3]))
return 1
`)

func localSkillListKey(id string) string { return localSkillListKeyPrefix + id }
func localSkillListPendingKey(runtimeID string) string {
	return localSkillListPendingPrefix + runtimeID
}
func localSkillImportKey(id string) string { return localSkillImportKeyPrefix + id }
func localSkillImportPendingKey(runtimeID string) string {
	return localSkillImportPendingPrefix + runtimeID
}

type RedisLocalSkillListStore struct {
	rdb *redis.Client
}

func NewRedisLocalSkillListStore(rdb *redis.Client) *RedisLocalSkillListStore {
	return &RedisLocalSkillListStore{rdb: rdb}
}

func (s *RedisLocalSkillListStore) Create(ctx context.Context, runtimeID string) (*RuntimeLocalSkillListRequest, error) {
	now := time.Now()
	req := &RuntimeLocalSkillListRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    RuntimeLocalSkillPending,
		Supported: true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal list request: %w", err)
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, localSkillListKey(req.ID), data, runtimeLocalSkillStoreRetention)
	pipe.ZAdd(ctx, localSkillListPendingKey(runtimeID), redis.Z{
		Score:  float64(now.UnixNano()),
		Member: req.ID,
	})

	pipe.Expire(ctx, localSkillListPendingKey(runtimeID), runtimeLocalSkillStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("persist list request: %w", err)
	}
	return req, nil
}

func (s *RedisLocalSkillListStore) Get(ctx context.Context, id string) (*RuntimeLocalSkillListRequest, error) {
	return s.loadListRequest(ctx, id)
}

func (s *RedisLocalSkillListStore) loadListRequest(ctx context.Context, id string) (*RuntimeLocalSkillListRequest, error) {
	raw, err := s.rdb.Get(ctx, localSkillListKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get list request: %w", err)
	}
	var req RuntimeLocalSkillListRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode list request: %w", err)
	}
	if applyLocalSkillListTimeout(&req, time.Now()) {

		if err := s.persistListRequest(ctx, &req); err != nil {
			return nil, err
		}
		s.rdb.ZRem(ctx, localSkillListPendingKey(req.RuntimeID), req.ID)
	}
	return &req, nil
}

func (s *RedisLocalSkillListStore) persistListRequest(ctx context.Context, req *RuntimeLocalSkillListRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal list request: %w", err)
	}
	if err := s.rdb.Set(ctx, localSkillListKey(req.ID), data, runtimeLocalSkillStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist list request: %w", err)
	}
	return nil
}

func (s *RedisLocalSkillListStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCard(ctx, localSkillListPendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisLocalSkillListStore) PopPending(ctx context.Context, runtimeID string) (*RuntimeLocalSkillListRequest, error) {
	pendingKey := localSkillListPendingKey(runtimeID)

	for attempt := 0; attempt < localSkillRedisPopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]

		req, err := s.loadListRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil {

			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		if req.Status != RuntimeLocalSkillPending {

			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}

		now := time.Now()
		req.Status = RuntimeLocalSkillRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		data, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal list request: %w", err)
		}

		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, localSkillListKey(id)},
			id, data, int(runtimeLocalSkillStoreRetention.Seconds()),
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

func (s *RedisLocalSkillListStore) Complete(ctx context.Context, id string, skills []RuntimeLocalSkillSummary, supported bool, mcpServers []RuntimeLocalMcpServerSummary, mcpSupported bool) error {
	req, err := s.loadListRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = RuntimeLocalSkillCompleted
	req.Skills = skills
	req.Supported = supported
	req.McpServers = mcpServers
	req.McpSupported = mcpSupported
	req.UpdatedAt = time.Now()
	return s.persistListRequest(ctx, req)
}

func (s *RedisLocalSkillListStore) Fail(ctx context.Context, id string, errMsg string) error {
	req, err := s.loadListRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = RuntimeLocalSkillFailed
	req.Error = errMsg
	req.UpdatedAt = time.Now()
	return s.persistListRequest(ctx, req)
}

type RedisLocalSkillImportStore struct {
	rdb *redis.Client
}

func NewRedisLocalSkillImportStore(rdb *redis.Client) *RedisLocalSkillImportStore {
	return &RedisLocalSkillImportStore{rdb: rdb}
}

func (s *RedisLocalSkillImportStore) Create(ctx context.Context, input LocalSkillImportRequestInput) (*RuntimeLocalSkillImportRequest, error) {
	now := time.Now()
	req := &RuntimeLocalSkillImportRequest{
		ID:               randomID(),
		RuntimeID:        input.RuntimeID,
		SkillKey:         input.SkillKey,
		Name:             input.Name,
		Description:      input.Description,
		Action:           input.Action,
		TargetSkillID:    input.TargetSkillID,
		SupportsConflict: input.SupportsConflict,
		Status:           RuntimeLocalSkillPending,
		CreatedAt:        now,
		UpdatedAt:        now,
		CreatorID:        input.CreatorID,
	}
	data, err := s.marshalImport(req)
	if err != nil {
		return nil, err
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, localSkillImportKey(req.ID), data, runtimeLocalSkillStoreRetention)
	pipe.ZAdd(ctx, localSkillImportPendingKey(input.RuntimeID), redis.Z{
		Score:  float64(now.UnixNano()),
		Member: req.ID,
	})
	pipe.Expire(ctx, localSkillImportPendingKey(input.RuntimeID), runtimeLocalSkillStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("persist import request: %w", err)
	}
	return req, nil
}

func (s *RedisLocalSkillImportStore) Get(ctx context.Context, id string) (*RuntimeLocalSkillImportRequest, error) {
	return s.loadImportRequest(ctx, id)
}

func (s *RedisLocalSkillImportStore) loadImportRequest(ctx context.Context, id string) (*RuntimeLocalSkillImportRequest, error) {
	raw, err := s.rdb.Get(ctx, localSkillImportKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get import request: %w", err)
	}
	req, err := s.unmarshalImport(raw)
	if err != nil {
		return nil, err
	}
	if applyLocalSkillImportTimeout(req, time.Now()) {
		if err := s.persistImportRequest(ctx, req); err != nil {
			return nil, err
		}
		s.rdb.ZRem(ctx, localSkillImportPendingKey(req.RuntimeID), req.ID)
	}
	return req, nil
}

func (s *RedisLocalSkillImportStore) persistImportRequest(ctx context.Context, req *RuntimeLocalSkillImportRequest) error {
	data, err := s.marshalImport(req)
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, localSkillImportKey(req.ID), data, runtimeLocalSkillStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist import request: %w", err)
	}
	return nil
}

type redisImportEnvelope struct {
	Public       *RuntimeLocalSkillImportRequest `json:"r"`
	CreatorID    string                          `json:"c"`
	RunStartedAt *time.Time                      `json:"s"`
}

func (s *RedisLocalSkillImportStore) marshalImport(req *RuntimeLocalSkillImportRequest) ([]byte, error) {
	env := redisImportEnvelope{
		Public:       req,
		CreatorID:    req.CreatorID,
		RunStartedAt: req.RunStartedAt,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal import request: %w", err)
	}
	return data, nil
}

func (s *RedisLocalSkillImportStore) unmarshalImport(raw []byte) (*RuntimeLocalSkillImportRequest, error) {
	var env redisImportEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode import request: %w", err)
	}
	if env.Public == nil {
		return nil, fmt.Errorf("decode import request: missing payload")
	}
	env.Public.CreatorID = env.CreatorID
	env.Public.RunStartedAt = env.RunStartedAt
	return env.Public, nil
}

func (s *RedisLocalSkillImportStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCard(ctx, localSkillImportPendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisLocalSkillImportStore) PopPending(ctx context.Context, runtimeID string) (*RuntimeLocalSkillImportRequest, error) {
	pendingKey := localSkillImportPendingKey(runtimeID)

	for attempt := 0; attempt < localSkillRedisPopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]

		req, err := s.loadImportRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil {
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		if req.Status != RuntimeLocalSkillPending {
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}

		now := time.Now()
		req.Status = RuntimeLocalSkillRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		data, err := s.marshalImport(req)
		if err != nil {
			return nil, err
		}

		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, localSkillImportKey(id)},
			id, data, int(runtimeLocalSkillStoreRetention.Seconds()),
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

func (s *RedisLocalSkillImportStore) PopPendingBatch(ctx context.Context, runtimeID string, limit int) ([]*RuntimeLocalSkillImportRequest, error) {
	pendingKey := localSkillImportPendingKey(runtimeID)

	ids, err := s.rdb.ZRange(ctx, pendingKey, 0, int64(limit)-1).Result()
	if err != nil {
		return nil, fmt.Errorf("zrange pending batch: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	var result []*RuntimeLocalSkillImportRequest
	for _, id := range ids {
		req, err := s.loadImportRequest(ctx, id)
		if err != nil {
			return result, err
		}
		if req == nil {
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		if req.Status != RuntimeLocalSkillPending {
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}

		now := time.Now()
		req.Status = RuntimeLocalSkillRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		data, err := s.marshalImport(req)
		if err != nil {
			return result, err
		}

		claimed, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, localSkillImportKey(id)},
			id, data, int(runtimeLocalSkillStoreRetention.Seconds()),
		).Int64()
		if err != nil {
			return result, fmt.Errorf("claim pending batch: %w", err)
		}
		if claimed == 1 {
			result = append(result, req)
		}
	}
	return result, nil
}

func (s *RedisLocalSkillImportStore) Complete(ctx context.Context, id string, skill SkillWithFilesResponse) error {
	req, err := s.loadImportRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = RuntimeLocalSkillCompleted
	req.Skill = &skill
	req.UpdatedAt = time.Now()
	return s.persistImportRequest(ctx, req)
}

func (s *RedisLocalSkillImportStore) Conflict(ctx context.Context, id string, info LocalSkillImportConflict) error {
	req, err := s.loadImportRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = RuntimeLocalSkillConflict
	conflict := info
	req.Conflict = &conflict
	req.UpdatedAt = time.Now()
	return s.persistImportRequest(ctx, req)
}

func (s *RedisLocalSkillImportStore) Fail(ctx context.Context, id string, errMsg string) error {
	req, err := s.loadImportRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = RuntimeLocalSkillFailed
	req.Error = errMsg
	req.UpdatedAt = time.Now()
	return s.persistImportRequest(ctx, req)
}
