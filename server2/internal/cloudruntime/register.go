// Package cloudruntime реализует `/api/cloud-runtime/**` (docs/50-api-contract.md
// §6): не самостоятельная подсистема, а прозрачный HTTP-прокси во внешний
// облачный fleet-сервис управления виртуальными runtime-нодами. server2 не
// хранит состояние нод — только форвардит запрос, добавляя X-User-ID и (для
// нод-мутаций) тело как есть; ответ ретранслируется как есть.
//
// Имена переменных окружения (GOOSAR_CLOUDRUNTIME_BASE_URL/_API_KEY) не
// зафиксированы контрактом — решение зафиксировано в
// server2/docs/decisions.md, раздел T-028.
package cloudruntime

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/wsctx"
)

// Deps — зависимости домена cloudruntime.
type Deps struct {
	BaseURL  string // GOOSAR_CLOUDRUNTIME_BASE_URL — пусто = не настроено (contract: 503 на все маршруты группы)
	APIKey   string // GOOSAR_CLOUDRUNTIME_API_KEY — Authorization: Bearer <key> к fleet-сервису, если задан
	Client   *http.Client
	Resolver *wsctx.Resolver
	Logger   *slog.Logger
}

// New собирает Deps. baseURL/apiKey — уже прочитанные значения
// config.Config (GOOSAR_CLOUDRUNTIME_BASE_URL/_API_KEY).
func New(db *store.Store, baseURL, apiKey string, logger *slog.Logger) *Deps {
	return &Deps{
		BaseURL:  baseURL,
		APIKey:   apiKey,
		Client:   &http.Client{Timeout: 15 * time.Second},
		Resolver: wsctx.New(db),
		Logger:   logger,
	}
}

// Configured — fleet-сервис настроен на этом деплое (contract: "если
// fleet-сервис не настроен — все маршруты этой группы отвечают 503").
func (d *Deps) Configured() bool { return d.BaseURL != "" }

// route — одна строка таблицы регистрации: метод контракта + суффикс пути
// на стороне fleet-сервиса (upstream, без BaseURL) + признак того, что
// contract требует непустого JSON-тела для этой операции.
type route struct {
	method       string
	contractPath string
	upstream     string
	needsBody    bool
}

// cloudRuntimeRoutes — все одиннадцать операций тега CloudRuntime
// (docs/50-api-contract.md §6). GET-маршруты без тела; create/delete/
// start/stop/reboot/status/exec несут instance_id (и, для exec, command) в
// теле — contract явно требует непустого/валидного JSON для них.
var cloudRuntimeRoutes = []route{
	{http.MethodGet, "/api/cloud-runtime", "/", false},
	{http.MethodGet, "/api/cloud-runtime/healthz", "/healthz", false},
	{http.MethodGet, "/api/cloud-runtime/readyz", "/readyz", false},
	{http.MethodGet, "/api/cloud-runtime/nodes", "/nodes", false},
	{http.MethodPost, "/api/cloud-runtime/nodes", "/nodes", true},
	{http.MethodDelete, "/api/cloud-runtime/nodes", "/nodes", true},
	{http.MethodPost, "/api/cloud-runtime/nodes/start", "/nodes/start", true},
	{http.MethodPost, "/api/cloud-runtime/nodes/stop", "/nodes/stop", true},
	{http.MethodPost, "/api/cloud-runtime/nodes/reboot", "/nodes/reboot", true},
	{http.MethodPost, "/api/cloud-runtime/nodes/status", "/nodes/status", true},
	{http.MethodPost, "/api/cloud-runtime/nodes/exec", "/nodes/exec", true},
}

// Register занимает все маршруты cloudRuntimeRoutes на общем httpapi.Router.
func Register(router *httpapi.Router, deps *Deps) {
	for _, rt := range cloudRuntimeRoutes {
		router.Handle(rt.method, rt.contractPath, deps.proxy(rt.upstream, rt.needsBody))
	}
}
