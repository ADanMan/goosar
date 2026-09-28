package skill

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует тег Skills контракта (`/api/skills/**`).
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/skills", deps.handleList)
	router.Handle(http.MethodPost, "/api/skills", deps.handleCreate)
	router.Handle(http.MethodGet, "/api/skills/search", deps.handleSearch)
	router.Handle(http.MethodPost, "/api/skills/import", deps.handleImport)
	router.Handle(http.MethodGet, "/api/skills/{id}", deps.handleGet)
	router.Handle(http.MethodPut, "/api/skills/{id}", deps.handleUpdate)
	router.Handle(http.MethodDelete, "/api/skills/{id}", deps.handleDelete)

	router.Handle(http.MethodGet, "/api/skills/{id}/labels", deps.handleListLabels)
	router.Handle(http.MethodPost, "/api/skills/{id}/labels", deps.handleAttachLabel)
	router.Handle(http.MethodDelete, "/api/skills/{id}/labels/{labelId}", deps.handleDetachLabel)

	router.Handle(http.MethodGet, "/api/skills/{id}/files", deps.handleListFiles)
	router.Handle(http.MethodPut, "/api/skills/{id}/files", deps.handleUpsertFile)
	router.Handle(http.MethodDelete, "/api/skills/{id}/files/{fileId}", deps.handleDeleteFile)
}
