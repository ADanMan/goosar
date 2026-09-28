package daemon

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует весь daemon-протокол (contract §2.2/§3.7).
func Register(router *httpapi.Router, d *Deps) {
	router.Handle(http.MethodPost, "/api/daemon/register", d.handleRegister)
	router.Handle(http.MethodPost, "/api/daemon/deregister", d.handleDeregister)
	router.Handle(http.MethodPost, "/api/daemon/heartbeat", d.handleHeartbeat)
	router.Handle(http.MethodGet, "/api/daemon/ws", d.handleWS)
	router.Handle(http.MethodGet, "/api/daemon/workspaces", d.handleListWorkspaces)
	router.Handle(http.MethodGet, "/api/daemon/workspaces/{workspaceId}/repos", d.handleGetWorkspaceRepos)
	router.Handle(http.MethodGet, "/api/daemon/workspaces/{workspaceId}/runtime-profiles", d.handleListRuntimeProfiles)

	router.Handle(http.MethodPost, "/api/daemon/runtimes/{runtimeId}/tasks/claim", d.handleClaimForRuntime)
	router.Handle(http.MethodPost, "/api/daemon/tasks/claim", d.handleClaimBatch)
	router.Handle(http.MethodPost, "/api/daemon/claim", d.handleClaimBatch)
	router.Handle(http.MethodPost, "/api/daemon/runtimes/{runtimeId}/tasks/{taskId}/prepare-lease", d.handlePrepareLease)
	router.Handle(http.MethodPost, "/api/daemon/runtimes/{runtimeId}/tasks/{taskId}/skill-bundles/resolve", d.handleResolveSkillBundles)
	router.Handle(http.MethodGet, "/api/daemon/runtimes/{runtimeId}/tasks/pending", d.handlePendingByRuntime)
	router.Handle(http.MethodPost, "/api/daemon/runtimes/{runtimeId}/recover-orphans", d.handleRecoverOrphans)

	router.Handle(http.MethodPost, "/api/daemon/runtimes/{runtimeId}/update/{updateId}/result", d.handleReportUpdateResult)
	router.Handle(http.MethodPost, "/api/daemon/runtimes/{runtimeId}/models/{requestId}/result", d.handleReportModelListResult)
	router.Handle(http.MethodPost, "/api/daemon/runtimes/{runtimeId}/local-skills/{requestId}/result", d.handleReportLocalSkillsResult)
	router.Handle(http.MethodPost, "/api/daemon/runtimes/{runtimeId}/local-skills/import/{requestId}/result", d.handleReportLocalSkillImportResult)

	router.Handle(http.MethodGet, "/api/daemon/tasks/{taskId}/status", d.handleTaskStatus)
	router.Handle(http.MethodPost, "/api/daemon/tasks/{taskId}/start", d.handleTaskStart)
	router.Handle(http.MethodPost, "/api/daemon/tasks/{taskId}/wait-local-directory", d.handleTaskWaitLocalDirectory)
	router.Handle(http.MethodPost, "/api/daemon/tasks/{taskId}/progress", d.handleTaskProgress)
	router.Handle(http.MethodPost, "/api/daemon/tasks/{taskId}/complete", d.handleTaskComplete)
	router.Handle(http.MethodPost, "/api/daemon/tasks/{taskId}/fail", d.handleTaskFail)
	router.Handle(http.MethodPost, "/api/daemon/tasks/{taskId}/usage", d.handleTaskUsage)
	router.Handle(http.MethodPost, "/api/daemon/tasks/{taskId}/messages", d.handleTaskMessagesPost)
	router.Handle(http.MethodGet, "/api/daemon/tasks/{taskId}/messages", d.handleTaskMessagesGet)
	router.Handle(http.MethodPost, "/api/daemon/tasks/{taskId}/cancel-ack", d.handleTaskCancelAck)
	router.Handle(http.MethodPost, "/api/daemon/tasks/{taskId}/session", d.handleTaskSession)

	router.Handle(http.MethodPost, "/api/daemon/workspaces/{workspaceId}/issues/gc-check", d.handleBatchIssueGcCheck)
	router.Handle(http.MethodGet, "/api/daemon/issues/{issueId}/gc-check", d.handleIssueGcCheck)
	router.Handle(http.MethodGet, "/api/daemon/chat-sessions/{sessionId}/gc-check", d.handleChatSessionGcCheck)
	router.Handle(http.MethodGet, "/api/daemon/autopilot-runs/{runId}/gc-check", d.handleAutopilotRunGcCheck)
	router.Handle(http.MethodGet, "/api/daemon/tasks/{taskId}/gc-check", d.handleTaskGcCheck)
}
