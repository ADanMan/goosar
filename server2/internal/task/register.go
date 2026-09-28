package task

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует тег Issues контракта, кроме комментариев/реакций/
// вложений задачи (пакет note) и /api/labels/**, /api/properties/** как
// самостоятельных ресурсов (см. package doc).
func Register(router *httpapi.Router, deps *Deps) {
	// /api/issues/table/{groups,rows,facets} и /api/issues/children (набор
	// родителей через query) не реализованы в этой сессии (см.
	// server2/docs/decisions.md) — остаются заглушками genstubs, этот пакет
	// их путь не занимает.

	router.Handle(http.MethodGet, "/api/issues/search", deps.handleSearchIssues)
	router.Handle(http.MethodGet, "/api/issues/child-progress", deps.handleChildProgress)
	router.Handle(http.MethodGet, "/api/issues/grouped", deps.handleGroupedIssues)

	router.Handle(http.MethodGet, "/api/issues", deps.handleListIssues)
	router.Handle(http.MethodPost, "/api/issues", deps.handleCreateIssue)
	router.Handle(http.MethodPost, "/api/issues/query", deps.handleQueryIssues)
	router.Handle(http.MethodPost, "/api/issues/quick-create", deps.handleQuickCreate)
	router.Handle(http.MethodPost, "/api/issues/preview-trigger", deps.handlePreviewTrigger)
	router.Handle(http.MethodPost, "/api/issues/batch-update", deps.handleBatchUpdate)
	router.Handle(http.MethodPost, "/api/issues/batch-delete", deps.handleBatchDelete)

	router.Handle(http.MethodGet, "/api/issues/{id}", deps.handleGetIssue)
	router.Handle(http.MethodPut, "/api/issues/{id}", deps.handleUpdateIssue)
	router.Handle(http.MethodPost, "/api/issues/{id}/move", deps.handleMoveIssue)
	router.Handle(http.MethodDelete, "/api/issues/{id}", deps.handleDeleteIssue)

	router.Handle(http.MethodGet, "/api/issues/{id}/timeline", deps.handleTimeline)
	router.Handle(http.MethodGet, "/api/issues/{id}/subscribers", deps.handleListSubscribers)
	router.Handle(http.MethodPost, "/api/issues/{id}/subscribe", deps.handleSubscribeIssue)
	router.Handle(http.MethodPost, "/api/issues/{id}/unsubscribe", deps.handleUnsubscribeIssue)

	router.Handle(http.MethodGet, "/api/issues/{id}/active-task", deps.handleActiveTask)
	router.Handle(http.MethodPost, "/api/issues/{id}/tasks/{taskId}/cancel", deps.handleCancelIssueTask)
	router.Handle(http.MethodPost, "/api/issues/{id}/rerun", deps.handleRerunIssue)
	router.Handle(http.MethodGet, "/api/issues/{id}/task-runs", deps.handleTaskRuns)
	router.Handle(http.MethodGet, "/api/issues/{id}/usage", deps.handleIssueUsage)

	router.Handle(http.MethodGet, "/api/issues/{id}/children", deps.handleChildIssues)

	router.Handle(http.MethodGet, "/api/issues/{id}/metadata", deps.handleListMetadata)
	router.Handle(http.MethodPut, "/api/issues/{id}/metadata/{key}", deps.handleSetMetadataKey)
	router.Handle(http.MethodDelete, "/api/issues/{id}/metadata/{key}", deps.handleDeleteMetadataKey)

	router.Handle(http.MethodGet, "/api/issues/{id}/pull-requests", deps.handlePullRequests)

	router.Handle(http.MethodGet, "/api/tasks/{taskId}/messages", deps.handleTaskMessages)
	router.Handle(http.MethodGet, "/api/assignee-frequency", deps.handleAssigneeFrequency)

	// Вне области этого домена (см. package doc и server2/docs/decisions.md):
	// /api/issues/{id}/comments*, /api/issues/{id}/reactions,
	// /api/issues/{id}/attachments (пакет note); /api/labels/**,
	// /api/properties/** как CRUD определений; /api/issues/{id}/squad-evaluated
	// (требует полной модели daemon/agent-actor, T-028) — остаются заглушками
	// genstubs (RegisterStubs), кроме тех, что этот пакет уже занял выше.
}
