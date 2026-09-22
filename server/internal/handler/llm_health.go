package handler

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

const llmHealthTimeout = 10 * time.Second

const llmHealthCacheTTL = 60 * time.Second

const (
	LLMHealthOK           = "ok"
	LLMHealthAuthRejected = "auth_rejected"
	LLMHealthDegraded     = "degraded"
	LLMHealthUnreachable  = "unreachable"
	LLMHealthUnconfigured = "unconfigured"
)

type LLMHealthResponse struct {
	Status    string `json:"status"`
	CheckedAt string `json:"checked_at"`
	LatencyMS int64  `json:"latency_ms"`
}

type llmHealthCacheEntry struct {
	response LLMHealthResponse
	expires  time.Time
}

type llmHealthState struct {
	mu    sync.Mutex
	entry *llmHealthCacheEntry
}

func (h *Handler) llmHealthGuard() *llmHealthState {
	if h.llmHealth == nil {
		h.llmHealth = &llmHealthState{}
	}
	return h.llmHealth
}

func (h *Handler) GetLLMHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.resolveLLMHealth(r.Context()))
}

func (h *Handler) resolveLLMHealth(ctx context.Context) LLMHealthResponse {
	state := h.llmHealthGuard()

	state.mu.Lock()
	if state.entry != nil && time.Now().Before(state.entry.expires) {
		cached := state.entry.response
		state.mu.Unlock()
		return cached
	}
	state.mu.Unlock()

	resp := h.probeLLMHealth(ctx)

	state.mu.Lock()
	state.entry = &llmHealthCacheEntry{response: resp, expires: time.Now().Add(llmHealthCacheTTL)}
	state.mu.Unlock()

	return resp
}

func (h *Handler) probeLLMHealth(ctx context.Context) LLMHealthResponse {
	now := time.Now()
	baseURL := strings.TrimSpace(h.cfg.LLMBaseURL)
	apiKey := strings.TrimSpace(h.cfg.LLMAPIKey)
	if baseURL == "" || apiKey == "" {
		return LLMHealthResponse{
			Status:    LLMHealthUnconfigured,
			CheckedAt: now.UTC().Format(time.RFC3339),
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, llmHealthTimeout)
	defer cancel()

	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return LLMHealthResponse{Status: LLMHealthUnreachable, CheckedAt: now.UTC().Format(time.RFC3339)}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := h.llmHealthHTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	start := time.Now()
	res, err := client.Do(req)
	latency := time.Since(start)
	checkedAt := now.UTC().Format(time.RFC3339)
	if err != nil {
		return LLMHealthResponse{Status: LLMHealthUnreachable, CheckedAt: checkedAt, LatencyMS: latency.Milliseconds()}
	}
	defer res.Body.Close()

	status := classifyLLMHealthStatus(res.StatusCode)
	return LLMHealthResponse{Status: status, CheckedAt: checkedAt, LatencyMS: latency.Milliseconds()}
}

func classifyLLMHealthStatus(code int) string {
	switch {
	case code >= 200 && code < 300:
		return LLMHealthOK
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return LLMHealthAuthRejected
	default:
		return LLMHealthDegraded
	}
}
