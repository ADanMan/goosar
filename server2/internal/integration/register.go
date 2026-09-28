// Package integration реализует внешние интеграции воркспейса (T-029,
// docs/50-api-contract.md §1 "Рабочие пространства, интеграции..."):
// GitHub App (`/api/workspaces/{id}/github/**`, `/api/github/setup`,
// `POST /api/webhooks/github`), self-hosted VCS (`/api/workspaces/{id}/vcs/**`,
// `POST /api/webhooks/vcs/{connectionId}`), Slack (`/api/workspaces/{id}/slack/**`,
// `/api/slack/binding/redeem`) и Composio (`/api/integrations/composio/**`).
//
// Внешние API (GitHub/Slack/self-hosted VCS/Composio) недоступны из песочницы
// этой сессии: каждый клиент ходит через net/http.Client с настраиваемым
// базовым URL (пусто — дефолт на реальный внешний хост), чтобы юнит-тесты
// подставляли httptest.Server (см. server2/docs/decisions.md, раздел T-029).
package integration

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
)

// Deps — зависимости домена integration.
type Deps struct {
	Store     *Store
	Publisher realtime.Publisher
	Logger    *slog.Logger

	GitHub   githubClient
	Slack    slackClient
	VCS      vcsClient
	Composio composioClient

	Cfg config.Config
}

// New собирает Deps поверх общего пула БД и конфигурации сервера.
func New(db *store.Store, cfg config.Config, pub realtime.Publisher, logger *slog.Logger) *Deps {
	httpClient := &http.Client{Timeout: 15 * time.Second}
	return &Deps{
		Store:     NewStore(db),
		Publisher: pub,
		Logger:    logger,
		Cfg:       cfg,
		GitHub:    newGitHubClient(httpClient, cfg),
		Slack:     newSlackClient(httpClient, cfg),
		VCS:       newVCSClient(httpClient),
		Composio:  newComposioClient(httpClient, cfg),
	}
}

func (d *Deps) publish(workspaceID, eventType string, payload any) {
	if d.Publisher == nil {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{Type: eventType, Payload: payload})
}

// integrationRoute — одна строка таблицы маршрутов ниже: метод, шаблон
// пути контракта и обработчик.
type integrationRoute struct {
	method  string
	pattern string
	handler http.HandlerFunc
}

// Register занимает маршруты тегов GitHub/VCS/Slack/Composio-интеграций
// (docs/50-api-contract.md §1 "Рабочие пространства, интеграции").
func Register(router *httpapi.Router, deps *Deps) {
	for _, rt := range deps.routeTable() {
		router.Handle(rt.method, rt.pattern, rt.handler)
	}
}

func (deps *Deps) routeTable() []integrationRoute {
	return []integrationRoute{
		// GitHub
		{http.MethodGet, "/api/workspaces/{id}/github/installations", deps.handleListGitHubInstallations},
		{http.MethodGet, "/api/workspaces/{id}/github/connect", deps.handleGetGitHubConnectURL},
		{http.MethodGet, "/api/workspaces/{id}/github/installations/{installationId}/repositories", deps.handleListGitHubInstallationRepositories},
		{http.MethodDelete, "/api/workspaces/{id}/github/installations/{installationId}", deps.handleDeleteGitHubInstallation},
		{http.MethodGet, "/api/github/setup", deps.handleGitHubSetupCallback},
		{http.MethodPost, "/api/webhooks/github", deps.handleGitHubWebhook},

		// VCS
		{http.MethodGet, "/api/workspaces/{id}/vcs/connections", deps.handleListVCSConnections},
		{http.MethodPost, "/api/workspaces/{id}/vcs/connections", deps.handleConnectVCS},
		{http.MethodDelete, "/api/workspaces/{id}/vcs/connections/{connectionId}", deps.handleDeleteVCSConnection},
		{http.MethodPost, "/api/workspaces/{id}/vcs/connections/{connectionId}/rotate-webhook", deps.handleRotateVCSWebhook},
		{http.MethodPost, "/api/webhooks/vcs/{connectionId}", deps.handleVCSWebhook},

		// Slack
		{http.MethodGet, "/api/workspaces/{id}/slack/installations", deps.handleListSlackInstallations},
		{http.MethodDelete, "/api/workspaces/{id}/slack/installations/{installationId}", deps.handleDeleteSlackInstallation},
		{http.MethodPost, "/api/workspaces/{id}/slack/install/byo", deps.handleRegisterSlackBotBYO},
		{http.MethodPost, "/api/slack/binding/redeem", deps.handleRedeemSlackBinding},

		// Composio
		{http.MethodPost, "/api/integrations/composio/connect/init", deps.handleComposioConnectInit},
		{http.MethodGet, "/api/integrations/composio/toolkits", deps.handleListComposioToolkits},
		{http.MethodGet, "/api/integrations/composio/connections", deps.handleListComposioConnections},
		{http.MethodDelete, "/api/integrations/composio/connections/{id}", deps.handleDeleteComposioConnection},
		{http.MethodGet, "/api/integrations/composio/callback", deps.handleComposioCallback},
	}
}
