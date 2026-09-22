package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/adanman/goosar/server/internal/cloudruntime"
)

const maxStripeWebhookBodySize = 1 << 20

const stripeSignatureHeader = "Stripe-Signature"

func (h *Handler) GetCloudBillingBalance(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodGet, "/api/v1/billing/balance", cloudRuntimeProxyOptions{
		withUserID: true,
	})
}

func (h *Handler) ListCloudBillingTransactions(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodGet, "/api/v1/billing/transactions", cloudRuntimeProxyOptions{
		withUserID: true,
		withQuery:  true,
	})
}

func (h *Handler) ListCloudBillingBatches(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodGet, "/api/v1/billing/batches", cloudRuntimeProxyOptions{
		withUserID: true,
		withQuery:  true,
	})
}

func (h *Handler) ListCloudBillingTopups(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodGet, "/api/v1/billing/topups", cloudRuntimeProxyOptions{
		withUserID: true,
		withQuery:  true,
	})
}

func (h *Handler) ListCloudBillingPriceTiers(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodGet, "/api/v1/billing/price-tiers", cloudRuntimeProxyOptions{
		withUserID: true,
	})
}

func (h *Handler) CreateCloudBillingCheckoutSession(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodPost, "/api/v1/billing/checkout-sessions", cloudRuntimeProxyOptions{
		withUserID: true,
		withBody:   true,
	})
}

func (h *Handler) GetCloudBillingCheckoutSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	if !isValidStripeSessionID(sessionID) {
		writeError(w, http.StatusBadRequest, "invalid session_id")
		return
	}
	h.proxyCloudRuntime(w, r, http.MethodGet, "/api/v1/billing/checkout-sessions/"+sessionID, cloudRuntimeProxyOptions{
		withUserID: true,
	})
}

func isValidStripeSessionID(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	return true
}

func (h *Handler) CreateCloudBillingPortalSession(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodPost, "/api/v1/billing/portal-sessions", cloudRuntimeProxyOptions{
		withUserID: true,
	})
}

func (h *Handler) HandleCloudBillingStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if h.CloudRuntime == nil || !h.CloudRuntime.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "cloud runtime is not configured")
		return
	}

	if h.WebhookIPRateLimiter != nil {
		if ip := h.clientIPForRateLimit(r); ip != "" {
			if !h.WebhookIPRateLimiter.Allow(r.Context(), ip) {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
		}
	}

	if len(r.Header.Values(stripeSignatureHeader)) == 0 {
		writeError(w, http.StatusUnauthorized, "missing Stripe-Signature header")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxStripeWebhookBodySize)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	headers := http.Header{}
	if sigs := r.Header.Values(stripeSignatureHeader); len(sigs) > 0 {
		headers[stripeSignatureHeader] = sigs
	}
	if cts := r.Header.Values("Content-Type"); len(cts) > 0 {
		headers["Content-Type"] = cts
	}

	resp, err := h.CloudRuntime.Do(r.Context(), cloudruntime.Request{
		Method:    http.MethodPost,
		Path:      "/api/v1/webhooks/stripe",
		Body:      body,
		Headers:   headers,
		RequestID: cloudRuntimeRequestID(r),
	})
	if err != nil {
		writeCloudRuntimeError(w, r, err)
		return
	}
	writeCloudRuntimeResponse(w, resp)
}
