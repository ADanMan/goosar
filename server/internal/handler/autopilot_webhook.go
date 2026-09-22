package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const maxWebhookBodyBytes = 256 * 1024

const webhookTokenPrefix = "awt_"

func generateWebhookToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return webhookTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

const (
	sigStatusNotRequired = "not_required"
	sigStatusValid       = "valid"
	sigStatusInvalid     = "invalid"
	sigStatusMissing     = "missing"
)

const (
	deliveryStatusQueued     = "queued"
	deliveryStatusDispatched = "dispatched"
	deliveryStatusRejected   = "rejected"
	deliveryStatusIgnored    = "ignored"
	deliveryStatusFailed     = "failed"
)

type WebhookEnvelope struct {
	Event        string          `json:"event"`
	EventPayload json.RawMessage `json:"eventPayload"`
	Request      WebhookRequest  `json:"request"`
}

type WebhookRequest struct {
	ReceivedAt  string `json:"receivedAt"`
	ContentType string `json:"contentType,omitempty"`
}

func normalizeWebhookPayload(body []byte, headers http.Header) (WebhookEnvelope, error) {
	body = stripBOM(body)
	if len(body) == 0 {
		return WebhookEnvelope{}, errors.New("empty body")
	}

	var asAny any
	if err := json.Unmarshal(body, &asAny); err != nil {
		return WebhookEnvelope{}, fmt.Errorf("invalid json: %w", err)
	}
	switch asAny.(type) {
	case map[string]any, []any:

	default:
		return WebhookEnvelope{}, errors.New("body must be a JSON object or array")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	contentType := headers.Get("Content-Type")
	if i := strings.Index(contentType, ";"); i >= 0 {
		contentType = strings.TrimSpace(contentType[:i])
	}

	env := WebhookEnvelope{
		Request: WebhookRequest{
			ReceivedAt:  now,
			ContentType: contentType,
		},
	}

	if obj, ok := asAny.(map[string]any); ok {
		if eventStr, ok := obj["event"].(string); ok && eventStr != "" {
			if rawPayload, ok := obj["eventPayload"]; ok {
				inner, err := json.Marshal(rawPayload)
				if err == nil {
					env.Event = eventStr
					env.EventPayload = inner
					return env, nil
				}
			}

			env.Event = eventStr
			env.EventPayload = json.RawMessage(body)
			return env, nil
		}
	}

	event := inferEvent(headers, asAny)
	env.Event = event
	env.EventPayload = json.RawMessage(body)
	return env, nil
}

func inferEvent(headers http.Header, body any) string {
	if gh := headers.Get("X-GitHub-Event"); gh != "" {
		if obj, ok := body.(map[string]any); ok {
			if action, ok := obj["action"].(string); ok && action != "" {
				return "github." + gh + "." + action
			}
		}
		return "github." + gh
	}
	if gl := headers.Get("X-Gitlab-Event"); gl != "" {
		return "gitlab." + gl
	}
	if xe := headers.Get("X-Event-Type"); xe != "" {
		return xe
	}
	if obj, ok := body.(map[string]any); ok {
		if e, ok := obj["event"].(string); ok && e != "" {
			return e
		}
		if t, ok := obj["type"].(string); ok && t != "" {
			return t
		}
		if a, ok := obj["action"].(string); ok && a != "" {
			return a
		}
	}
	return "webhook.received"
}

func stripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

func extractDedupeKey(provider string, headers http.Header) (string, string) {
	if v := strings.TrimSpace(headers.Get("X-GitHub-Delivery")); v != "" && provider == "github" {
		return v, "x-github-delivery"
	}
	if v := strings.TrimSpace(headers.Get("Idempotency-Key")); v != "" {
		return v, "idempotency-key"
	}
	if v := strings.TrimSpace(headers.Get("X-GitHub-Delivery")); v != "" {
		return v, "x-github-delivery"
	}
	return "", ""
}

func verifyWebhookSignatureForProvider(provider, secret string, headers http.Header, rawBody []byte) string {
	if secret == "" {
		return sigStatusNotRequired
	}
	sig := headers.Get("X-Hub-Signature-256")
	if sig == "" {
		return sigStatusMissing
	}
	if !verifyHubSignature(secret, sig, rawBody) {
		return sigStatusInvalid
	}
	_ = provider
	return sigStatusValid
}

func verifyHubSignature(secret, header string, body []byte) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

func selectedHeadersJSON(headers http.Header) []byte {
	out := map[string]any{}
	add := func(name string) {
		if v := headers.Get(name); v != "" {
			out[strings.ToLower(name)] = v
		}
	}
	add("User-Agent")
	add("X-GitHub-Event")
	add("X-GitHub-Delivery")
	add("X-Gitlab-Event")
	add("X-Event-Type")
	add("Idempotency-Key")
	if v := headers.Get("X-Hub-Signature-256"); v != "" {
		out["x-hub-signature-256-present"] = true
	}
	b, err := json.Marshal(out)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func (h *Handler) HandleAutopilotWebhook(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	ip := h.clientIPForRateLimit(r)
	if ip != "" && h.WebhookAbsoluteIPRateLimiter != nil && !h.WebhookAbsoluteIPRateLimiter.Allow(r.Context(), ip) {
		writeWebhookRateLimit(w, r, h.WebhookAbsoluteIPRateLimiter, ip, "absolute_ip", h.Metrics)
		return
	}
	if ip != "" && h.WebhookIPRateLimiter != nil && !webhookLimiterCheck(r.Context(), h.WebhookIPRateLimiter, ip) {
		writeWebhookRateLimit(w, r, h.WebhookIPRateLimiter, ip, "bad_credential_ip", h.Metrics)
		return
	}

	trigRow, err := h.Queries.GetWebhookTriggerByToken(r.Context(), pgtype.Text{String: token, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if ip != "" && h.WebhookIPRateLimiter != nil {
				h.WebhookIPRateLimiter.Allow(r.Context(), ip)
			}
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		slog.Error("webhook: token lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	middleware.SetWebhookTriggerID(r, uuidToString(trigRow.ID))

	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
			return
		}
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	autopilot, err := h.Queries.GetAutopilot(r.Context(), trigRow.AutopilotID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		slog.Error("webhook: autopilot lookup failed",
			"error", err,
			"trigger_id", uuidToString(trigRow.ID),
		)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if uuidToString(autopilot.WorkspaceID) != uuidToString(trigRow.AutopilotWorkspaceID) {
		slog.Warn("webhook: trigger workspace mismatch",
			"trigger_id", uuidToString(trigRow.ID),
			"autopilot_id", uuidToString(autopilot.ID),
		)
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	envelope, err := normalizeWebhookPayload(body, r.Header)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode envelope")
		return
	}

	provider := trigRow.Provider
	if provider == "" {
		provider = "generic"
	}
	dedupeKey, dedupeSource := extractDedupeKey(provider, r.Header)
	sigStatus := verifyWebhookSignatureForProvider(provider, trigRow.SigningSecret.String, r.Header, body)

	delivery, dup, err := h.persistInboundDelivery(r, persistDeliveryInput{
		WorkspaceID:     autopilot.WorkspaceID,
		AutopilotID:     autopilot.ID,
		TriggerID:       trigRow.ID,
		Provider:        provider,
		Event:           envelope.Event,
		DedupeKey:       dedupeKey,
		DedupeSource:    dedupeSource,
		SignatureStatus: sigStatus,
		ContentType:     envelope.Request.ContentType,
		RawBody:         body,
		SelectedHeaders: selectedHeadersJSON(r.Header),
	})
	if err != nil {
		slog.Error("webhook: persist delivery failed",
			"error", err,
			"trigger_id", uuidToString(trigRow.ID),
		)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if dup {

		resp := map[string]any{
			"status":      "duplicate",
			"delivery_id": uuidToString(delivery.ID),
		}
		runID := delivery.AutopilotRunID
		if !runID.Valid {
			run, runErr := h.Queries.GetAutopilotRunByWebhookDelivery(r.Context(), delivery.ID)
			switch {
			case runErr == nil:
				runID = run.ID
			case !errors.Is(runErr, pgx.ErrNoRows):
				slog.Error("webhook: resolve duplicate run failed",
					"delivery_id", uuidToString(delivery.ID),
					"error", runErr,
				)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
		if runID.Valid {
			resp["run_id"] = uuidToString(runID)
		}
		if delivery.Status == deliveryStatusQueued && h.WebhookDeliveryWorker != nil {
			h.WebhookDeliveryWorker.Notify()
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if sigStatus == sigStatusInvalid || sigStatus == sigStatusMissing {
		reason := "invalid_signature"
		if sigStatus == sigStatusMissing {
			reason = "missing_signature"
		}
		respBody := map[string]any{
			"status":      "rejected",
			"delivery_id": uuidToString(delivery.ID),
			"reason":      reason,
		}
		if ip != "" && h.WebhookIPRateLimiter != nil {
			h.WebhookIPRateLimiter.Allow(r.Context(), ip)
		}
		h.finaliseDeliveryTerminal(r, delivery.ID, deliveryStatusRejected, http.StatusUnauthorized, respBody, reason)
		writeJSON(w, http.StatusUnauthorized, respBody)
		return
	}

	if !trigRow.Enabled {
		respBody := map[string]any{"status": "ignored", "delivery_id": uuidToString(delivery.ID), "reason": "trigger_disabled"}
		h.finaliseDeliveryTerminal(r, delivery.ID, deliveryStatusIgnored, http.StatusOK, respBody, "trigger_disabled")
		writeJSON(w, http.StatusOK, respBody)
		return
	}
	if autopilot.Status == "archived" {
		respBody := map[string]any{"status": "ignored", "delivery_id": uuidToString(delivery.ID), "reason": "autopilot_archived"}
		h.finaliseDeliveryTerminal(r, delivery.ID, deliveryStatusIgnored, http.StatusOK, respBody, "autopilot_archived")
		writeJSON(w, http.StatusOK, respBody)
		return
	}
	if autopilot.Status != "active" {
		respBody := map[string]any{"status": "ignored", "delivery_id": uuidToString(delivery.ID), "reason": "autopilot_paused"}
		h.finaliseDeliveryTerminal(r, delivery.ID, deliveryStatusIgnored, http.StatusOK, respBody, "autopilot_paused")
		writeJSON(w, http.StatusOK, respBody)
		return
	}

	if !webhookEventAllowedByTriggerScope(trigRow.EventFilters, envelope) {
		respBody := map[string]any{
			"status":      "ignored",
			"delivery_id": uuidToString(delivery.ID),
			"reason":      "event_filtered",
			"event":       envelope.Event,
		}
		h.finaliseDeliveryTerminal(r, delivery.ID, deliveryStatusIgnored, http.StatusOK, respBody, "event_filtered")
		writeJSON(w, http.StatusOK, respBody)
		return
	}

	run, err := h.AutopilotService.AdmitAutopilotWebhookDelivery(
		r.Context(),
		autopilot,
		trigRow.ID,
		envelopeBytes,
		delivery.ID,
	)
	if err != nil {
		slog.Warn("webhook admission failed",
			"trigger_id", uuidToString(trigRow.ID),
			"autopilot_id", uuidToString(autopilot.ID),
			"delivery_id", uuidToString(delivery.ID),
			"error", err,
		)
		if h.WebhookDeliveryWorker != nil {
			h.WebhookDeliveryWorker.Notify()
		}
		writeError(w, http.StatusInternalServerError, "failed to admit autopilot")
		return
	}

	respBody := map[string]any{
		"status":       "accepted",
		"delivery_id":  uuidToString(delivery.ID),
		"run_id":       uuidToString(run.ID),
		"autopilot_id": uuidToString(autopilot.ID),
		"trigger_id":   uuidToString(trigRow.ID),
	}
	if run.Status == "skipped" {
		respBody = map[string]any{
			"status":      "skipped",
			"delivery_id": uuidToString(delivery.ID),
			"run_id":      uuidToString(run.ID),
		}
		if run.FailureReason.Valid {
			respBody["reason"] = run.FailureReason.String
		}
	}
	bodyJSON, _ := json.Marshal(respBody)
	if _, err := h.Queries.AcknowledgeWebhookDelivery(r.Context(), db.AcknowledgeWebhookDeliveryParams{
		ID:             delivery.ID,
		ResponseStatus: pgtype.Int4{Int32: http.StatusOK, Valid: true},
		ResponseBody:   pgtype.Text{String: string(bodyJSON), Valid: true},
	}); err != nil {
		slog.Warn("webhook: persist acknowledgement metadata failed",
			"delivery_id", uuidToString(delivery.ID),
			"error", err,
		)
	}
	if h.WebhookDeliveryWorker != nil {
		h.WebhookDeliveryWorker.Notify()
	}
	writeJSON(w, http.StatusOK, respBody)
}

type WebhookEventFilter struct {
	Event   string   `json:"event"`
	Actions []string `json:"actions,omitempty"`
}

func validateWebhookEventFilters(filters []WebhookEventFilter) error {
	for i, f := range filters {
		if strings.TrimSpace(f.Event) == "" {
			return fmt.Errorf("event_filters[%d].event must not be empty", i)
		}
		for j, a := range f.Actions {
			if strings.TrimSpace(a) == "" {
				return fmt.Errorf("event_filters[%d].actions[%d] must not be empty", i, j)
			}
		}
	}
	return nil
}

func encodeWebhookEventFilters(filters []WebhookEventFilter) ([]byte, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	return json.Marshal(filters)
}

func encodeWebhookEventFiltersAlways(filters []WebhookEventFilter) ([]byte, error) {
	if filters == nil {
		filters = []WebhookEventFilter{}
	}
	return json.Marshal(filters)
}

func webhookEventAllowedByTriggerScope(eventFilters []byte, envelope WebhookEnvelope) bool {
	if len(eventFilters) == 0 {
		return true
	}
	var filters []WebhookEventFilter
	if err := json.Unmarshal(eventFilters, &filters); err != nil {

		slog.Warn("webhook: malformed event_filters, denying", "error", err)
		return false
	}
	if len(filters) == 0 {
		return true
	}
	_, eventName, eventAction := splitWebhookEvent(envelope.Event)
	actionCandidates := webhookActionCandidates(eventAction, envelope.EventPayload)
	for _, f := range filters {
		if f.Event != eventName {
			continue
		}
		if len(f.Actions) == 0 {
			return true
		}
		for _, action := range actionCandidates {
			for _, allowed := range f.Actions {
				if action == allowed {
					return true
				}
			}
		}

	}
	return false
}

func splitWebhookEvent(event string) (provider, name, action string) {
	parts := strings.Split(event, ".")
	if isKnownProvider(parts[0]) {
		if len(parts) >= 3 {
			return parts[0], parts[1], strings.Join(parts[2:], ".")
		}
		if len(parts) == 2 {
			return parts[0], parts[1], ""
		}
		return parts[0], "", ""
	}
	if len(parts) >= 2 {
		return "", parts[0], strings.Join(parts[1:], ".")
	}
	return "", event, ""
}

func isKnownProvider(prefix string) bool {
	switch prefix {
	case "github", "gitlab", "bitbucket", "gitea":
		return true
	}
	return false
}

func webhookActionCandidates(eventAction string, payload json.RawMessage) []string {
	seen := map[string]struct{}{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		seen[v] = struct{}{}
	}
	add(eventAction)
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err == nil {
		for _, key := range []string{"action", "state", "conclusion", "status"} {
			if v, ok := obj[key].(string); ok {
				add(v)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	return out
}

type persistDeliveryInput struct {
	WorkspaceID     pgtype.UUID
	AutopilotID     pgtype.UUID
	TriggerID       pgtype.UUID
	Provider        string
	Event           string
	DedupeKey       string
	DedupeSource    string
	SignatureStatus string
	ContentType     string
	RawBody         []byte
	SelectedHeaders []byte
}

func (h *Handler) persistInboundDelivery(r *http.Request, in persistDeliveryInput) (db.WebhookDelivery, bool, error) {
	params := db.CreateWebhookDeliveryParams{
		WorkspaceID:     in.WorkspaceID,
		AutopilotID:     in.AutopilotID,
		TriggerID:       in.TriggerID,
		Provider:        in.Provider,
		Event:           in.Event,
		SignatureStatus: in.SignatureStatus,
		Status:          deliveryStatusQueued,
		SelectedHeaders: in.SelectedHeaders,
		RawBody:         in.RawBody,
	}
	if in.DedupeKey != "" {
		params.DedupeKey = pgtype.Text{String: in.DedupeKey, Valid: true}
		params.DedupeSource = pgtype.Text{String: in.DedupeSource, Valid: true}
	}
	if in.ContentType != "" {
		params.ContentType = pgtype.Text{String: in.ContentType, Valid: true}
	}

	delivery, err := h.Queries.CreateWebhookDelivery(r.Context(), params)
	if err == nil {
		return delivery, false, nil
	}
	if !isUniqueViolation(err) || in.DedupeKey == "" {
		return db.WebhookDelivery{}, false, err
	}

	existing, lookupErr := h.Queries.GetWebhookDeliveryByTriggerAndDedupe(r.Context(), db.GetWebhookDeliveryByTriggerAndDedupeParams{
		TriggerID: in.TriggerID,
		DedupeKey: pgtype.Text{String: in.DedupeKey, Valid: true},
	})
	if lookupErr != nil {
		return db.WebhookDelivery{}, false, fmt.Errorf("lookup duplicate delivery: %w", lookupErr)
	}
	bumped, bumpErr := h.Queries.BumpWebhookDeliveryAttempt(r.Context(), existing.ID)
	if bumpErr != nil {

		slog.Warn("webhook: failed to bump attempt_count",
			"delivery_id", uuidToString(existing.ID),
			"error", bumpErr,
		)
		return existing, true, nil
	}
	return bumped, true, nil
}

func (h *Handler) finaliseDeliveryTerminal(
	r *http.Request,
	id pgtype.UUID,
	status string,
	httpStatus int,
	responseBody any,
	errMsg string,
) {
	bodyJSON, _ := json.Marshal(responseBody)
	params := db.UpdateWebhookDeliveryTerminalParams{
		ID:             id,
		Status:         status,
		ResponseStatus: pgtype.Int4{Int32: int32(httpStatus), Valid: true},
		ResponseBody:   pgtype.Text{String: string(bodyJSON), Valid: true},
	}
	if errMsg != "" {
		params.Error = pgtype.Text{String: errMsg, Valid: true}
	}
	if _, err := h.Queries.UpdateWebhookDeliveryTerminal(r.Context(), params); err != nil {
		slog.Warn("webhook: finalise terminal failed",
			"delivery_id", uuidToString(id),
			"status", status,
			"error", err,
		)
	}
	h.Metrics.RecordWebhookDelivery(h.deliveryProvider(r.Context(), id), status)
}

func (h *Handler) finaliseDeliveryWithRun(
	r *http.Request,
	id pgtype.UUID,
	status string,
	runID pgtype.UUID,
	httpStatus int,
	responseBody any,
) {
	bodyJSON, _ := json.Marshal(responseBody)
	params := db.UpdateWebhookDeliveryDispatchedParams{
		ID:             id,
		Status:         status,
		AutopilotRunID: runID,
		ResponseStatus: pgtype.Int4{Int32: int32(httpStatus), Valid: true},
		ResponseBody:   pgtype.Text{String: string(bodyJSON), Valid: true},
	}
	if _, err := h.Queries.UpdateWebhookDeliveryDispatched(r.Context(), params); err != nil {
		slog.Warn("webhook: finalise with run failed",
			"delivery_id", uuidToString(id),
			"run_id", uuidToString(runID),
			"error", err,
		)
	}
	h.Metrics.RecordWebhookDelivery(h.deliveryProvider(r.Context(), id), status)
}

func (h *Handler) deliveryProvider(ctx context.Context, id pgtype.UUID) string {
	if h.Queries == nil {
		return "generic"
	}
	row, err := h.Queries.GetWebhookDelivery(ctx, id)
	if err != nil || row.Provider == "" {
		return "generic"
	}
	return row.Provider
}

func writeWebhookRateLimit(w http.ResponseWriter, r *http.Request, limiter WebhookRateLimiter, key, gate string, metrics *obsmetrics.BusinessMetrics) {
	retryAfter := time.Second
	if limiter != nil {
		if retry := webhookLimiterRetryAfter(r.Context(), limiter, key); retry > 0 {
			retryAfter = retry
		}
	}
	seconds := int64((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	metrics.RecordWebhookRateLimited(gate)
	writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
}

func (h *Handler) clientIPForRateLimit(r *http.Request) string {
	remoteIP := remoteAddrHost(r.RemoteAddr)
	if len(h.cfg.TrustedProxies) == 0 {
		return remoteIP
	}
	remoteAddr, ok := parseNetIPAddr(remoteIP)
	if !ok || !addrInPrefixes(remoteAddr, h.cfg.TrustedProxies) {

		return remoteIP
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	return remoteIP
}

func remoteAddrHost(remote string) string {
	if remote == "" {
		return ""
	}
	if strings.HasPrefix(remote, "[") {
		if end := strings.IndexByte(remote, ']'); end > 0 {
			return remote[1:end]
		}
	}
	if i := strings.LastIndexByte(remote, ':'); i >= 0 && !strings.Contains(remote, "]") {
		if strings.Count(remote, ":") == 1 {
			return remote[:i]
		}
	}
	return remote
}

func parseNetIPAddr(s string) (netip.Addr, bool) {
	if s == "" {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func addrInPrefixes(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
