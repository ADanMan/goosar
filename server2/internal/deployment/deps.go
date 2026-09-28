package deployment

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// Deps — зависимости домена deployment. Не переиспользует app.Deps напрямую
// (см. server2/docs/adr/0001-stack.md, «Раскладка пакетов») — только то, что
// нужно этому домену: пул БД для собственных таблиц (platform_*,
// space_config(_overrides), space_mcp_servers(_credentials),
// provisioning_pins), workspace.Store для резолва пространств/участников
// (переиспользуется, а не дублируется), authn.Deps для отзыва сессий/PAT и
// bump token epoch, realtime.Hub для закрытия подключений при
// деактивации/удалении пользователя.
type Deps struct {
	DB        *store.Store
	Workspace *workspace.Store
	Authn     *authn.Deps
	Hub       *realtime.Hub
	Config    config.Config
	Logger    *slog.Logger

	httpClient *http.Client

	llmHealth *llmHealthCache
}

func New(db *store.Store, ws *workspace.Store, authnDeps *authn.Deps, hub *realtime.Hub, cfg config.Config, logger *slog.Logger) *Deps {
	return &Deps{
		DB:         db,
		Workspace:  ws,
		Authn:      authnDeps,
		Hub:        hub,
		Config:     cfg,
		Logger:     logger,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		llmHealth:  &llmHealthCache{},
	}
}

// membership — тонкая обёртка над workspace.Store для резолва заголовка/query
// воркспейса в его id (httpapi.WorkspaceMembership.ResolveWorkspaceID), не
// подключая проверку членства (у части ручек этого домена доступ разрешён и
// не-участнику — deployment-admin).
func (d *Deps) resolveWorkspaceID(r *http.Request, actor *httpapi.Actor) (id string, ok bool) {
	ref, isSlug, has := httpapi.ResolveWorkspaceRef(r, actor)
	if !has {
		return "", false
	}
	if isSlug {
		ws, err := d.Workspace.GetWorkspaceBySlug(r.Context(), ref)
		if err != nil {
			return "", false
		}
		return ws.ID, true
	}
	ws, err := d.Workspace.GetWorkspace(r.Context(), ref)
	if err != nil {
		return "", false
	}
	return ws.ID, true
}
