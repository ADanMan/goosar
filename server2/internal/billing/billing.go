// Package billing реализует `/api/cloud-billing/**` и `POST /api/webhooks/stripe`
// (T-029, docs/50-api-contract.md §6): оба — прозрачный HTTP-прокси во
// внешний облачный биллинговый сервис (тот же деплой, что internal/cloudruntime
// проксирует под `/api/cloud-runtime/**` — GOOSAR_CLOUDRUNTIME_BASE_URL/_API_KEY,
// см. server2/docs/decisions.md, разделы T-028 и T-029). Контракт прямо
// оговаривает («Спорные места», п.5), что этот уровень не валидирует тело
// содержательно — вся проверка на стороне внешнего сервиса.
package billing

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Deps — зависимости домена billing.
type Deps struct {
	BaseURL string // GOOSAR_CLOUDRUNTIME_BASE_URL — пусто = 503 на всю группу
	APIKey  string // GOOSAR_CLOUDRUNTIME_API_KEY
	Client  *http.Client
	Logger  *slog.Logger
}

func New(baseURL, apiKey string, logger *slog.Logger) *Deps {
	return &Deps{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Client:  &http.Client{Timeout: 15 * time.Second},
		Logger:  logger,
	}
}

// Configured — облачный биллинговый сервис настроен на этом деплое.
func (d *Deps) Configured() bool { return d.BaseURL != "" }

const maxProxyBodyBytes = 1 << 20 // 1 MiB — тот же порядок, что contract называет для webhooks/stripe

// route — одна операция тега CloudBilling: метод+путь контракта, суффикс
// пути у биллингового сервиса (без BaseURL), и требует ли операция
// непустое тело (POST checkout-sessions/portal-sessions).
type route struct {
	method       string
	contractPath string
	upstream     string
	needsBody    bool
}

var billingRoutes = []route{
	{http.MethodGet, "/api/cloud-billing/balance", "/api/v1/billing/balance", false},
	{http.MethodGet, "/api/cloud-billing/transactions", "/api/v1/billing/transactions", false},
	{http.MethodGet, "/api/cloud-billing/batches", "/api/v1/billing/batches", false},
	{http.MethodGet, "/api/cloud-billing/topups", "/api/v1/billing/topups", false},
	{http.MethodGet, "/api/cloud-billing/price-tiers", "/api/v1/billing/price-tiers", false},
	{http.MethodPost, "/api/cloud-billing/checkout-sessions", "/api/v1/billing/checkout-sessions", true},
	{http.MethodPost, "/api/cloud-billing/portal-sessions", "/api/v1/billing/portal-sessions", false},
}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Register регистрирует операции тега CloudBilling + POST /api/webhooks/stripe.
func Register(router *httpapi.Router, deps *Deps) {
	for _, rt := range billingRoutes {
		router.Handle(rt.method, rt.contractPath, deps.proxy(rt.upstream, rt.needsBody))
	}
	router.Handle(http.MethodGet, "/api/cloud-billing/checkout-sessions/{sessionId}", deps.handleGetCheckoutSession)
	router.Handle(http.MethodPost, "/api/webhooks/stripe", deps.handleStripeWebhook)
}

// proxy собирает http.HandlerFunc для одну операцию: требует
// аутентифицированного вызывающего (contract: any-authenticated/human на
// каждой ручке группы), затем форвардит запрос как есть.
func (d *Deps) proxy(upstreamPath string, needsBody bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := httpapi.RequireActor(w, r)
		if !ok {
			return
		}
		d.forward(w, r, actor.UserID, upstreamPath, needsBody)
	}
}

// handleGetCheckoutSession — GET /api/cloud-billing/checkout-sessions/{sessionId}:
// contract требует валидировать sessionId по алфавиту буквы/цифры/подчёркивание
// до похода к биллингу (400 иначе).
func (d *Deps) handleGetCheckoutSession(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	sessionID := r.PathValue("sessionId")
	if !sessionIDPattern.MatchString(sessionID) {
		httpapi.BadRequest(w, "sessionId не проходит валидацию формата")
		return
	}
	d.forward(w, r, actor.UserID, "/api/v1/billing/checkout-sessions/"+sessionID, false)
}

