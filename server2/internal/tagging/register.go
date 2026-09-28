package tagging

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует операции тегов Labels, Properties, IssueLabels и
// IssueProperties.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/labels", deps.handleListLabels)
	router.Handle(http.MethodPost, "/api/labels", deps.handleCreateLabel)
	router.Handle(http.MethodGet, "/api/labels/{id}", deps.handleGetLabel)
	router.Handle(http.MethodPut, "/api/labels/{id}", deps.handleUpdateLabel)
	router.Handle(http.MethodDelete, "/api/labels/{id}", deps.handleDeleteLabel)

	router.Handle(http.MethodGet, "/api/issues/{id}/labels", deps.handleListIssueLabels)
	router.Handle(http.MethodPost, "/api/issues/{id}/labels", deps.handleAttachIssueLabel)
	router.Handle(http.MethodDelete, "/api/issues/{id}/labels/{labelId}", deps.handleDetachIssueLabel)

	router.Handle(http.MethodGet, "/api/properties", deps.handleListProperties)
	router.Handle(http.MethodPost, "/api/properties", deps.handleCreateProperty)
	router.Handle(http.MethodGet, "/api/properties/{id}", deps.handleGetProperty)
	router.Handle(http.MethodPatch, "/api/properties/{id}", deps.handleUpdateProperty)

	router.Handle(http.MethodPut, "/api/issues/{id}/properties/{propertyId}", deps.handleSetIssuePropertyValue)
	router.Handle(http.MethodDelete, "/api/issues/{id}/properties/{propertyId}", deps.handleDeleteIssuePropertyValue)
}
