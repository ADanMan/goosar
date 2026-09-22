package realtime

import (
	"sort"
	"sync"
	"sync/atomic"
)

type Metrics struct {
	ConnectsTotal        atomic.Int64
	DisconnectsTotal     atomic.Int64
	ActiveConnections    atomic.Int64
	SlowEvictionsTotal   atomic.Int64
	MessagesSentTotal    atomic.Int64
	MessagesDroppedTotal atomic.Int64

	InboundTooLargeTotal atomic.Int64

	eventSent sync.Map

	subscribeTotal       sync.Map
	unsubscribeTotal     sync.Map
	subscribeDeniedTotal sync.Map
	scopeRooms           sync.Map

	RedisXAddTotal             atomic.Int64
	RedisXAddErrors            atomic.Int64
	RedisXReadTotal            atomic.Int64
	RedisXReadErrors           atomic.Int64
	RedisAckTotal              atomic.Int64
	RedisLastXAddLagMicros     atomic.Int64
	RedisMirrorPrimaryErrors   atomic.Int64
	RedisMirrorSecondaryErrors atomic.Int64
	RedisMirrorDivergenceTotal atomic.Int64

	RedisConnected atomic.Bool

	redisLastErrMu sync.RWMutex
	redisLastErr   string

	NodeID atomic.Value
}

var M = &Metrics{}

func loadOrInitCounter(m *sync.Map, key string) *atomic.Int64 {
	if v, ok := m.Load(key); ok {
		return v.(*atomic.Int64)
	}
	c := new(atomic.Int64)
	if existing, loaded := m.LoadOrStore(key, c); loaded {
		return existing.(*atomic.Int64)
	}
	return c
}

func (m *Metrics) RecordEvent(eventType string) {
	if eventType == "" {
		return
	}
	loadOrInitCounter(&m.eventSent, eventType).Add(1)
}

func (m *Metrics) SubscribesTotal(scopeType string) *atomic.Int64 {
	return loadOrInitCounter(&m.subscribeTotal, scopeType)
}

func (m *Metrics) UnsubscribesTotal(scopeType string) *atomic.Int64 {
	return loadOrInitCounter(&m.unsubscribeTotal, scopeType)
}

func (m *Metrics) SubscribeDeniedTotal(scopeType string) *atomic.Int64 {
	return loadOrInitCounter(&m.subscribeDeniedTotal, scopeType)
}

func (m *Metrics) IncRoom(scopeType string) { loadOrInitCounter(&m.scopeRooms, scopeType).Add(1) }
func (m *Metrics) DecRoom(scopeType string) { loadOrInitCounter(&m.scopeRooms, scopeType).Add(-1) }

func (m *Metrics) SetRedisLastError(msg string) {
	m.redisLastErrMu.Lock()
	m.redisLastErr = msg
	m.redisLastErrMu.Unlock()
}

func (m *Metrics) lastRedisErr() string {
	m.redisLastErrMu.RLock()
	defer m.redisLastErrMu.RUnlock()
	return m.redisLastErr
}

func snapshotCounters(s *sync.Map) map[string]int64 {
	out := map[string]int64{}
	s.Range(func(k, v any) bool {
		out[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]int64, len(out))
	for _, k := range keys {
		ordered[k] = out[k]
	}
	return ordered
}

func (m *Metrics) Snapshot() map[string]any {
	nodeID := ""
	if v := m.NodeID.Load(); v != nil {
		nodeID, _ = v.(string)
	}
	return map[string]any{
		"connects_total":          m.ConnectsTotal.Load(),
		"disconnects_total":       m.DisconnectsTotal.Load(),
		"active_connections":      m.ActiveConnections.Load(),
		"slow_evictions_total":    m.SlowEvictionsTotal.Load(),
		"messages_sent_total":     m.MessagesSentTotal.Load(),
		"messages_dropped_total":  m.MessagesDroppedTotal.Load(),
		"inbound_too_large_total": m.InboundTooLargeTotal.Load(),
		"events_sent_by_type":     snapshotCounters(&m.eventSent),
		"subscribes_total":        snapshotCounters(&m.subscribeTotal),
		"unsubscribes_total":      snapshotCounters(&m.unsubscribeTotal),
		"subscribe_denied_total":  snapshotCounters(&m.subscribeDeniedTotal),
		"active_scope_rooms":      snapshotCounters(&m.scopeRooms),
		"redis": map[string]any{
			"connected":               m.RedisConnected.Load(),
			"node_id":                 nodeID,
			"xadd_total":              m.RedisXAddTotal.Load(),
			"xadd_errors":             m.RedisXAddErrors.Load(),
			"xread_total":             m.RedisXReadTotal.Load(),
			"xread_errors":            m.RedisXReadErrors.Load(),
			"ack_total":               m.RedisAckTotal.Load(),
			"last_xadd_lag_micros":    m.RedisLastXAddLagMicros.Load(),
			"mirror_primary_errors":   m.RedisMirrorPrimaryErrors.Load(),
			"mirror_secondary_errors": m.RedisMirrorSecondaryErrors.Load(),
			"mirror_divergence_total": m.RedisMirrorDivergenceTotal.Load(),
			"last_error":              m.lastRedisErr(),
		},
	}
}

func (m *Metrics) Reset() {
	m.ConnectsTotal.Store(0)
	m.DisconnectsTotal.Store(0)
	m.ActiveConnections.Store(0)
	m.SlowEvictionsTotal.Store(0)
	m.MessagesSentTotal.Store(0)
	m.MessagesDroppedTotal.Store(0)
	m.InboundTooLargeTotal.Store(0)
	m.eventSent.Range(func(k, _ any) bool { m.eventSent.Delete(k); return true })
	m.subscribeTotal.Range(func(k, _ any) bool { m.subscribeTotal.Delete(k); return true })
	m.unsubscribeTotal.Range(func(k, _ any) bool { m.unsubscribeTotal.Delete(k); return true })
	m.subscribeDeniedTotal.Range(func(k, _ any) bool { m.subscribeDeniedTotal.Delete(k); return true })
	m.scopeRooms.Range(func(k, _ any) bool { m.scopeRooms.Delete(k); return true })
	m.RedisXAddTotal.Store(0)
	m.RedisXAddErrors.Store(0)
	m.RedisXReadTotal.Store(0)
	m.RedisXReadErrors.Store(0)
	m.RedisAckTotal.Store(0)
	m.RedisLastXAddLagMicros.Store(0)
	m.RedisMirrorPrimaryErrors.Store(0)
	m.RedisMirrorSecondaryErrors.Store(0)
	m.RedisMirrorDivergenceTotal.Store(0)
	m.RedisConnected.Store(false)
	m.SetRedisLastError("")
}
