package daemon

import (
	"sync"
	"time"
)

type reconcileBroadcaster struct {
	mu sync.Mutex

	gen        chan struct{}
	hasReplay  bool
	lastFireAt time.Time

	minBroadcastInterval time.Duration

	now func() time.Time
}

func newReconcileBroadcaster() *reconcileBroadcaster {
	return &reconcileBroadcaster{
		gen:                  make(chan struct{}),
		minBroadcastInterval: time.Second,
		now:                  time.Now,
	}
}

func (b *reconcileBroadcaster) notify() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.hasReplay {
		b.hasReplay = false
		fired := make(chan struct{})
		close(fired)
		return fired
	}
	return b.gen
}

func (b *reconcileBroadcaster) broadcast() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	moment := b.now()
	if !b.lastFireAt.IsZero() && moment.Sub(b.lastFireAt) < b.minBroadcastInterval {
		return false
	}
	b.lastFireAt = moment
	b.hasReplay = true
	close(b.gen)
	b.gen = make(chan struct{})
	return true
}

type workspaceChangeSignal struct {
	dirty chan struct{}
}

func newWorkspaceChangeSignal() *workspaceChangeSignal {
	return &workspaceChangeSignal{dirty: make(chan struct{}, 1)}
}

func (s *workspaceChangeSignal) notify() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.dirty
}

func (s *workspaceChangeSignal) broadcast() bool {
	if s == nil {
		return false
	}
	select {
	case s.dirty <- struct{}{}:
		return true
	default:
		return false
	}
}
