// Политика сессий деплоя: сколько может жить сессия, сколько сессий держит
// один пользователь и для кого обязателен второй фактор. Живёт как блок
// session внутри уже существующего документа политики деплоя, а не как
// отдельная поверхность со своей таблицей и аудитом. Значения по умолчанию
// нарочно мягкие — стенд, обновившийся до этой фичи, не должен разлогинить
// всех первым же запросом.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	DefaultSessionIdleTimeoutHours     = 12
	DefaultSessionAbsoluteLifetimeDays = 30

	DefaultSessionMaxConcurrent = 0
)

const (
	RequireMFANone   = "none"
	RequireMFAAdmins = "admins"
	RequireMFAAll    = "all"
)

type SessionPolicy struct {
	IdleTimeoutHours     int    `json:"idle_timeout_hours"`
	AbsoluteLifetimeDays int    `json:"absolute_lifetime_days"`
	MaxConcurrent        int    `json:"max_concurrent_sessions"`
	RequireMFA           string `json:"require_mfa"`
}

func DefaultSessionPolicy() SessionPolicy {
	return SessionPolicy{
		IdleTimeoutHours:     DefaultSessionIdleTimeoutHours,
		AbsoluteLifetimeDays: DefaultSessionAbsoluteLifetimeDays,
		MaxConcurrent:        DefaultSessionMaxConcurrent,
		RequireMFA:           RequireMFANone,
	}
}

func (p SessionPolicy) IdleTimeout() time.Duration {
	return time.Duration(p.IdleTimeoutHours) * time.Hour
}

func (p SessionPolicy) AbsoluteLifetime() time.Duration {
	return time.Duration(p.AbsoluteLifetimeDays) * 24 * time.Hour
}

func (p SessionPolicy) MFARequiredFor(isDeploymentAdmin bool) bool {
	switch p.RequireMFA {
	case RequireMFAAll:
		return true
	case RequireMFAAdmins:
		return isDeploymentAdmin
	default:
		return false
	}
}

const (
	minSessionIdleTimeoutHours     = 1
	maxSessionIdleTimeoutHours     = 24 * 30
	minSessionAbsoluteLifetimeDays = 1
	maxSessionAbsoluteLifetimeDays = 365
	maxSessionMaxConcurrent        = 100
)

type sessionPolicyBlock struct {
	IdleTimeoutHours     *int    `json:"idle_timeout_hours"`
	AbsoluteLifetimeDays *int    `json:"absolute_lifetime_days"`
	MaxConcurrent        *int    `json:"max_concurrent_sessions"`
	RequireMFA           *string `json:"require_mfa"`
}

func parseSessionPolicy(doc []byte) SessionPolicy {
	out := DefaultSessionPolicy()
	if len(doc) == 0 {
		return out
	}
	var wrapper struct {
		Session *sessionPolicyBlock `json:"session"`
	}
	if err := json.Unmarshal(doc, &wrapper); err != nil || wrapper.Session == nil {
		return out
	}
	blk := wrapper.Session
	if blk.IdleTimeoutHours != nil {
		out.IdleTimeoutHours = *blk.IdleTimeoutHours
	}
	if blk.AbsoluteLifetimeDays != nil {
		out.AbsoluteLifetimeDays = *blk.AbsoluteLifetimeDays
	}
	if blk.MaxConcurrent != nil {
		out.MaxConcurrent = *blk.MaxConcurrent
	}
	if blk.RequireMFA != nil {
		out.RequireMFA = strings.ToLower(strings.TrimSpace(*blk.RequireMFA))
	}
	return out
}

func validateSessionPolicyBlock(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var blk sessionPolicyBlock
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&blk); err != nil {
		return errors.New("session block accepts only idle_timeout_hours, absolute_lifetime_days, max_concurrent_sessions and require_mfa")
	}
	if blk.IdleTimeoutHours != nil && (*blk.IdleTimeoutHours < minSessionIdleTimeoutHours || *blk.IdleTimeoutHours > maxSessionIdleTimeoutHours) {
		return errIntRange("session.idle_timeout_hours", minSessionIdleTimeoutHours, maxSessionIdleTimeoutHours)
	}
	if blk.AbsoluteLifetimeDays != nil && (*blk.AbsoluteLifetimeDays < minSessionAbsoluteLifetimeDays || *blk.AbsoluteLifetimeDays > maxSessionAbsoluteLifetimeDays) {
		return errIntRange("session.absolute_lifetime_days", minSessionAbsoluteLifetimeDays, maxSessionAbsoluteLifetimeDays)
	}
	if blk.MaxConcurrent != nil && (*blk.MaxConcurrent < 0 || *blk.MaxConcurrent > maxSessionMaxConcurrent) {
		return errIntRange("session.max_concurrent_sessions", 0, maxSessionMaxConcurrent)
	}
	if blk.RequireMFA != nil {
		switch strings.ToLower(strings.TrimSpace(*blk.RequireMFA)) {
		case RequireMFANone, RequireMFAAdmins, RequireMFAAll:
		default:
			return errors.New(`session.require_mfa must be one of "none", "admins", "all"`)
		}
	}
	return nil
}

func errIntRange(field string, lo, hi int) error {
	return fmt.Errorf("%s must be between %d and %d", field, lo, hi)
}

const sessionPolicyCacheTTL = 30 * time.Second

type sessionPolicyCache struct {
	mu       sync.RWMutex
	policy   SessionPolicy
	loadedAt time.Time
}

var sessionPolicies sessionPolicyCache

func (c *sessionPolicyCache) get() (SessionPolicy, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.loadedAt.IsZero() || time.Since(c.loadedAt) > sessionPolicyCacheTTL {
		return SessionPolicy{}, false
	}
	return c.policy, true
}

func (c *sessionPolicyCache) set(p SessionPolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.policy = p
	c.loadedAt = time.Now()
}

func (c *sessionPolicyCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadedAt = time.Time{}
}

func InvalidateSessionPolicyCache() { sessionPolicies.invalidate() }

type SessionPolicyReader interface {
	SessionPolicy(ctx context.Context) SessionPolicy
}

func (h *Handler) SessionPolicy(ctx context.Context) SessionPolicy {
	if p, ok := sessionPolicies.get(); ok {
		return p
	}
	policy := DefaultSessionPolicy()
	if h.Queries != nil {
		row, err := h.Queries.GetDeploymentPolicy(ctx)
		switch {
		case err == nil:
			policy = parseSessionPolicy(row.Policy)
		case errors.Is(err, pgx.ErrNoRows):

		default:

			return policy
		}
	}
	sessionPolicies.set(policy)
	return policy
}
