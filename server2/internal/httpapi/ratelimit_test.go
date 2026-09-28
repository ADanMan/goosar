package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestLimiterRetryAfter(t *testing.T) {
	l := NewLimiter(1, 100*time.Millisecond)
	if rt := l.RetryAfter("k"); rt != 0 {
		t.Fatalf("RetryAfter до исчерпания лимита = %v, ожидался 0", rt)
	}
	l.Allow("k")
	if l.Allow("k") {
		t.Fatal("второй Allow в том же окне должен быть отклонён")
	}
	rt := l.RetryAfter("k")
	if rt <= 0 || rt > 100*time.Millisecond {
		t.Fatalf("RetryAfter = %v, ожидалось (0, 100ms]", rt)
	}
}

func TestKeyForRequestPrefersActor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.RemoteAddr = "203.0.113.5:1234"
	if got := KeyForRequest(r); got != "ip:203.0.113.5" {
		t.Fatalf("KeyForRequest без актора = %q, ожидался ip:203.0.113.5", got)
	}

	actor := &Actor{UserID: "u1"}
	r = r.WithContext(WithActor(context.Background(), actor))
	if got := KeyForRequest(r); got != "user:u1" {
		t.Fatalf("KeyForRequest с актором = %q, ожидался user:u1", got)
	}
}

func TestTooManyRequestsRetryAfterSetsHeader(t *testing.T) {
	w := httptest.NewRecorder()
	TooManyRequestsRetryAfter(w, "", 2500*time.Millisecond)
	if got := w.Header().Get("Retry-After"); got != "3" {
		t.Fatalf("Retry-After = %q, ожидалось 3 (округление вверх)", got)
	}
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, ожидался 429", w.Code)
	}
}

func TestWithAPIRateLimitSkipsNonAPIPaths(t *testing.T) {
	l := NewLimiter(0, time.Minute) // max<=0: Allow всегда true, но проверим что /health вообще не спрашивает лимитер
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := WithAPIRateLimit(next, l)

	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called || w.Code != http.StatusOK {
		t.Fatalf("/health должен пройти без учёта лимитера: called=%v code=%d", called, w.Code)
	}
}

func TestLimiterEnforce(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	w1 := httptest.NewRecorder()
	if !l.Enforce(w1, "k", "nope") {
		t.Fatal("первый вызов Enforce должен пройти")
	}
	if w1.Code != http.StatusOK {
		t.Fatalf("Enforce не должен трогать ответ при успехе, код = %d", w1.Code)
	}

	w2 := httptest.NewRecorder()
	if l.Enforce(w2, "k", "nope") {
		t.Fatal("второй вызов Enforce на исчерпанном лимите должен вернуть false")
	}
	if w2.Code != http.StatusTooManyRequests || w2.Header().Get("Retry-After") == "" {
		t.Fatalf("Enforce при отказе должен писать 429+Retry-After, код=%d Retry-After=%q",
			w2.Code, w2.Header().Get("Retry-After"))
	}
}

func TestWithAPIRateLimitBlocksOverLimit(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := WithAPIRateLimit(next, l)

	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.RemoteAddr = "203.0.113.9:1"
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, r)
	if w1.Code != http.StatusOK {
		t.Fatalf("первый запрос: код = %d, ожидался 200", w1.Code)
	}

	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("второй запрос: код = %d, ожидался 429", w2.Code)
	}
	if w2.Header().Get("Retry-After") == "" {
		t.Fatal("ожидался заголовок Retry-After на 429")
	}
}
