// handlers_webhook.go — публичный `POST /api/webhooks/autopilots/{token}`
// (contract §3.6/§7.3). Не под общей аутентификацией домена — `token` в
// пути сам является учётной записью (`x-roles: public (bearer is the
// trigger's own opaque token)`), поэтому этот хендлер не зовёт
// httpapi.RequireActor/wsctx.Resolver вовсе.
package autopilot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// maxWebhookBodyBytes — contract: "413: payload larger than 256 KiB".
const maxWebhookBodyBytes = 256 * 1024

// webhookEnvelope — "{event, eventPayload, request}" контракта.
type webhookEnvelope struct {
	Event        string          `json:"event"`
	EventPayload json.RawMessage `json:"eventPayload"`
	Request      webhookRequest  `json:"request"`
}

type webhookRequest struct {
	Headers map[string]string `json:"headers"`
}

func (d *Deps) handleWebhookAutopilotTrigger(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	resolved, err := d.Store.triggerByWebhookToken(r.Context(), token)
	if err == ErrTriggerNotFound {
		httpapi.NotFound(w, "unknown webhook token")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodyBytes+1))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if len(body) > maxWebhookBodyBytes {
		httpapi.WriteError(w, http.StatusRequestEntityTooLarge, "payload too large", "payload_too_large")
		return
	}

	dedupeKey, dedupeSource := dedupeKeyFrom(r)
	if dedupeKey != "" {
		if existing, found, err := d.Store.FindDeliveryByDedupe(r.Context(), resolved.ID, dedupeKey); err == nil && found && existing.Status != "rejected" {
			httpapi.WriteJSON(w, http.StatusOK, map[string]any{
				"status": "duplicate", "delivery_id": existing.ID, "run_id": strFromPtr(existing.AutopilotRunID),
				"autopilot_id": resolved.AutopilotID, "trigger_id": resolved.ID, "event": existing.Event,
			})
			return
		}
	}

	event := eventNameFrom(r, resolved)
	sigStatus, rejectReason := d.verifyWebhookSignature(r, resolved, body)
	if rejectReason != "" {
		bodyStr := string(body)
		del, cerr := d.Store.CreateDelivery(r.Context(), CreateDeliveryParams{
			WorkspaceID: resolved.SentinelWorkspaceID, AutopilotID: resolved.AutopilotID, TriggerID: resolved.ID,
			Provider: strFromPtr(resolved.Provider), Event: event, DedupeKey: nullableString(dedupeKey),
			DedupeSource: nullableString(dedupeSource), SignatureStatus: sigStatus, Status: "rejected",
			ContentType: nullableString(r.Header.Get("Content-Type")), SelectedHeaders: selectedHeaders(r),
			RawBody: &bodyStr, Error: &rejectReason,
		})
		if cerr != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		httpapi.WriteJSON(w, http.StatusUnauthorized, map[string]any{
			"status": "rejected", "delivery_id": del.ID, "reason": rejectReason,
		})
		return
	}

	envelope := normalizeWebhookEnvelope(event, body)
	matched := matchEventFilters(resolved.Trigger, envelope)

	bodyStr := string(body)
	if !matched {
		del, cerr := d.Store.CreateDelivery(r.Context(), CreateDeliveryParams{
			WorkspaceID: resolved.SentinelWorkspaceID, AutopilotID: resolved.AutopilotID, TriggerID: resolved.ID,
			Provider: strFromPtr(resolved.Provider), Event: event, DedupeKey: nullableString(dedupeKey),
			DedupeSource: nullableString(dedupeSource), SignatureStatus: sigStatus, Status: "ignored",
			ContentType: nullableString(r.Header.Get("Content-Type")), SelectedHeaders: selectedHeaders(r),
			RawBody: &bodyStr,
		})
		if cerr != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{
			"status": "ignored", "delivery_id": del.ID, "autopilot_id": resolved.AutopilotID,
			"trigger_id": resolved.ID, "event": event, "reason": "no matching event filter",
		})
		return
	}

	result, err := d.Dispatcher.DispatchWebhook(r.Context(), resolved.AutopilotID, resolved.ID, envelope)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	respStatus := "accepted"
	if result.Run.Status == "skipped" {
		respStatus = "skipped"
	}
	runID := result.Run.ID
	del, cerr := d.Store.CreateDelivery(r.Context(), CreateDeliveryParams{
		WorkspaceID: resolved.SentinelWorkspaceID, AutopilotID: resolved.AutopilotID, TriggerID: resolved.ID,
		Provider: strFromPtr(resolved.Provider), Event: event, DedupeKey: nullableString(dedupeKey),
		DedupeSource: nullableString(dedupeSource), SignatureStatus: sigStatus, Status: "dispatched",
		ContentType: nullableString(r.Header.Get("Content-Type")), SelectedHeaders: selectedHeaders(r),
		RawBody: &bodyStr, AutopilotRunID: &runID,
	})
	if cerr != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(resolved.SentinelWorkspaceID, "autopilot:run_start", map[string]any{"run": result.Run})
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"status": respStatus, "delivery_id": del.ID, "run_id": runID,
		"autopilot_id": resolved.AutopilotID, "trigger_id": resolved.ID, "event": event,
	})
}

