package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter — интерфейс лимитера "по ключу" (contract §1.5: "Бэкенд
// лимитера настраивается через Redis, если он подключён (rdb), иначе —
// in-process fallback (тот же интерфейс, per-instance)"). Домены зависят от
// этого интерфейса, а не от конкретного *Limiter, — будущая
// Redis-реализация (за рамками T-029: сейчас есть только in-process
// fallback, который контракт и разрешает) подключается заменой одной
// переменной, без правок вызывающего кода.
type RateLimiter interface {
	// Allow — true, если событие с этим ключом укладывается в лимит (и
	// сразу засчитывает его в счётчик).
	Allow(key string) bool
	// RetryAfter — сколько ждать до следующей допустимой попытки этого
	// ключа (для заголовка Retry-After на 429); 0, если лимит прямо сейчас
	// не исчерпан. Осмысленный результат гарантирован только сразу после
	// Allow(key) == false для того же ключа.
	RetryAfter(key string) time.Duration
}

var _ RateLimiter = (*Limiter)(nil)

// Limiter — in-process лимитер по скользящему окну (sliding window log): на
// ключ хранится не более max меток времени последних событий, укладывающихся
// в окно window. В отличие от счётчика с фиксированным окном, это не даёт
// всплеска в 2×max на границе окна (например 5 запросов под конец минуты +
// ещё 5 сразу в начале следующей) ценой O(max) памяти на активный ключ —
// приемлемо для лимитов этого масштаба (единицы-сотни в минуту/час).
type Limiter struct {
	max    int
	window time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	times []time.Time
}

// NewLimiter создаёт лимитер: не более max событий за window на ключ.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, buckets: make(map[string]*bucket)}
}

// Allow — см. RateLimiter.
func (l *Limiter) Allow(key string) bool {
	if l == nil || l.max <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucketLocked(key, now)
	if len(b.times) >= l.max {
		return false
	}
	b.times = append(b.times, now)
	return true
}

// RetryAfter — см. RateLimiter.
func (l *Limiter) RetryAfter(key string) time.Duration {
	if l == nil || l.max <= 0 {
		return 0
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucketLocked(key, now)
	if len(b.times) < l.max {
		return 0
	}
	if wait := b.times[0].Add(l.window).Sub(now); wait > 0 {
		return wait
	}
	return 0
}

// bucketLocked возвращает bucket ключа, предварительно вычистив метки
// старше window (вызывать только под l.mu).
func (l *Limiter) bucketLocked(key string, now time.Time) *bucket {
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{}
		l.buckets[key] = b
	}
	cutoff := now.Add(-l.window)
	i := 0
	for i < len(b.times) && b.times[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		b.times = append(b.times[:0], b.times[i:]...)
	}
	return b
}

// ClientIP извлекает IP вызывающего для ключей rate limit (без доверия
// X-Forwarded-For от произвольных клиентов — берём RemoteAddr; прокси-режим
// не входит в объём T-026/T-029).
func ClientIP(r *http.Request) string {
	host := r.RemoteAddr
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
	}
	return host
}

// KeyForRequest — ключ "по пользователю, иначе по IP", которым контракт
// описывает сразу несколько лимитов (RATE_LIMIT_API, RATE_LIMIT_TOKEN,
// RATE_LIMIT_EXPORT, RATE_LIMIT_JOIN, §1.5): предпочесть actor.UserID, если
// запрос аутентифицирован, иначе клиентский IP. Префикс ("user:"/"ip:")
// не даёт двум разным пространствам ключей случайно столкнуться.
func KeyForRequest(r *http.Request) string {
	if a, ok := ActorFrom(r.Context()); ok && a.UserID != "" {
		return "user:" + a.UserID
	}
	return "ip:" + ClientIP(r)
}

// Enforce — Allow, и при отказе сразу пишет 429+Retry-After одним вызовом:
// `if !limiter.Enforce(w, key, "too many requests") { return }` вместо
// четырёх строк (Allow/RetryAfter/TooManyRequestsRetryAfter/return),
// повторяющихся перед каждой лимитируемой ручкой.
func (l *Limiter) Enforce(w http.ResponseWriter, key, message string) bool {
	if l.Allow(key) {
		return true
	}
	TooManyRequestsRetryAfter(w, message, l.RetryAfter(key))
	return false
}

// TooManyRequests — 429 с кодом rate_limited, без заголовка Retry-After
// (вызывающий код не знает, когда лимит освободится — например уже
// специфический код ошибки без общего Limiter под рукой).
func TooManyRequests(w http.ResponseWriter, message string) {
	writeTooManyRequests(w, message, 0)
}

// TooManyRequestsRetryAfter — как TooManyRequests, но дополнительно
// выставляет заголовок Retry-After в секундах (contract §1.5: "Превышение
// лимита — 429 с заголовком Retry-After (секунды)"), округляя вверх до
// ближайшей целой секунды.
func TooManyRequestsRetryAfter(w http.ResponseWriter, message string, retryAfter time.Duration) {
	writeTooManyRequests(w, message, retryAfter)
}

func writeTooManyRequests(w http.ResponseWriter, message string, retryAfter time.Duration) {
	if retryAfter > 0 {
		seconds := int(retryAfter / time.Second)
		if retryAfter%time.Second != 0 {
			seconds++
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
	}
	if message == "" {
		message = "rate limit exceeded"
	}
	WriteError(w, http.StatusTooManyRequests, message, "rate_limited")
}

// WithAPIRateLimit — общий лимит RATE_LIMIT_API на всю группу /api/**
// (contract §1.5: "Общий API-лимит на всю аутентифицированную группу
// /api/**", по умолчанию 600/мин, по user иначе по IP). Регистрируется один
// раз поверх готового Router в app.BuildHandler, после слоя аутентификации
// (актор уже должен быть в контексте запроса — иначе ключ всегда был бы по
// IP). Пути вне префикса "/api/" (например /auth/*, /health, /ws) в этот
// лимит не попадают — у них либо нет лимита по контракту, либо свой,
// отдельно подключаемый доменом (см. internal/authn).
func WithAPIRateLimit(next http.Handler, limiter RateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			key := KeyForRequest(r)
			if !limiter.Allow(key) {
				TooManyRequestsRetryAfter(w, "", limiter.RetryAfter(key))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
