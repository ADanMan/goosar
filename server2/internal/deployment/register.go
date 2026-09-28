package deployment

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register закрепляет за router все маршруты раздела «администрирование
// деплоя» (docs/50-api-contract.md §7 и последующие одиночные ручки).
func Register(router *httpapi.Router, d *Deps) {
	// deployment-admin: роли, заявки, аудит.
	router.Handle(http.MethodGet, "/api/deployment/admins", d.handleListAdmins)
	router.Handle(http.MethodPost, "/api/deployment/admins", d.handleRequestGrant)
	router.Handle(http.MethodGet, "/api/deployment/admins/pending", d.handleListPendingRequests)
	router.Handle(http.MethodDelete, "/api/deployment/admins/{userId}", d.handleRequestRevoke)
	router.Handle(http.MethodGet, "/api/deployment/audit", d.handleListAudit)

	// пользователи на уровне деплоя.
	router.Handle(http.MethodPost, "/api/deployment/users/{userId}/deactivate", d.handleDeactivateUser)
	router.Handle(http.MethodPost, "/api/deployment/users/{userId}/reactivate", d.handleReactivateUser)
	router.Handle(http.MethodPost, "/api/deployment/users/{userId}/revoke-sessions", d.handleRevokeUserSessions)
	router.Handle(http.MethodDelete, "/api/deployment/users/{userId}", d.handleDeleteUser)

	// mcp-серверы деплоя (библиотека) и их видимость воркспейсу.
	router.Handle(http.MethodGet, "/api/deployment/mcp-servers", d.handleListPlatformMcpServers)
	router.Handle(http.MethodPost, "/api/deployment/mcp-servers", d.handleCreatePlatformMcpServer)
	router.Handle(http.MethodPut, "/api/deployment/mcp-servers/{serverId}", d.handleUpdatePlatformMcpServer)
	router.Handle(http.MethodDelete, "/api/deployment/mcp-servers/{serverId}", d.handleDeletePlatformMcpServer)
	router.Handle(http.MethodGet, "/api/deployment-mcp-servers", d.handleListWorkspaceDeploymentMcpServers)
	router.Handle(http.MethodPut, "/api/deployment-mcp-servers/{serverId}/enabled", d.handleSetWorkspaceDeploymentMcpServerEnabled)

	// политика деплоя.
	router.Handle(http.MethodGet, "/api/deployment/policy", d.handleGetPolicyAdmin)
	router.Handle(http.MethodPut, "/api/deployment/policy", d.handlePutPolicy)
	router.Handle(http.MethodGet, "/api/deployment-policy", d.handleGetPolicyMember)

	// воркспейсы деплоя, join-targets, fleet.
	router.Handle(http.MethodGet, "/api/deployment/workspaces", d.handleListDeploymentWorkspaces)
	router.Handle(http.MethodGet, "/api/deployment/join-targets", d.handleListJoinTargets)
	router.Handle(http.MethodPost, "/api/deployment/join-targets/{workspaceId}/join", d.handleJoinTargetWorkspace)
	router.Handle(http.MethodGet, "/api/deployment/fleet", d.handleListFleet)
	router.Handle(http.MethodPatch, "/api/deployment/workspaces/{workspaceId}", d.handleSetWorkspaceOpenJoin)
	router.Handle(http.MethodGet, "/api/deployment/workspaces/{workspaceId}/members", d.handleListDeploymentWorkspaceMembers)

	// слой конфигурации воркспейса — вход по заголовку/query.
	router.Handle(http.MethodGet, "/api/workspace-config", d.handleGetWorkspaceConfig)
	router.Handle(http.MethodPut, "/api/workspace-config", d.handlePutWorkspaceConfig)
	router.Handle(http.MethodGet, "/api/workspace-config/overrides/{userId}", d.handleGetWorkspaceUserOverride)
	router.Handle(http.MethodPut, "/api/workspace-config/overrides/{userId}", d.handlePutWorkspaceUserOverride)
	router.Handle(http.MethodDelete, "/api/workspace-config/overrides/{userId}", d.handleDeleteWorkspaceUserOverride)

	// слой конфигурации воркспейса — административный вход по {workspaceId}.
	router.Handle(http.MethodGet, "/api/deployment/workspaces/{workspaceId}/config", d.handleGetWorkspaceConfigByID)
	router.Handle(http.MethodPut, "/api/deployment/workspaces/{workspaceId}/config", d.handlePutWorkspaceConfigByID)
	router.Handle(http.MethodGet, "/api/deployment/workspaces/{workspaceId}/config/overrides", d.handleListWorkspaceConfigOverridesByID)
	router.Handle(http.MethodGet, "/api/deployment/workspaces/{workspaceId}/config/overrides/{userId}", d.handleGetWorkspaceUserOverrideByID)
	router.Handle(http.MethodPut, "/api/deployment/workspaces/{workspaceId}/config/overrides/{userId}", d.handlePutWorkspaceUserOverrideByID)
	router.Handle(http.MethodDelete, "/api/deployment/workspaces/{workspaceId}/config/overrides/{userId}", d.handleDeleteWorkspaceUserOverrideByID)

	// mcp-серверы воркспейса (собственные + самообслуживание credential-полей).
	router.Handle(http.MethodGet, "/api/workspace-mcp-servers", d.handleListWorkspaceMcpServers)
	router.Handle(http.MethodPost, "/api/workspace-mcp-servers", d.handleCreateWorkspaceMcpServer)
	router.Handle(http.MethodPut, "/api/workspace-mcp-servers/{serverId}", d.handleUpdateWorkspaceMcpServer)
	router.Handle(http.MethodDelete, "/api/workspace-mcp-servers/{serverId}", d.handleDeleteWorkspaceMcpServer)
	router.Handle(http.MethodPut, "/api/workspace-mcp-servers/{serverId}/credentials", d.handleSetWorkspaceMcpCredentials)
	router.Handle(http.MethodDelete, "/api/workspace-mcp-servers/{serverId}/credentials", d.handleDeleteWorkspaceMcpCredentials)

	// provisioning.
	router.Handle(http.MethodGet, "/api/provisioning/manifest", d.handleGetProvisioningManifest)
	router.Handle(http.MethodGet, "/api/provisioning/blob/{name}/{version}", d.handleGetProvisioningBlob)
	router.Handle(http.MethodGet, "/api/provisioning/catalog", d.handleGetProvisioningCatalog)
	router.Handle(http.MethodGet, "/api/provisioning/pins", d.handleGetProvisioningPins)
	router.Handle(http.MethodPut, "/api/provisioning/pins", d.handlePutProvisioningPins)

	// одиночные ручки.
	router.Handle(http.MethodGet, "/api/effective-config", d.handleGetEffectiveConfig)
	router.Handle(http.MethodGet, "/api/deployment/client-secrets", d.handleGetClientSecrets)
	router.Handle(http.MethodGet, "/api/llm/health", d.handleGetLlmHealth)
}
