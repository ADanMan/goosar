package daemon

import (
	"sync"
	"testing"
	"time"
)

func TestReconcileBroadcaster_FansOutToManySubscribers(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = 0

	const subs = 16
	var wg sync.WaitGroup
	wg.Add(subs)
	wokeUp := make(chan struct{}, subs)
	for i := 0; i < subs; i++ {
		ch := b.notify()
		go func() {
			defer wg.Done()
			<-ch
			wokeUp <- struct{}{}
		}()
	}

	time.Sleep(20 * time.Millisecond)

	if !b.broadcast() {
		t.Fatalf("broadcast() = false, want true")
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("only %d/%d subscribers woke up", len(wokeUp), subs)
	}
}

func TestReconcileBroadcaster_ReplaysMissedBroadcastToFirstLateSubscriber(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = 0

	if !b.broadcast() {
		t.Fatalf("broadcast() = false, want true")
	}

	first := b.notify()
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatalf("first late subscriber did not receive replayed broadcast")
	}

	second := b.notify()
	select {
	case <-second:
		t.Fatalf("second late subscriber received a stale replay; should be fresh")
	case <-time.After(50 * time.Millisecond):

	}
}

func TestReconcileBroadcaster_ReplayPersistsAcrossSubscriberDelay(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = 0

	b.broadcast()
	time.Sleep(100 * time.Millisecond)

	ch := b.notify()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("pending replay was lost after 100ms delay")
	}
}

func TestReconcileBroadcaster_DebouncesFlappingReconnects(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = time.Second
	var nowVal time.Time
	b.now = func() time.Time { return nowVal }

	nowVal = time.Unix(1_700_000_000, 0)
	if !b.broadcast() {
		t.Fatalf("first broadcast suppressed")
	}

	for i := 0; i < 10; i++ {
		nowVal = nowVal.Add(90 * time.Millisecond)
		if b.broadcast() {
			t.Fatalf("broadcast at +%dms not debounced", (i+1)*90)
		}
	}

	nowVal = nowVal.Add(time.Second)
	if !b.broadcast() {
		t.Fatalf("broadcast past debounce window suppressed")
	}
}

func TestReconcileBroadcaster_DebounceBoundaryIsExact(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = time.Second
	var nowVal time.Time
	b.now = func() time.Time { return nowVal }

	nowVal = time.Unix(1_700_000_000, 0)
	if !b.broadcast() {
		t.Fatal("first broadcast suppressed")
	}

	nowVal = nowVal.Add(time.Second)
	if !b.broadcast() {
		t.Fatal("broadcast at exact debounce boundary was suppressed")
	}

	nowVal = nowVal.Add(999 * time.Millisecond)
	if b.broadcast() {
		t.Fatal("broadcast at boundary-minus-1ms was not suppressed")
	}
}

func TestReconcileBroadcaster_ReSubscribesEachWake(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = 0

	ch1 := b.notify()
	b.broadcast()
	select {
	case <-ch1:
	case <-time.After(time.Second):
		t.Fatalf("first wake did not arrive on ch1")
	}

	b.broadcast()
	select {
	case <-ch1:

	default:
		t.Fatalf("ch1 should remain closed after second broadcast")
	}
}

func TestWorkspaceChangeSignalCoalescesUntilConsumed(t *testing.T) {
	s := newWorkspaceChangeSignal()
	if !s.broadcast() {
		t.Fatal("first workspace change was not recorded")
	}
	if s.broadcast() {
		t.Fatal("duplicate workspace change should coalesce while dirty")
	}

	select {
	case <-s.notify():
	case <-time.After(time.Second):
		t.Fatal("recorded workspace change was not delivered")
	}
	if !s.broadcast() {
		t.Fatal("workspace change after consumption was not recorded")
	}
}

func TestReconcileBroadcaster_ConcurrentBroadcastAndNotify(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = 0

	stop := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				ch := b.notify()
				select {
				case <-ch:
				case <-stop:
					return
				}
			}
		}()
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				b.broadcast()
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent broadcast/notify did not converge after stop")
	}
}
