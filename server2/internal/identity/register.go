package identity

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// Register регистрирует операции тега Me плюс /api/tokens (Tokens) и
// /api/workspace-templates.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodGet, "/api/me", deps.handleGetProfile)
	router.Handle(http.MethodPatch, "/api/me", deps.handleUpdateProfile)

	router.Handle(http.MethodPatch, "/api/me/onboarding", deps.handleUpdateOnboardingQuestionnaire)
	router.Handle(http.MethodPost, "/api/me/onboarding/complete", deps.handleCompleteOnboarding)
	router.Handle(http.MethodPost, "/api/me/onboarding/cloud-waitlist", deps.handleJoinCloudWaitlist)
	// GET /api/me/export — было временным 501 здесь (T-026, decisions.md
	// "требует вложений/агентов/задач, которых ещё нет"); T-029
	// (internal/export) теперь реализует его по-настоящему и сам
	// регистрирует этот путь — оставлять его здесь как router.Handle
	// (modeClaim) означало бы панику "маршрут уже зарегистрирован" при
	// сборке router'а. Правка T-029: удалена одна строка, ничего другого в
	// этом домене не менялось.
	router.Handle(http.MethodPost, "/api/me/onboarding/runtime-bootstrap", deps.notImplemented)
	router.Handle(http.MethodPost, "/api/me/onboarding/no-runtime-bootstrap", deps.notImplemented)

	router.Handle(http.MethodPost, "/api/cli-token", deps.handleIssueCliToken)

	router.Handle(http.MethodGet, "/api/tokens", deps.handleListPATs)
	router.Handle(http.MethodPost, "/api/tokens", deps.handleCreatePAT)
	router.Handle(http.MethodPost, "/api/tokens/current/renew", deps.handleRenewCurrentPAT)
	router.Handle(http.MethodDelete, "/api/tokens/{id}", deps.handleRevokePAT)

	router.Handle(http.MethodGet, "/api/workspace-templates", deps.handleListWorkspaceTemplates)
}
