package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RateLimitStore interface {
	Incr(ctx context.Context, key string, window time.Duration) (count int64, retryAfter time.Duration, err error)

	Backend() string
}

func NewRateLimitStore(rdb *redis.Client) RateLimitStore {
	if rdb == nil {
		return NewMemoryRateLimitStore()
	}
	return &redisRateLimitStore{rdb: rdb}
}

var rateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
    redis.call('EXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('TTL', KEYS[1])
if ttl < 0 then
    ttl = tonumber(ARGV[1])
end
return {count, ttl}
`)

type redisRateLimitStore struct{ rdb *redis.Client }

func (s *redisRateLimitStore) Backend() string { return "redis" }

func (s *redisRateLimitStore) Incr(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	res, err := rateLimitScript.Run(ctx, s.rdb, []string{key}, int(window.Seconds())).Slice()
	if err != nil {
		return 0, 0, err
	}
	count, _ := res[0].(int64)
	ttl, _ := res[1].(int64)
	return count, time.Duration(ttl) * time.Second, nil
}

type memoryWindow struct {
	count   int64
	expires time.Time
}

type memoryRateLimitStore struct {
	mu        sync.Mutex
	windows   map[string]*memoryWindow
	lastSweep time.Time
}

const memorySweepInterval = time.Minute

func NewMemoryRateLimitStore() RateLimitStore {
	return &memoryRateLimitStore{windows: make(map[string]*memoryWindow)}
}

func (s *memoryRateLimitStore) Backend() string { return "memory" }

func (s *memoryRateLimitStore) Incr(_ context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if now.Sub(s.lastSweep) >= memorySweepInterval {
		for k, w := range s.windows {
			if now.After(w.expires) {
				delete(s.windows, k)
			}
		}
		s.lastSweep = now
	}

	w, ok := s.windows[key]
	if !ok || now.After(w.expires) {
		w = &memoryWindow{expires: now.Add(window)}
		s.windows[key] = w
	}
	w.count++
	return w.count, time.Until(w.expires), nil
}
