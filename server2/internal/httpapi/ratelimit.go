package httpapi

import (
	"net/http"
	"sync"
	"time"
)

// Limiter — простой rate limit с фиксированным окном на ключ (IP, email,
// user id, ...). Контракт задаёт лимиты по разным осям одновременно
// (например "5/min per IP; 10/min per email"); каждый вызов Allow относится
// к одной оси — вызывающий код комбинирует несколько Limiter'ов при
// необходимости.
type Limiter struct {
	max    int
	window time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	count      int
	windowFrom time.Time
}

// NewLimiter создаёт лимитер: не более max событий за window на ключ.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, buckets: make(map[string]*bucket)}
}

// Allow — true, если событие с этим ключом укладывается в лимит (и
// засчитывает его); false — лимит исчерпан для текущего окна.
func (l *Limiter) Allow(key string) bool {
	if l == nil || l.max <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok || now.Sub(b.windowFrom) >= l.window {
		l.buckets[key] = &bucket{count: 1, windowFrom: now}
		return true
	}
	if b.count >= l.max {
		return false
	}
	b.count++
	return true
}

// ClientIP извлекает IP вызывающего для ключей rate limit (без доверия
// X-Forwarded-For от произвольных клиентов — берём RemoteAddr; прокси-режим
// не входит в объём T-026).
func ClientIP(r *http.Request) string {
	host := r.RemoteAddr
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
	}
	return host
}

// TooManyRequests — 429 с кодом rate_limited.
func TooManyRequests(w http.ResponseWriter, message string) {
	if message == "" {
		message = "rate limit exceeded"
	}
	WriteError(w, http.StatusTooManyRequests, message, "rate_limited")
}
