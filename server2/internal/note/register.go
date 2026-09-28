package note

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует операции тегов IssueComments, Comments и
// IssueReactions.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodPost, "/api/issues/{id}/comments/trigger-preview", deps.handleTriggerPreview)
	router.Handle(http.MethodPost, "/api/issues/{id}/comments", deps.handleCreateComment)
	router.Handle(http.MethodGet, "/api/issues/{id}/comments", deps.handleListComments)

	router.Handle(http.MethodPut, "/api/comments/{commentId}", deps.handleUpdateComment)
	router.Handle(http.MethodDelete, "/api/comments/{commentId}", deps.handleDeleteComment)
	router.Handle(http.MethodPost, "/api/comments/{commentId}/resolve", deps.handleResolveComment)
	router.Handle(http.MethodDelete, "/api/comments/{commentId}/resolve", deps.handleUnresolveComment)
	router.Handle(http.MethodPost, "/api/comments/{commentId}/reactions", deps.handleAddCommentReaction)
	router.Handle(http.MethodDelete, "/api/comments/{commentId}/reactions", deps.handleRemoveCommentReaction)

	router.Handle(http.MethodPost, "/api/issues/{id}/reactions", deps.handleAddIssueReaction)
	router.Handle(http.MethodDelete, "/api/issues/{id}/reactions", deps.handleRemoveIssueReaction)
}
