package agent

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует тег Agents/AgentSkills/AgentMcpServers контракта
// (`/api/agents/**`). `/api/agents/from-template` использует
// internal/agenttemplate (каталог шаблонов), но остаётся маршрутом этого
// домена (contract §10.7 перечисляет его в разделе Agents).
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/agents", deps.handleListAgents)
	router.Handle(http.MethodPost, "/api/agents", deps.handleCreateAgent)
	router.Handle(http.MethodPost, "/api/agents/from-template", deps.handleCreateFromTemplate)
	router.Handle(http.MethodGet, "/api/agents/{id}", deps.handleGetAgent)
	router.Handle(http.MethodPut, "/api/agents/{id}", deps.handleUpdateAgent)
	router.Handle(http.MethodPost, "/api/agents/{id}/archive", deps.handleArchiveAgent)
	router.Handle(http.MethodPost, "/api/agents/{id}/restore", deps.handleRestoreAgent)
	router.Handle(http.MethodPost, "/api/agents/{id}/cancel-tasks", deps.handleCancelAgentTasks)
	router.Handle(http.MethodGet, "/api/agents/{id}/tasks", deps.handleListAgentTasks)

	router.Handle(http.MethodGet, "/api/agents/{id}/skills", deps.handleListAgentSkills)
	router.Handle(http.MethodPut, "/api/agents/{id}/skills", deps.handleSetAgentSkills)
	router.Handle(http.MethodPost, "/api/agents/{id}/skills/add", deps.handleAddAgentSkills)
	router.Handle(http.MethodPut, "/api/agents/{id}/skills/{skillId}/enabled", deps.handleSetAgentSkillEnabled)
	router.Handle(http.MethodDelete, "/api/agents/{id}/skills/{skillId}", deps.handleRemoveAgentSkill)
	router.Handle(http.MethodPut, "/api/agents/{id}/runtime-skills/enabled", deps.handleSetAgentRuntimeSkillEnabled)

	router.Handle(http.MethodGet, "/api/agents/{id}/labels", deps.handleListAgentLabels)
	router.Handle(http.MethodPost, "/api/agents/{id}/labels", deps.handleAttachAgentLabel)
	router.Handle(http.MethodDelete, "/api/agents/{id}/labels/{labelId}", deps.handleDetachAgentLabel)

	router.Handle(http.MethodGet, "/api/agents/{id}/env", deps.handleGetAgentEnv)
	router.Handle(http.MethodPut, "/api/agents/{id}/env", deps.handleUpdateAgentEnv)

	router.Handle(http.MethodGet, "/api/agents/{id}/mcp-servers", deps.handleListAgentMcpServers)
	router.Handle(http.MethodPost, "/api/agents/{id}/mcp-servers", deps.handleAddAgentMcpServer)
	router.Handle(http.MethodPut, "/api/agents/{id}/mcp-servers/{serverId}/enabled", deps.handleSetAgentMcpServerEnabled)
	router.Handle(http.MethodDelete, "/api/agents/{id}/mcp-servers/{serverId}", deps.handleRemoveAgentMcpServer)
}
