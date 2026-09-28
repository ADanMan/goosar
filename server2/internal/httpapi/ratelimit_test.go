package httpapi

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiterBehaviour(t *testing.T) {
	t.Run("caps at max per key", func(t *testing.T) {
		l := NewLimiter(3, time.Minute)
		passed := 0
		for i := 0; i < 5; i++ {
			if l.Allow("k") {
				passed++
			}
		}
		if passed != 3 {
			t.Fatalf("из 5 попыток прошло %d, ожидалось ровно 3", passed)
		}
	})

	t.Run("keys are independent", func(t *testing.T) {
		l := NewLimiter(1, time.Minute)
		l.Allow("a")
		if !l.Allow("b") {
			t.Fatal("ключ b не должен зависеть от исчерпанного лимита ключа a")
		}
	})

	t.Run("window resets", func(t *testing.T) {
		l := NewLimiter(1, 10*time.Millisecond)
		l.Allow("k")
		time.Sleep(20 * time.Millisecond)
		if !l.Allow("k") {
			t.Fatal("после окончания окна лимит должен сброситься")
		}
	})

	t.Run("nil limiter never blocks", func(t *testing.T) {
		var l *Limiter
		for i := 0; i < 10; i++ {
			if !l.Allow("k") {
				t.Fatal("nil-лимитер обязан всегда возвращать true")
			}
		}
	})
}

// TestLimiterConcurrentAccessNeverExceedsMax гоняет Allow из множества
// горутин одновременно на одном ключе — Limiter защищает свои bucket'ы
// мьютексом именно ради этого сценария (например 5/min per IP на
// send-code, куда параллельные запросы одного клиента вполне могут прийти
// одновременно), поэтому эта гарантия стоит отдельной проверки под -race.
func TestLimiterConcurrentAccessNeverExceedsMax(t *testing.T) {
	const max = 20
	l := NewLimiter(max, time.Minute)

	var wg sync.WaitGroup
	var passed int64
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow("shared-key") {
				atomic.AddInt64(&passed, 1)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&passed); got != max {
		t.Fatalf("прошло %d запросов из 200 при лимите %d, ожидалось ровно %d", got, max, max)
	}
}
