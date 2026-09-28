package squad

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует тег Squads контракта (`/api/squads/**`) плюс
// `/api/issues/{id}/squad-evaluated` (contract §6, tag Squads — оставлен
// незанятым доменом task, см. server2/docs/decisions.md).
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/squads", deps.handleList)
	router.Handle(http.MethodPost, "/api/squads", deps.handleCreate)
	router.Handle(http.MethodGet, "/api/squads/{id}", deps.handleGet)
	router.Handle(http.MethodPut, "/api/squads/{id}", deps.handleUpdate)
	router.Handle(http.MethodDelete, "/api/squads/{id}", deps.handleArchive)
	router.Handle(http.MethodGet, "/api/squads/{id}/members", deps.handleListMembers)
	router.Handle(http.MethodPost, "/api/squads/{id}/members", deps.handleAddMember)
	router.Handle(http.MethodDelete, "/api/squads/{id}/members", deps.handleRemoveMember)
	router.Handle(http.MethodPatch, "/api/squads/{id}/members/role", deps.handleUpdateMemberRole)
	router.Handle(http.MethodGet, "/api/squads/{id}/members/status", deps.handleMemberStatus)

	router.Handle(http.MethodPost, "/api/issues/{id}/squad-evaluated", deps.handleSquadEvaluated)
}
