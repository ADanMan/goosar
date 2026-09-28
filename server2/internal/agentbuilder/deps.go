// Package agentbuilder реализует тег AgentBuilder контракта
// (`/api/agent-builder/**`): не отдельная подсистема, а обычный чат с
// предустановленным системным агентом (contract §2 "Конструктор агента").
// createAgentBuilderSession заводит служебного агента (kind=system,
// system_key="agent_builder:<flowId>") и обычную чат-сессию поверх него —
// сам чат (сообщения, стриминг) ведёт internal/chat; этот домен только
// создаёт/переключает runtime сессии-конструктора.
package agentbuilder

import (
	"log/slog"
	"net/http"

	"github.com/adanman/goosar/server2/internal/agent"
	"github.com/adanman/goosar/server2/internal/chat"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// Deps — зависимости домена agentbuilder.
type Deps struct {
	Agent     *agent.Store
	Chat      *chat.Store
	Workspace httpapi.WorkspaceMembership
	DB        *store.Store
	Logger    *slog.Logger
}

func New(db *store.Store, agentStore *agent.Store, chatStore *chat.Store, wsStore *workspace.Store, logger *slog.Logger) *Deps {
	return &Deps{Agent: agentStore, Chat: chatStore, Workspace: wsStore.HTTPAPIMembership(), DB: db, Logger: logger}
}

// Register закрепляет за роутером оба маршрута тега AgentBuilder
// (`/api/agent-builder/**`); держится в этом же файле, а не отдельным
// register.go — домен состоит всего из двух обработчиков, отдельный файл
// на три строки не оправдан.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodPost, "/api/agent-builder/sessions", deps.handleCreateSession)
	router.Handle(http.MethodPatch, "/api/agent-builder/sessions/{sessionId}/runtime", deps.handleSwitchRuntime)
}
