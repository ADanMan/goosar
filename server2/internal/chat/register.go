package chat

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует операции тега Chat.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodPost, "/api/chat/sessions", deps.handleCreateSession)
	router.Handle(http.MethodGet, "/api/chat/sessions", deps.handleListSessions)

	router.Handle(http.MethodGet, "/api/chat/sessions/{sessionId}", deps.handleGetSession)
	router.Handle(http.MethodPatch, "/api/chat/sessions/{sessionId}", deps.handleUpdateSession)
	router.Handle(http.MethodDelete, "/api/chat/sessions/{sessionId}", deps.handleDeleteSession)

	router.Handle(http.MethodPatch, "/api/chat/sessions/{sessionId}/pin", deps.handleSetPinned)
	router.Handle(http.MethodPatch, "/api/chat/sessions/{sessionId}/archive", deps.handleSetArchived)

	router.Handle(http.MethodPost, "/api/chat/sessions/{sessionId}/messages", deps.handleSendMessage)
	router.Handle(http.MethodGet, "/api/chat/sessions/{sessionId}/messages", deps.handleListMessages)
	router.Handle(http.MethodGet, "/api/chat/sessions/{sessionId}/messages/page", deps.handleListMessagesPage)

	router.Handle(http.MethodGet, "/api/chat/sessions/{sessionId}/pending-task", deps.handleGetPendingTask)
	router.Handle(http.MethodPost, "/api/chat/sessions/{sessionId}/read", deps.handleMarkSessionRead)

	router.Handle(http.MethodGet, "/api/chat/sessions/{sessionId}/draft-restores", deps.handleListDraftRestores)
	router.Handle(http.MethodDelete, "/api/chat/sessions/{sessionId}/draft-restores/{restoreId}", deps.handleConsumeDraftRestore)

	router.Handle(http.MethodGet, "/api/chat/pending-tasks", deps.handleListPendingTasks)
	router.Handle(http.MethodGet, "/api/chat/pending-tasks/has-any", deps.handleHasPendingTasks)

	router.Handle(http.MethodGet, "/api/chat/pinned-agents", deps.handleListPinnedAgents)
	router.Handle(http.MethodPost, "/api/chat/pinned-agents", deps.handlePinAgent)
	router.Handle(http.MethodDelete, "/api/chat/pinned-agents/{agentId}", deps.handleUnpinAgent)

	router.Handle(http.MethodGet, "/api/chat/history", deps.channelHistory)
	router.Handle(http.MethodGet, "/api/chat/thread", deps.channelHistory)
}
