package app

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// RegisterConfig регистрирует GET /api/config (components/schemas/AppConfig).
func RegisterConfig(router *httpapi.Router, d *Deps) {
	router.Handle(http.MethodGet, "/api/config", d.handleGetPublicConfig)
}

// publicConfig строит components/schemas/AppConfig для этого деплоя —
// значения читаются из config.Config там, где контракт называет конкретную
// переменную окружения (docs/50-api-contract.md, «Приложение»); поля без
// такой переменной остаются на своём прежнем месте (T-029).
func (d *Deps) publicConfig() map[string]any {
	emailTransport := "logger"
	if p := d.Config.MailProvider(); p != "" {
		emailTransport = p
	}
	skillSources := d.Config.SkillSources
	if skillSources == nil {
		skillSources = []string{}
	}
	allowedProviders := d.Config.AllowedProviders
	if allowedProviders == nil {
		allowedProviders = []string{}
	}
	imageHosts := d.Config.ImageHosts
	if imageHosts == nil {
		imageHosts = []string{}
	}
	deploymentHosts := map[string]string{}
	for k, v := range map[string]string{
		"jira":        d.Config.DeploymentJiraURL,
		"confluence":  d.Config.DeploymentConfluenceURL,
		"ews":         d.Config.DeploymentEWSURL,
		"mail_domain": d.Config.DeploymentMailDomain,
		"bitrix24":    d.Config.DeploymentBitrix24URL,
		"mcp_gateway": d.Config.DeploymentMcpGatewayURL,
	} {
		if v != "" {
			deploymentHosts[k] = v
		}
	}
	out := map[string]any{
		"allow_signup":                d.Config.AllowSignup,
		"workspace_creation_disabled": d.Config.DisableWorkspaceCreation,
		"server_version":              d.Config.ServerVersion,
		"delivery_profile":            d.Config.DeliveryProfile,
		"vcs_integration_available":   d.Config.VCSIntegrationEnabled && d.Config.VCSSecretKey != "",
		"email_transport":             emailTransport,
		"feature_flags":               map[string]bool{},
		"skill_sources":               skillSources,
		"allowed_providers":           allowedProviders,
		"image_hosts":                 imageHosts,
		"deployment_hosts":            deploymentHosts,
	}
	// components/schemas/AppConfig.external_images: enum ["block","allowlist"]
	// — "allow" (без ограничений, дефолт GOOSAR_EXTERNAL_IMAGES) не входит в
	// перечисление; поле необязательное, так что этот режим отражается его
	// отсутствием, а не значением вне enum (server2/docs/decisions.md,
	// «Пробелы спецификации»).
	if d.Config.ExternalImages == "block" || d.Config.ExternalImages == "allowlist" {
		out["external_images"] = d.Config.ExternalImages
	}
	return out
}

func (d *Deps) handleGetPublicConfig(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteJSON(w, http.StatusOK, d.publicConfig())
}
