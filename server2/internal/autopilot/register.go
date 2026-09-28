package autopilot

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует операции тегов Autopilots/AutopilotTriggers/
// AutopilotRuns/AutopilotDeliveries и публичный webhook-приёмник (contract
// §3.6/§7). Не регистрирует `GET /api/daemon/autopilot-runs/{runId}/gc-check`
// (тег Daemon, daemonAuth) — это домен daemon-протокола (T-028, пакет
// соседнего реализатора), см. server2/docs/decisions.md.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/autopilots", deps.handleListAutopilots)
	router.Handle(http.MethodPost, "/api/autopilots", deps.handleCreateAutopilot)
	router.Handle(http.MethodGet, "/api/autopilots/cron-preview", deps.handleCronPreview)
	router.Handle(http.MethodGet, "/api/autopilots/{id}", deps.handleGetAutopilot)
	router.Handle(http.MethodPatch, "/api/autopilots/{id}", deps.handleUpdateAutopilot)
	router.Handle(http.MethodDelete, "/api/autopilots/{id}", deps.handleDeleteAutopilot)
	router.Handle(http.MethodPost, "/api/autopilots/{id}/trigger", deps.handleTriggerAutopilot)

	router.Handle(http.MethodGet, "/api/autopilots/{id}/runs", deps.handleListRuns)
	router.Handle(http.MethodGet, "/api/autopilots/{id}/runs/{runId}", deps.handleGetRun)

	router.Handle(http.MethodGet, "/api/autopilots/{id}/deliveries", deps.handleListDeliveries)
	router.Handle(http.MethodGet, "/api/autopilots/{id}/deliveries/{deliveryId}", deps.handleGetDelivery)
	router.Handle(http.MethodPost, "/api/autopilots/{id}/deliveries/{deliveryId}/replay", deps.handleReplayDelivery)

	router.Handle(http.MethodPost, "/api/autopilots/{id}/triggers", deps.handleCreateTrigger)
	router.Handle(http.MethodPatch, "/api/autopilots/{id}/triggers/{triggerId}", deps.handleUpdateTrigger)
	router.Handle(http.MethodDelete, "/api/autopilots/{id}/triggers/{triggerId}", deps.handleDeleteTrigger)
	router.Handle(http.MethodPost, "/api/autopilots/{id}/triggers/{triggerId}/rotate-webhook-token", deps.handleRotateWebhookToken)
	router.Handle(http.MethodPut, "/api/autopilots/{id}/triggers/{triggerId}/signing-secret", deps.handleSetSigningSecret)

	router.Handle(http.MethodPost, "/api/autopilots/{id}/collaborators", deps.handleAddCollaborator)
	router.Handle(http.MethodDelete, "/api/autopilots/{id}/collaborators/{userId}", deps.handleRemoveCollaborator)

	router.Handle(http.MethodPost, "/api/webhooks/autopilots/{token}", deps.handleWebhookAutopilotTrigger)
}
