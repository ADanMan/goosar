package handler

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/featureflags"
	"github.com/adanman/goosar/server/internal/middleware"
	"github.com/adanman/goosar/server/internal/skillsources"
)

type AppConfig struct {
	CdnDomain string `json:"cdn_domain"`

	CdnSigned bool `json:"cdn_signed,omitempty"`

	AllowSignup bool `json:"allow_signup"`

	WorkspaceCreationDisabled bool `json:"workspace_creation_disabled,omitempty"`

	DaemonServerURL string `json:"daemon_server_url,omitempty"`
	DaemonAppURL    string `json:"daemon_app_url,omitempty"`

	VCSIntegrationAvailable bool `json:"vcs_integration_available,omitempty"`

	PosthogKey           string `json:"posthog_key"`
	PosthogHost          string `json:"posthog_host"`
	AnalyticsEnvironment string `json:"analytics_environment"`

	FeatureFlags map[string]bool `json:"feature_flags,omitempty"`

	ServerVersion string `json:"server_version,omitempty"`

	DeliveryProfile string `json:"delivery_profile,omitempty"`

	SkillSources *[]string `json:"skill_sources,omitempty"`

	AllowedProviders []string `json:"allowed_providers,omitempty"`

	ExternalImages string `json:"external_images,omitempty"`

	EmailTransport string `json:"email_transport,omitempty"`

	MinDaemonVersion string `json:"min_daemon_version,omitempty"`

	ImageHosts []string `json:"image_hosts,omitempty"`

	DeploymentHosts map[string]string `json:"deployment_hosts,omitempty"`
}

func deploymentHostsFromEnv() map[string]string {
	fields := map[string]string{
		"jiraUrl":       "GOOSAR_DEPLOYMENT_JIRA_URL",
		"confluenceUrl": "GOOSAR_DEPLOYMENT_CONFLUENCE_URL",
		"ewsUrl":        "GOOSAR_DEPLOYMENT_EWS_URL",
		"mailDomain":    "GOOSAR_DEPLOYMENT_MAIL_DOMAIN",
		"llmApiBase":    "GOOSAR_DEPLOYMENT_LLM_API_BASE",
		"llmModel":      "GOOSAR_DEPLOYMENT_LLM_MODEL",
	}
	hosts := make(map[string]string, len(fields))
	for key, env := range fields {
		if value := strings.TrimSpace(os.Getenv(env)); value != "" {
			hosts[key] = value
		}
	}
	if len(hosts) == 0 {
		return nil
	}
	return hosts
}

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	config := AppConfig{
		AllowSignup:               os.Getenv("ALLOW_SIGNUP") != "false",
		WorkspaceCreationDisabled: os.Getenv("DISABLE_WORKSPACE_CREATION") == "true",
	}
	if h.Storage != nil {
		config.CdnDomain = h.Storage.CdnDomain()
	}
	config.CdnSigned = h.CFSigner != nil
	config.DaemonServerURL, config.DaemonAppURL = daemonSetupURLsFromEnv()
	config.VCSIntegrationAvailable = h.cfg.VCSIntegrationEnabled

	config.DeliveryProfile = h.cfg.DeliveryProfile.PublicValue()

	config.AllowedProviders = h.cfg.AllowedProviders.AllowedList()
	config.FeatureFlags = featureflags.EvaluateFrontendPublicFlags(r.Context(), h.FeatureFlags)

	config.MinDaemonVersion = MinDaemonVersion()
	if h.EmailService != nil {
		config.EmailTransport = h.EmailService.Transport()
	}

	if !isOfficialCloudDeployment() {
		config.ServerVersion = h.cfg.ServerVersion
	}

	if set, err := skillsources.FromEnv(); err != nil {
		empty := []string{}
		config.SkillSources = &empty
	} else if set.Explicit() {
		names := set.EnabledNames()
		config.SkillSources = &names
	}

	config.DeploymentHosts = deploymentHostsFromEnv()

	if mode := middleware.ExternalImagesMode(); mode != "allow" {
		config.ExternalImages = mode
		if mode == "allowlist" {
			config.ImageHosts = middleware.AllowlistedImageHosts()
		}
	}

	if frontendAnalyticsEnabled() && !analyticsDisabled() {
		config.PosthogKey = os.Getenv("POSTHOG_API_KEY")
		config.PosthogHost = os.Getenv("POSTHOG_HOST")
		config.AnalyticsEnvironment = analytics.EnvironmentFromEnv()
		if config.PosthogHost == "" && config.PosthogKey != "" {
			config.PosthogHost = "https://us.i.posthog.com"
		}
	}

	writeJSON(w, http.StatusOK, config)
}

func frontendAnalyticsEnabled() bool {
	v := strings.TrimSpace(os.Getenv("ANALYTICS_FRONTEND_ENABLED"))
	return v == "true" || v == "1"
}

func analyticsDisabled() bool {
	v := os.Getenv("ANALYTICS_DISABLED")
	return v == "true" || v == "1"
}

func daemonSetupURLsFromEnv() (string, string) {
	serverURL := normalizePublicURL(os.Getenv("GOOSAR_PUBLIC_URL"))
	appURL := resolveFrontendAppURL()
	if appURL == "" {
		return "", ""
	}

	if serverURL == "" {
		serverURL = appURL
	}
	if isOfficialCloudDaemonConfig(appURL) {
		return "", ""
	}
	return serverURL, appURL
}

func resolveFrontendAppURL() string {
	appURL := normalizePublicURL(os.Getenv("GOOSAR_APP_URL"))
	if appURL == "" {
		appURL = normalizePublicURL(os.Getenv("FRONTEND_ORIGIN"))
	}
	return appURL
}

func normalizePublicURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func isOfficialCloudDaemonConfig(appURL string) bool {
	host := os.Getenv("GOOSAR_OFFICIAL_CLOUD_HOST")
	if host == "" {
		return false
	}
	return urlHostEquals(appURL, host)
}

func isOfficialCloudDeployment() bool {
	return isOfficialCloudDaemonConfig(resolveFrontendAppURL())
}

func urlHostEquals(raw, want string) bool {
	host := canonicalURLHost(raw)
	if host == "" {
		return false
	}
	want = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(want)), ".")
	return host == want
}

func canonicalURLHost(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" && !strings.Contains(raw, "://") {
		u, err = url.Parse("https://" + raw)
		if err != nil {
			return ""
		}
		host = u.Hostname()
	}
	return strings.TrimSuffix(strings.ToLower(host), ".")
}
