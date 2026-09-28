package workspace

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует операции тегов Workspaces, RuntimeProfiles и Invitations.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/workspaces", deps.handleListWorkspaces)
	router.Handle(http.MethodPost, "/api/workspaces", deps.handleCreateWorkspace)

	router.Handle(http.MethodGet, "/api/workspaces/{id}", deps.handleGetWorkspace)
	router.Handle(http.MethodPut, "/api/workspaces/{id}", deps.handleUpdateWorkspace)
	router.Handle(http.MethodPatch, "/api/workspaces/{id}", deps.handleUpdateWorkspace)
	router.Handle(http.MethodDelete, "/api/workspaces/{id}", deps.handleDeleteWorkspace)

	router.Handle(http.MethodGet, "/api/workspaces/{id}/capabilities", deps.handleGetCapabilities)

	router.Handle(http.MethodGet, "/api/workspaces/{id}/members", deps.handleListMembers)
	router.Handle(http.MethodPost, "/api/workspaces/{id}/members", deps.handleInviteMember)
	router.Handle(http.MethodPatch, "/api/workspaces/{id}/members/{memberId}", deps.handleUpdateMember)
	router.Handle(http.MethodDelete, "/api/workspaces/{id}/members/{memberId}", deps.handleRemoveMember)
	router.Handle(http.MethodPost, "/api/workspaces/{id}/leave", deps.handleLeaveWorkspace)

	router.Handle(http.MethodGet, "/api/workspaces/{id}/invitations", deps.handleListWorkspaceInvitations)
	router.Handle(http.MethodDelete, "/api/workspaces/{id}/invitations/{invitationId}", deps.handleRevokeInvitation)

	router.Handle(http.MethodGet, "/api/workspaces/{id}/runtime-profiles", deps.handleListRuntimeProfiles)
	router.Handle(http.MethodPost, "/api/workspaces/{id}/runtime-profiles", deps.handleCreateRuntimeProfile)
	router.Handle(http.MethodGet, "/api/workspaces/{id}/runtime-profiles/{profileId}", deps.handleGetRuntimeProfile)
	router.Handle(http.MethodPatch, "/api/workspaces/{id}/runtime-profiles/{profileId}", deps.handleUpdateRuntimeProfile)
	router.Handle(http.MethodPut, "/api/workspaces/{id}/runtime-profiles/{profileId}", deps.handleUpdateRuntimeProfile)
	router.Handle(http.MethodDelete, "/api/workspaces/{id}/runtime-profiles/{profileId}", deps.handleDeleteRuntimeProfile)

	// GitHub/VCS/Slack/export/composio под тегом Workspaces — вне объёма
	// T-026 (интеграции — T-029), остаются заглушками через genstubs.

	router.Handle(http.MethodGet, "/api/invitations", deps.handleListMyInvitations)
	router.Handle(http.MethodGet, "/api/invitations/{id}", deps.handleGetMyInvitation)
	router.Handle(http.MethodPost, "/api/invitations/{id}/accept", deps.handleAcceptInvitation)
	router.Handle(http.MethodPost, "/api/invitations/{id}/decline", deps.handleDeclineInvitation)
}
