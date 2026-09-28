package cloudruntime

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// maxProxyBodyBytes — предохранитель на размер тела запроса/ответа,
// который эта сессия прокидывает; контракт не задаёт лимит для
// cloud-runtime явно (в отличие от вебхуков автопилота, 256 KiB) — 4 MiB
// выбраны как разумный запас для JSON-тел управления нодами (см.
// server2/docs/decisions.md, раздел T-028).
const maxProxyBodyBytes = 4 * 1024 * 1024

// proxy собирает http.HandlerFunc для одной операции тега CloudRuntime:
// проверяет членство вызывающего в воркспейсе (contract: owner/admin/member
// на каждой операции группы), затем делегирует forwardAndRespond.
func (d *Deps) proxy(upstreamPath string, requireBody bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := d.Resolver.RequireMember(w, r); !ok {
			return
		}
		d.forwardAndRespond(w, r, upstreamPath, requireBody)
	}
}

// forwardAndRespond — собственно прокси, без проверки членства (вынесена в
// proxy() выше; здесь она не нужна для юнит-теста, см. proxy_test.go, где
// членство подменяется напрямую актором в контексте).
func (d *Deps) forwardAndRespond(w http.ResponseWriter, r *http.Request, upstreamPath string, requireBody bool) {
	if !d.Configured() {
		writeUnavailable(w)
		return
	}

	body, ok := readRequestBody(w, r, requireBody)
	if !ok {
		return
	}

	upstreamResp, err := d.callUpstream(r, upstreamPath, body)
	if err != nil {
		writeUpstreamFailure(w, err)
		return
	}
	defer upstreamResp.Body.Close()

	relayResponse(w, upstreamResp)
}

// readRequestBody читает тело запроса под лимитом maxProxyBodyBytes и, если
// requireBody, отказывает 400-й на пустое/невалидное JSON-тело (contract:
// "тело запроса пустое или не JSON" на create/delete/exec — тот же принцип
// единообразно применён к start/stop/reboot/status, тоже несущим
// instance_id в теле).
func readRequestBody(w http.ResponseWriter, r *http.Request, requireBody bool) (body []byte, ok bool) {
	if r.Body != nil {
		b, err := io.ReadAll(io.LimitReader(r.Body, maxProxyBodyBytes+1))
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return nil, false
		}
		body = b
	}
	if requireBody && (len(body) == 0 || !json.Valid(body)) {
		httpapi.BadRequest(w, "request body is required and must be valid JSON")
		return nil, false
	}
	return body, true
}

// callUpstream строит и выполняет запрос к fleet-сервису: тот же метод и
// query-строку, что и у входящего запроса, тело как есть, плюс X-User-ID
// вызывающего и Authorization по ключу деплоя (если задан).
func (d *Deps) callUpstream(r *http.Request, upstreamPath string, body []byte) (*http.Response, error) {
	target := strings.TrimRight(d.BaseURL, "/") + upstreamPath
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, bodyReader(body))
	if err != nil {
		return nil, err
	}
	d.applyUpstreamHeaders(upstreamReq, r, body)
	return d.Client.Do(upstreamReq)
}

func (d *Deps) applyUpstreamHeaders(upstreamReq, incoming *http.Request, body []byte) {
	if len(body) > 0 {
		upstreamReq.Header.Set("Content-Type", "application/json")
	}
	if actor, ok := httpapi.ActorFrom(incoming.Context()); ok && actor.UserID != "" {
		upstreamReq.Header.Set("X-User-ID", actor.UserID)
	}
	if d.APIKey != "" {
		upstreamReq.Header.Set("Authorization", "Bearer "+d.APIKey)
	}
}

func bodyReader(body []byte) io.Reader {
	if len(body) == 0 {
		return http.NoBody
	}
	return strings.NewReader(string(body))
}

// writeUnavailable — 503, fleet-сервис не сконфигурирован (contract).
func writeUnavailable(w http.ResponseWriter) {
	httpapi.WriteError(w, http.StatusServiceUnavailable, "cloud runtime is not configured on this deployment", "cloud_runtime_unavailable")
}

// writeUpstreamFailure различает таймаут (504) от прочих сетевых сбоев (502).
func writeUpstreamFailure(w http.ResponseWriter, err error) {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		httpapi.WriteError(w, http.StatusGatewayTimeout, "cloud runtime service error", "cloud_runtime_timeout")
		return
	}
	httpapi.WriteError(w, http.StatusBadGateway, "cloud runtime service error", "cloud_runtime_bad_gateway")
}

// relayResponse копирует статус/Content-Type/тело ответа fleet-сервиса как
// есть (contract: "ответ проксируется как есть").
func relayResponse(w http.ResponseWriter, upstream *http.Response) {
	respBody, err := io.ReadAll(io.LimitReader(upstream.Body, maxProxyBodyBytes))
	if err != nil {
		httpapi.WriteError(w, http.StatusBadGateway, "cloud runtime service error", "cloud_runtime_bad_gateway")
		return
	}
	if ct := upstream.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(upstream.StatusCode)
	_, _ = w.Write(respBody)
}