// dedupeKeyFrom — contract: "Дедуп по X-GitHub-Delivery/Idempotency-Key".
func dedupeKeyFrom(r *http.Request) (key, source string) {
	if v := r.Header.Get("X-GitHub-Delivery"); v != "" {
		return v, "X-GitHub-Delivery"
	}
	if v := r.Header.Get("Idempotency-Key"); v != "" {
		return v, "Idempotency-Key"
	}
	return "", ""
}

// eventNameFrom — источник имени события: заголовок X-GitHub-Event для
// provider=github (тот же заголовок, что и настоящий GitHub webhook, см.
// docs/50-api-contract.md §3.6 "POST /api/webhooks/github"); для generic —
// поле "event" тела запроса, если оно есть; иначе "generic" — контракт не
// формализует источник имени события для generic-провайдера (см.
// server2/docs/decisions.md, раздел T-028).
func eventNameFrom(r *http.Request, t resolvedTrigger) string {
	if t.Provider != nil && *t.Provider == "github" {
		if v := r.Header.Get("X-GitHub-Event"); v != "" {
			return v
		}
	}
	return "generic"
}

func normalizeWebhookEnvelope(event string, body []byte) json.RawMessage {
	env := webhookEnvelope{Event: event, EventPayload: nonEmptyOrNull(body)}
	b, err := json.Marshal(env)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

func nonEmptyOrNull(b []byte) json.RawMessage {
	if len(b) == 0 || !json.Valid(b) {
		return json.RawMessage(`null`)
	}
	return b
}

// verifyWebhookSignature — HMAC-SHA256 над сырым телом, ключ — расшифрованный
// strig_signing_secret_sealed. Заголовок подписи: `X-Hub-Signature-256`
// (`sha256=<hex>`) для provider=github (тот же формат, что и настоящий
// GitHub App webhook), `X-Autopilot-Signature` в том же формате для generic
// — имя заголовка для generic не задано контрактом, решение зафиксировано в
// server2/docs/decisions.md, раздел T-028. Секрет не задан — not_required,
// пропускается без проверки.
func (d *Deps) verifyWebhookSignature(r *http.Request, t resolvedTrigger, body []byte) (status, rejectReason string) {
	if !t.HasSigningSecret {
		return "not_required", ""
	}
	secret, ok, err := d.Store.signingSecretFor(r.Context(), t.ID, d.McpSecretKey, d.McpSecretKeyPrevious)
	if err != nil || !ok {
		// секрет помечен как заданный, но расшифровать не удалось (например
		// GOOSAR_MCP_SECRET_KEY сменился без ротации) — по контракту это тот
		// же исход, что "нет валидной подписи": сервер не может её проверить.
		return "invalid", "invalid_signature"
	}
	headerName := "X-Autopilot-Signature"
	if t.Provider != nil && *t.Provider == "github" {
		headerName = "X-Hub-Signature-256"
	}
	header := r.Header.Get(headerName)
	if header == "" {
		return "missing", "missing_signature"
	}
	want := computeHMACSHA256Hex(secret, body)
	got := extractHexSignature(header)
	if !hmac.Equal([]byte(want), []byte(got)) {
		return "invalid", "invalid_signature"
	}
	return "valid", ""
}

func matchEventFilters(t Trigger, envelope json.RawMessage) bool {
	var filters []map[string]any
	if len(t.EventFilters) == 0 {
		return true
	}
	if err := json.Unmarshal(t.EventFilters, &filters); err != nil || len(filters) == 0 {
		return true
	}
	var env map[string]any
	_ = json.Unmarshal(envelope, &env)
	for _, f := range filters {
		if filterMatches(f, env) {
			return true
		}
	}
	return false
}

// filterMatches — один WebhookEventFilter contract-объект (additionalProperties:
// true, форма не формализована — см. server2/docs/decisions.md, раздел
// T-028). Решение: ключ "event" сравнивается с envelope.event; ключ "payload"
// (объект) — плоское сравнение равенства top-level полей envelope.eventPayload.
// Пустой фильтр {} — совпадает всегда.
func filterMatches(filter map[string]any, envelope map[string]any) bool {
	if want, ok := filter["event"]; ok {
		if got, _ := envelope["event"].(string); got != want {
			return false
		}
	}
	if payloadFilter, ok := filter["payload"].(map[string]any); ok {
		payload, _ := envelope["eventPayload"].(map[string]any)
		for k, want := range payloadFilter {
			if payload == nil {
				return false
			}
			got, present := payload[k]
			if !present || got != want {
				return false
			}
		}
	}
	return true
}

func strFromPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func selectedHeaders(r *http.Request) map[string]string {
	names := []string{"Content-Type", "X-GitHub-Event", "X-GitHub-Delivery", "Idempotency-Key", "User-Agent"}
	out := map[string]string{}
	for _, n := range names {
		if v := r.Header.Get(n); v != "" {
			out[n] = v
		}
	}
	return out
}

func computeHMACSHA256Hex(key string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func extractHexSignature(header string) string {
	if idx := strings.IndexByte(header, '='); idx >= 0 {
		return header[idx+1:]
	}
	return header
}
