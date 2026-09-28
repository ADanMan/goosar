package feed

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует операции тегов Inbox и NotificationPreferences.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/inbox", deps.handleListInbox)
	router.Handle(http.MethodGet, "/api/inbox/archived", deps.handleListArchivedInbox)
	router.Handle(http.MethodGet, "/api/inbox/unread-count", deps.handleCountUnread)
	router.Handle(http.MethodGet, "/api/inbox/unread-summary", deps.handleUnreadSummary)
	router.Handle(http.MethodPost, "/api/inbox/mark-all-read", deps.handleMarkAllRead)
	router.Handle(http.MethodPost, "/api/inbox/archive-all", deps.handleArchiveAll)
	router.Handle(http.MethodPost, "/api/inbox/archive-all-read", deps.handleArchiveAllRead)
	router.Handle(http.MethodPost, "/api/inbox/archive-completed", deps.handleArchiveCompleted)
	router.Handle(http.MethodPost, "/api/inbox/{id}/read", deps.handleMarkItemRead)
	router.Handle(http.MethodPost, "/api/inbox/{id}/archive", deps.handleArchiveItem)
	router.Handle(http.MethodPost, "/api/inbox/{id}/unarchive", deps.handleUnarchiveItem)

	router.Handle(http.MethodGet, "/api/notification-preferences", deps.handleGetPrefs)
	router.Handle(http.MethodPatch, "/api/notification-preferences", deps.handlePatchPrefs)
	router.Handle(http.MethodPut, "/api/notification-preferences", deps.handlePutPrefs)
}
