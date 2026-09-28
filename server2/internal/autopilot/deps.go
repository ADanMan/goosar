package autopilot

import (
	"log/slog"
	"net/http"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/task"
	"github.com/adanman/goosar/server2/internal/workspace"
	"github.com/adanman/goosar/server2/internal/wsctx"
)

// Deps — зависимости домена autopilot.
type Deps struct {
	Store      *Store
	Dispatcher *Dispatcher
	Resolver   *wsctx.Resolver
	Publisher  realtime.Publisher
	Logger     *slog.Logger

	// McpSecretKey — GOOSAR_MCP_SECRET_KEY, нужен только setAutopilotTriggerSigningSecret
	// (запечатывание секрета, см. crypto.go) и проверке подписи входящего
	// вебхука (handlers_webhook.go).
	McpSecretKey string
	// PublicURL — GOOSAR_PUBLIC_URL, для построения webhook_url; пусто —
	// строится из Host входящего запроса (см. handlers_triggers.go).
	PublicURL string

	// Scheduler — фоновый цикл расписаний; поле здесь только для того,
	// чтобы cmd/server мог запустить Scheduler.Run(ctx) в своей горутине
	// (см. server2/cmd/server) — сам Deps его не запускает.
	Scheduler *Scheduler
}

// New собирает Deps домена autopilot. taskStore/wsStore — уже собранные
// Store доменов task/workspace (см. server2/internal/app/deps.go); autopilot
// использует их напрямую (task.Store.CreateIssue, workspace.Store для
// нумерации), а не через их HTTP-обработчики — тот же приём, что
// internal/note использует internal/asset.Store.
func New(db *store.Store, wsStore *workspace.Store, taskStore *task.Store, dispatchDeps *dispatch.Deps,
	pub realtime.Publisher, mcpSecretKey, publicURL string, logger *slog.Logger) *Deps {
	st := NewStore(db)
	disp := &Dispatcher{Store: st, Dispatch: dispatchDeps, Tasks: taskStore, Workspace: wsStore, DB: db}
	sched := &Scheduler{Store: st, Dispatcher: disp, Clock: RealClock{}, Logger: logger}
	return &Deps{
		Store: st, Dispatcher: disp, Resolver: wsctx.New(db), Publisher: pub, Logger: logger,
		McpSecretKey: mcpSecretKey, PublicURL: publicURL, Scheduler: sched,
	}
}

func (d *Deps) publish(workspaceID, eventType string, payload any) {
	if d.Publisher == nil || workspaceID == "" {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{Type: eventType, Payload: payload})
}

// externalOrigin — один кандидат для построения абсолютного публичного URL:
// либо фиксированный (GOOSAR_PUBLIC_URL), либо выведенный из пары
// заголовков схема+хост запроса.
type externalOrigin struct {
	scheme, host string
}

func (o externalOrigin) String() string { return o.scheme + "://" + o.host }

// baseURL строит абсолютный публичный адрес сервера для webhook_url
// (contract §7, AutopilotTrigger.webhook_url): GOOSAR_PUBLIC_URL, если
// оператор деплоя его задал (см. server2/README.md, "переменные окружения"),
// иначе выводится из заголовков конкретного запроса — этой кодовой базе
// негде централизованно узнать свой публичный адрес без него (см.
// server2/docs/decisions.md, раздел T-028).
func (d *Deps) baseURL(r *http.Request) string {
	if d.PublicURL != "" {
		return d.PublicURL
	}
	return requestOrigin(r).String()
}

func requestOrigin(r *http.Request) externalOrigin {
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return externalOrigin{scheme: proto, host: host}
	}
	if r.TLS != nil {
		return externalOrigin{scheme: "https", host: host}
	}
	return externalOrigin{scheme: "http", host: host}
}