func (d *Deps) forward(w http.ResponseWriter, r *http.Request, userID, upstreamPath string, needsBody bool) {
	if !d.Configured() {
		writeUnavailable(w)
		return
	}
	body, ok := readBody(w, r, needsBody)
	if !ok {
		return
	}
	resp, err := d.callUpstream(r, upstreamPath, body, userID)
	if err != nil {
		writeUpstreamFailure(w, err)
		return
	}
	defer resp.Body.Close()
	relay(w, resp)
}

func readBody(w http.ResponseWriter, r *http.Request, needsBody bool) ([]byte, bool) {
	var body []byte
	if r.Body != nil {
		b, err := io.ReadAll(io.LimitReader(r.Body, maxProxyBodyBytes+1))
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return nil, false
		}
		body = b
	}
	if needsBody && len(body) == 0 {
		httpapi.BadRequest(w, "request body is required")
		return nil, false
	}
	return body, true
}

func (d *Deps) callUpstream(r *http.Request, upstreamPath string, body []byte, userID string) (*http.Response, error) {
	target := strings.TrimRight(d.BaseURL, "/") + upstreamPath
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, reader)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	if d.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+d.APIKey)
	}
	return d.Client.Do(req)
}

func writeUnavailable(w http.ResponseWriter) {
	httpapi.WriteError(w, http.StatusServiceUnavailable, "облачный биллинговый сервис не подключён на этом деплое", "cloud_billing_unavailable")
}

func writeUpstreamFailure(w http.ResponseWriter, err error) {
	var netErr net.Error
	if ok := isTimeout(err, &netErr); ok {
		httpapi.WriteError(w, http.StatusGatewayTimeout, "cloud billing service error", "cloud_billing_timeout")
		return
	}
	httpapi.WriteError(w, http.StatusBadGateway, "cloud billing service error", "cloud_billing_bad_gateway")
}

func isTimeout(err error, netErr *net.Error) bool {
	if e, ok := err.(net.Error); ok {
		*netErr = e
		return e.Timeout()
	}
	return false
}

func relay(w http.ResponseWriter, upstream *http.Response) {
	body, err := io.ReadAll(io.LimitReader(upstream.Body, maxProxyBodyBytes))
	if err != nil {
		httpapi.WriteError(w, http.StatusBadGateway, "cloud billing service error", "cloud_billing_bad_gateway")
		return
	}
	if ct := upstream.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(upstream.StatusCode)
	_, _ = w.Write(body)
}

const maxStripeBodyBytes = 1 << 20 // 1 MiB, contract: "body larger than 1 MiB → 413"

// handleStripeWebhook — POST /api/webhooks/stripe: тело + Stripe-Signature
// форвардятся как есть в биллинговый сервис облачного рантайма, который сам
// проверяет подпись (contract: "проверяется выше по цепочке", securitySchemes
// .stripeWebhookSignature "verified by the upstream cloud-runtime billing
// service, not by this server directly"); этот уровень только требует
// присутствия заголовка и режет по размеру/rate limit'у (rate limit — общая
// httpapi.WithCommonMiddleware/IP-лимитер, не этот файл).
func (d *Deps) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	sig := r.Header.Get("Stripe-Signature")
	if sig == "" {
		httpapi.Unauthorized(w, "missing Stripe-Signature header")
		return
	}
	if !d.Configured() {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "cloud runtime billing not configured", "cloud_billing_unavailable")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxStripeBodyBytes+1))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if len(body) > maxStripeBodyBytes {
		httpapi.WriteError(w, http.StatusRequestEntityTooLarge, "body larger than 1 MiB", "payload_too_large")
		return
	}
	target := strings.TrimRight(d.BaseURL, "/") + "/api/v1/webhooks/stripe"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, strings.NewReader(string(body)))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", sig)
	if d.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+d.APIKey)
	}
	resp, err := d.Client.Do(req)
	if err != nil {
		writeUpstreamFailure(w, err)
		return
	}
	defer resp.Body.Close()
	relay(w, resp)
}
