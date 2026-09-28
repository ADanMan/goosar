package runtime

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует тег Runtimes, четыре read-only агрегата активности
// агентов воркспейса и отмену задачи пользователем (см. deps.go за то,
// почему последние два оказались в этом пакете).
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/runtimes", deps.handleList)
	router.Handle(http.MethodPatch, "/api/runtimes/{runtimeId}", deps.handleUpdate)
	router.Handle(http.MethodDelete, "/api/runtimes/{runtimeId}", deps.handleDelete)
	router.Handle(http.MethodPost, "/api/runtimes/{runtimeId}/mcp-verified", deps.handleMcpVerified)
	router.Handle(http.MethodGet, "/api/runtimes/{runtimeId}/usage", deps.handleUsage)
	router.Handle(http.MethodGet, "/api/runtimes/{runtimeId}/usage/by-agent", deps.handleUsageByAgent)
	router.Handle(http.MethodGet, "/api/runtimes/{runtimeId}/usage/by-hour", deps.handleUsageByHour)
	router.Handle(http.MethodGet, "/api/runtimes/{runtimeId}/activity", deps.handleActivity)
	router.Handle(http.MethodPost, "/api/runtimes/{runtimeId}/update", deps.handleInitiateUpdate)
	router.Handle(http.MethodGet, "/api/runtimes/{runtimeId}/update/{updateId}", deps.handleGetUpdate)
	router.Handle(http.MethodPost, "/api/runtimes/{runtimeId}/models", deps.handleInitiateModelList)
	router.Handle(http.MethodGet, "/api/runtimes/{runtimeId}/models/{requestId}", deps.handleGetModelList)
	router.Handle(http.MethodPost, "/api/runtimes/{runtimeId}/local-skills", deps.handleInitiateLocalSkills)
	router.Handle(http.MethodGet, "/api/runtimes/{runtimeId}/local-skills/{requestId}", deps.handleGetLocalSkills)
	router.Handle(http.MethodPost, "/api/runtimes/{runtimeId}/local-skills/import", deps.handleInitiateLocalSkillImport)
	router.Handle(http.MethodGet, "/api/runtimes/{runtimeId}/local-skills/import/{requestId}", deps.handleGetLocalSkillImport)
	router.Handle(http.MethodPost, "/api/runtimes/{runtimeId}/archive-agents-and-delete", deps.handleArchiveAgentsAndDelete)

	router.Handle(http.MethodGet, "/api/agent-task-snapshot", deps.handleAgentTaskSnapshot)
	router.Handle(http.MethodGet, "/api/working-agents", deps.handleWorkingAgents)
	router.Handle(http.MethodGet, "/api/agent-activity-30d", deps.handleAgentActivity30d)
	router.Handle(http.MethodGet, "/api/agent-run-counts", deps.handleAgentRunCounts)
	router.Handle(http.MethodPost, "/api/tasks/{taskId}/cancel", deps.handleCancelTaskByUser)
}
