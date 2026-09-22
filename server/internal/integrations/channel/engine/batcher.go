package engine

import (
	"sync"
	"time"
)

const DefaultChatRunBatchWindow = 3 * time.Second

type stoppableTimer interface {
	Stop() bool
}

type pendingBatcher struct {
	window time.Duration

	afterFunc func(d time.Duration, fn func()) stoppableTimer

	mu      sync.Mutex
	pending map[string]*pendingEntry

	seq      uint64
	stopped  bool
	inflight sync.WaitGroup
}

type pendingEntry struct {
	timer stoppableTimer
	flush func()
	gen   uint64
}

func newPendingBatcher(window time.Duration) *pendingBatcher {
	if window <= 0 {
		window = DefaultChatRunBatchWindow
	}
	return &pendingBatcher{
		window:    window,
		afterFunc: realAfterFunc,
		pending:   make(map[string]*pendingEntry),
	}
}

func realAfterFunc(d time.Duration, fn func()) stoppableTimer {
	return time.AfterFunc(d, fn)
}

func (b *pendingBatcher) Schedule(key string, flush func()) {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		flush()
		return
	}
	b.seq++
	gen := b.seq
	fire := func() { b.onFire(key, gen) }
	if e, ok := b.pending[key]; ok {
		e.timer.Stop()
		e.flush = flush
		e.gen = gen
		e.timer = b.afterFunc(b.window, fire)
		b.mu.Unlock()
		return
	}
	b.pending[key] = &pendingEntry{
		flush: flush,
		gen:   gen,
		timer: b.afterFunc(b.window, fire),
	}
	b.mu.Unlock()
}

func (b *pendingBatcher) onFire(key string, gen uint64) {
	b.mu.Lock()
	e, ok := b.pending[key]
	if !ok || b.stopped || e.gen != gen {
		b.mu.Unlock()
		return
	}
	delete(b.pending, key)
	flush := e.flush
	b.inflight.Add(1)
	b.mu.Unlock()

	defer b.inflight.Done()
	flush()
}

func (b *pendingBatcher) FlushAll() {
	b.mu.Lock()
	b.stopped = true
	entries := make([]*pendingEntry, 0, len(b.pending))
	for _, e := range b.pending {
		e.timer.Stop()
		entries = append(entries, e)
	}
	b.pending = make(map[string]*pendingEntry)
	b.mu.Unlock()

	for _, e := range entries {
		e.flush()
	}
	b.inflight.Wait()
}

func (b *pendingBatcher) pendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}
