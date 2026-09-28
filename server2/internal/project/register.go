package project

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует операции тегов Projects и ProjectResources.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/projects/search", deps.handleSearchProjects)
	router.Handle(http.MethodGet, "/api/projects", deps.handleListProjects)
	router.Handle(http.MethodPost, "/api/projects", deps.handleCreateProject)

	router.Handle(http.MethodGet, "/api/projects/{id}", deps.handleGetProject)
	router.Handle(http.MethodPut, "/api/projects/{id}", deps.handleUpdateProject)
	router.Handle(http.MethodDelete, "/api/projects/{id}", deps.handleDeleteProject)

	router.Handle(http.MethodGet, "/api/projects/{id}/resources", deps.handleListProjectResources)
	router.Handle(http.MethodPost, "/api/projects/{id}/resources", deps.handleCreateProjectResource)
	router.Handle(http.MethodPut, "/api/projects/{id}/resources/{resourceId}", deps.handleUpdateProjectResource)
	router.Handle(http.MethodDelete, "/api/projects/{id}/resources/{resourceId}", deps.handleDeleteProjectResource)
}
